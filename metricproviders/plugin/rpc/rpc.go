package rpc

import (
	"encoding/gob"
	"fmt"
	"net/rpc"

	"github.com/argoproj/argo-rollouts/utils/plugin/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	"github.com/hashicorp/go-plugin"
)

type RunArgs struct {
	AnalysisRun *v1alpha1.AnalysisRun
	Metric      v1alpha1.Metric
}

type TerminateAndResumeArgs struct {
	AnalysisRun *v1alpha1.AnalysisRun
	Metric      v1alpha1.Metric
	Measurement v1alpha1.Measurement
}

type GarbageCollectArgs struct {
	AnalysisRun *v1alpha1.AnalysisRun
	Metric      v1alpha1.Metric
	Limit       int
}

type GetMetadataArgs struct {
	Metric v1alpha1.Metric
}

func init() {
	gob.RegisterName("RunArgs", new(RunArgs))
	gob.RegisterName("TerminateAndResumeArgs", new(TerminateAndResumeArgs))
	gob.RegisterName("GarbageCollectArgs", new(GarbageCollectArgs))
	gob.RegisterName("GetMetadataArgs", new(GetMetadataArgs))
}

var _ types.RpcMetricProvider = &MetricsPluginRPC{}

// MetricProviderPlugin is the interface that we're exposing as a plugin. It needs to match metricproviders.Providers but we can
// not import that package because it would create a circular dependency.
type MetricProviderPlugin interface {
	InitPlugin() types.RpcError
	types.RpcMetricProvider
}

// MetricsPluginRPC Here is an implementation that talks over RPC
type MetricsPluginRPC struct{ client *rpc.Client }

// InitPlugin is the client side function that is wrapped by a local provider this makes a rpc call to the
// server side function.
func (g *MetricsPluginRPC) InitPlugin() types.RpcError {
	var resp types.RpcError
	err := g.client.Call("Plugin.InitPlugin", new(interface{}), &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("InitPlugin rpc call error: %s", err)}
	}
	return resp
}

// Run is the client side function that is wrapped by a local provider this makes an rpc call to the server side function.
func (g *MetricsPluginRPC) Run(analysisRun *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	var resp v1alpha1.Measurement
	var args interface{} = RunArgs{
		AnalysisRun: analysisRun,
		Metric:      metric,
	}
	err := g.client.Call("Plugin.Run", &args, &resp)
	if err != nil {
		return metricutil.MarkMeasurementError(resp, fmt.Errorf("Run rpc call error: %s", err))
	}
	if resp.Phase == v1alpha1.AnalysisPhaseError {
		resp.Message = fmt.Sprintf("failed to run via plugin: %s", resp.Message)
	}
	return resp
}

// Resume is the client side function that is wrapped by a local provider this makes an rpc call to the server side function.
func (g *MetricsPluginRPC) Resume(analysisRun *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	var resp v1alpha1.Measurement
	var args interface{} = TerminateAndResumeArgs{
		AnalysisRun: analysisRun,
		Metric:      metric,
		Measurement: measurement,
	}
	err := g.client.Call("Plugin.Resume", &args, &resp)
	if err != nil {
		return metricutil.MarkMeasurementError(resp, fmt.Errorf("Resume rpc call error: %s", err))
	}
	if resp.Phase == v1alpha1.AnalysisPhaseError {
		resp.Message = fmt.Sprintf("failed to resume via plugin: %s", resp.Message)
	}
	return resp
}

// Terminate is the client side function that is wrapped by a local provider this makes an rpc call to the server side function.
func (g *MetricsPluginRPC) Terminate(analysisRun *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	var resp v1alpha1.Measurement
	var args interface{} = TerminateAndResumeArgs{
		AnalysisRun: analysisRun,
		Metric:      metric,
		Measurement: measurement,
	}
	err := g.client.Call("Plugin.Terminate", &args, &resp)
	if err != nil {
		return metricutil.MarkMeasurementError(resp, fmt.Errorf("Terminate rpc call error: %s", err))
	}
	if resp.Phase == v1alpha1.AnalysisPhaseError {
		resp.Message = fmt.Sprintf("failed to terminate via plugin: %s", resp.Message)
	}
	return resp
}

// GarbageCollect is the client side function that is wrapped by a local provider this makes an rpc call to the server side function.
func (g *MetricsPluginRPC) GarbageCollect(analysisRun *v1alpha1.AnalysisRun, metric v1alpha1.Metric, limit int) types.RpcError {
	var resp types.RpcError
	var args interface{} = GarbageCollectArgs{
		AnalysisRun: analysisRun,
		Metric:      metric,
		Limit:       limit,
	}
	err := g.client.Call("Plugin.GarbageCollect", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("GarbageCollect rpc call error: %s", err)}
	}
	return resp
}

