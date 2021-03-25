package kapcom

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	extensionslisters "k8s.io/client-go/listers/extensions/v1beta1"
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	argoprojclientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Type holds this controller type
const Type = "Kapcom"

// ReconcilerConfig describes static configuration data for the nginx reconciler
type ReconcilerConfig struct {
	Rollout        *v1alpha1.Rollout
	Client         argoprojclientset.Interface
	Recorder       record.EventRecorder
	ControllerKind schema.GroupVersionKind
	IngressLister  extensionslisters.IngressLister
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

func (r *Reconciler) SetWeight(desiredWeight int32) error {
	ctx := context.TODO()
	stableIngressName := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress
	canaryIngressName := stableIngressName + "-base-canary"
	// canaryServiceName := r.cfg.Rollout.Spec.Strategy.Canary.CanaryService

	// Check if stable ingress exists (from lister, which has a cache), error if it does not
	_, err := r.cfg.IngressLister.Ingresses(r.cfg.Rollout.Namespace).Get(stableIngressName)
	if err != nil {
		r.log.WithField(logutil.IngressKey, stableIngressName).WithField("err", err.Error()).Error("error retrieving stableIngress")
		return fmt.Errorf("error retrieving stableIngress `%s` from cache: %v", stableIngressName, err)
	}
	// Check if canary ingress exists (from lister which has a cache), determines whether we later call Create() or Update()
	// canaryIngress, err := r.cfg.IngressLister.Ingresses(r.cfg.Rollout.Namespace).Get(canaryIngressName)
	canaryIngress, err := r.cfg.Client.ArgoprojV1alpha1().IngressRoutes(r.cfg.Rollout.Namespace).Get(ctx, canaryIngressName, metav1.GetOptions{})
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
	desiredCanaryIngress, err := r.canaryIngressRoute(desiredWeight)
	if err != nil {
		r.log.WithField(logutil.IngressKey, canaryIngressName).Error(err.Error())
		return err
	}

	if !canaryIngressExists {
		_, err = r.cfg.Client.ArgoprojV1alpha1().IngressRoutes(r.cfg.Rollout.Namespace).Create(ctx, desiredCanaryIngress, metav1.CreateOptions{})
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
		canaryIngress, err = r.cfg.Client.ArgoprojV1alpha1().IngressRoutes(r.cfg.Rollout.Namespace).Get(ctx, canaryIngressName, metav1.GetOptions{})
		if err != nil {
			r.log.WithField(logutil.IngressKey, canaryIngressName).Error(err.Error())
			return fmt.Errorf("error retrieving canary ingress `%s` from api: %v", canaryIngressName, err)
		}
	}
	current_canary_weight := canaryIngress.Spec.Routes[0].Services[1].Weight

	if current_canary_weight != int(desiredWeight) {
		r.cfg.Client.ArgoprojV1alpha1().IngressRoutes(r.cfg.Rollout.Namespace).Update(ctx, desiredCanaryIngress, metav1.UpdateOptions{})
	}

	return nil
}

// This method should create the new ingressroute for base, if not present.
// SetWeight modifies Nginx Ingress resources to reach desired state
func (r *Reconciler) canaryIngressRoute(desiredWeight int32) (*v1alpha1.IngressRoute, error) {
	canary := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.Kapcom
	stableIngressName := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.Kapcom.Ingress
	stableServiceName := r.cfg.Rollout.Spec.Strategy.Canary.StableService
	canaryServiceName := r.cfg.Rollout.Spec.Strategy.Canary.CanaryService

	canaryIngressName := stableIngressName + "-base-canary"

	// On the fqdn ingress basis this will be created
	// Check if stable ingress exists (from lister, which has a cache), error if it does not

	newSpec := v1alpha1.IngressRouteSpec{
		Routes: []v1alpha1.Route{
			{
				PermitInsecure: true,
				Match:          "/",
				Services: []v1alpha1.Service{
					{
						Name:   stableServiceName,
						Port:   int(canary.ServicePort),
						Weight: 100,
					},
					{
						Name:   canaryServiceName,
						Port:   int(canary.ServicePort),
						Weight: 0,
					},
				},
			},
		},
	}

	if desiredWeight > 0 {
		primary_weight := 100 - desiredWeight
		newSpec = v1alpha1.IngressRouteSpec{
			Routes: []v1alpha1.Route{
				{
					PermitInsecure: true,
					Match:          "/",
					Services: []v1alpha1.Service{
						{
							Name:   stableServiceName,
							Port:   int(canary.ServicePort),
							Weight: int(primary_weight),
						},
						{
							Name:   canaryServiceName,
							Port:   int(canary.ServicePort),
							Weight: int(desiredWeight),
						},
					},
				},
			},
		}
	}

	proxy, err := r.cfg.Client.ArgoprojV1alpha1().IngressRoutes(r.cfg.Rollout.Namespace).Get(context.TODO(), canaryIngressName, metav1.GetOptions{})
	m := make(map[string]string)
	m["annotation"] = "contour-corp"

	if errors.IsNotFound(err) {
		proxy = &v1alpha1.IngressRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      canaryIngressName,
				Namespace: r.cfg.Rollout.Namespace,
			},
			Spec: newSpec,
			Status: v1alpha1.Status{
				CurrentStatus: "valid",
				Description:   "valid IngressRoute",
			},
		}

		proxy.Annotations = m
	}
	return proxy, nil
}

func (r *Reconciler) VerifyWeight(desiredWeight int32) (bool, error) {
	return true, nil
}

// UpdateHash informs a traffic routing reconciler about new canary/stable pod hashes
func (r *Reconciler) UpdateHash(canaryHash, stableHash string) error {
	return nil
}
