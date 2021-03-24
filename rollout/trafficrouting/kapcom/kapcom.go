package kapcom

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	kapcomv1beta1 "github.com/fluxcd/flagger/pkg/apis/kapcom/v1beta1"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	extensionslisters "k8s.io/client-go/listers/extensions/v1beta1"
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

// Type holds this controller type
const Type = "Kapcom"

// ReconcilerConfig describes static configuration data for the nginx reconciler
type ReconcilerConfig struct {
	Rollout        *v1alpha1.Rollout
	Client         kubernetes.Interface
	Recorder       record.EventRecorder
	ControllerKind schema.GroupVersionKind
	IngressLister  extensionslisters.IngressLister
	ingressClass   string // countour class I think
}

// Reconciler holds required fields to reconcile Nginx resources
type Reconciler struct {
	cfg ReconcilerConfig
	log *logrus.Entry
}

// NewReconciler returns a reconciler struct that brings the canary Ingress into the desired state
func NewReconciler(cfg ReconcilerConfig) *Reconciler {
	return &Reconciler{
		cfg: cfg,
		log: logutil.WithRollout(cfg.Rollout),
	}
}

// Type indicates this reconciler is an Nginx reconciler
func (r *Reconciler) Type() string {
	return Type
}

// This method should create the new ingressroute for base, if not present.
// SetWeight modifies Nginx Ingress resources to reach desired state
func (r *Reconciler) canaryIngressRoute(stableIngress *kapcomv1beta1.Ingress, name string, desiredWeight int32) (*kapcomv1beta1.Ingress, error) {
	canary := r.cfg.Rollout.Spec.Strategy.Canary
	stableIngressName := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress
	stableServiceName := r.cfg.Rollout.Spec.Strategy.Canary.StableService
	canaryServiceName := r.cfg.Rollout.Spec.Strategy.Canary.CanaryService
	// On the fqdn ingress basis this will be created
	// Check if stable ingress exists (from lister, which has a cache), error if it does not
	stableIngress, err := r.cfg.IngressLister.Ingresses(r.cfg.Rollout.Namespace).Get(stableIngressName)
	canaryIngressName := stableIngress.routes[0].delegate.name
	if err != nil {
		r.log.WithField(logutil.IngressKey, stableIngressName).WithField("err", err.Error()).Error("error retrieving stableIngress")
		return fmt.Errorf("error retrieving stableIngress `%s` from cache: %v", stableIngressName, err)
	}
	// Set metadata
	//metadata: {
	//	name: canaryIngressName,
	//		namespace: stableIngress.metadata.namespace,
	//},

	newSpec := v1beta1.IngressRouteSpec{
		Routes: []v1beta1.Route{
			{
				PermitInsecure: true,
				Match:          r.makeExactPath(canary),
				TimeoutPolicy:  r.makeTimeoutPolicy(canary),
				RetryPolicy:    r.makeRetryPolicy(canary),
				Services: []v1beta1.Service{
					{
						Name:   stableServiceName,
						Port:   int(canary.Spec.Service.Port),
						Weight: 100,
					},
					{
						Name:   canaryServiceName,
						Port:   int(canary.Spec.Service.Port),
						Weight: 0,
					},
				},
			},
		},
	}

	if desiredWeight > 0 {
		newSpec = v1beta1.IngressRouteSpec{
			Routes: []v1beta1.Route{
				{
					PermitInsecure: true,
					Match:          r.makeExactPath(canary),
					TimeoutPolicy:  r.makeTimeoutPolicy(canary),
					RetryPolicy:    r.makeRetryPolicy(canary),
					Services: []v1beta1.Service{
						{
							Name:   primaryName,
							Port:   int(canary.Spec.Service.Port),
							Weight: 100 - desiredWeight,
						},
						{
							Name:   canaryName,
							Port:   int(canary.Spec.Service.Port),
							Weight: desiredWeight,
						},
					},
				},
			},
		}
	}

	proxy, err := kr.kapcomClient.KapcomV1beta1().IngressRoutes(canary.Namespace).Get(context.TODO(), apexName, metav1.GetOptions{})

	if errors.IsNotFound(err) {
		proxy = &v1beta1.IngressRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      apexName,
				Namespace: canary.Namespace,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(canary, schema.GroupVersionKind{
						Group:   flaggerv1.SchemeGroupVersion.Group,
						Version: flaggerv1.SchemeGroupVersion.Version,
						Kind:    flaggerv1.CanaryKind,
					}),
				},
			},
			Spec: newSpec,
			Status: v1beta1.Status{
				CurrentStatus: "valid",
				Description:   "valid IngressRoute",
			},
		}

		if kr.ingressClass != "" {
			proxy.Annotations = map[string]string{
				annotation: "contour-corp",
			}
		}
	return proxy

}

