package replicaset

import (
	"encoding/json"
	"math"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
)

const (
	// EphemeralMetadataAnnotation denotes pod metadata which is ephemerally injected to canary/stable pods
	EphemeralMetadataAnnotation = "rollout.argoproj.io/ephemeral-metadata"
)

func allDesiredAreAvailable(rs *appsv1.ReplicaSet, desired int32) bool {
	return rs != nil && desired == *rs.Spec.Replicas && desired == rs.Status.AvailableReplicas
}

func allDesiredAreCreated(rs *appsv1.ReplicaSet, desired int32) bool {
	return rs != nil && desired == *rs.Spec.Replicas && desired == rs.Status.Replicas
}

func AtDesiredReplicaCountsForCanary(ro *v1alpha1.Rollout, newRS, stableRS *appsv1.ReplicaSet, olderRSs []*appsv1.ReplicaSet, weights *v1alpha1.TrafficWeights) bool {
	var desiredNewRSReplicaCount, desiredStableRSReplicaCount int32
	if ro.Spec.Strategy.Canary.TrafficRouting == nil {
		desiredNewRSReplicaCount, desiredStableRSReplicaCount = CalculateReplicaCountsForBasicCanary(ro, newRS, stableRS, olderRSs)
	} else {
		desiredNewRSReplicaCount, desiredStableRSReplicaCount = CalculateReplicaCountsForTrafficRoutedCanary(ro, weights)
	}
	if !allDesiredAreAvailable(newRS, desiredNewRSReplicaCount) {
		return false
	}
	if ro.Spec.Strategy.Canary.TrafficRouting == nil || !ro.Spec.Strategy.Canary.DynamicStableScale {
		if !allDesiredAreCreated(stableRS, desiredStableRSReplicaCount) {
			// only check stable RS if we are not using dynamic stable scaling
			return false
		}
	}
	if ro.Spec.Strategy.Canary.TrafficRouting == nil {
		// For basic canary, all older ReplicaSets must be scaled to zero since they serve traffic.
		// For traffic weighted canary, it's okay if they are still scaled up, since the traffic
		// router will prevent them from serving traffic
		if GetAvailableReplicaCountForReplicaSets(olderRSs) != int32(0) {
			return false
		}
	}
	return true
}

