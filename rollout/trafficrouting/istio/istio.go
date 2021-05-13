package istio

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"
	log "github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamiclister"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
)

const Type = "Istio"

// NewReconciler returns a reconciler struct that brings the Virtual Service into the desired state
func NewReconciler(r *v1alpha1.Rollout, client dynamic.Interface, recorder record.EventRecorder, virtualServiceLister, destinationRuleLister dynamiclister.Lister) *Reconciler {
	return &Reconciler{
		rollout: r,
		log:     logutil.WithRollout(r),

		client:                client,
		recorder:              recorder,
		virtualServiceLister:  virtualServiceLister,
		destinationRuleLister: destinationRuleLister,
	}
}

// Reconciler holds required fields to reconcile Istio resources
type Reconciler struct {
	rollout               *v1alpha1.Rollout
	log                   *log.Entry
	client                dynamic.Interface
	recorder              record.EventRecorder
	virtualServiceLister  dynamiclister.Lister
	destinationRuleLister dynamiclister.Lister
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

func (r *Reconciler) generateVirtualServicePatches(httpRoutes []VirtualServiceHTTPRoute, desiredWeight int64) virtualServicePatches {
	canarySvc := r.rollout.Spec.Strategy.Canary.CanaryService
	stableSvc := r.rollout.Spec.Strategy.Canary.StableService
	routes := map[string]bool{}
	for _, r := range r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Routes {
		routes[r] = true
	}
	canarySubset := ""
	stableSubset := ""
	if r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule != nil {
		canarySubset = r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule.CanarySubsetName
		stableSubset = r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule.StableSubsetName
	}

	patches := virtualServicePatches{}
	for i := range httpRoutes {
		route := httpRoutes[i]
		if !routes[route.Name] {
			continue
		}
		for j := range route.Route {
			destination := httpRoutes[i].Route[j]

			var host string
			if idx := strings.Index(destination.Destination.Host, "."); idx > 0 {
				host = destination.Destination.Host[:idx]
			} else if idx < 0 {
				host = destination.Destination.Host
			}

			subset := destination.Destination.Subset
			weight := destination.Weight
			if (host != "" && host == canarySvc) || (subset != "" && subset == canarySubset) {
				if weight != desiredWeight {
					patch := virtualServicePatch{
						routeIndex:       i,
						destinationIndex: j,
						weight:           desiredWeight,
					}
					patches = append(patches, patch)
				}
			}
			if (host != "" && host == stableSvc) || (subset != "" && subset == stableSubset) {
				if weight != 100-desiredWeight {
					patch := virtualServicePatch{
						routeIndex:       i,
						destinationIndex: j,
						weight:           100 - desiredWeight,
					}
					patches = append(patches, patch)
				}
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
	err = patches.patchVirtualService(httpRoutesI)
	if err != nil {
		return nil, false, err
	}

	err = unstructured.SetNestedSlice(newObj.Object, httpRoutesI, "spec", "http")
	return newObj, len(patches) > 0, err
}

func (r *Reconciler) UpdateHash(canaryHash, stableHash string) error {
	dRuleSpec := r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule
	if dRuleSpec == nil {
		return nil
	}
	ctx := context.TODO()
	client := r.client.Resource(istioutil.GetIstioDestinationRuleGVR()).Namespace(r.rollout.Namespace)

	var dRuleUn *unstructured.Unstructured
	var err error
	if r.destinationRuleLister != nil {
		dRuleUn, err = r.destinationRuleLister.Namespace(r.rollout.Namespace).Get(dRuleSpec.Name)
	} else {
		dRuleUn, err = client.Get(ctx, dRuleSpec.Name, metav1.GetOptions{})
	}

	if err != nil {
		if k8serrors.IsNotFound(err) {
			r.recorder.Warnf(r.rollout, record.EventOptions{EventReason: "DestinationRuleNotFound"}, "DestinationRule `%s` not found", dRuleSpec.Name)
		}
		return err
	}
	origBytes, dRule, dRuleNew, err := unstructuredToDestinationRules(dRuleUn)
	if err != nil {
		return err
	}
	if dRuleNew.Annotations == nil {
		dRuleNew.Annotations = make(map[string]string)
	}
	dRuleNew.Annotations[v1alpha1.ManagedByRolloutsKey] = r.rollout.Name
	for i, subset := range dRuleNew.Spec.Subsets {
		if subset.Name == dRuleSpec.CanarySubsetName {
			if subset.Labels == nil {
				subset.Labels = make(map[string]string)
			}
			if canaryHash != "" {
				subset.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] = canaryHash
			} else {
				delete(subset.Labels, v1alpha1.DefaultRolloutUniqueLabelKey)
			}
		} else if subset.Name == dRuleSpec.StableSubsetName {
			if subset.Labels == nil {
				subset.Labels = make(map[string]string)
			}
			if stableHash != "" {
				subset.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] = stableHash
			} else {
				delete(subset.Labels, v1alpha1.DefaultRolloutUniqueLabelKey)
			}
		}
		dRuleNew.Spec.Subsets[i] = subset
	}
	modified, err := updateDestinationRule(ctx, client, origBytes, dRule, dRuleNew)
	if err != nil {
		return err
	}
	if modified {
		r.recorder.Eventf(r.rollout, record.EventOptions{EventReason: "UpdatedDestinationRule"},
			"DestinationRule %s subset updated (%s: %s, %s: %s)", dRuleSpec.Name, dRuleSpec.CanarySubsetName, canaryHash, dRuleSpec.StableSubsetName, stableHash)
	}
	return nil
}

func updateDestinationRule(ctx context.Context, client dynamic.ResourceInterface, orig []byte, dRule, dRuleNew *DestinationRule) (bool, error) {
	dRuleBytes, err := json.Marshal(dRule)
	if err != nil {
		return false, err
	}
	dRuleNewBytes, err := json.Marshal(dRuleNew)
	if err != nil {
		return false, err
	}
	patch, err := jsonpatch.CreateMergePatch(dRuleBytes, dRuleNewBytes)
	if err != nil {
		return false, err
	}
	if string(patch) == "{}" {
		return false, nil
	}
	dRuleNewBytes, err = jsonpatch.MergePatch(orig, patch)
	if err != nil {
		return false, err
	}
	var newDRuleUn unstructured.Unstructured
	err = json.Unmarshal(dRuleNewBytes, &newDRuleUn.Object)
	if err != nil {
		return false, err
	}
	_, err = client.Update(ctx, &newDRuleUn, metav1.UpdateOptions{})
	if err != nil {
		return false, err
	}
	log.Infof("updated destinationrule: %s", string(patch))
	return true, nil
}

// unstructuredToDestinationRules is a helper which returns two instances of DestinationRule
// from an unstructured object. The two copies are used to calculate patches.
func unstructuredToDestinationRules(un *unstructured.Unstructured) ([]byte, *DestinationRule, *DestinationRule, error) {
	dRuleBytes, err := json.Marshal(un)
	if err != nil {
		return nil, nil, nil, err
	}
	dRule1, err := jsonBytesToDestinationRule(dRuleBytes)
	if err != nil {
		return nil, nil, nil, err
	}
	dRule2, err := jsonBytesToDestinationRule(dRuleBytes)
	if err != nil {
		return nil, nil, nil, err
	}
	return dRuleBytes, dRule1, dRule2, nil
}

func jsonBytesToDestinationRule(dRuleBytes []byte) (*DestinationRule, error) {
	var dRule DestinationRule
	err := json.Unmarshal(dRuleBytes, &dRule)
	if err != nil {
		return nil, err
	}
	return &dRule, nil
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

func GetHttpRoutes(obj *unstructured.Unstructured, httpRoutesI []interface{}) ([]VirtualServiceHTTPRoute, error) {
	routeBytes, err := json.Marshal(httpRoutesI)
	if err != nil {
		return nil, err
	}

	var httpRoutes []VirtualServiceHTTPRoute
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

// SetWeight modifies Istio resources to reach desired state
func (r *Reconciler) SetWeight(desiredWeight int32) error {
	ctx := context.TODO()
	var vsvc *unstructured.Unstructured
	var err error

	namespace, vsvcName := istioutil.GetVirtualServiceNamespaceName(r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Name)
	if namespace == "" {
		namespace = r.rollout.Namespace
	}
	client := r.client.Resource(istioutil.GetIstioVirtualServiceGVR()).Namespace(namespace)
	if r.virtualServiceLister != nil {
		vsvc, err = r.virtualServiceLister.Namespace(namespace).Get(vsvcName)
	} else {
		vsvc, err = client.Get(ctx, vsvcName, metav1.GetOptions{})
	}
	if err != nil {
		if k8serrors.IsNotFound(err) {
			r.recorder.Warnf(r.rollout, record.EventOptions{EventReason: "VirtualServiceNotFound"}, "VirtualService `%s` not found", vsvcName)
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
	_, err = client.Update(ctx, modifiedVsvc, metav1.UpdateOptions{})
	if err == nil {
		r.recorder.Eventf(r.rollout, record.EventOptions{EventReason: "UpdatedVirtualService"}, "VirtualService `%s` set to desiredWeight '%d'", vsvcName, desiredWeight)
	}
	return err
}

func (r *Reconciler) VerifyWeight(desiredWeight int32) (bool, error) {
	return true, nil
}

// validateHTTPRoutes ensures that all the routes in the rollout exist and they only have two destinations
func ValidateHTTPRoutes(r *v1alpha1.Rollout, httpRoutes []VirtualServiceHTTPRoute) error {
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
			err := validateVirtualServiceHTTPRouteDestinations(route, stableSvc, canarySvc, r.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule)
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

// validateVirtualServiceHTTPRouteDestinations ensures there are two destinations within a route and
// verifies that there is both a canary and a stable host or subset specified
func validateVirtualServiceHTTPRouteDestinations(hr VirtualServiceHTTPRoute, stableSvc, canarySvc string, dRule *v1alpha1.IstioDestinationRule) error {
	if len(hr.Route) != 2 {
		return fmt.Errorf("Route '%s' does not have exactly two routes", hr.Name)
	}
	hasStableSvc := false
	hasCanarySvc := false
	hasStableSubset := false
	hasCanarySubset := false
	for _, r := range hr.Route {
		host := ""
		if idx := strings.Index(r.Destination.Host, "."); idx > 0 {
			host = r.Destination.Host[:idx]
		} else if idx < 0 {
			host = r.Destination.Host
		}

		if stableSvc != "" && host == stableSvc {
			hasStableSvc = true
		}

		if canarySvc != "" && host == canarySvc {
			hasCanarySvc = true
		}
		if dRule != nil {
			if dRule.StableSubsetName != "" && r.Destination.Subset == dRule.StableSubsetName {
				hasStableSubset = true
			}
			if dRule.CanarySubsetName != "" && r.Destination.Subset == dRule.CanarySubsetName {
				hasCanarySubset = true
			}
		}
	}
	if dRule != nil {
		if !hasCanarySubset {
			return fmt.Errorf("Canary DestinationRule subset '%s' not found in route", dRule.CanarySubsetName)
		}
		if !hasStableSubset {
			return fmt.Errorf("Stable DestinationRule subset '%s' not found in route", dRule.StableSubsetName)
		}
	} else {
		if !hasCanarySvc {
			return fmt.Errorf("Canary Service '%s' not found in route", canarySvc)
		}
		if !hasStableSvc {
			return fmt.Errorf("Stable Service '%s' not found in route", stableSvc)
		}
	}
	return nil

}
