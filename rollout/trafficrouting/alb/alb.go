package alb

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	rolloututil "github.com/argoproj/argo-rollouts/utils/rollout"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting"
	"github.com/argoproj/argo-rollouts/utils/aws"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	jsonutil "github.com/argoproj/argo-rollouts/utils/json"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

const (
	// Type holds this controller type
	Type = "ALB"
)

// ReconcilerConfig describes static configuration data for the ALB Ingress reconciler
type ReconcilerConfig struct {
	Rollout        *v1alpha1.Rollout
	Client         kubernetes.Interface
	Recorder       record.EventRecorder
	ControllerKind schema.GroupVersionKind
	IngressWrapper IngressWrapper
	Status         *v1alpha1.RolloutStatus
	VerifyWeight   *bool
}

type IngressWrapper interface {
	GetCached(namespace, name string) (*ingressutil.Ingress, error)
	Patch(ctx context.Context, namespace, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*ingressutil.Ingress, error)
}

// Reconciler holds required fields to reconcile ALB Ingress resources
type Reconciler struct {
	cfg ReconcilerConfig
	log *logrus.Entry
	aws aws.Client
}

// NewReconciler returns a reconciler struct that brings the ALB Ingress into the desired state
func NewReconciler(cfg ReconcilerConfig) (*Reconciler, error) {
	awsClient, err := aws.NewClient()
	if err != nil {
		return nil, err
	}
	reconciler := Reconciler{
		cfg: cfg,
		log: logutil.WithRollout(cfg.Rollout).WithField(logutil.IngressKey, cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingress),
		aws: awsClient,
	}
	return &reconciler, nil
}

// Type indicates this reconciler is an ALB ingress reconciler
func (r *Reconciler) Type() string {
	return Type
}

// SetWeight modifies ALB Ingress resources to reach desired state
func (r *Reconciler) SetWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
	if ingresses := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingresses; ingresses != nil {
		return r.SetWeightPerIngress(desiredWeight, ingresses, additionalDestinations...)
	} else {
		return r.SetWeightPerIngress(desiredWeight, []string{r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingress}, additionalDestinations...)
	}
}

// SetWeightPerIngress modifies each ALB Ingress resource to reach desired state in the scenario of a rollout
func (r *Reconciler) SetWeightPerIngress(desiredWeight int32, ingresses []string, additionalDestinations ...v1alpha1.WeightDestination) error {
	for _, ingress := range ingresses {
		ctx := context.TODO()
		rollout := r.cfg.Rollout
		ingressName := ingress
		ingress, err := r.cfg.IngressWrapper.GetCached(rollout.Namespace, ingressName)
		if err != nil {
			return err
		}
		actionService := rollout.Spec.Strategy.Canary.StableService
		if rollout.Spec.Strategy.Canary.TrafficRouting.ALB.RootService != "" {
			actionService = rollout.Spec.Strategy.Canary.TrafficRouting.ALB.RootService
		}
		port := rollout.Spec.Strategy.Canary.TrafficRouting.ALB.ServicePort
		if !ingressutil.HasRuleWithService(ingress, actionService) {
			return fmt.Errorf("ingress does not have service `%s` in rules", actionService)
		}

		desiredAnnotations, err := getDesiredAnnotations(ingress, rollout, port, desiredWeight, additionalDestinations...)
		if err != nil {
			return err
		}
		desiredIngress := ingressutil.NewIngressWithAnnotations(ingress.Mode(), desiredAnnotations)
		patch, modified, err := ingressutil.BuildIngressPatch(ingress.Mode(), ingress, desiredIngress, ingressutil.WithAnnotations())
		if err != nil {
			return nil
		}
		if !modified {
			r.log.Info("no changes to the ALB Ingress")
			return nil
		}
		r.log.WithField("patch", string(patch)).Debug("applying ALB Ingress patch")
		r.log.WithField("desiredWeight", desiredWeight).Info("updating ALB Ingress")
		r.cfg.Recorder.Eventf(rollout, record.EventOptions{EventReason: "PatchingALBIngress"}, "Updating Ingress `%s` to desiredWeight '%d'", ingressName, desiredWeight)

		_, err = r.cfg.IngressWrapper.Patch(ctx, ingress.GetNamespace(), ingress.GetName(), types.MergePatchType, patch, metav1.PatchOptions{})
		if err != nil {
			r.log.WithField("err", err.Error()).Error("error patching alb ingress")
			return fmt.Errorf("error patching alb ingress `%s`: %v", ingressName, err)
		}
	}
	return nil
}