// CalculateReplicaCountsForBasicCanary calculates the number of replicas for the newRS and the stableRS
// when using the basic canary strategy. The function calculates the desired number of replicas for
// the new and stable RS using the following equations:
//
// newRS Replica count = spec.Replica * (setweight / 100)
// stableRS Replica count = spec.Replica * (1 - setweight / 100)
//
// In both equations, the function rounds the desired replica count up if the math does not divide into whole numbers
// because the rollout guarantees at least one replica for both the stable and new RS when the setWeight is not 0 or 100.
// Then, the function finds the number of replicas it can scale up using the following equation:
//
// scaleUpCount := (maxSurge + rollout.Spec.Replica) - sum of rollout's RSs spec.Replica
//
// If the rollout has not reached its max number of replicas, it will scale up the RS whose desired replica
// count is greater than its current count to the desired number. The rollout will either scale the RS up as much as it
// can unless the rollout can reach the RS desired count. In order to give precedence to the stableRS, the function will
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
// For more examples, check the CalculateReplicaCountsForBasicCanary test in canary/canary_test.go
func CalculateReplicaCountsForBasicCanary(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, stableRS *appsv1.ReplicaSet, oldRSs []*appsv1.ReplicaSet) (int32, int32) {
	rolloutSpecReplica := defaults.GetReplicasOrDefault(rollout.Spec.Replicas)
	_, desiredWeight := GetCanaryReplicasOrWeight(rollout)
	maxSurge := MaxSurge(rollout)

	desiredNewRSReplicaCount, desiredStableRSReplicaCount := approximateWeightedCanaryStableReplicaCounts(rolloutSpecReplica, desiredWeight, maxSurge)

	stableRSReplicaCount := int32(0)
	newRSReplicaCount := int32(0)
	if newRS != nil {
		newRSReplicaCount = *newRS.Spec.Replicas
	}

	scaleStableRS := CheckStableRSExists(newRS, stableRS)
	if scaleStableRS {
		stableRSReplicaCount = *stableRS.Spec.Replicas
	} else {
		// If there is no stableRS or it is the same as the newRS, then the rollout does not follow the canary steps.
		// Instead the controller tries to get the newRS to 100% traffic.
		desiredNewRSReplicaCount = rolloutSpecReplica
		desiredStableRSReplicaCount = 0
	}

	maxReplicaCountAllowed := rolloutSpecReplica + maxSurge

	allRSs := append(oldRSs, newRS)
	if scaleStableRS {
		allRSs = append(allRSs, stableRS)
	}

	totalCurrentReplicaCount := GetReplicaCountForReplicaSets(allRSs)
	scaleUpCount := maxReplicaCountAllowed - totalCurrentReplicaCount

	if scaleStableRS && *stableRS.Spec.Replicas < desiredStableRSReplicaCount && scaleUpCount > 0 {
		// if the controller doesn't have to use every replica to achieve the desired count, it only scales up to the
		// desired count.
		if *stableRS.Spec.Replicas+scaleUpCount < desiredStableRSReplicaCount {
			// The controller is using every replica it can to get closer to desired state.
			stableRSReplicaCount = *stableRS.Spec.Replicas + scaleUpCount
			scaleUpCount = 0
		} else {
			stableRSReplicaCount = desiredStableRSReplicaCount
			// Calculating how many replicas were used to scale up to the desired count
			scaleUpCount = scaleUpCount - (desiredStableRSReplicaCount - *stableRS.Spec.Replicas)
		}
	}

	if newRS != nil && *newRS.Spec.Replicas < desiredNewRSReplicaCount && scaleUpCount > 0 {
		// This follows the same logic as scaling up the stable except with the newRS and it does not need to
		// set the scaleDownCount again since it's not used again
		if *newRS.Spec.Replicas+scaleUpCount < desiredNewRSReplicaCount {
			newRSReplicaCount = *newRS.Spec.Replicas + scaleUpCount
		} else {
			newRSReplicaCount = desiredNewRSReplicaCount
		}
	}

	if GetReplicaCountForReplicaSets(oldRSs) > 0 {
		// If any older ReplicaSets exist, we should scale those down first, before even considering
		// scaling down the newRS or stableRS
		return newRSReplicaCount, stableRSReplicaCount
	}

	minAvailableReplicaCount := rolloutSpecReplica - MaxUnavailable(rollout)

	// isIncreasing indicates if we are supposed to be increasing our canary replica count.
	// If so, we can ignore pod availability of the stableRS. Otherwise, if we are reducing our
	// weight (e.g. we are aborting), then we can ignore pod availability of the canaryRS.
	isIncreasing := newRS == nil || desiredNewRSReplicaCount >= *newRS.Spec.Replicas
	replicasToScaleDown := GetReplicasForScaleDown(newRS, !isIncreasing) + GetReplicasForScaleDown(stableRS, isIncreasing)
	if replicasToScaleDown <= minAvailableReplicaCount {
		// Cannot scale down stableRS or newRS without going below min available replica count
		return newRSReplicaCount, stableRSReplicaCount
	}

	scaleDownCount := replicasToScaleDown - minAvailableReplicaCount
	if !isIncreasing {
		// Skip scalingDown Stable replicaSet when Canary availability is not taken into calculation for scaleDown
		newRSReplicaCount = calculateScaleDownReplicaCount(newRS, desiredNewRSReplicaCount, scaleDownCount, newRSReplicaCount)
		newRSReplicaCount, stableRSReplicaCount = adjustReplicaWithinLimits(newRS, stableRS, newRSReplicaCount, stableRSReplicaCount, maxReplicaCountAllowed, minAvailableReplicaCount)
	} else if scaleStableRS {
		// Skip scalingDown canary replicaSet when StableSet availability is not taken into calculation for scaleDown
		stableRSReplicaCount = calculateScaleDownReplicaCount(stableRS, desiredStableRSReplicaCount, scaleDownCount, stableRSReplicaCount)
		stableRSReplicaCount, newRSReplicaCount = adjustReplicaWithinLimits(stableRS, newRS, stableRSReplicaCount, newRSReplicaCount, maxReplicaCountAllowed, minAvailableReplicaCount)
	}
	return newRSReplicaCount, stableRSReplicaCount
}

