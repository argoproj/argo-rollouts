package nginx

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
)

// Type holds this controller type
const Type = "Nginx"

// ReconcilerConfig describes static configuration data for the nginx reconciler
type ReconcilerConfig struct {
	Rollout        *v1alpha1.Rollout
	Client         kubernetes.Interface
	Recorder       record.EventRecorder
	ControllerKind schema.GroupVersionKind
	IngressWrapper IngressWrapper
}

type IngressWrapper interface {
	Get(ctx context.Context, namespace, name string, opts metav1.GetOptions) (*ingressutil.Ingress, error)
	GetCached(namespace, name string) (*ingressutil.Ingress, error)
	Patch(ctx context.Context, namespace, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*ingressutil.Ingress, error)
	Create(ctx context.Context, namespace string, ingress *ingressutil.Ingress, opts metav1.CreateOptions) (*ingressutil.Ingress, error)
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

func (r *Reconciler) buildCanaryIngress(stableIngress *networkingv1.Ingress, name string, desiredWeight int32) (*ingressutil.Ingress, error) {
	stableIngressName := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress
	stableServiceName := r.cfg.Rollout.Spec.Strategy.Canary.StableService
	canaryServiceName := r.cfg.Rollout.Spec.Strategy.Canary.CanaryService
	annotationPrefix := defaults.GetCanaryIngressAnnotationPrefixOrDefault(r.cfg.Rollout)

	desiredCanaryIngress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{},
		},
		Spec: networkingv1.IngressSpec{
			Rules: make([]networkingv1.IngressRule, 0), // We have no way of knowing yet how many rules there will be
		},
	}

	// Preserve TLS from stable ingress
	if stableIngress.Spec.TLS != nil {
		desiredCanaryIngress.Spec.TLS = make([]networkingv1.IngressTLS, len(stableIngress.Spec.TLS))
		for it := 0; it < len(stableIngress.Spec.TLS); it++ {
			stableIngress.Spec.TLS[it].DeepCopyInto(&desiredCanaryIngress.Spec.TLS[it])
		}
	}

	// Preserve ingressClassName from stable ingress
	if stableIngress.Spec.IngressClassName != nil {
		desiredCanaryIngress.Spec.IngressClassName = stableIngress.Spec.IngressClassName
	}

	// Must preserve ingress.class on canary ingress, no other annotations matter
	// See: https://kubernetes.github.io/ingress-nginx/user-guide/nginx-configuration/annotations/#canary
	if val, ok := stableIngress.Annotations["kubernetes.io/ingress.class"]; ok {
		desiredCanaryIngress.Annotations["kubernetes.io/ingress.class"] = val
	}

	// Ensure canaryIngress is owned by this Rollout for cleanup
	desiredCanaryIngress.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(r.cfg.Rollout, r.cfg.ControllerKind)})

	// Copy only the rules which reference the stableService from the stableIngress to the canaryIngress
	// and change service backend to canaryService. Rules **not** referencing the stableIngress will be ignored.
	for ir := 0; ir < len(stableIngress.Spec.Rules); ir++ {
		var hasStableServiceBackendRule bool
		ingressRule := stableIngress.Spec.Rules[ir].DeepCopy()

		// Update all backends pointing to the stableService to point to the canaryService now
		for ip := 0; ip < len(ingressRule.HTTP.Paths); ip++ {
			if ingressRule.HTTP.Paths[ip].Backend.Service.Name == stableServiceName {
				hasStableServiceBackendRule = true
				ingressRule.HTTP.Paths[ip].Backend.Service.Name = canaryServiceName
			}
		}

		// If this rule was using the specified stableService backend, append it to the canary Ingress spec
		if hasStableServiceBackendRule {
			desiredCanaryIngress.Spec.Rules = append(desiredCanaryIngress.Spec.Rules, *ingressRule)
		}
	}

	if len(desiredCanaryIngress.Spec.Rules) == 0 {
		return nil, fmt.Errorf("ingress `%s` has no rules using service %s backend", stableIngressName, stableServiceName)
	}

	// Process additional annotations, would commonly be things like `canary-by-header` or `load-balance`
	for k, v := range r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.AdditionalIngressAnnotations {
		if !strings.HasPrefix(k, annotationPrefix) {
			k = fmt.Sprintf("%s/%s", annotationPrefix, k)
		}
		desiredCanaryIngress.Annotations[k] = v
	}
	// Always set `canary` and `canary-weight` - `canary-by-header` and `canary-by-cookie`, if set,  will always take precedence
	desiredCanaryIngress.Annotations[fmt.Sprintf("%s/canary", annotationPrefix)] = "true"
	desiredCanaryIngress.Annotations[fmt.Sprintf("%s/canary-weight", annotationPrefix)] = fmt.Sprintf("%d", desiredWeight)

	return ingressutil.NewIngress(desiredCanaryIngress), nil
}

