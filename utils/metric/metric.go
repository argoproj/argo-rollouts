package metric

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

// MarkMeasurementError sets an error message on a measurement along with finish time
func MarkMeasurementError(m v1alpha1.Measurement, err error) v1alpha1.Measurement {
	m.Phase = v1alpha1.AnalysisPhaseError
	m.Message = err.Error()
	if m.FinishedAt == nil {
		finishedTime := timeutil.MetaNow()
		m.FinishedAt = &finishedTime
	}
	return m
}
