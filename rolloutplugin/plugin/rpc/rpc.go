package rpc

import (
	"encoding/gob"
	"fmt"
	"net/rpc"

	"github.com/hashicorp/go-plugin"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

// Args for RPC calls
type InitPluginArgs struct {
	Namespace string
}

type GetResourceStatusArgs struct {
	WorkloadRef v1alpha1.WorkloadRef
}

type SetWeightArgs struct {
	WorkloadRef v1alpha1.WorkloadRef
	Weight      int32
}

type VerifyWeightArgs struct {
	WorkloadRef v1alpha1.WorkloadRef
	Weight      int32
}

type PromoteFullArgs struct {
	WorkloadRef v1alpha1.WorkloadRef
}

type AbortArgs struct {
	WorkloadRef v1alpha1.WorkloadRef
}

type RestartArgs struct {
	WorkloadRef v1alpha1.WorkloadRef
}

// Responses for RPC calls
type GetResourceStatusResponse struct {
	Status *types.ResourceStatus
	Error  types.RpcError
}

type VerifyWeightResponse struct {
	Verified bool
	Error    types.RpcError
}

func init() {
	gob.RegisterName("rolloutplugin.InitPluginArgs", new(InitPluginArgs))
	gob.RegisterName("rolloutplugin.GetResourceStatusArgs", new(GetResourceStatusArgs))
	gob.RegisterName("rolloutplugin.SetWeightArgs", new(SetWeightArgs))
	gob.RegisterName("rolloutplugin.VerifyWeightArgs", new(VerifyWeightArgs))
	gob.RegisterName("rolloutplugin.PromoteFullArgs", new(PromoteFullArgs))
	gob.RegisterName("rolloutplugin.AbortArgs", new(AbortArgs))
	gob.RegisterName("rolloutplugin.RestartArgs", new(RestartArgs))
	gob.RegisterName("rolloutplugin.GetResourceStatusResponse", new(GetResourceStatusResponse))
	gob.RegisterName("rolloutplugin.VerifyWeightResponse", new(VerifyWeightResponse))
}

// ResourcePlugin is an alias for the shared RPC interface.
// External plugins implement this interface.
type ResourcePlugin = types.RpcResourcePlugin

// PluginRPCClient is the RPC client implementation
type PluginRPCClient struct {
	client *rpc.Client
}

// InitPlugin calls the plugin's initialization
func (c *PluginRPCClient) InitPlugin(namespace string) types.RpcError {
	var resp types.RpcError
	var args any = InitPluginArgs{Namespace: namespace}
	err := c.client.Call("Plugin.InitPlugin", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("InitPlugin rpc call error: %s", err)}
	}
	return resp
}

// GetResourceStatus gets the current status of the workload
func (c *PluginRPCClient) GetResourceStatus(workloadRef v1alpha1.WorkloadRef) (*types.ResourceStatus, types.RpcError) {
	var resp GetResourceStatusResponse
	var args any = GetResourceStatusArgs{WorkloadRef: workloadRef}
	err := c.client.Call("Plugin.GetResourceStatus", &args, &resp)
	if err != nil {
		return nil, types.RpcError{ErrorString: fmt.Sprintf("GetResourceStatus rpc call error: %s", err)}
	}
	return resp.Status, resp.Error
}

// SetWeight sets the canary weight
func (c *PluginRPCClient) SetWeight(workloadRef v1alpha1.WorkloadRef, weight int32) types.RpcError {
	var resp types.RpcError
	var args any = SetWeightArgs{WorkloadRef: workloadRef, Weight: weight}
	err := c.client.Call("Plugin.SetWeight", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("SetWeight rpc call error: %s", err)}
	}
	return resp
}

// VerifyWeight verifies that the canary weight has been achieved
func (c *PluginRPCClient) VerifyWeight(workloadRef v1alpha1.WorkloadRef, weight int32) (bool, types.RpcError) {
	var resp VerifyWeightResponse
	var args any = VerifyWeightArgs{WorkloadRef: workloadRef, Weight: weight}
	err := c.client.Call("Plugin.VerifyWeight", &args, &resp)
	if err != nil {
		return false, types.RpcError{ErrorString: fmt.Sprintf("VerifyWeight rpc call error: %s", err)}
	}
	return resp.Verified, resp.Error
}

// PromoteFull skips remaining steps and promotes new version to stable
func (c *PluginRPCClient) PromoteFull(workloadRef v1alpha1.WorkloadRef) types.RpcError {
	var resp types.RpcError
	var args any = PromoteFullArgs{WorkloadRef: workloadRef}
	err := c.client.Call("Plugin.PromoteFull", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("PromoteFull rpc call error: %s", err)}
	}
	return resp
}

// Abort aborts the rollout
func (c *PluginRPCClient) Abort(workloadRef v1alpha1.WorkloadRef) types.RpcError {
	var resp types.RpcError
	var args any = AbortArgs{WorkloadRef: workloadRef}
	err := c.client.Call("Plugin.Abort", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("Abort rpc call error: %s", err)}
	}
	return resp
}

