package list

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestRolloutStatusDegraded(t *testing.T) {
	ro := newCanaryRollout()
	ro.Status.Conditions = append(ro.Status.Conditions, v1alpha1.RolloutCondition{
		Type:   v1alpha1.RolloutProgressing,
		Reason: "ProgressDeadlineExceeded",
	})
	assert.Equal(t, "Degraded", rolloutStatus(ro))
}

func TestRolloutStatusInvalidSpec(t *testing.T) {
	ro := newCanaryRollout()
	ro.Status.Conditions = append(ro.Status.Conditions, v1alpha1.RolloutCondition{
		Type: v1alpha1.InvalidSpec,
	})
	assert.Equal(t, string(v1alpha1.InvalidSpec), rolloutStatus(ro))
}

func TestRolloutStatusPaused(t *testing.T) {
	ro := newCanaryRollout()
	ro.Spec.Paused = true
	assert.Equal(t, "Paused", rolloutStatus(ro))
}

func TestRolloutStatusProgressing(t *testing.T) {
	{
		ro := newCanaryRollout()
		assert.Equal(t, "Progressing", rolloutStatus(ro))
	}
	{
		ro := newCanaryRollout()
		ro.Status.UpdatedReplicas = 1
		ro.Status.Replicas = 2
		assert.Equal(t, "Progressing", rolloutStatus(ro))
	}
	{
		ro := newCanaryRollout()
		ro.Status.UpdatedReplicas = 2
		ro.Status.Replicas = 1
		assert.Equal(t, "Progressing", rolloutStatus(ro))
	}
	{
		ro := newCanaryRollout()
		ro.Status.AvailableReplicas = 1
		ro.Status.UpdatedReplicas = 2
		assert.Equal(t, "Progressing", rolloutStatus(ro))
	}
	{
		ro := newCanaryRollout()
		ro.Status.AvailableReplicas = 1
		ro.Status.UpdatedReplicas = 2
		assert.Equal(t, "Progressing", rolloutStatus(ro))
	}
	{
		ro := newCanaryRollout()
		ro.Status.Canary.StableRS = ""
		assert.Equal(t, "Progressing", rolloutStatus(ro))
	}
	{
		ro := newCanaryRollout()
		ro.Status.Canary.StableRS = "abc1234"
		ro.Status.CurrentPodHash = "def5678"
		assert.Equal(t, "Progressing", rolloutStatus(ro))
	}
	{
		ro := newBlueGreenRollout()
		ro.Status.BlueGreen.ActiveSelector = ""
		assert.Equal(t, "Progressing", rolloutStatus(ro))
	}
	{
		ro := newBlueGreenRollout()
		ro.Status.BlueGreen.ActiveSelector = "abc1234"
		ro.Status.CurrentPodHash = "def5678"
		assert.Equal(t, "Progressing", rolloutStatus(ro))
	}
}

func TestRolloutStatusHealthy(t *testing.T) {
	{
		ro := newCanaryRollout()
		ro.Status.Replicas = 1
		ro.Status.UpdatedReplicas = 1
		ro.Status.AvailableReplicas = 1
		ro.Status.ReadyReplicas = 1
		ro.Status.Canary.StableRS = "abc1234"
		ro.Status.CurrentPodHash = "abc1234"
		assert.Equal(t, "Healthy", rolloutStatus(ro))
	}
	{
		ro := newBlueGreenRollout()
		ro.Status.Replicas = 1
		ro.Status.UpdatedReplicas = 1
		ro.Status.AvailableReplicas = 1
		ro.Status.ReadyReplicas = 1
		ro.Status.BlueGreen.ActiveSelector = "abc1234"
		ro.Status.CurrentPodHash = "abc1234"
		assert.Equal(t, "Healthy", rolloutStatus(ro))
	}
}
