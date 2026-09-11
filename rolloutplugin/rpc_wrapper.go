package rolloutplugin

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"

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

// asError converts an RpcError into a standard error, wrapping it with %w so the
// underlying RpcError (which implements error) stays inspectable via errors.As/Is.
// It returns nil when the RpcError carries no error.
func asError(e types.RpcError, verb string) error {
	if e.HasError() {
		return fmt.Errorf("failed to %s: %w", verb, e)
	}
	return nil
}

// The following methods adapt the RPC interface to the controller interface: they convert
// RpcError -> error and accept a context.Context. The ctx is part of the ResourcePlugin
// contract but is not forwarded — the underlying net/rpc calls are synchronous and have no
// cancellation channel.

// Init initializes the plugin
func (r RpcPluginWrapper) Init(namespace string) error {
	return asError(r.RpcResourcePlugin.InitPlugin(namespace), "initialize plugin")
}

// WatchedGVK returns the GroupVersionKind of the workload resource this plugin manages.
func (r RpcPluginWrapper) WatchedGVK() (schema.GroupVersionKind, error) {
	gvk, rpcErr := r.RpcResourcePlugin.WatchedGVK()
	if err := asError(rpcErr, "get watched GVK"); err != nil {
		return schema.GroupVersionKind{}, err
	}
	return schema.GroupVersionKind{Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind}, nil
}

// GetResourceStatus gets the current status of the workload.
func (r RpcPluginWrapper) GetResourceStatus(ctx context.Context, namespace string, workloadRef v1alpha1.WorkloadRef) (*ResourceStatus, error) {
	status, rpcErr := r.RpcResourcePlugin.GetResourceStatus(namespace, workloadRef)
	return status, asError(rpcErr, "get resource status")
}

// SetWeight sets the canary weight
func (r RpcPluginWrapper) SetWeight(ctx context.Context, namespace string, workloadRef v1alpha1.WorkloadRef, weight int32) error {
	return asError(r.RpcResourcePlugin.SetWeight(namespace, workloadRef, weight), "set weight")
}

// VerifyWeight verifies that the canary weight has been achieved
func (r RpcPluginWrapper) VerifyWeight(ctx context.Context, namespace string, workloadRef v1alpha1.WorkloadRef, weight int32) (bool, error) {
	verified, rpcErr := r.RpcResourcePlugin.VerifyWeight(namespace, workloadRef, weight)
	return verified, asError(rpcErr, "verify weight")
}

// PromoteFull skips all remaining steps and promotes the new version to stable immediately
func (r RpcPluginWrapper) PromoteFull(ctx context.Context, namespace string, workloadRef v1alpha1.WorkloadRef) error {
	return asError(r.RpcResourcePlugin.PromoteFull(namespace, workloadRef), "promote")
}

// Abort aborts the rollout
func (r RpcPluginWrapper) Abort(ctx context.Context, namespace string, workloadRef v1alpha1.WorkloadRef) error {
	return asError(r.RpcResourcePlugin.Abort(namespace, workloadRef), "abort")
}

// Restart restarts aborted rollout
func (r RpcPluginWrapper) Restart(ctx context.Context, namespace string, workloadRef v1alpha1.WorkloadRef) error {
	return asError(r.RpcResourcePlugin.Restart(namespace, workloadRef), "restart")
}
