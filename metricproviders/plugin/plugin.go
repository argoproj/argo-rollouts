package plugin

import (
	"fmt"

	"github.com/argoproj/argo-rollouts/metric"
	"github.com/argoproj/argo-rollouts/metricproviders/plugin/client"
	"github.com/argoproj/argo-rollouts/metricproviders/plugin/rpc"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const ProviderType = "RPCPlugin"

type MetricPlugin struct {
	rpc.MetricProviderPlugin
}

// NewRpcPlugin returns a new RPC plugin with a singleton client
func NewRpcPlugin(metric v1alpha1.Metric) (metric.Provider, error) {
	pluginClient, err := client.GetMetricPlugin(metric)
	if err != nil {
		return nil, fmt.Errorf("unable to get metric plugin: %w", err)
	}

	return MetricPlugin{
		MetricProviderPlugin: pluginClient,
	}, nil
}

// GarbageCollect calls the plugins garbage collect method but cast the error back to an "error" type for the internal interface
func (m MetricPlugin) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, limit int) error {
	resp := m.MetricProviderPlugin.GarbageCollect(run, metric, limit)
	if resp.HasError() {
		return fmt.Errorf("failed to garbage collect via plugin: %w", resp)
	}
	return nil
}
