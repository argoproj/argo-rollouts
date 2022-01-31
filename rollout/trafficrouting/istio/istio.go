package istio

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/mitchellh/mapstructure"
	log "github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamiclister"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	evalUtils "github.com/argoproj/argo-rollouts/utils/evaluate"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
)

const Http = "http"
const Tls = "tls"
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
	routeType        string
	destinationIndex int
	weight           int64
	host             string
	toDelete         bool
}

type virtualServicePatches []virtualServicePatch

type svcSubsets struct {
	canarySvc    string
	stableSvc    string
	canarySubset string
	stableSubset string
}

const (
	invalidCasting = "Invalid casting: field '%s' is not of type '%s'"
)

func (patches virtualServicePatches) patchVirtualService(httpRoutes []interface{}, tlsRoutes []interface{}) error {
	for _, patch := range patches {
		var route map[string]interface{}
		err := false
		if patch.routeType == Http {
			route, err = httpRoutes[patch.routeIndex].(map[string]interface{})
		} else if patch.routeType == Tls {
			route, err = tlsRoutes[patch.routeIndex].(map[string]interface{})
		}
		if !err {
			return fmt.Errorf(invalidCasting, patch.routeType+"[]", "map[string]interface")
		}
		destinations, ok := route["route"].([]interface{})
		if !ok {
			return fmt.Errorf(invalidCasting, patch.routeType+"[].route", "[]interface")
		}
		if patch.destinationIndex < len(destinations) {
			destination, ok := destinations[patch.destinationIndex].(map[string]interface{})
			if !ok {
				return fmt.Errorf(invalidCasting, patch.routeType+"[].route[].destination", "map[string]interface")
			}
			if patch.toDelete {
				destinations = append(destinations[:patch.destinationIndex], destinations[patch.destinationIndex+1:]...)
			} else {
				destination["weight"] = float64(patch.weight)
				destinations[patch.destinationIndex] = destination
			}
			route["route"] = destinations
		} else {
			destination := make(map[string]interface{}, 0)
			destination["weight"] = float64(patch.weight)
			destination["destination"] = map[string]interface{}{"host": patch.host}
			destinations = append(destinations, destination)
			route["route"] = destinations
		}
		if patch.routeType == Http {
			httpRoutes[patch.routeIndex] = route
		} else if patch.routeType == Tls {
			tlsRoutes[patch.routeIndex] = route
		}
	}
	return nil
}

func (r *Reconciler) generateVirtualServicePatches(rolloutVsvcRouteNames []string, httpRoutes []VirtualServiceHTTPRoute, rolloutVsvcTLSRoutes []v1alpha1.TLSRoute, tlsRoutes []VirtualServiceTLSRoute, desiredWeight int64, additionalDestinations ...v1alpha1.WeightDestination) virtualServicePatches {
	canarySvc := r.rollout.Spec.Strategy.Canary.CanaryService
	stableSvc := r.rollout.Spec.Strategy.Canary.StableService
	canarySubset := ""
	stableSubset := ""
	if r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule != nil {
		canarySubset = r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule.CanarySubsetName
		stableSubset = r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule.StableSubsetName
	}

	// err can be ignored because we already called ValidateHTTPRoutes earlier
	httpRouteIndexesToPatch, _ := getHttpRouteIndexesToPatch(rolloutVsvcRouteNames, httpRoutes)
	tlsRouteIndexesToPatch, _ := getTlsRouteIndexesToPatch(rolloutVsvcTLSRoutes, tlsRoutes)

	patches := virtualServicePatches{}
	svcSubsets := svcSubsets{
		canarySvc:    canarySvc,
		stableSvc:    stableSvc,
		canarySubset: canarySubset,
		stableSubset: stableSubset,
	}
	// Process HTTP Routes
	for _, routeIdx := range httpRouteIndexesToPatch {
		if len(httpRoutes) <= routeIdx {
			break
		}
		patches = processRoutes(Http, routeIdx, httpRoutes[routeIdx].Route, desiredWeight, svcSubsets, patches, additionalDestinations...)
	}
	// Process TLS Routes
	for _, routeIdx := range tlsRouteIndexesToPatch {
		if len(tlsRoutes) <= routeIdx {
			break
		}
		patches = processRoutes(Tls, routeIdx, tlsRoutes[routeIdx].Route, desiredWeight, svcSubsets, patches, additionalDestinations...)
	}
	return patches
}

