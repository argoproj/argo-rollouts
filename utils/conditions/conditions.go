package conditions

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"reflect"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/validation"
	hashutil "k8s.io/kubernetes/pkg/util/hash"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

const (
	// Verify Spec constants

	// InvalidSpecReason indicates that the spec is invalid
	InvalidSpecReason = "InvalidSpec"
	// MissingFieldMessage the message to indicate rollout is missing a field
	MissingFieldMessage = "Rollout has missing field '%s'"
	// SelectAllMessage the message to indicate that the rollout has an empty selector
	SelectAllMessage = "This rollout is selecting all pods. A non-empty selector is required."
	// InvalidSetWeightMessage indicates the setweight value needs to be between 0 and 100
	InvalidSetWeightMessage = "SetWeight needs to be between 0 and 100"
	// InvalidDurationMessage indicates the Duration value needs to be greater than 0
	InvalidDurationMessage = "Duration needs to be greater than 0"
	// InvalidMaxSurgeMaxUnavailable indicates both maxSurge and MaxUnavailable can not be set to zero
	InvalidMaxSurgeMaxUnavailable = "MaxSurge and MaxUnavailable both can not be zero"
	// InvalidStepMessage indicates that a step must have either setWeight or pause set
	InvalidStepMessage = "Step must have either setWeight or pause set"
	// ScaleDownDelayLongerThanDeadlineMessage indicates the ScaleDownDelaySeconds is longer than ProgressDeadlineSeconds
	ScaleDownDelayLongerThanDeadlineMessage = "ScaleDownDelaySeconds cannot be longer than ProgressDeadlineSeconds"
	// MinReadyLongerThanDeadlineMessage indicates the MinReadySeconds is longer than ProgressDeadlineSeconds
	MinReadyLongerThanDeadlineMessage = "MinReadySeconds cannot be longer than ProgressDeadlineSeconds"
	// InvalidStrategyMessage indiciates that multiple strategies can not be listed
	InvalidStrategyMessage = "Multiple Strategies can not be listed"
	// DuplicatedServicesMessage the message to indicate that the rollout uses the same service for the active and preview services
	DuplicatedServicesMessage = "This rollout uses the same service for the active and preview services, but two different services are required."
	// ScaleDownLimitLargerThanRevisionLimit the message to indicate that the rollout's revision history limit can not be smaller than the rollout's scale down limit
	ScaleDownLimitLargerThanRevisionLimit = "This rollout's revision history limit can not be smaller than the rollout's scale down limit"
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
	// RolloutProgessingMessage is added in a rollout when one of its replica sets is updated as part
	// of the rollout process.
	RolloutProgressingMessage = "Rollout %q is progressing."
	// ReplicaSetProgessingMessage is added in a rollout when one of its replica sets is updated as part
	// of the rollout process.
	ReplicaSetProgressingMessage = "ReplicaSet %q is progressing."
	// FailedRSCreateReason is added in a rollout when it cannot create a new replica set.
	FailedRSCreateReason = "ReplicaSetCreateError"
	// FailedRSCreateMessage is added in a rollout when it cannot create a new replica set.
	FailedRSCreateMessage = "Failed to create new replica set %q: %v"

	// NewReplicaSetReason is added in a rollout when it creates a new replica set.
	NewReplicaSetReason = "NewReplicaSetCreated"
	//NewReplicasSetMessage is added in a rollout when it creates a new replicas \set.
	NewReplicaSetMessage = "Created new replica set %q"
	// FoundNewRSReason is added in a rollout when it adopts an existing replica set.
	FoundNewRSReason = "FoundNewReplicaSet"
	// FoundNewRSMessage is added in a rollout when it adopts an existing replica set.
	FoundNewRSMessage = "Found new replica set %q"

	// NewRSAvailableReason is added in a rollout when its newest replica set is made available
	// ie. the number of new pods that have passed readiness checks and run for at least minReadySeconds
	// is at least the minimum available pods that need to run for the rollout.
	NewRSAvailableReason = "NewReplicaSetAvailable"

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
	if rollout.Spec.Strategy.BlueGreenStrategy != nil {
		activeSelectorChange := rollout.Status.BlueGreen.ActiveSelector != newStatus.BlueGreen.ActiveSelector
		previewSelectorChange := rollout.Status.BlueGreen.PreviewSelector != newStatus.BlueGreen.PreviewSelector
		strategySpecificProgress = activeSelectorChange || previewSelectorChange
	}

	if rollout.Spec.Strategy.CanaryStrategy != nil {
		stableRSChange := newStatus.Canary.StableRS != oldStatus.Canary.StableRS
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
	if rollout.Spec.Strategy.BlueGreenStrategy != nil {
		activeSelectorComplete := newStatus.BlueGreen.ActiveSelector == newStatus.CurrentPodHash
		previewSelectorComplete := true
		if rollout.Spec.Strategy.BlueGreenStrategy.PreviewService != "" {
			previewSelectorComplete = newStatus.BlueGreen.PreviewSelector == ""
		}
		completedStrategy = activeSelectorComplete && previewSelectorComplete
	}
	if rollout.Spec.Strategy.CanaryStrategy != nil {
		stepCount := len(rollout.Spec.Strategy.CanaryStrategy.Steps)
		executedAllSteps := true
		if stepCount > 0 && newStatus.CurrentStepIndex != nil {
			executedAllSteps = int32(stepCount) == *newStatus.CurrentStepIndex
		}
		currentRSIsStable := newStatus.Canary.StableRS != "" && newStatus.Canary.StableRS == newStatus.CurrentPodHash
		completedStrategy = executedAllSteps && currentRSIsStable
	}

	replicas := defaults.GetRolloutReplicasOrDefault(rollout)
	return newStatus.UpdatedReplicas == replicas &&
		newStatus.Replicas == replicas &&
		newStatus.AvailableReplicas == replicas &&
		rollout.Status.ObservedGeneration == ComputeGenerationHash(rollout.Spec) &&
		completedStrategy
}

// ComputeStepHash returns a hash value calculated from the Rollout's steps. The hash will
// be safe encoded to avoid bad words.
func ComputeStepHash(rollout *v1alpha1.Rollout) string {
	if rollout.Spec.Strategy.BlueGreenStrategy != nil || rollout.Spec.Strategy.CanaryStrategy == nil {
		return ""
	}
	rolloutStepHasher := fnv.New32a()
	stepsBytes, err := json.Marshal(rollout.Spec.Strategy.CanaryStrategy.Steps)
	if err != nil {
		panic(err)
	}
	_, err = rolloutStepHasher.Write(stepsBytes)
	if err != nil {
		panic(err)
	}
	return rand.SafeEncodeString(fmt.Sprint(rolloutStepHasher.Sum32()))
}

// ComputeGenerationHash returns a hash value calculated from the Rollout Spec. The hash will
// be safe encoded to avoid bad words.
func ComputeGenerationHash(spec v1alpha1.RolloutSpec) string {
	rolloutSpecHasher := fnv.New32a()
	hashutil.DeepHashObject(rolloutSpecHasher, spec)
	return rand.SafeEncodeString(fmt.Sprint(rolloutSpecHasher.Sum32()))
}

func newInvalidSpecRolloutCondition(prevCond *v1alpha1.RolloutCondition, reason string, message string) *v1alpha1.RolloutCondition {
	if prevCond != nil && prevCond.Message == message {
		prevCond.LastUpdateTime = metav1.Now()
		return prevCond
	}
	return NewRolloutCondition(v1alpha1.InvalidSpec, corev1.ConditionTrue, reason, message)
}

// VerifyRolloutSpec Checks for a valid spec otherwise returns a invalidSpec condition.
func VerifyRolloutSpec(rollout *v1alpha1.Rollout, prevCond *v1alpha1.RolloutCondition) *v1alpha1.RolloutCondition {
	if rollout.Spec.Selector == nil {
		message := fmt.Sprintf(MissingFieldMessage, ".Spec.Selector")
		return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, message)
	}

	everything := metav1.LabelSelector{}
	if reflect.DeepEqual(rollout.Spec.Selector, &everything) {
		return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, SelectAllMessage)
	}

	if rollout.Spec.Strategy.CanaryStrategy == nil && rollout.Spec.Strategy.BlueGreenStrategy == nil {
		message := fmt.Sprintf(MissingFieldMessage, ".Spec.Strategy.CanaryStrategy or .Spec.Strategy.BlueGreen")
		return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, message)
	}

	if rollout.Spec.Strategy.CanaryStrategy != nil && rollout.Spec.Strategy.BlueGreenStrategy != nil {
		return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, InvalidStrategyMessage)
	}

	if rollout.Spec.MinReadySeconds > defaults.GetProgressDeadlineSecondsOrDefault(rollout) {
		return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, MinReadyLongerThanDeadlineMessage)
	}

	if rollout.Spec.Strategy.BlueGreenStrategy != nil {
		if defaults.GetScaleDownDelaySecondsOrDefault(rollout) > defaults.GetProgressDeadlineSecondsOrDefault(rollout) {
			return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, ScaleDownDelayLongerThanDeadlineMessage)
		}

		if rollout.Spec.Strategy.BlueGreenStrategy.ActiveService == "" {
			message := fmt.Sprintf(MissingFieldMessage, ".Spec.Strategy.BlueGreenStrategy.ActiveService")
			return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, message)
		}
		if rollout.Spec.Strategy.BlueGreenStrategy.ActiveService == rollout.Spec.Strategy.BlueGreenStrategy.PreviewService {
			return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, DuplicatedServicesMessage)
		}
		revisionHistoryLimit := defaults.GetRevisionHistoryLimitOrDefault(rollout)
		if rollout.Spec.Strategy.BlueGreenStrategy.ScaleDownDelayRevisionLimit != nil && revisionHistoryLimit < *rollout.Spec.Strategy.BlueGreenStrategy.ScaleDownDelayRevisionLimit {
			return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, ScaleDownLimitLargerThanRevisionLimit)
		}
	}

	if rollout.Spec.Strategy.CanaryStrategy != nil {
		if invalidMaxSurgeMaxUnavailable(rollout) {
			return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, InvalidMaxSurgeMaxUnavailable)
		}
		for _, step := range rollout.Spec.Strategy.CanaryStrategy.Steps {
			if (step.Pause != nil && step.SetWeight != nil) || (step.Pause == nil && step.SetWeight == nil) {
				return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, InvalidStepMessage)
			}
			if step.SetWeight != nil && (*step.SetWeight < 0 || *step.SetWeight > 100) {
				return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, InvalidSetWeightMessage)
			}
			if step.Pause != nil && step.Pause.Duration != nil && *step.Pause.Duration < 0 {
				return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, InvalidDurationMessage)
			}
		}
	}

	return nil
}

