package plugin

import (
	"fmt"
	"path/filepath"

	"github.com/argoproj/argo-rollouts/utils/config"
)

func GetPluginLocation(pluginName string) (string, error) {
	configMap, err := config.GetConfig()
	if err != nil {
		return "", err
	}

	for _, item := range configMap.GetMetricPluginsConfig() {
		if pluginName == item.Name {
			return filepath.Join("/tmp", item.Name), nil
		}
	}
	return "", fmt.Errorf("plugin %s not configured", pluginName)
}
