package istio

import (
	"encoding/json"
	"fmt"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamiclister"
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

const Type = "Istio"

// NewReconciler returns a reconciler struct that brings the Virtual Service into the desired state
func NewReconciler(r *v1alpha1.Rollout, client dynamic.Interface, recorder record.EventRecorder, defaultAPIVersion string, istioVirtualServiceLister dynamiclister.Lister) *Reconciler {
	return &Reconciler{
		rollout: r,
		log:     logutil.WithRollout(r),

		client:                    client,
		recorder:                  recorder,
		defaultAPIVersion:         defaultAPIVersion,
		istioVirtualServiceLister: istioVirtualServiceLister,
	}
}

// GetRolloutVirtualServiceKeys gets the virtual service and its namespace from a rollout
func GetRolloutVirtualServiceKeys(rollout *v1alpha1.Rollout) []string {
	if rollout.Spec.Strategy.Canary == nil {
		return []string{}
	}
	if rollout.Spec.Strategy.Canary.TrafficRouting == nil {
		return []string{}
	}
	if rollout.Spec.Strategy.Canary.TrafficRouting.Istio == nil {
		return []string{}
	}
	if rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Name == "" {
		return []string{}
	}
	return []string{fmt.Sprintf("%s/%s", rollout.Namespace, rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Name)}
}

// Reconciler holds required fields to reconcile Istio resources
type Reconciler struct {
	rollout                   *v1alpha1.Rollout
	log                       *logrus.Entry
	client                    dynamic.Interface
	recorder                  record.EventRecorder
	defaultAPIVersion         string
	istioVirtualServiceLister dynamiclister.Lister
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

func (r *Reconciler) generateVirtualServicePatches(httpRoutes []HttpRoute, desiredWeight int64) virtualServicePatches {
	canarySvc := r.rollout.Spec.Strategy.Canary.CanaryService
	stableSvc := r.rollout.Spec.Strategy.Canary.StableService
	routes := map[string]bool{}
	for _, r := range r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Routes {
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
			if host == canarySvc && weight != desiredWeight {
				patch := virtualServicePatch{
					routeIndex:       i,
					destinationIndex: j,
					weight:           desiredWeight,
				}
				patches = append(patches, patch)
			}
			if host == stableSvc && weight != 100-desiredWeight {
				patch := virtualServicePatch{
					routeIndex:       i,
					destinationIndex: j,
					weight:           100 - desiredWeight,
				}
				patches = append(patches, patch)
			}
		}
	}
	return patches
}

func (r *Reconciler) reconcileVirtualService(obj *unstructured.Unstructured, desiredWeight int32) (*unstructured.Unstructured, bool, error) {
	newObj := obj.DeepCopy()
	httpRoutesI, err := GetHttpRoutesI(newObj)
	if err != nil {
		return nil, false, err
	}
	httpRoutes, err := GetHttpRoutes(newObj, httpRoutesI)
	if err != nil {
		return nil, false, err
	}

	if err := ValidateHTTPRoutes(r.rollout, httpRoutes); err != nil {
		return nil, false, err
	}

	patches := r.generateVirtualServicePatches(httpRoutes, int64(desiredWeight))
	patches.patchVirtualService(httpRoutesI)

	err = unstructured.SetNestedSlice(newObj.Object, httpRoutesI, "spec", "http")
	return newObj, len(patches) > 0, err
}

func GetHttpRoutesI(obj *unstructured.Unstructured) ([]interface{}, error) {
	httpRoutesI, notFound, err := unstructured.NestedSlice(obj.Object, "spec", "http")
	if !notFound {
		return nil, fmt.Errorf(".spec.http is not defined")
	}
	if err != nil {
		return nil, err
	}
	return httpRoutesI, nil
}

func GetHttpRoutes(obj *unstructured.Unstructured, httpRoutesI []interface{}) ([]HttpRoute, error) {
	routeBytes, err := json.Marshal(httpRoutesI)
	if err != nil {
		return nil, err
	}

	var httpRoutes []HttpRoute
	err = json.Unmarshal(routeBytes, &httpRoutes)
	if err != nil {
		return nil, err
	}

	return httpRoutes, nil
}

// Type indicates this reconciler is an Istio reconciler
func (r *Reconciler) Type() string {
	return Type
}

// Reconcile modifies Istio resources to reach desired state
func (r *Reconciler) Reconcile(desiredWeight int32) error {
	var vsvc *unstructured.Unstructured
	var err error
	vsvcName := r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Name
	client := r.client.Resource(istioutil.GetIstioGVR(r.defaultAPIVersion)).Namespace(r.rollout.Namespace)
	if r.istioVirtualServiceLister != nil {
		vsvc, err = r.istioVirtualServiceLister.Namespace(r.rollout.Namespace).Get(vsvcName)
	} else {
		vsvc, err = client.Get(vsvcName, metav1.GetOptions{})
	}
	if err != nil {
		if k8serrors.IsNotFound(err) {
			msg := fmt.Sprintf("Virtual Service `%s` not found", vsvcName)
			r.recorder.Event(r.rollout, corev1.EventTypeWarning, "VirtualServiceNotFound", msg)
		}
		return err
	}
	modifiedVsvc, modified, err := r.reconcileVirtualService(vsvc, desiredWeight)
	if err != nil {
		return err
	}
	if !modified {
		return nil
	}
	msg := fmt.Sprintf("Updating VirtualService `%s` to desiredWeight '%d'", vsvcName, desiredWeight)
	r.log.Info(msg)
	r.recorder.Event(r.rollout, corev1.EventTypeNormal, "UpdatingVirtualService", msg)
	_, err = client.Update(modifiedVsvc, metav1.UpdateOptions{})
	return err
}

// validateHTTPRoutes ensures that all the routes in the rollout exist and they only have two destinations
func ValidateHTTPRoutes(r *v1alpha1.Rollout, httpRoutes []HttpRoute) error {
	routes := r.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Routes
	stableSvc := r.Spec.Strategy.Canary.StableService
	canarySvc := r.Spec.Strategy.Canary.CanaryService

	routesPatched := map[string]bool{}
	for _, route := range routes {
		routesPatched[route] = false
	}

	for _, route := range httpRoutes {
		// check if the httpRoute is in the list of routes from the rollout
		if _, ok := routesPatched[route.Name]; ok {
			routesPatched[route.Name] = true
			err := validateHosts(route, stableSvc, canarySvc)
			if err != nil {
				return err
			}
		}
	}

	for i := range routesPatched {
		if !routesPatched[i] {
			return fmt.Errorf("Route '%s' is not found", i)
		}
	}

	return nil
}

// validateHosts ensures there are two destinations within a route and their hosts are the stable and canary service
func validateHosts(hr HttpRoute, stableSvc, canarySvc string) error {
	if len(hr.Route) != 2 {
		return fmt.Errorf("Route '%s' does not have exactly two routes", hr.Name)
	}
	hasStableSvc := false
	hasCanarySvc := false
	for _, r := range hr.Route {
		if r.Destination.Host == stableSvc {
			hasStableSvc = true
		}
		if r.Destination.Host == canarySvc {
			hasCanarySvc = true
		}
	}
	if !hasCanarySvc {
		return fmt.Errorf("Canary Service '%s' not found in route", canarySvc)
	}
	if !hasStableSvc {
		return fmt.Errorf("Stable Service '%s' not found in route", stableSvc)
	}
	return nil

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
type HttpRoute struct {
	Name  string  `json:"name,omitempty"`
	Route []route `json:"route,omitempty"`
}
