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

// RolloutPluginArgs carries the RolloutPlugin, shared by every RPC method whose only
// argument is the RolloutPlugin (Validate, GetResourceStatus, PromoteFull, Abort, Restart).
type RolloutPluginArgs struct {
	RolloutPlugin v1alpha1.RolloutPlugin
}

type SetWeightArgs struct {
	RolloutPlugin v1alpha1.RolloutPlugin
	Weight        int32
}

type VerifyWeightArgs struct {
	RolloutPlugin v1alpha1.RolloutPlugin
	Weight        int32
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

type WatchedGVKResponse struct {
	GVK   types.WatchedGVK
	Error types.RpcError
}

func init() {
	gob.RegisterName("rolloutplugin.InitPluginArgs", new(InitPluginArgs))
	gob.RegisterName("rolloutplugin.RolloutPluginArgs", new(RolloutPluginArgs))
	gob.RegisterName("rolloutplugin.SetWeightArgs", new(SetWeightArgs))
	gob.RegisterName("rolloutplugin.VerifyWeightArgs", new(VerifyWeightArgs))
	gob.RegisterName("rolloutplugin.GetResourceStatusResponse", new(GetResourceStatusResponse))
	gob.RegisterName("rolloutplugin.VerifyWeightResponse", new(VerifyWeightResponse))
	gob.RegisterName("rolloutplugin.WatchedGVKResponse", new(WatchedGVKResponse))
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

// WatchedGVK returns the GroupVersionKind of the workload resource this plugin manages
func (c *PluginRPCClient) WatchedGVK() (types.WatchedGVK, types.RpcError) {
	var resp WatchedGVKResponse
	err := c.client.Call("Plugin.WatchedGVK", new(any), &resp)
	if err != nil {
		return types.WatchedGVK{}, types.RpcError{ErrorString: fmt.Sprintf("WatchedGVK rpc call error: %s", err)}
	}
	return resp.GVK, resp.Error
}

// GetResourceStatus gets the current status of the workload
func (c *PluginRPCClient) GetResourceStatus(rolloutPlugin *v1alpha1.RolloutPlugin) (*types.ResourceStatus, types.RpcError) {
	var resp GetResourceStatusResponse
	var args any = RolloutPluginArgs{RolloutPlugin: *rolloutPlugin}
	err := c.client.Call("Plugin.GetResourceStatus", &args, &resp)
	if err != nil {
		return nil, types.RpcError{ErrorString: fmt.Sprintf("GetResourceStatus rpc call error: %s", err)}
	}
	return resp.Status, resp.Error
}

// SetWeight sets the canary weight
func (c *PluginRPCClient) SetWeight(rolloutPlugin *v1alpha1.RolloutPlugin, weight int32) types.RpcError {
	var resp types.RpcError
	var args any = SetWeightArgs{RolloutPlugin: *rolloutPlugin, Weight: weight}
	err := c.client.Call("Plugin.SetWeight", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("SetWeight rpc call error: %s", err)}
	}
	return resp
}

// VerifyWeight verifies that the canary weight has been achieved
func (c *PluginRPCClient) VerifyWeight(rolloutPlugin *v1alpha1.RolloutPlugin, weight int32) (bool, types.RpcError) {
	var resp VerifyWeightResponse
	var args any = VerifyWeightArgs{RolloutPlugin: *rolloutPlugin, Weight: weight}
	err := c.client.Call("Plugin.VerifyWeight", &args, &resp)
	if err != nil {
		return false, types.RpcError{ErrorString: fmt.Sprintf("VerifyWeight rpc call error: %s", err)}
	}
	return resp.Verified, resp.Error
}

// PromoteFull skips remaining steps and promotes new version to stable
func (c *PluginRPCClient) PromoteFull(rolloutPlugin *v1alpha1.RolloutPlugin) types.RpcError {
	var resp types.RpcError
	var args any = RolloutPluginArgs{RolloutPlugin: *rolloutPlugin}
	err := c.client.Call("Plugin.PromoteFull", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("PromoteFull rpc call error: %s", err)}
	}
	return resp
}

