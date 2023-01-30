package plugin

import (
	"github.com/argoproj/argo-rollouts/metric"
	"github.com/argoproj/argo-rollouts/metricproviders/plugin/client"
	"github.com/argoproj/argo-rollouts/metricproviders/plugin/rpc"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const ProviderType = "RPCPlugin"

type MetricPlugin struct {
	plugin rpc.MetricsPlugin
	metric.Provider
}

func NewRpcPlugin(metric v1alpha1.Metric) (metric.Provider, error) {
	pluginClient, err := client.GetMetricPlugin(metric)
	if err != nil {
		return nil, err
	}

	return MetricPlugin{plugin: pluginClient}, nil
}

func (m MetricPlugin) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	return m.plugin.Run(run, metric)
}

func (m MetricPlugin) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	return m.plugin.Resume(run, metric, measurement)
}

func (m MetricPlugin) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	return m.plugin.Terminate(run, metric, measurement)
}

func (m MetricPlugin) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, limit int) error {
	err := m.plugin.GarbageCollect(run, metric, limit)
	if err.Error() != "" {
		return err
	}
	return nil
}

func (m MetricPlugin) Type() string {
	return ProviderType
}

func (m MetricPlugin) GetMetadata(metric v1alpha1.Metric) map[string]string {
	return m.plugin.GetMetadata(metric)
}