// approximateWeightedCanaryStableReplicaCounts approximates the desired canary weight and returns
// the closest replica count values for the canary and stable to reach the desired weight. The
// canary/stable replica counts might sum to either spec.replicas or spec.replicas + 1 but will not
// exceed spec.replicas if maxSurge is 0. If the canary weight is between 1-99, and spec.replicas is > 1,
// we will always return a minimum of 1 for stable and canary as to not return 0.
func approximateWeightedCanaryStableReplicaCounts(specReplicas, desiredWeight, maxSurge int32) (int32, int32) {
	if specReplicas == 0 {
		return 0, 0
	}
	// canaryOption is one potential return value of this function. We will evaluate multiple options
	// for the canary count in order to best approximate the desired weight.
	type canaryOption struct {
		canary int32
		total  int32
	}
	var options []canaryOption

	ceilWeightedCanaryCount := int32(math.Ceil(float64(specReplicas*desiredWeight) / 100.0))
	floorWeightedCanaryCount := int32(math.Floor(float64(specReplicas*desiredWeight) / 100.0))

	tied := floorCeilingTied(desiredWeight, specReplicas)

	// zeroAllowed indicates if are allowed to return the floored value if it is zero. We don't allow
	// the value to be zero if when user has a weight from 1-99, and they run 2+ replicas (surge included)
	zeroAllowed := desiredWeight == 100 || desiredWeight == 0 || (specReplicas == 1 && maxSurge == 0)

	if ceilWeightedCanaryCount < specReplicas || zeroAllowed {
		options = append(options, canaryOption{ceilWeightedCanaryCount, specReplicas})
	}

	if !tied && (floorWeightedCanaryCount != 0 || zeroAllowed) {
		options = append(options, canaryOption{floorWeightedCanaryCount, specReplicas})
	}

	// check if we are allowed to surge. if we are, we can also consider rounding up to spec.replicas + 1
	// in order to achieve a closer canary weight
	if maxSurge > 0 {
		options = append(options, canaryOption{ceilWeightedCanaryCount, specReplicas + 1})
		surgeIsTied := floorCeilingTied(desiredWeight, specReplicas+1)
		if !surgeIsTied && (floorWeightedCanaryCount != 0 || zeroAllowed) {
			options = append(options, canaryOption{floorWeightedCanaryCount, specReplicas + 1})
		}
	}

	if len(options) == 0 {
		// should not get here
		return 0, specReplicas
	}

	bestOption := options[0]
	bestDelta := weightDelta(desiredWeight, bestOption.canary, bestOption.total)
	for i := 1; i < len(options); i++ {
		currOption := options[i]
		currDelta := weightDelta(desiredWeight, currOption.canary, currOption.total)
		if currDelta < bestDelta {
			bestOption = currOption
			bestDelta = currDelta
		}
	}
	return bestOption.canary, bestOption.total - bestOption.canary
}

// floorCeilingTied indicates if the ceiling and floor values are equidistant from the desired weight
// For example: replicas: 3, desiredWeight: 50%
// A canary count of 1 (33.33%) or 2 (66.66%) are both equidistant from desired weight of 50%.
// When this happens, we will pick the larger canary count
func floorCeilingTied(desiredWeight, totalReplicas int32) bool {
	_, frac := math.Modf(float64(totalReplicas) * (float64(desiredWeight) / 100))
	return frac == 0.5
}

