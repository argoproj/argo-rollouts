package rolloutplugin

import (
	"fmt"
	"net/url"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

// BuiltinPluginScheme is the location scheme used in the argo-rollouts-config ConfigMap
// (under rolloutPlugins) to mark a plugin as a built-in.
const BuiltinPluginScheme = "builtin"

// BuiltinPluginFactory constructs an in-process ResourcePlugin. Factories are defined
// where the concrete built-ins can be imported (cmd/rollouts-controller), avoiding the
// statefulset -> rolloutplugin import cycle that a direct reference here would create.
type BuiltinPluginFactory func(logCtx *log.Entry) ResourcePlugin

var (
	// globalPluginManager is the singleton instance of the plugin manager
	globalPluginManager *DefaultPluginManager
	// once ensures the plugin manager is initialized only once
	once sync.Once
)

// newRpcPlugin constructs an external RPC-backed ResourcePlugin. It is a package-level
// variable so tests can substitute a fake without spawning a real plugin subprocess.
var newRpcPlugin = NewRpcPlugin

// DefaultPluginManager implements PluginManager
type DefaultPluginManager struct {
	// plugins is a map of plugin name to plugin instance
	plugins map[string]ResourcePlugin

	// builtinEnabled records which built-in plugins (keyed by builtin://<id> host,
	// e.g. "statefulset") were enabled via the rolloutPlugins ConfigMap and registered
	// at startup.
	builtinEnabled map[string]bool

	// namespace is the controller's watch namespace
	namespace string

	// mu protects plugins and builtinEnabled maps
	mu sync.RWMutex
}

// GetGlobalPluginManager returns the singleton plugin manager instance, initializing it
// with the controller's watch namespace on first call. The namespace is stored
// unconditionally here (not as a side effect of registering a built-in) so that lazily
// loaded external RPC plugins are always started with the correct namespace, even when the
// ConfigMap contains only external entries and no built-in is ever registered.
func GetGlobalPluginManager(namespace string) *DefaultPluginManager {
	once.Do(func() {
		log.Info("Initializing global plugin manager singleton")
		globalPluginManager = &DefaultPluginManager{
			plugins:        make(map[string]ResourcePlugin),
			builtinEnabled: make(map[string]bool),
			namespace:      namespace,
		}
	})
	return globalPluginManager
}

// GetPlugin returns a plugin by name.
//
// Built-in plugins are registered eagerly at startup and cached in pm.plugins, so they are
// returned directly. Anything not in that map is treated as an external RPC plugin and is
// (re)derived on every call rather than cached: the underlying process is de-duplicated and
// liveness-checked by the client registry (client.GetResourcePlugin only re-execs when the
// process has Exited() and Pings the live connection each call). Re-deriving the wrapper
// each reconcile is therefore cheap and, crucially, re-arms the client's crash-detect /
// restart path — a permanently cached wrapper would keep pointing at a dead process forever.
// TODO: revisit this approach
func (pm *DefaultPluginManager) GetPlugin(name string) (ResourcePlugin, error) {
	pm.mu.RLock()
	plugin, exists := pm.plugins[name]
	pm.mu.RUnlock()
	if exists {
		return plugin, nil
	}

	rpcPlugin, err := newRpcPlugin(name, pm.namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to load external plugin '%s': %w", name, err)
	}
	return rpcPlugin, nil
}

// RegisterPlugin registers a plugin with a specific name.
func (pm *DefaultPluginManager) RegisterPlugin(name string, plugin ResourcePlugin, namespace string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.plugins[name]; exists {
		return fmt.Errorf("plugin '%s' already registered", name)
	}

	// Initialize the plugin once during registration
	if err := plugin.Init(namespace); err != nil {
		return fmt.Errorf("failed to initialize plugin '%s': %w", name, err)
	}

	pm.plugins[name] = plugin
	log.WithField("plugin", name).Info("Plugin registered and initialized successfully")

	return nil
}

// RegisterBuiltinPlugins registers every rolloutPlugins ConfigMap entry whose location uses
// the builtin:// scheme, using the supplied factories keyed by the built-in id (the URL host,
// e.g. "statefulset" for "builtin://statefulset"). The plugin is registered under the entry's
// ConfigMap name (e.g. "argoproj/statefulset"), which is what a RolloutPlugin references via
// spec.plugin.name
func (pm *DefaultPluginManager) RegisterBuiltinPlugins(items []types.PluginItem, factories map[string]BuiltinPluginFactory, namespace string) error {
	for _, item := range items {
		if item.Type != types.PluginTypeResourcePlugin {
			continue
		}
		u, err := url.Parse(item.Location)
		if err != nil || u.Scheme != BuiltinPluginScheme {
			continue
		}
		id := u.Host
		factory, ok := factories[id]
		if !ok {
			return fmt.Errorf("rolloutPlugins entry %q references unknown built-in plugin %q", item.Name, id)
		}
		if err := pm.RegisterPlugin(item.Name, factory(log.WithField("plugin", item.Name)), namespace); err != nil {
			return err
		}
		pm.mu.Lock()
		pm.builtinEnabled[id] = true
		pm.mu.Unlock()
	}
	return nil
}

// IsBuiltinEnabled reports whether the built-in plugin with the given id
// (the builtin://<id> host, e.g. "statefulset") was enabled via the
// rolloutPlugins ConfigMap and registered at startup.
func (pm *DefaultPluginManager) IsBuiltinEnabled(id string) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.builtinEnabled[id]
}
