package conditions

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

// TestRolloutPluginTimedOut_UsesTimeoutSeconds verifies the deadline is sourced from
// spec.timeoutSeconds.
func TestRolloutPluginTimedOut_UsesTimeoutSeconds(t *testing.T) {
	base := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	timeutil.SetNowTimeFunc(func() time.Time { return base })
	defer timeutil.SetNowTimeFunc(time.Now)

	newRP := func(deadline int32) *v1alpha1.RolloutPlugin {
		return &v1alpha1.RolloutPlugin{
			Spec: v1alpha1.RolloutPluginSpec{
				Plugin:         v1alpha1.PluginStep{Name: "argoproj/statefulset"},
				TimeoutSeconds: &deadline,
			},
		}
	}

	// Progressing condition last updated 10s ago.
	newStatus := func() *v1alpha1.RolloutPluginStatus {
		return &v1alpha1.RolloutPluginStatus{
			Conditions: []v1alpha1.RolloutPluginCondition{{
				Type:           v1alpha1.RolloutPluginConditionProgressing,
				Status:         corev1.ConditionTrue,
				Reason:         RolloutPluginProgressingReason,
				LastUpdateTime: metav1.NewTime(base.Add(-10 * time.Second)),
			}},
		}
	}

	t.Run("not timed out when deadline exceeds elapsed", func(t *testing.T) {
		rp := newRP(60)
		assert.False(t, RolloutPluginTimedOut(rp, newStatus()))
	})

	t.Run("timed out when deadline is below elapsed", func(t *testing.T) {
		rp := newRP(5)
		assert.True(t, RolloutPluginTimedOut(rp, newStatus()))
	})

	t.Run("default 600s applies when unset", func(t *testing.T) {
		rp := &v1alpha1.RolloutPlugin{}
		// 10s elapsed is well under the 600s default.
		assert.False(t, RolloutPluginTimedOut(rp, newStatus()))
	})
}

// TestRolloutPluginMadeProgress verifies what refreshes the timeoutSeconds clock, mirroring
// RolloutProgressing: pods moving to the updated revision, old pods going away, more ready or
// available pods, a new stable revision, or a step change.
func TestRolloutPluginMadeProgress(t *testing.T) {
	step := func(i int32) *int32 { return &i }
	old := v1alpha1.RolloutPluginStatus{
		Replicas:          5,
		UpdatedReplicas:   2,
		ReadyReplicas:     4,
		AvailableReplicas: 4,
		CurrentRevision:   "rev-1",
		UpdatedRevision:   "rev-2",
		CurrentStepIndex:  step(1),
		CurrentStepHash:   "hash-1",
	}
	tests := []struct {
		name   string
		mutate func(s *v1alpha1.RolloutPluginStatus)
		want   bool
	}{
		{name: "nothing changed", mutate: func(s *v1alpha1.RolloutPluginStatus) {}, want: false},
		{name: "more updated replicas", mutate: func(s *v1alpha1.RolloutPluginStatus) { s.UpdatedReplicas = 3 }, want: true},
		{name: "fewer updated replicas (rolling back)", mutate: func(s *v1alpha1.RolloutPluginStatus) { s.UpdatedReplicas = 1 }, want: true},
		{name: "old replicas scaled down", mutate: func(s *v1alpha1.RolloutPluginStatus) { s.Replicas = 4 }, want: true},
		{name: "old replicas scaled up", mutate: func(s *v1alpha1.RolloutPluginStatus) { s.Replicas = 6 }, want: false},
		{name: "more ready replicas", mutate: func(s *v1alpha1.RolloutPluginStatus) { s.ReadyReplicas = 5 }, want: true},
		{name: "fewer ready replicas", mutate: func(s *v1alpha1.RolloutPluginStatus) { s.ReadyReplicas = 3 }, want: false},
		{name: "more available replicas", mutate: func(s *v1alpha1.RolloutPluginStatus) { s.AvailableReplicas = 5 }, want: true},
		{name: "fewer available replicas", mutate: func(s *v1alpha1.RolloutPluginStatus) { s.AvailableReplicas = 3 }, want: false},
		{name: "new stable revision", mutate: func(s *v1alpha1.RolloutPluginStatus) { s.CurrentRevision = "rev-2" }, want: true},
		{name: "step advanced", mutate: func(s *v1alpha1.RolloutPluginStatus) { s.CurrentStepIndex = step(2) }, want: true},
		{name: "step index reset", mutate: func(s *v1alpha1.RolloutPluginStatus) { s.CurrentStepIndex = nil }, want: false},
		{name: "steps changed", mutate: func(s *v1alpha1.RolloutPluginStatus) { s.CurrentStepHash = "hash-2" }, want: true},
		{name: "only the updated revision changed", mutate: func(s *v1alpha1.RolloutPluginStatus) { s.UpdatedRevision = "rev-3" }, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rp := &v1alpha1.RolloutPlugin{Status: *old.DeepCopy()}
			newStatus := old.DeepCopy()
			tt.mutate(newStatus)
			assert.Equal(t, tt.want, RolloutPluginMadeProgress(rp, newStatus))
		})
	}
}