func processRoutes(routeType string, routeIdx int, destinations []VirtualServiceRouteDestination, desiredWeight int64, svcSubsets svcSubsets, patches virtualServicePatches, additionalDestinations ...v1alpha1.WeightDestination) virtualServicePatches {
	svcToDest := map[string]v1alpha1.WeightDestination{}
	stableWeight := 100 - desiredWeight
	for _, dest := range additionalDestinations {
		svcToDest[dest.ServiceName] = dest
		stableWeight -= int64(dest.Weight)
	}
	for idx, destination := range destinations {
		host := getHost(destination)
		subset := destination.Destination.Subset
		weight := destination.Weight
		if host != "" {
			if host == svcSubsets.canarySvc || (subset != "" && subset == svcSubsets.canarySubset) {
				patches = appendPatch(routeIdx, routeType, weight, desiredWeight, idx, host, false, patches)
			} else if host == svcSubsets.stableSvc || (subset != "" && subset == svcSubsets.stableSubset) {
				patches = appendPatch(routeIdx, routeType, weight, stableWeight, idx, host, false, patches)
			} else if dest, ok := svcToDest[host]; ok { // Patch weight for existing experiment services
				patches = appendPatch(routeIdx, routeType, weight, int64(dest.Weight), idx, host, false, patches)
				delete(svcToDest, host)
			} else {
				patches = appendPatch(routeIdx, routeType, weight, 0, idx, host, true, patches)
			}
		}
	}
	// Add new destinations for experiment services which don't exist yet
	idx := len(destinations)
	for _, dest := range svcToDest {
		patches = appendPatch(routeIdx, routeType, 0, int64(dest.Weight), idx, dest.ServiceName, false, patches)
		idx += 1
	}
	return patches
}

func getHost(destination VirtualServiceRouteDestination) string {
	var host string
	if idx := strings.Index(destination.Destination.Host, "."); idx > 0 {
		host = destination.Destination.Host[:idx]
	} else if idx < 0 {
		host = destination.Destination.Host
	}
	return host
}

func appendPatch(routeIdx int, routeType string, weight int64, desiredWeight int64, destinationIndex int, host string, toDelete bool, patches virtualServicePatches) virtualServicePatches {
	if weight != desiredWeight {
		patch := virtualServicePatch{
			routeIndex:       routeIdx,
			routeType:        routeType,
			destinationIndex: destinationIndex,
			weight:           desiredWeight,
			host:             host,
			toDelete:         toDelete,
		}
		patches = append(patches, patch)
	}
	return patches
}

func (r *Reconciler) reconcileVirtualService(obj *unstructured.Unstructured, vsvcRouteNames []string, vsvcTLSRoutes []v1alpha1.TLSRoute, desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*unstructured.Unstructured, bool, error) {
	newObj := obj.DeepCopy()

	// HTTP Routes
	var httpRoutes []VirtualServiceHTTPRoute
	httpRoutesI, err := GetHttpRoutesI(newObj)
	if err == nil {
		routes, err := GetHttpRoutes(newObj, httpRoutesI)
		httpRoutes = routes
		if err != nil {
			return nil, false, err
		}
		if err := ValidateHTTPRoutes(r.rollout, vsvcRouteNames, httpRoutes); err != nil {
			return nil, false, err
		}
	}

	// TLS Routes
	var tlsRoutes []VirtualServiceTLSRoute
	tlsRoutesI, err := GetTlsRoutesI(newObj)
	if err == nil {
		routes, err := GetTlsRoutes(newObj, tlsRoutesI)
		tlsRoutes = routes
		if err != nil {
			return nil, false, err
		}
		if err := ValidateTlsRoutes(r.rollout, vsvcTLSRoutes, tlsRoutes); err != nil {
			return nil, false, err
		}
	}

	// Generate Patches
	patches := r.generateVirtualServicePatches(vsvcRouteNames, httpRoutes, vsvcTLSRoutes, tlsRoutes, int64(desiredWeight), additionalDestinations...)
	err = patches.patchVirtualService(httpRoutesI, tlsRoutesI)
	if err != nil {
		return nil, false, err
	}

	// Set HTTP Route Slice
	if len(httpRoutes) > 0 {
		err = unstructured.SetNestedSlice(newObj.Object, httpRoutesI, "spec", Http)
		if err != nil {
			return newObj, len(patches) > 0, err
		}
	}

	// Set TLS Route Slice
	if tlsRoutesI != nil {
		err = unstructured.SetNestedSlice(newObj.Object, tlsRoutesI, "spec", Tls)
	}
	return newObj, len(patches) > 0, err
}