// weightDelta calculates the difference that the canary replicas will be from the desired weight
// This is used to pick the closest approximation of canary counts.
func weightDelta(desiredWeight, canaryReplicas, totalReplicas int32) float64 {
	actualWeight := float64(canaryReplicas*100) / float64(totalReplicas)
	return math.Abs(actualWeight - float64(desiredWeight))
}

// calculateScaleDownReplicaCount calculates drainRSReplicaCount
// drainRSReplicaCount can be either stableRS count or canaryRS count
// drainRSReplicaCount corresponds to RS whose availability is not considered in calculating replicasToScaleDown
func calculateScaleDownReplicaCount(drainRS *appsv1.ReplicaSet, desireRSReplicaCount int32, scaleDownCount int32, drainRSReplicaCount int32) int32 {
	if drainRS != nil && *drainRS.Spec.Replicas > desireRSReplicaCount {
		// if the controller doesn't have to use every replica to achieve the desired count,
		// it can scales down to the desired count or get closer to desired state.
		drainRSReplicaCount = maxValue(desireRSReplicaCount, *drainRS.Spec.Replicas-scaleDownCount)
	}
	return drainRSReplicaCount
}

// adjustReplicaWithinLimits adjusts replicaCounters to be within maxSurge & maxUnavailable limits
// drainRSReplicaCount corresponds to RS whose availability is not considered in calculating replicasToScaleDown
// adjustRSReplicaCount corresponds to RS whose availability is to taken account while adjusting maxUnavailable limit
func adjustReplicaWithinLimits(drainRS *appsv1.ReplicaSet, adjustRS *appsv1.ReplicaSet, drainRSReplicaCount int32, adjustRSReplicaCount int32, maxReplicaCountAllowed int32, minAvailableReplicaCount int32) (int32, int32) {
	extraAvailableAdjustRS := int32(0)
	totalAvailableReplicas := int32(0)
	// calculates current limit over the allowed value
	overTheLimitVal := maxValue(0, adjustRSReplicaCount+drainRSReplicaCount-maxReplicaCountAllowed)
	if drainRS != nil {
		totalAvailableReplicas = totalAvailableReplicas + minValue(drainRS.Status.AvailableReplicas, drainRSReplicaCount)
	}
	if adjustRS != nil {
		// 1. adjust adjustRSReplicaCount to be within maxSurge
		adjustRSReplicaCount = adjustRSReplicaCount - overTheLimitVal
		// 2. Calculate availability corresponding to adjusted count
		totalAvailableReplicas = totalAvailableReplicas + minValue(adjustRS.Status.AvailableReplicas, adjustRSReplicaCount)
		// 3. Calculate decrease in availability of adjustRS because of (1)
		extraAvailableAdjustRS = maxValue(0, adjustRS.Status.AvailableReplicas-adjustRSReplicaCount)

		// 4. Now calculate how far count is from maxUnavailable limit
		moreToNeedAvailableReplicas := maxValue(0, minAvailableReplicaCount-totalAvailableReplicas)
		// 5. From (3), we got the count for decrease in availability because of (1),
		// take the min of (3) & (4) and add it back to adjustRS
		// remaining of moreToNeedAvailableReplicas can be ignored as it is part of drainRS,
		// there is no case of deviating from maxUnavailable limit from drainRS as in the event of said case,
		// scaleDown calculation wont even occur as we are checking
		// replicasToScaleDown <= minAvailableReplicaCount in caller function
		adjustRSReplicaCount = adjustRSReplicaCount + minValue(extraAvailableAdjustRS, moreToNeedAvailableReplicas)
		// 6. Calculate final overTheLimit because of adjustment
		overTheLimitVal = maxValue(0, adjustRSReplicaCount+drainRSReplicaCount-maxReplicaCountAllowed)
		// 7. we can safely subtract from drainRS and other cases like deviation from maxUnavailable limit
		// wont occur as said in (5)
		drainRSReplicaCount = drainRSReplicaCount - overTheLimitVal
	}

	return drainRSReplicaCount, adjustRSReplicaCount
}

