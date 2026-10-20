package rollout

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/record"
)

func TestCanaryProgressDeadlineAfterOldReplicaSetScaleDown(t *testing.T) {
	for _, tc := range []struct {
		name        string
		promoted    bool
		available   int32
		oldReplicas int32
		wantTimeout bool
	}{
		{"promoted available canary with stale old status", true, 5, 5, false},
		{"unpromoted stuck canary", false, 5, 5, true},
		{"promoted canary loses availability", true, 4, 5, true},
		{"promoted unavailable canary without old replicas", true, 4, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Generation: 1},
				Spec: v1alpha1.RolloutSpec{
					Replicas: int32Ptr(5), ProgressDeadlineSeconds: int32Ptr(600),
					Strategy: v1alpha1.RolloutStrategy{Canary: &v1alpha1.CanaryStrategy{}},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "1", CurrentPodHash: "new", StableRS: "old",
					Replicas: 5 + tc.oldReplicas, UpdatedReplicas: 5,
					ReadyReplicas: tc.available + tc.oldReplicas, AvailableReplicas: tc.available + tc.oldReplicas,
					Conditions: []v1alpha1.RolloutCondition{
						{Type: v1alpha1.RolloutProgressing, Status: corev1.ConditionTrue, Reason: conditions.ReplicaSetUpdatedReason,
							LastUpdateTime: metav1.NewTime(time.Now().Add(-901 * time.Second))},
						{Type: v1alpha1.RolloutHealthy, Status: corev1.ConditionFalse, Reason: conditions.RolloutHealthyReason},
					},
				},
			}
			if tc.promoted {
				r.Status.StableRS = "new"
			}
			newRS := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "new"},
				Spec:   appsv1.ReplicaSetSpec{Replicas: int32Ptr(5)},
				Status: appsv1.ReplicaSetStatus{Replicas: 5, AvailableReplicas: tc.available}}
			// The scale-down API update removed the annotation and set spec to zero,
			// but the ReplicaSet controller has not yet updated status.
			oldRS := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "old"},
				Spec:   appsv1.ReplicaSetSpec{Replicas: int32Ptr(0)},
				Status: appsv1.ReplicaSetStatus{Replicas: tc.oldReplicas, AvailableReplicas: tc.oldReplicas}}
			c := &rolloutContext{rollout: r, newRS: newRS, stableRS: newRS, reconcilerBase: reconcilerBase{recorder: record.NewFakeEventRecorder()},
				allRSs: []*appsv1.ReplicaSet{newRS, oldRS}, pauseContext: &pauseContext{rollout: r}}
			s := r.Status.DeepCopy()
			c.calculateRolloutConditions(s)
			p := conditions.GetRolloutCondition(*s, v1alpha1.RolloutProgressing)
			require.NotNil(t, p)
			assert.Equal(t, tc.wantTimeout, p.Reason == conditions.TimedOutReason)
			if tc.promoted && tc.available == 5 {
				// Once old status catches up, normal healthy condition calculation resumes.
				s.Replicas, s.ReadyReplicas, s.AvailableReplicas = 5, 5, 5
				c.calculateRolloutConditions(s)
				assert.Equal(t, conditions.NewRSAvailableReason,
					conditions.GetRolloutCondition(*s, v1alpha1.RolloutProgressing).Reason)
			}
		})
	}
}
