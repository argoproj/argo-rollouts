package plugin

import (
	"fmt"
	"path/filepath"

	"github.com/argoproj/argo-rollouts/utils/defaults"

	"github.com/argoproj/argo-rollouts/utils/config"
)

func GetPluginLocation(pluginName string) (string, error) {
	configMap, err := config.GetConfig()
	if err != nil {
		return "", fmt.Errorf("failed to get config: %w", err)
	}

	for _, item := range configMap.GetMetricPluginsConfig() {
		if pluginName == item.Name {
			asbFilePath, err := filepath.Abs(filepath.Join(defaults.DefaultRolloutPluginFolder, item.Name))
			if err != nil {
				return "", fmt.Errorf("failed to get absolute path of plugin folder: %w", err)
			}
			return asbFilePath, nil
		}
	}
	return "", fmt.Errorf("plugin %s not configured in configmap", pluginName)
}
