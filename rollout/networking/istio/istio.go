package istio

import (
	"encoding/json"
	"fmt"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

const Type = "Istio"

// NewReconciler returns a reconciler struct that brings the Virtual Service into the desired state
func NewReconciler(r *v1alpha1.Rollout, desiredWeight int32, client dynamic.Interface, recorder record.EventRecorder) *Reconciler {
	return &Reconciler{
		rollout:       r,
		log:           logutil.WithRollout(r),
		desiredWeight: desiredWeight,

		client:   client,
		recorder: recorder,
	}
}

// Reconciler holds required fields to reconcile Istio resources
type Reconciler struct {
	rollout       *v1alpha1.Rollout
	log           *logrus.Entry
	desiredWeight int32
	client        dynamic.Interface
	recorder      record.EventRecorder
}

type virtualServicePatch struct {
	routeIndex       int
	destinationIndex int
	weight           int64
}
type virtualServicePatches []virtualServicePatch

const (
	invalidCasting = "Invalid casting: field '%s' is not of type '%s'"
)

func (patches virtualServicePatches) patchVirtualService(httpRoutes []interface{}) error {
	for _, patch := range patches {
		route, ok := httpRoutes[patch.routeIndex].(map[string]interface{})
		if !ok {
			return fmt.Errorf(invalidCasting, "http[]", "map[string]interface")
		}
		destinations, ok := route["route"].([]interface{})
		if !ok {
			return fmt.Errorf(invalidCasting, "http[].route", "[]interface")
		}
		destination, ok := destinations[patch.destinationIndex].(map[string]interface{})
		if !ok {
			return fmt.Errorf(invalidCasting, "http[].route[].destination", "map[string]interface")
		}
		destination["weight"] = float64(patch.weight)

		destinations[patch.destinationIndex] = destination
		route["route"] = destinations
		httpRoutes[patch.routeIndex] = route
	}
	return nil
}

func (r *Reconciler) generateVirtualServicePatches(httpRoutes []httpRoute) virtualServicePatches {
	canarySvc := r.rollout.Spec.Strategy.Canary.CanaryService
	stableSvc := r.rollout.Spec.Strategy.Canary.StableService
	routes := map[string]bool{}
	for _, r := range r.rollout.Spec.Strategy.Canary.Networking.Istio.VirtualService.Routes {
		routes[r] = true
	}

	patches := virtualServicePatches{}
	for i := range httpRoutes {
		route := httpRoutes[i]
		if !routes[route.Name] {
			continue
		}
		for j := range route.Route {
			destination := httpRoutes[i].Route[j]
			host := destination.Destination.Host
			weight := destination.Weight
			if host == canarySvc && weight != int64(r.desiredWeight) {
				patch := virtualServicePatch{
					routeIndex:       i,
					destinationIndex: j,
					weight:           int64(r.desiredWeight),
				}
				patches = append(patches, patch)
			}
			if host == stableSvc && weight != int64(100-r.desiredWeight) {
				patch := virtualServicePatch{
					routeIndex:       i,
					destinationIndex: j,
					weight:           int64(100 - r.desiredWeight),
				}
				patches = append(patches, patch)
			}
		}
	}
	return patches
}

func (r *Reconciler) reconcileVirtualService(obj *unstructured.Unstructured) (*unstructured.Unstructured, bool, error) {
	newObj := obj.DeepCopy()
	httpRoutesI, notFound, err := unstructured.NestedSlice(newObj.Object, "spec", "http")
	if !notFound {
		return nil, false, fmt.Errorf(".spec.http is not defined")
	}
	if err != nil {
		return nil, false, err
	}
	routeBytes, err := json.Marshal(httpRoutesI)
	if err != nil {
		return nil, false, err
	}

	var httpRoutes []httpRoute
	err = json.Unmarshal(routeBytes, &httpRoutes)
	if err != nil {
		return nil, false, err
	}

	patches := r.generateVirtualServicePatches(httpRoutes)
	patches.patchVirtualService(httpRoutesI)

	err = unstructured.SetNestedSlice(newObj.Object, httpRoutesI, "spec", "http")
	return newObj, len(patches) > 0, err
}

// Type indicates this reconciler is an Istio reconciler
func (r *Reconciler) Type() string {
	return Type
}

// Reconcile modifies Istio resources to reach desired state
func (r *Reconciler) Reconcile() error {
	vsvcName := r.rollout.Spec.Strategy.Canary.Networking.Istio.VirtualService.Name

	//TODO(dthomson) is there a better way to get GVK
	gvk := schema.ParseGroupResource("virtualservices.networking.istio.io").WithVersion("v1alpha3")
	client := r.client.Resource(gvk).Namespace(r.rollout.Namespace)
	vsvc, err := client.Get(vsvcName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			msg := fmt.Sprintf("Virtual Service `%s` not found", vsvcName)
			r.recorder.Event(r.rollout, corev1.EventTypeWarning, "VirtualServiceNotFound", msg)
		}
		return err
	}
	modifiedVsvc, modifed, err := r.reconcileVirtualService(vsvc)
	if err != nil {
		return err
	}
	if !modifed {
		return nil
	}
	msg := fmt.Sprintf("Updating VirtualService `%s` to desiredWeight '%d'", vsvcName, r.desiredWeight)
	r.log.Info(msg)
	r.recorder.Event(r.rollout, corev1.EventTypeNormal, "UpdatingVirtualService", msg)
	_, err = client.Update(modifiedVsvc, metav1.UpdateOptions{})
	return err
}

// Structs below describe fields within Istio's VirtualService that the Rollout needs to modify

// Destination fields within the destination struct of the Virtual Service that the controller modifies
type destination struct {
	Host string `json:"host,omitempty"`
}

// route fields within the route struct of the Virtual Service that the controller modifies
type route struct {
	// Destination holds the destination struct of the virtual service
	Destination destination `json:"destination,omitempty"`
	// Weight holds the destination struct of the virtual service
	Weight int64 `json:"weight,omitempty"`
}

// httpRoute fields within the HTTP struct of the Virtual Service that the controller modifies
type httpRoute struct {
	Name  string  `json:"name,omitempty"`
	Route []route `json:"route,omitempty"`
}
