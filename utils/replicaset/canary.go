package replicaset

import (
	"encoding/json"
	"math"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	log "github.com/sirupsen/logrus"
)

const (
	// EphemeralMetadataAnnotation denotes pod metadata which are ephemerally injected to canary/stable pods
	EphemeralMetadataAnnotation = "rollout.argoproj.io/ephemeral-metadata"
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
	rolloutSpecReplica := defaults.GetReplicasOrDefault(rollout.Spec.Replicas)
	replicas, weight := GetCanaryReplicasOrWeight(rollout)

	desiredNewRSReplicaCount := int32(0)
	desiredStableRSReplicaCount := int32(0)
	if replicas != nil {
		desiredNewRSReplicaCount = *replicas
		desiredStableRSReplicaCount = rolloutSpecReplica
	} else {
		desiredNewRSReplicaCount = int32(math.Ceil(float64(rolloutSpecReplica) * (float64(weight) / 100)))
		desiredStableRSReplicaCount = int32(math.Ceil(float64(rolloutSpecReplica) * (1 - (float64(weight) / 100))))
	}

	if !CheckStableRSExists(newRS, stableRS) {
		// If there is no stableRS or it is the same as the newRS, then the rollout does not follow the canary steps.
		// Instead the controller tries to get the newRS to 100% traffic.
		desiredNewRSReplicaCount = rolloutSpecReplica
		desiredStableRSReplicaCount = 0
	}
	// Unlike the ReplicaSet based weighted canary, a service mesh/ingress
	// based canary leaves the stable as 100% scaled until the rollout completes.
	if rollout.Spec.Strategy.Canary.TrafficRouting != nil {
		desiredStableRSReplicaCount = rolloutSpecReplica
	}

	return desiredNewRSReplicaCount, desiredStableRSReplicaCount

}

// CalculateReplicaCountsForCanary calculates the number of replicas for the newRS and the stableRS.  The function
// calculates the desired number of replicas for the new and stable RS using the following equations:
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
// For more examples, check the TestCalculateReplicaCountsForCanary test in canary/canary_test.go
func CalculateReplicaCountsForCanary(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, stableRS *appsv1.ReplicaSet, oldRSs []*appsv1.ReplicaSet) (int32, int32) {
	rolloutSpecReplica := defaults.GetReplicasOrDefault(rollout.Spec.Replicas)
	replicas, weight := GetCanaryReplicasOrWeight(rollout)
	if replicas != nil {
		return *replicas, rolloutSpecReplica
	}

	desiredStableRSReplicaCount := int32(math.Ceil(float64(rolloutSpecReplica) * (1 - (float64(weight) / 100))))
	desiredNewRSReplicaCount := int32(math.Ceil(float64(rolloutSpecReplica) * (float64(weight) / 100)))

	if rollout.Spec.Strategy.Canary.TrafficRouting != nil {
		return desiredNewRSReplicaCount, rolloutSpecReplica
	}

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

	maxSurge := MaxSurge(rollout)

	if extraReplicaAdded(rolloutSpecReplica, weight) {
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

	if newRS != nil && *newRS.Spec.Replicas > desiredNewRSReplicaCount {
		// if the controller doesn't have to use every replica to achieve the desired count, it only scales down to the
		// desired count.
		if *newRS.Spec.Replicas-scaleDownCount < desiredNewRSReplicaCount {
			newRSReplicaCount = desiredNewRSReplicaCount
			// Calculating how many replicas were used to scale down to the desired count
			scaleDownCount = scaleDownCount - (*newRS.Spec.Replicas - desiredNewRSReplicaCount)
		} else {
			// The controller is using every replica it can to get closer to desired state.
			newRSReplicaCount = *newRS.Spec.Replicas - scaleDownCount
			scaleDownCount = 0
		}
	}

	if scaleStableRS && *stableRS.Spec.Replicas > desiredStableRSReplicaCount {
		// This follows the same logic as scaling down the newRS except with the stableRS and it does not need to
		// set the scaleDownCount again since it's not used again
		if *stableRS.Spec.Replicas-scaleDownCount < desiredStableRSReplicaCount {
			stableRSReplicaCount = desiredStableRSReplicaCount
		} else {
			stableRSReplicaCount = *stableRS.Spec.Replicas - scaleDownCount
		}
	}

	return newRSReplicaCount, stableRSReplicaCount
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
		// Otherwise, the controller use those extra replicas to scale down more replicas and potentially
		// violate the min available.
		return *rs.Spec.Replicas
	}
	if ignoreAvailability {
		return *rs.Spec.Replicas
	}
	return rs.Status.AvailableReplicas
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
	if rollout.Status.PromoteFull || rollout.Status.CurrentPodHash == rollout.Status.StableRS {
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
	currentStep, currentStepIndex := GetCurrentCanaryStep(rollout)
	if currentStep == nil {
		return nil
	}
	// SetCanaryScale only works with TrafficRouting
	if rollout.Spec.Strategy.Canary == nil || rollout.Spec.Strategy.Canary.TrafficRouting == nil {
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