func (r *Reconciler) UpdateHash(canaryHash, stableHash string, additionalDestinations ...v1alpha1.WeightDestination) error {
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
	// Maps service to WeightDestination object
	svcToDest := map[string]v1alpha1.WeightDestination{}
	for _, dest := range additionalDestinations {
		svcToDest[dest.ServiceName] = dest
	}
	tmp := make([]Subset, 0)
	for _, subset := range dRuleNew.Spec.Subsets {
		if subset.Name == dRuleSpec.CanarySubsetName { // Canary Subset
			if subset.Labels == nil {
				subset.Labels = make(map[string]string)
			}
			if canaryHash != "" {
				subset.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] = canaryHash
			} else {
				delete(subset.Labels, v1alpha1.DefaultRolloutUniqueLabelKey)
			}
		} else if subset.Name == dRuleSpec.StableSubsetName { // Stable Subset
			if subset.Labels == nil {
				subset.Labels = make(map[string]string)
			}
			if stableHash != "" {
				subset.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] = stableHash
			} else {
				delete(subset.Labels, v1alpha1.DefaultRolloutUniqueLabelKey)
			}
		} else if dest, ok := svcToDest[subset.Name]; ok { // Current experiment steps
			if dest.PodTemplateHash != "" {
				subset.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] = dest.PodTemplateHash
				delete(svcToDest, subset.Name)
			} else {
				delete(subset.Labels, v1alpha1.DefaultRolloutUniqueLabelKey)
			}
		} else {
			continue // Ignore any extraneous subsets (not stable, canary, or additionalDestination)
		}
		tmp = append(tmp, subset)
	}
	dRuleNew.Spec.Subsets = tmp
	// Add new subsets for experiment services if they don't exist yet
	for _, dest := range svcToDest {
		dRuleNew.Spec.Subsets = append(dRuleNew.Spec.Subsets, Subset{
			Name:   dest.ServiceName,
			Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: dest.PodTemplateHash},
		})
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

// destinationRuleReplaceExtraMarshal relace the key of "Extra" with the actual content
// e.g., "trafficpolicy" and return the bytes of the new object
func destinationRuleReplaceExtraMarshal(dRule *DestinationRule) []byte {
	dRuleNew := map[string]interface{}{}
	dRuleNew["metadata"] = dRule.ObjectMeta.DeepCopy()

	subsets := []map[string]interface{}{}
	for _, subset := range dRule.Spec.Subsets {
		newsubset := map[string]interface{}{}
		newsubset["name"] = subset.Name
		newsubset["labels"] = subset.Labels

		if subset.Extra == nil {
			subsets = append(subsets, newsubset)
			continue
		}

		extra := map[string]interface{}{}
		inputbyte, _ := json.Marshal(subset.Extra)
		json.Unmarshal(inputbyte, &extra)

		subset.Extra = nil
		for k, v := range extra {
			newsubset[k] = v
		}
		subsets = append(subsets, newsubset)
	}
	dRuleNew["spec"] = map[string]interface{}{
		"subsets": subsets,
	}

	dRuleNewBytes, _ := json.Marshal(dRuleNew)
	return dRuleNewBytes
}

