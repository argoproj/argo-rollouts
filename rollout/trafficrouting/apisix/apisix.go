package apisix

import (
	"context"
	"fmt"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/record"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var controllerKind = v1alpha1.SchemeGroupVersion.WithKind("Rollout")

// Type holds this controller type
const Type = "Apisix"

const apisixRouteUpdateError = "ApisixRouteUpdateError"
const apisixRouteCreateError = "ApisixRouteCreateError"
const apisixRouteDeleteError = "ApisixRouteDeleteError"
const failedToTypeAssertion = "Failed type assertion for Apisix http route"

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
	Create(ctx context.Context, obj *unstructured.Unstructured, options metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error)
	Delete(ctx context.Context, name string, options metav1.DeleteOptions, subresources ...string) error
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

	httpRoutes, err := r.processSetWeightRoutes(desiredWeight, apisixRoute, rollout, apisixRouteName)
	if err != nil {
		return err
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

func (r *Reconciler) processSetWeightRoutes(desiredWeight int32, apisixRoute *unstructured.Unstructured, rollout *v1alpha1.Rollout, apisixRouteName string) ([]interface{}, error) {
	httpRoutes, isFound, err := unstructured.NestedSlice(apisixRoute.Object, "spec", "http")
	if err != nil {
		return nil, err
	}
	if !isFound {
		return nil, errors.New("spec.http was not found in Apisix Route manifest")
	}
	rules := rollout.Spec.Strategy.Canary.TrafficRouting.Apisix.Route.Rules
	if rules == nil {
		rules = append(rules, apisixRouteName)
	}
	for _, ruleName := range rules {
		httpRoute, err := GetHttpRoute(httpRoutes, ruleName)
		if err != nil {
			return nil, err
		}

		backends, err := GetBackends(httpRoute)
		if err != nil {
			return nil, err
		}

		canaryBackendName := rollout.Spec.Strategy.Canary.CanaryService
		err = setBackendWeight(canaryBackendName, backends, int64(desiredWeight))
		if err != nil {
			return nil, err
		}

		stableBackendName := rollout.Spec.Strategy.Canary.StableService
		err = setBackendWeight(stableBackendName, backends, int64(100-desiredWeight))
		if err != nil {
			return nil, err
		}
	}
	return httpRoutes, nil
}

func GetHttpRoute(routes []interface{}, ref string) (interface{}, error) {
	for _, route := range routes {
		typedRoute, ok := route.(map[string]interface{})
		if !ok {
			return nil, errors.New(failedToTypeAssertion)
		}
		rawName, ok := typedRoute["name"]
		if !ok {
			return nil, errors.New("Apisix http route rule name field not found")
		}
		typedName, ok := rawName.(string)
		if !ok {
			return nil, errors.New(fmt.Sprintf("%s rule name", failedToTypeAssertion))
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
		return nil, errors.New(failedToTypeAssertion)
	}
	rawBackends, ok := typedHttpRoute["backends"]
	if !ok {
		return nil, errors.New("Apisix http route backends not found")
	}
	backends, ok := rawBackends.([]interface{})
	if !ok {
		return nil, errors.New(fmt.Sprintf("%s backends", failedToTypeAssertion))
	}
	return backends, nil
}

func setBackendWeight(backendName string, backends []interface{}, weight int64) error {
	found := false
	for _, backend := range backends {
		typedBackend, ok := backend.(map[string]interface{})
		if !ok {
			return errors.New(fmt.Sprintf("%s backends", failedToTypeAssertion))
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
	ctx := context.TODO()
	rollout := r.Rollout
	apisixRouteName := rollout.Spec.Strategy.Canary.TrafficRouting.Apisix.Route.Name
	apisixRoute, err := r.Client.Get(ctx, apisixRouteName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	setHeaderApisixRoute, isNew, err := r.makeSetHeaderRoute(ctx, headerRouting, apisixRoute)
	if !isNew && err != nil {
		return err
	}

	if headerRouting.Match == nil {
		if isNew {
			return nil
		} else {
			err = r.Client.Delete(ctx, headerRouting.Name, metav1.DeleteOptions{})
			if err != nil {
				msg := fmt.Sprintf("Error delete apisix route %q: %s", setHeaderApisixRoute.GetName(), err)
				r.sendWarningEvent(apisixRouteDeleteError, msg)
				return err
			}
			return nil
		}
	}

	err = r.processSetHeaderApisixRoute(headerRouting, setHeaderApisixRoute, isNew)
	if err != nil {
		return err
	}
	if isNew {
		_, err = r.Client.Create(ctx, setHeaderApisixRoute, metav1.CreateOptions{})
	} else {
		_, err = r.Client.Update(ctx, setHeaderApisixRoute, metav1.UpdateOptions{})
	}
	operate := "update"
	if isNew {
		operate = "create"
	}

	if err != nil {
		msg := fmt.Sprintf("Error %s apisix route %q: %s", operate, setHeaderApisixRoute.GetName(), err)
		if isNew {
			r.sendWarningEvent(apisixRouteCreateError, msg)
		} else {
			r.sendWarningEvent(apisixRouteUpdateError, msg)
		}

	}
	return err
}

func (r *Reconciler) processSetHeaderApisixRoute(headerRouting *v1alpha1.SetHeaderRoute, setHeaderApisixRoute *unstructured.Unstructured, isNew bool) error {
	httpRoutes, isFound, err := unstructured.NestedSlice(setHeaderApisixRoute.Object, "spec", "http")
	if err != nil {
		return err
	}
	if !isFound {
		return errors.New("spec.http was not found in Apisix Route manifest")
	}
	rules := r.Rollout.Spec.Strategy.Canary.TrafficRouting.Apisix.Route.Rules
	if rules == nil {
		rules = append(rules, r.Rollout.Spec.Strategy.Canary.TrafficRouting.Apisix.Route.Name)
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

		canaryBackendName := r.Rollout.Spec.Strategy.Canary.CanaryService
		err = setBackendWeight(canaryBackendName, backends, 100)
		if err != nil {
			return err
		}

		stableBackendName := r.Rollout.Spec.Strategy.Canary.StableService
		err = removeBackend(httpRoute, stableBackendName, backends)
		if err != nil {
			return err
		}
		if isNew {
			if err = processRulePriority(httpRoute); err != nil {
				return err
			}
		}

		if err = setApisixRuleMatch(httpRoute, headerRouting); err != nil {
			return err
		}
	}
	return unstructured.SetNestedSlice(setHeaderApisixRoute.Object, httpRoutes, "spec", "http")
}

func (r *Reconciler) makeSetHeaderRoute(ctx context.Context, headerRouting *v1alpha1.SetHeaderRoute, apisixRoute *unstructured.Unstructured) (*unstructured.Unstructured, bool, error) {
	setHeaderApisixRoute, err := r.Client.Get(ctx, headerRouting.Name, metav1.GetOptions{})
	isNew := false

	if err != nil {
		// create new ApisixRoute CR
		if k8serrors.IsNotFound(err) {
			setHeaderApisixRoute = apisixRoute.DeepCopy()
			setHeaderApisixRoute.SetName(headerRouting.Name)
			setHeaderApisixRoute.SetResourceVersion("")
			setHeaderApisixRoute.SetGeneration(0)
			setHeaderApisixRoute.SetUID("")
			setHeaderApisixRoute.SetCreationTimestamp(metav1.NewTime(time.Time{}))
			setHeaderApisixRoute.SetOwnerReferences([]metav1.OwnerReference{
				*metav1.NewControllerRef(r.Rollout, controllerKind),
			})
			isNew = true
		} else {
			return nil, false, err
		}
	} else {
		if !metav1.IsControlledBy(setHeaderApisixRoute, r.Rollout) {
			return nil, false, errors.New(fmt.Sprintf("duplicate ApisixRoute [%s] already exists", headerRouting.Name))
		}
	}
	return setHeaderApisixRoute, isNew, nil
}

func removeBackend(route interface{}, backendName string, backends []interface{}) error {
	typedRoute, ok := route.(map[string]interface{})
	if !ok {
		return errors.New("Failed type assertion for Apisix http route")
	}
	result := []interface{}{}
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
		if nameOfCurrentBackend != backendName {
			result = append(result, backend)
		}
	}
	return unstructured.SetNestedSlice(typedRoute, result, "backends")
}

func processRulePriority(route interface{}) error {
	typedRoute, ok := route.(map[string]interface{})
	if !ok {
		return errors.New("Failed type assertion for Apisix http route")
	}

	priority, ok, err := unstructured.NestedInt64(typedRoute, "priority")
	if err != nil {
		return err
	}
	if !ok {
		priority = 0
	}
	priority++
	typedRoute["priority"] = priority
	return nil
}

func setApisixRuleMatch(route interface{}, headerRouting *v1alpha1.SetHeaderRoute) error {
	typedRoute, ok := route.(map[string]interface{})
	if !ok {
		return errors.New("Failed type assertion for Apisix http route")
	}
	exprs := []interface{}{}
	for _, match := range headerRouting.Match {
		exprs = append(exprs, apisixExprs(match.HeaderName, match.HeaderValue.Exact, match.HeaderValue.Regex, match.HeaderValue.Prefix)...)
	}
	return unstructured.SetNestedSlice(typedRoute, exprs, "match", "exprs")
}

func apisixExprs(header, exact, regex, prefix string) []interface{} {
	subject := map[string]interface{}{
		"scope": "Header",
		"name":  header,
	}
	exprs := []interface{}{}
	if exact != "" {
		exprs = append(exprs, map[string]interface{}{
			"subject": subject,
			"op":      "Equal",
			"value":   exact,
		})
	}
	if regex != "" {
		exprs = append(exprs, map[string]interface{}{
			"subject": subject,
			"op":      "RegexMatch",
			"value":   regex,
		})
	}
	if prefix != "" {
		exprs = append(exprs, map[string]interface{}{
			"subject": subject,
			"op":      "RegexMatch",
			"value":   fmt.Sprintf("^%s.*", prefix),
		})
	}
	return exprs
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

	ctx := context.TODO()

	managedRoutes := r.Rollout.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes
	if len(managedRoutes) == 0 {
		return nil
	}

	for _, managedRoute := range managedRoutes {
		apisixRoute, err := r.Client.Get(ctx, managedRoute.Name, metav1.GetOptions{})
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return err
			} else {
				continue
			}
		}
		if metav1.IsControlledBy(apisixRoute, r.Rollout) {
			err = r.Client.Delete(ctx, managedRoute.Name, metav1.DeleteOptions{})
			if err != nil {
				msg := fmt.Sprintf("Error deleting apisix route %q: %s", apisixRoute.GetName(), err)
				r.sendWarningEvent(apisixRouteDeleteError, msg)
				return err
			}
		}
	}
	return nil
}