// SetWeight modifies Nginx Ingress resources to reach desired state
func (r *Reconciler) SetWeight(desiredWeight int32) error {
	// get desirec ingressroute
	// create or update depending upon conditions
	ctx := context.TODO()
	stableIngressName := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress
	canaryIngressName := ingressutil.GetCanaryIngressName(r.cfg.Rollout)

	// Check if stable ingress exists (from lister, which has a cache), error if it does not
	stableIngress, err := r.cfg.IngressLister.Ingresses(r.cfg.Rollout.Namespace).Get(stableIngressName)
	if err != nil {
		r.log.WithField(logutil.IngressKey, stableIngressName).WithField("err", err.Error()).Error("error retrieving stableIngress")
		return fmt.Errorf("error retrieving stableIngress `%s` from cache: %v", stableIngressName, err)
	}
	// Check if canary ingress exists (from lister which has a cache), determines whether we later call Create() or Update()
	canaryIngress, err := r.cfg.IngressLister.Ingresses(r.cfg.Rollout.Namespace).Get(canaryIngressName)

	canaryIngressExists := true
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			// An error other than "not found" occurred
			r.log.WithField(logutil.IngressKey, canaryIngressName).WithField("err", err.Error()).Error("error retrieving canary ingress")
			return fmt.Errorf("error retrieving canary ingress `%s` from cache: %v", canaryIngressName, err)
		}
		r.log.WithField(logutil.IngressKey, canaryIngressName).Infof("canary ingress not found")
		canaryIngressExists = false
	}

	// Construct the desired canary Ingress resource
	desiredCanaryIngress, err := r.canaryIngressRoute(stableIngress, canaryIngressName, desiredWeight)
	if err != nil {
		r.log.WithField(logutil.IngressKey, canaryIngressName).Error(err.Error())
		return err
	}

	if !canaryIngressExists {
		r.log.WithField(logutil.IngressKey, canaryIngressName).WithField("desiredWeight", desiredWeight).Info("Creating canary Ingress")
		r.cfg.Recorder.Event(r.cfg.Rollout, corev1.EventTypeNormal, "CreatingCanaryIngress", fmt.Sprintf("Creating canary ingress `%s` with weight `%d`", canaryIngressName, desiredWeight))
		_, err = r.cfg.Client.ExtensionsV1beta1().Ingresses(r.cfg.Rollout.Namespace).Create(ctx, desiredCanaryIngress, metav1.CreateOptions{})
		if err == nil {
			return nil
		}
		if !k8serrors.IsAlreadyExists(err) {
			r.log.WithField(logutil.IngressKey, canaryIngressName).WithField("err", err.Error()).Error("error creating canary ingress")
			return fmt.Errorf("error creating canary ingress `%s`: %v", canaryIngressName, err)
		}
		// Canary ingress was created by a different reconcile call before this one could complete (race)
		// This means we just read it from the API now (instead of cache) and continue with the normal
		// flow we take when the canary already existed.
		canaryIngress, err = r.cfg.Client.ExtensionsV1beta1().Ingresses(r.cfg.Rollout.Namespace).Get(ctx, canaryIngressName, metav1.GetOptions{})
		if err != nil {
			r.log.WithField(logutil.IngressKey, canaryIngressName).Error(err.Error())
			return fmt.Errorf("error retrieving canary ingress `%s` from api: %v", canaryIngressName, err)
		}
	}

	// Canary Ingress already exists, apply a patch if needed

	// Only modify canaryIngress if it is controlled by this Rollout
	if !metav1.IsControlledBy(canaryIngress, r.cfg.Rollout) {
		r.log.WithField(logutil.IngressKey, canaryIngressName).Error("canary ingress controlled by different object")
		return fmt.Errorf("canary ingress `%s` controlled by different object", canaryIngressName)
	}

	// Make patches
	patch, modified, err := compareCanaryIngresses(canaryIngress, desiredCanaryIngress)

	if err != nil {
		r.log.WithField(logutil.IngressKey, canaryIngressName).WithField("err", err.Error()).Error("error constructing canary ingress patch")
		return fmt.Errorf("error constructing canary ingress patch for `%s`: %v", canaryIngressName, err)
	}
	if !modified {
		r.log.WithField(logutil.IngressKey, canaryIngressName).Info("No changes to canary ingress - skipping patch")
		return nil
	}

	r.log.WithField(logutil.IngressKey, canaryIngressName).WithField("patch", string(patch)).Debug("applying canary Ingress patch")
	r.log.WithField(logutil.IngressKey, canaryIngressName).WithField("desiredWeight", desiredWeight).Info("updating canary Ingress")
	r.cfg.Recorder.Event(r.cfg.Rollout, corev1.EventTypeNormal, "PatchingCanaryIngress", fmt.Sprintf("Updating Ingress `%s` to desiredWeight '%d'", canaryIngressName, desiredWeight))

	_, err = r.cfg.Client.ExtensionsV1beta1().Ingresses(r.cfg.Rollout.Namespace).Patch(ctx, canaryIngressName, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		r.log.WithField(logutil.IngressKey, canaryIngressName).WithField("err", err.Error()).Error("error patching canary ingress")
		return fmt.Errorf("error patching canary ingress `%s`: %v", canaryIngressName, err)
	}

	return nil
}

