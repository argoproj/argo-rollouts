package traefik

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/record"
)

// Type holds this controller type
const Type = "Traefik"

const traefikServices = "traefikservices"
const TraefikServiceUpdateError = "TraefikServiceUpdateError"

var (
	apiGroupToResource = map[string]string{
		defaults.DefaultTraefikAPIGroup: traefikServices,
	}
)

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
	traefikServiceName := rollout.Spec.Strategy.Canary.TrafficRouting.Traefik.WeightedTraefikServiceName
	traefikService, err := r.Client.Get(ctx, traefikServiceName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	canaryServiceName := rollout.Spec.Strategy.Canary.CanaryService
	stableServiceName := rollout.Spec.Strategy.Canary.StableService
	services, isFound, err := unstructured.NestedSlice(traefikService.Object, "spec", "weighted", "services")
	if err != nil {
		return err
	}
	if !isFound {
		return errors.New("spec.weighted.services was not found in traefik service manifest")
	}
	canaryService, err := getService(canaryServiceName, services)
	if err != nil {
		return err
	}
	if canaryService == nil {
		return errors.New("traefik canary service was not found")
	}
	err = unstructured.SetNestedField(canaryService, int64(desiredWeight), "weight")
	if err != nil {
		return err
	}
	stableService, err := getService(stableServiceName, services)
	if err != nil {
		return err
	}
	if stableService == nil {
		return errors.New("traefik stable service was not found")
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
	if err != nil {
		msg := fmt.Sprintf("Error updating traefik service %q: %s", traefikService.GetName(), err)
		r.sendWarningEvent(TraefikServiceUpdateError, msg)
	}
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
