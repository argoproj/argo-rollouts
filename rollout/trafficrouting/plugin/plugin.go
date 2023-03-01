package plugin

import (
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/plugin/client"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/plugin/rpc"
	"github.com/argoproj/argo-rollouts/utils/record"
	"k8s.io/client-go/kubernetes"
)

type ReconcilerConfig struct {
	Rollout    *v1alpha1.Rollout
	PluginName string
	Client     kubernetes.Interface
	Recorder   record.EventRecorder
}

type Reconciler struct {
	Rollout    *v1alpha1.Rollout
	PluginName string
	Client     kubernetes.Interface
	Recorder   record.EventRecorder
	rpc.TrafficRouterPlugin
}

func NewReconciler(cfg *ReconcilerConfig) (*Reconciler, error) {
	pluginClient, err := client.GetTrafficPlugin(cfg.PluginName)
	if err != nil {
		return nil, fmt.Errorf("failed to get traffic router plugin %s: %w", cfg.PluginName, err)
	}

	reconciler := &Reconciler{
		Rollout:             cfg.Rollout,
		Client:              cfg.Client,
		Recorder:            cfg.Recorder,
		PluginName:          cfg.PluginName,
		TrafficRouterPlugin: pluginClient,
	}
	return reconciler, nil
}

// UpdateHash informs a traffic routing reconciler about new canary, stable, and additionalDestination(s) pod hashes
func (r *Reconciler) UpdateHash(canaryHash, stableHash string, additionalDestinations ...v1alpha1.WeightDestination) error {
	resp := r.TrafficRouterPlugin.UpdateHash(r.Rollout, canaryHash, stableHash, additionalDestinations)
	if resp.HasError() {
		return fmt.Errorf("failed to update hash via plugin: %w", resp)
	}
	return nil
}

// SetWeight sets the canary weight to the desired weight
func (r *Reconciler) SetWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
	resp := r.TrafficRouterPlugin.SetWeight(r.Rollout, desiredWeight, additionalDestinations)
	if resp.HasError() {
		return fmt.Errorf("failed to set weight via plugin: %w", resp)
	}
	return nil
}

// SetHeaderRoute sets the header routing step
func (r *Reconciler) SetHeaderRoute(headerRouting *v1alpha1.SetHeaderRoute) error {
	resp := r.TrafficRouterPlugin.SetHeaderRoute(r.Rollout, headerRouting)
	if resp.HasError() {
		return fmt.Errorf("failed to set header route via plugin: %w", resp)
	}
	return nil
}

// VerifyWeight returns true if the canary is at the desired weight and additionalDestinations are at the weights specified
// Returns nil if weight verification is not supported or not applicable
func (r *Reconciler) VerifyWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error) {
	verified, errResp := r.TrafficRouterPlugin.VerifyWeight(r.Rollout, desiredWeight, additionalDestinations)
	if errResp.HasError() {
		return verified.IsVerified(), fmt.Errorf("failed to verify weight via plugin: %w", errResp)
	}
	return verified.IsVerified(), nil
}

// SetMirrorRoute sets up the traffic router to mirror traffic to a service
func (r *Reconciler) SetMirrorRoute(setMirrorRoute *v1alpha1.SetMirrorRoute) error {
	resp := r.TrafficRouterPlugin.SetMirrorRoute(r.Rollout, setMirrorRoute)
	if resp.HasError() {
		return fmt.Errorf("failed to set mirror route via plugin: %w", resp)
	}
	return nil
}

// RemoveManagedRoutes Removes all routes that are managed by rollouts by looking at spec.strategy.canary.trafficRouting.managedRoutes
func (r *Reconciler) RemoveManagedRoutes() error {
	resp := r.TrafficRouterPlugin.RemoveManagedRoutes(r.Rollout)
	if resp.HasError() {
		return fmt.Errorf("failed to remove managed routes via plugin: %w", resp)
	}
	return nil
}
