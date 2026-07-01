package client

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

func resetMetricSingleton() {
	once = sync.Once{}
	pluginClients = nil
}

func overrideMetricGetPluginInfo(err error) func() {
	orig := getPluginInfo
	getPluginInfo = func(pluginName string, pluginType types.PluginType) (string, []string, error) {
		return "", nil, err
	}
	return func() { getPluginInfo = orig }
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
	defer overrideMetricGetPluginInfo(fmt.Errorf("plugin not found"))()

	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			Plugin: map[string]json.RawMessage{
				"test-plugin": json.RawMessage(`{}`),
			},
		},
	}

	result, err := GetMetricPlugin(metric)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unable to start plugin system")
}
