package rolloutplugin

import (
	"fmt"
	"sync"

	log "github.com/sirupsen/logrus"
)

var (
	// globalPluginManager is the singleton instance of the plugin manager
	globalPluginManager *DefaultPluginManager
	// once ensures the plugin manager is initialized only once
	once sync.Once
)

// DefaultPluginManager implements PluginManager
type DefaultPluginManager struct {
	// plugins is a map of plugin name to plugin instance
	plugins map[string]ResourcePlugin

	// namespace is the controller's watch namespace
	namespace string

	// mu protects plugins map
	mu sync.RWMutex
}

// GetGlobalPluginManager returns the singleton plugin manager instance
func GetGlobalPluginManager() *DefaultPluginManager {
	once.Do(func() {
		log.Info("Initializing global plugin manager singleton")
		globalPluginManager = &DefaultPluginManager{
			plugins: make(map[string]ResourcePlugin),
		}
	})
	return globalPluginManager
}

// GetPlugin returns a plugin by name.
// If the plugin is not registered as built-in, it attempts to lazily load it as an external RPC plugin.
func (pm *DefaultPluginManager) GetPlugin(name string) (ResourcePlugin, error) {
	pm.mu.RLock()
	plugin, exists := pm.plugins[name]
	pm.mu.RUnlock()
	if exists {
		return plugin, nil
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Double-check after acquiring write lock
	if plugin, exists := pm.plugins[name]; exists {
		return plugin, nil
	}

	log.WithField("plugin", name).Info("Lazily loading external RPC plugin")
	rpcPlugin, err := NewRpcPlugin(name, pm.namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to load external plugin '%s': %w", name, err)
	}
	pm.plugins[name] = rpcPlugin
	return rpcPlugin, nil
}

// RegisterPlugin registers a plugin with a specific name.
func (pm *DefaultPluginManager) RegisterPlugin(name string, plugin ResourcePlugin, namespace string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.plugins[name]; exists {
		return fmt.Errorf("plugin '%s' already registered", name)
	}

	// Store namespace for lazy-loaded external plugins
	if pm.namespace == "" {
		pm.namespace = namespace
	}

	// Initialize the plugin once during registration
	if err := plugin.Init(namespace); err != nil {
		return fmt.Errorf("failed to initialize plugin '%s': %w", name, err)
	}

	pm.plugins[name] = plugin
	log.WithField("plugin", name).Info("Plugin registered and initialized successfully")

	return nil
}
