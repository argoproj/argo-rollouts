package client

import (
	"fmt"
	"os/exec"
	"sync"

	goPlugin "github.com/hashicorp/go-plugin"

	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/plugin/rpc"
	"github.com/argoproj/argo-rollouts/utils/plugin"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

type trafficPlugin struct {
	pluginClient map[string]*goPlugin.Client
	rpcClient    map[string]goPlugin.ClientProtocol
	plugin       map[string]rpc.TrafficRouterPlugin
}

var pluginClients *trafficPlugin
var once sync.Once
var mutex sync.Mutex

var handshakeConfig = goPlugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
	MagicCookieValue: "trafficrouter",
}

// pluginMap is the map of plugins we can dispense.
var pluginMap = map[string]goPlugin.Plugin{
	"RpcTrafficRouterPlugin": &rpc.RpcTrafficRouterPlugin{},
}

// getPluginInfo is a package-level function variable to allow test overrides.
var getPluginInfo = plugin.GetPluginInfo

// testBeforePing is a hook called right before Ping() in the if-branch of startPluginLocked.
// It is nil in production and only set during tests to simulate ping failures.
var testBeforePing func(pluginName string)

// GetTrafficPlugin returns a singleton plugin client for the given traffic router plugin. Calling this multiple times
// returns the same plugin client instance for the plugin name defined in the rollout object.
func GetTrafficPlugin(pluginName string) (rpc.TrafficRouterPlugin, error) {
	once.Do(func() {
		pluginClients = &trafficPlugin{
			pluginClient: make(map[string]*goPlugin.Client),
			rpcClient:    make(map[string]goPlugin.ClientProtocol),
			plugin:       make(map[string]rpc.TrafficRouterPlugin),
		}
	})
	plugin, err := pluginClients.startPlugin(pluginName)
	if err != nil {
		return nil, fmt.Errorf("unable to start plugin system: %w", err)
	}
	return plugin, nil
}

func (t *trafficPlugin) startPlugin(pluginName string) (rpc.TrafficRouterPlugin, error) {
	mutex.Lock()
	defer mutex.Unlock()
	return t.startPluginLocked(pluginName)
}

// startPluginLocked contains the core logic and must be called with mutex held.
func (t *trafficPlugin) startPluginLocked(pluginName string) (rpc.TrafficRouterPlugin, error) {
	if t.pluginClient[pluginName] == nil || t.pluginClient[pluginName].Exited() {

		pluginPath, args, err := getPluginInfo(pluginName, types.PluginTypeTrafficRouter)
		if err != nil {
			return nil, fmt.Errorf("unable to find plugin (%s): %w", pluginName, err)
		}

		t.pluginClient[pluginName] = goPlugin.NewClient(&goPlugin.ClientConfig{
			HandshakeConfig: handshakeConfig,
			Plugins:         pluginMap,
			Cmd:             exec.Command(pluginPath, args...),
			Managed:         true,
		})

		rpcClient, err := t.pluginClient[pluginName].Client()
		if err != nil {
			return nil, fmt.Errorf("unable to get plugin client (%s): %w", pluginName, err)
		}

		// Cache the RPC client to avoid calling Client() again
		t.rpcClient[pluginName] = rpcClient

		// Request the plugin
		plugin, err := rpcClient.Dispense("RpcTrafficRouterPlugin")
		if err != nil {
			return nil, fmt.Errorf("unable to dispense plugin (%s): %w", pluginName, err)
		}

		pluginType, ok := plugin.(rpc.TrafficRouterPlugin)
		if !ok {
			return nil, fmt.Errorf("unexpected type from plugin")
		}
		t.plugin[pluginName] = pluginType

		resp := t.plugin[pluginName].InitPlugin()
		if resp.HasError() {
			return nil, fmt.Errorf("unable to initialize plugin via rpc (%s): %w", pluginName, resp)
		}

		// Ping using the cached RPC client
		if testBeforePing != nil {
			testBeforePing(pluginName)
		}
		if err := rpcClient.Ping(); err != nil {
			t.pluginClient[pluginName].Kill()
			t.pluginClient[pluginName] = nil
			t.rpcClient[pluginName] = nil
			t.plugin[pluginName] = nil
			return nil, fmt.Errorf("could not ping plugin will cleanup process so we can restart it next reconcile (%w)", err)
		}
	} else {
		// Plugin already initialized, use cached RPC client for ping
		if t.rpcClient[pluginName] == nil || t.plugin[pluginName] == nil {
			// RPC client or plugin was cleaned up, need to reinitialize
			t.pluginClient[pluginName].Kill()
			t.pluginClient[pluginName] = nil
			// Recursively call to reinitialize (mutex already held)
			return t.startPluginLocked(pluginName)
		}

		if err := t.rpcClient[pluginName].Ping(); err != nil {
			t.pluginClient[pluginName].Kill()
			t.pluginClient[pluginName] = nil
			t.rpcClient[pluginName] = nil
			t.plugin[pluginName] = nil
			return nil, fmt.Errorf("could not ping plugin will cleanup process so we can restart it next reconcile (%w)", err)
		}
	}

	if t.plugin[pluginName] == nil {
		return nil, fmt.Errorf("plugin %s is not initialized", pluginName)
	}

	return t.plugin[pluginName], nil
}
