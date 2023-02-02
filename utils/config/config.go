package config

import (
	"context"
	"fmt"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/ghodss/yaml"
	v1 "k8s.io/api/core/v1"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
)

// Config is the in memory representation of the configmap with some additional fields/functions for ease of use.
type Config struct {
	configMap *v1.ConfigMap
	plugins   types.Plugin
}

var configMemoryCache *Config
var mutex sync.RWMutex

// InitializeConfig initializes the in memory config and downloads the plugins to the filesystem. Subsequent calls to this
// function will update the configmap in memory.
func InitializeConfig(k8sClientset kubernetes.Interface, configMapName string) (*Config, error) {
	configMapCluster, err := k8sClientset.CoreV1().ConfigMaps(defaults.Namespace()).Get(context.Background(), configMapName, metav1.GetOptions{})
	if err != nil {
		if k8errors.IsNotFound(err) {
			configMemoryCache = &Config{} // We create an empty config so that we don't try to initialize again
			// If the configmap is not found, we return
			return configMemoryCache, nil
		}
		return nil, fmt.Errorf("failed to get configmap %s/%s: %w", defaults.Namespace(), configMapName, err)
	}

	plugins := types.Plugin{}
	if err = yaml.Unmarshal([]byte(configMapCluster.Data["plugins"]), &plugins); err != nil {
		return nil, fmt.Errorf("failed to unmarshal plugins while initializing: %w", err)
	}

	mutex.Lock()
	configMemoryCache = &Config{
		configMap: configMapCluster,
		plugins:   plugins,
	}
	mutex.Unlock()

	return configMemoryCache, nil
}

// GetConfig returns the initialized in memory config object if it exists otherwise errors if InitializeConfig has not been called.
func GetConfig() (*Config, error) {
	mutex.RLock()
	defer mutex.RUnlock()
	if configMemoryCache == nil {
		return nil, fmt.Errorf("config not initialized, please initialize before use")
	}
	return configMemoryCache, nil
}

// GetMetricPluginsConfig returns the metric plugins configured in the configmap for metric providers
func (c *Config) GetMetricPluginsConfig() []types.PluginItem {
	mutex.RLock()
	defer mutex.RUnlock()
	var copiedPlugins []types.PluginItem
	for _, p := range configMemoryCache.plugins.Metrics {
		copiedPlugins = append(copiedPlugins, p)
	}
	return copiedPlugins
}

// GetTrafficPluginsConfig returns the metric plugins configured in the configmap for traffic routers
func (c *Config) GetTrafficPluginsConfig() []types.PluginItem {
	return configMemoryCache.plugins.Trafficrouters
}

// GetAllPlugins returns a flattened list of plugin items. This is useful for iterating over all plugins.
func (c *Config) GetAllPlugins() []types.PluginItem {
	return append(configMemoryCache.plugins.Metrics, configMemoryCache.plugins.Trafficrouters...)
}