// Validate checks the plugin-specific parts of the RolloutPlugin spec
func (c *PluginRPCClient) Validate(rolloutPlugin *v1alpha1.RolloutPlugin) types.RpcError {
	var resp types.RpcError
	var args any = RolloutPluginArgs{RolloutPlugin: *rolloutPlugin}
	err := c.client.Call("Plugin.Validate", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("Validate rpc call error: %s", err)}
	}
	return resp
}

// Abort aborts the rollout
func (c *PluginRPCClient) Abort(rolloutPlugin *v1alpha1.RolloutPlugin) types.RpcError {
	var resp types.RpcError
	var args any = RolloutPluginArgs{RolloutPlugin: *rolloutPlugin}
	err := c.client.Call("Plugin.Abort", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("Abort rpc call error: %s", err)}
	}
	return resp
}

// Restart returns the workload to baseline state for restart
func (c *PluginRPCClient) Restart(rolloutPlugin *v1alpha1.RolloutPlugin) types.RpcError {
	var resp types.RpcError
	var args any = RolloutPluginArgs{RolloutPlugin: *rolloutPlugin}
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

// WatchedGVK handles the WatchedGVK RPC call
func (s *PluginRPCServer) WatchedGVK(args any, resp *WatchedGVKResponse) error {
	gvk, rpcErr := s.Impl.WatchedGVK()
	resp.GVK = gvk
	resp.Error = rpcErr
	return nil
}

// GetResourceStatus handles the GetResourceStatus RPC call
func (s *PluginRPCServer) GetResourceStatus(args any, resp *GetResourceStatusResponse) error {
	getStatusArgs, ok := args.(*RolloutPluginArgs)
	if !ok {
		resp.Error = types.RpcError{ErrorString: fmt.Sprintf("invalid args %v", args)}
		return nil
	}
	status, rpcErr := s.Impl.GetResourceStatus(&getStatusArgs.RolloutPlugin)
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
	*resp = s.Impl.SetWeight(&setWeightArgs.RolloutPlugin, setWeightArgs.Weight)
	return nil
}

// VerifyWeight handles the VerifyWeight RPC call
func (s *PluginRPCServer) VerifyWeight(args any, resp *VerifyWeightResponse) error {
	verifyWeightArgs, ok := args.(*VerifyWeightArgs)
	if !ok {
		resp.Error = types.RpcError{ErrorString: fmt.Sprintf("invalid args %v", args)}
		return nil
	}
	verified, rpcErr := s.Impl.VerifyWeight(&verifyWeightArgs.RolloutPlugin, verifyWeightArgs.Weight)
	resp.Verified = verified
	resp.Error = rpcErr
	return nil
}

// PromoteFull handles the PromoteFull RPC call
func (s *PluginRPCServer) PromoteFull(args any, resp *types.RpcError) error {
	promoteFullArgs, ok := args.(*RolloutPluginArgs)
	if !ok {
		*resp = types.RpcError{ErrorString: fmt.Sprintf("invalid args %v", args)}
		return nil
	}
	*resp = s.Impl.PromoteFull(&promoteFullArgs.RolloutPlugin)
	return nil
}

// Validate handles the Validate RPC call
func (s *PluginRPCServer) Validate(args any, resp *types.RpcError) error {
	validateArgs, ok := args.(*RolloutPluginArgs)
	if !ok {
		*resp = types.RpcError{ErrorString: fmt.Sprintf("invalid args %v", args)}
		return nil
	}
	*resp = s.Impl.Validate(&validateArgs.RolloutPlugin)
	return nil
}

// Abort handles the Abort RPC call
func (s *PluginRPCServer) Abort(args any, resp *types.RpcError) error {
	abortArgs, ok := args.(*RolloutPluginArgs)
	if !ok {
		*resp = types.RpcError{ErrorString: fmt.Sprintf("invalid args %v", args)}
		return nil
	}
	*resp = s.Impl.Abort(&abortArgs.RolloutPlugin)
	return nil
}

// Restart handles the Restart RPC call
func (s *PluginRPCServer) Restart(args any, resp *types.RpcError) error {
	restartArgs, ok := args.(*RolloutPluginArgs)
	if !ok {
		*resp = types.RpcError{ErrorString: fmt.Sprintf("invalid args %v", args)}
		return nil
	}
	*resp = s.Impl.Restart(&restartArgs.RolloutPlugin)
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
