package trace

import "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"

// AnalysisPhaseValue bounds externally stored phase values to known vocabulary.
func AnalysisPhaseValue(phase v1alpha1.AnalysisPhase) string {
	switch phase {
	case v1alpha1.AnalysisPhasePending, v1alpha1.AnalysisPhaseRunning,
		v1alpha1.AnalysisPhaseSuccessful, v1alpha1.AnalysisPhaseFailed,
		v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseInconclusive:
		return string(phase)
	default:
		return "Unknown"
	}
}

// RolloutPhaseValue bounds externally stored phase values to known vocabulary.
func RolloutPhaseValue(phase v1alpha1.RolloutPhase) string {
	switch phase {
	case v1alpha1.RolloutPhaseHealthy, v1alpha1.RolloutPhasePaused,
		v1alpha1.RolloutPhaseProgressing, v1alpha1.RolloutPhaseDegraded:
		return string(phase)
	default:
		return "Unknown"
	}
}
