package info

import (
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func newBenchNamespace(rollouts, replicaSetsPerRollout, podsPerReplicaSet int) ([]*v1alpha1.Rollout, []*appsv1.ReplicaSet, []*corev1.Pod) {
	var ros []*v1alpha1.Rollout
	var rss []*appsv1.ReplicaSet
	var pods []*corev1.Pod
	for r := 0; r < rollouts; r++ {
		ro := &v1alpha1.Rollout{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("rollout-%d", r),
				Namespace: "default",
				UID:       types.UID(fmt.Sprintf("ro-%d", r)),
			},
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{Canary: &v1alpha1.CanaryStrategy{}},
			},
		}
		ros = append(ros, ro)
		for s := 0; s < replicaSetsPerRollout; s++ {
			rs := &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:            fmt.Sprintf("%s-%d", ro.Name, s),
					Namespace:       ro.Namespace,
					UID:             types.UID(fmt.Sprintf("rs-%d-%d", r, s)),
					OwnerReferences: []metav1.OwnerReference{{Kind: "Rollout", Name: ro.Name, UID: ro.UID}},
				},
			}
			rss = append(rss, rs)
			for p := 0; p < podsPerReplicaSet; p++ {
				pods = append(pods, &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:            fmt.Sprintf("%s-%d", rs.Name, p),
						Namespace:       ro.Namespace,
						UID:             types.UID(fmt.Sprintf("pod-%d-%d-%d", r, s, p)),
						OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID}},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				})
			}
		}
	}
	return ros, rss, pods
}

// BenchmarkGetReplicaSetInfo measures building the ReplicaSet and Pod info of every
// Rollout in a namespace from namespace-wide slices, as the dashboard list view does.
func BenchmarkGetReplicaSetInfo(b *testing.B) {
	ros, rss, pods := newBenchNamespace(50, 5, 4)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, ro := range ros {
			GetReplicaSetInfo(ro.UID, ro, rss, pods)
		}
	}
}
