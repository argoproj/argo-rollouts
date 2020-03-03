package nginx

import (
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/diff"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

const Type = "Nginx"

// NewReconciler returns a reconciler struct that brings the canary Ingress into the desired state
func NewReconciler(r *v1alpha1.Rollout, client kubernetes.Interface, recorder record.EventRecorder, controllerKind schema.GroupVersionKind) *Reconciler {
	return &Reconciler{
		rollout: r,
		log:     logutil.WithRollout(r),

		client:         client,
		recorder:       recorder,
		controllerKind: controllerKind,
	}
}

// Reconciler holds required fields to reconcile Nginx resources
type Reconciler struct {
	rollout           *v1alpha1.Rollout
	log               *logrus.Entry
	client            kubernetes.Interface
	recorder          record.EventRecorder
	controllerKind    schema.GroupVersionKind
	defaultAPIVersion string
}

// Type indicates this reconciler is an Istio reconciler
func (r *Reconciler) Type() string {
	return Type
}

// canaryIngress returns the desired state of the canary ingress
func (r *Reconciler) canaryIngress(stableIngress *extensionsv1beta1.Ingress, currentCanaryIngress *extensionsv1beta1.Ingress, desiredWeight int32) *extensionsv1beta1.Ingress {
	stableIngressName := r.rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress
	canaryIngressName := fmt.Sprintf("%s-canary", stableIngressName)
	stableServiceName := r.rollout.Spec.Strategy.Canary.StableService
	canaryServiceName := r.rollout.Spec.Strategy.Canary.CanaryService
	annotationPrefix := defaults.GetCanaryIngressAnnotationPrefixOrDefault(r.rollout)

	desiredCanaryIngress := stableIngress.DeepCopy()

	// Update ingress name
	desiredCanaryIngress.SetName(canaryIngressName)

	// Remove Argo CD instance label to avoid the canaryIngress being pruned by Argo CD
	// TODO: This will not work as intended if `application.instanceLabelKey` was changed from the
	// default value.
	delete(desiredCanaryIngress.Labels, "app.kubernetes.io/instance")

	// Delete other annotations we never want
	delete(desiredCanaryIngress.Annotations, "kubectl.kubernetes.io/last-applied-configuration")

	// Ensure canaryIngress is owned by this Rollout for cleanup
	desiredCanaryIngress.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(r.rollout, r.controllerKind)})

	// Change all references to the stable service to point to the canary service instead
	for ir := 0; ir < len(desiredCanaryIngress.Spec.Rules); ir++ {
		for ip := 0; ip < len(desiredCanaryIngress.Spec.Rules[ir].HTTP.Paths); ip++ {
			if desiredCanaryIngress.Spec.Rules[ir].HTTP.Paths[ip].Backend.ServiceName == stableServiceName {
				desiredCanaryIngress.Spec.Rules[ir].HTTP.Paths[ip].Backend.ServiceName = canaryServiceName
			}
		}
	}

	// Process additional annotations, prepend annotationPrefix unless supplied. We are keeping all the annotations
	// from the stableIngress since the controller automatically ignores most of them anyway:
	// See: https://kubernetes.github.io/ingress-nginx/user-guide/nginx-configuration/annotations/#canary
	for k, v := range r.rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.AdditionalIngressAnnotations {
		if !strings.HasPrefix(k, annotationPrefix) {
			k = fmt.Sprintf("%s/%s", annotationPrefix, k)
		}
		desiredCanaryIngress.Annotations[k] = v
	}
	// Always set `canary` and `canary-weight` - `canary-by-header` and `canary-by-cookie`, if set,  will always take precedence
	desiredCanaryIngress.Annotations[fmt.Sprintf("%s/canary", annotationPrefix)] = "true"
	desiredCanaryIngress.Annotations[fmt.Sprintf("%s/canary-weight", annotationPrefix)] = fmt.Sprintf("%d", desiredWeight)

	return desiredCanaryIngress
}

