package experiment

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
)

func HasStarted(experiment *v1alpha1.Experiment) bool {
	return experiment.Status.Running != nil
}

func HasFinished(experiment *v1alpha1.Experiment) bool {
	return experiment.Status.Running != nil && !*experiment.Status.Running
}

// PassedDurations indicates if the experiment has run longer than the duration
func PassedDurations(experiment *v1alpha1.Experiment) (bool, time.Duration) {
	if experiment.Spec.Duration == nil {
		return false, 0
	}
	if experiment.Status.AvailableAt == nil {
		return false, 0
	}
	now := metav1.Now()
	expiredTime := experiment.Status.AvailableAt.Add(time.Duration(*experiment.Spec.Duration) * time.Second)
	return now.After(expiredTime), expiredTime.Sub(now.Time)
}

func CalculateTemplateReplicasCount(experiment *v1alpha1.Experiment, template v1alpha1.TemplateSpec) int32 {
	if HasFinished(experiment) {
		return int32(0)
	}
	return defaults.GetExperimentTemplateReplicasOrDefault(template)
}

// GetTemplateStatusMapping returns a mapping of name to template statuses
func GetTemplateStatusMapping(status v1alpha1.ExperimentStatus) map[string]v1alpha1.TemplateStatus {
	mapping := make(map[string]v1alpha1.TemplateStatus, len(status.TemplateStatuses))
	for i := range status.TemplateStatuses {
		template := status.TemplateStatuses[i]
		mapping[template.Name] = template
	}
	return mapping
}

func GetCollisionCountForTemplate(experiment *v1alpha1.Experiment, template v1alpha1.TemplateSpec) *int32 {
	templateStatuses := GetTemplateStatusMapping(experiment.Status)
	templateStatus := templateStatuses[template.Name]
	var collisionCount *int32
	if templateStatus.CollisionCount != nil {
		collisionCount = templateStatus.CollisionCount
	}
	return collisionCount
}

// ExperimentGeneratedNameFromRollout gets the name of the experiment based on the rollout
func ExperimentGeneratedNameFromRollout(rollout *v1alpha1.Rollout) string {
	currentStep := int32(0)
	if rollout.Status.CurrentStepIndex != nil {
		currentStep = *rollout.Status.CurrentStepIndex
	}
	podTemplateSpecHash := controller.ComputeHash(&rollout.Spec.Template, rollout.Status.CollisionCount)
	return fmt.Sprintf("%s-%s-%d-", rollout.Name, podTemplateSpecHash, currentStep)
}

// ReplicasetNameFromExperiment gets the replicaset name based off of the experiment and the template
func ReplicasetNameFromExperiment(experiment *v1alpha1.Experiment, template v1alpha1.TemplateSpec) string {
	collisionCount := GetCollisionCountForTemplate(experiment, template)
	podTemplateSpecHash := controller.ComputeHash(&template.Template, collisionCount)
	return fmt.Sprintf("%s-%s-%s", experiment.Name, template.Name, podTemplateSpecHash)
}

// ExperimentByCreationTimestamp sorts a list of experiment by creation timestamp (earliest to latest), using their name as a tie breaker.
type ExperimentByCreationTimestamp []*v1alpha1.Experiment

func (o ExperimentByCreationTimestamp) Len() int      { return len(o) }
func (o ExperimentByCreationTimestamp) Swap(i, j int) { o[i], o[j] = o[j], o[i] }
func (o ExperimentByCreationTimestamp) Less(i, j int) bool {
	if o[i].CreationTimestamp.Equal(&o[j].CreationTimestamp) {
		return o[i].Name < o[j].Name
	}
	return o[i].CreationTimestamp.Before(&o[j].CreationTimestamp)
}
