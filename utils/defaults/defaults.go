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
	DefaultMaxSurge = 1
	// DefaultMaxUnavailable default number for the max number of unavailable pods during a rollout
	DefaultMaxUnavailable = 1
)

// GetRolloutReplicasOrDefault returns the specified number of replicas in a rollout or the default number
func GetRolloutReplicasOrDefault(rollout *v1alpha1.Rollout) int32 {
	if rollout.Spec.Replicas == nil {
		return DefaultReplicas
	}
	return *rollout.Spec.Replicas
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
	defaultValue := intstr.FromInt(DefaultMaxSurge)
	return &defaultValue
}

func GetMaxUnavailableOrDefault(rollout *v1alpha1.Rollout) *intstr.IntOrString {
	if rollout.Spec.Strategy.CanaryStrategy != nil && rollout.Spec.Strategy.CanaryStrategy.MaxUnavailable != nil {
		return rollout.Spec.Strategy.CanaryStrategy.MaxUnavailable
	}
	defaultValue := intstr.FromInt(DefaultMaxUnavailable)
	return &defaultValue
}