func (r *Reconciler) buildLegacyCanaryIngress(stableIngress *extensionsv1beta1.Ingress, name string, desiredWeight int32) (*ingressutil.Ingress, error) {
	stableIngressName := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress
	stableServiceName := r.cfg.Rollout.Spec.Strategy.Canary.StableService
	canaryServiceName := r.cfg.Rollout.Spec.Strategy.Canary.CanaryService
	annotationPrefix := defaults.GetCanaryIngressAnnotationPrefixOrDefault(r.cfg.Rollout)

	desiredCanaryIngress := &extensionsv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{},
		},
		Spec: extensionsv1beta1.IngressSpec{
			Rules: make([]extensionsv1beta1.IngressRule, 0), // We have no way of knowing yet how many rules there will be
		},
	}

	// Preserve TLS from stable ingress
	if stableIngress.Spec.TLS != nil {
		desiredCanaryIngress.Spec.TLS = make([]extensionsv1beta1.IngressTLS, len(stableIngress.Spec.TLS))
		for it := 0; it < len(stableIngress.Spec.TLS); it++ {
			stableIngress.Spec.TLS[it].DeepCopyInto(&desiredCanaryIngress.Spec.TLS[it])
		}
	}

	// Preserve ingressClassName from stable ingress
	if stableIngress.Spec.IngressClassName != nil {
		desiredCanaryIngress.Spec.IngressClassName = stableIngress.Spec.IngressClassName
	}

	// Must preserve ingress.class on canary ingress, no other annotations matter
	// See: https://kubernetes.github.io/ingress-nginx/user-guide/nginx-configuration/annotations/#canary
	if val, ok := stableIngress.Annotations["kubernetes.io/ingress.class"]; ok {
		desiredCanaryIngress.Annotations["kubernetes.io/ingress.class"] = val
	}

	// Ensure canaryIngress is owned by this Rollout for cleanup
	desiredCanaryIngress.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(r.cfg.Rollout, r.cfg.ControllerKind)})

	// Copy only the rules which reference the stableService from the stableIngress to the canaryIngress
	// and change service backend to canaryService. Rules **not** referencing the stableIngress will be ignored.
	for ir := 0; ir < len(stableIngress.Spec.Rules); ir++ {
		var hasStableServiceBackendRule bool
		ingressRule := stableIngress.Spec.Rules[ir].DeepCopy()

		// Update all backends pointing to the stableService to point to the canaryService now
		for ip := 0; ip < len(ingressRule.HTTP.Paths); ip++ {
			if ingressRule.HTTP.Paths[ip].Backend.ServiceName == stableServiceName {
				hasStableServiceBackendRule = true
				ingressRule.HTTP.Paths[ip].Backend.ServiceName = canaryServiceName
			}
		}

		// If this rule was using the specified stableService backend, append it to the canary Ingress spec
		if hasStableServiceBackendRule {
			desiredCanaryIngress.Spec.Rules = append(desiredCanaryIngress.Spec.Rules, *ingressRule)
		}
	}

	if len(desiredCanaryIngress.Spec.Rules) == 0 {
		return nil, fmt.Errorf("ingress `%s` has no rules using service %s backend", stableIngressName, stableServiceName)
	}

	// Process additional annotations, would commonly be things like `canary-by-header` or `load-balance`
	for k, v := range r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.AdditionalIngressAnnotations {
		if !strings.HasPrefix(k, annotationPrefix) {
			k = fmt.Sprintf("%s/%s", annotationPrefix, k)
		}
		desiredCanaryIngress.Annotations[k] = v
	}
	// Always set `canary` and `canary-weight` - `canary-by-header` and `canary-by-cookie`, if set,  will always take precedence
	desiredCanaryIngress.Annotations[fmt.Sprintf("%s/canary", annotationPrefix)] = "true"
	desiredCanaryIngress.Annotations[fmt.Sprintf("%s/canary-weight", annotationPrefix)] = fmt.Sprintf("%d", desiredWeight)

	return ingressutil.NewLegacyIngress(desiredCanaryIngress), nil
}

