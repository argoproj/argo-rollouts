package plugin

import (
	"github.com/argoproj/argo-rollouts/metric"
	"github.com/argoproj/argo-rollouts/metricproviders/plugin/client"
	"github.com/argoproj/argo-rollouts/metricproviders/plugin/rpc"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const ProviderType = "RPCPlugin"

type MetricPlugin struct {
	rpc.MetricsPlugin
}

// NewRpcPlugin returns a new RPC plugin with a singleton client
func NewRpcPlugin(metric v1alpha1.Metric) (metric.Provider, error) {
	pluginClient, err := client.GetMetricPlugin(metric)
	if err != nil {
		return nil, err
	}

	return MetricPlugin{
		MetricsPlugin: pluginClient,
	}, nil
}

// Run calls the plugins run method and returns the current measurement
func (m MetricPlugin) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	return m.Run(run, metric)
}

// Resume calls the plugins resume method and returns the current measurement
func (m MetricPlugin) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	return m.Resume(run, metric, measurement)
}

// Terminate calls the plugins terminate method and returns the current measurement
func (m MetricPlugin) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	return m.Terminate(run, metric, measurement)
}

// GarbageCollect calls the plugins garbage collect method
func (m MetricPlugin) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, limit int) error {
	err := m.GarbageCollect(run, metric, limit)
	if err.Error() != "" {
		return err
	}
	return nil
}

// Type returns the provider type
func (m MetricPlugin) Type() string {
	return ProviderType
}

// GetMetadata calls the plugins get metadata method
func (m MetricPlugin) GetMetadata(metric v1alpha1.Metric) map[string]string {
	return m.GetMetadata(metric)
}
