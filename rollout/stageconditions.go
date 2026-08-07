package rollout

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
)

const maxStageConditionMessageLen = 256

var trackedStageConditionTypes = []v1alpha1.RolloutConditionType{
	v1alpha1.RolloutTrafficRoutingApplied,
	v1alpha1.RolloutServicesReconciled,
	v1alpha1.RolloutActuationSucceeded,
}

func truncateStageConditionMessage(message string) string {
	if len(message) <= maxStageConditionMessageLen {
		return message
	}
	return message[:maxStageConditionMessageLen]
}

func (c *rolloutContext) setStageCondition(condType v1alpha1.RolloutConditionType, status corev1.ConditionStatus, reason, message string) {
	if c.stageConditions == nil {
		c.stageConditions = make(map[v1alpha1.RolloutConditionType]v1alpha1.RolloutCondition)
	}
	c.stageConditions[condType] = *conditions.NewRolloutCondition(condType, status, reason, truncateStageConditionMessage(message))
}

// stageConditionFalse reports whether condType was recorded False THIS reconcile. Gates must key
// off current-pass failures, not stale persisted conditions.
func (c *rolloutContext) stageConditionFalse(condType v1alpha1.RolloutConditionType) bool {
	if c.stageConditions == nil {
		return false
	}
	cond, ok := c.stageConditions[condType]
	return ok && cond.Status == corev1.ConditionFalse
}

func (c *rolloutContext) anyStageConditionFalse() bool {
	for _, condType := range trackedStageConditionTypes {
		if c.stageConditionFalse(condType) {
			return true
		}
	}
	return false
}

func (c *rolloutContext) ensureActuationFailureCondition(err error) {
	for _, condType := range trackedStageConditionTypes {
		if c.stageConditionFalse(condType) {
			return
		}
	}
	c.setStageCondition(v1alpha1.RolloutActuationSucceeded, corev1.ConditionFalse, conditions.ActuationErrorReason, err.Error())
}

func (c *rolloutContext) mergeStageConditions(newStatus *v1alpha1.RolloutStatus) {
	if c.rollout.Spec.Strategy.Canary == nil {
		return
	}
	trafficRoutingConfigured := c.rollout.Spec.Strategy.Canary.TrafficRouting != nil

	for _, condType := range trackedStageConditionTypes {
		if condType == v1alpha1.RolloutTrafficRoutingApplied && !trafficRoutingConfigured {
			conditions.RemoveRolloutCondition(newStatus, condType)
			continue
		}

		if cond, ok := c.stageConditions[condType]; ok {
			conditions.SetRolloutCondition(newStatus, cond)
			continue
		}

		prevCond := conditions.GetRolloutCondition(c.rollout.Status, condType)
		if prevCond != nil && prevCond.Status == corev1.ConditionFalse {
			reason := conditions.StageConditionAppliedReason
			if condType == v1alpha1.RolloutServicesReconciled {
				reason = conditions.StageConditionReconciledReason
			}
			conditions.SetRolloutCondition(newStatus, *conditions.NewRolloutCondition(condType, corev1.ConditionTrue, reason, ""))
		}
	}
}
