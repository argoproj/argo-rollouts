package plugin

import (
	"fmt"
	"path/filepath"

	"github.com/argoproj/argo-rollouts/utils/defaults"

	"github.com/argoproj/argo-rollouts/utils/config"
)

// GetPluginLocation returns the location of the plugin on the filesystem via plugin name. If the plugin is not
// configured in the configmap, an error is returned.
func GetPluginLocation(pluginName string) (string, error) {
	configMap, err := config.GetConfig()
	if err != nil {
		return "", fmt.Errorf("failed to get config: %w", err)
	}

	for _, item := range configMap.GetAllPlugins() {
		if pluginName == item.Name {
			dir, filename, err := config.GetPluginDirectoryAndFilename(item.Name)
			if err != nil {
				return "", err
			}
			absFilePath, err := filepath.Abs(filepath.Join(defaults.DefaultRolloutPluginFolder, dir, filename))
			if err != nil {
				return "", fmt.Errorf("failed to get absolute path of plugin folder: %w", err)
			}
			return absFilePath, nil
		}
	}
	return "", fmt.Errorf("plugin %s not configured in configmap", pluginName)
}
