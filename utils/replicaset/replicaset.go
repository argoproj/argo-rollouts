package replicaset

import (
	"fmt"
	"sort"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/controller"

	v1alpha1 "github.com/argoproj/rollout-controller/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/rollout-controller/utils/annotations"
	"github.com/argoproj/rollout-controller/utils/defaults"
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

// MaxRevision finds the highest revision in the replica sets
func MaxRevision(allRSs []*appsv1.ReplicaSet) int64 {
	max := int64(0)
	for _, rs := range allRSs {
		if v, err := Revision(rs); err != nil {
			// Skip the replica sets when it failed to parse their revision information
			klog.V(4).Infof("Error: %v. Couldn't parse revision for replica set %#v, deployment controller will skip it when reconciling revisions.", err, rs)
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
