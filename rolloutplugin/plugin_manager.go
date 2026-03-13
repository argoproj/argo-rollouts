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
	plugins map[string]ResourcePlugin // TODOH Rename?

	// mu protects plugins map
	mu sync.RWMutex
}

// GetGlobalPluginManager returns the singleton plugin manager instance
// This ensures all controllers share the same plugin instances
func GetGlobalPluginManager() *DefaultPluginManager {
	once.Do(func() {
		log.Info("Initializing global plugin manager singleton")
		globalPluginManager = &DefaultPluginManager{
			plugins: make(map[string]ResourcePlugin),
		}
	})
	return globalPluginManager
}

// GetPlugin returns a plugin by name
func (pm *DefaultPluginManager) GetPlugin(name string) (ResourcePlugin, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	plugin, exists := pm.plugins[name]
	if !exists {
		return nil, fmt.Errorf("plugin '%s' not found", name)
	}

	return plugin, nil
}

// RegisterPlugin registers a plugin with a specific name
// This is useful for registering built-in plugins at startup
func (pm *DefaultPluginManager) RegisterPlugin(name string, plugin ResourcePlugin) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.plugins[name]; exists {
		return fmt.Errorf("plugin '%s' already registered", name)
	}

	// Initialize the plugin once during registration
	if err := plugin.Init(); err != nil {
		return fmt.Errorf("failed to initialize plugin '%s': %w", name, err)
	}

	pm.plugins[name] = plugin
	log.WithField("plugin", name).Info("Plugin registered and initialized successfully")

	return nil
}
