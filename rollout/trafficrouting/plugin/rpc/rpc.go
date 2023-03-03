package rpc

import (
	"encoding/gob"
	"fmt"
	"net/rpc"

	"github.com/argoproj/argo-rollouts/utils/plugin/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/hashicorp/go-plugin"
)

type UpdateHashArgs struct {
	Rollout                v1alpha1.Rollout
	CanaryHash             string
	StableHash             string
	AdditionalDestinations []v1alpha1.WeightDestination
}

type SetWeightAndVerifyWeightArgs struct {
	Rollout                v1alpha1.Rollout
	DesiredWeight          int32
	AdditionalDestinations []v1alpha1.WeightDestination
}

type SetHeaderArgs struct {
	Rollout        v1alpha1.Rollout
	SetHeaderRoute v1alpha1.SetHeaderRoute
}

type SetMirrorArgs struct {
	Rollout        v1alpha1.Rollout
	SetMirrorRoute v1alpha1.SetMirrorRoute
}

type RemoveManagedRoutesArgs struct {
	Rollout v1alpha1.Rollout
}

type VerifyWeightResponse struct {
	Verified types.RpcVerified
	Err      types.RpcError
}

func init() {
	gob.RegisterName("UpdateHashArgs", new(UpdateHashArgs))
	gob.RegisterName("SetWeightAndVerifyWeightArgs", new(SetWeightAndVerifyWeightArgs))
	gob.RegisterName("SetHeaderArgs", new(SetHeaderArgs))
	gob.RegisterName("SetMirrorArgs", new(SetMirrorArgs))
	gob.RegisterName("RemoveManagedRoutesArgs", new(RemoveManagedRoutesArgs))
}

// TrafficRouterPlugin is the interface that we're exposing as a plugin. It needs to match metricproviders.Providers but we can
// not import that package because it would create a circular dependency.
type TrafficRouterPlugin interface {
	InitPlugin() types.RpcError
	types.RpcTrafficRoutingReconciler
}

// TrafficRouterPluginRPC Here is an implementation that talks over RPC
type TrafficRouterPluginRPC struct{ client *rpc.Client }

// NewTrafficRouterPlugin this is the client aka the controller side function that calls the server side rpc (plugin)
// this gets called once during startup of the plugin and can be used to set up informers or k8s clients etc.
func (g *TrafficRouterPluginRPC) InitPlugin() types.RpcError {
	var resp types.RpcError
	err := g.client.Call("Plugin.InitPlugin", new(interface{}), &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("InitPlugin rpc call error: %s", err)}
	}
	return resp
}

// UpdateHash informs a traffic routing reconciler about new canary, stable, and additionalDestination(s) pod hashes
func (g *TrafficRouterPluginRPC) UpdateHash(rollout *v1alpha1.Rollout, canaryHash string, stableHash string, additionalDestinations []v1alpha1.WeightDestination) types.RpcError {
	var resp types.RpcError
	var args interface{} = UpdateHashArgs{
		Rollout:                *rollout,
		CanaryHash:             canaryHash,
		StableHash:             stableHash,
		AdditionalDestinations: additionalDestinations,
	}
	err := g.client.Call("Plugin.UpdateHash", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("UpdateHash rpc call error: %s", err)}
	}
	return resp
}

// SetWeight sets the canary weight to the desired weight
func (g *TrafficRouterPluginRPC) SetWeight(rollout *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) types.RpcError {
	var resp types.RpcError
	var args interface{} = SetWeightAndVerifyWeightArgs{
		Rollout:                *rollout,
		DesiredWeight:          desiredWeight,
		AdditionalDestinations: additionalDestinations,
	}
	err := g.client.Call("Plugin.SetWeight", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("SetWeight rpc call error: %s", err)}
	}
	return resp
}

// SetHeaderRoute sets the header routing step
func (g *TrafficRouterPluginRPC) SetHeaderRoute(rollout *v1alpha1.Rollout, setHeaderRoute *v1alpha1.SetHeaderRoute) types.RpcError {
	var resp types.RpcError
	var args interface{} = SetHeaderArgs{
		Rollout:        *rollout,
		SetHeaderRoute: *setHeaderRoute,
	}
	err := g.client.Call("Plugin.SetHeaderRoute", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("SetHeaderRoute rpc call error: %s", err)}
	}
	return resp
}

// SetMirrorRoute sets up the traffic router to mirror traffic to a service
func (g *TrafficRouterPluginRPC) SetMirrorRoute(rollout *v1alpha1.Rollout, setMirrorRoute *v1alpha1.SetMirrorRoute) types.RpcError {
	var resp types.RpcError
	var args interface{} = SetMirrorArgs{
		Rollout:        *rollout,
		SetMirrorRoute: *setMirrorRoute,
	}
	err := g.client.Call("Plugin.SetMirrorRoute", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("SetMirrorRoute rpc call error: %s", err)}
	}
	return resp
}

// Type returns the type of the traffic routing reconciler
func (g *TrafficRouterPluginRPC) Type() string {
	var resp string
	err := g.client.Call("Plugin.Type", new(interface{}), &resp)
	if err != nil {
		return fmt.Sprintf("Type rpc call error: %s", err)
	}

	return resp
}

// VerifyWeight returns true if the canary is at the desired weight and additionalDestinations are at the weights specified
// Returns nil if weight verification is not supported or not applicable
func (g *TrafficRouterPluginRPC) VerifyWeight(rollout *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) (types.RpcVerified, types.RpcError) {
	var resp VerifyWeightResponse
	var args interface{} = SetWeightAndVerifyWeightArgs{
		Rollout:                *rollout,
		DesiredWeight:          desiredWeight,
		AdditionalDestinations: additionalDestinations,
	}
	err := g.client.Call("Plugin.VerifyWeight", &args, &resp)
	if err != nil {
		return types.NotVerified, types.RpcError{ErrorString: fmt.Sprintf("VerifyWeight rpc call error: %s", err)}
	}
	return resp.Verified, resp.Err
}