func minValue(countA int32, countB int32) int32 {
	if countA > countB {
		return countB
	}
	return countA
}

func maxValue(countA int32, countB int32) int32 {
	if countA < countB {
		return countB
	}
	return countA
}

// CheckMinPodsPerReplicaSet ensures that if the desired number of pods in a stable or canary ReplicaSet is not zero,
// then it is at least MinPodsPerReplicaSet for High Availability. Only applicable if using TrafficRouting
func CheckMinPodsPerReplicaSet(rollout *v1alpha1.Rollout, count int32) int32 {
	if count == 0 {
		return count
	}
	if rollout.Spec.Strategy.Canary == nil || rollout.Spec.Strategy.Canary.MinPodsPerReplicaSet == nil || rollout.Spec.Strategy.Canary.TrafficRouting == nil {
		return count
	}
	return max(count, *rollout.Spec.Strategy.Canary.MinPodsPerReplicaSet)
}

// CalculateReplicaCountsForTrafficRoutedCanary calculates the canary and stable replica counts
// when using canary with traffic routing. If current traffic weights are supplied, we factor the
// those weights into the and return the higher of current traffic scale vs. desired traffic scale
// If MinPodsPerReplicaSet is defined and the number of replicas in either RS is not 0, then return at least MinPodsPerReplicaSet
func CalculateReplicaCountsForTrafficRoutedCanary(rollout *v1alpha1.Rollout, weights *v1alpha1.TrafficWeights) (int32, int32) {
	var canaryCount, stableCount int32
	rolloutSpecReplica := defaults.GetReplicasOrDefault(rollout.Spec.Replicas)
	setCanaryScaleReplicas, desiredWeight := GetCanaryReplicasOrWeight(rollout)
	if setCanaryScaleReplicas != nil {
		// a canary count was explicitly set
		canaryCount = *setCanaryScaleReplicas
	} else {
		canaryCount = CheckMinPodsPerReplicaSet(rollout, trafficWeightToReplicas(rolloutSpecReplica, desiredWeight))
	}

	if !rollout.Spec.Strategy.Canary.DynamicStableScale {
		// Not using dynamic stable scaling. Stable should be left fully scaled (100%), and canary
		// will be calculated from setWeight
		return canaryCount, rolloutSpecReplica
	}

	// When using dynamic stable scaling, the stable replica count is calculated from the higher of:
	//  1. actual stable traffic weight
	//  2. desired stable traffic weight
	// Case 1 occurs when we are going from low to high canary weight. The stable scale must remain
	// high, until we reduce traffic to it.
	// Case 2 occurs when we are going from high to low canary weight. In this scenario,
	// we need to increase the stable scale in preparation for increase of traffic to stable.
	stableCount = trafficWeightToReplicas(rolloutSpecReplica, 100-desiredWeight)
	if weights != nil {
		actualStableWeightReplicaCount := trafficWeightToReplicas(rolloutSpecReplica, weights.Stable.Weight)
		stableCount = max(stableCount, actualStableWeightReplicaCount)

		if rollout.Status.Abort {
			// When aborting and using dynamic stable scaling, we cannot reduce canary count until
			// traffic has shifted back to stable. Canary count is calculated from the higher of:
			//  1. actual canary traffic weight
			//  2. desired canary traffic weight
			// This if block makes sure we don't scale down the canary prematurely
			trafficWeightReplicaCount := trafficWeightToReplicas(rolloutSpecReplica, weights.Canary.Weight)
			canaryCount = max(trafficWeightReplicaCount, canaryCount)
		}
	}
	return CheckMinPodsPerReplicaSet(rollout, canaryCount), CheckMinPodsPerReplicaSet(rollout, stableCount)
}