func (r *Reconciler) SetHeaderRoute(headerRoute *v1alpha1.SetHeaderRoute) error {
	if headerRoute == nil {
		return nil
	}

	if ingresses := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingresses; ingresses != nil {
		return r.SetHeaderRoutePerIngress(headerRoute, ingresses)
	} else {
		return r.SetHeaderRoutePerIngress(headerRoute, []string{r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingress})
	}

}

func (r *Reconciler) SetHeaderRoutePerIngress(headerRoute *v1alpha1.SetHeaderRoute, ingresses []string) error {
	for _, ingress := range ingresses {
		ctx := context.TODO()
		rollout := r.cfg.Rollout
		ingressName := ingress
		action := headerRoute.Name
		port := rollout.Spec.Strategy.Canary.TrafficRouting.ALB.ServicePort

		ingress, err := r.cfg.IngressWrapper.GetCached(rollout.Namespace, ingressName)
		if err != nil {
			return err
		}

		desiredAnnotations, err := getDesiredHeaderAnnotations(ingress, rollout, port, headerRoute)
		if err != nil {
			return err
		}
		desiredIngress := ingressutil.NewIngressWithSpecAndAnnotations(ingress, desiredAnnotations)
		hasRule := ingressutil.HasRuleWithService(ingress, action)
		if hasRule && headerRoute.Match == nil {
			desiredIngress.RemovePathByServiceName(action)
		}
		if !hasRule && headerRoute.Match != nil {
			desiredIngress.CreateAnnotationBasedPath(action)
		}
		desiredIngress.SortHttpPaths(rollout.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes)
		patch, modified, err := ingressutil.BuildIngressPatch(ingress.Mode(), ingress, desiredIngress, ingressutil.WithAnnotations(), ingressutil.WithSpec())
		if err != nil {
			return nil
		}
		if !modified {
			r.log.Info("no changes to the ALB Ingress for header routing")
			return nil
		}
		r.log.WithField("patch", string(patch)).Debug("applying ALB Ingress patch")
		r.cfg.Recorder.Eventf(rollout, record.EventOptions{EventReason: "PatchingALBIngress"}, "Updating Ingress `%s` to headerRoute '%d'", ingressName, headerRoute)

		_, err = r.cfg.IngressWrapper.Patch(ctx, ingress.GetNamespace(), ingress.GetName(), types.MergePatchType, patch, metav1.PatchOptions{})
		if err != nil {
			r.log.WithField("err", err.Error()).Error("error patching alb ingress")
			return fmt.Errorf("error patching alb ingress `%s`: %v", ingressName, err)
		}
	}
	return nil
}

// Gets the controller configuration flag for verifying alb weights
func (r *Reconciler) getShouldVerifyWeightCfg() bool {
	if r.cfg.VerifyWeight != nil {
		return *r.cfg.VerifyWeight
	}
	return defaults.VerifyTargetGroup()
}

