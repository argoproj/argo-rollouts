package conditions

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

const (
	// RolloutPluginProgressingReason indicates the RolloutPlugin is progressing
	RolloutPluginProgressingReason = "RolloutPluginProgressing"
	// RolloutPluginProgressingMessage is the message for progressing condition
	RolloutPluginProgressingMessage = "RolloutPlugin is progressing"

	// RolloutPluginPausedReason indicates the RolloutPlugin is paused
	RolloutPluginPausedReason = "RolloutPluginPaused"
	// RolloutPluginPausedMessage is the message for paused condition
	RolloutPluginPausedMessage = "RolloutPlugin is paused"

	// RolloutPluginResumedReason indicates the RolloutPlugin was resumed
	RolloutPluginResumedReason = "RolloutPluginResumed"
	// RolloutPluginResumedMessage is the message for resumed condition
	RolloutPluginResumedMessage = "RolloutPlugin is resumed"

	// RolloutPluginUpdatedReason indicates a new revision has been observed
	RolloutPluginUpdatedReason = "RolloutPluginUpdated"
	// RolloutPluginUpdatedMessage is the message for updated condition
	RolloutPluginUpdatedMessage = "RolloutPlugin updated to revision %s"

	// RolloutPluginTimedOutReason indicates progress deadline exceeded
	RolloutPluginTimedOutReason = "ProgressDeadlineExceeded"
	// RolloutPluginTimedOutMessage is the message for timeout
	RolloutPluginTimedOutMessage = "RolloutPlugin %q has timed out progressing."

	// RolloutPluginAbortedReason indicates the RolloutPlugin was aborted
	RolloutPluginAbortedReason = "RolloutPluginAborted"
	// RolloutPluginAbortedMessage is the message for abort
	RolloutPluginAbortedMessage = "RolloutPlugin aborted"

	// RolloutPluginRestartedReason indicates the RolloutPlugin was restarted from step 0
	RolloutPluginRestartedReason = "RolloutPluginRestarted"

	// RolloutPluginInvalidSpecReason indicates the spec is invalid
	RolloutPluginInvalidSpecReason = "InvalidSpec"

	// RolloutPluginHealthyReason indicates the RolloutPlugin is healthy
	RolloutPluginHealthyReason = "RolloutPluginHealthy"
	// RolloutPluginHealthyMessage is the message for healthy condition
	RolloutPluginHealthyMessage = "RolloutPlugin is healthy"
	// RolloutPluginNotHealthyMessage is the message for not-healthy condition
	RolloutPluginNotHealthyMessage = "RolloutPlugin is not healthy"

	// RolloutPluginCompletedReason indicates the RolloutPlugin has completed
	RolloutPluginCompletedReason = "RolloutPluginCompleted"
	// RolloutPluginCompletedMessage is the message for completed condition
	RolloutPluginCompletedMessage = "RolloutPlugin has completed"

	// RolloutPluginNotCompletedReason indicates a Completed→Progressing regression
	RolloutPluginNotCompletedReason = "RolloutPluginNotCompleted"
	// RolloutPluginNotCompletedMessage is the message for not-completed condition
	RolloutPluginNotCompletedMessage = "RolloutPlugin not completed, started update to revision %s"

	// RolloutPluginAnalysisRunFailedReason indicates an owned AnalysisRun failed
	RolloutPluginAnalysisRunFailedReason = "AnalysisRunFailed"
	// RolloutPluginAnalysisRunFailedMessage is the message for analysis failure
	RolloutPluginAnalysisRunFailedMessage = "AnalysisRun '%s' owned by the RolloutPlugin '%q' failed."

	// RolloutPluginReconciliationErrorReason indicates a reconciliation error
	RolloutPluginReconciliationErrorReason = "ReconciliationError"
	// RolloutPluginReconciliationErrorMessage is the message for reconciliation error
	RolloutPluginReconciliationErrorMessage = "Reconciliation failed with error: %v"
)