func updateDestinationRule(ctx context.Context, client dynamic.ResourceInterface, orig []byte, dRule, dRuleNew *DestinationRule) (bool, error) {
	dRuleBytes, err := json.Marshal(dRule)
	if err != nil {
		return false, err
	}
	dRuleNewBytes := destinationRuleReplaceExtraMarshal(dRuleNew)
	log.Debugf("dRuleNewBytes: %s", string(dRuleNewBytes))

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
	log.Infof("updating destinationrule: %s", string(patch))
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

func unMarshalSubsets(dRule *DestinationRule, dRuleBytes []byte) error {
	var err error

	unstructured := map[string]interface{}{}
	var extractFieldBytes func([]byte, string) ([]byte, error)
	extractFieldBytes = func(input []byte, name string) ([]byte, error) {
		err = json.Unmarshal(input, &unstructured)
		if err != nil {
			return nil, err
		}
		fieldBytes, err := json.Marshal(unstructured[name])
		if err != nil {
			return nil, err
		}
		return fieldBytes, nil
	}

	specBytes, err := extractFieldBytes(dRuleBytes, "spec")
	if err != nil {
		return err
	}

	subsetsBytes, err := extractFieldBytes(specBytes, "subsets")
	if err != nil {
		return err
	}

	subsetsMap := []map[string]interface{}{}
	err = json.Unmarshal(subsetsBytes, &subsetsMap)
	if err != nil {
		return err
	}

	dRule.Spec.Subsets = []Subset{}
	for _, si := range subsetsMap {
		var subset Subset

		jsonInput, _ := json.Marshal(si)
		extra, err := UnmarshalJson(jsonInput, &subset)
		if err != nil {
			return err
		}

		subset.Extra = extra
		if len(subset.Extra) == 0 {
			subset.Extra = nil
		}
		dRule.Spec.Subsets = append(dRule.Spec.Subsets, subset)
	}
	return nil
}

func UnmarshalJson(input []byte, result interface{}) (map[string]interface{}, error) {
	// unmarshal json to a map
	foomap := make(map[string]interface{})
	json.Unmarshal(input, &foomap)

	// create a mapstructure decoder
	var md mapstructure.Metadata
	decoder, err := mapstructure.NewDecoder(
		&mapstructure.DecoderConfig{
			Metadata: &md,
			Result:   result,
		})
	if err != nil {
		return nil, err
	}

	// decode the unmarshalled map into the given struct
	if err := decoder.Decode(foomap); err != nil {
		return nil, err
	}

	// copy and return unused fields
	unused := map[string]interface{}{}
	for _, k := range md.Unused {
		unused[k] = foomap[k]
	}
	return unused, nil
}

func jsonBytesToDestinationRule(dRuleBytes []byte) (*DestinationRule, error) {
	var dRule DestinationRule
	err := json.Unmarshal(dRuleBytes, &dRule)
	if err != nil {
		return nil, err
	}
	err = unMarshalSubsets(&dRule, dRuleBytes)
	if err != nil {
		return nil, err
	}
	return &dRule, nil
}

func GetHttpRoutesI(obj *unstructured.Unstructured) ([]interface{}, error) {
	httpRoutesI, notFound, err := unstructured.NestedSlice(obj.Object, "spec", Http)
	if !notFound {
		return nil, fmt.Errorf(".spec.http is not defined")
	}
	if err != nil {
		return nil, err
	}
	return httpRoutesI, nil
}

func GetTlsRoutesI(obj *unstructured.Unstructured) ([]interface{}, error) {
	tlsRoutesI, notFound, err := unstructured.NestedSlice(obj.Object, "spec", Tls)
	if !notFound {
		return nil, fmt.Errorf(".spec.tls is not defined")
	}
	if err != nil {
		return nil, err
	}
	return tlsRoutesI, nil
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

func GetTlsRoutes(obj *unstructured.Unstructured, tlsRoutesI []interface{}) ([]VirtualServiceTLSRoute, error) {
	routeBytes, err := json.Marshal(tlsRoutesI)
	if err != nil {
		return nil, err
	}

	var tlsRoutes []VirtualServiceTLSRoute
	err = json.Unmarshal(routeBytes, &tlsRoutes)
	if err != nil {
		return nil, err
	}

	return tlsRoutes, nil
}

// Type indicates this reconciler is an Istio reconciler
func (r *Reconciler) Type() string {
	return Type
}

// SetWeight modifies Istio resources to reach desired state
func (r *Reconciler) SetWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
	ctx := context.TODO()
	var vsvc *unstructured.Unstructured
	var virtualServices []v1alpha1.IstioVirtualService

	if istioutil.MultipleVirtualServiceConfigured(r.rollout) {
		virtualServices = r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualServices
	} else {
		virtualServices = []v1alpha1.IstioVirtualService{*r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService}
	}

	for _, virtualService := range virtualServices {
		name := virtualService.Name
		namespace, vsvcName := istioutil.GetVirtualServiceNamespaceName(name)
		if namespace == "" {
			namespace = r.rollout.Namespace
		}

		var err error
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
		modifiedVirtualService, modified, err := r.reconcileVirtualService(vsvc, virtualService.Routes, virtualService.TLSRoutes, desiredWeight, additionalDestinations...)
		if err != nil {
			return err
		}
		if !modified {
			continue
		}
		_, err = client.Update(ctx, modifiedVirtualService, metav1.UpdateOptions{})
		if err == nil {
			r.log.Debugf("Updated VirtualService: %s", modifiedVirtualService)
			r.recorder.Eventf(r.rollout, record.EventOptions{EventReason: "Updated VirtualService"}, "VirtualService `%s` set to desiredWeight '%d'", vsvcName, desiredWeight)
		} else {
			return err
		}
	}
	return nil
}

func (r *Reconciler) VerifyWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error) {
	return nil, nil
}

