package replicaset

import (
	"math"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/util/integer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
)

// AtDesiredReplicaCountsForCanary indicates if the rollout is at the desired state for the current step
func AtDesiredReplicaCountsForCanary(rollout *v1alpha1.Rollout, newRS, stableRS *appsv1.ReplicaSet, olderRSs []*appsv1.ReplicaSet) bool {
	desiredNewRSReplicaCount, desiredStableRSReplicaCount := DesiredReplicaCountsForCanary(rollout, newRS, stableRS)
	if newRS == nil || desiredNewRSReplicaCount != *newRS.Spec.Replicas || desiredNewRSReplicaCount != newRS.Status.AvailableReplicas {
		return false
	}
	if stableRS == nil || desiredStableRSReplicaCount != *stableRS.Spec.Replicas || desiredStableRSReplicaCount != stableRS.Status.AvailableReplicas {
		return false
	}
	if GetAvailableReplicaCountForReplicaSets(olderRSs) != int32(0) {
		return false
	}
	return true
}

//DesiredReplicaCountsForCanary calculates the desired endstate replica count for the new and stable replicasets
func DesiredReplicaCountsForCanary(rollout *v1alpha1.Rollout, newRS, stableRS *appsv1.ReplicaSet) (int32, int32) {
	rolloutSpecReplica := defaults.GetRolloutReplicasOrDefault(rollout)
	setWeight := GetCurrentSetWeight(rollout)

	desiredStableRSReplicaCount := int32(math.Ceil(float64(rolloutSpecReplica) * (1 - (float64(setWeight) / 100))))
	desiredNewRSReplicaCount := int32(math.Ceil(float64(rolloutSpecReplica) * (float64(setWeight) / 100)))
	if !CheckStableRSExists(newRS, stableRS) {
		// If there is no stableRS or it is the same as the newRS, then the rollout does not follow the canary steps.
		// Instead the controller tries to get the newRS to 100% traffic.
		desiredNewRSReplicaCount = rolloutSpecReplica
		desiredStableRSReplicaCount = 0
	}
	return desiredNewRSReplicaCount, desiredStableRSReplicaCount

}

