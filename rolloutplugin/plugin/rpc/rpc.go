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
type InitPluginArgs struct{}

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

type PromoteArgs struct {
	WorkloadRef v1alpha1.WorkloadRef
}

type AbortArgs struct {
	WorkloadRef v1alpha1.WorkloadRef
}

type ResetArgs struct {
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
	gob.RegisterName("rolloutplugin.PromoteArgs", new(PromoteArgs))
	gob.RegisterName("rolloutplugin.AbortArgs", new(AbortArgs))
	gob.RegisterName("rolloutplugin.ResetArgs", new(ResetArgs))
	gob.RegisterName("rolloutplugin.GetResourceStatusResponse", new(GetResourceStatusResponse))
	gob.RegisterName("rolloutplugin.VerifyWeightResponse", new(VerifyWeightResponse))
}

// ResourcePlugin is an alias for the shared RPC interface.
// External plugins implement this interface.
type ResourcePlugin = types.RpcResourcePlugin

// ResourcePluginRPC is the RPC client implementation
type ResourcePluginRPC struct {
	client *rpc.Client
}

// InitPlugin calls the plugin's initialization
func (g *ResourcePluginRPC) InitPlugin() types.RpcError {
	var resp types.RpcError
	err := g.client.Call("Plugin.InitPlugin", new(InitPluginArgs), &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("InitPlugin rpc call error: %s", err)}
	}
	return resp
}

// GetResourceStatus gets the current status of the workload
func (g *ResourcePluginRPC) GetResourceStatus(workloadRef v1alpha1.WorkloadRef) (*types.ResourceStatus, types.RpcError) {
	var resp GetResourceStatusResponse
	args := GetResourceStatusArgs{WorkloadRef: workloadRef}
	err := g.client.Call("Plugin.GetResourceStatus", &args, &resp)
	if err != nil {
		return nil, types.RpcError{ErrorString: fmt.Sprintf("GetResourceStatus rpc call error: %s", err)}
	}
	return resp.Status, resp.Error
}

// SetWeight sets the canary weight
func (g *ResourcePluginRPC) SetWeight(workloadRef v1alpha1.WorkloadRef, weight int32) types.RpcError {
	var resp types.RpcError
	args := SetWeightArgs{WorkloadRef: workloadRef, Weight: weight}
	err := g.client.Call("Plugin.SetWeight", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("SetWeight rpc call error: %s", err)}
	}
	return resp
}

// VerifyWeight verifies that the canary weight has been achieved
func (g *ResourcePluginRPC) VerifyWeight(workloadRef v1alpha1.WorkloadRef, weight int32) (bool, types.RpcError) {
	var resp VerifyWeightResponse
	args := VerifyWeightArgs{WorkloadRef: workloadRef, Weight: weight}
	err := g.client.Call("Plugin.VerifyWeight", &args, &resp)
	if err != nil {
		return false, types.RpcError{ErrorString: fmt.Sprintf("VerifyWeight rpc call error: %s", err)}
	}
	return resp.Verified, resp.Error
}

// Promote completes the rollout
func (g *ResourcePluginRPC) Promote(workloadRef v1alpha1.WorkloadRef) types.RpcError {
	var resp types.RpcError
	args := PromoteArgs{WorkloadRef: workloadRef}
	err := g.client.Call("Plugin.Promote", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("Promote rpc call error: %s", err)}
	}
	return resp
}

// Abort aborts the rollout
func (g *ResourcePluginRPC) Abort(workloadRef v1alpha1.WorkloadRef) types.RpcError {
	var resp types.RpcError
	args := AbortArgs{WorkloadRef: workloadRef}
	err := g.client.Call("Plugin.Abort", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("Abort rpc call error: %s", err)}
	}
	return resp
}

// Reset returns the workload to baseline state for retry
func (g *ResourcePluginRPC) Reset(workloadRef v1alpha1.WorkloadRef) types.RpcError {
	var resp types.RpcError
	args := ResetArgs{WorkloadRef: workloadRef}
	err := g.client.Call("Plugin.Reset", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("Reset rpc call error: %s", err)}
	}
	return resp
}

// Type returns the type of the resource plugin
func (g *ResourcePluginRPC) Type() string {
	var resp string
	err := g.client.Call("Plugin.Type", new(any), &resp)
	if err != nil {
		return fmt.Sprintf("Type rpc call error: %s", err)
	}
	return resp
}

// ResourcePluginRPCServer is the RPC server implementation
type ResourcePluginRPCServer struct {
	// This is the real implementation
	Impl ResourcePlugin
}

// InitPlugin handles the InitPlugin RPC call
func (s *ResourcePluginRPCServer) InitPlugin(args any, resp *types.RpcError) error {
	*resp = s.Impl.InitPlugin()
	return nil
}

// TODOH Return type?
// GetResourceStatus handles the GetResourceStatus RPC call
func (s *ResourcePluginRPCServer) GetResourceStatus(args any, resp *GetResourceStatusResponse) error {
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
func (s *ResourcePluginRPCServer) SetWeight(args any, resp *types.RpcError) error {
	setWeightArgs, ok := args.(*SetWeightArgs)
	if !ok {
		*resp = types.RpcError{ErrorString: fmt.Sprintf("invalid args %v", args)}
		return nil
	}
	*resp = s.Impl.SetWeight(setWeightArgs.WorkloadRef, setWeightArgs.Weight)
	return nil
}

// VerifyWeight handles the VerifyWeight RPC call
func (s *ResourcePluginRPCServer) VerifyWeight(args any, resp *VerifyWeightResponse) error {
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

// Promote handles the Promote RPC call
func (s *ResourcePluginRPCServer) Promote(args any, resp *types.RpcError) error {
	promoteArgs, ok := args.(*PromoteArgs)
	if !ok {
		*resp = types.RpcError{ErrorString: fmt.Sprintf("invalid args %v", args)}
		return nil
	}
	*resp = s.Impl.Promote(promoteArgs.WorkloadRef)
	return nil
}

// Abort handles the Abort RPC call
func (s *ResourcePluginRPCServer) Abort(args any, resp *types.RpcError) error {
	abortArgs, ok := args.(*AbortArgs)
	if !ok {
		*resp = types.RpcError{ErrorString: fmt.Sprintf("invalid args %v", args)}
		return nil
	}
	*resp = s.Impl.Abort(abortArgs.WorkloadRef)
	return nil
}

// Reset handles the Reset RPC call
func (s *ResourcePluginRPCServer) Reset(args any, resp *types.RpcError) error {
	resetArgs, ok := args.(*ResetArgs)
	if !ok {
		*resp = types.RpcError{ErrorString: fmt.Sprintf("invalid args %v", args)}
		return nil
	}
	*resp = s.Impl.Reset(resetArgs.WorkloadRef)
	return nil
}

// Type handles the Type RPC call
func (s *ResourcePluginRPCServer) Type(args any, resp *string) error {
	*resp = s.Impl.Type()
	return nil
}

// RpcResourcePlugin is the implementation of plugin.Plugin
type RpcResourcePlugin struct {
	// Impl is the concrete implementation
	Impl ResourcePlugin
}

func (p *RpcResourcePlugin) Server(*plugin.MuxBroker) (any, error) {
	return &ResourcePluginRPCServer{Impl: p.Impl}, nil
}

func (RpcResourcePlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (any, error) {
	return &ResourcePluginRPC{client: c}, nil
}
