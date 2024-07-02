package plugin

import (
	"fmt"
	"path/filepath"

	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"

	"github.com/argoproj/argo-rollouts/utils/config"
)

// GetPluginInfo returns the location & command arguments of the plugin on the filesystem via plugin name. If the plugin is not
// configured in the configmap, an error is returned.
func GetPluginInfo(pluginName string, pluginType types.PluginType) (string, []string, error) {
	configMap, err := config.GetConfig()
	if err != nil {
		return "", nil, fmt.Errorf("failed to get config: %w", err)
	}

	plugin := configMap.GetPlugin(pluginName, pluginType)
	if plugin == nil {
		return "", nil, fmt.Errorf("plugin %s not configured in configmap", pluginName)
	}

	dir, filename, err := config.GetPluginDirectoryAndFilename(plugin.Name)
	if err != nil {
		return "", nil, err
	}
	absFilePath, err := filepath.Abs(filepath.Join(defaults.DefaultRolloutPluginFolder, dir, filename))
	if err != nil {
		return "", nil, fmt.Errorf("failed to get absolute path of plugin folder: %w", err)
	}
	return absFilePath, plugin.Args, nil

}
