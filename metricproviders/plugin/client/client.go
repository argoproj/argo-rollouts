package client

import (
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/hashicorp/go-hclog"
	goPlugin "github.com/hashicorp/go-plugin"
	log "github.com/sirupsen/logrus"

	"github.com/argoproj/argo-rollouts/metricproviders/plugin/rpc"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/plugin"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

type metricPlugin struct {
	pluginClient map[string]*goPlugin.Client
	plugin       map[string]rpc.MetricProviderPlugin
}

var pluginClients *metricPlugin
var once sync.Once
var mutex sync.Mutex

var handshakeConfig = goPlugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
	MagicCookieValue: "metricprovider",
}

// pluginMap is the map of plugins we can dispense.
var pluginMap = map[string]goPlugin.Plugin{
	"RpcMetricProviderPlugin": &rpc.RpcMetricProviderPlugin{},
}

// GetMetricPlugin returns a singleton plugin client for the given metric plugin. Calling this multiple times
// returns the same plugin client instance for the plugin name defined in the metric.
func GetMetricPlugin(metric v1alpha1.Metric) (rpc.MetricProviderPlugin, error) {
	once.Do(func() {
		pluginClients = &metricPlugin{
			pluginClient: make(map[string]*goPlugin.Client),
			plugin:       make(map[string]rpc.MetricProviderPlugin),
		}
	})
	plugin, err := pluginClients.startPluginSystem(metric)
	if err != nil {
		return nil, fmt.Errorf("unable to start plugin system: %w", err)
	}
	return plugin, nil
}

func (m *metricPlugin) startPluginSystem(metric v1alpha1.Metric) (rpc.MetricProviderPlugin, error) {
	mutex.Lock()
	defer mutex.Unlock()

	// There should only ever be one plugin defined in metric.Provider.Plugin per analysis template this gets checked
	// during validation
	for pluginName := range metric.Provider.Plugin {
		pluginPath, args, err := plugin.GetPluginInfo(pluginName, types.PluginTypeMetricProvider)
		if err != nil {
			return nil, fmt.Errorf("unable to find plugin (%s): %w", pluginName, err)
		}

		if m.pluginClient[pluginName] == nil || m.pluginClient[pluginName].Exited() {

			m.pluginClient[pluginName] = goPlugin.NewClient(&goPlugin.ClientConfig{
				HandshakeConfig: handshakeConfig,
				Plugins:         pluginMap,
				Cmd:             exec.Command(pluginPath, args...),
				Managed:         true,
				Logger:          newPluginLogger(),
			})

			rpcClient, err := m.pluginClient[pluginName].Client()
			if err != nil {
				return nil, fmt.Errorf("unable to get plugin client (%s): %w", pluginName, err)
			}

			// Request the plugin
			plugin, err := rpcClient.Dispense("RpcMetricProviderPlugin")
			if err != nil {
				return nil, fmt.Errorf("unable to dispense plugin (%s): %w", pluginName, err)
			}

			pluginType, ok := plugin.(rpc.MetricProviderPlugin)
			if !ok {
				return nil, fmt.Errorf("unexpected type from plugin")
			}
			m.plugin[pluginName] = pluginType

			resp := m.plugin[pluginName].InitPlugin()
			if resp.HasError() {
				return nil, fmt.Errorf("unable to initialize plugin via rpc (%s): %w", pluginName, resp)
			}
		}

		client, err := m.pluginClient[pluginName].Client()
		if err != nil {
			return nil, fmt.Errorf("unable to get plugin client (%s) for ping: %w", pluginName, err)
		}
		if err := client.Ping(); err != nil {
			m.pluginClient[pluginName].Kill()
			m.pluginClient[pluginName] = nil
			return nil, fmt.Errorf("could not ping plugin will cleanup process so we can restart it next reconcile (%w)", err)
		}

		return m.plugin[pluginName], nil
	}

	return nil, fmt.Errorf("no plugin found")
}

// newPluginLogger builds the hclog.Logger used for the go-plugin client's own log output
// (handshake/lifecycle logs, and the plugin process's stderr relay). By default go-plugin
// falls back to its own unstructured text logger whenever ClientConfig.Logger is nil, which
// is inconsistent with the controller's own logs when the controller is run with
// `--logformat json`: every other component's logs are JSON, but metric plugin logs remain
// plain text. When the controller's standard logger is configured for JSON output, mirror
// that here so the plugin logger's output is JSON too. Returns nil (go-plugin's own default
// logger) in every other case, preserving prior behavior exactly.
func newPluginLogger() hclog.Logger {
	return newPluginLoggerWithOutput(hclog.DefaultOutput)
}

func newPluginLoggerWithOutput(w io.Writer) hclog.Logger {
	if _, ok := log.StandardLogger().Formatter.(*log.JSONFormatter); !ok {
		return nil
	}
	return hclog.New(&hclog.LoggerOptions{
		Output:     w,
		Level:      hclog.Trace,
		Name:       "plugin",
		JSONFormat: true,
	})
}
