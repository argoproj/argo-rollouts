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
	// Back up to a rune boundary so the cut never splits a multi-byte UTF-8 character, which
	// would persist an invalid-UTF-8 condition message.
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

// markStageSucceeded records that reconcile work completed without error this pass, making a
// previously-False ReconcileSucceeded eligible to recover to True in mergeStageConditions.
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

	// A previously-False condition may only recover to True when reconcile work completed without
	// error this pass. Early exits (e.g. waiting for scaling) leave the condition unchanged.
	prevCond := conditions.GetRolloutCondition(c.rollout.Status, condType)
	if prevCond != nil && prevCond.Status == corev1.ConditionFalse && c.stageSuccesses[condType] {
		conditions.SetRolloutCondition(newStatus, *conditions.NewRolloutCondition(
			condType, corev1.ConditionTrue, conditions.StageConditionAppliedReason, ""))
	}
}
