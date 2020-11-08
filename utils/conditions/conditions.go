package conditions

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

const (
	// InvalidSpecReason indicates that the spec is invalid
	InvalidSpecReason = "InvalidSpec"
	// MissingFieldMessage the message to indicate rollout is missing a field
	MissingFieldMessage = "Rollout has missing field '%s'"
	// RolloutSelectAllMessage the message to indicate that the rollout has an empty selector
	RolloutSelectAllMessage = "This rollout is selecting all pods. A non-empty selector is required."
	// AvailableReason the reason to indicate that the rollout is serving traffic from the active service
	AvailableReason = "AvailableReason"
	// NotAvailableMessage the message to indicate that the Rollout does not have min availability
	NotAvailableMessage = "Rollout does not have minimum availability"
	// AvailableMessage the message to indicate that the Rollout does have min availability
	AvailableMessage = "Rollout has minimum availability"

	// Reasons and Messages for rollout Progressing Condition

	// ReplicaSetUpdatedReason is added in a rollout when one of its replica sets is updated as part
	// of the rollout process.
	ReplicaSetUpdatedReason = "ReplicaSetUpdated"
	// RolloutProgressingMessage is added in a rollout when one of its replica sets is updated as part
	// of the rollout process.
	RolloutProgressingMessage = "Rollout %q is progressing."
	// ReplicaSetProgressingMessage is added in a rollout when one of its replica sets is updated as part
	// of the rollout process.
	ReplicaSetProgressingMessage = "ReplicaSet %q is progressing."
	// FailedRSCreateReason is added in a rollout when it cannot create a new replica set.
	FailedRSCreateReason = "ReplicaSetCreateError"
	// FailedRSCreateMessage is added in a rollout when it cannot create a new replica set.
	FailedRSCreateMessage = "Failed to create new replica set %q: %v"

	// NewReplicaSetReason is added in a rollout when it creates a new replica set.
	NewReplicaSetReason = "NewReplicaSetCreated"
	//NewReplicaSetMessage is added in a rollout when it creates a new replicas \set.
	NewReplicaSetMessage = "Created new replica set %q"
	// FoundNewRSReason is added in a rollout when it adopts an existing replica set.
	FoundNewRSReason = "FoundNewReplicaSet"
	// FoundNewRSMessage is added in a rollout when it adopts an existing replica set.
	FoundNewRSMessage = "Found new replica set %q"

	// RolloutAbortedReason indicates that the rollout was aborted
	RolloutAbortedReason = "RolloutAborted"
	// RolloutAbortedMessage indicates that the rollout was aborted
	RolloutAbortedMessage = "Rollout is aborted"
	// RolloutRetryReason indicates that the rollout is retrying after being aborted
	RolloutRetryReason = "RolloutRetry"
	// RolloutRetryMessage indicates that the rollout is retrying after being aborted
	RolloutRetryMessage = "Retrying Rollout after abort"

	// NewRSAvailableReason is added in a rollout when its newest replica set is made available
	// ie. the number of new pods that have passed readiness checks and run for at least minReadySeconds
	// is at least the minimum available pods that need to run for the rollout.
	NewRSAvailableReason = "NewReplicaSetAvailable"
	// RolloutAnalysisRunFailedReason is added in a rollout when the analysisRun owned by a rollout fails or errors out
	RolloutAnalysisRunFailedReason = "AnalysisRunFailed"
	// RolloutAnalysisRunFailedMessage is added in a rollout when the analysisRun owned by a rollout fails or errors out
	RolloutAnalysisRunFailedMessage = "AnalysisRun '%s' owned by the Rollout '%q' failed."
	// RolloutExperimentFailedReason is added in a rollout when the analysisRun owned by a rollout fails to show any progress
	RolloutExperimentFailedReason = "ExperimentFailed"
	// RolloutExperimentFailedMessage is added in a rollout when the experiment owned by a rollout fails to show any progress
	RolloutExperimentFailedMessage = "Experiment '%s' owned by the Rollout '%q' has timed out."
	// TimedOutReason is added in a rollout when its newest replica set fails to show any progress
	// within the given deadline (progressDeadlineSeconds).
	TimedOutReason = "ProgressDeadlineExceeded"
	// RolloutTimeOutMessage is is added in a rollout when the rollout fails to show any progress
	// within the given deadline (progressDeadlineSeconds).
	RolloutTimeOutMessage = "Rollout %q has timed out progressing."
	// ReplicaSetTimeOutMessage is added in a rollout when its newest replica set fails to show any progress
	// within the given deadline (progressDeadlineSeconds).
	ReplicaSetTimeOutMessage = "ReplicaSet %q has timed out progressing."

	// RolloutCompletedMessage is added when the rollout is completed
	RolloutCompletedMessage = "Rollout %q has successfully progressed."
	// ReplicaSetCompletedMessage is added when the rollout is completed
	ReplicaSetCompletedMessage = "ReplicaSet %q has successfully progressed."

	// PausedRolloutReason is added in a rollout when it is paused. Lack of progress shouldn't be
	// estimated once a rollout is paused.
	PausedRolloutReason = "RolloutPaused"
	// PausedRolloutMessage is added in a rollout when it is paused. Lack of progress shouldn't be
	// estimated once a rollout is paused.
	PausedRolloutMessage = "Rollout is paused"
	// ResumedRolloutReason is added in a rollout when it is resumed. Useful for not failing accidentally
	// rollout that paused amidst a rollout and are bounded by a deadline.
	ResumedRolloutReason = "RolloutResumed"
	// ResumeRolloutMessage is added in a rollout when it is resumed. Useful for not failing accidentally
	// rollout that paused amidst a rollout and are bounded by a deadline.
	ResumeRolloutMessage = "Rollout is resumed"
	// ServiceNotFoundReason is added in a rollout when the service defined in the spec is not found
	ServiceNotFoundReason = "ServiceNotFound"
	// ServiceNotFoundMessage is added in a rollout when the service defined in the spec is not found
	ServiceNotFoundMessage = "Service %q is not found"
	// ServiceReferenceReason is added to a Rollout when there is an error with a Service reference
	ServiceReferenceReason = "ServiceReferenceError"
	// ServiceReferencingManagedService is added in a rollout when the multiple rollouts reference a Rollout
	ServiceReferencingManagedService = "Service %q is managed by another Rollout"
)