// compareCanaryIngresses compares the current canaryIngress with the desired one and returns a patch
func compareCanaryIngresses(current *extensionsv1beta1.Ingress, desired *extensionsv1beta1.Ingress) ([]byte, bool, error) {
	// only compare Spec, Annotations, and Labels
	return diff.CreateTwoWayMergePatch(
		&extensionsv1beta1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: current.Annotations,
				Labels:      current.Labels,
			},
			Spec: current.Spec,
		},
		&extensionsv1beta1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: desired.Annotations,
				Labels:      desired.Labels,
			},
			Spec: desired.Spec,
		}, extensionsv1beta1.Ingress{})
}

// Reconcile modifies Istio resources to reach desired state
func (r *Reconciler) Reconcile(desiredWeight int32) error {
	stableIngressName := r.rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress
	canaryIngressName := fmt.Sprintf("%s-canary", stableIngressName)

	// Check if stable ingress exists, error if it does not
	stableIngress, err := r.client.ExtensionsV1beta1().Ingresses(r.rollout.Namespace).Get(stableIngressName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			msg := fmt.Sprintf("Ingress `%s` not found", stableIngressName)
			r.recorder.Event(r.rollout, corev1.EventTypeWarning, "StableIngressNotFound", msg)
		}
		return err
	}

	// Check if canary ingress exists, determines whether we later call Create() or Update()
	canaryIngress, err := r.client.ExtensionsV1beta1().Ingresses(r.rollout.Namespace).Get(canaryIngressName, metav1.GetOptions{})

	canaryIngressExists := true
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			// An error other than "not found" occurred
			r.log.Errorf("Error retrieving canary ingress `%s`: %v", canaryIngressName, err)
			return err
		}
		r.log.Infof("Canary ingress `%s` not found, creating", canaryIngressName)
		canaryIngressExists = false
	}

	// Construct the desired canary Ingress resource
	desiredCanaryIngress := r.canaryIngress(stableIngress, canaryIngress, desiredWeight)

	if !canaryIngressExists {
		msg := fmt.Sprintf("Creating canary Ingress `%s` at desiredWeight '%d'", canaryIngressName, desiredWeight)
		r.log.Info(msg)
		r.recorder.Event(r.rollout, corev1.EventTypeNormal, "CreatingCanaryIngress", msg)
		// Remove fields which must never be sent on a Create()
		desiredCanaryIngress.SetResourceVersion("")
		desiredCanaryIngress.SetSelfLink("")
		desiredCanaryIngress.SetUID("")
		_, err = r.client.ExtensionsV1beta1().Ingresses(r.rollout.Namespace).Create(desiredCanaryIngress)
		if err != nil {
			msg := fmt.Sprintf("Cannot create or update canary ingress `%s`: %v", canaryIngressName, err)
			r.log.Error(msg)
			r.recorder.Event(r.rollout, corev1.EventTypeWarning, "CanaryIngressFailed", msg)
			return err
		}
		return nil
	}

	// Canary Ingress already exists, apply a patch if needed
	msg := fmt.Sprintf("Updating Ingress `%s` to desiredWeight '%d'", canaryIngressName, desiredWeight)
	r.log.Info(msg)
	r.recorder.Event(r.rollout, corev1.EventTypeNormal, "PatchingCanaryIngress", msg)

	// Make patches
	patch, modified, err := compareCanaryIngresses(canaryIngress, desiredCanaryIngress)

	if err != nil {
		msg := fmt.Sprintf("Error constructing canary ingress patch for `%s`: %v", canaryIngressName, err)
		r.log.Error(msg)
		r.recorder.Event(r.rollout, corev1.EventTypeWarning, "CanaryIngressPatchError", msg)
		return err
	}
	if !modified {
		r.log.Infof("No changes to canary ingress `%s` - skipping patch", canaryIngressName)
		return nil
	}
	_, err = r.client.ExtensionsV1beta1().Ingresses(r.rollout.Namespace).Patch(canaryIngressName, types.MergePatchType, patch)

	if err != nil {
		msg := fmt.Sprintf("Cannot patch canary ingress `%s`: %v", canaryIngressName, err)
		r.log.Error(msg)
		r.recorder.Event(r.rollout, corev1.EventTypeWarning, "CanaryIngressPatchError", msg)
		return err
	}

	return nil
}