// getHttpRouteIndexesToPatch returns array indices of the httpRoutes which need to be patched when updating weights
func getHttpRouteIndexesToPatch(routeNames []string, httpRoutes []VirtualServiceHTTPRoute) ([]int, error) {
	if len(routeNames) == 0 {
		return []int{0}, nil
	}

	var routeIndexesToPatch []int
	for _, routeName := range routeNames {
		routeIndex := searchHttpRoute(routeName, httpRoutes)
		if routeIndex > -1 {
			routeIndexesToPatch = append(routeIndexesToPatch, routeIndex)
		} else {
			return nil, fmt.Errorf("HTTP Route '%s' is not found in the defined Virtual Service.", routeName)
		}
	}
	return routeIndexesToPatch, nil
}

func searchHttpRoute(routeName string, httpRoutes []VirtualServiceHTTPRoute) int {
	routeIndex := -1
	for i, route := range httpRoutes {
		if route.Name == routeName {
			routeIndex = i
			break
		}
	}
	return routeIndex
}

// getTlsRouteIndexesToPatch returns array indices of the tlsRoutes which need to be patched when updating weights
func getTlsRouteIndexesToPatch(tlsRoutes []v1alpha1.TLSRoute, istioTlsRoutes []VirtualServiceTLSRoute) ([]int, error) {
	if len(tlsRoutes) == 0 {
		return []int{0}, nil
	}

	var routeIndexesToPatch []int
	for _, tlsRoute := range tlsRoutes {
		routeIndices := searchTlsRoute(tlsRoute, istioTlsRoutes)
		if len(routeIndices) > 0 {
			for _, routeIndex := range routeIndices {
				routeIndexesToPatch = append(routeIndexesToPatch, routeIndex)
			}
		} else {
			return nil, fmt.Errorf("No matching TLS routes found in the defined Virtual Service.")
		}
	}
	return routeIndexesToPatch, nil
}

