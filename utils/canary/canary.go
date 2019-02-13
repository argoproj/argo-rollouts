package canary

import (
	"math"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/util/integer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

func CalculateReplicaCountsForCanary(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, stableRS *appsv1.ReplicaSet, oldRSs []*appsv1.ReplicaSet) (int32, int32, error) {
	rolloutSpecReplica := defaults.GetRolloutReplicasOrDefault(rollout)
	setWeight := GetCurrentSetWeight(rollout)

	maxSurge := replicasetutil.MaxSurge(rollout)

	if extraReplicaAdded(rolloutSpecReplica, setWeight) {
		// In the case where the weight of the stable and canary replica counts cannot be divided evenly,
		// the controller needs to surges by one to account for both replica counts being rounded up.
		maxSurge = maxSurge + 1
	}
	maxReplicaCountAllowed := rolloutSpecReplica + maxSurge

	allRSs := append(oldRSs, newRS)
	if checkStableRSExists(newRS, stableRS) {
		allRSs = append(allRSs, stableRS)
	}

	totalCurrentReplicaCount := replicasetutil.GetReplicaCountForReplicaSets(allRSs)
	scaleUpCount := maxReplicaCountAllowed - totalCurrentReplicaCount

	desiredStableRSReplicaCount := int32(math.Ceil(float64(rolloutSpecReplica) * (1 - (float64(setWeight) / 100))))
	desiredNewRSReplicaCount := int32(math.Ceil(float64(rolloutSpecReplica) * (float64(setWeight) / 100)))

	stableRSReplicaCount := int32(0)
	newRSReplicaCount := *newRS.Spec.Replicas
	if checkStableRSExists(newRS, stableRS) {
		stableRSReplicaCount = *stableRS.Spec.Replicas
	} else {
		// If there is no stableRS or it is the same as the newRS, then the rollout does not follow the canary steps.
		// Instead the controller tries to get the newRS to 100% traffic.
		desiredNewRSReplicaCount = rolloutSpecReplica
		desiredStableRSReplicaCount = 0
	}

	if checkStableRSExists(newRS, stableRS) && *stableRS.Spec.Replicas < desiredStableRSReplicaCount {
		if scaleUpCount > 0 {
			stableRSReplicaCount = integer.Int32Min(*stableRS.Spec.Replicas+scaleUpCount, desiredStableRSReplicaCount)
			scaleUpCount = integer.Int32Max(0, scaleUpCount-(desiredStableRSReplicaCount-*stableRS.Spec.Replicas))
		}
	}

	if *newRS.Spec.Replicas < desiredNewRSReplicaCount {
		if scaleUpCount > 0 {
			newRSReplicaCount = integer.Int32Min(*newRS.Spec.Replicas+scaleUpCount, desiredNewRSReplicaCount)
		}
	}

	minAvailableReplicaCount := rolloutSpecReplica - replicasetutil.MaxUnavailable(rollout)
	totalCurrentAvailableReplicaCount := replicasetutil.GetAvailableReplicaCountForReplicaSets(allRSs)
	scaleDownCount := totalCurrentAvailableReplicaCount - minAvailableReplicaCount
	if scaleDownCount <= 0 {
		// Cannot scale down stableRS or newRS without going below min available replica count
		return newRSReplicaCount, stableRSReplicaCount, nil
	}

	totalAvailableOlderReplicaCount := replicasetutil.GetAvailableReplicaCountForReplicaSets(oldRSs)

	if scaleDownCount <= totalAvailableOlderReplicaCount {
		//Need to scale down older replicas before scaling down the newRS or stableRS.
		return newRSReplicaCount, stableRSReplicaCount, nil
	}
	scaleDownCount = scaleDownCount - totalAvailableOlderReplicaCount

	if *newRS.Spec.Replicas > desiredNewRSReplicaCount {
		if scaleDownCount > 0 {
			newRSReplicaCount = integer.Int32Max(*newRS.Spec.Replicas-scaleDownCount, desiredNewRSReplicaCount)
			scaleDownCount = integer.Int32Max(0, scaleDownCount-(desiredNewRSReplicaCount-*newRS.Spec.Replicas))
		}
	}

	if checkStableRSExists(newRS, stableRS) && *stableRS.Spec.Replicas > desiredStableRSReplicaCount {
		if scaleDownCount > 0 {
			stableRSReplicaCount = integer.Int32Max(*stableRS.Spec.Replicas-scaleDownCount, desiredStableRSReplicaCount)
		}
	}

	return newRSReplicaCount, stableRSReplicaCount, nil
}

// checkStableRS checks if the stableRS exists and is different than the newRS
func checkStableRSExists(newRS, stableRS *appsv1.ReplicaSet) bool {
	if stableRS == nil {
		return false
	}
	if newRS.Name == stableRS.Name {
		return false
	}
	return true
}

// extraReplicaAdded checks if an extra replica is added because the stable and canary replicas count are both
// rounded up. The controller rounds both of the replica counts when the setWeight does not distribute evenly
// in order to prevent either from having a 0 replica count.
func extraReplicaAdded(replicas int32, setWeight int32) bool {
	_, frac := math.Modf(float64(replicas) * (float64(setWeight) / 100))
	return frac != 0.0
}

// GetCurrentCanaryStep returns the current canary step. If there are no steps or the rollout
// has already executed the last step, the func returns nil
func GetCurrentCanaryStep(rollout *v1alpha1.Rollout) (*v1alpha1.CanaryStep, int32) {
	if len(rollout.Spec.Strategy.CanaryStrategy.Steps) == 0 {
		return nil, 0
	}
	currentStepIndex := int32(0)
	if rollout.Status.CurrentStepIndex != nil {
		currentStepIndex = *rollout.Status.CurrentStepIndex
	}
	if len(rollout.Spec.Strategy.CanaryStrategy.Steps) <= int(currentStepIndex) {
		return nil, currentStepIndex
	}
	return &rollout.Spec.Strategy.CanaryStrategy.Steps[currentStepIndex], currentStepIndex
}

func GetCurrentSetWeight(rollout *v1alpha1.Rollout) int32 {
	currentStep, currentStepIndex := GetCurrentCanaryStep(rollout)
	if currentStep == nil {
		return 100
	}

	for i := currentStepIndex; i >= 0; i-- {
		step := rollout.Spec.Strategy.CanaryStrategy.Steps[i]
		if step.SetWeight != nil {
			return *step.SetWeight
		}
	}
	return 100
}
