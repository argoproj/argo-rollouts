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
	// Condition types
	// RolloutPluginProgressing indicates the RolloutPlugin is progressing
	RolloutPluginProgressing = "Progressing"
	// RolloutPluginHealthy indicates the RolloutPlugin is healthy
	RolloutPluginHealthy = "Healthy"
	// RolloutPluginPaused indicates the RolloutPlugin is paused
	RolloutPluginPaused = "Paused"
	// RolloutPluginCompleted indicates the RolloutPlugin has completed
	RolloutPluginCompleted = "Completed"

	// Reasons for Progressing condition
	// RolloutPluginProgressingReason indicates the RolloutPlugin is progressing
	RolloutPluginProgressingReason = "RolloutPluginProgressing"
	// RolloutPluginProgressingMessage is the message for progressing condition
	RolloutPluginProgressingMessage = "RolloutPlugin is progressing"

	// Reasons for Paused condition
	// RolloutPluginPausedReason indicates the RolloutPlugin is paused
	RolloutPluginPausedReason = "RolloutPluginPaused"
	// RolloutPluginPausedMessage is the message for paused condition
	RolloutPluginPausedMessage = "RolloutPlugin is paused"

	// Reasons for failure/timeout
	// RolloutPluginTimedOutReason indicates progress deadline exceeded
	RolloutPluginTimedOutReason = "ProgressDeadlineExceeded"
	// RolloutPluginTimedOutMessage is the message for timeout
	RolloutPluginTimedOutMessage = "RolloutPlugin %q has timed out progressing."

	// RolloutPluginAbortedReason indicates the RolloutPlugin was aborted
	RolloutPluginAbortedReason = "RolloutPluginAborted"
	// RolloutPluginAbortedMessage is the message for abort
	RolloutPluginAbortedMessage = "RolloutPlugin aborted"

	// Reasons for Healthy/Completed condition
	// RolloutPluginHealthyReason indicates the RolloutPlugin is healthy
	RolloutPluginHealthyReason = "RolloutPluginHealthy"
	// RolloutPluginHealthyMessage is the message for healthy condition
	RolloutPluginHealthyMessage = "RolloutPlugin is healthy"
	// RolloutPluginCompletedReason indicates the RolloutPlugin has completed
	RolloutPluginCompletedReason = "RolloutPluginCompleted"
)

// NewRolloutPluginCondition creates a new RolloutPlugin condition
func NewRolloutPluginCondition(condType string, status corev1.ConditionStatus, reason, message string) *v1alpha1.RolloutPluginCondition {
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
func GetRolloutPluginCondition(status v1alpha1.RolloutPluginStatus, condType string) *v1alpha1.RolloutPluginCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

// SetRolloutPluginCondition updates the RolloutPlugin to include the provided condition.
// If the condition already exists with the same status, reason, and message, it is not updated.
// Returns true if the condition was updated.
func SetRolloutPluginCondition(status *v1alpha1.RolloutPluginStatus, condition v1alpha1.RolloutPluginCondition) bool {
	currentCond := GetRolloutPluginCondition(*status, condition.Type)
	if currentCond != nil && currentCond.Status == condition.Status &&
		currentCond.Reason == condition.Reason && currentCond.Message == condition.Message {
		return false
	}

	if currentCond != nil && currentCond.Status == condition.Status {
		condition.LastTransitionTime = currentCond.LastTransitionTime
	}

	newConditions := filterOutRolloutPluginCondition(status.Conditions, condition.Type)
	status.Conditions = append(newConditions, condition)
	return true
}

// RemoveRolloutPluginCondition removes the RolloutPlugin condition with the provided type
func RemoveRolloutPluginCondition(status *v1alpha1.RolloutPluginStatus, condType string) {
	status.Conditions = filterOutRolloutPluginCondition(status.Conditions, condType)
}

func filterOutRolloutPluginCondition(conditions []v1alpha1.RolloutPluginCondition, condType string) []v1alpha1.RolloutPluginCondition {
	var newConditions []v1alpha1.RolloutPluginCondition
	for _, c := range conditions {
		if c.Type != condType {
			newConditions = append(newConditions, c)
		}
	}
	return newConditions
}

// RolloutPluginTimedOut checks if the RolloutPlugin has timed out based on progressDeadlineSeconds
func RolloutPluginTimedOut(rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus) bool {
	condition := GetRolloutPluginCondition(*newStatus, RolloutPluginProgressing)

	// No progressing condition means we haven't started yet
	if condition == nil {
		return false
	}

	// Already timed out
	if condition.Reason == RolloutPluginTimedOutReason {
		return true
	}

	// Don't time out if paused or aborted
	if rolloutPlugin.Spec.Paused || newStatus.Paused || newStatus.Aborted {
		return false
	}

	// Check if we've exceeded the deadline
	from := condition.LastUpdateTime
	now := timeutil.Now()
	progressDeadlineSeconds := defaults.GetRolloutPluginProgressDeadlineSecondsOrDefault(rolloutPlugin)
	delta := time.Duration(progressDeadlineSeconds) * time.Second
	timedOut := from.Add(delta).Before(now)

	return timedOut
}

// RolloutPluginIsHealthy returns true if the RolloutPlugin is considered healthy
func RolloutPluginIsHealthy(rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus) bool {
	// Not in progress and no errors
	if !newStatus.RolloutInProgress && newStatus.Phase != "Failed" && newStatus.Phase != "Degraded" {
		return true
	}
	return false
}