// NewRolloutCondition creates a new rollout condition.
func NewRolloutCondition(condType v1alpha1.RolloutConditionType, status corev1.ConditionStatus, reason, message string) *v1alpha1.RolloutCondition {
	return &v1alpha1.RolloutCondition{
		Type:               condType,
		Status:             status,
		LastUpdateTime:     metav1.Now(),
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
}

// GetRolloutCondition returns the condition with the provided type.
func GetRolloutCondition(status v1alpha1.RolloutStatus, condType v1alpha1.RolloutConditionType) *v1alpha1.RolloutCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

// SetRolloutCondition updates the rollout to include the provided condition. If the condition that
// we are about to add already exists and has the same status and reason then we are not going to update.
func SetRolloutCondition(status *v1alpha1.RolloutStatus, condition v1alpha1.RolloutCondition) {
	currentCond := GetRolloutCondition(*status, condition.Type)
	if currentCond != nil && currentCond.Status == condition.Status && currentCond.Reason == condition.Reason {
		return
	}
	// Do not update lastTransitionTime if the status of the condition doesn't change.
	if currentCond != nil && currentCond.Status == condition.Status {
		condition.LastTransitionTime = currentCond.LastTransitionTime
	}
	newConditions := filterOutCondition(status.Conditions, condition.Type)
	status.Conditions = append(newConditions, condition)
}

// RemoveRolloutCondition removes the rollout condition with the provided type.
func RemoveRolloutCondition(status *v1alpha1.RolloutStatus, condType v1alpha1.RolloutConditionType) {
	status.Conditions = filterOutCondition(status.Conditions, condType)
}

// filterOutCondition returns a new slice of rollout conditions without conditions with the provided type.
func filterOutCondition(conditions []v1alpha1.RolloutCondition, condType v1alpha1.RolloutConditionType) []v1alpha1.RolloutCondition {
	var newConditions []v1alpha1.RolloutCondition
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}

// RolloutProgressing reports progress for a rollout. Progress is estimated by comparing the
// current with the new status of the rollout that the controller is observing. More specifically,
// when new pods are scaled up, become ready or available, old pods are scaled down, or we modify the
// services, then we consider the rollout is progressing.
func RolloutProgressing(rollout *v1alpha1.Rollout, newStatus *v1alpha1.RolloutStatus) bool {
	oldStatus := rollout.Status

	strategySpecificProgress := false
	if rollout.Spec.Strategy.BlueGreen != nil {
		activeSelectorChange := rollout.Status.BlueGreen.ActiveSelector != newStatus.BlueGreen.ActiveSelector
		previewSelectorChange := rollout.Status.BlueGreen.PreviewSelector != newStatus.BlueGreen.PreviewSelector
		strategySpecificProgress = activeSelectorChange || previewSelectorChange
	}

	if rollout.Spec.Strategy.Canary != nil {
		stableRSChange := newStatus.StableRS != oldStatus.StableRS
		incrementStepIndex := false
		if newStatus.CurrentStepIndex != nil && oldStatus.CurrentStepIndex != nil {
			incrementStepIndex = *newStatus.CurrentStepIndex != *oldStatus.CurrentStepIndex
		}
		stepsHashChange := newStatus.CurrentStepHash != oldStatus.CurrentStepHash
		strategySpecificProgress = stableRSChange || incrementStepIndex || stepsHashChange
	}

	// Old replicas that need to be scaled down
	oldStatusOldReplicas := oldStatus.Replicas - oldStatus.UpdatedReplicas
	newStatusOldReplicas := newStatus.Replicas - newStatus.UpdatedReplicas

	return (newStatus.UpdatedReplicas != oldStatus.UpdatedReplicas) ||
		(newStatusOldReplicas < oldStatusOldReplicas) ||
		newStatus.ReadyReplicas > rollout.Status.ReadyReplicas ||
		newStatus.AvailableReplicas > rollout.Status.AvailableReplicas ||
		strategySpecificProgress
}

// RolloutComplete considers a rollout to be complete once all of its desired replicas
// are updated, available, and receiving traffic from the active service, and no old pods are running.
func RolloutComplete(rollout *v1alpha1.Rollout, newStatus *v1alpha1.RolloutStatus) bool {
	completedStrategy := true
	replicas := defaults.GetReplicasOrDefault(rollout.Spec.Replicas)

	if rollout.Spec.Strategy.BlueGreen != nil {
		activeSelectorComplete := newStatus.BlueGreen.ActiveSelector == newStatus.CurrentPodHash
		previewSelectorComplete := true
		if rollout.Spec.Strategy.BlueGreen.PreviewService != "" {
			previewSelectorComplete = newStatus.BlueGreen.PreviewSelector == newStatus.CurrentPodHash
		}
		completedStrategy = activeSelectorComplete && previewSelectorComplete
	}
	if rollout.Spec.Strategy.Canary != nil {
		stepCount := len(rollout.Spec.Strategy.Canary.Steps)
		executedAllSteps := true
		if stepCount > 0 && newStatus.CurrentStepIndex != nil {
			executedAllSteps = int32(stepCount) == *newStatus.CurrentStepIndex
		}
		currentRSIsStable := newStatus.StableRS != "" && newStatus.StableRS == newStatus.CurrentPodHash
		scaleDownOldReplicas := newStatus.Replicas == replicas
		completedStrategy = executedAllSteps && currentRSIsStable && scaleDownOldReplicas
	}

	return newStatus.UpdatedReplicas == replicas &&
		newStatus.AvailableReplicas == replicas &&
		rollout.Status.ObservedGeneration == strconv.Itoa(int(rollout.Generation)) &&
		completedStrategy
}

// ComputeStepHash returns a hash value calculated from the Rollout's steps. The hash will
// be safe encoded to avoid bad words.
func ComputeStepHash(rollout *v1alpha1.Rollout) string {
	if rollout.Spec.Strategy.BlueGreen != nil || rollout.Spec.Strategy.Canary == nil {
		return ""
	}
	rolloutStepHasher := fnv.New32a()
	stepsBytes, err := json.Marshal(rollout.Spec.Strategy.Canary.Steps)
	if err != nil {
		panic(err)
	}
	_, err = rolloutStepHasher.Write(stepsBytes)
	if err != nil {
		panic(err)
	}
	return rand.SafeEncodeString(fmt.Sprint(rolloutStepHasher.Sum32()))
}

// RolloutTimedOut considers a rollout to have timed out once its condition that reports progress
// is older than progressDeadlineSeconds or a Progressing condition with a TimedOutReason reason already
// exists.
func RolloutTimedOut(rollout *v1alpha1.Rollout, newStatus *v1alpha1.RolloutStatus) bool {
	// Look for the Progressing condition. If it doesn't exist, we have no base to estimate progress.
	// If it's already set with a TimedOutReason reason, we have already timed out, no need to check
	// again.
	condition := GetRolloutCondition(*newStatus, v1alpha1.RolloutProgressing)
	// When a rollout is retried, the controller should not evaluate for a timeout based on the
	// aborted condition because the abort could have happened a while back and the rollout should
	// not enter degraded as a result of that
	if condition == nil || condition.Reason == RolloutAbortedReason {
		return false
	}

	if condition.Reason == TimedOutReason {
		return true
	}

	// Look at the difference in seconds between now and the last time we reported any
	// progress or tried to create a replica set, or resumed a paused rollout and
	// compare against progressDeadlineSeconds.
	from := condition.LastUpdateTime
	now := time.Now()

	progressDeadlineSeconds := defaults.GetProgressDeadlineSecondsOrDefault(rollout)
	delta := time.Duration(progressDeadlineSeconds) * time.Second
	timedOut := from.Add(delta).Before(now)
	logCtx := logutil.WithRollout(rollout)

	logCtx.Infof("Timed out (%t) [last progress check: %v - now: %v]", timedOut, from, now)
	return timedOut
}

// ReplicaSetToRolloutCondition converts a replica set condition into a rollout condition.
// Useful for promoting replica set failure conditions into rollout.
func ReplicaSetToRolloutCondition(cond appsv1.ReplicaSetCondition) v1alpha1.RolloutCondition {
	return v1alpha1.RolloutCondition{
		Type:               v1alpha1.RolloutConditionType(cond.Type),
		Status:             cond.Status,
		LastTransitionTime: cond.LastTransitionTime,
		LastUpdateTime:     cond.LastTransitionTime,
		Reason:             cond.Reason,
		Message:            cond.Message,
	}
}
