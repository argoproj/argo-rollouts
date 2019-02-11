package replicaset

import (
	"fmt"
	"sort"
	"strconv"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/controller"
	intstrutil "k8s.io/apimachinery/pkg/util/intstr"


	v1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/defaults"
)

// FindNewReplicaSet returns the new RS this given rollout targets.
func FindNewReplicaSet(rollout *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet) *appsv1.ReplicaSet {
	sort.Sort(controller.ReplicaSetsByCreationTimestamp(rsList))
	replicaSetName := fmt.Sprintf("%s-%s", rollout.Name, controller.ComputeHash(&rollout.Spec.Template, rollout.Status.CollisionCount))
	for i := range rsList {
		if rsList[i].Name == replicaSetName {
			return rsList[i]
		}
	}
	// new ReplicaSet does not exist.
	return nil
}

// FindOldReplicaSets returns the old replica sets targeted by the given Rollout, with the given slice of RSes.
func FindOldReplicaSets(rollout *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet) []*appsv1.ReplicaSet {
	var allRSs []*appsv1.ReplicaSet
	newRS := FindNewReplicaSet(rollout, rsList)
	for _, rs := range rsList {
		// Filter out new replica set
		if newRS != nil && rs.UID == newRS.UID {
			continue
		}
		allRSs = append(allRSs, rs)
	}
	return allRSs
}

// NewRSNewReplicas calculates the number of replicas a Rollout's new RS should have.
// When one of the followings is true, we're rolling out the deployment; otherwise, we're scaling it.
// 1) The new RS is saturated: newRS's replicas == deployment's replicas
// 2) Max number of pods allowed is reached: deployment's replicas + maxSurge == all RSs' replicas
func NewRSNewReplicas(rollout *v1alpha1.Rollout, allRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet) (int32, error) {
	switch rollout.Spec.Strategy.Type {
	case v1alpha1.BlueGreenRolloutStrategyType:
		return defaults.GetRolloutReplicasOrDefault(rollout), nil
	default:
		return 0, fmt.Errorf("rollout strategy type %v isn't supported", rollout.Spec.Strategy.Type)
	}
}

func GetCurrentSetWeight(rollout *v1alpha1.Rollout) int32 {
	if len(rollout.Spec.Strategy.CanaryStrategy.Steps) == 0 {
		return 100
	}
	currentCanaryStep := int32(0)
	if rollout.Status.CurrentStepIndex != nil {
		currentCanaryStep = *rollout.Status.CurrentStepIndex
	}

	if len(rollout.Spec.Strategy.CanaryStrategy.Steps) == int(currentCanaryStep) {
		return 100
	}

	for i := currentCanaryStep; i >= 0; i-- {
		step := rollout.Spec.Strategy.CanaryStrategy.Steps[i]
		if step.SetWeight != nil {
			return *step.SetWeight
		}
	}
	return 100
}

// MaxRevision finds the highest revision in the replica sets
func MaxRevision(allRSs []*appsv1.ReplicaSet) int64 {
	max := int64(0)
	for _, rs := range allRSs {
		if v, err := Revision(rs); err != nil {
			// Skip the replica sets when it failed to parse their revision information
			log.WithError(err).Info("Couldn't parse revision, rollout controller will skip it when reconciling revisions.")
		} else if v > max {
			max = v
		}
	}
	return max
}

// Revision returns the revision number of the input object.
func Revision(obj runtime.Object) (int64, error) {
	acc, err := meta.Accessor(obj)
	if err != nil {
		return 0, err
	}
	v, ok := acc.GetAnnotations()[annotations.RevisionAnnotation]
	if !ok {
		return 0, nil
	}
	return strconv.ParseInt(v, 10, 64)
}

// FindActiveOrLatest returns the only active or the latest replica set in case there is at most one active
// replica set. If there are more active replica sets, then we should proportionally scale them.
func FindActiveOrLatest(newRS *appsv1.ReplicaSet, oldRSs []*appsv1.ReplicaSet) *appsv1.ReplicaSet {
	if newRS == nil && len(oldRSs) == 0 {
		return nil
	}

	sort.Sort(sort.Reverse(controller.ReplicaSetsByCreationTimestamp(oldRSs)))
	allRSs := controller.FilterActiveReplicaSets(append(oldRSs, newRS))

	switch len(allRSs) {
	case 0:
		// If there is no active replica set then we should return the newest.
		if newRS != nil {
			return newRS
		}
		return oldRSs[0]
	case 1:
		return allRSs[0]
	default:
		return nil
	}
}

