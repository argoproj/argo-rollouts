package analysis

import (
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// ValidateAnalysisTemplateSpec validates an analysis template spec
func ValidateAnalysisTemplateSpec(spec v1alpha1.AnalysisTemplateSpec) error {
	if len(spec.Metrics) == 0 {
		return fmt.Errorf("no metrics specified")
	}
	duplicateNames := make(map[string]bool)
	for i, metric := range spec.Metrics {
		if _, ok := duplicateNames[metric.Name]; ok {
			return fmt.Errorf("metrics[%d]: duplicate name '%s", i, metric.Name)
		}
		duplicateNames[metric.Name] = true
		if err := ValidateMetric(metric); err != nil {
			return fmt.Errorf("metrics[%d]: %v", i, err)
		}
	}
	return nil
}

// ValidateMetric validates a single metric spec
func ValidateMetric(metric v1alpha1.Metric) error {
	if metric.Count < metric.MaxFailures {
		return fmt.Errorf("count must be >= maxFailures")
	}
	if metric.Count > 1 && metric.Interval == nil {
		return fmt.Errorf("interval must be specified when count > 1")
	}
	if metric.MaxFailures < 0 {
		return fmt.Errorf("maxFailures must be >= 0")
	}
	if metric.MaxConsecutiveErrors != nil && *metric.MaxConsecutiveErrors < 0 {
		return fmt.Errorf("maxConsecutiveErrors must be >= 0")
	}
	numProviders := 0
	if metric.Provider.Prometheus != nil {
		numProviders++
	}
	if numProviders == 0 {
		return fmt.Errorf("no provider specified")
	}
	if numProviders > 1 {
		return fmt.Errorf("multiple providers specified")
	}
	return nil
}

// analysisStatusOrder is a list of completed analysis sorted in best to worst condition
var analysisStatusOrder = []v1alpha1.AnalysisStatus{
	v1alpha1.AnalysisStatusSuccessful,
	v1alpha1.AnalysisStatusInconclusive,
	v1alpha1.AnalysisStatusError,
	v1alpha1.AnalysisStatusFailed,
}

// IsWorse returns whether or not the new health status code is a worser condition than the current.
// Both statuses must be already completed
func IsWorse(current, new v1alpha1.AnalysisStatus) bool {
	if !current.Completed() || !new.Completed() {
		panic("IsWorse called against incomplete statuses")
	}
	currentIndex := 0
	newIndex := 0
	for i, code := range analysisStatusOrder {
		if current == code {
			currentIndex = i
		}
		if new == code {
			newIndex = i
		}
	}
	return newIndex > currentIndex
}

// IsTerminating returns whether or not the analysis run is terminating
func IsTerminating(run *v1alpha1.AnalysisRun) bool {
	return run.Spec.Terminate || IsFailing(run)
}

// IsFailing returns whether or not any metric has measured Failed or Error, which will eventually
// cause the entire run to fail.
func IsFailing(run *v1alpha1.AnalysisRun) bool {
	for _, res := range run.Status.MetricResults {
		switch res.Status {
		case v1alpha1.AnalysisStatusFailed, v1alpha1.AnalysisStatusError:
			return true
		}
	}
	return false
}

// MetricResult returns the metric result by name
func MetricResult(run *v1alpha1.AnalysisRun, metricName string) *v1alpha1.MetricResult {
	metricResult, ok := run.Status.MetricResults[metricName]
	if !ok {
		return nil
	}
	return &metricResult
}

// MetricCompleted returns whether or not a metric was completed or not
func MetricCompleted(run *v1alpha1.AnalysisRun, metricName string) bool {
	metricResult, ok := run.Status.MetricResults[metricName]
	if !ok {
		return false
	}
	return metricResult.Status.Completed()
}

// LastMeasurement returns the last measurement started or completed for a specific metric
func LastMeasurement(run *v1alpha1.AnalysisRun, metricName string) *v1alpha1.Measurement {
	result, ok := run.Status.MetricResults[metricName]
	if !ok {
		return nil
	}
	totalMeasurements := len(result.Measurements)
	if totalMeasurements == 0 {
		return nil
	}
	return &result.Measurements[totalMeasurements-1]
}

// ConsecutiveErrors returns number of most recent consecutive errors
func ConsecutiveErrors(result v1alpha1.MetricResult) int {
	consecutiveErrors := 0
	for i := len(result.Measurements) - 1; i >= 0; i-- {
		measurement := result.Measurements[i]
		switch measurement.Status {
		case v1alpha1.AnalysisStatusError:
			consecutiveErrors++
		default:
			return consecutiveErrors
		}
	}
	return consecutiveErrors
}
