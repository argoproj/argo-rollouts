package gatewayapi

import (
	"context"
	"errors"
	"strings"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/record"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Type holds this controller type
const Type = "GatewayAPI"
const httpRoutes = "httproutes"

var (
	apiGroupToResource = map[string]string{
		defaults.DefaultGatewayAPIGroup: httpRoutes,
	}
)

type ReconcilerConfig struct {
	Rollout  *v1alpha1.Rollout
	Client   ClientInterface
	Recorder *record.EventRecorder
}

type Reconciler struct {
	Rollout  *v1alpha1.Rollout
	Client   ClientInterface
	Recorder *record.EventRecorder
}

type ClientInterface interface {
	Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error)
	Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error)
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
		Rollout: cfg.Rollout,
		Client:  cfg.Client,
	}
	return &reconciler
}

func (r *Reconciler) UpdateHash(canaryHash, stableHash string, additionalDestinations ...v1alpha1.WeightDestination) error {
	return nil
}

func (r *Reconciler) SetWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
	/*
		1 Get HTTPRoute
		2 Change it
		3 Update it
	*/
	ctx := context.TODO()
	rollout := r.Rollout
	httpRouteName := rollout.Spec.Strategy.Canary.TrafficRouting.GatewayAPI.HTTPRoute
	httpRoute, err := r.Client.Get(ctx, httpRouteName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	// canaryServiceName := rollout.Spec.Strategy.Canary.CanaryService
	// stableServiceName := rollout.Spec.Strategy.Canary.StableService
	_, isFound, err := unstructured.NestedSlice(httpRoute.Object, "spec", "rules", "backendRefs")
	if err != nil {
		return err
	}
	if !isFound {
		return errors.New("spec.rules.backendRefs field was not found in httpRoute")
	}
	return nil
}

func getService(serviceName string, services []interface{}) (map[string]interface{}, error) {
	var selectedService map[string]interface{}
	for _, service := range services {
		typedService, ok := service.(map[string]interface{})
		if !ok {
			return nil, errors.New("Failed type assertion setting weight for traefik service")
		}
		nameOfCurrentService, isFound, err := unstructured.NestedString(typedService, "name")
		if err != nil {
			return nil, err
		}
		if !isFound {
			return nil, errors.New("name field was not found in service")
		}
		if nameOfCurrentService == serviceName {
			selectedService = typedService
			break
		}
	}
	return selectedService, nil
}

func (r *Reconciler) VerifyWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error) {
	return nil, nil
}

func (r *Reconciler) Type() string {
	return Type
}