// Restart returns the workload to baseline state for restart
func (c *PluginRPCClient) Restart(workloadRef v1alpha1.WorkloadRef) types.RpcError {
	var resp types.RpcError
	var args any = RestartArgs{WorkloadRef: workloadRef}
	err := c.client.Call("Plugin.Restart", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("Restart rpc call error: %s", err)}
	}
	return resp
}

// Type returns the type of the resource plugin
func (c *PluginRPCClient) Type() string {
	var resp string
	err := c.client.Call("Plugin.Type", new(any), &resp)
	if err != nil {
		return fmt.Sprintf("Type rpc call error: %s", err)
	}
	return resp
}

// PluginRPCServer is the RPC server implementation
type PluginRPCServer struct {
	// This is the real implementation
	Impl ResourcePlugin
}

// InitPlugin handles the InitPlugin RPC call
func (s *PluginRPCServer) InitPlugin(args any, resp *types.RpcError) error {
	initArgs, ok := args.(*InitPluginArgs)
	if !ok {
		*resp = types.RpcError{ErrorString: fmt.Sprintf("invalid args %v", args)}
		return nil
	}
	*resp = s.Impl.InitPlugin(initArgs.Namespace)
	return nil
}

// GetResourceStatus handles the GetResourceStatus RPC call
func (s *PluginRPCServer) GetResourceStatus(args any, resp *GetResourceStatusResponse) error {
	getStatusArgs, ok := args.(*GetResourceStatusArgs)
	if !ok {
		resp.Error = types.RpcError{ErrorString: fmt.Sprintf("invalid args %v", args)}
		return nil
	}
	status, rpcErr := s.Impl.GetResourceStatus(getStatusArgs.WorkloadRef)
	resp.Status = status
	resp.Error = rpcErr
	return nil
}

// SetWeight handles the SetWeight RPC call
func (s *PluginRPCServer) SetWeight(args any, resp *types.RpcError) error {
	setWeightArgs, ok := args.(*SetWeightArgs)
	if !ok {
		*resp = types.RpcError{ErrorString: fmt.Sprintf("invalid args %v", args)}
		return nil
	}
	*resp = s.Impl.SetWeight(setWeightArgs.WorkloadRef, setWeightArgs.Weight)
	return nil
}

// VerifyWeight handles the VerifyWeight RPC call
func (s *PluginRPCServer) VerifyWeight(args any, resp *VerifyWeightResponse) error {
	verifyWeightArgs, ok := args.(*VerifyWeightArgs)
	if !ok {
		resp.Error = types.RpcError{ErrorString: fmt.Sprintf("invalid args %v", args)}
		return nil
	}
	verified, rpcErr := s.Impl.VerifyWeight(verifyWeightArgs.WorkloadRef, verifyWeightArgs.Weight)
	resp.Verified = verified
	resp.Error = rpcErr
	return nil
}

// PromoteFull handles the PromoteFull RPC call
func (s *PluginRPCServer) PromoteFull(args any, resp *types.RpcError) error {
	promoteFullArgs, ok := args.(*PromoteFullArgs)
	if !ok {
		*resp = types.RpcError{ErrorString: fmt.Sprintf("invalid args %v", args)}
		return nil
	}
	*resp = s.Impl.PromoteFull(promoteFullArgs.WorkloadRef)
	return nil
}

// Abort handles the Abort RPC call
func (s *PluginRPCServer) Abort(args any, resp *types.RpcError) error {
	abortArgs, ok := args.(*AbortArgs)
	if !ok {
		*resp = types.RpcError{ErrorString: fmt.Sprintf("invalid args %v", args)}
		return nil
	}
	*resp = s.Impl.Abort(abortArgs.WorkloadRef)
	return nil
}

// Restart handles the Restart RPC call
func (s *PluginRPCServer) Restart(args any, resp *types.RpcError) error {
	restartArgs, ok := args.(*RestartArgs)
	if !ok {
		*resp = types.RpcError{ErrorString: fmt.Sprintf("invalid args %v", args)}
		return nil
	}
	*resp = s.Impl.Restart(restartArgs.WorkloadRef)
	return nil
}

// Type handles the Type RPC call
func (s *PluginRPCServer) Type(args any, resp *string) error {
	*resp = s.Impl.Type()
	return nil
}

// ResourcePluginImpl is the implementation of plugin.Plugin
type ResourcePluginImpl struct {
	// Impl is the concrete implementation
	Impl ResourcePlugin
}

func (p *ResourcePluginImpl) Server(*plugin.MuxBroker) (any, error) {
	return &PluginRPCServer{Impl: p.Impl}, nil
}

func (ResourcePluginImpl) Client(b *plugin.MuxBroker, c *rpc.Client) (any, error) {
	return &PluginRPCClient{client: c}, nil
}
