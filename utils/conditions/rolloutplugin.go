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
	// RolloutPluginInvalidSpec indicates the RolloutPlugin spec is invalid
	RolloutPluginInvalidSpec = "InvalidSpec"

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

	// RolloutPluginInvalidSpecReason indicates the spec is invalid
	RolloutPluginInvalidSpecReason = "InvalidSpec"

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
// This prevents unnecessary reconciliation loops caused by timestamp changes.
// Returns true if the condition was updated.
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

	// Preserve LastUpdateTime if nothing has changed (already handled above by returning false)
	// If we reach here, something has changed, so update the timestamp

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

// newInvalidSpecRolloutPluginCondition creates a new InvalidSpec condition for RolloutPlugin
func newInvalidSpecRolloutPluginCondition(prevCond *v1alpha1.RolloutPluginCondition, reason string, message string) *v1alpha1.RolloutPluginCondition {
	if prevCond != nil && prevCond.Message == message {
		prevCond.LastUpdateTime = metav1.Now()
		return prevCond
	}
	return NewRolloutPluginCondition(RolloutPluginInvalidSpec, corev1.ConditionTrue, reason, message)
}

// VerifyRolloutPluginSpec checks for a valid spec and returns an InvalidSpec condition if invalid.
// Returns nil if the spec is valid.
func VerifyRolloutPluginSpec(rolloutPlugin *v1alpha1.RolloutPlugin, prevCond *v1alpha1.RolloutPluginCondition) *v1alpha1.RolloutPluginCondition {
	spec := rolloutPlugin.Spec

	// Validate WorkloadRef
	if spec.WorkloadRef.Name == "" {
		message := "RolloutPlugin spec.workloadRef.name is required"
		return newInvalidSpecRolloutPluginCondition(prevCond, RolloutPluginInvalidSpecReason, message)
	}
	if spec.WorkloadRef.Kind == "" {
		message := "RolloutPlugin spec.workloadRef.kind is required"
		return newInvalidSpecRolloutPluginCondition(prevCond, RolloutPluginInvalidSpecReason, message)
	}
	if spec.WorkloadRef.APIVersion == "" {
		message := "RolloutPlugin spec.workloadRef.apiVersion is required"
		return newInvalidSpecRolloutPluginCondition(prevCond, RolloutPluginInvalidSpecReason, message)
	}

	// Validate Plugin
	if spec.Plugin.Name == "" {
		message := "RolloutPlugin spec.plugin.name is required"
		return newInvalidSpecRolloutPluginCondition(prevCond, RolloutPluginInvalidSpecReason, message)
	}

	// Validate Strategy
	strategyType := spec.Strategy.Type
	if strategyType != "" && strategyType != "Canary" && strategyType != "BlueGreen" {
		message := "RolloutPlugin spec.strategy.type must be 'Canary' or 'BlueGreen'"
		return newInvalidSpecRolloutPluginCondition(prevCond, RolloutPluginInvalidSpecReason, message)
	}

	// Validate strategy type matches strategy configuration
	if strategyType == "Canary" && spec.Strategy.Canary == nil {
		message := "RolloutPlugin spec.strategy.canary is required when strategy type is 'Canary'"
		return newInvalidSpecRolloutPluginCondition(prevCond, RolloutPluginInvalidSpecReason, message)
	}
	if strategyType == "BlueGreen" && spec.Strategy.BlueGreen == nil {
		message := "RolloutPlugin spec.strategy.blueGreen is required when strategy type is 'BlueGreen'"
		return newInvalidSpecRolloutPluginCondition(prevCond, RolloutPluginInvalidSpecReason, message)
	}

	// Validate that only one strategy is specified (not both)
	if spec.Strategy.Canary != nil && spec.Strategy.BlueGreen != nil {
		message := "RolloutPlugin cannot have both canary and blueGreen strategies specified"
		return newInvalidSpecRolloutPluginCondition(prevCond, RolloutPluginInvalidSpecReason, message)
	}

	// Infer strategy type if not explicitly set
	if strategyType == "" {
		if spec.Strategy.Canary == nil && spec.Strategy.BlueGreen == nil {
			message := "RolloutPlugin must have either canary or blueGreen strategy specified"
			return newInvalidSpecRolloutPluginCondition(prevCond, RolloutPluginInvalidSpecReason, message)
		}
	}

	// Validate RestartAt is within bounds if set
	if spec.RestartAt != nil {
		restartAt := *spec.RestartAt
		if restartAt < 0 {
			message := "RolloutPlugin spec.restartAt cannot be negative"
			return newInvalidSpecRolloutPluginCondition(prevCond, RolloutPluginInvalidSpecReason, message)
		}
		// Validate restartAt is within the step count if canary strategy with steps
		if spec.Strategy.Canary != nil && spec.Strategy.Canary.Steps != nil {
			stepCount := int32(len(spec.Strategy.Canary.Steps))
			if restartAt >= stepCount {
				message := "RolloutPlugin spec.restartAt exceeds the number of steps"
				return newInvalidSpecRolloutPluginCondition(prevCond, RolloutPluginInvalidSpecReason, message)
			}
		}
	}

	// Validate minReadySeconds
	if spec.MinReadySeconds < 0 {
		message := "RolloutPlugin spec.minReadySeconds cannot be negative"
		return newInvalidSpecRolloutPluginCondition(prevCond, RolloutPluginInvalidSpecReason, message)
	}

	// Validate progressDeadlineSeconds
	if spec.ProgressDeadlineSeconds != nil && *spec.ProgressDeadlineSeconds <= 0 {
		message := "RolloutPlugin spec.progressDeadlineSeconds must be greater than 0"
		return newInvalidSpecRolloutPluginCondition(prevCond, RolloutPluginInvalidSpecReason, message)
	}

	return nil
}
