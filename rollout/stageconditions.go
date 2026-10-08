package rollout

import (
	"unicode/utf8"

	corev1 "k8s.io/api/core/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
)

const maxStageConditionMessageLen = 256

func truncateStageConditionMessage(message string) string {
	if len(message) <= maxStageConditionMessageLen {
		return message
	}
	// Cut on a rune boundary to keep the message valid UTF-8.
	cut := maxStageConditionMessageLen
	for cut > 0 && !utf8.RuneStart(message[cut]) {
		cut--
	}
	return message[:cut]
}

func (c *rolloutContext) setStageCondition(condType v1alpha1.RolloutConditionType, status corev1.ConditionStatus, reason, message string) {
	if c.stageConditions == nil {
		c.stageConditions = make(map[v1alpha1.RolloutConditionType]v1alpha1.RolloutCondition)
	}
	c.stageConditions[condType] = *conditions.NewRolloutCondition(condType, status, reason, truncateStageConditionMessage(message))
}

// markStageSucceeded lets a False ReconcileSucceeded recover to True this pass.
func (c *rolloutContext) markStageSucceeded() {
	if c.stageSuccesses == nil {
		c.stageSuccesses = make(map[v1alpha1.RolloutConditionType]bool)
	}
	c.stageSuccesses[v1alpha1.RolloutReconcileSucceeded] = true
}

func (c *rolloutContext) mergeStageConditions(newStatus *v1alpha1.RolloutStatus) {
	condType := v1alpha1.RolloutReconcileSucceeded
	if cond, ok := c.stageConditions[condType]; ok {
		conditions.SetRolloutCondition(newStatus, cond)
		return
	}

	// Only recover after a pass that ran every stage without error.
	prevCond := conditions.GetRolloutCondition(c.rollout.Status, condType)
	if prevCond != nil && prevCond.Status == corev1.ConditionFalse && c.stageSuccesses[condType] {
		conditions.SetRolloutCondition(newStatus, *conditions.NewRolloutCondition(
			condType, corev1.ConditionTrue, conditions.StageConditionAppliedReason, ""))
	}
}
