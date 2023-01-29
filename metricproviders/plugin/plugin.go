package plugin

import (
	"fmt"
	"os/exec"

	"github.com/argoproj/argo-rollouts/metric"
	"github.com/argoproj/argo-rollouts/metricproviders/plugin/rpc"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	goPlugin "github.com/hashicorp/go-plugin"
)

const ProviderType = "RPCPlugin"

var pluginClient *goPlugin.Client
var plugin rpc.MetricsPlugin

type MetricPlugin struct {
	metric.Provider
}

func NewRpcPlugin(metric v1alpha1.Metric) (metric.Provider, error) {
	err := startPluginSystem(metric)
	if err != nil {
		return nil, err
	}

	return MetricPlugin{}, nil
}

func startPluginSystem(metric v1alpha1.Metric) error {
	if defaults.GetMetricPluginLocation() == "" {
		return fmt.Errorf("no plugin location specified")
	}

	var handshakeConfig = goPlugin.HandshakeConfig{
		ProtocolVersion:  1,
		MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
		MagicCookieValue: "metrics",
	}

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]goPlugin.Plugin{
		"RpcMetricsPlugin": &rpc.RpcMetricsPlugin{},
	}

	if pluginClient == nil || pluginClient.Exited() {
		pluginClient = goPlugin.NewClient(&goPlugin.ClientConfig{
			HandshakeConfig:  handshakeConfig,
			Plugins:          pluginMap,
			VersionedPlugins: nil,
			Cmd:              exec.Command(defaults.GetMetricPluginLocation()),
			Managed:          true,
		})

		rpcClient, err := pluginClient.Client()
		if err != nil {
			return err
		}

		// Request the plugin
		raw, err := rpcClient.Dispense("RpcMetricsPlugin")
		if err != nil {
			return err
		}

		plugin = raw.(rpc.MetricsPlugin)

		err = plugin.NewMetricsPlugin(metric)
		if err.Error() != "" {
			return err
		}
	}

	return nil
}

func (m MetricPlugin) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	return plugin.Run(run, metric)
}

func (m MetricPlugin) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	return plugin.Resume(run, metric, measurement)
}

func (m MetricPlugin) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	return plugin.Terminate(run, metric, measurement)
}

func (m MetricPlugin) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, limit int) error {
	err := plugin.GarbageCollect(run, metric, limit)
	if err.Error() != "" {
		return err
	}
	return nil
}

func (m MetricPlugin) Type() string {
	return ProviderType
}

func (m MetricPlugin) GetMetadata(metric v1alpha1.Metric) map[string]string {
	return plugin.GetMetadata(metric)
}