// CalculateReplicaCountsForCanary calculates the number of replicas for the newRS and the stableRS.  The function
// calculates the desired number of replicas for the new and stable RS using the following equations:
//
// newRS Replica count = spec.Replica * (setweight / 100)
// stableRS Replica count = spec.Replica * ( (1 - setweight) / 100)
//
// In both equations, the function rounds the desired replica count up if the math does not divide into whole numbers
// because the rollout guarantees at least one replica for both the stable and new RS when the setWeight is not 0 or 100.
// Then, the function finds the number of replicas it can scale up using the following equation:
//
// scaleUpCount := (maxSurge + rollout.Spec.Replica) - sum of rollout's RSs spec.Replica
//
// If the rollout has not reached its max number of replicas, it will scale up the RS whose desired replica
// count is greater than its current count to the desired number. The rollout will either scale the RS up as much as it
// can unless the rollout can reach the RS desired count. In order to give precenence to the stableRS, the function will
// scale up the stable RS to desired count before scaling up the new RS.
//
// At this point, the function then finds the number of replicas it can scale down using the following equation:
//
// scaleDownCount := count of all the available replicas - (spec.Replica - maxUnavailable)
//
// If the rollout has not reached at the min available replicas count, it will scale down the RS whose desired replica
// count is less than its current count to the desired number. However before scaling any new or stable RS down, the
// function will scale down the replicas in the old RS list first.  Afterwards if the rollout is not at the min available
// replica count, the function will check the newRS before the stableRS.
//
// Examples:
// replicas 10 currentWeight 10 NewRS 0 stableRS 10 max unavailable 1, surge 1 - should return newRS 1 stableRS 9
// replicas 10 currentWeight 30 NewRS 0 stableRS 10 max unavailable 0, surge 3 - should return newRS 3 stableRS 10
// replicas 10 currentWeight 30 NewRS 0 stableRS 10 max unavailable 5, surge 0 - should return newRS 0 stableRS 7
// replicas 10 currentWeight 5 NewRS 0 stableRS 10 max unavailable 1, surge 1 - should return newRS 1 stableRS 9
// replicas 1 currentWeight 5 NewRS 0 stableRS 1 max unavailable 0, surge 1 - should return newRS 1 stableRS 1
// replicas 1 currentWeight 95 NewRS 0 stableRS 1 max unavailable 0, surge 1 - should return newRS 1 stableRS 1
// For more examples, check the TestCalculateReplicaCountsForCanary test in canary/canary_test.go
func CalculateReplicaCountsForCanary(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, stableRS *appsv1.ReplicaSet, oldRSs []*appsv1.ReplicaSet) (int32, int32) {
	rolloutSpecReplica := defaults.GetRolloutReplicasOrDefault(rollout)
	setWeight := GetCurrentSetWeight(rollout)

	desiredStableRSReplicaCount := int32(math.Ceil(float64(rolloutSpecReplica) * (1 - (float64(setWeight) / 100))))
	desiredNewRSReplicaCount := int32(math.Ceil(float64(rolloutSpecReplica) * (float64(setWeight) / 100)))

	stableRSReplicaCount := int32(0)
	newRSReplicaCount := *newRS.Spec.Replicas

	scaleStableRS := CheckStableRSExists(newRS, stableRS)
	if scaleStableRS {
		stableRSReplicaCount = *stableRS.Spec.Replicas
	} else {
		// If there is no stableRS or it is the same as the newRS, then the rollout does not follow the canary steps.
		// Instead the controller tries to get the newRS to 100% traffic.
		desiredNewRSReplicaCount = rolloutSpecReplica
		desiredStableRSReplicaCount = 0
	}

	maxSurge := MaxSurge(rollout)

	if extraReplicaAdded(rolloutSpecReplica, setWeight) {
		// In the case where the weight of the stable and canary replica counts cannot be divided evenly,
		// the controller needs to surges by one to account for both replica counts being rounded up.
		maxSurge = maxSurge + 1
	}
	maxReplicaCountAllowed := rolloutSpecReplica + maxSurge

	allRSs := append(oldRSs, newRS)
	if scaleStableRS {
		allRSs = append(allRSs, stableRS)
	}

	totalCurrentReplicaCount := GetReplicaCountForReplicaSets(allRSs)
	scaleUpCount := maxReplicaCountAllowed - totalCurrentReplicaCount

	if scaleStableRS && *stableRS.Spec.Replicas < desiredStableRSReplicaCount {
		if scaleUpCount > 0 {
			// The controller is scaling up the stableRS to reach the overall max surge or the desired replica
			// count for the stableRS. It picks the min to ensure that the max surge upper bound is upheld and it does
			// not scale the stableRS above the desired replica count.
			stableRSReplicaCount = integer.Int32Min(*stableRS.Spec.Replicas+scaleUpCount, desiredStableRSReplicaCount)
			// The ensures that the scaleUpCount is never less than 0
			scaleUpCount = integer.Int32Max(0, scaleUpCount-(desiredStableRSReplicaCount-*stableRS.Spec.Replicas))
		}
	}

	if *newRS.Spec.Replicas < desiredNewRSReplicaCount {
		if scaleUpCount > 0 {
			//This follows the same logic as scaling up the stableRS except with the newRS
			newRSReplicaCount = integer.Int32Min(*newRS.Spec.Replicas+scaleUpCount, desiredNewRSReplicaCount)
		}
	}

	minAvailableReplicaCount := rolloutSpecReplica - MaxUnavailable(rollout)
	totalCurrentAvailableReplicaCount := GetAvailableReplicaCountForReplicaSets(allRSs)
	scaleDownCount := totalCurrentAvailableReplicaCount - minAvailableReplicaCount
	if scaleDownCount <= 0 {
		// Cannot scale down stableRS or newRS without going below min available replica count
		return newRSReplicaCount, stableRSReplicaCount
	}

	totalAvailableOlderReplicaCount := GetAvailableReplicaCountForReplicaSets(oldRSs)

	if scaleDownCount <= totalAvailableOlderReplicaCount {
		//Need to scale down older replicas before scaling down the newRS or stableRS.
		return newRSReplicaCount, stableRSReplicaCount
	}
	scaleDownCount = scaleDownCount - totalAvailableOlderReplicaCount

	if *newRS.Spec.Replicas > desiredNewRSReplicaCount {
		if scaleDownCount > 0 {
			// The controller is scaling down the newRS to reach the overall min available count or the desired replica
			// count for the newRS. It picks the max to ensure that the min available count is upheld and it does not
			// scale the newRS below the desired replica count.
			newRSReplicaCount = integer.Int32Max(*newRS.Spec.Replicas-scaleDownCount, desiredNewRSReplicaCount)
			// The ensures that the scaleDownCount is never less than 0
			scaleDownCount = integer.Int32Max(0, scaleDownCount-(desiredNewRSReplicaCount-*newRS.Spec.Replicas))
		}
	}

	if scaleStableRS && *stableRS.Spec.Replicas > desiredStableRSReplicaCount {
		if scaleDownCount > 0 {
			//This follows the same logic as scaling down the newRS except with the stableRS
			stableRSReplicaCount = integer.Int32Max(*stableRS.Spec.Replicas-scaleDownCount, desiredStableRSReplicaCount)
		}
	}

	return newRSReplicaCount, stableRSReplicaCount
}

// CheckStableRSExists checks if the stableRS exists and is different than the newRS
func CheckStableRSExists(newRS, stableRS *appsv1.ReplicaSet) bool {
	if stableRS == nil {
		return false
	}
	if newRS == nil {
		return true
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

// GetCurrentSetWeight grabs the current setWeight used by the rollout by iterating backwards from the current step
// until it finds a setWeight step. The controller defaults to 100 if it iterates through all the steps with no
// setWeight or if there is no current step (i.e. the controller has already stepped through all the steps).
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

func GetStableRS(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, rslist []*appsv1.ReplicaSet) (*appsv1.ReplicaSet, []*appsv1.ReplicaSet) {
	if rollout.Status.CanaryStatus.StableRS == "" {
		return nil, rslist
	}
	olderRSs := []*appsv1.ReplicaSet{}
	var stableRS *appsv1.ReplicaSet
	for i := range rslist {
		rs := rslist[i]
		if rs != nil {
			if rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] == rollout.Status.CanaryStatus.StableRS {
				stableRS = rs
				continue
			}
			if newRS != nil && rs.Name == newRS.Name {
				continue
			}
			olderRSs = append(olderRSs, rs)
		}
	}
	return stableRS, olderRSs
}
