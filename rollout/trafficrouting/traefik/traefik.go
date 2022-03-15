package traefik

import (
	"context"
	"strings"

	"github.com/pkg/errors"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Type holds this controller type
const Type = "Traefik"

var (
	apiGroupToResource = map[string]string{
		"traefik.containo.us": "traefikservices",
	}
)

type ReconcilerConfig struct {
	Rollout *v1alpha1.Rollout
	Client  ClientInterface
}

type Reconciler struct {
	Rollout *v1alpha1.Rollout
	Client  ClientInterface
}

type ClientInterface interface {
	Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error)
	Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error)
}

func NewReconciler(cfg ReconcilerConfig) *Reconciler {
	reconciler := &Reconciler{
		Rollout: cfg.Rollout,
		Client:  cfg.Client,
	}
	return reconciler
}

func NewDynamicClient(di dynamic.Interface, namespace string) dynamic.ResourceInterface {
	return di.Resource(GetMappingGVR()).Namespace(namespace)
}

func GetMappingGVR() schema.GroupVersionResource {
	group := defaults.DefaultTraefikAPIGroup
	parts := strings.Split(defaults.DefaultTraefikVersion, "/")
	version := parts[len(parts)-1]
	resourceName := apiGroupToResource[group]
	return schema.GroupVersionResource{
		Group:    group,
		Version:  version,
		Resource: resourceName,
	}
}

func (r *Reconciler) UpdateHash(canaryHash, stableHash string, additionalDestinations ...v1alpha1.WeightDestination) error {
	return nil
}

func (r *Reconciler) SetWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
	ctx := context.TODO()
	rollout := r.Rollout
	traefikServiceName := rollout.Spec.Strategy.Canary.TrafficRouting.Traefik.Service
	traefikService, err := r.Client.Get(ctx, traefikServiceName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	canaryServiceName := rollout.Spec.Strategy.Canary.CanaryService
	stableServiceName := rollout.Spec.Strategy.Canary.StableService
	services, isFound, err := unstructured.NestedSlice(traefikService.Object, "spec", "weighted", "services")
	if err != nil || !isFound {
		return err
	}
	canaryService, err := getService(canaryServiceName, services)
	if err != nil {
		return err
	}
	err = unstructured.SetNestedField(canaryService, int64(desiredWeight), "weight")
	if err != nil {
		return err
	}
	stableService, err := getService(stableServiceName, services)
	if err != nil {
		return err
	}
	err = unstructured.SetNestedField(stableService, int64(100-desiredWeight), "weight")
	if err != nil {
		return err
	}
	err = unstructured.SetNestedSlice(traefikService.Object, services, "spec", "weighted", "services")
	if err != nil {
		return err
	}
	_, err = r.Client.Update(ctx, traefikService, metav1.UpdateOptions{})
	return err
}

func getService(serviceName string, services []interface{}) (map[string]interface{}, error) {
	var selectedService map[string]interface{}
	for _, service := range services {
		typedService, ok := service.(map[string]interface{})
		if !ok {
			return nil, errors.New("Failed type assertion setting weight for traefik service")
		}
		nameOfCurrentService, isFound, err := unstructured.NestedString(typedService, "name")
		if err != nil || !isFound {
			return nil, err
		}
		if nameOfCurrentService == serviceName {
			selectedService = typedService
			break
		}
	}
	return selectedService, nil
}

func (r *Reconciler) VerifyWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error) {
	ctx := context.TODO()
	verifyingStatus := false
	rollout := r.Rollout
	traefikServiceName := rollout.Spec.Strategy.Canary.TrafficRouting.Traefik.Service
	traefikService, err := r.Client.Get(ctx, traefikServiceName, metav1.GetOptions{})
	if err != nil {
		return &verifyingStatus, err
	}
	canaryServiceName := rollout.Spec.Strategy.Canary.CanaryService
	stableServiceName := rollout.Spec.Strategy.Canary.StableService
	services, isFound, err := unstructured.NestedSlice(traefikService.Object, "spec", "weighted", "services")
	if err != nil || !isFound {
		return &verifyingStatus, err
	}
	canaryService, err := getService(canaryServiceName, services)
	if err != nil {
		return &verifyingStatus, err
	}
	weight, isFound, err := unstructured.NestedInt64(canaryService, "weight")
	verifyingStatus = weight == int64(desiredWeight)
	if err != nil || !isFound || !verifyingStatus {
		return &verifyingStatus, err
	}
	stableService, err := getService(stableServiceName, services)
	if err != nil {
		return &verifyingStatus, err
	}
	weight, isFound, err = unstructured.NestedInt64(stableService, "weight")
	verifyingStatus = weight == int64(100-desiredWeight)
	if err != nil || !isFound || !verifyingStatus {
		return &verifyingStatus, err
	}
	return &verifyingStatus, nil
}

func (r *Reconciler) Type() string {
	return Type
}