// trafficWeightToReplicas returns the appropriate replicas given the full spec.replicas and a weight
// Rounds up if not evenly divisible.
func trafficWeightToReplicas(replicas, weight int32) int32 {
	return int32(math.Ceil(float64(weight*replicas) / 100))
}

func max(left, right int32) int32 {
	if left > right {
		return left
	}
	return right
}

// BeforeStartingStep checks if canary rollout is at the starting step
func BeforeStartingStep(rollout *v1alpha1.Rollout) bool {
	if rollout.Spec.Strategy.Canary == nil || rollout.Spec.Strategy.Canary.Analysis == nil || rollout.Spec.Strategy.Canary.Analysis.StartingStep == nil {
		return false
	}
	_, currStep := GetCurrentCanaryStep(rollout)
	if currStep == nil {
		return false
	}
	return *currStep < *rollout.Spec.Strategy.Canary.Analysis.StartingStep
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

// GetReplicasForScaleDown returns the total number of replicas to consider for scaling down the
// given ReplicaSet. ignoreAvailability indicates if we are allowed to ignore availability
// of pods during the calculation, in which case we return just the desired replicas.
// The purpose of ignoring availability is to handle the case when the ReplicaSet which we are
// considering for scaledown might be scaled up, but its pods may be unavailable (e.g. because of
// a CrashloopBackoff). In this case we need to return the spec.Replicas so that the controller will
// still consider scaling down this ReplicaSet. Without this, a rollout could become stuck not
// scaling down the stable, in order to make room for more canaries.
func GetReplicasForScaleDown(rs *appsv1.ReplicaSet, ignoreAvailability bool) int32 {
	if rs == nil {
		return int32(0)
	}
	if *rs.Spec.Replicas < rs.Status.AvailableReplicas {
		// The ReplicaSet is already going to scale down replicas since the availableReplica count is bigger
		// than the spec count. The controller uses the .Spec.Replicas to prevent the controller from
		// assuming the extra replicas (availableReplica - .Spec.Replicas) are going to remain available.
		// Otherwise, the controller uses those extra replicas to scale down more replicas and potentially
		// violates the min available.
		return *rs.Spec.Replicas
	}
	if ignoreAvailability {
		return *rs.Spec.Replicas
	}
	return rs.Status.AvailableReplicas
}

// GetCurrentCanaryStep returns the current canary step. If there are no steps or the rollout
// has already executed the last step, the func returns nil
func GetCurrentCanaryStep(rollout *v1alpha1.Rollout) (*v1alpha1.CanaryStep, *int32) {
	if rollout.Spec.Strategy.Canary == nil || len(rollout.Spec.Strategy.Canary.Steps) == 0 {
		return nil, nil
	}
	currentStepIndex := int32(0)
	if rollout.Status.CurrentStepIndex != nil {
		currentStepIndex = *rollout.Status.CurrentStepIndex
	}
	if len(rollout.Spec.Strategy.Canary.Steps) <= int(currentStepIndex) {
		return nil, &currentStepIndex
	}
	return &rollout.Spec.Strategy.Canary.Steps[currentStepIndex], &currentStepIndex
}

// GetCanaryReplicasOrWeight either returns a static set of replicas or a weight percentage
func GetCanaryReplicasOrWeight(rollout *v1alpha1.Rollout) (*int32, int32) {
	if rollout.Status.PromoteFull || rollout.Status.StableRS == "" || rollout.Status.CurrentPodHash == rollout.Status.StableRS {
		return nil, 100
	}
	if scs := UseSetCanaryScale(rollout); scs != nil {
		if scs.Replicas != nil {
			return scs.Replicas, 0
		} else if scs.Weight != nil {
			return nil, *scs.Weight
		}
	}
	return nil, GetCurrentSetWeight(rollout)
}

// GetCurrentSetWeight grabs the current setWeight used by the rollout by iterating backwards from the current step
// until it finds a setWeight step. The controller defaults to 100 if it iterates through all the steps with no
// setWeight or if there is no current step (i.e. the controller has already stepped through all the steps).
func GetCurrentSetWeight(rollout *v1alpha1.Rollout) int32 {
	if rollout.Status.Abort {
		return 0
	}
	currentStep, currentStepIndex := GetCurrentCanaryStep(rollout)
	if currentStep == nil {
		return 100
	}

	for i := *currentStepIndex; i >= 0; i-- {
		step := rollout.Spec.Strategy.Canary.Steps[i]
		if step.SetWeight != nil {
			return *step.SetWeight
		}
	}
	return 0
}

// UseSetCanaryScale will return a SetCanaryScale if specified and should be used, returns nil otherwise.
// TrafficRouting is required to be set for SetCanaryScale to be applicable.
// If MatchTrafficWeight is set after a previous SetCanaryScale step, it will likewise be ignored.
func UseSetCanaryScale(rollout *v1alpha1.Rollout) *v1alpha1.SetCanaryScale {
	if rollout.Spec.Strategy.Canary == nil || rollout.Spec.Strategy.Canary.TrafficRouting == nil {
		// SetCanaryScale only works with TrafficRouting
		return nil
	}
	if rollout.Status.Abort {
		if abortDelay, _ := defaults.GetAbortScaleDownDelaySecondsOrDefault(rollout); abortDelay != nil {
			// If rollout is aborted do not use the set canary scale, *unless* the user explicitly
			// indicated to leave the canary scaled up (abortScaleDownDelaySeconds: 0).
			return nil
		}
	}
	currentStep, currentStepIndex := GetCurrentCanaryStep(rollout)
	if currentStep == nil {
		// setCanaryScale feature is unused
		return nil
	}
	for i := *currentStepIndex; i >= 0; i-- {
		step := rollout.Spec.Strategy.Canary.Steps[i]
		if step.SetCanaryScale == nil {
			continue
		}
		if step.SetCanaryScale.MatchTrafficWeight {
			return nil
		}
		return step.SetCanaryScale
	}
	return nil
}

// GetOtherRSs the function goes through a list of ReplicaSets and returns a list of RS that are not the new or stable RS
func GetOtherRSs(rollout *v1alpha1.Rollout, newRS, stableRS *appsv1.ReplicaSet, allRSs []*appsv1.ReplicaSet) []*appsv1.ReplicaSet {
	otherRSs := []*appsv1.ReplicaSet{}
	for _, rs := range allRSs {
		if rs != nil {
			if stableRS != nil && rs.Name == stableRS.Name {
				continue
			}
			if newRS != nil && rs.Name == newRS.Name {
				continue
			}
			otherRSs = append(otherRSs, rs)
		}
	}
	return otherRSs
}

// GetStableRS finds the stable RS using the RS's RolloutUniqueLabelKey and the stored StableRS in the rollout status
func GetStableRS(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, rslist []*appsv1.ReplicaSet) *appsv1.ReplicaSet {
	if rollout.Status.StableRS == "" {
		return nil
	}
	if newRS != nil && newRS.Labels != nil && newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] == rollout.Status.StableRS {
		return newRS
	}
	for i := range rslist {
		rs := rslist[i]
		if rs != nil {
			if rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] == rollout.Status.StableRS {
				return rs
			}
		}
	}
	return nil
}

