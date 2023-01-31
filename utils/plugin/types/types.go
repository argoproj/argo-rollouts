package types

import "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"

type RpcError struct {
	ErrorString string
}

func (e RpcError) Error() string {
	return e.ErrorString
}

type RpcMetricProvider interface {
	// Run start a new external system call for a measurement
	// Should be idempotent and do nothing if a call has already been started
	Run(*v1alpha1.AnalysisRun, v1alpha1.Metric) v1alpha1.Measurement
	// Checks if the external system call is finished and returns the current measurement
	Resume(*v1alpha1.AnalysisRun, v1alpha1.Metric, v1alpha1.Measurement) v1alpha1.Measurement
	// Terminate will terminate an in-progress measurement
	Terminate(*v1alpha1.AnalysisRun, v1alpha1.Metric, v1alpha1.Measurement) v1alpha1.Measurement
	// GarbageCollect is used to garbage collect completed measurements to the specified limit
	GarbageCollect(*v1alpha1.AnalysisRun, v1alpha1.Metric, int) RpcError
	// Type gets the provider type
	Type() string
	// GetMetadata returns any additional metadata which providers need to store/display as part
	// of the metric result. For example, Prometheus uses is to store the final resolved queries.
	GetMetadata(metric v1alpha1.Metric) map[string]string
}

type Plugin struct {
	Metrics []PluginItem `json:"metrics" yaml:"metrics"`
}

type PluginItem struct {
	Name           string `json:"name" yaml:"name"`
	PluginLocation string `json:"pluginLocation" yaml:"pluginLocation"`
	PluginSha256   string `json:"pluginSha256" yaml:"pluginSha256"`
}
