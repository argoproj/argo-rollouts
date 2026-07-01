package client

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

func resetStepSingleton() {
	once = sync.Once{}
	pluginClients = nil
}

func overrideStepGetPluginInfo(err error) func() {
	orig := getPluginInfo
	getPluginInfo = func(pluginName string, pluginType types.PluginType) (string, []string, error) {
		return "", nil, err
	}
	return func() { getPluginInfo = orig }
}

func TestGetPlugin_NotConfigured(t *testing.T) {
	resetStepSingleton()

	result, err := GetPlugin("nonexistent-plugin")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unable to start plugin system")
}

func TestGetPlugin_GetPluginInfoError(t *testing.T) {
	resetStepSingleton()
	defer overrideStepGetPluginInfo(fmt.Errorf("plugin not found"))()

	result, err := GetPlugin("test-plugin")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unable to start plugin system")
}
