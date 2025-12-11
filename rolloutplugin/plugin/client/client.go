package client

import (
	"fmt"
	"os/exec"
	"sync"

	goPlugin "github.com/hashicorp/go-plugin"

	"github.com/argoproj/argo-rollouts/rolloutplugin/plugin/rpc"
	"github.com/argoproj/argo-rollouts/utils/plugin"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

type resourcePlugin struct {
	pluginClient map[string]*goPlugin.Client
	plugin       map[string]rpc.ResourcePlugin
}

var pluginClients *resourcePlugin
var once sync.Once
var mutex sync.Mutex

var handshakeConfig = goPlugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
	MagicCookieValue: "resourceplugin",
}

// pluginMap is the map of plugins we can dispense.
var pluginMap = map[string]goPlugin.Plugin{
	"RpcResourcePlugin": &rpc.RpcResourcePlugin{},
}

// GetResourcePlugin returns a singleton plugin client for the given resource plugin. Calling this multiple times
// returns the same plugin client instance for the plugin name defined in the rolloutplugin object.
func GetResourcePlugin(pluginName string) (rpc.ResourcePlugin, error) {
	once.Do(func() {
		pluginClients = &resourcePlugin{
			pluginClient: make(map[string]*goPlugin.Client),
			plugin:       make(map[string]rpc.ResourcePlugin),
		}
	})
	plugin, err := pluginClients.startPlugin(pluginName)
	if err != nil {
		return nil, fmt.Errorf("unable to start plugin system: %w", err)
	}
	return plugin, nil
}

func (r *resourcePlugin) startPlugin(pluginName string) (rpc.ResourcePlugin, error) {
	mutex.Lock()
	defer mutex.Unlock()

	if r.pluginClient[pluginName] == nil || r.pluginClient[pluginName].Exited() {

		// For now, we'll use a custom plugin type. This can be registered in types.go later
		pluginPath, args, err := plugin.GetPluginInfo(pluginName, types.PluginType("ResourcePlugin"))
		if err != nil {
			return nil, fmt.Errorf("unable to find plugin (%s): %w", pluginName, err)
		}

		r.pluginClient[pluginName] = goPlugin.NewClient(&goPlugin.ClientConfig{
			HandshakeConfig: handshakeConfig,
			Plugins:         pluginMap,
			Cmd:             exec.Command(pluginPath, args...),
			Managed:         true,
		})

		rpcClient, err := r.pluginClient[pluginName].Client()
		if err != nil {
			return nil, fmt.Errorf("unable to get plugin client (%s): %w", pluginName, err)
		}

		// Request the plugin
		pluginInstance, err := rpcClient.Dispense("RpcResourcePlugin")
		if err != nil {
			return nil, fmt.Errorf("unable to dispense plugin (%s): %w", pluginName, err)
		}

		pluginType, ok := pluginInstance.(rpc.ResourcePlugin)
		if !ok {
			return nil, fmt.Errorf("unexpected type from plugin")
		}
		r.plugin[pluginName] = pluginType

		resp := r.plugin[pluginName].InitPlugin()
		if resp.HasError() {
			return nil, fmt.Errorf("unable to initialize plugin via rpc (%s): %s", pluginName, resp.ErrorString)
		}
	}

	return r.plugin[pluginName], nil
}
