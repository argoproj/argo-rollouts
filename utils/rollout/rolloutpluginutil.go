package rollout

import (
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
)

// CalculateRolloutPluginPhase calculates the phase and message for a RolloutPlugin
func CalculateRolloutPluginPhase(spec v1alpha1.RolloutPluginSpec, status v1alpha1.RolloutPluginStatus) (v1alpha1.RolloutPluginPhase, string) {
	for _, cond := range status.Conditions {
		if cond.Type == v1alpha1.RolloutPluginConditionInvalidSpec {
			return v1alpha1.RolloutPluginPhaseDegraded, fmt.Sprintf("%s: %s", v1alpha1.RolloutPluginConditionInvalidSpec, cond.Message)
		}
		switch cond.Reason {
		case conditions.RolloutPluginAbortedReason,
			conditions.RolloutPluginTimedOutReason,
			conditions.RolloutPluginAnalysisRunFailedReason,
			conditions.RolloutPluginReconciliationErrorReason:
			return v1alpha1.RolloutPluginPhaseDegraded, fmt.Sprintf("%s: %s", cond.Reason, cond.Message)
		}
	}
	if spec.Paused {
		return v1alpha1.RolloutPluginPhasePaused, "manually paused"
	}
	if len(status.PauseConditions) > 0 {
		return v1alpha1.RolloutPluginPhasePaused, string(status.PauseConditions[0].Reason)
	}
	if conditions.IsRolloutPluginProgressing(&status) {
		return v1alpha1.RolloutPluginPhaseProgressing, status.Message
	}
	if status.CurrentRevision != status.UpdatedRevision {
		return v1alpha1.RolloutPluginPhaseProgressing, "waiting for pods to converge"
	}
	return v1alpha1.RolloutPluginPhaseHealthy, ""
}
