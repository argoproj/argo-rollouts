package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/plugin/rpc"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
)

const PluginName = "argoproj/phased-istio-router"

var _ rpc.TrafficRouterPlugin = &RpcPlugin{}

// PluginConfig is unmarshalled from the Rollout's trafficRouting.plugins entry.
type PluginConfig struct {
	VirtualService  VSRef   `json:"virtualService"`
	DestinationRule DRRef   `json:"destinationRule,omitempty"`
	Phases          []Phase `json:"phases"`
}

type VSRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// DRRef identifies the DestinationRule and names the canary/stable subsets used in the VS routes.
type DRRef struct {
	Name             string `json:"name"`
	Namespace        string `json:"namespace,omitempty"`
	CanarySubsetName string `json:"canarySubsetName"`
	StableSubsetName string `json:"stableSubsetName"`
}

// Phase names a single VirtualService HTTP route to be ramped.
// Weight is the fraction of total traffic (0–100) handled by this route; phases must sum to 100.
// When weight is omitted on all phases the plugin falls back to legacy mode (first incomplete
// phase receives desiredWeight directly).
type Phase struct {
	Route  string `json:"route"`
	Weight int32  `json:"weight,omitempty"`
}

type RpcPlugin struct {
	LogCtx        *logrus.Entry
	dynamicClient dynamic.Interface
}

func (p *RpcPlugin) InitPlugin() pluginTypes.RpcError {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	restConfig, err := kubeConfig.ClientConfig()
	if err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}
	dynClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}
	p.dynamicClient = dynClient
	return pluginTypes.RpcError{}
}

func (p *RpcPlugin) parseConfig(ro *v1alpha1.Rollout) (PluginConfig, pluginTypes.RpcError) {
	var cfg PluginConfig
	data, ok := ro.Spec.Strategy.Canary.TrafficRouting.Plugins[PluginName]
	if !ok {
		return cfg, pluginTypes.RpcError{ErrorString: fmt.Sprintf("plugin config not found for %s", PluginName)}
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, pluginTypes.RpcError{ErrorString: fmt.Sprintf("failed to unmarshal plugin config: %v", err)}
	}
	if cfg.VirtualService.Namespace == "" {
		cfg.VirtualService.Namespace = ro.Namespace
	}
	if cfg.DestinationRule.Namespace == "" {
		cfg.DestinationRule.Namespace = ro.Namespace
	}
	return cfg, pluginTypes.RpcError{}
}

func (p *RpcPlugin) getVS(ctx context.Context, namespace, name string) (*unstructured.Unstructured, pluginTypes.RpcError) {
	vs, err := p.dynamicClient.Resource(istioutil.GetIstioVirtualServiceGVR()).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, pluginTypes.RpcError{ErrorString: fmt.Sprintf("failed to get VirtualService %s/%s: %v", namespace, name, err)}
	}
	return vs, pluginTypes.RpcError{}
}

// cumulativeRanges returns the [start, end] range in desiredWeight space for each phase, and
// reports whether any phase has an explicit weight (proportional mode). When the second return
// value is false, the ranges are meaningless and proportional logic should not be used.
func cumulativeRanges(phases []Phase) ([][2]int32, bool) {
	ranges := make([][2]int32, len(phases))
	var cum int32
	proportional := false
	for i, p := range phases {
		if p.Weight > 0 {
			proportional = true
		}
		ranges[i] = [2]int32{cum, cum + p.Weight}
		cum += p.Weight
	}
	return ranges, proportional
}

// phaseRouteWeight maps a global desiredWeight to a per-route canary weight [0, 100].
// start and end are the phase's cumulative range boundaries in desiredWeight space.
func phaseRouteWeight(desiredWeight, start, end int32) int32 {
	if desiredWeight >= end {
		return 100
	}
	if desiredWeight <= start {
		return 0
	}
	return (desiredWeight - start) * 100 / (end - start)
}

func findHTTPRoute(httpRoutes []interface{}, routeName string) (map[string]interface{}, bool) {
	for _, r := range httpRoutes {
		route, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		if route["name"] == routeName {
			return route, true
		}
	}
	return nil, false
}

// routeCanaryWeight returns the current canary destination weight for the named HTTP route.
// Returns -1 if the route or its canary destination is not found.
func routeCanaryWeight(httpRoutes []interface{}, routeName, canarySubset string) int64 {
	route, ok := findHTTPRoute(httpRoutes, routeName)
	if !ok {
		return -1
	}
	destinations, _ := route["route"].([]interface{})
	for _, d := range destinations {
		dest, ok := d.(map[string]interface{})
		if !ok {
			continue
		}
		destInfo, _ := dest["destination"].(map[string]interface{})
		if destInfo["subset"] != canarySubset {
			continue
		}
		switch w := dest["weight"].(type) {
		case int64:
			return w
		case float64:
			return int64(w)
		case int:
			return int64(w)
		default:
			return 0
		}
	}
	return -1
}

