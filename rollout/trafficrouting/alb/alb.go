package alb

import (
	"context"
	"fmt"
	"strconv"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/aws"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	jsonutil "github.com/argoproj/argo-rollouts/utils/json"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
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
	ctx := context.TODO()
	rollout := r.cfg.Rollout
	ingressName := rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingress
	ingress, err := r.cfg.IngressWrapper.GetCached(rollout.Namespace, ingressName)
	if err != nil {
		return err
	}
	actionService := r.cfg.Rollout.Spec.Strategy.Canary.StableService
	if r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.ALB.RootService != "" {
		actionService = r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.ALB.RootService
	}
	port := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.ALB.ServicePort
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
	r.cfg.Recorder.Eventf(r.cfg.Rollout, record.EventOptions{EventReason: "PatchingALBIngress"}, "Updating Ingress `%s` to desiredWeight '%d'", ingressName, desiredWeight)

	_, err = r.cfg.IngressWrapper.Patch(ctx, ingress.GetNamespace(), ingress.GetName(), types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		r.log.WithField("err", err.Error()).Error("error patching alb ingress")
		return fmt.Errorf("error patching alb ingress `%s`: %v", ingressName, err)
	}
	return nil
}

func (r *Reconciler) shouldVerifyWeight() bool {
	if r.cfg.VerifyWeight != nil {
		return *r.cfg.VerifyWeight
	}
	return defaults.VerifyTargetGroup()
}

func (r *Reconciler) VerifyWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error) {
	if !r.shouldVerifyWeight() {
		return nil, nil
	}
	ctx := context.TODO()
	rollout := r.cfg.Rollout
	ingressName := rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingress
	ingress, err := r.cfg.IngressWrapper.GetCached(rollout.Namespace, ingressName)
	if err != nil {
		return pointer.BoolPtr(false), err
	}
	resourceIDToDest := map[string]v1alpha1.WeightDestination{}

	canaryService := rollout.Spec.Strategy.Canary.CanaryService
	canaryResourceID := aws.BuildTargetGroupResourceID(rollout.Namespace, ingress.GetName(), canaryService, rollout.Spec.Strategy.Canary.TrafficRouting.ALB.ServicePort)

	for _, dest := range additionalDestinations {
		resourceID := aws.BuildTargetGroupResourceID(rollout.Namespace, ingress.GetName(), dest.ServiceName, rollout.Spec.Strategy.Canary.TrafficRouting.ALB.ServicePort)
		resourceIDToDest[resourceID] = dest
	}

	loadBalancerStatus := ingress.GetLoadBalancerStatus()
	if len(loadBalancerStatus.Ingress) == 0 {
		r.log.Infof("LoadBalancer not yet allocated")
	}

	numVerifiedWeights := 0
	for _, lbIngress := range loadBalancerStatus.Ingress {
		if lbIngress.Hostname == "" {
			continue
		}
		lb, err := r.aws.FindLoadBalancerByDNSName(ctx, lbIngress.Hostname)
		if err != nil {
			r.cfg.Recorder.Warnf(rollout, record.EventOptions{EventReason: conditions.TargetGroupVerifyErrorReason}, conditions.TargetGroupVerifyErrorMessage, canaryService, "unknown", err.Error())
			return pointer.BoolPtr(false), err
		}
		if lb == nil || lb.LoadBalancerArn == nil {
			r.log.Infof("LoadBalancer %s not found", lbIngress.Hostname)
			return pointer.BoolPtr(false), nil
		}
		lbTargetGroups, err := r.aws.GetTargetGroupMetadata(ctx, *lb.LoadBalancerArn)
		if err != nil {
			r.cfg.Recorder.Warnf(rollout, record.EventOptions{EventReason: conditions.TargetGroupVerifyErrorReason}, conditions.TargetGroupVerifyErrorMessage, canaryService, "unknown", err.Error())
			return pointer.BoolPtr(false), err
		}
		logCtx := r.log.WithField("lb", *lb.LoadBalancerArn)
		for _, tg := range lbTargetGroups {
			if tg.Tags[aws.AWSLoadBalancerV2TagKeyResourceID] == canaryResourceID {
				if tg.Weight != nil {
					logCtx := logCtx.WithField("tg", *tg.TargetGroupArn)
					logCtx.Infof("canary weight of %s (desired: %d, current: %d)", canaryResourceID, desiredWeight, *tg.Weight)
					verified := *tg.Weight == desiredWeight
					if verified {
						numVerifiedWeights += 1
						r.cfg.Recorder.Eventf(rollout, record.EventOptions{EventReason: conditions.TargetGroupVerifiedReason}, conditions.TargetGroupVerifiedWeightsMessage, canaryService, *tg.TargetGroupArn, desiredWeight)
					} else {
						r.cfg.Recorder.Warnf(rollout, record.EventOptions{EventReason: conditions.TargetGroupUnverifiedReason}, conditions.TargetGroupUnverifiedWeightsMessage, canaryService, *tg.TargetGroupArn, desiredWeight, *tg.Weight)
					}
				}
			} else if dest, ok := resourceIDToDest[tg.Tags[aws.AWSLoadBalancerV2TagKeyResourceID]]; ok {
				if tg.Weight != nil {
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
	return pointer.BoolPtr(numVerifiedWeights == 1+len(additionalDestinations)), nil
}

func getForwardActionString(r *v1alpha1.Rollout, port int32, desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) string {
	stableService := r.Spec.Strategy.Canary.StableService
	canaryService := r.Spec.Strategy.Canary.CanaryService
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
	bytes := jsonutil.MustMarshal(action)
	return string(bytes)
}

func getDesiredAnnotations(current *ingressutil.Ingress, r *v1alpha1.Rollout, port int32, desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (map[string]string, error) {
	desired := current.DeepCopy().GetAnnotations()
	key := ingressutil.ALBActionAnnotationKey(r)
	desired[key] = getForwardActionString(r, port, desiredWeight, additionalDestinations...)
	m, err := ingressutil.NewManagedALBActions(desired[ingressutil.ManagedActionsAnnotation])
	if err != nil {
		return nil, err
	}
	m[r.Name] = key
	desired[ingressutil.ManagedActionsAnnotation] = m.String()
	return desired, nil
}

// UpdateHash informs a traffic routing reconciler about new canary/stable pod hashes
func (r *Reconciler) UpdateHash(canaryHash, stableHash string) error {
	return nil
}
