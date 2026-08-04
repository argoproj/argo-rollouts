package conditions

import (
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	experimentutil "github.com/argoproj/argo-rollouts/utils/experiment"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

const (
	// ExperimentProgressingMessage is added in a Experiment when one of its replica sets is updated as part
	// of the experiment process.
	ExperimentProgressingMessage = "Experiment %q is progressing."
	// ExperimentRunningMessage is added when a experiment has all the templates running
	ExperimentRunningMessage = "Experiment %q is running."
	// ExperimentCompletedMessage is added when the experiment is completed
	ExperimentCompletedMessage = "Experiment %q has successfully ran and completed."
	// ExperimentCompleteReason is added when the experiment is completed
	ExperimentCompleteReason = "ExperimentCompleted"
	// ExperimentTemplateNameRepeatedMessage message when name in spec.template is repeated
	ExperimentTemplateNameRepeatedMessage = "Experiment %s has repeated template name '%s' in templates"
	// ExperimentTemplateNameEmpty message when name in template is empty
	ExperimentTemplateNameEmpty = "Experiment %s has empty template name at index %d"
	// DurationLongerThanDeadlineMessage indicates the Duration is longer than ProgressDeadlineSeconds
	DurationLongerThanDeadlineMessage = "Duration cannot be longer than ProgressDeadlineSeconds"
	// ExperimentSelectAllMessage the message to indicate that the rollout has an empty selector
	ExperimentSelectAllMessage = "This experiment is selecting all pods at index %d. A non-empty selector is required."
	// ExperimentMinReadyLongerThanDeadlineMessage indicates the MinReadySeconds is longer than ProgressDeadlineSeconds
	ExperimentMinReadyLongerThanDeadlineMessage = "MinReadySeconds cannot be longer than ProgressDeadlineSeconds. Check template index %d"
)

// NewExperimentConditions takes arguments to create new Condition
func NewExperimentConditions(condType v1alpha1.ExperimentConditionType, status corev1.ConditionStatus, reason, message string) *v1alpha1.ExperimentCondition {
	return &v1alpha1.ExperimentCondition{
		Type:               condType,
		Status:             status,
		LastUpdateTime:     timeutil.MetaNow(),
		LastTransitionTime: timeutil.MetaNow(),
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

// ExperimentProgressing determines if the experiment has made any progress
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

// ExperimentRunning indicates when a experiment has become healthy and started to run for the `spec.duration` time
func ExperimentRunning(experiment *v1alpha1.Experiment) bool {
	passedDuration, _ := experimentutil.PassedDurations(experiment)
	return experiment.Status.AvailableAt != nil && !passedDuration
}

func newInvalidSpecExperimentCondition(prevCond *v1alpha1.ExperimentCondition, reason string, message string) *v1alpha1.ExperimentCondition {
	if prevCond != nil && prevCond.Message == message {
		prevCond.LastUpdateTime = timeutil.MetaNow()
		return prevCond
	}
	return NewExperimentConditions(v1alpha1.InvalidExperimentSpec, corev1.ConditionTrue, reason, message)
}

// VerifyExperimentSpec Checks for a valid spec otherwise returns a invalidSpec condition.
func VerifyExperimentSpec(experiment *v1alpha1.Experiment, prevCond *v1alpha1.ExperimentCondition) *v1alpha1.ExperimentCondition {
	templateNameSet := make(map[string]bool)
	for i := range experiment.Spec.Templates {
		template := experiment.Spec.Templates[i]
		if template.Selector == nil {
			missingFieldPath := fmt.Sprintf(".Spec.Templates[%d].Selector", i)
			message := fmt.Sprintf(MissingFieldMessage, missingFieldPath)
			return newInvalidSpecExperimentCondition(prevCond, InvalidSpecReason, message)
		}

		everything := metav1.LabelSelector{}
		if reflect.DeepEqual(template.Selector, &everything) {
			message := fmt.Sprintf(ExperimentSelectAllMessage, i)
			return newInvalidSpecExperimentCondition(prevCond, InvalidSpecReason, message)
		}

		if template.MinReadySeconds > defaults.GetExperimentProgressDeadlineSecondsOrDefault(experiment) {
			message := fmt.Sprintf(ExperimentMinReadyLongerThanDeadlineMessage, i)
			return newInvalidSpecExperimentCondition(prevCond, InvalidSpecReason, message)
		}

		if template.Name == "" {
			message := fmt.Sprintf(ExperimentTemplateNameEmpty, experiment.Name, i)
			return newInvalidSpecExperimentCondition(prevCond, InvalidSpecReason, message)
		}
		if ok := templateNameSet[template.Name]; ok {
			message := fmt.Sprintf(ExperimentTemplateNameRepeatedMessage, experiment.Name, template.Name)
			return newInvalidSpecExperimentCondition(prevCond, InvalidSpecReason, message)
		}
		templateNameSet[template.Name] = true
	}
	return nil
}
