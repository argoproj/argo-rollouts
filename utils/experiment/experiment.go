package experiment

import (
	"fmt"

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

func CalculateTemplateReplicasCount(experiment *v1alpha1.Experiment, template v1alpha1.TemplateSpec) int32 {
	if HasFinished(experiment) {
		return int32(0)
	}
	return defaults.GetExperimentTemplateReplicasOrDefault(template)
}

func GetTemplateStatus(experiment *v1alpha1.Experiment, template v1alpha1.TemplateSpec) (*v1alpha1.TemplateStatus, *int) {
	for i := range experiment.Status.TemplateStatuses {
		status := experiment.Status.TemplateStatuses[i]
		if status.Name == template.Name {
			return &status, &i
		}
	}
	return nil, nil
}

func GetCollisionCountForTemplate(experiment *v1alpha1.Experiment, template v1alpha1.TemplateSpec) *int32 {
	templateStatus, _ := GetTemplateStatus(experiment, template)
	var collisionCount *int32
	if templateStatus != nil && templateStatus.CollisionCount != nil {
		collisionCount = templateStatus.CollisionCount
	}
	return collisionCount
}

func ReplicasetNameFromExperiment(experiment *v1alpha1.Experiment, template v1alpha1.TemplateSpec) string {
	collisionCount := GetCollisionCountForTemplate(experiment, template)
	podTemplateSpecHash := controller.ComputeHash(&template.Template, collisionCount)
	return fmt.Sprintf("%s-%s-%s", experiment.Name, template.Name, podTemplateSpecHash)
}