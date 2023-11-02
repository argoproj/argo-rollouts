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
const Tcp = "tcp"
const Type = "Istio"

const SpecHttpNotFound = "spec.http not found"

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

func (patches virtualServicePatches) patchVirtualService(httpRoutes []interface{}, tlsRoutes []interface{}, tcpRoutes []interface{}) error {
	for _, patch := range patches {
		var route map[string]interface{}
		err := false
		if patch.routeType == Http {
			route, err = httpRoutes[patch.routeIndex].(map[string]interface{})
		} else if patch.routeType == Tls {
			route, err = tlsRoutes[patch.routeIndex].(map[string]interface{})
		} else if patch.routeType == Tcp {
			route, err = tcpRoutes[patch.routeIndex].(map[string]interface{})
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
		} else if patch.routeType == Tcp {
			tcpRoutes[patch.routeIndex] = route
		}
	}
	return nil
}

func (r *Reconciler) generateVirtualServicePatches(rolloutVsvcRouteNames []string, httpRoutes []VirtualServiceHTTPRoute, rolloutVsvcTLSRoutes []v1alpha1.TLSRoute, tlsRoutes []VirtualServiceTLSRoute, rolloutVsvcTCPRoutes []v1alpha1.TCPRoute, tcpRoutes []VirtualServiceTCPRoute, desiredWeight int64, additionalDestinations ...v1alpha1.WeightDestination) virtualServicePatches {
	canarySvc := r.rollout.Spec.Strategy.Canary.CanaryService
	stableSvc := r.rollout.Spec.Strategy.Canary.StableService
	canarySubset := ""
	stableSubset := ""
	if r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule != nil {
		canarySubset = r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule.CanarySubsetName
		stableSubset = r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule.StableSubsetName
	}

	// Go through all the routes on the Istio Virtual Service looking for routes that are Istio mirror routes as well as on the
	// managedRoutes field on the rollout object so that we can update the Istio mirror upon set weight calls
	if r.rollout.Spec.Strategy.Canary.TrafficRouting != nil && r.rollout.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes != nil {
		for _, httpRoute := range httpRoutes {
			if httpRoute.Mirror != nil {
				for _, managedRoute := range r.rollout.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes {
					//Make sure we only add mirror routes from the managedRoutes field to the list of routes to update for setWeight
					if managedRoute.Name == httpRoute.Name {
						rolloutVsvcRouteNames = append(rolloutVsvcRouteNames, httpRoute.Name)
					}
				}
			}
		}
	}

	// err can be ignored because we already called ValidateHTTPRoutes earlier
	httpRouteIndexesToPatch, _ := getHttpRouteIndexesToPatch(rolloutVsvcRouteNames, httpRoutes)
	tlsRouteIndexesToPatch, _ := getTlsRouteIndexesToPatch(rolloutVsvcTLSRoutes, tlsRoutes)
	tcpRouteIndexesToPatch, _ := getTcpRouteIndexesToPatch(rolloutVsvcTCPRoutes, tcpRoutes)

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
	// Process TCP Routes
	for _, routeIdx := range tcpRouteIndexesToPatch {
		if len(tcpRoutes) <= routeIdx {
			break
		}
		patches = processRoutes(Tcp, routeIdx, tcpRoutes[routeIdx].Route, desiredWeight, svcSubsets, patches, additionalDestinations...)
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

func (r *Reconciler) reconcileVirtualService(obj *unstructured.Unstructured, vsvcRouteNames []string, vsvcTLSRoutes []v1alpha1.TLSRoute, vsvcTCPRoutes []v1alpha1.TCPRoute, desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*unstructured.Unstructured, bool, error) {
	newObj := obj.DeepCopy()

	// HTTP Routes
	var httpRoutes []VirtualServiceHTTPRoute
	httpRoutesI, err := GetHttpRoutesI(newObj)
	if err == nil {
		httpRoutes, err = GetHttpRoutes(httpRoutesI)
		if err != nil {
			return nil, false, err
		}
		if err = ValidateHTTPRoutes(r.rollout, vsvcRouteNames, httpRoutes); err != nil {
			return nil, false, err
		}
	}

	// TLS Routes
	var tlsRoutes []VirtualServiceTLSRoute
	tlsRoutesI, err := GetTlsRoutesI(newObj)
	if err == nil {
		tlsRoutes, err = GetTlsRoutes(newObj, tlsRoutesI)
		if err != nil {
			return nil, false, err
		}
		if err = ValidateTlsRoutes(r.rollout, vsvcTLSRoutes, tlsRoutes); err != nil {
			return nil, false, err
		}
	}

	// TCP Routes
	var tcpRoutes []VirtualServiceTCPRoute
	tcpRoutesI, err := GetTcpRoutesI(newObj)
	if err == nil {
		tcpRoutes, err = GetTcpRoutes(newObj, tcpRoutesI)
		if err != nil {
			return nil, false, err
		}
		if err = ValidateTcpRoutes(r.rollout, vsvcTCPRoutes, tcpRoutes); err != nil {
			return nil, false, err
		}
	}

	// Generate Patches
	patches := r.generateVirtualServicePatches(vsvcRouteNames, httpRoutes, vsvcTLSRoutes, tlsRoutes, vsvcTCPRoutes, tcpRoutes, int64(desiredWeight), additionalDestinations...)
	err = patches.patchVirtualService(httpRoutesI, tlsRoutesI, tcpRoutesI)
	if err != nil {
		return nil, false, err
	}

	// Set HTTP Route Slice
	if len(httpRoutes) > 0 {
		if err := unstructured.SetNestedSlice(newObj.Object, httpRoutesI, "spec", Http); err != nil {
			return newObj, len(patches) > 0, err
		}
	}

	// Set TLS Route Slice
	if len(tlsRoutes) > 0 {
		err = unstructured.SetNestedSlice(newObj.Object, tlsRoutesI, "spec", Tls)
		if err != nil {
			return newObj, len(patches) > 0, err
		}
	}

	// Set TCP Route Slice
	if len(tcpRoutes) > 0 {
		err = unstructured.SetNestedSlice(newObj.Object, tcpRoutesI, "spec", Tcp)
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

	origBytes, dRule, dRuleNew, err := r.getDestinationRule(dRuleSpec, client, ctx)
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
		"host":    dRule.Spec.Host,
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
		return nil, fmt.Errorf(SpecHttpNotFound)
	}
	if err != nil {
		return nil, err
	}
	return httpRoutesI, nil
}

func GetTlsRoutesI(obj *unstructured.Unstructured) ([]interface{}, error) {
	tlsRoutesI, notFound, err := unstructured.NestedSlice(obj.Object, "spec", Tls)
	if !notFound {
		return nil, fmt.Errorf(SpecHttpNotFound)
	}
	if err != nil {
		return nil, err
	}
	return tlsRoutesI, nil
}

func GetTcpRoutesI(obj *unstructured.Unstructured) ([]interface{}, error) {
	tcpRoutesI, notFound, err := unstructured.NestedSlice(obj.Object, "spec", Tcp)
	if !notFound {
		return nil, fmt.Errorf(".spec.tcp is not defined")
	}
	if err != nil {
		return nil, err
	}
	return tcpRoutesI, nil
}

func GetHttpRoutes(httpRoutesI []interface{}) ([]VirtualServiceHTTPRoute, error) {
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

func GetTcpRoutes(obj *unstructured.Unstructured, tcpRoutesI []interface{}) ([]VirtualServiceTCPRoute, error) {
	routeBytes, err := json.Marshal(tcpRoutesI)
	if err != nil {
		return nil, err
	}

	var tcpRoutes []VirtualServiceTCPRoute
	err = json.Unmarshal(routeBytes, &tcpRoutes)
	if err != nil {
		return nil, err
	}

	return tcpRoutes, nil
}

// Type indicates this reconciler is an Istio reconciler
func (r *Reconciler) Type() string {
	return Type
}

// SetWeight modifies Istio resources to reach desired state
func (r *Reconciler) SetWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
	ctx := context.TODO()
	virtualServices := r.getVirtualServices()
	for _, virtualService := range virtualServices {
		name := virtualService.Name
		namespace, vsvcName := istioutil.GetVirtualServiceNamespaceName(name)
		if namespace == "" {
			namespace = r.rollout.Namespace
		}

		client := r.client.Resource(istioutil.GetIstioVirtualServiceGVR()).Namespace(namespace)
		vsvc, err := r.getVirtualService(namespace, vsvcName, client, ctx)
		if err != nil {
			return err
		}
		modifiedVirtualService, modified, err := r.reconcileVirtualService(vsvc, virtualService.Routes, virtualService.TLSRoutes, virtualService.TCPRoutes, desiredWeight, additionalDestinations...)
		if err != nil {
			return err
		}
		if !modified {
			continue
		}

		if err := r.orderRoutes(modifiedVirtualService); err != nil && err.Error() != SpecHttpNotFound {
			return fmt.Errorf("[SetWeight] failed to order routes: %w", err)
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

func (r *Reconciler) getVirtualServices() []v1alpha1.IstioVirtualService {
	if istioutil.MultipleVirtualServiceConfigured(r.rollout) {
		return r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualServices
	} else {
		return []v1alpha1.IstioVirtualService{*r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService}
	}
}

func (r *Reconciler) getVirtualService(namespace string, vsvcName string, client dynamic.ResourceInterface, ctx context.Context) (*unstructured.Unstructured, error) {
	var vsvc *unstructured.Unstructured
	var err error
	if r.virtualServiceLister != nil {
		vsvc, err = r.virtualServiceLister.Namespace(namespace).Get(vsvcName)
	} else {
		vsvc, err = client.Get(ctx, vsvcName, metav1.GetOptions{})
	}
	if err != nil {
		if k8serrors.IsNotFound(err) {
			r.recorder.Warnf(r.rollout, record.EventOptions{EventReason: "VirtualServiceNotFound"}, "VirtualService `%s` not found", vsvcName)
		}
		return nil, err
	}
	return vsvc, err
}

func (r *Reconciler) reconcileVirtualServiceHeaderRoutes(virtualService v1alpha1.IstioVirtualService, obj *unstructured.Unstructured, headerRouting *v1alpha1.SetHeaderRoute) error {
	destRuleHost, err := r.getDestinationRuleHost()
	if err != nil {
		return err
	}

	canarySvc := r.rollout.Spec.Strategy.Canary.CanaryService
	if destRuleHost != "" {
		canarySvc = destRuleHost
	}
	var canarySubset string
	if r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule != nil {
		canarySubset = r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule.CanarySubsetName
	}

	if headerRouting.Match == nil {
		//Remove mirror route
		err := removeRoute(obj, headerRouting.Name)
		if err != nil {
			return fmt.Errorf("[reconcileVirtualServiceHeaderRoutes] failed to remove route from virtual service: %w", err)
		}
		return nil
	}

	//Remove route first to avoid duplicates
	err = removeRoute(obj, headerRouting.Name)
	if err != nil {
		return fmt.Errorf("[reconcileVirtualServiceHeaderRoutes] failed to remove http route from virtual service: %w", err)
	}

	// HTTP Routes
	httpRoutesI, err := GetHttpRoutesI(obj)
	if err != nil {
		return err
	}

	httpRoutesI = append(httpRoutesI, createHeaderRoute(virtualService, obj, headerRouting, canarySvc, canarySubset))

	err = unstructured.SetNestedSlice(obj.Object, httpRoutesI, "spec", Http)
	if err != nil {
		return err
	}
	return nil
}

func (r *Reconciler) SetHeaderRoute(headerRouting *v1alpha1.SetHeaderRoute) error {
	ctx := context.TODO()
	virtualServices := r.getVirtualServices()
	for _, virtualService := range virtualServices {
		name := virtualService.Name
		namespace, vsvcName := istioutil.GetVirtualServiceNamespaceName(name)
		if namespace == "" {
			namespace = r.rollout.Namespace
		}

		client := r.client.Resource(istioutil.GetIstioVirtualServiceGVR()).Namespace(namespace)
		vsvc, err := r.getVirtualService(namespace, vsvcName, client, ctx)
		if err != nil {
			return fmt.Errorf("[SetHeaderRoute] failed to get istio virtual service: %w", err)
		}

		err = r.reconcileVirtualServiceHeaderRoutes(virtualService, vsvc, headerRouting)
		if err != nil {
			return fmt.Errorf("[SetHeaderRoute] failed to reconcile header routes: %w", err)
		}

		if err := r.orderRoutes(vsvc); err != nil && err.Error() != SpecHttpNotFound {
			return fmt.Errorf("[SetHeaderRoute] failed to order routes: %w", err)
		}
		_, err = client.Update(ctx, vsvc, metav1.UpdateOptions{})
		if err == nil {
			r.log.Debugf("Updated VirtualService: %s", vsvc)
			r.recorder.Eventf(r.rollout, record.EventOptions{EventReason: "Updated VirtualService"}, "VirtualService `%s` set headerRoute '%v'", vsvcName, headerRouting.Name)
		} else {
			return fmt.Errorf("[SetHeaderRoute] failed to update routes: %w", err)
		}
	}
	return nil
}

func (r *Reconciler) getDestinationRuleHost() (string, error) {
	if r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule != nil {
		ctx := context.TODO()
		client := r.client.Resource(istioutil.GetIstioDestinationRuleGVR()).Namespace(r.rollout.Namespace)
		_, dRule, _, err := r.getDestinationRule(r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule, client, ctx)
		if err != nil {
			return "", err
		}
		return dRule.Spec.Host, nil
	}
	return "", nil
}

func (r *Reconciler) getDestinationRule(dRuleSpec *v1alpha1.IstioDestinationRule, client dynamic.ResourceInterface, ctx context.Context) ([]byte, *DestinationRule, *DestinationRule, error) {
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
		return nil, nil, nil, err
	}
	origBytes, dRule, dRuleNew, err := unstructuredToDestinationRules(dRuleUn)
	if err != nil {
		return nil, nil, nil, err
	}
	return origBytes, dRule, dRuleNew, nil
}

func createHeaderRoute(virtualService v1alpha1.IstioVirtualService, unVsvc *unstructured.Unstructured, headerRouting *v1alpha1.SetHeaderRoute, host string, subset string) map[string]interface{} {
	var routeMatches []interface{}
	for _, hrm := range headerRouting.Match {
		routeMatches = append(routeMatches, createHeaderRouteMatch(hrm))
	}

	port, err := getVirtualServiceCanaryPort(unVsvc, virtualService)
	if err != nil {
		port = Port{Number: 0}
	}

	canaryDestination := routeDestination(host, port.Number, subset, 100)

	return map[string]interface{}{
		"name":  headerRouting.Name,
		"match": routeMatches,
		"route": []interface{}{canaryDestination},
	}
}

func createHeaderRouteMatch(hrm v1alpha1.HeaderRoutingMatch) interface{} {
	res := map[string]interface{}{}
	value := hrm.HeaderValue
	setMapValueIfNotEmpty(res, "exact", value.Exact)
	setMapValueIfNotEmpty(res, "regex", value.Regex)
	setMapValueIfNotEmpty(res, "prefix", value.Prefix)
	return map[string]interface{}{
		"headers": map[string]interface{}{hrm.HeaderName: res},
	}
}

func setMapValueIfNotEmpty(m map[string]interface{}, key string, value string) {
	if value != "" {
		m[key] = value
	}
}

func routeDestination(host string, port uint32, subset string, weight int64) map[string]interface{} {
	dest := map[string]interface{}{
		"host": host,
	}
	if port > 0 {
		dest["port"] = map[string]interface{}{"number": int64(port)}
	}
	if subset != "" {
		dest["subset"] = subset
	}
	routeValue := map[string]interface{}{
		"weight":      float64(weight),
		"destination": dest,
	}
	return routeValue
}

func (r *Reconciler) VerifyWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error) {
	return nil, nil
}

// getHttpRouteIndexesToPatch returns array indices of the httpRoutes which need to be patched when updating weights
func getHttpRouteIndexesToPatch(routeNames []string, httpRoutes []VirtualServiceHTTPRoute) ([]int, error) {
	//We have no routes listed in spec.strategy.canary.trafficRouting.istio.virtualService.routes so find index
	//of the first empty named route
	if len(routeNames) == 0 {
		for i, route := range httpRoutes {
			if route.Name == "" {
				return []int{i}, nil
			}
		}
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
	for i, route := range httpRoutes {
		if route.Name == routeName {
			return i
		}
	}
	return -1
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

// getTcpRouteIndexesToPatch returns array indices of the tcpRoutes which need to be patched when updating weights
func getTcpRouteIndexesToPatch(tcpRoutes []v1alpha1.TCPRoute, istioTcpRoutes []VirtualServiceTCPRoute) ([]int, error) {
	if len(tcpRoutes) == 0 {
		return []int{0}, nil
	}

	var routeIndexesToPatch []int
	for _, tcpRoute := range tcpRoutes {
		routeIndices := searchTcpRoute(tcpRoute, istioTcpRoutes)
		if len(routeIndices) > 0 {
			for _, routeIndex := range routeIndices {
				routeIndexesToPatch = append(routeIndexesToPatch, routeIndex)
			}
		} else {
			return nil, fmt.Errorf("No matching TCP routes found in the defined Virtual Service.")
		}
	}
	return routeIndexesToPatch, nil
}

func searchTcpRoute(tcpRoute v1alpha1.TCPRoute, istioTcpRoutes []VirtualServiceTCPRoute) []int {
	routeIndices := []int{}
	for i, route := range istioTcpRoutes {
		portsMap := make(map[int64]bool)
		for _, routeMatch := range route.Match {
			portsMap[routeMatch.Port] = true
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
		// To find a match for TCP Routes in Istio VS, we'll have to verify that:
		// 1. There is exactly one port present in the `ports`;
		// 2. The single port in `ports` matches with the `tcpRoute.Port`;
		if port == tcpRoute.Port {
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

	httpRoutesBytes, err := json.Marshal(httpRoutes)
	if err != nil {
		return fmt.Errorf("[ValidateHTTPRoutes] failed to marshal http routes: %w", err)
	}
	var httpRoutesI []interface{}
	err = json.Unmarshal(httpRoutesBytes, &httpRoutesI)
	if err != nil {
		return fmt.Errorf("[ValidateHTTPRoutes] failed to marshal http routes to []interface{}: %w", err)
	}

	_, httpRoutesNotWithinManagedRoutes, err := splitManagedRoutesAndNonManagedRoutes(r.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes, httpRoutesI)
	if err != nil {
		return fmt.Errorf("[ValidateHTTPRoutes] failed to split managed and non-managed routes: %w", err)
	}

	if len(routeNames) == 0 && len(httpRoutesNotWithinManagedRoutes) > 1 {
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

// ValidateTcpRoutes ensures that all the routes in the rollout exist and they only have two destinations
func ValidateTcpRoutes(r *v1alpha1.Rollout, vsvcTCPRoutes []v1alpha1.TCPRoute, tcpRoutes []VirtualServiceTCPRoute) error {
	stableSvc := r.Spec.Strategy.Canary.StableService
	canarySvc := r.Spec.Strategy.Canary.CanaryService

	routeIndexesToPatch, err := getTcpRouteIndexesToPatch(vsvcTCPRoutes, tcpRoutes)
	if err != nil {
		return err
	}
	for _, routeIndex := range routeIndexesToPatch {
		route := tcpRoutes[routeIndex]
		err := validateVirtualServiceRouteDestinations(route.Route, stableSvc, canarySvc, r.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule)
		if err != nil {
			return err
		}
	}
	if len(vsvcTCPRoutes) == 0 && len(tcpRoutes) > 1 {
		return fmt.Errorf("spec.tcp[] should be set in VirtualService and it must have exactly one route when omitting spec.strategy.canary.trafficRouting.istio.virtualService.tcpRoutes")
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

func (r *Reconciler) SetMirrorRoute(setMirrorRoute *v1alpha1.SetMirrorRoute) error {
	ctx := context.TODO()
	virtualServices := r.getVirtualServices()

	for _, virtualService := range virtualServices {
		name := virtualService.Name
		namespace, vsvcName := istioutil.GetVirtualServiceNamespaceName(name)
		if namespace == "" {
			namespace = r.rollout.Namespace
		}

		client := r.client.Resource(istioutil.GetIstioVirtualServiceGVR()).Namespace(namespace)
		istioVirtualSvc, err := r.getVirtualService(namespace, vsvcName, client, ctx)
		if err != nil {
			return fmt.Errorf("[SetMirrorRoute] failed to get virtual service: %w", err)
		}

		err = r.reconcileVirtualServiceMirrorRoutes(virtualService, istioVirtualSvc, setMirrorRoute)
		if err != nil {
			return fmt.Errorf("[SetMirrorRoute] failed reconcile virtual service for mirror routes: %w", err)
		}

		if err := r.orderRoutes(istioVirtualSvc); err != nil && err.Error() != SpecHttpNotFound {
			return fmt.Errorf("[SetMirrorRoute] failed to order routes based on managedRoute order: %w", err)
		}
		_, err = client.Update(ctx, istioVirtualSvc, metav1.UpdateOptions{})
		if err == nil {
			r.log.Debugf("Updated VirtualService: %s", istioVirtualSvc)
			r.recorder.Eventf(r.rollout, record.EventOptions{EventReason: "Updated VirtualService"}, "VirtualService `%s` set mirrorRoute '%v'", vsvcName, setMirrorRoute.Name)
		} else {
			return fmt.Errorf("[SetMirrorRoute] failed to update virtual service %w", err)
		}

	}
	return nil
}

func (r *Reconciler) reconcileVirtualServiceMirrorRoutes(virtualService v1alpha1.IstioVirtualService, istioVirtualService *unstructured.Unstructured, mirrorRoute *v1alpha1.SetMirrorRoute) error {
	destRuleHost, err := r.getDestinationRuleHost()
	if err != nil {
		return fmt.Errorf("[reconcileVirtualServiceMirrorRoutes] failed to get destination rule host: %w", err)
	}
	canarySvc := r.rollout.Spec.Strategy.Canary.CanaryService
	if destRuleHost != "" {
		canarySvc = destRuleHost
	}
	var canarySubset string
	if r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule != nil {
		canarySubset = r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule.CanarySubsetName
	}

	//Remove mirror route when there is no match rules we require a match on routes for safety so a none listed match
	//acts like a removal of the route instead of say routing all traffic
	if mirrorRoute.Match == nil {
		//Remove mirror route
		err := removeRoute(istioVirtualService, mirrorRoute.Name)
		if err != nil {
			return fmt.Errorf("[reconcileVirtualServiceMirrorRoutes] failed to remove route from virtual service: %w", err)
		}
		return nil
	}

	//Remove route first to avoid duplicates
	err = removeRoute(istioVirtualService, mirrorRoute.Name)
	if err != nil {
		return fmt.Errorf("[reconcileVirtualServiceMirrorRoutes] failed to remove http route from virtual service: %w", err)
	}

	httpRoutes, _, err := getVirtualServiceHttpRoutes(istioVirtualService)
	if err != nil {
		return fmt.Errorf("[reconcileVirtualServiceMirrorRoutes] failed to get virtual service http routes: %w", err)
	}

	mR, err := createMirrorRoute(virtualService, httpRoutes, mirrorRoute, canarySvc, canarySubset)
	if err != nil {
		return fmt.Errorf("[reconcileVirtualServiceMirrorRoutes] failed to create mirror route: %w", err)
	}

	vsRoutes, found, err := unstructured.NestedSlice(istioVirtualService.Object, "spec", Http)
	if err != nil {
		return fmt.Errorf("[reconcileVirtualServiceMirrorRoutes] failed to get http routes from virtual service: %w", err)
	}
	if !found {
		return fmt.Errorf(SpecHttpNotFound)
	}
	vsRoutes = append([]interface{}{mR}, vsRoutes...)
	if err := unstructured.SetNestedSlice(istioVirtualService.Object, vsRoutes, "spec", Http); err != nil {
		return fmt.Errorf("[reconcileVirtualServiceMirrorRoutes] failed to update virtual service routes via set nested slice: %w", err)
	}

	return nil
}

// getVirtualServiceHttpRoutes This returns all the http routes from an istio virtual service as both a rollouts wrapped type
// []VirtualServiceHTTPRoute and a []interface{} of VirtualServiceHTTPRoute
func getVirtualServiceHttpRoutes(obj *unstructured.Unstructured) ([]VirtualServiceHTTPRoute, []interface{}, error) {
	httpRoutesI, err := GetHttpRoutesI(obj)
	if err != nil {
		return nil, nil, fmt.Errorf("[getVirtualServiceHttpRoutes] failed to get http route interfaces: %w", err)
	}
	routes, err := GetHttpRoutes(httpRoutesI)
	if err != nil {
		return nil, httpRoutesI, fmt.Errorf("[getVirtualServiceHttpRoutes] failed to get http route types: %w", err)
	}
	return routes, httpRoutesI, nil
}

// createMirrorRoute This returns a map[string]interface{} of an istio virtual service mirror route configuration using the last
// set weight as values for the non-matching destinations and canary service for the matching destination.
func createMirrorRoute(virtualService v1alpha1.IstioVirtualService, httpRoutes []VirtualServiceHTTPRoute, mirrorRouting *v1alpha1.SetMirrorRoute, canarySvc string, subset string) (map[string]interface{}, error) {
	var percent int32
	if mirrorRouting.Percentage == nil {
		percent = 100
	} else {
		percent = *mirrorRouting.Percentage
	}

	route, err := getVirtualServiceSetWeightRoute(virtualService.Routes, httpRoutes)
	if err != nil {
		return nil, fmt.Errorf("[createMirrorRoute] failed to get virtual service http route for keeping non-mirror weights set: %w", err)
	}

	var istioMatch []RouteMatch
	for _, match := range mirrorRouting.Match {
		istioMatch = append(istioMatch, RouteMatch{
			Method:  match.Method,
			Uri:     match.Path,
			Headers: match.Headers,
		})
	}

	mirrorDestinations := VirtualServiceDestination{
		Host:   canarySvc,
		Subset: subset,
	}
	if len(route) >= 0 && route[0].Destination.Port != nil {
		// We try to pull the port from any of the routes destinations that are supposed to be updated via SetWeight
		mirrorDestinations.Port = &Port{Number: route[0].Destination.Port.Number}
	}

	mirrorRoute := map[string]interface{}{
		"name":             mirrorRouting.Name,
		"match":            istioMatch,
		"route":            route,
		"mirror":           mirrorDestinations,
		"mirrorPercentage": map[string]interface{}{"value": float64(percent)},
	}

	mirrorRouteBytes, err := json.Marshal(mirrorRoute)
	if err != nil {
		return nil, fmt.Errorf("[createMirrorRoute] failed to marshal mirror route: %w", err)
	}

	var mirrorRouteI map[string]interface{}
	err = json.Unmarshal(mirrorRouteBytes, &mirrorRouteI)
	if err != nil {
		return nil, fmt.Errorf("[createMirrorRoute] failed to unmarshal mirror route: %w", err)
	}

	return mirrorRouteI, nil
}

// getVirtualServiceSetWeightRoute This functions goes through the list of Istio Virtual service routes and finds the first
// match from the trafficRouting.istio.virtualService[s].routes field and returns the []VirtualServiceRouteDestination array
// from the istio virtual service this can be useful to get the last set destination percentages on the canary route.
func getVirtualServiceSetWeightRoute(rolloutVsvcRouteNames []string, httpRoutes []VirtualServiceHTTPRoute) ([]VirtualServiceRouteDestination, error) {
	routeIndexesToPatch, err := getHttpRouteIndexesToPatch(rolloutVsvcRouteNames, httpRoutes)
	if err != nil {
		return nil, fmt.Errorf("[getVirtualServiceSetWeightRoute] failed to get routes that need to be patch when set weight is called: %w", err)
	}
	for _, routeIndex := range routeIndexesToPatch {
		route := httpRoutes[routeIndex]
		return route.Route, nil
	}
	return nil, nil
}

// removeRoute This functions removes the `routeName` route from the Istio Virtual Service
func removeRoute(istioVirtualService *unstructured.Unstructured, routeName string) error {
	vsRoutes, found, err := unstructured.NestedSlice(istioVirtualService.Object, "spec", Http)
	if err != nil {
		return fmt.Errorf("[removeRoute] failed to get http routes from virtual service: %w", err)
	}
	if !found {
		return fmt.Errorf(SpecHttpNotFound)
	}

	var newVsRoutes []interface{}
	for _, route := range vsRoutes {
		routeMap, ok := route.(map[string]interface{})
		if !ok {
			return fmt.Errorf("Could not cast type to map[string]interface{} to find route name in Istio Virtual Service")
		}
		routeNameIstioSvc, ok := routeMap["name"].(string)
		if !ok {
			log.Debugf("Could not cast type to string to find route name in Istio Virtual Service, route probably has no name set")
		}
		if routeName != routeNameIstioSvc {
			newVsRoutes = append(newVsRoutes, route)
		}
	}
	if err := unstructured.SetNestedSlice(istioVirtualService.Object, newVsRoutes, "spec", Http); err != nil {
		return fmt.Errorf("[removeRoute] failed to set http routes on virtual service: %w", err)
	}
	return nil
}

// orderRoutes Is a function that orders the routes based on the managedRoute field in the rollout spec. It then places
// the sorted routes ontop of any other route that is already defined on the Istio Virtual Service.
func (r *Reconciler) orderRoutes(istioVirtualService *unstructured.Unstructured) error {
	httpRouteI, found, err := unstructured.NestedSlice(istioVirtualService.Object, "spec", Http)
	if err != nil {
		return fmt.Errorf("[orderRoutes] failed to get virtual service http routes: %w", err)
	}
	if !found {
		return fmt.Errorf(SpecHttpNotFound)
	}

	if r.rollout.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes == nil {
		return nil //Not really and error there is just nothing to sort on
	}

	managedRoutes := r.rollout.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes
	httpRoutesWithinManagedRoutes, httpRoutesNotWithinManagedRoutes, err := splitManagedRoutesAndNonManagedRoutes(managedRoutes, httpRouteI)
	if err != nil {
		return fmt.Errorf("[orderRoutes] could not split routes between managed and non managed: %w", err)
	}

	finalRoutes, err := getOrderedVirtualServiceRoutes(httpRouteI, managedRoutes, httpRoutesWithinManagedRoutes, httpRoutesNotWithinManagedRoutes)
	if err != nil {
		return fmt.Errorf("[orderRoutes] could not get ordered virtual service routes: %w", err)
	}

	if err := unstructured.SetNestedSlice(istioVirtualService.Object, finalRoutes, "spec", Http); err != nil {
		return fmt.Errorf("[orderRoutes] set nested slice failed: %w", err)
	}

	return nil
}

// splitManagedRoutesAndNonManagedRoutes This splits the routes from an istio virtual service into two slices
// one slice contains all the routes that are also in the rollouts managedRoutes object and one that contains routes
// that where only in the virtual service (aka routes that where manually added by user)
func splitManagedRoutesAndNonManagedRoutes(managedRoutes []v1alpha1.MangedRoutes, httpRouteI []interface{}) (httpRoutesWithinManagedRoutes []map[string]interface{}, httpRoutesNotWithinManagedRoutes []map[string]interface{}, err error) {
	var httpRoutes []map[string]interface{}

	jsonHttpRoutes, err := json.Marshal(httpRouteI)
	if err != nil {
		return nil, nil, fmt.Errorf("[splitManagedRoutesAndNonManagedRoutes] failed to marsharl http route interface: %w", err)
	}

	if err := json.Unmarshal(jsonHttpRoutes, &httpRoutes); err != nil {
		return nil, nil, fmt.Errorf("[splitManagedRoutesAndNonManagedRoutes] failed to unmarsharl http route interface: %w", err)
	}

	for _, route := range httpRoutes {
		var found bool = false
		for _, managedRoute := range managedRoutes {
			// Not checking the cast success here is ok because it covers the case when the route has no name
			// when there is no name the cast return an empty string and will just not match the managed route
			name, _ := route["name"].(string)
			if name == managedRoute.Name {
				httpRoutesWithinManagedRoutes = append(httpRoutesWithinManagedRoutes, route)
				found = true
				break
			}
		}
		if !found {
			httpRoutesNotWithinManagedRoutes = append(httpRoutesNotWithinManagedRoutes, route)
		}
	}

	return httpRoutesWithinManagedRoutes, httpRoutesNotWithinManagedRoutes, nil
}

// getOrderedVirtualServiceRoutes This returns an []interface{} of istio virtual routes where the routes are ordered based
// on the rollouts managedRoutes field. We take the routes from the rollouts managedRoutes field order them and place them on top
// of routes that are manually defined within the virtual service (aka. routes that users have defined manually)
func getOrderedVirtualServiceRoutes(httpRouteI []interface{}, managedRoutes []v1alpha1.MangedRoutes, httpRoutesWithinManagedRoutes []map[string]interface{}, httpRoutesNotWithinManagedRoutes []map[string]interface{}) ([]interface{}, error) {
	var orderedManagedRoutes []map[string]interface{}
	for _, route := range managedRoutes {
		for _, managedRoute := range httpRoutesWithinManagedRoutes {
			if route.Name == managedRoute["name"] {
				orderedManagedRoutes = append(orderedManagedRoutes, managedRoute)
			}
		}
	}

	orderedVirtualServiceHTTPRoutes := append(orderedManagedRoutes, httpRoutesNotWithinManagedRoutes...)

	var orderedInterfaceVSVCHTTPRoutes []interface{}
	for _, routeMap := range orderedVirtualServiceHTTPRoutes {
		for _, route := range httpRouteI {
			r := route.(map[string]interface{})

			// Not checking the cast success here is ok because it covers the case when the route has no name
			name, rNameOK := r["name"].(string)
			routeMapName, RMapNameOK := routeMap["name"].(string)
			// The second or clause is for the case when we have a route that has no name set because this field
			// is optional in istio virtual service if there is only one route
			if name == routeMapName || (!rNameOK && !RMapNameOK) {
				orderedInterfaceVSVCHTTPRoutes = append(orderedInterfaceVSVCHTTPRoutes, route)
			}
		}
	}

	return orderedInterfaceVSVCHTTPRoutes, nil
}

// getVirtualServiceCanaryPort This function returns the port that the canary service is running on. It does this by looking at the
// istio Virtual Service and finding any port from a destination that is suppose to be update via SetWeight.
func getVirtualServiceCanaryPort(unVsvc *unstructured.Unstructured, virtualService v1alpha1.IstioVirtualService) (Port, error) {
	httpRoutes, _, err := getVirtualServiceHttpRoutes(unVsvc)
	if err != nil {
		return Port{}, fmt.Errorf("[getVirtualServiceCanaryPort] failed to get virtual service http routes: %w", err)
	}

	route, err := getVirtualServiceSetWeightRoute(virtualService.Routes, httpRoutes)
	if err != nil {
		return Port{}, fmt.Errorf("[getVirtualServiceCanaryPort] failed to get virtual service set weight route: %w", err)
	}

	var port uint32 = 0
	if len(route) > 0 && route[0].Destination.Port != nil {
		port = route[0].Destination.Port.Number
	}

	return Port{
		Number: port,
	}, nil
}

// RemoveManagedRoutes this removes all the routes in all the istio virtual services rollouts is managing by getting two slices
// from the splitManagedRoutesAndNonManagedRoutes function and setting the Istio Virtual Service routes to just the ones not managed
// by rollouts
func (r *Reconciler) RemoveManagedRoutes() error {
	ctx := context.TODO()
	virtualServices := r.getVirtualServices()

	for _, virtualService := range virtualServices {
		name := virtualService.Name
		namespace, vsvcName := istioutil.GetVirtualServiceNamespaceName(name)
		if namespace == "" {
			namespace = r.rollout.Namespace
		}

		client := r.client.Resource(istioutil.GetIstioVirtualServiceGVR()).Namespace(namespace)
		istioVirtualService, err := r.getVirtualService(namespace, vsvcName, client, ctx)
		if err != nil {
			return fmt.Errorf("[RemoveManagedRoutes] failed to get virtual service: %w", err)
		}

		httpRouteI, found, err := unstructured.NestedSlice(istioVirtualService.Object, "spec", Http)
		if err != nil {
			return fmt.Errorf("[RemoveManagedRoutes] failed to get http routes from virtual service: %w", err)
		}
		if !found {
			// This could happen when only TLS routes are defined. We don't need to do anything else and hence we return early
			// because tls routes do not support header and mirroring which are features that require the use of managed routes.
			log.Debugf("[RemoveManagedRoutes] %s: not removing any routes", SpecHttpNotFound)
			return nil
		}

		managedRoutes := r.rollout.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes
		if len(managedRoutes) == 0 {
			return nil
		}
		httpRoutesWithinManagedRoutes, httpRoutesNotWithinManagedRoutes, err := splitManagedRoutesAndNonManagedRoutes(managedRoutes, httpRouteI)
		if err != nil {
			return fmt.Errorf("[RemoveManagedRoutes] failed to split managaed and non-managed routes: %w", err)
		}

		if len(httpRoutesWithinManagedRoutes) == 0 {
			//no routes to remove
			return nil
		}

		jsonNonManagedRoutes, err := json.Marshal(httpRoutesNotWithinManagedRoutes)
		if err != nil {
			return fmt.Errorf("[RemoveManagedRoutes] failed to marshal non-managed routes: %w", err)
		}
		var nonManagedRoutesI []interface{}
		if err := json.Unmarshal(jsonNonManagedRoutes, &nonManagedRoutesI); err != nil {
			return fmt.Errorf("[RemoveManagedRoutes] failed to split managaed and non-managed routes: %w", err)
		}

		if err := unstructured.SetNestedSlice(istioVirtualService.Object, nonManagedRoutesI, "spec", Http); err != nil {
			return fmt.Errorf("[RemoveManagedRoutes] failed to set nested slice on virtual service to remove managed routes: %w", err)
		}

		_, err = client.Update(ctx, istioVirtualService, metav1.UpdateOptions{})
		if err == nil {
			r.log.Debugf("Updated VirtualService: %s", istioVirtualService)
			r.recorder.Eventf(r.rollout, record.EventOptions{EventReason: "Updated VirtualService"}, "VirtualService `%s` removed all managed routes.", vsvcName)
		} else {
			return fmt.Errorf("[RemoveManagedRoutes] failed to update kubernetes virtual service: %w", err)
		}
	}
	return nil
}