// canaryIngress returns the desired state of the canary ingress
func (r *Reconciler) canaryIngress(stableIngress *ingressutil.Ingress, name string, desiredWeight int32) (*ingressutil.Ingress, error) {
	switch stableIngress.Mode() {
	case ingressutil.IngressModeNetworking:
		networkingIngress, err := stableIngress.GetNetworkingIngress()
		if err != nil {
			return nil, err
		}
		return r.buildCanaryIngress(networkingIngress, name, desiredWeight)
	case ingressutil.IngressModeExtensions:
		extensionsIngress, err := stableIngress.GetExtensionsIngress()
		if err != nil {
			return nil, err
		}
		return r.buildLegacyCanaryIngress(extensionsIngress, name, desiredWeight)
	default:
		return nil, errors.New("undefined ingress mode")
	}
}

// SetWeight modifies Nginx Ingress resources to reach desired state
func (r *Reconciler) SetWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
	if ingresses := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngresses; ingresses != nil {
		return r.SetWeightPerIngress(desiredWeight, ingresses)
	} else {
		return r.SetWeightPerIngress(desiredWeight, []string{r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress})
	}
}

// SetWeightMultiIngress modifies each Nginx Ingress resource to reach desired state in the scenario of a rollout
// having multiple Ngnix Ingress resources.
func (r *Reconciler) SetWeightPerIngress(desiredWeight int32, ingresses []string) error {
	for _, ingress := range ingresses {
		ctx := context.TODO()
		stableIngressName := ingress
		canaryIngressName := ingressutil.GetCanaryIngressName(r.cfg.Rollout.GetName(), stableIngressName)

		// Check if stable ingress exists (from lister, which has a cache), error if it does not
		stableIngress, err := r.cfg.IngressWrapper.GetCached(r.cfg.Rollout.Namespace, stableIngressName)
		if err != nil {
			r.log.WithField(logutil.IngressKey, stableIngressName).WithField("err", err.Error()).Error("error retrieving stableIngress")
			return fmt.Errorf("error retrieving stableIngress `%s` from cache: %v", stableIngressName, err)
		}
		// Check if canary ingress exists (from lister which has a cache), determines whether we later call Create() or Update()
		canaryIngress, err := r.cfg.IngressWrapper.GetCached(r.cfg.Rollout.Namespace, canaryIngressName)

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
		desiredCanaryIngress, err := r.canaryIngress(stableIngress, canaryIngressName, desiredWeight)
		if err != nil {
			r.log.WithField(logutil.IngressKey, canaryIngressName).Error(err.Error())
			return err
		}

		if !canaryIngressExists {
			r.cfg.Recorder.Eventf(r.cfg.Rollout, record.EventOptions{EventReason: "CreatingCanaryIngress"}, "Creating canary ingress `%s` with weight `%d`", canaryIngressName, desiredWeight)
			_, err = r.cfg.IngressWrapper.Create(ctx, r.cfg.Rollout.Namespace, desiredCanaryIngress, metav1.CreateOptions{})
			if err == nil {
				continue
			}
			if !k8serrors.IsAlreadyExists(err) {
				r.log.WithField(logutil.IngressKey, canaryIngressName).WithField("err", err.Error()).Error("error creating canary ingress")
				return fmt.Errorf("error creating canary ingress `%s`: %v", canaryIngressName, err)
			}
			// Canary ingress was created by a different reconcile call before this one could complete (race)
			// This means we just read it from the API now (instead of cache) and continue with the normal
			// flow we take when the canary already existed.
			canaryIngress, err = r.cfg.IngressWrapper.Get(ctx, r.cfg.Rollout.Namespace, canaryIngressName, metav1.GetOptions{})
			if err != nil {
				r.log.WithField(logutil.IngressKey, canaryIngressName).Error(err.Error())
				return fmt.Errorf("error retrieving canary ingress `%s` from api: %v", canaryIngressName, err)
			}
		}

		// Canary Ingress already exists, apply a patch if needed

		// Only modify canaryIngress if it is controlled by this Rollout
		if !metav1.IsControlledBy(canaryIngress.GetObjectMeta(), r.cfg.Rollout) {
			r.log.WithField(logutil.IngressKey, canaryIngressName).Error("canary ingress controlled by different object")
			return fmt.Errorf("canary ingress `%s` controlled by different object", canaryIngressName)
		}

		// Make patches
		patch, modified, err := ingressutil.BuildIngressPatch(canaryIngress.Mode(), canaryIngress,
			desiredCanaryIngress, ingressutil.WithAnnotations(), ingressutil.WithLabels(), ingressutil.WithSpec())

		if err != nil {
			r.log.WithField(logutil.IngressKey, canaryIngressName).WithField("err", err.Error()).Error("error constructing canary ingress patch")
			return fmt.Errorf("error constructing canary ingress patch for `%s`: %v", canaryIngressName, err)
		}
		if !modified {
			r.log.WithField(logutil.IngressKey, canaryIngressName).Info("No changes to canary ingress - skipping patch")
			continue
		}

		r.log.WithField(logutil.IngressKey, canaryIngressName).WithField("patch", string(patch)).Debug("applying canary Ingress patch")
		r.log.WithField(logutil.IngressKey, canaryIngressName).WithField("desiredWeight", desiredWeight).Info("updating canary Ingress")
		r.cfg.Recorder.Eventf(r.cfg.Rollout, record.EventOptions{EventReason: "PatchingCanaryIngress"}, "Updating Ingress `%s` to desiredWeight '%d'", canaryIngressName, desiredWeight)

		_, err = r.cfg.IngressWrapper.Patch(ctx, r.cfg.Rollout.Namespace, canaryIngressName, types.MergePatchType, patch, metav1.PatchOptions{})
		if err != nil {
			r.log.WithField(logutil.IngressKey, canaryIngressName).WithField("err", err.Error()).Error("error patching canary ingress")
			return fmt.Errorf("error patching canary ingress `%s`: %v", canaryIngressName, err)
		}
	}

	return nil
}

func (r *Reconciler) SetHeaderRoute(headerRouting *v1alpha1.SetHeaderRoute) error {
	return nil
}

func (r *Reconciler) VerifyWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error) {
	return nil, nil
}

// UpdateHash informs a traffic routing reconciler about new canary/stable pod hashes
func (r *Reconciler) UpdateHash(canaryHash, stableHash string, additionalDestinations ...v1alpha1.WeightDestination) error {
	return nil
}

func (r *Reconciler) SetMirrorRoute(setMirrorRoute *v1alpha1.SetMirrorRoute) error {
	return nil
}

func (r *Reconciler) RemoveManagedRoutes() error {
	return nil
}
