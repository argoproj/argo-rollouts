package rollout

import (
	"fmt"
	"strconv"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
)

// GetRolloutPhase returns a status and message for a rollout. Takes into consideration whether
// or not metadata.generation was observed in status.observedGeneration
// use this instead of CalculateRolloutPhase
func GetRolloutPhase(ro *v1alpha1.Rollout) (v1alpha1.RolloutPhase, string) {
	if !isGenerationObserved(ro) {
		return v1alpha1.RolloutPhaseProgressing, "waiting for rollout spec update to be observed"
	}

	if ro.Spec.TemplateResolvedFromRef && !isWorkloadGenerationObserved(ro) {
		return v1alpha1.RolloutPhaseProgressing, "waiting for rollout spec update to be observed for the reference workload"
	}

	if ro.Status.Phase != "" {
		// for 1.0+ phase/message is calculated controller side
		return ro.Status.Phase, ro.Status.Message
	}
	// for v0.10 and below, fall back to client-side calculation
	return CalculateRolloutPhase(ro.Spec, ro.Status)
}

// isGenerationObserved determines if the rollout spec has been observed by the controller. This
// only applies to v0.10 rollout which uses a numeric status.observedGeneration. For v0.9 rollouts
// and below this function always returns true.
func isGenerationObserved(ro *v1alpha1.Rollout) bool {
	observedGen, err := strconv.Atoi(ro.Status.ObservedGeneration)
	if err != nil {
		return true
	}
	// It's still possible for a v0.9 rollout to have an all numeric hash, this covers that corner case
	if int64(observedGen) > ro.Generation {
		return true
	}
	return int64(observedGen) == ro.Generation
}

func isWorkloadGenerationObserved(ro *v1alpha1.Rollout) bool {
	if _, ok := annotations.GetWorkloadGenerationAnnotation(ro); !ok {
		return true
	}
	workloadGeneration, _ := annotations.GetWorkloadGenerationAnnotation(ro)
	observedWorkloadGen, err := strconv.ParseInt(ro.Status.WorkloadObservedGeneration, 10, 32)
	if err != nil {
		return true
	}

	return int32(observedWorkloadGen) == workloadGeneration
}

// CalculateRolloutPhase calculates a rollout phase and message for the given rollout based on
// rollout spec and status. This function is intended to be used by the controller (and not
// by clients). Clients should instead call GetRolloutPhase, which takes into consideration
// status.observedGeneration
func CalculateRolloutPhase(spec v1alpha1.RolloutSpec, status v1alpha1.RolloutStatus) (v1alpha1.RolloutPhase, string) {
	ro := v1alpha1.Rollout{
		Spec:   spec,
		Status: status,
	}
	for _, cond := range ro.Status.Conditions {
		if cond.Type == v1alpha1.InvalidSpec {
			return v1alpha1.RolloutPhaseDegraded, fmt.Sprintf("%s: %s", v1alpha1.InvalidSpec, cond.Message)
		}
		switch cond.Reason {
		case conditions.RolloutAbortedReason, conditions.TimedOutReason:
			return v1alpha1.RolloutPhaseDegraded, fmt.Sprintf("%s: %s", cond.Reason, cond.Message)
		}
	}
	if ro.Spec.Paused {
		return v1alpha1.RolloutPhasePaused, "manually paused"
	}
	for _, pauseCond := range ro.Status.PauseConditions {
		return v1alpha1.RolloutPhasePaused, string(pauseCond.Reason)
	}
	if ro.Spec.RestartAt != nil && (ro.Status.RestartedAt == nil || !ro.Spec.RestartAt.Time.Equal(ro.Status.RestartedAt.Time)) {
		return v1alpha1.RolloutPhaseProgressing, "rollout is restarting"
	}
	if ro.Status.UpdatedReplicas < defaults.GetReplicasOrDefault(ro.Spec.Replicas) {
		return v1alpha1.RolloutPhaseProgressing, "more replicas need to be updated"
	}
	if ro.Status.AvailableReplicas < ro.Status.UpdatedReplicas {
		return v1alpha1.RolloutPhaseProgressing, "updated replicas are still becoming available"
	}
	if ro.Spec.Strategy.BlueGreen != nil {
		if ro.Status.BlueGreen.ActiveSelector == "" || ro.Status.BlueGreen.ActiveSelector != ro.Status.CurrentPodHash {
			return v1alpha1.RolloutPhaseProgressing, "active service cutover pending"
		}
		if ro.Status.StableRS == "" || ro.Status.StableRS != ro.Status.CurrentPodHash {
			return v1alpha1.RolloutPhaseProgressing, "waiting for analysis to complete"
		}
	} else if ro.Spec.Strategy.Canary != nil {
		if ro.Spec.Strategy.Canary.TrafficRouting == nil {
			if ro.Status.Replicas > ro.Status.UpdatedReplicas {
				// This check should only be done for basic canary and not blue-green or canary with traffic routing
				// since the latter two have the scaleDownDelay feature which leaves the old stack of replicas
				// running for a long time
				return v1alpha1.RolloutPhaseProgressing, "old replicas are pending termination"
			}
		}
		if ro.Status.StableRS == "" || ro.Status.StableRS != ro.Status.CurrentPodHash {
			return v1alpha1.RolloutPhaseProgressing, "waiting for all steps to complete"
		}
	}
	return v1alpha1.RolloutPhaseHealthy, ""
}

// CanaryStepString returns a string representation of a canary step
func CanaryStepString(c v1alpha1.CanaryStep) string {
	if c.SetWeight != nil {
		return fmt.Sprintf("setWeight: %d", *c.SetWeight)
	}
	if c.Pause != nil {
		str := "pause"
		if c.Pause.Duration != nil {
			str = fmt.Sprintf("%s: %s", str, c.Pause.Duration.String())
		}
		return str
	}
	if c.Experiment != nil {
		return "experiment"
	}
	if c.Analysis != nil {
		return "analysis"
	}
	if c.SetCanaryScale != nil {
		if c.SetCanaryScale.Weight != nil {
			return fmt.Sprintf("setCanaryScale{weight: %d}", *c.SetCanaryScale.Weight)
		} else if c.SetCanaryScale.MatchTrafficWeight {
			return "setCanaryScale{matchTrafficWeight: true}"
		} else if c.SetCanaryScale.Replicas != nil {
			return fmt.Sprintf("setCanaryScale{replicas: %d}", *c.SetCanaryScale.Replicas)
		}
	}
	return "invalid"
}
