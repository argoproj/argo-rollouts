package rpc

import (
	"context"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/utils/plugin/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	goPlugin "github.com/hashicorp/go-plugin"
	"github.com/tj/assert"
)

var testHandshake = goPlugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
	MagicCookieValue: "metricprovider",
}

func pluginClient(t *testing.T) (MetricProviderPlugin, goPlugin.ClientProtocol, func(), chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())

	rpcPluginImp := &testRpcPlugin{}

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]goPlugin.Plugin{
		"RpcMetricProviderPlugin": &RpcMetricProviderPlugin{Impl: rpcPluginImp},
	}

	ch := make(chan *goPlugin.ReattachConfig, 1)
	closeCh := make(chan struct{})
	go goPlugin.Serve(&goPlugin.ServeConfig{
		HandshakeConfig: testHandshake,
		Plugins:         pluginMap,
		Test: &goPlugin.ServeTestConfig{
			Context:          ctx,
			ReattachConfigCh: ch,
			CloseCh:          closeCh,
		},
	})

	// We should get a config
	var config *goPlugin.ReattachConfig
	select {
	case config = <-ch:
	case <-time.After(2000 * time.Millisecond):
		t.Fatal("should've received reattach")
	}
	if config == nil {
		t.Fatal("config should not be nil")
	}

	// Connect!
	c := goPlugin.NewClient(&goPlugin.ClientConfig{
		Cmd:             nil,
		HandshakeConfig: testHandshake,
		Plugins:         pluginMap,
		Reattach:        config,
	})
	client, err := c.Client()
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	// Request the plugin
	raw, err := client.Dispense("RpcMetricProviderPlugin")
	if err != nil {
		t.Fail()
	}

	plugin, ok := raw.(MetricProviderPlugin)
	if !ok {
		t.Fail()
	}

	return plugin, client, cancel, closeCh
}

func TestPlugin(t *testing.T) {
	plugin, _, cancel, closeCh := pluginClient(t)
	defer cancel()

	err := plugin.InitPlugin()
	if err.Error() != "" {
		t.Fail()
	}

	runMeasurement := plugin.Run(&v1alpha1.AnalysisRun{}, v1alpha1.Metric{})
	assert.Equal(t, "TestCompleted", string(runMeasurement.Phase))

	runMeasurementErr := plugin.Run(nil, v1alpha1.Metric{})
	assert.Equal(t, "Error", string(runMeasurementErr.Phase))
	assert.Contains(t, runMeasurementErr.Message, "analysisRun is nil")

	resumeMeasurement := plugin.Resume(&v1alpha1.AnalysisRun{}, v1alpha1.Metric{}, v1alpha1.Measurement{
		Phase:   "TestCompletedResume",
		Message: "Check to see if we get same phase back",
	})
	assert.Equal(t, "TestCompletedResume", string(resumeMeasurement.Phase))

	terminateMeasurement := plugin.Terminate(&v1alpha1.AnalysisRun{}, v1alpha1.Metric{}, v1alpha1.Measurement{
		Phase:   "TestCompletedTerminate",
		Message: "Check to see if we get same phase back",
	})
	assert.Equal(t, "TestCompletedTerminate", string(terminateMeasurement.Phase))

	gcError := plugin.GarbageCollect(&v1alpha1.AnalysisRun{}, v1alpha1.Metric{}, 0)
	assert.Equal(t, "not-implemented", gcError.Error())

	typeTest := plugin.Type()
	assert.Equal(t, "TestRPCPlugin", typeTest)

	metadata := plugin.GetMetadata(v1alpha1.Metric{
		Name: "testMetric",
	})
	assert.Equal(t, "testMetric", metadata["metricName"])

	// Canceling should cause an exit
	cancel()
	<-closeCh
}

func TestPluginClosedConnection(t *testing.T) {
	plugin, client, cancel, closeCh := pluginClient(t)
	defer cancel()

	client.Close()
	time.Sleep(100 * time.Millisecond)

	const expectedError = "connection is shut down"

	newMetrics := plugin.InitPlugin()
	assert.Contains(t, newMetrics.Error(), expectedError)

	measurement := plugin.Terminate(&v1alpha1.AnalysisRun{}, v1alpha1.Metric{}, v1alpha1.Measurement{})
	assert.Contains(t, measurement.Message, expectedError)

	measurement = plugin.Run(&v1alpha1.AnalysisRun{}, v1alpha1.Metric{})
	assert.Contains(t, measurement.Message, expectedError)

	measurement = plugin.Resume(&v1alpha1.AnalysisRun{}, v1alpha1.Metric{}, v1alpha1.Measurement{})
	assert.Contains(t, measurement.Message, expectedError)

	measurement = plugin.Terminate(&v1alpha1.AnalysisRun{}, v1alpha1.Metric{}, v1alpha1.Measurement{})
	assert.Contains(t, measurement.Message, expectedError)

	typeStr := plugin.Type()
	assert.Contains(t, typeStr, expectedError)

	metadata := plugin.GetMetadata(v1alpha1.Metric{})
	assert.Contains(t, metadata["error"], expectedError)

	gcError := plugin.GarbageCollect(&v1alpha1.AnalysisRun{}, v1alpha1.Metric{}, 0)
	assert.Contains(t, gcError.Error(), expectedError)

	cancel()
	<-closeCh
}

func TestInvalidArgs(t *testing.T) {
	server := MetricsRPCServer{}
	badtype := struct {
		Args string
	}{}
	err := server.Run(badtype, &v1alpha1.Measurement{})
	assert.Error(t, err)

	err = server.Resume(badtype, &v1alpha1.Measurement{})
	assert.Error(t, err)

	err = server.Terminate(badtype, &v1alpha1.Measurement{})
	assert.Error(t, err)

	err = server.GarbageCollect(badtype, &types.RpcError{})
	assert.Error(t, err)

	resp := make(map[string]string)
	err = server.GetMetadata(badtype, &resp)
	assert.Error(t, err)
}
