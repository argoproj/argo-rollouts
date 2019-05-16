package replicaset

import (
	appsv1 "k8s.io/api/apps/v1"

	v1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// GetActiveReplicaSet finds the replicaset that is serving traffic from the active service or returns nil
func GetActiveReplicaSet(allRS []*appsv1.ReplicaSet, activeSelector string) (*appsv1.ReplicaSet, []*appsv1.ReplicaSet) {
	if activeSelector == "" {
		return nil, allRS
	}
	for i, rs := range allRS {
		if rs == nil {
			continue
		}
		if podHash, ok := rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]; ok {
			if podHash == activeSelector {
				return rs, append(allRS[:i], allRS[i+1:]...)
			}
		}
	}
	return nil, allRS
}

func ReadyForPreview(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, allRSs []*appsv1.ReplicaSet) bool {
	newRSReplicaCount, err := NewRSNewReplicas(rollout, allRSs, newRS)
	if err != nil {
		return false
	}
	return *(newRS.Spec.Replicas) == newRSReplicaCount &&
		newRS.Status.AvailableReplicas == newRSReplicaCount
}