// RemoveAllRoutes Removes all routes that are managed by rollouts by looking at spec.strategy.canary.trafficRouting.managedRoutes
func (g *TrafficRouterPluginRPC) RemoveManagedRoutes(rollout *v1alpha1.Rollout) types.RpcError {
	var resp types.RpcError
	var args interface{} = RemoveManagedRoutesArgs{
		Rollout: *rollout,
	}
	err := g.client.Call("Plugin.RemoveManagedRoutes", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("RemoveManagedRoutes rpc call error: %s", err)}
	}
	return resp
}

// TrafficRouterRPCServer Here is the RPC server that MetricsPluginRPC talks to, conforming to
// the requirements of net/rpc
type TrafficRouterRPCServer struct {
	// This is the real implementation
	Impl TrafficRouterPlugin
}

// InitPlugin this is the server aka the controller side function that receives calls from the client side rpc (controller)
// this gets called once during startup of the plugin and can be used to set up informers or k8s clients etc.
func (s *TrafficRouterRPCServer) InitPlugin(args interface{}, resp *types.RpcError) error {
	*resp = s.Impl.InitPlugin()
	return nil
}

// UpdateHash informs a traffic routing reconciler about new canary, stable, and additionalDestination(s) pod hashes
func (s *TrafficRouterRPCServer) UpdateHash(args interface{}, resp *types.RpcError) error {
	runArgs, ok := args.(*UpdateHashArgs)
	if !ok {
		return fmt.Errorf("invalid args %s", args)
	}
	*resp = s.Impl.UpdateHash(&runArgs.Rollout, runArgs.CanaryHash, runArgs.StableHash, runArgs.AdditionalDestinations)
	return nil
}

// SetWeight sets the canary weight to the desired weight
func (s *TrafficRouterRPCServer) SetWeight(args interface{}, resp *types.RpcError) error {
	setWeigthArgs, ok := args.(*SetWeightAndVerifyWeightArgs)
	if !ok {
		return fmt.Errorf("invalid args %s", args)
	}
	*resp = s.Impl.SetWeight(&setWeigthArgs.Rollout, setWeigthArgs.DesiredWeight, setWeigthArgs.AdditionalDestinations)
	return nil
}

// SetHeaderRoute sets the header routing step
func (s *TrafficRouterRPCServer) SetHeaderRoute(args interface{}, resp *types.RpcError) error {
	setHeaderArgs, ok := args.(*SetHeaderArgs)
	if !ok {
		return fmt.Errorf("invalid args %s", args)
	}
	*resp = s.Impl.SetHeaderRoute(&setHeaderArgs.Rollout, &setHeaderArgs.SetHeaderRoute)
	return nil
}

// SetMirrorRoute sets up the traffic router to mirror traffic to a service
func (s *TrafficRouterRPCServer) SetMirrorRoute(args interface{}, resp *types.RpcError) error {
	setMirrorArgs, ok := args.(*SetMirrorArgs)
	if !ok {
		return fmt.Errorf("invalid args %s", args)
	}
	*resp = s.Impl.SetMirrorRoute(&setMirrorArgs.Rollout, &setMirrorArgs.SetMirrorRoute)
	return nil
}

// Type returns the type of the traffic routing reconciler
func (s *TrafficRouterRPCServer) Type(args interface{}, resp *string) error {
	*resp = s.Impl.Type()
	return nil
}

// VerifyWeight returns true if the canary is at the desired weight and additionalDestinations are at the weights specified
// Returns nil if weight verification is not supported or not applicable
func (s *TrafficRouterRPCServer) VerifyWeight(args interface{}, resp *VerifyWeightResponse) error {
	verifyWeightArgs, ok := args.(*SetWeightAndVerifyWeightArgs)
	if !ok {
		return fmt.Errorf("invalid args %s", args)
	}
	verified, err := s.Impl.VerifyWeight(&verifyWeightArgs.Rollout, verifyWeightArgs.DesiredWeight, verifyWeightArgs.AdditionalDestinations)
	*resp = VerifyWeightResponse{
		Verified: verified,
		Err:      err,
	}
	return nil
}

// RemoveAllRoutes Removes all routes that are managed by rollouts by looking at spec.strategy.canary.trafficRouting.managedRoutes
func (s *TrafficRouterRPCServer) RemoveManagedRoutes(args interface{}, resp *types.RpcError) error {
	removeManagedRoutesArgs, ok := args.(*RemoveManagedRoutesArgs)
	if !ok {
		return fmt.Errorf("invalid args %s", args)
	}
	*resp = s.Impl.RemoveManagedRoutes(&removeManagedRoutesArgs.Rollout)
	return nil
}

// RpcTrafficRouterPlugin This is the implementation of plugin.Plugin so we can serve/consume
//
// This has two methods: Server must return an RPC server for this plugin
// type. We construct a MetricsRPCServer for this.
//
// Client must return an implementation of our interface that communicates
// over an RPC client. We return MetricsPluginRPC for this.
//
// Ignore MuxBroker. That is used to create more multiplexed streams on our
// plugin connection and is a more advanced use case.
type RpcTrafficRouterPlugin struct {
	// Impl Injection
	Impl TrafficRouterPlugin
}

func (p *RpcTrafficRouterPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &TrafficRouterRPCServer{Impl: p.Impl}, nil
}

func (RpcTrafficRouterPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &TrafficRouterPluginRPC{client: c}, nil
}