func getPercentValue(intOrStringValue intstr.IntOrString) (int, bool) {
	if intOrStringValue.Type != intstr.String {
		return 0, false
	}
	if len(validation.IsValidPercent(intOrStringValue.StrVal)) != 0 {
		return 0, false
	}
	value, _ := strconv.Atoi(intOrStringValue.StrVal[:len(intOrStringValue.StrVal)-1])
	return value, true
}

func getIntOrPercentValue(intOrStringValue intstr.IntOrString) int {
	value, isPercent := getPercentValue(intOrStringValue)
	if isPercent {
		return value
	}
	return intOrStringValue.IntValue()
}

func invalidMaxSurgeMaxUnavailable(r *v1alpha1.Rollout) bool {
	maxSurge := defaults.GetMaxSurgeOrDefault(r)
	maxUnavailable := defaults.GetMaxUnavailableOrDefault(r)
	maxSurgeValue := getIntOrPercentValue(*maxSurge)
	maxUnavailableValue := getIntOrPercentValue(*maxUnavailable)
	return maxSurgeValue == 0 && maxUnavailableValue == 0
}

// HasRevisionHistoryLimit checks if the RevisionHistoryLimit field is set
func HasRevisionHistoryLimit(r *v1alpha1.Rollout) bool {
	return r.Spec.RevisionHistoryLimit != nil && *r.Spec.RevisionHistoryLimit != math.MaxInt32
}

// RolloutTimedOut considers a rollout to have timed out once its condition that reports progress
// is older than progressDeadlineSeconds or a Progressing condition with a TimedOutReason reason already
// exists.
func RolloutTimedOut(rollout *v1alpha1.Rollout, newStatus *v1alpha1.RolloutStatus) bool {
	// Look for the Progressing condition. If it doesn't exist, we have no base to estimate progress.
	// If it's already set with a TimedOutReason reason, we have already timed out, no need to check
	// again.
	condition := GetRolloutCondition(*newStatus, v1alpha1.RolloutProgressing)
	if condition == nil {
		return false
	}
	// If the previous condition has been a successful rollout then we shouldn't try to
	// estimate any progress. Scenario:
	//
	// * progressDeadlineSeconds is smaller than the difference between now and the time
	//   the last rollout finished in the past.
	// * the creation of a new ReplicaSet triggers a resync of the rollout prior to the
	//   cached copy of the Rollout getting updated with the status.condition that indicates
	//   the creation of the new ReplicaSet.
	//
	// The rollout will be resynced and eventually its Progressing condition will catch
	// up with the state of the world.
	if condition.Reason == NewRSAvailableReason {
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