// Remove all canary instance of flagger
func (r *Reconciler) Reconcile() error {

	const annotation = "kubernetes.io/ingress.class"
	rollout := r.cfg.Rollout
	ingressName := rollout.Spec.Strategy.Canary.TrafficRouting.Kapcom.Ingress
	ingress, err := r.cfg.IngressLister.Ingresses(rollout.Namespace).Get(ingressName)
	if err != nil {
		return err
	}
	canary := r.cfg.Rollout.Spec.Strategy.Canary
	stableService := r.cfg.Rollout.Spec.Strategy.Canary.StableService
	port := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.ALB.ServicePort

	apexName, primaryName, canaryName := canary.GetServiceNames()

	r.log.WithField("desiredWeight", desiredWeight).Info("updating Kapcom Ingress")

	// How to get the kapcomclient
	proxy, err := r.kapcomClient.KapcomV1beta1().IngressRoutes(canary.Namespace).Get(context.TODO(), apexName, metav1.GetOptions{})
	// if not found it is recreating
	if errors.IsNotFound(err) {
		proxy = &v1beta1.IngressRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      apexName,
				Namespace: canary.Namespace,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(canary, schema.GroupVersionKind{
						Group:   flaggerv1.SchemeGroupVersion.Group,
						Version: flaggerv1.SchemeGroupVersion.Version,
						Kind:    flaggerv1.CanaryKind,
					}),
				},
			},
			Spec: newSpec,
			Status: v1beta1.Status{
				CurrentStatus: "valid",
				Description:   "valid IngressRoute",
			},
		}

		if r.ingressClass != "" {
			proxy.Annotations = map[string]string{
				annotation: "contour-corp",
			}
		}
		//proxy.Spec.Routes[0].PermitInsecure=true
		_, err = r.kapcomClient.KapcomV1beta1().IngressRoutes(canary.Namespace).Create(context.TODO(), proxy, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("IngressRoute %s.%s create error: %w", apexName, canary.Namespace, err)
		}
		r.logger.With("canary", fmt.Sprintf("%s.%s", canary.Name, canary.Namespace)).
			Infof("IngressRoute %s.%s created", proxy.GetName(), canary.Namespace)
		return nil
	} else if err != nil {
		return fmt.Errorf("IngressRoute %s.%s get query error: %w", apexName, canary.Namespace, err)
	}

	// update IngressRoute but keep the original destination weights
	if proxy != nil {
		if diff := cmp.Diff(
			newSpec,
			proxy.Spec,
			cmpopts.IgnoreFields(kapcomv1beta1.Service{}, "Weight"),
		); diff != "" {
			clone := proxy.DeepCopy()
			clone.Spec = newSpec
			_, err = r.kapcomClient.KapcomV1beta1().IngressRoutes(canary.Namespace).Update(context.TODO(), clone, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("IngressRoute %s.%s update error: %w", apexName, canary.Namespace, err)
			}
			r.logger.With("canary", fmt.Sprintf("%s.%s", canary.Name, canary.Namespace)).
				Infof("IngressRoute %s.%s updated", proxy.GetName(), canary.Namespace)
		}

	}

	return nil
}

