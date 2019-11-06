package metric

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// MarkMeasurementError sets an error message on a measurement along with finish time
func MarkMeasurementError(m v1alpha1.Measurement, err error) v1alpha1.Measurement {
	m.Phase = v1alpha1.AnalysisPhaseError
	m.Message = err.Error()
	if m.FinishedAt == nil {
		finishedTime := metav1.Now()
		m.FinishedAt = &finishedTime
	}
	return m
}
