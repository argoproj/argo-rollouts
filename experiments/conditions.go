package experiments

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
)

func calculateExperimentConditions(experiment *v1alpha1.Experiment, newStatus v1alpha1.ExperimentStatus) *v1alpha1.ExperimentStatus {

	prevCond := conditions.GetExperimentCondition(experiment.Status, v1alpha1.InvalidExperimentSpec)
	invalidSpecCond := conditions.VerifyExperimentSpec(experiment, prevCond)
	if prevCond != nil && invalidSpecCond == nil {
		conditions.RemoveExperimentCondition(&newStatus, v1alpha1.InvalidExperimentSpec)
	}

	switch {
	case newStatus.Phase.Completed() && newStatus.Phase == v1alpha1.AnalysisPhaseSuccessful:
		msg := fmt.Sprintf(conditions.ExperimentCompletedMessage, experiment.Name)
		condition := conditions.NewExperimentConditions(v1alpha1.ExperimentProgressing, corev1.ConditionFalse, conditions.ExperimentCompleteReason, msg)
		conditions.SetExperimentCondition(&newStatus, *condition)
	case conditions.ExperimentProgressing(experiment, newStatus):
		currentCond := conditions.GetExperimentCondition(experiment.Status, v1alpha1.ExperimentProgressing)
		// If there is any progress made, continue by not checking if the experiment failed. This
		// behavior emulates the rolling updater progressDeadline check.
		msg := fmt.Sprintf(conditions.ExperimentProgressingMessage, experiment.Name)
		condition := conditions.NewExperimentConditions(v1alpha1.ExperimentProgressing, corev1.ConditionTrue, conditions.ReplicaSetUpdatedReason, msg)
		// Update the current Progressing condition or add a new one if it doesn't exist.
		// If a Progressing condition with status=true already exists, we should update
		// everything but lastTransitionTime. SetExperimentCondition already does that but
		// it also is not updating conditions when the reason of the new condition is the
		// same as the old. The Progressing condition is a special case because we want to
		// update with the same reason and change just lastUpdateTime iff we notice any
		// progress. That's why we handle it here.
		if currentCond != nil {
			if currentCond.Status == corev1.ConditionTrue {
				condition.LastTransitionTime = currentCond.LastTransitionTime
			}
			conditions.RemoveExperimentCondition(&newStatus, v1alpha1.ExperimentProgressing)
		}
		conditions.SetExperimentCondition(&newStatus, *condition)
	case conditions.ExperimentRunning(experiment):
		// Update the experiment conditions with a message for the new replica sets that
		// was successfully deployed and is running for the timed duration from the
		// `spec.duration` field. If the condition already exists, we ignore this update.
		msg := fmt.Sprintf(conditions.ExperimentRunningMessage, experiment.Name)
		condition := conditions.NewExperimentConditions(v1alpha1.ExperimentProgressing, corev1.ConditionTrue, conditions.NewRSAvailableReason, msg)
		conditions.SetExperimentCondition(&newStatus, *condition)
	}
	return &newStatus
}
