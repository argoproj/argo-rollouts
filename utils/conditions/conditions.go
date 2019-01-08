package conditions

import (
	"fmt"
	"hash/fnv"
	"math"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/kubernetes/pkg/controller"
	hashutil "k8s.io/kubernetes/pkg/util/hash"

	"github.com/argoproj/rollout-controller/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/rollout-controller/utils/defaults"
)

const (
	// FailedRSCreateReason is added in a rollout when it cannot create a new replica set.
	FailedRSCreateReason = "ReplicaSetCreateError"
	// Verify Spec constants

	// MissingFieldMessage the message to indicate rollout is missing a field
	MissingFieldMessage = "Rollout has missing field '%s'"
	// SameServicesMessage the message to indicate that the rollout uses the same service for the active and preview services
	SameServicesMessage = "This rollout uses the same service for the active and preview services, but two different services are required."
	// SelectAllMessage the message to indicate that the rollout has an empty selector
	SelectAllMessage = "This rollout is selecting all pods. A non-empty selector is required."
	// MissingStrategyTypeReason the reason to indicate that the rollout is missing the strategy field
	MissingStrategyTypeReason = "MissingStrategy"
	// MissingSelectorReason the reason to indicate that the rollout is missing the selector field
	MissingSelectorReason = "MissingSelector"
	// MissingBlueGreenStrategyReason the reason to indicate that the rollout is missing the .spec.strategy.blueGreen field
	MissingBlueGreenStrategyReason = "MissingBlueGreen"
	// MissingActiveServiceReason the reason to indicate that the rollout is missing the active service field
	MissingActiveServiceReason = "MissingActive"
	// InvalidSelectorReason the reason to indicate the selector is selecting all the pods
	InvalidSelectorReason = "InvalidSelector"
	// SameServicesReason the reason to indicate that the rollout uses the same service for the active and preview services
	SameServicesReason = "SameService"
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

// RolloutComplete considers a rollout to be complete once all of its desired replicas
// are updated, available, and receiving traffic from the active service, and no old pods are running.
func RolloutComplete(rollout *v1alpha1.Rollout, newStatus *v1alpha1.RolloutStatus) bool {
	replicas := defaults.GetRolloutReplicasOrDefault(rollout)
	podHash := controller.ComputeHash(&rollout.Spec.Template, rollout.Status.CollisionCount)
	return newStatus.UpdatedReplicas == replicas &&
		newStatus.Replicas == replicas &&
		newStatus.AvailableReplicas == replicas &&
		newStatus.ActiveSelector == podHash &&
		newStatus.ObservedGeneration == ComputeGenerationHash(rollout.Spec)
}

// ComputeGenerationHash returns a hash value calculated from the Rollout Spec. The hash will
// be safe encoded to avoid bad words.
func ComputeGenerationHash(spec v1alpha1.RolloutSpec) string {
	rolloutSpecHasher := fnv.New32a()
	hashutil.DeepHashObject(rolloutSpecHasher, spec)
	return rand.SafeEncodeString(fmt.Sprint(rolloutSpecHasher.Sum32()))
}

// used for ing
var nowFn = func() time.Time { return time.Now() }

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
		return newInvalidSpecRolloutCondition(prevCond, MissingSelectorReason, message)
	}

	everything := metav1.LabelSelector{}
	if reflect.DeepEqual(rollout.Spec.Selector, &everything) {
		return newInvalidSpecRolloutCondition(prevCond, InvalidSelectorReason, SelectAllMessage)
	}

	switch rollout.Spec.Strategy.Type {
	case v1alpha1.BlueGreenRolloutStrategyType:
		if rollout.Spec.Strategy.BlueGreenStrategy == nil {
			message := fmt.Sprintf(MissingFieldMessage, ".Spec.Strategy.BlueGreenStrategy")
			return newInvalidSpecRolloutCondition(prevCond, MissingBlueGreenStrategyReason, message)
		}
		if rollout.Spec.Strategy.BlueGreenStrategy.ActiveService == "" {
			message := fmt.Sprintf(MissingFieldMessage, ".Spec.Strategy.BlueGreenStrategy.ActiveService")
			return newInvalidSpecRolloutCondition(prevCond, MissingActiveServiceReason, message)
		}
		if rollout.Spec.Strategy.BlueGreenStrategy.ActiveService == rollout.Spec.Strategy.BlueGreenStrategy.PreviewService {
			return newInvalidSpecRolloutCondition(prevCond, SameServicesReason, SameServicesMessage)
		}
	case "":
		message := fmt.Sprintf(MissingFieldMessage, ".Spec.Strategy.BlueGreenRolloutStrategyType")
		return newInvalidSpecRolloutCondition(prevCond, MissingStrategyTypeReason, message)
	}

	return nil
}

// HasRevisionHistoryLimit checks if the RevisionHistoryLimit field is set
func HasRevisionHistoryLimit(r *v1alpha1.Rollout) bool {
	return r.Spec.RevisionHistoryLimit != nil && *r.Spec.RevisionHistoryLimit != math.MaxInt32
}
