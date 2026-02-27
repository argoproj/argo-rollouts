package rollout

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
)

// TestCanaryRolloutRestartPersistsStatus verifies that when pods are restarted during a canary rollout,
// the status.RestartedAt field is properly persisted even though we return early from reconciliation.
// This is a regression test for the bug where restarting a single-replica canary rollout would get
// stuck in Progressing state because status.RestartedAt was never saved.
func TestCanaryRolloutRestartPersistsStatus(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	// Create a canary rollout with 1 replica (the problematic case)
	steps := []v1alpha1.CanaryStep{{SetWeight: ptr.To[int32](10)}}
	r := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](1), intstr.FromInt(0), intstr.FromInt(0))

	// Set it as healthy and stable
	r.Status.StableRS = r.Status.CurrentPodHash
	r.Status.AvailableReplicas = 1
	r.Status.ReadyReplicas = 1
	r.Status.UpdatedReplicas = 1
	r.Status.Replicas = 1
	healthyCond := conditions.NewRolloutCondition(v1alpha1.RolloutHealthy, corev1.ConditionTrue, conditions.RolloutHealthyReason, conditions.RolloutHealthyMessage)
	conditions.SetRolloutCondition(&r.Status, *healthyCond)

	// Set restartAt to trigger a restart
	now := metav1.Now()
	r.Spec.RestartAt = &now

	rs := newReplicaSetWithStatus(r, 1, 1)
	rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] = r.Status.CurrentPodHash

	// Create a pod that's older than restartAt (needs to be restarted)
	oldPodTime := metav1.NewTime(now.Add(-10 * time.Minute))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "foo-pod",
			Namespace:         r.Namespace,
			CreationTimestamp: oldPodTime,
			Labels:            map[string]string{"foo": "bar"},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(rs, v1alpha1.SchemeGroupVersion.WithKind("ReplicaSet")),
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)
	f.kubeobjects = append(f.kubeobjects, rs, pod)
	f.replicaSetLister = append(f.replicaSetLister, rs)

	// Expect pod eviction
	f.expectCreatePodAction(pod) // This is the eviction

	// IMPORTANT: Expect status patch that includes RestartedAt
	patchIndex := f.expectPatchRolloutAction(r)

	f.run(getKey(r, t))

	// Verify that the status was patched (this is the fix!)
	patch := f.getPatchedRollout(patchIndex)

	// The patch should contain restartedAt being set
	assert.Contains(t, patch, "restartedAt", "Status patch should include restartedAt field")
	assert.Contains(t, patch, now.Format(time.RFC3339), "RestartedAt should be set to the spec.RestartAt value")
}

// TestCanaryRolloutRestartSingleReplicaHealthTransition verifies that a single-replica canary rollout
// properly transitions from Healthy -> Progressing -> Healthy during a pod restart.
func TestCanaryRolloutRestartSingleReplicaHealthTransition(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{SetWeight: ptr.To[int32](10)}}
	r := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](1), intstr.FromInt(0), intstr.FromInt(0))

	// Set it as healthy and stable
	r.Status.StableRS = r.Status.CurrentPodHash
	r.Status.AvailableReplicas = 1
	r.Status.ReadyReplicas = 1
	r.Status.UpdatedReplicas = 1
	r.Status.Replicas = 1

	healthyCond := conditions.NewRolloutCondition(v1alpha1.RolloutHealthy, corev1.ConditionTrue, conditions.RolloutHealthyReason, conditions.RolloutHealthyMessage)
	conditions.SetRolloutCondition(&r.Status, *healthyCond)

	progressingCond := conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionTrue, conditions.NewRSAvailableReason, "ReplicaSet completed")
	conditions.SetRolloutCondition(&r.Status, *progressingCond)

	// Set restartAt (already in the past, so restart should happen immediately)
	restartAt := metav1.NewTime(metav1.Now().Add(-1 * time.Minute))
	r.Spec.RestartAt = &restartAt

	// RestartedAt is NOT set yet (this is the bug scenario)
	r.Status.RestartedAt = nil

	rs := newReplicaSetWithStatus(r, 1, 1)
	rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] = r.Status.CurrentPodHash

	// Pod that's older than restartAt
	oldPodTime := metav1.NewTime(restartAt.Add(-10 * time.Minute))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "foo-pod",
			Namespace:         r.Namespace,
			CreationTimestamp: oldPodTime,
			Labels:            map[string]string{"foo": "bar"},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(rs, v1alpha1.SchemeGroupVersion.WithKind("ReplicaSet")),
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)
	f.kubeobjects = append(f.kubeobjects, rs, pod)
	f.replicaSetLister = append(f.replicaSetLister, rs)

	// Expect pod eviction
	f.expectCreatePodAction(pod)

	// Expect status patch
	patchIndex := f.expectPatchRolloutAction(r)

	f.run(getKey(r, t))

	// Verify the patch contains RestartedAt
	patch := f.getPatchedRollout(patchIndex)
	assert.Contains(t, patch, "restartedAt", "First reconciliation should persist restartedAt even when restarting pods")
}