func searchTlsRoute(tlsRoute v1alpha1.TLSRoute, istioTlsRoutes []VirtualServiceTLSRoute) []int {
	routeIndices := []int{}
	for i, route := range istioTlsRoutes {
		portsMap := make(map[int64]bool)
		sniHostsMap := make(map[string]bool)
		for _, routeMatch := range route.Match {
			portsMap[routeMatch.Port] = true
			for _, sniHost := range routeMatch.SNI {
				sniHostsMap[sniHost] = true
			}
		}
		// If there are multiple ports defined then this rules is never gonna match.
		if len(portsMap) > 1 {
			continue
		}
		// Extract the first port number from the `portsMap` if it has more than
		// zero ports in it.
		var port int64 = 0
		for portNumber := range portsMap {
			port = portNumber
		}
		sniHosts := []string{}
		for sniHostName := range sniHostsMap {
			sniHosts = append(sniHosts, sniHostName)
		}
		// To find a match for TLS Routes in Istio VS, we'll have to verify that:
		// 1. There is exactly one port present in the `ports`;
		// 2. The single port in `ports` matches with the `tlsRoute.Port`;
		// 3. All the SNI hosts from a single match block in the VirtualService,
		//    matches exactly with what the user have defined in `tlsRoute.SNIHosts`
		if port == tlsRoute.Port && evalUtils.Equal(tlsRoute.SNIHosts, sniHosts) {
			routeIndices = append(routeIndices, i)
		}
	}
	return routeIndices
}

// ValidateHTTPRoutes ensures that all the routes in the rollout exist
func ValidateHTTPRoutes(r *v1alpha1.Rollout, routeNames []string, httpRoutes []VirtualServiceHTTPRoute) error {
	stableSvc := r.Spec.Strategy.Canary.StableService
	canarySvc := r.Spec.Strategy.Canary.CanaryService

	routeIndexesToPatch, err := getHttpRouteIndexesToPatch(routeNames, httpRoutes)
	if err != nil {
		return err
	}
	for _, routeIndex := range routeIndexesToPatch {
		route := httpRoutes[routeIndex]
		err := validateVirtualServiceRouteDestinations(route.Route, stableSvc, canarySvc, r.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule)
		if err != nil {
			return err
		}
	}
	if len(routeNames) == 0 && len(httpRoutes) > 1 {
		return fmt.Errorf("spec.http[] should be set in VirtualService and it must have exactly one route when omitting spec.strategy.canary.trafficRouting.istio.virtualService.routes")
	}
	return nil
}

// ValidateTlsRoutes ensures that all the routes in the rollout exist and they only have two destinations
func ValidateTlsRoutes(r *v1alpha1.Rollout, vsvcTLSRoutes []v1alpha1.TLSRoute, tlsRoutes []VirtualServiceTLSRoute) error {
	stableSvc := r.Spec.Strategy.Canary.StableService
	canarySvc := r.Spec.Strategy.Canary.CanaryService

	routeIndexesToPatch, err := getTlsRouteIndexesToPatch(vsvcTLSRoutes, tlsRoutes)
	if err != nil {
		return err
	}
	for _, routeIndex := range routeIndexesToPatch {
		route := tlsRoutes[routeIndex]
		err := validateVirtualServiceRouteDestinations(route.Route, stableSvc, canarySvc, r.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule)
		if err != nil {
			return err
		}
	}
	if len(vsvcTLSRoutes) == 0 && len(tlsRoutes) > 1 {
		return fmt.Errorf("spec.tls[] should be set in VirtualService and it must have exactly one route when omitting spec.strategy.canary.trafficRouting.istio.virtualService.tlsRoutes")
	}
	return nil
}

// validateVirtualServiceRouteDestinations verifies that there is both a canary and a stable host or subset specified
func validateVirtualServiceRouteDestinations(hr []VirtualServiceRouteDestination, stableSvc, canarySvc string, dRule *v1alpha1.IstioDestinationRule) error {
	hasStableSvc := false
	hasCanarySvc := false
	hasStableSubset := false
	hasCanarySubset := false
	for _, r := range hr {
		host := getHost(r)

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
	return validateDestinationRule(dRule, hasCanarySubset, hasStableSubset, hasCanarySvc, hasStableSvc, canarySvc, stableSvc)
}

func validateDestinationRule(dRule *v1alpha1.IstioDestinationRule, hasCanarySubset, hasStableSubset, hasCanarySvc, hasStableSvc bool, canarySvc, stableSvc string) error {
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
