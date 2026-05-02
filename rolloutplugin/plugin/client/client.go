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

type pluginRegistry struct {
	// processClient manages the plugin subprocess lifecycle (start, kill, exit detection).
	processClient map[string]*goPlugin.Client
	// rpcConnClient is the RPC transport obtained from processClient; used for Dispense and Ping.
	rpcConnClient map[string]goPlugin.ClientProtocol
	// instances holds the dispensed, typed plugin interface ready for method calls.
	instances map[string]types.RpcResourcePlugin
}

var registry *pluginRegistry
var once sync.Once
var mutex sync.Mutex

var handshakeConfig = goPlugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
	MagicCookieValue: "resourceplugin",
}

// pluginMap is the map of plugins we can dispense.
var pluginMap = map[string]goPlugin.Plugin{
	"RpcResourcePlugin": &rpc.ResourcePluginImpl{},
}

// getPluginInfo is a package-level function variable to allow test overrides.
var getPluginInfo = plugin.GetPluginInfo

// testBeforePing is a hook called right before Ping().
// It is nil in production and only set during tests to simulate ping failures.
var testBeforePing func(pluginName string)

// GetResourcePlugin returns a singleton plugin client for the given resource plugin. Calling this multiple times
// returns the same plugin client instance for the plugin name defined in the rolloutplugin object.
func GetResourcePlugin(pluginName, namespace string) (types.RpcResourcePlugin, error) {
	once.Do(func() {
		registry = &pluginRegistry{
			processClient: make(map[string]*goPlugin.Client),
			rpcConnClient: make(map[string]goPlugin.ClientProtocol),
			instances:     make(map[string]types.RpcResourcePlugin),
		}
	})
	p, err := registry.startPlugin(pluginName, namespace)
	if err != nil {
		return nil, fmt.Errorf("unable to start plugin system: %w", err)
	}
	return p, nil
}

func (r *pluginRegistry) startPlugin(pluginName, namespace string) (types.RpcResourcePlugin, error) {
	mutex.Lock()
	defer mutex.Unlock()
	return r.startPluginLocked(pluginName, namespace)
}

// startPluginLocked contains the core logic and must be called with mutex held.
func (r *pluginRegistry) startPluginLocked(pluginName, namespace string) (types.RpcResourcePlugin, error) {
	if r.processClient[pluginName] == nil || r.processClient[pluginName].Exited() {

		pluginPath, args, err := getPluginInfo(pluginName, types.PluginTypeResourcePlugin)
		if err != nil {
			return nil, fmt.Errorf("unable to find plugin (%s): %w", pluginName, err)
		}

		r.processClient[pluginName] = goPlugin.NewClient(&goPlugin.ClientConfig{
			HandshakeConfig: handshakeConfig,
			Plugins:         pluginMap,
			Cmd:             exec.Command(pluginPath, args...),
			Managed:         true,
		})

		rpcClient, err := r.processClient[pluginName].Client()
		if err != nil {
			return nil, fmt.Errorf("unable to get plugin client (%s): %w", pluginName, err)
		}

		// Cache the RPC client to avoid calling Client() again
		r.rpcConnClient[pluginName] = rpcClient

		// Request the plugin
		pluginInstance, err := rpcClient.Dispense("RpcResourcePlugin")
		if err != nil {
			return nil, fmt.Errorf("unable to dispense plugin (%s): %w", pluginName, err)
		}

		typedPlugin, ok := pluginInstance.(types.RpcResourcePlugin)
		if !ok {
			return nil, fmt.Errorf("unexpected type from plugin")
		}
		r.instances[pluginName] = typedPlugin

		resp := r.instances[pluginName].InitPlugin(namespace)
		if resp.HasError() {
			return nil, fmt.Errorf("unable to initialize plugin via rpc (%s): %w", pluginName, resp)
		}

		// Ping to verify liveness after init
		if testBeforePing != nil {
			testBeforePing(pluginName)
		}
		if err := rpcClient.Ping(); err != nil {
			r.cleanupPlugin(pluginName)
			return nil, fmt.Errorf("could not ping plugin, will cleanup process so we can restart it next reconcile (%w)", err)
		}
	} else {
		// Plugin already initialized, verify it's still alive
		if r.rpcConnClient[pluginName] == nil || r.instances[pluginName] == nil {
			// RPC client or plugin was cleaned up, need to reinitialize
			r.processClient[pluginName].Kill()
			r.processClient[pluginName] = nil
			// Recursively call to reinitialize (mutex already held)
			return r.startPluginLocked(pluginName, namespace)
		}

		if err := r.rpcConnClient[pluginName].Ping(); err != nil {
			r.cleanupPlugin(pluginName)
			return nil, fmt.Errorf("could not ping plugin, will cleanup process so we can restart it next reconcile (%w)", err)
		}
	}

	if r.instances[pluginName] == nil {
		return nil, fmt.Errorf("plugin %s is not initialized", pluginName)
	}

	return r.instances[pluginName], nil
}

// cleanupPlugin kills and removes all references for a plugin so it can be restarted.
func (r *pluginRegistry) cleanupPlugin(pluginName string) {
	if r.processClient[pluginName] != nil {
		r.processClient[pluginName].Kill()
	}
	r.processClient[pluginName] = nil
	r.rpcConnClient[pluginName] = nil
	r.instances[pluginName] = nil
}
