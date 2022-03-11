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
	Create(ctx context.Context, obj *unstructured.Unstructured, options metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error)
	Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error)
	Delete(ctx context.Context, name string, options metav1.DeleteOptions, subresources ...string) error
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
	for _, service := range services {
		typedService, ok := service.(map[string]interface{})
		if !ok {
			return errors.New("Failed type assertion setting weight for traefik service")
		}
		serviceName, isFound, err := unstructured.NestedString(typedService, "name")
		if err != nil || !isFound {
			return err
		}
		if serviceName == canaryServiceName {
			err := unstructured.SetNestedField(typedService, int64(desiredWeight), "weight")
			if err != nil {
				return err
			}
			continue
		}
		if serviceName == stableServiceName {
			err := unstructured.SetNestedField(typedService, int64(100-desiredWeight), "weight")
			if err != nil {
				return err
			}
			continue
		}
	}
	err = unstructured.SetNestedSlice(traefikService.Object, services, "spec", "weighted", "services")
	if err != nil {
		return err
	}
	_, err = r.Client.Update(ctx, traefikService, metav1.UpdateOptions{})
	return err
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
	for _, service := range services {
		typedService, ok := service.(map[string]interface{})
		if !ok {
			return &verifyingStatus, errors.New("Failed type assertion setting weight for traefik service")
		}
		serviceName, isFound, err := unstructured.NestedString(typedService, "name")
		if err != nil || !isFound {
			return &verifyingStatus, err
		}
		if serviceName == canaryServiceName {
			verifyingStatus = false
			weight, isFound, err := unstructured.NestedInt64(typedService, "weight")
			if err != nil || !isFound {
				return &verifyingStatus, err
			}
			verifyingStatus = weight == int64(desiredWeight)
			if !verifyingStatus {
				return &verifyingStatus, errors.New("Traefik service weight for canary service is not right")
			}
		}
		if serviceName == stableServiceName {
			verifyingStatus = false
			weight, isFound, err := unstructured.NestedInt64(typedService, "weight")
			if err != nil || !isFound {
				return &verifyingStatus, err
			}
			verifyingStatus = weight == int64(100-desiredWeight)
			if !verifyingStatus {
				return &verifyingStatus, errors.New("Traefik service weight for canary service is not right")
			}
		}
	}
	return &verifyingStatus, nil
}

func (r *Reconciler) Type() string {
	return Type
}
