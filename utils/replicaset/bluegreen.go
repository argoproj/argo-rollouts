package replicaset

import (
	appsv1 "k8s.io/api/apps/v1"

	v1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// GetReplicaSetByTemplateHash find the replicaset that matches the podTemplateHash
func GetReplicaSetByTemplateHash(allRS []*appsv1.ReplicaSet, podTemplateHash string) (*appsv1.ReplicaSet, []*appsv1.ReplicaSet) {
	if podTemplateHash == "" {
		return nil, allRS
	}

	otherRSs := []*appsv1.ReplicaSet{}
	var filterRS *appsv1.ReplicaSet
	for i := range allRS {
		rs := allRS[i]
		if rs == nil {
			continue
		}
		if rsPodHash, ok := rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]; ok {
			if rsPodHash == podTemplateHash {
				filterRS = rs.DeepCopy()
				continue
			}
			otherRSs = append(otherRSs, rs)
		}
	}
	return filterRS, otherRSs
}

func ReadyForPause(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, allRSs []*appsv1.ReplicaSet) bool {
	newRSReplicaCount, err := NewRSNewReplicas(rollout, allRSs, newRS, nil)
	if err != nil {
		return false
	}
	return *(newRS.Spec.Replicas) == newRSReplicaCount &&
		newRS.Status.AvailableReplicas == newRSReplicaCount
}
