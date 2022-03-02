package traefik

import (
	"context"
	"strings"

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

type TraefikServicesField struct {
	Name string
	Weight int64
}

func NewReconciler(cfg ReconcilerConfig) *Reconciler {
	reconciler := Reconciler{
		Rollout: cfg.Rollout,
		Client:  cfg.Client,
	}
	return &reconciler
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
	services, isFound, err := unstructured.NestedSlice(traefikService.Object, "spec", "weighted", "services")
	if err != nil || !isFound {
		return err
	} 
	for _, rolloutService := range additionalDestinations {
		for _, service := range services {
			serviceField, _ := service.(TraefikServicesField)
			if serviceField.Name == rolloutService.ServiceName {
				serviceField.Weight = int64(desiredWeight)
				break
			}
		}
	}
	err = unstructured.SetNestedSlice(traefikService.Object, services, "spec", "weighted", "services")
	if err != nil {
		return err
	}
	return nil
}

func (r *Reconciler) VerifyWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error) {
	return nil, nil
}

func (r *Reconciler) Type() string {
	return Type
}