// GetRoutes returns the service weight for primary and canary
func (r *Reconciler) GetRoutes(canary *flaggerv1.Canary) (
	primaryWeight int,
	canaryWeight int,
	mirrored bool,
	err error,
) {
	apexName, primaryName, _ := canary.GetServiceNames()

	proxy, err := r.kapcomClient.KapcomV1beta1().IngressRoutes(canary.Namespace).Get(context.TODO(), apexName, metav1.GetOptions{})
	if err != nil {
		err = fmt.Errorf("IngressRoute %s.%s get query error %w", apexName, canary.Namespace, err)
		return
	}

	if len(proxy.Spec.Routes) < 1 || len(proxy.Spec.Routes[0].Services) < 2 {
		err = fmt.Errorf("IngressRoute %s.%s services not found", apexName, canary.Namespace)
		return
	}

	for _, dst := range proxy.Spec.Routes[0].Services {
		if dst.Name == primaryName {
			primaryWeight = int(dst.Weight)
			canaryWeight = 100 - primaryWeight
			return
		}
	}
	return

}

func (r *Reconciler) SetRoutes(
	canary *flaggerv1.Canary,
	primaryWeight int,
	canaryWeight int,
	_ bool,
) error {
	apexName, primaryName, canaryName := canary.GetServiceNames()

	if primaryWeight == 0 && canaryWeight == 0 {
		return fmt.Errorf("Ingressroute %s.%s update failed: no valid weights", apexName, canary.Namespace)
	}

	proxy, err := r.kapcomClient.KapcomV1beta1().IngressRoutes(canary.Namespace).Get(context.TODO(), apexName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("Ingressroute %s.%s query error: %w", apexName, canary.Namespace, err)
	}

	newRoutes := []v1beta1.Route{
		{
			PermitInsecure: true,
			Match:          r.makeExactPath(canary),
			TimeoutPolicy:  r.makeTimeoutPolicy(canary),
			RetryPolicy:    r.makeRetryPolicy(canary),
			Services: []v1beta1.Service{
				{
					Name:   primaryName,
					Port:   int(canary.Spec.Service.Port),
					Weight: primaryWeight,
					//HealthCheck: kr.makeHealthCheck(canary),
				},
				{
					Name:   canaryName,
					Port:   int(canary.Spec.Service.Port),
					Weight: canaryWeight,
					//HealthCheck: kr.makeHealthCheck(canary),
				},
			}},
	}

	proxy.Spec = v1beta1.IngressRouteSpec{
		Routes: newRoutes,
	}

	if len(canary.GetAnalysis().Match) > 0 {
		proxy.Spec = v1beta1.IngressRouteSpec{
			Routes: []v1beta1.Route{
				{
					PermitInsecure: true,
					Match:          r.makeExactPath(canary),
					TimeoutPolicy:  r.makeTimeoutPolicy(canary),
					RetryPolicy:    r.makeRetryPolicy(canary),
					Services: []v1beta1.Service{
						{
							Name:   primaryName,
							Port:   int(canary.Spec.Service.Port),
							Weight: primaryWeight,
							//HealthCheck: kr.makeHealthCheck(canary),
						},
						{
							Name:   canaryName,
							Port:   int(canary.Spec.Service.Port),
							Weight: canaryWeight,
							//HealthCheck: kr.makeHealthCheck(canary),
						},
					},
				},
				{
					PermitInsecure: true,
					Match:          r.makeExactPath(canary),
					TimeoutPolicy:  r.makeTimeoutPolicy(canary),
					RetryPolicy:    r.makeRetryPolicy(canary),
					Services: []v1beta1.Service{
						{
							Name:   primaryName,
							Port:   int(canary.Spec.Service.Port),
							Weight: 100,
							//HealthCheck: kr.makeHealthCheck(canary),
						},
						{
							Name:   canaryName,
							Port:   int(canary.Spec.Service.Port),
							Weight: 0,
							//HealthCheck: kr.makeHealthCheck(canary),
						},
					},
				},
			},
		}
	}

	payloadBytes, _ := json.Marshal(proxy)
	_, err = r.kapcomClient.KapcomV1beta1().IngressRoutes(canary.Namespace).Patch(context.TODO(), apexName, types.MergePatchType, payloadBytes, metav1.PatchOptions{}, "")
	if err != nil {
		return fmt.Errorf("Ingressroute %s.%s query error: %w", apexName, canary.Namespace, err)
	}

	svc, err := r.kubeClient.CoreV1().Services(canary.Namespace).Get(context.TODO(), canaryName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("Service  %s.%s fetch error: %w", apexName, canary.Namespace, err)
	}
	selectorMap := make(map[string]string)
	// Ensure Kapcom has upstream from both primary and canary sides to start with.
	if canary.Status.CanaryWeight == 0 && canary.Status.Phase == flaggerv1.CanaryPhaseProgressing {
		selectorMap = svc.Spec.Selector
	}
	for selector, _ := range svc.Spec.Selector {
		if strings.LastIndex(selector, "-common") > 0 {
			selectorMap[strings.ReplaceAll(selector, "-common", "")] =
				strings.ReplaceAll(svc.Spec.Selector[selector], "-common", "")
		}
	}
	if len(selectorMap) > 0 {
		svc.Spec.Selector = selectorMap
		//if updateMap{
		_, err = r.kubeClient.CoreV1().Services(canary.Namespace).Update(context.TODO(), svc, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("Service  %s.%s fetch error: %w", apexName, canary.Namespace, err)
		}
	}
	//_, err = kr.kapcomClient.KapcomV1beta1().IngressRoutes(canary.Namespace).Update(context.TODO(), proxy, metav1.UpdateOptions{})
	return nil
}

