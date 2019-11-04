package analysis

import (
	"encoding/json"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	patchtypes "k8s.io/apimachinery/pkg/types"

	argoprojclient "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/typed/rollouts/v1alpha1"
)

// analysisStatusOrder is a list of completed analysis sorted in best to worst condition
var analysisStatusOrder = []v1alpha1.AnalysisStatus{
	v1alpha1.AnalysisStatusSuccessful,
	v1alpha1.AnalysisStatusRunning,
	v1alpha1.AnalysisStatusPending,
	v1alpha1.AnalysisStatusInconclusive,
	v1alpha1.AnalysisStatusError,
	v1alpha1.AnalysisStatusFailed,
}

// IsWorse returns whether or not the new health status code is a worser condition than the current.
// Both statuses must be already completed
func IsWorse(current, new v1alpha1.AnalysisStatus) bool {
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

// Worst returns the worst of the two statuses
func Worst(left, right v1alpha1.AnalysisStatus) v1alpha1.AnalysisStatus {
	if IsWorse(left, right) {
		return right
	}
	return left
}

// IsTerminating returns whether or not the analysis run is terminating, either because a terminate
// was requested explicitly, or because a metric has already measured Failed, Error, or Inconclusive
// which causes the run to end prematurely.
func IsTerminating(run *v1alpha1.AnalysisRun) bool {
	if run.Spec.Terminate {
		return true
	}
	for _, res := range run.Status.MetricResults {
		switch res.Status {
		case v1alpha1.AnalysisStatusFailed, v1alpha1.AnalysisStatusError, v1alpha1.AnalysisStatusInconclusive:
			return true
		}
	}
	return false
}

// GetResult returns the metric result by name
func GetResult(run *v1alpha1.AnalysisRun, metricName string) *v1alpha1.MetricResult {
	for _, result := range run.Status.MetricResults {
		if result.Name == metricName {
			return &result
		}
	}
	return nil
}

// SetResult updates the metric result
func SetResult(run *v1alpha1.AnalysisRun, result v1alpha1.MetricResult) {
	for i, r := range run.Status.MetricResults {
		if r.Name == result.Name {
			run.Status.MetricResults[i] = result
			return
		}
	}
	run.Status.MetricResults = append(run.Status.MetricResults, result)
}

// MetricCompleted returns whether or not a metric was completed or not
func MetricCompleted(run *v1alpha1.AnalysisRun, metricName string) bool {
	if result := GetResult(run, metricName); result != nil {
		return result.Status.Completed()
	}
	return false
}

// LastMeasurement returns the last measurement started or completed for a specific metric
func LastMeasurement(run *v1alpha1.AnalysisRun, metricName string) *v1alpha1.Measurement {
	if result := GetResult(run, metricName); result != nil {
		totalMeasurements := len(result.Measurements)
		if totalMeasurements == 0 {
			return nil
		}
		return &result.Measurements[totalMeasurements-1]
	}
	return nil
}

// TerminateRun terminates an analysis run
func TerminateRun(analysisRunIf argoprojclient.AnalysisRunInterface, name string) error {
	_, err := analysisRunIf.Patch(name, patchtypes.MergePatchType, []byte(`{"spec":{"terminate":true}}`))
	return err
}

// IsSemanticallyEqual checks to see if two analysis runs are semantically equal
func IsSemanticallyEqual(left, right v1alpha1.AnalysisRunSpec) bool {
	leftBytes, err := json.Marshal(left)
	if err != nil {
		panic(err)
	}
	rightBytes, err := json.Marshal(right)
	if err != nil {
		panic(err)
	}
	return string(leftBytes) == string(rightBytes)
}
