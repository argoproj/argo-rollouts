package k8sutil

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/dump"
	"k8s.io/apimachinery/pkg/util/rand"
)

// ReplicaSetsByCreationTimestamp sorts replica sets by creation timestamp, using their names as a tie breaker.
// Copied from k8s.io/kubernetes/pkg/controller to avoid importing the kubernetes monorepo in library packages.
type ReplicaSetsByCreationTimestamp []*appsv1.ReplicaSet

func (o ReplicaSetsByCreationTimestamp) Len() int      { return len(o) }
func (o ReplicaSetsByCreationTimestamp) Swap(i, j int) { o[i], o[j] = o[j], o[i] }
func (o ReplicaSetsByCreationTimestamp) Less(i, j int) bool {
	if o[i].CreationTimestamp.Equal(&o[j].CreationTimestamp) {
		return o[i].Name < o[j].Name
	}
	return o[i].CreationTimestamp.Before(&o[j].CreationTimestamp)
}

// FilterActiveReplicaSets returns replica sets that have (or at least ought to have) pods.
func FilterActiveReplicaSets(replicaSets []*appsv1.ReplicaSet) []*appsv1.ReplicaSet {
	active := make([]*appsv1.ReplicaSet, 0, len(replicaSets))
	for _, rs := range replicaSets {
		if rs != nil && rs.Spec.Replicas != nil && *rs.Spec.Replicas > 0 {
			active = append(active, rs)
		}
	}
	return active
}

// ComputeHash returns a hash value calculated from pod template and a collisionCount to avoid hash collision.
// This matches the legacy k8s.io/kubernetes/pkg/controller.ComputeHash implementation so existing
// ReplicaSets labeled with the old hash remain discoverable after upgrades.
func ComputeHash(template *corev1.PodTemplateSpec, collisionCount *int32) string {
	podTemplateSpecHasher := fnv.New32a()
	fmt.Fprintf(podTemplateSpecHasher, "%v", dump.ForHash(*template))

	if collisionCount != nil {
		collisionCountBytes := make([]byte, 8)
		binary.LittleEndian.PutUint32(collisionCountBytes, uint32(*collisionCount))
		podTemplateSpecHasher.Write(collisionCountBytes)
	}

	return rand.SafeEncodeString(fmt.Sprint(podTemplateSpecHasher.Sum32()))
}