func (r *Reconciler) VerifyWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error) {
	if !r.getShouldVerifyWeightCfg() {
		r.cfg.Status.ALB = nil
		r.cfg.Status.ALBs = nil
		return nil, nil
	}

	if !rolloututil.ShouldVerifyWeight(r.cfg.Rollout) {
		// If we should not verify weight but the ALB status has not been set yet due to a Rollout resource just being
		// installed in the cluster we want to actually run the rest of the function, so we do not return if
		// r.cfg.Rollout.Status.ALB is nil. However, if we should not verify, and we have already updated the status once
		// we return early to avoid calling AWS apis.
		if r.cfg.Rollout.Status.ALBs != nil || r.cfg.Rollout.Status.ALB != nil {
			return nil, nil
		}
	}

	if r.cfg.Status.ALB == nil {
		r.cfg.Status.ALB = &v1alpha1.ALBStatus{}
	}
	albsCount := len(r.cfg.Status.ALBs)
	if ingresses := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingresses; len(ingresses) > 0 {
		if albsCount == 0 || albsCount != len(ingresses) {
			r.cfg.Status.ALBs = make([]v1alpha1.ALBStatus, len(ingresses))
		}
		return r.VerifyWeightPerIngress(desiredWeight, ingresses, additionalDestinations...)
	} else {
		if albsCount == 0 || albsCount != 1 {
			r.cfg.Status.ALBs = make([]v1alpha1.ALBStatus, 1)
		}
		return r.VerifyWeightPerIngress(desiredWeight, []string{r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingress}, additionalDestinations...)
	}
}

