package experiment

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
)

func HasStarted(experiment *v1alpha1.Experiment) bool {
	return experiment.Status.Running != nil
}

func HasFinished(experiment *v1alpha1.Experiment) bool {
	return experiment.Status.Running != nil && !*experiment.Status.Running
}

func PassedDurations(experiment *v1alpha1.Experiment) bool {
	if experiment.Spec.Duration == nil {
		return false
	}
	if experiment.Status.AvailableAt == nil {
		return false
	}
	now := metav1.Now()
	expiredTime := experiment.Status.AvailableAt.Add(time.Duration(*experiment.Spec.Duration) * time.Second)
	return now.After(expiredTime)
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