// Type is the client side function that is wrapped by a local provider this makes an rpc call to the server side function.
func (g *MetricsPluginRPC) Type() string {
	var resp string
	err := g.client.Call("Plugin.Type", new(interface{}), &resp)
	if err != nil {
		return fmt.Sprintf("Type rpc call error: %s", err)
	}
	return resp
}

// GetMetadata is the client side function that is wrapped by a local provider this makes an rpc call to the server side function.
func (g *MetricsPluginRPC) GetMetadata(metric v1alpha1.Metric) map[string]string {
	var resp map[string]string
	var args interface{} = GetMetadataArgs{
		Metric: metric,
	}
	err := g.client.Call("Plugin.GetMetadata", &args, &resp)
	if err != nil {
		return map[string]string{"error": fmt.Sprintf("GetMetadata rpc call error: %s", err)}
	}
	if resp != nil && resp["error"] != "" {
		resp["error"] = fmt.Sprintf("failed to get metadata via plugin: %s", resp["error"])
	}
	return resp
}

// MetricsRPCServer Here is the RPC server that MetricsPluginRPC talks to, conforming to
// the requirements of net/rpc
type MetricsRPCServer struct {
	// This is the real implementation
	Impl MetricProviderPlugin
}

// InitPlugin is the receiving end of the RPC call running in the plugin executable process (the server), and it calls the
// implementation of the plugin.
func (s *MetricsRPCServer) InitPlugin(args interface{}, resp *types.RpcError) error {
	*resp = s.Impl.InitPlugin()
	return nil
}

// Run is the receiving end of the RPC call running in the plugin executable process (the server), and it calls the
// implementation of the plugin.
func (s *MetricsRPCServer) Run(args interface{}, resp *v1alpha1.Measurement) error {
	runArgs, ok := args.(*RunArgs)
	if !ok {
		return fmt.Errorf("invalid args %s", args)
	}
	*resp = s.Impl.Run(runArgs.AnalysisRun, runArgs.Metric)
	return nil
}

// Resume is the receiving end of the RPC call running in the plugin executable process (the server), and it calls the
// implementation of the plugin.
func (s *MetricsRPCServer) Resume(args interface{}, resp *v1alpha1.Measurement) error {
	resumeArgs, ok := args.(*TerminateAndResumeArgs)
	if !ok {
		return fmt.Errorf("invalid args %s", args)
	}
	*resp = s.Impl.Resume(resumeArgs.AnalysisRun, resumeArgs.Metric, resumeArgs.Measurement)
	return nil
}

// Terminate is the receiving end of the RPC call running in the plugin executable process (the server), and it calls the
// implementation of the plugin.
func (s *MetricsRPCServer) Terminate(args interface{}, resp *v1alpha1.Measurement) error {
	resumeArgs, ok := args.(*TerminateAndResumeArgs)
	if !ok {
		return fmt.Errorf("invalid args %s", args)
	}
	*resp = s.Impl.Terminate(resumeArgs.AnalysisRun, resumeArgs.Metric, resumeArgs.Measurement)
	return nil
}

// GarbageCollect is the receiving end of the RPC call running in the plugin executable process (the server), and it calls the
// implementation of the plugin.
func (s *MetricsRPCServer) GarbageCollect(args interface{}, resp *types.RpcError) error {
	gcArgs, ok := args.(*GarbageCollectArgs)
	if !ok {
		return fmt.Errorf("invalid args %s", args)
	}
	*resp = s.Impl.GarbageCollect(gcArgs.AnalysisRun, gcArgs.Metric, gcArgs.Limit)
	return nil
}

// Type is the receiving end of the RPC call running in the plugin executable process (the server), and it calls the
// implementation of the plugin.
func (s *MetricsRPCServer) Type(args interface{}, resp *string) error {
	*resp = s.Impl.Type()
	return nil
}

// GetMetadata is the receiving end of the RPC call running in the plugin executable process (the server), and it calls the
// implementation of the plugin.
func (s *MetricsRPCServer) GetMetadata(args interface{}, resp *map[string]string) error {
	getMetadataArgs, ok := args.(*GetMetadataArgs)
	if !ok {
		return fmt.Errorf("invalid args %s", args)
	}
	*resp = s.Impl.GetMetadata(getMetadataArgs.Metric)
	return nil
}

// RpcMetricProviderPlugin This is the implementation of plugin.Plugin so we can serve/consume
//
// This has two methods: Server must return an RPC server for this plugin
// type. We construct a MetricsRPCServer for this.
//
// Client must return an implementation of our interface that communicates
// over an RPC client. We return MetricsPluginRPC for this.
//
// Ignore MuxBroker. That is used to create more multiplexed streams on our
// plugin connection and is a more advanced use case.
type RpcMetricProviderPlugin struct {
	// Impl Injection
	Impl MetricProviderPlugin
}

func (p *RpcMetricProviderPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &MetricsRPCServer{Impl: p.Impl}, nil
}

func (RpcMetricProviderPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &MetricsPluginRPC{client: c}, nil
}