// applyRouteWeights sets canary=desiredWeight and stable=100-desiredWeight on the named route.
func applyRouteWeights(httpRoutes []interface{}, routeName, canarySubset, stableSubset string, desiredWeight int32) error {
	route, ok := findHTTPRoute(httpRoutes, routeName)
	if !ok {
		return fmt.Errorf("route %q not found in VirtualService", routeName)
	}
	destinations, ok := route["route"].([]interface{})
	if !ok {
		return fmt.Errorf("route %q has no destinations", routeName)
	}
	for _, d := range destinations {
		dest, ok := d.(map[string]interface{})
		if !ok {
			continue
		}
		destInfo, _ := dest["destination"].(map[string]interface{})
		switch destInfo["subset"] {
		case canarySubset:
			dest["weight"] = int64(desiredWeight)
		case stableSubset:
			dest["weight"] = int64(100 - desiredWeight)
		}
	}
	return nil
}

// SetWeight advances the active phase's route to desiredWeight. The active phase is the first
// phase (in config order) whose route's canary weight is currently below 100. This allows
// phases to progress sequentially: phase N completes before phase N+1 begins.
func (p *RpcPlugin) SetWeight(ro *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) pluginTypes.RpcError {
	ctx := context.TODO()
	cfg, rpcErr := p.parseConfig(ro)
	if rpcErr.HasError() {
		return rpcErr
	}

	vs, rpcErr := p.getVS(ctx, cfg.VirtualService.Namespace, cfg.VirtualService.Name)
	if rpcErr.HasError() {
		return rpcErr
	}

	httpRoutes, _, err := unstructured.NestedSlice(vs.Object, "spec", "http")
	if err != nil {
		return pluginTypes.RpcError{ErrorString: fmt.Sprintf("failed to read spec.http: %v", err)}
	}

	canary := cfg.DestinationRule.CanarySubsetName
	stable := cfg.DestinationRule.StableSubsetName

	if ranges, proportional := cumulativeRanges(cfg.Phases); proportional {
		for i, phase := range cfg.Phases {
			rw := phaseRouteWeight(desiredWeight, ranges[i][0], ranges[i][1])
			if err := applyRouteWeights(httpRoutes, phase.Route, canary, stable, rw); err != nil {
				p.LogCtx.Warnf("could not set route %s: %v", phase.Route, err)
			}
		}
	} else if desiredWeight == 0 {
		for _, phase := range cfg.Phases {
			if err := applyRouteWeights(httpRoutes, phase.Route, canary, stable, 0); err != nil {
				p.LogCtx.Warnf("could not reset route %s: %v", phase.Route, err)
			}
		}
	} else {
		activeRoute := ""
		for _, phase := range cfg.Phases {
			if routeCanaryWeight(httpRoutes, phase.Route, canary) < 100 {
				activeRoute = phase.Route
				break
			}
		}
		if activeRoute == "" {
			return pluginTypes.RpcError{}
		}
		if err := applyRouteWeights(httpRoutes, activeRoute, canary, stable, desiredWeight); err != nil {
			return pluginTypes.RpcError{ErrorString: err.Error()}
		}
	}

	if err := unstructured.SetNestedSlice(vs.Object, httpRoutes, "spec", "http"); err != nil {
		return pluginTypes.RpcError{ErrorString: fmt.Sprintf("failed to set spec.http: %v", err)}
	}

	if _, err := p.dynamicClient.Resource(istioutil.GetIstioVirtualServiceGVR()).Namespace(cfg.VirtualService.Namespace).Update(ctx, vs, metav1.UpdateOptions{}); err != nil {
		return pluginTypes.RpcError{ErrorString: fmt.Sprintf("failed to update VirtualService: %v", err)}
	}

	return pluginTypes.RpcError{}
}

