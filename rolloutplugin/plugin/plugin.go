package plugin

import (
	"context"
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rolloutplugin"
	"github.com/argoproj/argo-rollouts/rolloutplugin/plugin/client"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

const ResourceType = "RPCPlugin"

// RpcPluginWrapper wraps an external RPC plugin to implement the controller's ResourcePlugin interface.
// This adapter is ONLY needed for external plugins that communicate via RPC.
// Built-in plugins (like StatefulSet) directly implement rolloutplugin.ResourcePlugin
// and don't need this wrapper.
//
// The wrapper only converts RpcError to error - no struct conversions needed since
// both interfaces use the shared types.ResourceStatus struct.
type RpcPluginWrapper struct {
	types.RpcResourcePlugin
}

// NewRpcPlugin returns a new RPC plugin wrapper with a singleton client.
// This is called when the plugin is loaded dynamically from an external executable.
// For built-in plugins, use the plugin directly (they implement ResourcePlugin natively).
func NewRpcPlugin(pluginName string) (rolloutplugin.ResourcePlugin, error) {
	pluginClient, err := client.GetResourcePlugin(pluginName)
	if err != nil {
		return nil, fmt.Errorf("unable to get rollout plugin: %w", err)
	}

	return RpcPluginWrapper{
		RpcResourcePlugin: pluginClient,
	}, nil
}

// The following methods adapt the RPC interface to the controller interface.
// The only difference is: RpcError -> error, and context.Context is added (but ignored for RPC).

// Init initializes the plugin
func (r RpcPluginWrapper) Init() error {
	rpcErr := r.RpcResourcePlugin.InitPlugin()
	if rpcErr.HasError() {
		return fmt.Errorf("failed to initialize plugin: %s", rpcErr.ErrorString)
	}
	return nil
}

// GetResourceStatus gets the current status of the workload.
// No struct conversion needed - both interfaces use types.ResourceStatus.
func (r RpcPluginWrapper) GetResourceStatus(ctx context.Context, workloadRef v1alpha1.WorkloadRef) (*rolloutplugin.ResourceStatus, error) {
	status, rpcErr := r.RpcResourcePlugin.GetResourceStatus(workloadRef)
	if rpcErr.HasError() {
		return nil, fmt.Errorf("failed to get resource status: %s", rpcErr.ErrorString)
	}
	return status, nil
}

// SetWeight sets the canary weight
func (r RpcPluginWrapper) SetWeight(ctx context.Context, workloadRef v1alpha1.WorkloadRef, weight int32) error {
	rpcErr := r.RpcResourcePlugin.SetWeight(workloadRef, weight)
	if rpcErr.HasError() {
		return fmt.Errorf("failed to set weight: %s", rpcErr.ErrorString)
	}
	return nil
}

// VerifyWeight verifies that the canary weight has been achieved
func (r RpcPluginWrapper) VerifyWeight(ctx context.Context, workloadRef v1alpha1.WorkloadRef, weight int32) (bool, error) {
	verified, rpcErr := r.RpcResourcePlugin.VerifyWeight(workloadRef, weight)
	if rpcErr.HasError() {
		return false, fmt.Errorf("failed to verify weight: %s", rpcErr.ErrorString)
	}
	return verified, nil
}

// Promote completes the rollout
func (r RpcPluginWrapper) Promote(ctx context.Context, workloadRef v1alpha1.WorkloadRef) error {
	rpcErr := r.RpcResourcePlugin.Promote(workloadRef)
	if rpcErr.HasError() {
		return fmt.Errorf("failed to promote: %s", rpcErr.ErrorString)
	}
	return nil
}

// Abort aborts the rollout
func (r RpcPluginWrapper) Abort(ctx context.Context, workloadRef v1alpha1.WorkloadRef) error {
	rpcErr := r.RpcResourcePlugin.Abort(workloadRef)
	if rpcErr.HasError() {
		return fmt.Errorf("failed to abort: %s", rpcErr.ErrorString)
	}
	return nil
}

func (r RpcPluginWrapper) Restart(ctx context.Context, workloadRef v1alpha1.WorkloadRef) error {
	rpcErr := r.RpcResourcePlugin.Restart(workloadRef)
	if rpcErr.HasError() {
		return fmt.Errorf("failed to reset: %s", rpcErr.ErrorString)
	}
	return nil
}