// GetCurrentExperimentStep grabs the latest Experiment step
func GetCurrentExperimentStep(r *v1alpha1.Rollout) *v1alpha1.RolloutExperimentStep {
	currentStep, currentStepIndex := GetCurrentCanaryStep(r)
	if currentStep == nil {
		return nil
	}

	for i := *currentStepIndex; i >= 0; i-- {
		step := r.Spec.Strategy.Canary.Steps[i]
		if step.Experiment != nil {
			return step.Experiment
		}
	}
	return nil
}

// ParseExistingPodMetadata returns the existing podMetadata which was injected to the ReplicaSet
// based on examination of rollout.argoproj.io/ephemeral-metadata annotation on  the ReplicaSet.
// Returns nil if there was no metadata, or the metadata was not parseable.
func ParseExistingPodMetadata(rs *appsv1.ReplicaSet) *v1alpha1.PodTemplateMetadata {
	var existingPodMetadata *v1alpha1.PodTemplateMetadata
	if rs.Annotations != nil {
		if existingPodMetadataStr, ok := rs.Annotations[EphemeralMetadataAnnotation]; ok {
			err := json.Unmarshal([]byte(existingPodMetadataStr), &existingPodMetadata)
			if err != nil {
				log.Warnf("Failed to determine existing ephemeral metadata from annotation: %s", existingPodMetadataStr)
				return nil
			}
			return existingPodMetadata
		}
	}
	return nil
}