func (r *Reconciler) makeExactPath(canary *flaggerv1.Canary) string {
	prefix := "/"

	if len(canary.Spec.Service.Match) > 0 &&
		canary.Spec.Service.Match[0].Uri != nil &&
		canary.Spec.Service.Match[0].Uri.Prefix != "" {
		prefix = canary.Spec.Service.Match[0].Uri.Exact
	}

	return prefix
}

func (r *Reconciler) Finalize(_ *flaggerv1.Canary) error {
	return nil
}

func (r *Reconciler) makeTimeoutPolicy(canary *flaggerv1.Canary) *v1beta1.TimeoutPolicy {
	if canary.Spec.Service.Timeout != "" {
		return &v1beta1.TimeoutPolicy{
			Request: fmt.Sprintf("%s", canary.Spec.Service.Timeout),
		}
	}
	return nil
}

func (r *Reconciler) makeRetryPolicy(canary *flaggerv1.Canary) *v1beta1.RetryPolicy {
	if canary.Spec.Service.Retries != nil {
		return &v1beta1.RetryPolicy{
			NumRetries:    canary.Spec.Service.Retries.Attempts,
			PerTryTimeout: canary.Spec.Service.Retries.PerTryTimeout,
		}
	}
	return nil
}

func (r *Reconciler) makeHealthCheck(canary *flaggerv1.Canary) *v1beta1.HealthCheck {
	if canary.Spec.Service.Retries != nil {
		return &v1beta1.HealthCheck{
			Path:                    "/ping",
			IntervalSeconds:         60,
			TimeoutSeconds:          5,
			UnhealthyThresholdCount: 3,
			HealthyThresholdCount:   1,
		}
	}
	return nil
}
