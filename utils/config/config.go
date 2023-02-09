package config

import (
	"context"
	"fmt"
	"path"
	"regexp"
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

var re = regexp.MustCompile(`^([a-zA-Z0-9.\-]+\.+[a-zA-Z0-9]+)\/{1}([a-zA-Z0-9\-]+)\/{1}([a-zA-Z0-9_\-.]+)$`)

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

	err = configMemoryCache.ValidateConfig()
	if err != nil {
		return nil, fmt.Errorf("validation of config due to (%w)", err)
	}

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

// UnInitializeConfig resets the in memory config to nil. This is useful for testing.
func UnInitializeConfig() {
	mutex.Lock()
	defer mutex.Unlock()
	configMemoryCache = nil
}

// GetMetricPluginsConfig returns the metric plugins configured in the configmap
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
	mutex.RLock()
	defer mutex.RUnlock()
	var copiedPlugins []types.PluginItem
	for _, p := range configMemoryCache.plugins.Trafficrouters {
		copiedPlugins = append(copiedPlugins, p)
	}
	return copiedPlugins
}

// GetAllPlugins returns a flattened list of plugin items. This is useful for iterating over all plugins.
func (c *Config) GetAllPlugins() []types.PluginItem {
	var copiedPlugins []types.PluginItem
	copiedPlugins = append(c.GetTrafficPluginsConfig(), c.GetMetricPluginsConfig()...)
	return copiedPlugins
}

// GetPluginDirectoryAndFilename this functions return the directory and file name from a given pluginName such as
// github.com/argoproj-labs/sample-plugin
func GetPluginDirectoryAndFilename(pluginRepository string) (directory string, filename string, err error) {
	matches := re.FindAllStringSubmatch(pluginRepository, -1)
	if len(matches) != 1 || len(matches[0]) != 4 {
		return "", "", fmt.Errorf("plugin repository (%s) must be in the format of <domain>/<namespace>/<repo>", pluginRepository)
	}
	domain := matches[0][1]
	namespace := matches[0][2]
	repo := matches[0][3]

	return path.Join(domain, namespace), repo, nil
}

func (c *Config) ValidateConfig() error {
	mutex.RLock()
	defer mutex.RUnlock()

	for _, pluginItem := range c.GetAllPlugins() {
		matches := re.FindAllStringSubmatch(pluginItem.Repository, -1)
		if len(matches) != 1 || len(matches[0]) != 4 {
			return fmt.Errorf("plugin repository (%s) must be in the format of <domain>/<namespace>/<repo>", pluginItem.Name)
		}
	}
	return nil
}
