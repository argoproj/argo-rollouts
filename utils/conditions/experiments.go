package conditions

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	experimentutil "github.com/argoproj/argo-rollouts/utils/experiment"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

const (
	// ExperimentProgressingMessage is added in a Experiment when one of its replica sets is updated as part
	// of the experiment process.
	ExperimentProgressingMessage = "Experiment %q is progressing."
	// ExperimentTimeOutMessage is added in a experiment when the experiment fails to show any progress
	// within the given deadline (progressDeadlineSeconds).
	ExperimentTimeOutMessage = "Experiment %q has timed out progressing."
	// ExperimentRunningMessage is added when a experiment has all the templates running
	ExperimentRunningMessage = "Experiment %q is running."
	// ExperimentCompletedMessage is added when the experiment is completed
	ExperimentCompletedMessage = "Experiment %q has successfully ran and completed."
	// ExperimentCompleteReason is added when the experiment is completed
	ExperimentCompleteReason = "ExperimentCompleted"
)

// NewExperimentConditions takes arguments to create new Condition
func NewExperimentConditions(condType v1alpha1.ExperimentConditionType, status corev1.ConditionStatus, reason, message string) *v1alpha1.ExperimentCondition {
	return &v1alpha1.ExperimentCondition{
		Type:               condType,
		Status:             status,
		LastUpdateTime:     metav1.Now(),
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
}

// GetExperimentCondition returns the condition with the provided type.
func GetExperimentCondition(status v1alpha1.ExperimentStatus, condType v1alpha1.ExperimentConditionType) *v1alpha1.ExperimentCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

// SetExperimentCondition updates the experiment to include the provided condition. If the condition that
// we are about to add already exists and has the same status and reason then we are not going to update.
func SetExperimentCondition(status *v1alpha1.ExperimentStatus, condition v1alpha1.ExperimentCondition) {
	currentCond := GetExperimentCondition(*status, condition.Type)
	if currentCond != nil && currentCond.Status == condition.Status && currentCond.Reason == condition.Reason {
		return
	}
	// Do not update lastTransitionTime if the status of the condition doesn't change.
	if currentCond != nil && currentCond.Status == condition.Status {
		condition.LastTransitionTime = currentCond.LastTransitionTime
	}
	newConditions := filterOutExperimentCondition(status.Conditions, condition.Type)
	status.Conditions = append(newConditions, condition)
}

// filterOutExperimentCondition returns a new slice of experiment conditions without conditions with the provided type.
func filterOutExperimentCondition(conditions []v1alpha1.ExperimentCondition, condType v1alpha1.ExperimentConditionType) []v1alpha1.ExperimentCondition {
	var newConditions []v1alpha1.ExperimentCondition
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}

// RemoveExperimentCondition removes the experiment condition with the provided type.
func RemoveExperimentCondition(status *v1alpha1.ExperimentStatus, condType v1alpha1.ExperimentConditionType) {
	status.Conditions = filterOutExperimentCondition(status.Conditions, condType)
}

//ExperimentProgressing determines if the experiment has made any progress
func ExperimentProgressing(experiment *v1alpha1.Experiment, newStatus v1alpha1.ExperimentStatus) bool {
	oldStatusMap := experimentutil.GetTemplateStatusMapping(experiment.Status)
	newStatusMap := experimentutil.GetTemplateStatusMapping(newStatus)
	if len(oldStatusMap) < len(newStatusMap) {
		return true
	}
	for i := range oldStatusMap {
		old := oldStatusMap[i]
		new, ok := newStatusMap[i]
		if !ok {
			continue
		}
		if old.Replicas != new.Replicas {
			return true
		}
		if old.UpdatedReplicas != new.UpdatedReplicas {
			return true
		}
		if old.ReadyReplicas != new.ReadyReplicas {
			return true
		}
		if old.AvailableReplicas != new.AvailableReplicas {
			return true
		}
	}

	return false
}

// ExperimentTimeOut indicates when the experiment has pasted the progress deadline limit
func ExperimentTimeOut(experiment *v1alpha1.Experiment, newStatus v1alpha1.ExperimentStatus) bool {
	if experiment.Status.AvailableAt != nil {
		return false
	}
	condition := GetExperimentCondition(newStatus, v1alpha1.ExperimentProgressing)
	if condition == nil {
		return false
	}

	if condition.Reason == TimedOutReason {
		return true
	}

	// Look at the difference in seconds between now and the last time we reported any
	// progress or tried to create a replica set, or resumed a paused experiment and
	// compare against progressDeadlineSeconds.
	from := condition.LastUpdateTime
	now := time.Now()

	progressDeadlineSeconds := defaults.GetExperimentProgressDeadlineSecondsOrDefault(experiment)
	delta := time.Duration(progressDeadlineSeconds) * time.Second
	timedOut := from.Add(delta).Before(now)
	logCtx := logutil.WithExperiment(experiment)

	logCtx.Infof("Timed out (%t) [last progress check: %v - now: %v]", timedOut, from, now)
	return timedOut
}

// ExperimentCompleted Indicates when the experiment has finished and completely scaled down
func ExperimentCompleted(newStatus v1alpha1.ExperimentStatus) bool {
	if newStatus.Running != nil && *newStatus.Running {
		return false
	}
	new := experimentutil.GetTemplateStatusMapping(newStatus)
	for i := range new {
		status := new[i]
		if status.Replicas != int32(0) {
			return false
		}
		if status.UpdatedReplicas != int32(0) {
			return false
		}
		if status.AvailableReplicas != int32(0) {
			return false
		}
		if status.ReadyReplicas != int32(0) {
			return false
		}
	}
	return true
}

// ExperimentRunning indicates when a experiment has become healthy and started to run for the `spec.duration` time
func ExperimentRunning(experiment *v1alpha1.Experiment) bool {
	passedDuration, _ := experimentutil.PassedDurations(experiment)
	return experiment.Status.AvailableAt != nil && !passedDuration
}