func (r *Reconciler) VerifyWeightPerIngress(desiredWeight int32, ingresses []string, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error) {
	var numVerifiedWeights int
	numVerifiedWeights = 0
	for i, ingress := range ingresses {
		ctx := context.TODO()
		rollout := r.cfg.Rollout
		ingressName := ingress
		ingress, err := r.cfg.IngressWrapper.GetCached(rollout.Namespace, ingressName)
		if err != nil {
			return pointer.Bool(false), err
		}
		resourceIDToDest := map[string]v1alpha1.WeightDestination{}

		stableService, canaryService := trafficrouting.GetStableAndCanaryServices(rollout)
		canaryResourceID := aws.BuildTargetGroupResourceID(rollout.Namespace, ingress.GetName(), canaryService, rollout.Spec.Strategy.Canary.TrafficRouting.ALB.ServicePort)
		stableResourceID := aws.BuildTargetGroupResourceID(rollout.Namespace, ingress.GetName(), stableService, rollout.Spec.Strategy.Canary.TrafficRouting.ALB.ServicePort)

		for _, dest := range additionalDestinations {
			resourceID := aws.BuildTargetGroupResourceID(rollout.Namespace, ingress.GetName(), dest.ServiceName, rollout.Spec.Strategy.Canary.TrafficRouting.ALB.ServicePort)
			resourceIDToDest[resourceID] = dest
		}

		loadBalancerStatus := ingress.GetLoadBalancerStatus()
		if len(loadBalancerStatus.Ingress) == 0 {
			r.log.Infof("LoadBalancer not yet allocated")
		}

		for _, lbIngress := range loadBalancerStatus.Ingress {
			if lbIngress.Hostname == "" {
				continue
			}
			lb, err := r.aws.FindLoadBalancerByDNSName(ctx, lbIngress.Hostname)
			if err != nil {
				r.cfg.Recorder.Warnf(rollout, record.EventOptions{EventReason: conditions.TargetGroupVerifyErrorReason}, conditions.TargetGroupVerifyErrorMessage, canaryService, "unknown", err.Error())
				return pointer.Bool(false), err
			}
			if lb == nil || lb.LoadBalancerArn == nil {
				r.cfg.Recorder.Warnf(rollout, record.EventOptions{EventReason: conditions.LoadBalancerNotFoundReason}, conditions.LoadBalancerNotFoundMessage, lbIngress.Hostname)
				return pointer.Bool(false), nil
			}

			r.cfg.Status.ALBs[i].Ingress = ingressName
			r.cfg.Status.ALB.Ingress = ingressName
			updateLoadBalancerStatus(&r.cfg.Status.ALBs[i], lb, r.log)
			updateLoadBalancerStatus(r.cfg.Status.ALB, lb, r.log)

			lbTargetGroups, err := r.aws.GetTargetGroupMetadata(ctx, *lb.LoadBalancerArn)
			if err != nil {
				r.cfg.Recorder.Warnf(rollout, record.EventOptions{EventReason: conditions.TargetGroupVerifyErrorReason}, conditions.TargetGroupVerifyErrorMessage, canaryService, "unknown", err.Error())
				return pointer.Bool(false), err
			}
			logCtx := r.log.WithField("lb", *lb.LoadBalancerArn)
			for _, tg := range lbTargetGroups {
				updateTargetGroupStatus(&r.cfg.Status.ALBs[i], &tg, canaryResourceID, stableResourceID, r.log)
				updateTargetGroupStatus(r.cfg.Status.ALB, &tg, canaryResourceID, stableResourceID, r.log)
				if tg.Weight != nil {
					if tg.Tags[aws.AWSLoadBalancerV2TagKeyResourceID] == canaryResourceID {
						logCtx := logCtx.WithField("tg", *tg.TargetGroupArn)
						logCtx.Infof("canary weight of %s (desired: %d, current: %d)", canaryResourceID, desiredWeight, *tg.Weight)
						verified := *tg.Weight == desiredWeight
						if verified {
							numVerifiedWeights += 1
							r.cfg.Recorder.Eventf(rollout, record.EventOptions{EventReason: conditions.TargetGroupVerifiedReason}, conditions.TargetGroupVerifiedWeightsMessage, canaryService, *tg.TargetGroupArn, desiredWeight)
						} else {
							r.cfg.Recorder.Warnf(rollout, record.EventOptions{EventReason: conditions.TargetGroupUnverifiedReason}, conditions.TargetGroupUnverifiedWeightsMessage, canaryService, *tg.TargetGroupArn, desiredWeight, *tg.Weight)
						}
					} else if dest, ok := resourceIDToDest[tg.Tags[aws.AWSLoadBalancerV2TagKeyResourceID]]; ok {
						logCtx := logCtx.WithField("tg", *tg.TargetGroupArn)
						logCtx.Infof("%s weight of %s (desired: %d, current: %d)", dest.ServiceName, tg.Tags[aws.AWSLoadBalancerV2TagKeyResourceID], dest.Weight, *tg.Weight)
						verified := *tg.Weight == dest.Weight
						if verified {
							numVerifiedWeights += 1
							r.cfg.Recorder.Eventf(rollout, record.EventOptions{EventReason: conditions.TargetGroupVerifiedReason}, conditions.TargetGroupVerifiedWeightsMessage, dest.ServiceName, *tg.TargetGroupArn, dest.Weight)
						} else {
							r.cfg.Recorder.Warnf(rollout, record.EventOptions{EventReason: conditions.TargetGroupUnverifiedReason}, conditions.TargetGroupUnverifiedWeightsMessage, dest.ServiceName, *tg.TargetGroupArn, dest.Weight, *tg.Weight)
						}
					}
				}
			}
		}
	}
	return pointer.Bool(numVerifiedWeights == len(ingresses)+len(additionalDestinations)), nil
}

func updateLoadBalancerStatus(status *v1alpha1.ALBStatus, lb *elbv2types.LoadBalancer, log *logrus.Entry) {
	status.LoadBalancer.Name = *lb.LoadBalancerName
	status.LoadBalancer.ARN = *lb.LoadBalancerArn
	if lbArnParts := strings.Split(*lb.LoadBalancerArn, "/"); len(lbArnParts) > 2 {
		status.LoadBalancer.FullName = strings.Join(lbArnParts[2:], "/")
	} else {
		status.LoadBalancer.FullName = ""
		log.Errorf("error parsing load balancer arn: '%s'", *lb.LoadBalancerArn)
	}
}

