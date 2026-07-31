package metrics

import (
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// MetricsRecorder defines the interface for recording rollout metrics
type MetricsRecorder interface {
	IncRolloutReconcile(rollout *v1alpha1.Rollout, duration time.Duration)
	IncExperimentReconcile(ex *v1alpha1.Experiment, duration time.Duration)
	IncAnalysisRunReconcile(ar *v1alpha1.AnalysisRun, duration time.Duration)
	IncError(namespace, name string, kind string)
	EmitRolloutDuration(ds *v1alpha1.RolloutDurationStatus)
	Remove(namespace string, name string, kind string)
}
