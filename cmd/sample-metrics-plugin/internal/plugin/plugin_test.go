package plugin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	rolloutsPlugin "github.com/argoproj/argo-rollouts/metricproviders/plugin"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	log "github.com/sirupsen/logrus"

	goPlugin "github.com/hashicorp/go-plugin"
)

var testHandshake = goPlugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
	MagicCookieValue: "metrics",
}

// This is just an example of how to test a plugin.
func TestRunSuccessfully(t *testing.T) {
	//Skip test because this is just an example of how to test a plugin.
	t.Skip("Skipping test because it requires a running prometheus server")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logCtx := *log.WithFields(log.Fields{"plugin-test": "prometheus"})

	rpcPluginImp := &RpcPlugin{
		LogCtx: logCtx,
	}

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]goPlugin.Plugin{
		"RpcMetricsPlugin": &rolloutsPlugin.RpcMetricsPlugin{Impl: rpcPluginImp},
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

	// Pinging should work
	if err := client.Ping(); err != nil {
		t.Fatalf("should not err: %s", err)
	}

	// Kill which should do nothing
	c.Kill()
	if err := client.Ping(); err != nil {
		t.Fatalf("should not err: %s", err)
	}

	// Request the plugin
	raw, err := client.Dispense("RpcMetricsPlugin")
	if err != nil {
		t.Fail()
	}

	plugin := raw.(rolloutsPlugin.MetricsPlugin)

	err = plugin.NewMetricsPlugin(v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			Plugin: &v1alpha1.PluginMetric{Config: json.RawMessage(`{"address":"http://prometheus.local", "query":"machine_cpu_cores"}`)},
		},
	})
	if err != nil {
		t.Fail()
	}

	// Canceling should cause an exit
	cancel()
	<-closeCh
}
