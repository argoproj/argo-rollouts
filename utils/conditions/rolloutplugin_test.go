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
