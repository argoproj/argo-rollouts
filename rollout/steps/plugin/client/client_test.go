package client

import (
	"fmt"
	"os/exec"
	"sync"
	"testing"

	goPlugin "github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

func resetStepSingleton() {
	once = sync.Once{}
	pluginClients = nil
}

func overrideStepGetPluginInfo(path string, err error) func() {
	orig := getPluginInfo
	getPluginInfo = func(pluginName string, pluginType types.PluginType) (string, []string, error) {
		return path, nil, err
	}
	return func() { getPluginInfo = orig }
}

func overrideStepNewClient(c *goPlugin.Client) func() {
	orig := newClient
	newClient = func(config *goPlugin.ClientConfig) *goPlugin.Client {
		return c
	}
	return func() { newClient = orig }
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
	defer overrideStepGetPluginInfo("", fmt.Errorf("plugin not found"))()

	result, err := GetPlugin("test-plugin")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unable to start plugin system")
}

func TestGetPlugin_NewClientCalled(t *testing.T) {
	resetStepSingleton()
	defer overrideStepGetPluginInfo("/nonexistent/binary", nil)()

	// Use a real goPlugin.Client pointing to a nonexistent binary — Client() will fail,
	// but this covers the NewClient call and Logger assignment.
	realClient := goPlugin.NewClient(&goPlugin.ClientConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		Cmd:             exec.Command("/nonexistent/binary"),
	})
	defer realClient.Kill()
	defer overrideStepNewClient(realClient)()

	result, err := GetPlugin("test-plugin")

	assert.Error(t, err)
	assert.Nil(t, result)
}
