package apisix

import (
	"context"
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/record"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Type holds this controller type
const Type = "Apisix"

const apisixRouteUpdateError = "ApisixRouteUpdateError"

type ReconcilerConfig struct {
	Rollout  *v1alpha1.Rollout
	Client   ClientInterface
	Recorder record.EventRecorder
}

type Reconciler struct {
	Rollout  *v1alpha1.Rollout
	Client   ClientInterface
	Recorder record.EventRecorder
}

func (r *Reconciler) sendWarningEvent(id, msg string) {
	r.sendEvent(corev1.EventTypeWarning, id, msg)
}

func (r *Reconciler) sendEvent(eventType, id, msg string) {
	r.Recorder.Eventf(r.Rollout, record.EventOptions{EventType: eventType, EventReason: id}, msg)
}

type ClientInterface interface {
	Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error)
	Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error)
}

func NewReconciler(cfg *ReconcilerConfig) *Reconciler {
	reconciler := &Reconciler{
		Rollout:  cfg.Rollout,
		Client:   cfg.Client,
		Recorder: cfg.Recorder,
	}
	return reconciler
}

func (r *Reconciler) UpdateHash(canaryHash, stableHash string, additionalDestinations ...v1alpha1.WeightDestination) error {
	return nil
}

func (r *Reconciler) SetWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
	ctx := context.TODO()
	rollout := r.Rollout
	apisixRouteName := rollout.Spec.Strategy.Canary.TrafficRouting.Apisix.Route.Name
	apisixRoute, err := r.Client.Get(ctx, apisixRouteName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	httpRoutes, isFound, err := unstructured.NestedSlice(apisixRoute.Object, "spec", "http")
	if err != nil {
		return err
	}
	if !isFound {
		return errors.New("spec.http was not found in Apisix Route manifest")
	}
	rules := rollout.Spec.Strategy.Canary.TrafficRouting.Apisix.Route.Rules
	if rules == nil {
		rules = append(rules, apisixRouteName)
	}
	for _, ruleName := range rules {
		httpRoute, err := GetHttpRoute(httpRoutes, ruleName)
		if err != nil {
			return err
		}

		backends, err := GetBackends(httpRoute)
		if err != nil {
			return err
		}

		canaryBackendName := rollout.Spec.Strategy.Canary.CanaryService
		err = setBackendWeight(canaryBackendName, backends, int64(desiredWeight))
		if err != nil {
			return err
		}

		stableBackendName := rollout.Spec.Strategy.Canary.StableService
		err = setBackendWeight(stableBackendName, backends, int64(100-desiredWeight))
		if err != nil {
			return err
		}
	}

	err = unstructured.SetNestedSlice(apisixRoute.Object, httpRoutes, "spec", "http")
	if err != nil {
		return err
	}
	_, err = r.Client.Update(ctx, apisixRoute, metav1.UpdateOptions{})
	if err != nil {
		msg := fmt.Sprintf("Error updating apisix route %q: %s", apisixRoute.GetName(), err)
		r.sendWarningEvent(apisixRouteUpdateError, msg)
	}

	return err
}

func GetHttpRoute(routes []interface{}, ref string) (interface{}, error) {
	for _, route := range routes {
		typedRoute, ok := route.(map[string]interface{})
		if !ok {
			return nil, errors.New("Failed type assertion for Apisix http route")
		}
		rawName, ok := typedRoute["name"]
		if !ok {
			return nil, errors.New("Apisix http route rule name field not found")
		}
		typedName, ok := rawName.(string)
		if !ok {
			return nil, errors.New("Failed type assertion for Apisix http route rule name")
		}
		if typedName == ref {
			return route, nil
		}

	}
	return nil, errors.New(fmt.Sprintf("Apisix http route rule %s not found", ref))
}

func GetBackends(httpRoute interface{}) ([]interface{}, error) {
	typedHttpRoute, ok := httpRoute.(map[string]interface{})
	if !ok {
		return nil, errors.New("Failed type assertion for Apisix http route")
	}
	rawBackends, ok := typedHttpRoute["backends"]
	if !ok {
		return nil, errors.New("Apisix http route backends not found")
	}
	backends, ok := rawBackends.([]interface{})
	if !ok {
		return nil, errors.New("Failed type assertion for Apisix http route backends")
	}
	return backends, nil
}

func setBackendWeight(backendName string, backends []interface{}, weight int64) error {
	found := false
	for _, backend := range backends {
		typedBackend, ok := backend.(map[string]interface{})
		if !ok {
			return errors.New("Failed type assertion for Apisix http route backend")
		}
		nameOfCurrentBackend, isFound, err := unstructured.NestedString(typedBackend, "serviceName")
		if err != nil {
			return err
		}
		if !isFound {
			return errors.New("serviceName field was not found in backend")
		}
		if nameOfCurrentBackend == backendName {
			found = true
			typedBackend["weight"] = weight
			break
		}
	}

	if !found {
		return errors.New(fmt.Sprintf("apisix route %s backend was not found", backendName))
	}
	return nil
}

func (r *Reconciler) SetHeaderRoute(headerRouting *v1alpha1.SetHeaderRoute) error {
	return nil
}

func (r *Reconciler) VerifyWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error) {
	return nil, nil
}

func (r *Reconciler) Type() string {
	return Type
}

func (r *Reconciler) SetMirrorRoute(setMirrorRoute *v1alpha1.SetMirrorRoute) error {
	return nil
}

func (r *Reconciler) RemoveManagedRoutes() error {
	return nil
}