// GetReplicaCountForReplicaSets returns the sum of Replicas of the given replica sets.
func GetReplicaCountForReplicaSets(replicaSets []*appsv1.ReplicaSet) int32 {
	totalReplicas := int32(0)
	for _, rs := range replicaSets {
		if rs != nil {
			totalReplicas += *(rs.Spec.Replicas)
		}
	}
	return totalReplicas
}

// GetAvailableReplicaCountForReplicaSets returns the number of available pods corresponding to the given replica sets.
func GetAvailableReplicaCountForReplicaSets(replicaSets []*appsv1.ReplicaSet) int32 {
	totalAvailableReplicas := int32(0)
	for _, rs := range replicaSets {
		if rs != nil {
			totalAvailableReplicas += rs.Status.AvailableReplicas
		}
	}
	return totalAvailableReplicas
}

// GetActualReplicaCountForReplicaSets returns the sum of actual replicas of the given replica sets.
func GetActualReplicaCountForReplicaSets(replicaSets []*appsv1.ReplicaSet) int32 {
	totalActualReplicas := int32(0)
	for _, rs := range replicaSets {
		if rs != nil {
			totalActualReplicas += rs.Status.Replicas
		}
	}
	return totalActualReplicas
}

// GetReadyReplicaCountForReplicaSets returns the number of ready pods corresponding to the given replica sets.
func GetReadyReplicaCountForReplicaSets(replicaSets []*appsv1.ReplicaSet) int32 {
	totalReadyReplicas := int32(0)
	for _, rs := range replicaSets {
		if rs != nil {
			totalReadyReplicas += rs.Status.ReadyReplicas
		}
	}
	return totalReadyReplicas
}

// ResolveFenceposts resolves both maxSurge and maxUnavailable. This needs to happen in one
// step. For example:
//
// 2 desired, max unavailable 1%, surge 0% - should scale old(-1), then new(+1), then old(-1), then new(+1)
// 1 desired, max unavailable 1%, surge 0% - should scale old(-1), then new(+1)
// 2 desired, max unavailable 25%, surge 1% - should scale new(+1), then old(-1), then new(+1), then old(-1)
// 1 desired, max unavailable 25%, surge 1% - should scale new(+1), then old(-1)
// 2 desired, max unavailable 0%, surge 1% - should scale new(+1), then old(-1), then new(+1), then old(-1)
// 1 desired, max unavailable 0%, surge 1% - should scale new(+1), then old(-1)
func resolveFenceposts(maxSurge, maxUnavailable *intstrutil.IntOrString, desired int32) (int32, int32, error) {
	surge, err := intstrutil.GetValueFromIntOrPercent(intstrutil.ValueOrDefault(maxSurge, intstrutil.FromInt(0)), int(desired), true)
	if err != nil {
		return 0, 0, err
	}
	unavailable, err := intstrutil.GetValueFromIntOrPercent(intstrutil.ValueOrDefault(maxUnavailable, intstrutil.FromInt(0)), int(desired), false)
	if err != nil {
		return 0, 0, err
	}

	if surge == 0 && unavailable == 0 {
		// Validation should never allow the user to explicitly use zero values for both maxSurge
		// maxUnavailable. Due to rounding down maxUnavailable though, it may resolve to zero.
		// If both fenceposts resolve to zero, then we should set maxUnavailable to 1 on the
		// theory that surge might not workf due to quota.
		unavailable = 1
	}

	return int32(surge), int32(unavailable), nil
}

// MaxUnavailable returns the maximum unavailable pods a rolling deployment can take.
func MaxUnavailable(rollout *v1alpha1.Rollout) int32 {
	rolloutReplicas := defaults.GetRolloutReplicasOrDefault(rollout)
	if rollout.Spec.Strategy.Type != v1alpha1.CanaryRolloutStrategyType || rolloutReplicas == 0 {
		return int32(0)
	}

	// Error caught by validation
	_, maxUnavailable, _ := resolveFenceposts(defaults.GetMaxSurgeOrDefault(rollout), defaults.GetMaxUnavailableOrDefault(rollout), rolloutReplicas)
	if maxUnavailable > rolloutReplicas {
		return rolloutReplicas
	}
	return maxUnavailable
}

// MaxSurge returns the maximum surge pods a rolling deployment can take.
func MaxSurge(rollout *v1alpha1.Rollout) int32 {
	rolloutReplicas := defaults.GetRolloutReplicasOrDefault(rollout)
	if rollout.Spec.Strategy.Type != v1alpha1.CanaryRolloutStrategyType {
		return int32(0)
	}
	// Error caught by validation
	maxSurge, _, _ := resolveFenceposts(defaults.GetMaxSurgeOrDefault(rollout), defaults.GetMaxUnavailableOrDefault(rollout), rolloutReplicas)
	return maxSurge
}
