package defaults

import (
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const (
	// DefaultReplicas default number of replicas for a rollout if the .Spec.Replicas is nil
	DefaultReplicas = int32(1)
	// DefaultRevisionHistoryLimit default number of revisions to keep if .Spec.RevisionHistoryLimit is nil
	DefaultRevisionHistoryLimit = int32(10)
	// DefaultMaxSurge default number for the max number of additional pods that can be brought up during a rollout
	DefaultMaxSurge = "25"
	// DefaultMaxUnavailable default number for the max number of unavailable pods during a rollout
	DefaultMaxUnavailable = 0
	// DefaultProgressDeadlineSeconds default number of seconds for the rollout to be making progress
	DefaultProgressDeadlineSeconds = int32(600)
	// DefaultScaleDownDelaySeconds default seconds before scaling down old replicaset after switching services
	DefaultScaleDownDelaySeconds = int32(30)
	// DefaultAutoPromotionEnabled default value for auto promoting a blueGreen strategy
	DefaultAutoPromotionEnabled = true
)

// GetRolloutReplicasOrDefault returns the specified number of replicas in a rollout or the default number
func GetRolloutReplicasOrDefault(rollout *v1alpha1.Rollout) int32 {
	if rollout.Spec.Replicas == nil {
		return DefaultReplicas
	}
	return *rollout.Spec.Replicas
}

// GetExperimentTemplateReplicasOrDefault returns the specified number of replicas in a Experiment template or the default number
func GetExperimentTemplateReplicasOrDefault(template v1alpha1.TemplateSpec) int32 {
	if template.Replicas == nil {
		return DefaultReplicas
	}
	return *template.Replicas
}

// GetRevisionHistoryLimitOrDefault returns the specified number of replicas in a rollout or the default number
func GetRevisionHistoryLimitOrDefault(rollout *v1alpha1.Rollout) int32 {
	if rollout.Spec.RevisionHistoryLimit == nil {
		return DefaultRevisionHistoryLimit
	}
	return *rollout.Spec.RevisionHistoryLimit
}

func GetMaxSurgeOrDefault(rollout *v1alpha1.Rollout) *intstr.IntOrString {
	if rollout.Spec.Strategy.CanaryStrategy != nil && rollout.Spec.Strategy.CanaryStrategy.MaxSurge != nil {
		return rollout.Spec.Strategy.CanaryStrategy.MaxSurge
	}
	defaultValue := intstr.FromString(DefaultMaxSurge)
	return &defaultValue
}

func GetMaxUnavailableOrDefault(rollout *v1alpha1.Rollout) *intstr.IntOrString {
	if rollout.Spec.Strategy.CanaryStrategy != nil && rollout.Spec.Strategy.CanaryStrategy.MaxUnavailable != nil {
		return rollout.Spec.Strategy.CanaryStrategy.MaxUnavailable
	}
	defaultValue := intstr.FromInt(DefaultMaxUnavailable)
	return &defaultValue
}

func GetStrategyType(rollout *v1alpha1.Rollout) string {
	if rollout.Spec.Strategy.BlueGreenStrategy != nil {
		return "blueGreen"
	}
	if rollout.Spec.Strategy.CanaryStrategy != nil {
		return "canary"
	}
	return "No Strategy listed"
}

func GetProgressDeadlineSecondsOrDefault(rollout *v1alpha1.Rollout) int32 {
	if rollout.Spec.ProgressDeadlineSeconds != nil {
		return *rollout.Spec.ProgressDeadlineSeconds
	}
	return DefaultProgressDeadlineSeconds
}

func GetExperimentProgressDeadlineSecondsOrDefault(e *v1alpha1.Experiment) int32 {
	if e.Spec.ProgressDeadlineSeconds != nil {
		return *e.Spec.ProgressDeadlineSeconds
	}
	return DefaultProgressDeadlineSeconds
}

func GetScaleDownDelaySecondsOrDefault(rollout *v1alpha1.Rollout) int32 {
	if rollout.Spec.Strategy.BlueGreenStrategy == nil {
		return DefaultScaleDownDelaySeconds
	}

	if rollout.Spec.Strategy.BlueGreenStrategy.ScaleDownDelaySeconds == nil {
		return DefaultScaleDownDelaySeconds
	}

	return *rollout.Spec.Strategy.BlueGreenStrategy.ScaleDownDelaySeconds
}

func GetAutoPromotionEnabledOrDefault(rollout *v1alpha1.Rollout) bool {
	if rollout.Spec.Strategy.BlueGreenStrategy == nil {
		return DefaultAutoPromotionEnabled
	}
	if rollout.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled == nil {
		return DefaultAutoPromotionEnabled
	}
	return *rollout.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled
}