// NewRolloutPluginCondition creates a new RolloutPlugin condition
func NewRolloutPluginCondition(condType v1alpha1.RolloutPluginConditionType, status corev1.ConditionStatus, reason, message string) *v1alpha1.RolloutPluginCondition {
	return &v1alpha1.RolloutPluginCondition{
		Type:               condType,
		Status:             status,
		LastUpdateTime:     metav1.Now(),
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
}

// GetRolloutPluginCondition returns the condition with the provided type
func GetRolloutPluginCondition(status v1alpha1.RolloutPluginStatus, condType v1alpha1.RolloutPluginConditionType) *v1alpha1.RolloutPluginCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

// SetRolloutPluginCondition updates the RolloutPlugin to include the provided condition.
func SetRolloutPluginCondition(status *v1alpha1.RolloutPluginStatus, condition v1alpha1.RolloutPluginCondition) bool {
	currentCond := GetRolloutPluginCondition(*status, condition.Type)
	if currentCond != nil && currentCond.Status == condition.Status &&
		currentCond.Reason == condition.Reason && currentCond.Message == condition.Message {
		// Condition is identical - don't update to avoid unnecessary reconciliation
		return false
	}

	// Preserve LastTransitionTime if status hasn't changed
	if currentCond != nil && currentCond.Status == condition.Status {
		condition.LastTransitionTime = currentCond.LastTransitionTime
	}

	newConditions := filterOutRolloutPluginCondition(status.Conditions, condition.Type)
	status.Conditions = append(newConditions, condition)
	return true
}

// RemoveRolloutPluginCondition removes the RolloutPlugin condition with the provided type
func RemoveRolloutPluginCondition(status *v1alpha1.RolloutPluginStatus, condType v1alpha1.RolloutPluginConditionType) {
	status.Conditions = filterOutRolloutPluginCondition(status.Conditions, condType)
}

func filterOutRolloutPluginCondition(conditions []v1alpha1.RolloutPluginCondition, condType v1alpha1.RolloutPluginConditionType) []v1alpha1.RolloutPluginCondition {
	var newConditions []v1alpha1.RolloutPluginCondition
	for _, c := range conditions {
		if c.Type != condType {
			newConditions = append(newConditions, c)
		}
	}
	return newConditions
}

// RolloutPluginTimedOut checks if the RolloutPlugin has timed out based on progressDeadlineSeconds.
func RolloutPluginTimedOut(rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus) bool {
	condition := GetRolloutPluginCondition(*newStatus, v1alpha1.RolloutPluginConditionProgressing)
	if condition == nil || condition.Reason == RolloutPluginAbortedReason || condition.Reason == RolloutPluginPausedReason {
		return false
	}

	// Already timed out - if observedGeneration changed (spec was edited), allow re-evaluation.
	if condition.Reason == RolloutPluginTimedOutReason {
		if newStatus.ObservedGeneration != rolloutPlugin.Generation {
			return false
		}
		return true
	}

	from := condition.LastUpdateTime
	now := timeutil.Now()
	progressDeadlineSeconds := defaults.GetRolloutPluginProgressDeadlineSecondsOrDefault(rolloutPlugin)
	delta := time.Duration(progressDeadlineSeconds) * time.Second
	return from.Add(delta).Before(now)
}

// IsRolloutPluginProgressing returns true if the RolloutPlugin has an active Progressing condition
func IsRolloutPluginProgressing(status *v1alpha1.RolloutPluginStatus) bool {
	cond := GetRolloutPluginCondition(*status, v1alpha1.RolloutPluginConditionProgressing)
	return cond != nil && cond.Status == corev1.ConditionTrue
}

// RolloutPluginIsHealthy returns true if the RolloutPlugin is considered healthy.
func RolloutPluginIsHealthy(rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus) bool {
	if newStatus.Aborted {
		return false
	}
	if IsRolloutPluginProgressing(newStatus) {
		return false
	}
	if newStatus.CurrentRevision == "" || newStatus.CurrentRevision != newStatus.UpdatedRevision {
		return false
	}
	return true
}