// UpdateHash sets the rollouts-pod-template-hash selector on the canary and stable DR subsets.
func (p *RpcPlugin) UpdateHash(ro *v1alpha1.Rollout, canaryHash, stableHash string, additionalDestinations []v1alpha1.WeightDestination) pluginTypes.RpcError {
	ctx := context.TODO()
	cfg, rpcErr := p.parseConfig(ro)
	if rpcErr.HasError() {
		return rpcErr
	}
	if cfg.DestinationRule.Name == "" {
		return pluginTypes.RpcError{}
	}

	drGVR := istioutil.GetIstioDestinationRuleGVR()
	dr, err := p.dynamicClient.Resource(drGVR).Namespace(cfg.DestinationRule.Namespace).Get(ctx, cfg.DestinationRule.Name, metav1.GetOptions{})
	if err != nil {
		return pluginTypes.RpcError{ErrorString: fmt.Sprintf("failed to get DestinationRule %s/%s: %v", cfg.DestinationRule.Namespace, cfg.DestinationRule.Name, err)}
	}

	subsets, _, err := unstructured.NestedSlice(dr.Object, "spec", "subsets")
	if err != nil {
		return pluginTypes.RpcError{ErrorString: fmt.Sprintf("failed to read spec.subsets: %v", err)}
	}

	for _, s := range subsets {
		subset, ok := s.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := subset["name"].(string)
		var hash string
		switch name {
		case cfg.DestinationRule.CanarySubsetName:
			hash = canaryHash
		case cfg.DestinationRule.StableSubsetName:
			hash = stableHash
		default:
			continue
		}
		if hash == "" {
			continue
		}
		labels, _ := subset["labels"].(map[string]interface{})
		if labels == nil {
			labels = map[string]interface{}{}
			subset["labels"] = labels
		}
		labels[v1alpha1.DefaultRolloutUniqueLabelKey] = hash
	}

	if err := unstructured.SetNestedSlice(dr.Object, subsets, "spec", "subsets"); err != nil {
		return pluginTypes.RpcError{ErrorString: fmt.Sprintf("failed to set spec.subsets: %v", err)}
	}

	if _, err := p.dynamicClient.Resource(drGVR).Namespace(cfg.DestinationRule.Namespace).Update(ctx, dr, metav1.UpdateOptions{}); err != nil {
		return pluginTypes.RpcError{ErrorString: fmt.Sprintf("failed to update DestinationRule: %v", err)}
	}

	return pluginTypes.RpcError{}
}

// VerifyWeight checks whether the active phase's route is at the expected canary weight.
func (p *RpcPlugin) VerifyWeight(ro *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) (pluginTypes.RpcVerified, pluginTypes.RpcError) {
	ctx := context.TODO()
	cfg, rpcErr := p.parseConfig(ro)
	if rpcErr.HasError() {
		return pluginTypes.NotVerified, rpcErr
	}

	vs, rpcErr := p.getVS(ctx, cfg.VirtualService.Namespace, cfg.VirtualService.Name)
	if rpcErr.HasError() {
		return pluginTypes.NotVerified, rpcErr
	}

	httpRoutes, _, err := unstructured.NestedSlice(vs.Object, "spec", "http")
	if err != nil {
		return pluginTypes.NotVerified, pluginTypes.RpcError{ErrorString: fmt.Sprintf("failed to read spec.http: %v", err)}
	}

	canary := cfg.DestinationRule.CanarySubsetName

	if ranges, proportional := cumulativeRanges(cfg.Phases); proportional {
		for i, phase := range cfg.Phases {
			expected := phaseRouteWeight(desiredWeight, ranges[i][0], ranges[i][1])
			if routeCanaryWeight(httpRoutes, phase.Route, canary) != int64(expected) {
				return pluginTypes.NotVerified, pluginTypes.RpcError{}
			}
		}
		return pluginTypes.Verified, pluginTypes.RpcError{}
	}

	for _, phase := range cfg.Phases {
		w := routeCanaryWeight(httpRoutes, phase.Route, canary)
		if w < 100 {
			if w == int64(desiredWeight) {
				return pluginTypes.Verified, pluginTypes.RpcError{}
			}
			return pluginTypes.NotVerified, pluginTypes.RpcError{}
		}
	}
	return pluginTypes.Verified, pluginTypes.RpcError{}
}

func (p *RpcPlugin) SetHeaderRoute(ro *v1alpha1.Rollout, headerRouting *v1alpha1.SetHeaderRoute) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (p *RpcPlugin) SetMirrorRoute(ro *v1alpha1.Rollout, setMirrorRoute *v1alpha1.SetMirrorRoute) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

// RemoveManagedRoutes resets all phase routes to stable=100, canary=0.
func (p *RpcPlugin) RemoveManagedRoutes(ro *v1alpha1.Rollout) pluginTypes.RpcError {
	return p.SetWeight(ro, 0, nil)
}

func (p *RpcPlugin) Type() string {
	return PluginName
}
