package client

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"
	"testing"

	goPlugin "github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

func resetMetricSingleton() {
	once = sync.Once{}
	pluginClients = nil
}

func overrideMetricGetPluginInfo(path string, err error) func() {
	orig := getPluginInfo
	getPluginInfo = func(pluginName string, pluginType types.PluginType) (string, []string, error) {
		return path, nil, err
	}
	return func() { getPluginInfo = orig }
}

func overrideMetricNewClient(c *goPlugin.Client) func() {
	orig := newClient
	newClient = func(config *goPlugin.ClientConfig) *goPlugin.Client {
		return c
	}
	return func() { newClient = orig }
}

func metricWith(pluginName string) v1alpha1.Metric {
	return v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			Plugin: map[string]json.RawMessage{
				pluginName: json.RawMessage(`{}`),
			},
		},
	}
}

func TestGetMetricPlugin_NoPlugin(t *testing.T) {
	resetMetricSingleton()

	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			Plugin: map[string]json.RawMessage{},
		},
	}

	result, err := GetMetricPlugin(metric)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no plugin found")
}

func TestGetMetricPlugin_GetPluginInfoError(t *testing.T) {
	resetMetricSingleton()
	defer overrideMetricGetPluginInfo("", fmt.Errorf("plugin not found"))()

	result, err := GetMetricPlugin(metricWith("test-plugin"))

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unable to start plugin system")
}

func TestGetMetricPlugin_NewClientCalled(t *testing.T) {
	resetMetricSingleton()
	defer overrideMetricGetPluginInfo("/nonexistent/binary", nil)()

	// Use a real goPlugin.Client pointing to a nonexistent binary — Client() will fail,
	// but this covers the NewClient call and Logger assignment.
	realClient := goPlugin.NewClient(&goPlugin.ClientConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
		Cmd:             exec.Command("/nonexistent/binary"),
	})
	defer realClient.Kill()
	defer overrideMetricNewClient(realClient)()

	result, err := GetMetricPlugin(metricWith("test-plugin"))

	assert.Error(t, err)
	assert.Nil(t, result)
}
