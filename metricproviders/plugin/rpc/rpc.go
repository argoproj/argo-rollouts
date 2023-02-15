package rpc

import (
	"encoding/gob"
	"fmt"
	"net/rpc"

	"github.com/argoproj/argo-rollouts/utils/plugin/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	"github.com/hashicorp/go-plugin"
	log "github.com/sirupsen/logrus"
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

type InitMetricsPluginAndGetMetadataArgs struct {
	Metric v1alpha1.Metric
}

func init() {
	gob.RegisterName("RunArgs", new(RunArgs))
	gob.RegisterName("TerminateAndResumeArgs", new(TerminateAndResumeArgs))
	gob.RegisterName("GarbageCollectArgs", new(GarbageCollectArgs))
	gob.RegisterName("InitMetricsPluginAndGetMetadataArgs", new(InitMetricsPluginAndGetMetadataArgs))
}

// MetricsPlugin is the interface that we're exposing as a plugin. It needs to match metricproviders.Providers but we can
// not import that package because it would create a circular dependency.
type MetricsPlugin interface {
	InitPlugin(metric v1alpha1.Metric) types.RpcError
	types.RpcMetricProvider
}

// MetricsPluginRPC Here is an implementation that talks over RPC
type MetricsPluginRPC struct{ client *rpc.Client }

// InitPlugin is the client side function that is wrapped by a local provider this makes a rpc call to the
// server side function.
func (g *MetricsPluginRPC) InitPlugin(metric v1alpha1.Metric) types.RpcError {
	var resp types.RpcError
	var args interface{} = InitMetricsPluginAndGetMetadataArgs{
		Metric: metric,
	}
	err := g.client.Call("Plugin.InitPlugin", &args, &resp)
	if err != nil {
		return types.RpcError{ErrorString: err.Error()}
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
		return metricutil.MarkMeasurementError(resp, err)
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
		return metricutil.MarkMeasurementError(resp, err)
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
		return metricutil.MarkMeasurementError(resp, err)
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
		return types.RpcError{ErrorString: err.Error()}
	}
	return resp
}

// Type is the client side function that is wrapped by a local provider this makes an rpc call to the server side function.
func (g *MetricsPluginRPC) Type() string {
	var resp string
	err := g.client.Call("Plugin.Type", new(interface{}), &resp)
	if err != nil {
		return err.Error()
	}

	return resp
}

// GetMetadata is the client side function that is wrapped by a local provider this makes an rpc call to the server side function.
func (g *MetricsPluginRPC) GetMetadata(metric v1alpha1.Metric) map[string]string {
	var resp map[string]string
	var args interface{} = InitMetricsPluginAndGetMetadataArgs{
		Metric: metric,
	}
	err := g.client.Call("Plugin.GetMetadata", &args, &resp)
	if err != nil {
		log.Errorf("Error calling GetMetadata: %v", err)
		return map[string]string{"error": err.Error()}
	}
	return resp
}

// MetricsRPCServer Here is the RPC server that MetricsPluginRPC talks to, conforming to
// the requirements of net/rpc
type MetricsRPCServer struct {
	// This is the real implementation
	Impl MetricsPlugin
}

// InitPlugin is the receiving end of the RPC call running in the plugin executable process (the server), and it calls the
// implementation of the plugin.
func (s *MetricsRPCServer) InitPlugin(args interface{}, resp *types.RpcError) error {
	initArgs, ok := args.(*InitMetricsPluginAndGetMetadataArgs)
	if !ok {
		return fmt.Errorf("invalid args %s", args)
	}
	*resp = s.Impl.InitPlugin(initArgs.Metric)
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
	getMetadataArgs, ok := args.(*InitMetricsPluginAndGetMetadataArgs)
	if !ok {
		return fmt.Errorf("invalid args %s", args)
	}
	*resp = s.Impl.GetMetadata(getMetadataArgs.Metric)
	return nil
}

// RpcMetricsPlugin This is the implementation of plugin.Plugin so we can serve/consume
//
// This has two methods: Server must return an RPC server for this plugin
// type. We construct a MetricsRPCServer for this.
//
// Client must return an implementation of our interface that communicates
// over an RPC client. We return MetricsPluginRPC for this.
//
// Ignore MuxBroker. That is used to create more multiplexed streams on our
// plugin connection and is a more advanced use case.
type RpcMetricsPlugin struct {
	// Impl Injection
	Impl MetricsPlugin
}

func (p *RpcMetricsPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &MetricsRPCServer{Impl: p.Impl}, nil
}

func (RpcMetricsPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &MetricsPluginRPC{client: c}, nil
}
