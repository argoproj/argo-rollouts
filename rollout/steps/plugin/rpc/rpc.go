package rpc

import (
	"encoding/gob"
	"fmt"
	"net/rpc"

	"github.com/argoproj/argo-rollouts/utils/plugin/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/hashicorp/go-plugin"
)

type RunArgs struct {
	Rollout *v1alpha1.Rollout
	Context *types.RpcStepContext
}

type TerminateArgs struct {
	Rollout *v1alpha1.Rollout
	Context *types.RpcStepContext
}

type AbortArgs struct {
	Rollout *v1alpha1.Rollout
	Context *types.RpcStepContext
}

type Response struct {
	Result types.RpcStepResult
	Error  types.RpcError
}

func init() {
	gob.RegisterName("step.RunArgs", new(RunArgs))
	gob.RegisterName("step.TerminateArgs", new(TerminateArgs))
	gob.RegisterName("step.AbortArgs", new(AbortArgs))
}

// StepPlugin is the interface that we're exposing as a plugin. It needs to match metricproviders.Providers but we can
// not import that package because it would create a circular dependency.
type StepPlugin interface {
	InitPlugin() types.RpcError
	types.RpcStep
}

// StepPluginRPC Here is an implementation that talks over RPC
type StepPluginRPC struct{ client *rpc.Client }

// InitPlugin is the client aka the controller side function that calls the server side rpc (plugin)
// this gets called once during startup of the plugin and can be used to set up informers, k8s clients, etc.
func (g *StepPluginRPC) InitPlugin() types.RpcError {
	var resp types.RpcError
	err := g.client.Call("Plugin.InitPlugin", new(any), &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("InitPlugin rpc call error: %s", err)}
	}
	return resp
}

// Run executes the step
func (g *StepPluginRPC) Run(rollout *v1alpha1.Rollout, context *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	var resp Response
	var args any = RunArgs{
		Rollout: rollout,
		Context: context,
	}
	err := g.client.Call("Plugin.Run", &args, &resp)
	if err != nil {
		return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Sprintf("Run rpc call error: %s", err)}
	}
	return resp.Result, resp.Error
}

// Terminate stops the execution of a running step and exits early
func (g *StepPluginRPC) Terminate(rollout *v1alpha1.Rollout, context *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	var resp Response
	var args any = TerminateArgs{
		Rollout: rollout,
		Context: context,
	}
	err := g.client.Call("Plugin.Terminate", &args, &resp)
	if err != nil {
		return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Sprintf("Terminate rpc call error: %s", err)}
	}
	return resp.Result, resp.Error
}

// Abort reverts previous operation executed by the step if necessary
func (g *StepPluginRPC) Abort(rollout *v1alpha1.Rollout, context *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	var resp Response
	var args any = AbortArgs{
		Rollout: rollout,
		Context: context,
	}
	err := g.client.Call("Plugin.Abort", &args, &resp)
	if err != nil {
		return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Sprintf("Abort rpc call error: %s", err)}
	}
	return resp.Result, resp.Error
}

// Type returns the type of the traffic routing reconciler
func (g *StepPluginRPC) Type() string {
	var resp string
	err := g.client.Call("Plugin.Type", new(any), &resp)
	if err != nil {
		return fmt.Sprintf("Type rpc call error: %s", err)
	}

	return resp
}

// StepRPCServer Here is the RPC server that MetricsPluginRPC talks to, conforming to
// the requirements of net/rpc
type StepRPCServer struct {
	// This is the real implementation
	Impl StepPlugin
}

// InitPlugin this is the server aka the controller side function that receives calls from the client side rpc (controller)
// this gets called once during startup of the plugin and can be used to set up informers or k8s clients etc.
func (s *StepRPCServer) InitPlugin(args any, resp *types.RpcError) error {
	*resp = s.Impl.InitPlugin()
	return nil
}

// Run executes the step
func (s *StepRPCServer) Run(args any, resp *Response) error {
	runArgs, ok := args.(*RunArgs)
	if !ok {
		return fmt.Errorf("invalid args %s", args)
	}
	result, err := s.Impl.Run(runArgs.Rollout, runArgs.Context)
	*resp = Response{
		Result: result,
		Error:  err,
	}
	return nil
}

// Terminate stops the execution of a running step and exits early
func (s *StepRPCServer) Terminate(args any, resp *Response) error {
	runArgs, ok := args.(*TerminateArgs)
	if !ok {
		return fmt.Errorf("invalid args %s", args)
	}
	result, err := s.Impl.Terminate(runArgs.Rollout, runArgs.Context)
	*resp = Response{
		Result: result,
		Error:  err,
	}
	return nil
}

// Abort reverts previous operation executed by the step if necessary
func (s *StepRPCServer) Abort(args any, resp *Response) error {
	runArgs, ok := args.(*AbortArgs)
	if !ok {
		return fmt.Errorf("invalid args %s", args)
	}
	result, err := s.Impl.Abort(runArgs.Rollout, runArgs.Context)
	*resp = Response{
		Result: result,
		Error:  err,
	}
	return nil
}

// Type returns the type of the traffic routing reconciler
func (s *StepRPCServer) Type(args any, resp *string) error {
	*resp = s.Impl.Type()
	return nil
}

// RpcStepPlugin This is the implementation of plugin.Plugin so we can serve/consume
//
// This has two methods: Server must return an RPC server for this plugin
// type. We construct a StepRPCServer for this.
//
// Client must return an implementation of our interface that communicates
// over an RPC client. We return StepPluginRPC for this.
//
// Ignore MuxBroker. That is used to create more multiplexed streams on our
// plugin connection and is a more advanced use case.
type RpcStepPlugin struct {
	// Impl Injection
	Impl StepPlugin
}

func (p *RpcStepPlugin) Server(*plugin.MuxBroker) (any, error) {
	return &StepRPCServer{Impl: p.Impl}, nil
}

func (RpcStepPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (any, error) {
	return &StepPluginRPC{client: c}, nil
}
