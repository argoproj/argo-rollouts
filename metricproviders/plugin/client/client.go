package client

import (
	"fmt"
	"os/exec"
	"sync"

	"github.com/argoproj/argo-rollouts/metricproviders/plugin/rpc"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/plugin"
	goPlugin "github.com/hashicorp/go-plugin"
)

type metricPlugin struct {
	pluginClient map[string]*goPlugin.Client
	plugin       map[string]rpc.MetricsPlugin
}

var pluginClients *metricPlugin
var once sync.Once

// GetMetricPlugin returns a singleton plugin client for the given metric plugin. Calling this multiple times
// returns the same plugin client instance for the plugin name defined in the metric.
func GetMetricPlugin(metric v1alpha1.Metric) (rpc.MetricsPlugin, error) {
	once.Do(func() {
		pluginClients = &metricPlugin{
			pluginClient: make(map[string]*goPlugin.Client),
			plugin:       make(map[string]rpc.MetricsPlugin),
		}
	})
	plugin, err := pluginClients.startPluginSystem(metric)
	if err != nil {
		return nil, fmt.Errorf("unable to start plugin system: %w", err)
	}
	return plugin, nil
}

func (m *metricPlugin) startPluginSystem(metric v1alpha1.Metric) (rpc.MetricsPlugin, error) {
	var handshakeConfig = goPlugin.HandshakeConfig{
		ProtocolVersion:  1,
		MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
		MagicCookieValue: "metrics",
	}

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]goPlugin.Plugin{
		"RpcMetricsPlugin": &rpc.RpcMetricsPlugin{},
	}

	// There should only ever be one plugin defined in metric.Provider.Plugin
	for pluginName := range metric.Provider.Plugin {
		pluginPath, err := plugin.GetPluginLocation(pluginName)
		if err != nil {
			return nil, fmt.Errorf("unable to find plugin (%s): %w", pluginName, err)
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
				return nil, fmt.Errorf("unable to start plugin (%s): %w", pluginName, err)
			}

			// Request the plugin
			plugin, err := rpcClient.Dispense("RpcMetricsPlugin")
			if err != nil {
				return nil, fmt.Errorf("unable to dispense plugin (%s): %w", pluginName, err)
			}

			pluginType, ok := plugin.(rpc.MetricsPlugin)
			if !ok {
				return nil, fmt.Errorf("unexpected type from plugin")
			}
			m.plugin[pluginName] = pluginType

			err = m.plugin[pluginName].NewMetricsPlugin(metric)
			if err.Error() != "" {
				return nil, fmt.Errorf("unable to initialize plugin via rpc (%s): %w", pluginName, err)
			}
		}

		return m.plugin[pluginName], nil
	}
	return nil, fmt.Errorf("no plugin found")
}
