package plugin

import (
	"context"
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rolloutplugin"
	"github.com/argoproj/argo-rollouts/rolloutplugin/plugin/client"
	"github.com/argoproj/argo-rollouts/rolloutplugin/plugin/rpc"
)

const ProviderType = "RPCPlugin"

// RolloutPluginWrapper wraps the RPC plugin to implement the controller's ResourcePlugin interface
type RolloutPluginWrapper struct {
	rpc.ResourcePlugin
}

// NewRolloutPlugin wraps a direct plugin implementation (built-in mode)
// This is used when the plugin is compiled directly into the controller
func NewRolloutPlugin(plugin rpc.ResourcePlugin) rolloutplugin.ResourcePlugin {
	return RolloutPluginWrapper{
		ResourcePlugin: plugin,
	}
}

// NewRpcPlugin returns a new RPC plugin with a singleton client
// This is called when the plugin is loaded dynamically from an external executable
func NewRpcPlugin(pluginName string) (rolloutplugin.ResourcePlugin, error) {
	pluginClient, err := client.GetResourcePlugin(pluginName)
	if err != nil {
		return nil, fmt.Errorf("unable to get rollout plugin: %w", err)
	}

	return RolloutPluginWrapper{
		ResourcePlugin: pluginClient,
	}, nil
}

// Adapt the RPC interface to the controller interface by adding context support

// Init initializes the plugin (adapts InitPlugin for controller interface)
func (r RolloutPluginWrapper) Init() error {
	rpcErr := r.ResourcePlugin.InitPlugin()
	if rpcErr.HasError() {
		return fmt.Errorf("failed to initialize plugin: %s", rpcErr.ErrorString)
	}
	return nil
}

// GetResourceStatus gets the current status of the workload (adapts RPC interface)
func (r RolloutPluginWrapper) GetResourceStatus(ctx context.Context, workloadRef v1alpha1.WorkloadRef) (*rolloutplugin.ResourceStatus, error) {
	rpcStatus, rpcErr := r.ResourcePlugin.GetResourceStatus(workloadRef)
	if rpcErr.HasError() {
		return nil, fmt.Errorf("failed to get resource status: %s", rpcErr.ErrorString)
	}

	// Convert RPC ResourceStatus to controller ResourceStatus
	return &rolloutplugin.ResourceStatus{
		Replicas:          rpcStatus.Replicas,
		UpdatedReplicas:   rpcStatus.UpdatedReplicas,
		ReadyReplicas:     rpcStatus.ReadyReplicas,
		AvailableReplicas: rpcStatus.AvailableReplicas,
		CurrentRevision:   rpcStatus.CurrentRevision,
		UpdatedRevision:   rpcStatus.UpdatedRevision,
		Ready:             rpcStatus.Ready,
	}, nil
}

// SetWeight sets the canary weight (adapts RPC interface)
func (r RolloutPluginWrapper) SetWeight(ctx context.Context, workloadRef v1alpha1.WorkloadRef, weight int32) error {
	rpcErr := r.ResourcePlugin.SetWeight(workloadRef, weight)
	if rpcErr.HasError() {
		return fmt.Errorf("failed to set weight: %s", rpcErr.ErrorString)
	}
	return nil
}

// VerifyWeight verifies that the canary weight has been achieved (adapts RPC interface)
func (r RolloutPluginWrapper) VerifyWeight(ctx context.Context, workloadRef v1alpha1.WorkloadRef, weight int32) (bool, error) {
	verified, rpcErr := r.ResourcePlugin.VerifyWeight(workloadRef, weight)
	if rpcErr.HasError() {
		return false, fmt.Errorf("failed to verify weight: %s", rpcErr.ErrorString)
	}
	return verified, nil
}

// Promote completes the rollout (adapts RPC interface)
func (r RolloutPluginWrapper) Promote(ctx context.Context, workloadRef v1alpha1.WorkloadRef) error {
	rpcErr := r.ResourcePlugin.Promote(workloadRef)
	if rpcErr.HasError() {
		return fmt.Errorf("failed to promote: %s", rpcErr.ErrorString)
	}
	return nil
}

// Abort aborts the rollout (adapts RPC interface)
func (r RolloutPluginWrapper) Abort(ctx context.Context, workloadRef v1alpha1.WorkloadRef) error {
	rpcErr := r.ResourcePlugin.Abort(workloadRef)
	if rpcErr.HasError() {
		return fmt.Errorf("failed to abort: %s", rpcErr.ErrorString)
	}
	return nil
}
