package rollout

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// isProgressiveMigrationComplete checks if the progressive scale down migration is complete,
// returning true if the rollout is healthy and the workload reference is set to scale down progressively.
func (c *rolloutContext) isProgressiveMigrationComplete() bool {
	if c.rollout.Spec.WorkloadRef == nil {
		return false
	}

	if c.rollout.Spec.WorkloadRef.ScaleDown != v1alpha1.ScaleDownProgressively {
		return false
	}

	if c.rollout.Status.Phase == v1alpha1.RolloutPhaseHealthy {
		return true
	}

	return false
}