// SyncEphemeralPodMetadata will inject the desired pod metadata to the ObjectMeta as well as remove
// previously injected pod metadata which is no longer desired. This function is careful to only
// modify metadata that we injected previously, and not affect other metadata which might be
// controlled by other controllers (e.g. istio pod sidecar injector)
func SyncEphemeralPodMetadata(metadata *metav1.ObjectMeta, existingPodMetadata, desiredPodMetadata *v1alpha1.PodTemplateMetadata) (*metav1.ObjectMeta, bool) {
	modified := false
	metadata = metadata.DeepCopy()

	// Inject the desired metadata
	if desiredPodMetadata != nil {
		for k, v := range desiredPodMetadata.Annotations {
			if metadata.Annotations == nil {
				metadata.Annotations = make(map[string]string)
			}
			if prev := metadata.Annotations[k]; prev != v {
				metadata.Annotations[k] = v
				modified = true
			}
		}
		for k, v := range desiredPodMetadata.Labels {
			if metadata.Labels == nil {
				metadata.Labels = make(map[string]string)
			}
			if prev := metadata.Labels[k]; prev != v {
				metadata.Labels[k] = v
				modified = true
			}
		}
	}

	isMetadataStillDesired := func(key string, desired map[string]string) bool {
		_, ok := desired[key]
		return ok
	}
	// Remove existing metadata which is no longer desired
	if existingPodMetadata != nil {
		for k := range existingPodMetadata.Annotations {
			if desiredPodMetadata == nil || !isMetadataStillDesired(k, desiredPodMetadata.Annotations) {
				if metadata.Annotations != nil {
					delete(metadata.Annotations, k)
					modified = true
				}
			}
		}
		for k := range existingPodMetadata.Labels {
			if desiredPodMetadata == nil || !isMetadataStillDesired(k, desiredPodMetadata.Labels) {
				if metadata.Labels != nil {
					delete(metadata.Labels, k)
					modified = true
				}
			}
		}
	}
	return metadata, modified
}

// SyncReplicaSetEphemeralPodMetadata injects the desired pod metadata to the ReplicaSet, and
// removes previously injected metadata (based on the rollout.argoproj.io/ephemeral-metadata
// annotation) if it is no longer desired. A podMetadata value of nil indicates all ephemeral
// metadata should be removed completely.
func SyncReplicaSetEphemeralPodMetadata(rs *appsv1.ReplicaSet, podMetadata *v1alpha1.PodTemplateMetadata) (*appsv1.ReplicaSet, bool) {
	existingPodMetadata := ParseExistingPodMetadata(rs)
	newObjectMeta, modified := SyncEphemeralPodMetadata(&rs.Spec.Template.ObjectMeta, existingPodMetadata, podMetadata)
	rs = rs.DeepCopy()
	if !modified {
		return rs, false
	}
	rs.Spec.Template.ObjectMeta = *newObjectMeta
	if podMetadata != nil {
		// remember what we injected by annotating it
		metadataBytes, _ := json.Marshal(podMetadata)
		if rs.Annotations == nil {
			rs.Annotations = make(map[string]string)
		}
		rs.Annotations[EphemeralMetadataAnnotation] = string(metadataBytes)
	} else {
		delete(rs.Annotations, EphemeralMetadataAnnotation)
	}
	return rs, true
}
