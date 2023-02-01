package client

import (
	"fmt"
	"os/exec"

	"github.com/argoproj/argo-rollouts/metricproviders/plugin/rpc"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/plugin"
	goPlugin "github.com/hashicorp/go-plugin"
)

type singletonMetricPlugin struct {
	pluginClient map[string]*goPlugin.Client
	plugin       map[string]rpc.MetricsPlugin
}

var singletonPluginClient *singletonMetricPlugin

// GetMetricPlugin returns a singleton plugin client for the given metric plugin. Calling this multi
// returns the same plugin client for the same plugin name.
func GetMetricPlugin(metric v1alpha1.Metric) (rpc.MetricsPlugin, error) {
	if singletonPluginClient == nil {
		singletonPluginClient = &singletonMetricPlugin{
			pluginClient: make(map[string]*goPlugin.Client),
			plugin:       make(map[string]rpc.MetricsPlugin),
		}
	}
	plugin, err := singletonPluginClient.startPluginSystem(metric)
	if err != nil {
		return nil, fmt.Errorf("unable to start plugin system: %w", err)
	}
	return plugin, nil
}

func (m *singletonMetricPlugin) startPluginSystem(metric v1alpha1.Metric) (rpc.MetricsPlugin, error) {
	var handshakeConfig = goPlugin.HandshakeConfig{
		ProtocolVersion:  1,
		MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
		MagicCookieValue: "metrics",
	}

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]goPlugin.Plugin{
		"RpcMetricsPlugin": &rpc.RpcMetricsPlugin{},
	}

	//There should only ever be one plugin defined in metric.Provider.Plugin
	for pluginName := range metric.Provider.Plugin {
		pluginPath, err := plugin.GetPluginLocation(pluginName)
		if err != nil {
			return nil, fmt.Errorf("unable to find plugin %s: %w", pluginName, err)
		}

		if m.pluginClient[pluginName] == nil || m.pluginClient[pluginName].Exited() {
			m.pluginClient[pluginName] = goPlugin.NewClient(&goPlugin.ClientConfig{
				HandshakeConfig: handshakeConfig,
				Plugins:         pluginMap,
				Cmd:             exec.Command(pluginPath),
				Managed:         true,
			})

			rpcClient, err := m.pluginClient[pluginName].Client()
			if err != nil {
				return nil, fmt.Errorf("unable to start plugin %s: %w", pluginName, err)
			}

			// Request the plugin
			raw, err := rpcClient.Dispense("RpcMetricsPlugin")
			if err != nil {
				return nil, fmt.Errorf("unable to dispense plugin %s: %w", pluginName, err)
			}

			pluginType, ok := raw.(rpc.MetricsPlugin)
			if !ok {
				return nil, fmt.Errorf("unexpected type from plugin")
			}
			m.plugin[pluginName] = pluginType

			err = m.plugin[pluginName].NewMetricsPlugin(metric)
			if err.Error() != "" {
				return nil, fmt.Errorf("unable to initialize plugin via rpc %s: %w", pluginName, err)
			}
		}

		return m.plugin[pluginName], nil
	}
	return nil, fmt.Errorf("no plugin found")
}
