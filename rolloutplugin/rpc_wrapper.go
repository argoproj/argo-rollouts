package rolloutplugin

import (
	"context"
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rolloutplugin/plugin/client"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

// RpcPluginWrapper wraps an external RPC plugin to implement the controller's ResourcePlugin interface.
// The wrapper only converts RpcError to error
type RpcPluginWrapper struct {
	types.RpcResourcePlugin
}

// NewRpcPlugin creates an RPC-backed ResourcePlugin from a plugin name.
// It is called lazily by the PluginManager when an external plugin is requested.
func NewRpcPlugin(pluginName, namespace string) (ResourcePlugin, error) {
	pluginClient, err := client.GetResourcePlugin(pluginName, namespace)
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
func (r RpcPluginWrapper) Init(namespace string) error {
	rpcErr := r.RpcResourcePlugin.InitPlugin(namespace)
	if rpcErr.HasError() {
		return fmt.Errorf("failed to initialize plugin: %s", rpcErr.ErrorString)
	}
	return nil
}

// GetResourceStatus gets the current status of the workload.
func (r RpcPluginWrapper) GetResourceStatus(ctx context.Context, workloadRef v1alpha1.WorkloadRef) (*ResourceStatus, error) {
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

// PromoteFull skips all remaining steps and promotes the new version to stable immediately
func (r RpcPluginWrapper) PromoteFull(ctx context.Context, workloadRef v1alpha1.WorkloadRef) error {
	rpcErr := r.RpcResourcePlugin.PromoteFull(workloadRef)
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

// Restart restarts aborted rollout
func (r RpcPluginWrapper) Restart(ctx context.Context, workloadRef v1alpha1.WorkloadRef) error {
	rpcErr := r.RpcResourcePlugin.Restart(workloadRef)
	if rpcErr.HasError() {
		return fmt.Errorf("failed to restart: %s", rpcErr.ErrorString)
	}
	return nil
}