func updateTargetGroupStatus(status *v1alpha1.ALBStatus, tg *aws.TargetGroupMeta, canaryResourceID string, stableResourceID string, log *logrus.Entry) {
	if tg.Tags[aws.AWSLoadBalancerV2TagKeyResourceID] == canaryResourceID {
		status.CanaryTargetGroup.Name = *tg.TargetGroupName
		status.CanaryTargetGroup.ARN = *tg.TargetGroupArn
		if tgArnParts := strings.Split(*tg.TargetGroupArn, "/"); len(tgArnParts) > 1 {
			status.CanaryTargetGroup.FullName = strings.Join(tgArnParts[1:], "/")
		} else {
			status.CanaryTargetGroup.FullName = ""
			log.Errorf("error parsing canary target group arn: '%s'", *tg.TargetGroupArn)
		}
	} else if tg.Tags[aws.AWSLoadBalancerV2TagKeyResourceID] == stableResourceID {
		status.StableTargetGroup.Name = *tg.TargetGroupName
		status.StableTargetGroup.ARN = *tg.TargetGroupArn
		if tgArnParts := strings.Split(*tg.TargetGroupArn, "/"); len(tgArnParts) > 1 {
			status.StableTargetGroup.FullName = strings.Join(tgArnParts[1:], "/")
		} else {
			status.StableTargetGroup.FullName = ""
			log.Errorf("error parsing stable target group arn: '%s'", *tg.TargetGroupArn)
		}
	}
}

