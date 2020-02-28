package nginx

import (
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
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

// Reconcile modifies Istio resources to reach desired state
func (r *Reconciler) Reconcile(desiredWeight int32) error {
	stableIngressName := r.rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress
	canaryIngressName := fmt.Sprintf("%s-canary", stableIngressName)
	stableServiceName := r.rollout.Spec.Strategy.Canary.StableService
	canaryServiceName := r.rollout.Spec.Strategy.Canary.CanaryService
	annotationPrefix := defaults.GetCanaryIngressAnnotationPrefixOrDefault(r.rollout)

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
			r.log.Errorf("Error retrieving canary ingress `%s`: %s", canaryIngressName, err.Error())
			return err
		}
		r.log.Infof("Canary ingress `%s` not found, creating", canaryIngressName)
		canaryIngressExists = false
	}

	// Update the ingress on each reconcile since stableIngress might have changed
	if !canaryIngressExists {
		// If it didn't yet exist, copy from stable, rename, and zero out fields not permitted on Create()
		canaryIngress = stableIngress.DeepCopy()
		// Update ingress name
		canaryIngress.SetName(canaryIngressName)
		canaryIngress.SetResourceVersion("")
		canaryIngress.SetSelfLink("")
		canaryIngress.SetUID("")
	} else {
		// canaryIngress already exists, just copy spec and labels from stableIngress
		canaryIngress.Spec = stableIngress.Spec
		canaryIngress.SetLabels(stableIngress.GetLabels())
	}

	// Remove Argo CD instance label
	delete(canaryIngress.Labels, "app.kubernetes.io/instance")

	canaryIngress.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(r.rollout, r.controllerKind)})

	// Change all references to the stable service to point to the canary service instead
	for ir := 0; ir < len(canaryIngress.Spec.Rules); ir++ {
		for ip := 0; ip < len(canaryIngress.Spec.Rules[ir].HTTP.Paths); ip++ {
			if canaryIngress.Spec.Rules[ir].HTTP.Paths[ip].Backend.ServiceName == stableServiceName {
				canaryIngress.Spec.Rules[ir].HTTP.Paths[ip].Backend.ServiceName = canaryServiceName
			}
		}
	}

	// TODO handle annotationPrefix

	// No need to retain original annotations. See: https://kubernetes.github.io/ingress-nginx/user-guide/nginx-configuration/annotations/#canary
	canaryAnnotations := map[string]string{}

	// Process additional annotations, prepend annotationPrefix unless supplied
	for k, v := range r.rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.AdditionalIngressAnnotations {
		if !strings.HasPrefix(k, annotationPrefix) {
			k = fmt.Sprintf("%s/%s", annotationPrefix, k)
		}
		canaryAnnotations[k] = v
	}
	// Always set `canary` and `canary-weight` - `canary-by-header` and `canary-by-cookie` will always take precedence
	canaryAnnotations[fmt.Sprintf("%s/canary", annotationPrefix)] = "true"
	canaryAnnotations[fmt.Sprintf("%s/canary-weight", annotationPrefix)] = fmt.Sprintf("%d", desiredWeight)

	canaryIngress.SetAnnotations(canaryAnnotations)

	msg := fmt.Sprintf("Updating Ingress `%s` to desiredWeight '%d'", canaryIngressName, desiredWeight)
	r.log.Info(msg)
	r.recorder.Event(r.rollout, corev1.EventTypeNormal, "UpdatingCanaryIngress", msg)
	if !canaryIngressExists {
		_, err = r.client.ExtensionsV1beta1().Ingresses(r.rollout.Namespace).Create(canaryIngress)
	} else {
		_, err = r.client.ExtensionsV1beta1().Ingresses(r.rollout.Namespace).Update(canaryIngress)
	}
	if err != nil {
		msg := fmt.Sprintf("Cannot create or update canary ingress `%s`: %s", canaryIngressName, err.Error())
		r.log.Error(msg)
		r.recorder.Event(r.rollout, corev1.EventTypeWarning, "CanaryIngressFailed", msg)
		return err
	}

	return nil
}
