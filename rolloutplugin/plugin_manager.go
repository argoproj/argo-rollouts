package rolloutplugin

import (
	"fmt"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// DefaultPluginManager implements PluginManager
type DefaultPluginManager struct {
	// plugins is a map of plugin name to plugin instance
	plugins map[string]ResourcePlugin

	// mu protects plugins map
	mu sync.RWMutex
}

// NewPluginManager creates a new plugin manager
func NewPluginManager() *DefaultPluginManager {
	return &DefaultPluginManager{
		plugins: make(map[string]ResourcePlugin),
	}
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

// LoadPlugin loads a plugin from the given configuration
func (pm *DefaultPluginManager) LoadPlugin(config v1alpha1.PluginConfig) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	log.WithField("plugin", config.Name).Info("Loading plugin")

	// Check if plugin is already loaded
	if _, exists := pm.plugins[config.Name]; exists {
		log.WithField("plugin", config.Name).Info("Plugin already loaded")
		return nil
	}

	// TODO: Implement plugin loading using HashiCorp go-plugin
	// For now, return an error indicating that plugin needs to be registered manually

	if config.Config != nil {
		if resourceType, ok := config.Config["resourceType"]; ok {
			switch resourceType {
			case "StatefulSet":
				// Create StatefulSet plugin
				// plugin, err := NewStatefulSetPlugin(config)
				return fmt.Errorf("StatefulSet plugin not yet implemented")
			default:
				return fmt.Errorf("unknown resource type: %s", resourceType)
			}
		}
	}

	return fmt.Errorf("plugin '%s' must be registered manually using RegisterPlugin", config.Name)
}

// RegisterPlugin registers a plugin with a specific name
// This is useful for registering built-in plugins at startup
func (pm *DefaultPluginManager) RegisterPlugin(name string, plugin ResourcePlugin) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.plugins[name]; exists {
		return fmt.Errorf("plugin '%s' already registered", name)
	}

	pm.plugins[name] = plugin
	log.WithField("plugin", name).Info("Plugin registered successfully")

	return nil
}

// UnloadPlugin unloads a plugin
func (pm *DefaultPluginManager) UnloadPlugin(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.plugins[name]; !exists {
		return fmt.Errorf("plugin '%s' not found", name)
	}

	delete(pm.plugins, name)
	log.WithField("plugin", name).Info("Plugin unloaded successfully")

	return nil
}

// ListPlugins returns a list of loaded plugin names
func (pm *DefaultPluginManager) ListPlugins() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	names := make([]string, 0, len(pm.plugins))
	for name := range pm.plugins {
		names = append(names, name)
	}

	return names
}