func getForwardActionString(r *v1alpha1.Rollout, port int32, desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (string, error) {
	stableService, canaryService := trafficrouting.GetStableAndCanaryServices(r)
	portStr := strconv.Itoa(int(port))
	stableWeight := int32(100)
	targetGroups := make([]ingressutil.ALBTargetGroup, 0)
	// create target group for canary
	targetGroups = append(targetGroups, ingressutil.ALBTargetGroup{
		ServiceName: canaryService,
		ServicePort: portStr,
		Weight:      pointer.Int64Ptr(int64(desiredWeight)),
	})
	// update stableWeight
	stableWeight -= desiredWeight

	for _, dest := range additionalDestinations {
		// Create target group for each additional destination
		targetGroups = append(targetGroups, ingressutil.ALBTargetGroup{
			ServiceName: dest.ServiceName,
			ServicePort: portStr,
			Weight:      pointer.Int64Ptr(int64(dest.Weight)),
		})
		stableWeight -= dest.Weight
	}

	// Create target group for stable with updated stableWeight
	targetGroups = append(targetGroups, ingressutil.ALBTargetGroup{
		ServiceName: stableService,
		ServicePort: portStr,
		Weight:      pointer.Int64Ptr(int64(stableWeight)),
	})

	action := ingressutil.ALBAction{
		Type: "forward",
		ForwardConfig: ingressutil.ALBForwardConfig{
			TargetGroups: targetGroups,
		},
	}

	var stickinessConfig = r.Spec.Strategy.Canary.TrafficRouting.ALB.StickinessConfig
	if stickinessConfig != nil && stickinessConfig.Enabled {
		// AWS API valid range
		// https://docs.aws.amazon.com/elasticloadbalancing/latest/APIReference/API_TargetGroupStickinessConfig.html
		if stickinessConfig.DurationSeconds < 1 || stickinessConfig.DurationSeconds > 604800 {
			return "", fmt.Errorf("TargetGroupStickinessConfig's duration must be between 1 and 604800 seconds (7 days)!")
		}
		newStickyConfig := ingressutil.ALBTargetGroupStickinessConfig{
			Enabled:         true,
			DurationSeconds: stickinessConfig.DurationSeconds,
		}
		action.ForwardConfig.TargetGroupStickinessConfig = &newStickyConfig
	}

	bytes := jsonutil.MustMarshal(action)
	return string(bytes), nil
}

func getDesiredAnnotations(current *ingressutil.Ingress, r *v1alpha1.Rollout, port int32, desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (map[string]string, error) {
	desired := current.DeepCopy().GetAnnotations()
	key := ingressutil.ALBActionAnnotationKey(r)
	value, err := getForwardActionString(r, port, desiredWeight, additionalDestinations...)
	if err != nil {
		return nil, err
	}
	desired[key] = value
	return modifyManagedAnnotation(desired, r.Name, true, key)
}

func getDesiredHeaderAnnotations(current *ingressutil.Ingress, r *v1alpha1.Rollout, port int32, headerRoute *v1alpha1.SetHeaderRoute) (map[string]string, error) {
	desired := current.DeepCopy().GetAnnotations()
	actionKey := ingressutil.ALBHeaderBasedActionAnnotationKey(r, headerRoute.Name)
	conditionKey := ingressutil.ALBHeaderBasedConditionAnnotationKey(r, headerRoute.Name)
	add := headerRoute.Match != nil
	if add {
		actionValue, err := getTrafficForwardActionString(r, port)
		if err != nil {
			return nil, err
		}
		conditionValue, err := getTrafficForwardConditionString(headerRoute)
		if err != nil {
			return nil, err
		}
		desired[actionKey] = actionValue
		desired[conditionKey] = conditionValue
	} else {
		delete(desired, actionKey)
		delete(desired, conditionKey)
	}

	return modifyManagedAnnotation(desired, r.Name, add, actionKey, conditionKey)
}

func modifyManagedAnnotation(annotations map[string]string, rolloutName string, add bool, annotationKeys ...string) (map[string]string, error) {
	m, err := ingressutil.NewManagedALBAnnotations(annotations[ingressutil.ManagedAnnotations])
	if err != nil {
		return nil, err
	}
	managedAnnotation := m[rolloutName]
	if managedAnnotation == nil {
		managedAnnotation = ingressutil.ManagedALBAnnotation{}
	}
	for _, annotationKey := range annotationKeys {
		if add {
			if !hasValue(managedAnnotation, annotationKey) {
				managedAnnotation = append(managedAnnotation, annotationKey)
			}
		} else {
			managedAnnotation = removeValue(managedAnnotation, annotationKey)
		}
	}
	m[rolloutName] = managedAnnotation
	annotations[ingressutil.ManagedAnnotations] = m.String()
	return annotations, nil
}

func hasValue(array []string, key string) bool {
	for _, item := range array {
		if item == key {
			return true
		}
	}
	return false
}

func removeValue(array []string, key string) []string {
	for i, v := range array {
		if v == key {
			array = append(array[:i], array[i+1:]...)
		}
	}
	return array
}

func getTrafficForwardActionString(r *v1alpha1.Rollout, port int32) (string, error) {
	_, canaryService := trafficrouting.GetStableAndCanaryServices(r)
	portStr := strconv.Itoa(int(port))
	weight := int64(100)
	targetGroups := make([]ingressutil.ALBTargetGroup, 0)
	// create target group for canary
	targetGroups = append(targetGroups, ingressutil.ALBTargetGroup{
		ServiceName: canaryService,
		ServicePort: portStr,
		Weight:      pointer.Int64Ptr(weight),
	})

	action := ingressutil.ALBAction{
		Type: "forward",
		ForwardConfig: ingressutil.ALBForwardConfig{
			TargetGroups: targetGroups,
		},
	}

	var stickinessConfig = r.Spec.Strategy.Canary.TrafficRouting.ALB.StickinessConfig
	if stickinessConfig != nil && stickinessConfig.Enabled {
		// AWS API valid range
		// https://docs.aws.amazon.com/elasticloadbalancing/latest/APIReference/API_TargetGroupStickinessConfig.html
		if stickinessConfig.DurationSeconds < 1 || stickinessConfig.DurationSeconds > 604800 {
			return "", fmt.Errorf("TargetGroupStickinessConfig's duration must be between 1 and 604800 seconds (7 days)!")
		}
		newStickyConfig := ingressutil.ALBTargetGroupStickinessConfig{
			Enabled:         true,
			DurationSeconds: stickinessConfig.DurationSeconds,
		}
		action.ForwardConfig.TargetGroupStickinessConfig = &newStickyConfig
	}

	bytes := jsonutil.MustMarshal(action)
	return string(bytes), nil
}

func getTrafficForwardConditionString(headerRoute *v1alpha1.SetHeaderRoute) (string, error) {
	var res []ingressutil.ALBCondition
	for _, match := range headerRoute.Match {
		condition := ingressutil.ALBCondition{
			Field: "http-header",
			HttpHeaderConfig: ingressutil.HttpHeaderConfig{
				HttpHeaderName: match.HeaderName,
				Values:         []string{match.HeaderValue.Exact},
			},
		}
		res = append(res, condition)
	}
	bytes := jsonutil.MustMarshal(res)
	return string(bytes), nil
}

// UpdateHash informs a traffic routing reconciler about new canary/stable pod hashes
func (r *Reconciler) UpdateHash(canaryHash, stableHash string, additionalDestinations ...v1alpha1.WeightDestination) error {
	return nil
}

func (r *Reconciler) SetMirrorRoute(setMirrorRoute *v1alpha1.SetMirrorRoute) error {
	return nil
}

func (r *Reconciler) RemoveManagedRoutes() error {
	if len(r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes) == 0 {
		return nil
	}

	if ingresses := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingresses; ingresses != nil {
		return r.RemoveManagedRoutesPerIngress(ingresses)
	} else {
		return r.RemoveManagedRoutesPerIngress([]string{r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingress})
	}
}

func (r *Reconciler) RemoveManagedRoutesPerIngress(ingresses []string) error {
	for _, ingress := range ingresses {
		ctx := context.TODO()
		rollout := r.cfg.Rollout
		ingressName := ingress

		ingress, err := r.cfg.IngressWrapper.GetCached(rollout.Namespace, ingressName)
		if err != nil {
			return err
		}

		desiredAnnotations := ingress.DeepCopy().GetAnnotations()
		var actionKeys []string
		for _, managedRoute := range rollout.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes {
			actionKey := ingressutil.ALBHeaderBasedActionAnnotationKey(rollout, managedRoute.Name)
			conditionKey := ingressutil.ALBHeaderBasedConditionAnnotationKey(rollout, managedRoute.Name)
			delete(desiredAnnotations, actionKey)
			delete(desiredAnnotations, conditionKey)
			actionKeys = append(actionKeys, actionKey, conditionKey)
		}
		desiredAnnotations, err = modifyManagedAnnotation(desiredAnnotations, rollout.Name, false, actionKeys...)
		if err != nil {
			return err
		}

		desiredIngress := ingressutil.NewIngressWithSpecAndAnnotations(ingress, desiredAnnotations)

		for _, managedRoute := range rollout.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes {
			desiredIngress.RemovePathByServiceName(managedRoute.Name)
		}

		patch, modified, err := ingressutil.BuildIngressPatch(ingress.Mode(), ingress, desiredIngress, ingressutil.WithAnnotations(), ingressutil.WithSpec())
		if err != nil {
			return nil
		}
		if !modified {
			r.log.Info("no changes to the ALB Ingress for header routing")
			return nil
		}
		r.log.WithField("patch", string(patch)).Debug("applying ALB Ingress patch")
		r.cfg.Recorder.Eventf(rollout, record.EventOptions{EventReason: "PatchingALBIngress"}, "Updating Ingress `%s` removing managed routes", ingressName)

		_, err = r.cfg.IngressWrapper.Patch(ctx, ingress.GetNamespace(), ingress.GetName(), types.MergePatchType, patch, metav1.PatchOptions{})
		if err != nil {
			r.log.WithField("err", err.Error()).Error("error patching alb ingress")
			return fmt.Errorf("error patching alb ingress `%s`: %v", ingressName, err)
		}
	}
	return nil
}
