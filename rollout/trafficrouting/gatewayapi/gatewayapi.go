package gatewayapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/argoproj/argo-rollouts/utils/record"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Type holds this controller type
const Type = "GatewayAPI"

const httpRoutes = "httproutes"
const GatewayAPIUpdateError = "GatewayAPIUpdateError"

var (
	apiGroupToResource = map[string]string{
		defaults.DefaultGatewayAPIGroup: httpRoutes,
	}
)

type ReconcilerConfig struct {
	Rollout  *v1alpha1.Rollout
	Client   ClientInterface
	Recorder record.EventRecorder
}

type GatewayAPITrafficRouting struct {
	// HTTPRoute refers to the name of the HTTPRoute used to route traffic to the
	// service
	HTTPRoute string `json:"httpRoute" protobuf:"bytes,1,name=httpRoute"`
}

type Reconciler struct {
	Rollout  *v1alpha1.Rollout
	Client   ClientInterface
	Recorder record.EventRecorder
}

type ClientInterface interface {
	Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error)
	Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error)
}

func (r *Reconciler) sendWarningEvent(id, msg string) {
	r.sendEvent(corev1.EventTypeWarning, id, msg)
}

func (r *Reconciler) sendEvent(eventType, id, msg string) {
	r.Recorder.Eventf(r.Rollout, record.EventOptions{EventType: eventType, EventReason: id}, msg)
}

func NewDynamicClient(di dynamic.Interface, namespace string) dynamic.ResourceInterface {
	return di.Resource(GetMappingGVR()).Namespace(namespace)
}

func GetMappingGVR() schema.GroupVersionResource {
	return toMappingGVR(defaults.GetGatewayAPIGroupVersion())
}

func toMappingGVR(apiVersion string) schema.GroupVersionResource {
	parts := strings.Split(apiVersion, "/")
	group := parts[0]
	resourcename := apiGroupToResource[group]
	version := parts[len(parts)-1]
	return schema.GroupVersionResource{
		Group:    group,
		Version:  version,
		Resource: resourcename,
	}
}

func NewReconciler(cfg *ReconcilerConfig) *Reconciler {
	reconciler := Reconciler{
		Rollout:  cfg.Rollout,
		Client:   cfg.Client,
		Recorder: cfg.Recorder,
	}
	return &reconciler
}

func (p *Reconciler) NewTrafficRouterPlugin() pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *Reconciler) UpdateHash(ro *v1alpha1.Rollout, canaryHash, stableHash string, additionalDestinations []v1alpha1.WeightDestination) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *Reconciler) SetWeight(ro *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) pluginTypes.RpcError {
	ctx := context.TODO()
	gatewayAPIConfig := GatewayAPITrafficRouting{}
	err := json.Unmarshal(ro.Spec.Strategy.Canary.TrafficRouting.Plugin["gatewayAPI"], &gatewayAPIConfig)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}
	httpRouteName := gatewayAPIConfig.HTTPRoute
	httpRoute, err := r.Client.Get(ctx, httpRouteName, metav1.GetOptions{})
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}
	canaryServiceName := ro.Spec.Strategy.Canary.CanaryService
	stableServiceName := ro.Spec.Strategy.Canary.StableService
	rules, isFound, err := unstructured.NestedSlice(httpRoute.Object, "spec", "rules")
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}
	if !isFound {
		return pluginTypes.RpcError{
			ErrorString: errors.New("spec.rules field was not found in httpRoute").Error(),
		}
	}
	backendRefs, err := getBackendRefList(rules)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}
	canaryBackendRef, err := getBackendRef(canaryServiceName, backendRefs)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}
	err = unstructured.SetNestedField(canaryBackendRef, int64(desiredWeight), "weight")
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}
	stableBackendRef, err := getBackendRef(stableServiceName, backendRefs)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}
	err = unstructured.SetNestedField(stableBackendRef, int64(100-desiredWeight), "weight")
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}
	rules, err = mergeBackendRefs(rules, backendRefs)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}
	err = unstructured.SetNestedSlice(httpRoute.Object, rules, "spec", "rules")
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}
	_, err = r.Client.Update(ctx, httpRoute, metav1.UpdateOptions{})
	if err != nil {
		msg := fmt.Sprintf("Error updating Gateway API %q: %s", httpRoute.GetName(), err)
		r.sendWarningEvent(GatewayAPIUpdateError, msg)
	}
	return pluginTypes.RpcError{
		ErrorString: err.Error(),
	}
}

func getBackendRef(serviceName string, backendRefs []interface{}) (map[string]interface{}, error) {
	var selectedService map[string]interface{}
	for _, service := range backendRefs {
		typedService, ok := service.(map[string]interface{})
		if !ok {
			return nil, errors.New("Failed type assertion for gateway api service")
		}
		nameOfCurrentService, isFound, err := unstructured.NestedString(typedService, "name")
		if err != nil {
			return nil, err
		}
		if !isFound {
			continue
		}
		if nameOfCurrentService == serviceName {
			selectedService = typedService
			break
		}
	}
	if selectedService == nil {
		return nil, errors.New("service was not found in httpRoute")
	}
	return selectedService, nil
}

func getBackendRefList(rules []interface{}) ([]interface{}, error) {
	for _, rule := range rules {
		typedRule, ok := rule.(map[string]interface{})
		if !ok {
			return nil, errors.New("Failed type assertion setting rule for http route")
		}
		backendRefs, isFound, err := unstructured.NestedSlice(typedRule, "backendRefs")
		if err != nil {
			return nil, err
		}
		if !isFound {
			continue
		}
		return backendRefs, nil
	}
	return nil, errors.New("backendRefs was not found in httpRoute")
}

func mergeBackendRefs(rules, backendRefs []interface{}) ([]interface{}, error) {
	for _, rule := range rules {
		typedRule, ok := rule.(map[string]interface{})
		if !ok {
			return nil, errors.New("Failed type assertion setting rule for http route")
		}
		isFound, err := hasBackendRefs(typedRule)
		if err != nil {
			return nil, err
		}
		if !isFound {
			continue
		}
		err = unstructured.SetNestedSlice(typedRule, backendRefs, "backendRefs")
		if err != nil {
			return nil, err
		}
		return rules, nil
	}
	return rules, errors.New("backendRefs was not found and merged in rules")
}

func hasBackendRefs(typedRule map[string]interface{}) (bool, error) {
	_, isFound, err := unstructured.NestedSlice(typedRule, "backendRefs")
	return isFound, err
}

func (r *Reconciler) SetHeaderRoute(ro *v1alpha1.Rollout, headerRouting *v1alpha1.SetHeaderRoute) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *Reconciler) SetMirrorRoute(ro *v1alpha1.Rollout, setMirrorRoute *v1alpha1.SetMirrorRoute) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *Reconciler) VerifyWeight(ro *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) (*bool, pluginTypes.RpcError) {
	return nil, pluginTypes.RpcError{}
}

func (r *Reconciler) RemoveManagedRoutes(ro *v1alpha1.Rollout) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *Reconciler) Type() string {
	return Type
}
