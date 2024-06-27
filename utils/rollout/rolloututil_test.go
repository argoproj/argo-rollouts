package rollout

import (
	"strconv"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/tj/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
)

func newCanaryRollout() *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "can-guestbook",
			Namespace: "test",
		},
		Spec: v1alpha1.RolloutSpec{
			Replicas: pointer.Int32Ptr(5),
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					Steps: []v1alpha1.CanaryStep{
						{
							SetWeight: pointer.Int32Ptr(10),
						},
						{
							Pause: &v1alpha1.RolloutPause{
								Duration: v1alpha1.DurationFromInt(60),
							},
						},
						{
							SetWeight: pointer.Int32Ptr(20),
						},
					},
				},
			},
		},
		Status: v1alpha1.RolloutStatus{
			CurrentStepIndex:  pointer.Int32Ptr(1),
			Replicas:          4,
			ReadyReplicas:     1,
			UpdatedReplicas:   3,
			AvailableReplicas: 2,
		},
	}
}

func newBlueGreenRollout() *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bg-guestbook",
			Namespace: "test",
		},
		Spec: v1alpha1.RolloutSpec{
			Replicas: pointer.Int32Ptr(5),
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{},
			},
		},
		Status: v1alpha1.RolloutStatus{
			CurrentStepIndex:  pointer.Int32Ptr(1),
			Replicas:          4,
			ReadyReplicas:     1,
			UpdatedReplicas:   3,
			AvailableReplicas: 2,
		},
	}
}

func TestIsFullyPromoted(t *testing.T) {
	{
		ro := &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				StableRS:       "abc123",
				CurrentPodHash: "abc123",
			},
		}
		assert.True(t, IsFullyPromoted(ro))
	}
	{
		ro := &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				StableRS:       "abc123",
				CurrentPodHash: "def456",
			},
		}
		assert.False(t, IsFullyPromoted(ro))
	}
}

func TestRolloutStatusDegraded(t *testing.T) {
	ro := newCanaryRollout()
	ro.Status.Conditions = append(ro.Status.Conditions, v1alpha1.RolloutCondition{
		Type:    v1alpha1.RolloutProgressing,
		Reason:  "ProgressDeadlineExceeded",
		Message: "timed out",
	})
	status, message := GetRolloutPhase(ro)
	assert.Equal(t, v1alpha1.RolloutPhaseDegraded, status)
	assert.Equal(t, "ProgressDeadlineExceeded: timed out", message)
}

func TestRolloutStatusPaused(t *testing.T) {
	ro := newCanaryRollout()
	ro.Spec.Paused = true
	status, message := GetRolloutPhase(ro)
	assert.Equal(t, v1alpha1.RolloutPhasePaused, status)
	assert.Equal(t, "manually paused", message)
}

func TestRolloutStatusProgressing(t *testing.T) {
	{
		ro := newCanaryRollout()
		ro.Spec.Replicas = pointer.Int32Ptr(5)
		ro.Status.UpdatedReplicas = 4
		ro.Status.AvailableReplicas = 4
		ro.Status.Replicas = 5
		status, message := GetRolloutPhase(ro)
		assert.Equal(t, v1alpha1.RolloutPhaseProgressing, status)
		assert.Equal(t, "more replicas need to be updated", message)
	}
	{
		ro := newCanaryRollout()
		ro.Spec.Replicas = pointer.Int32Ptr(5)
		ro.Status.UpdatedReplicas = 5
		ro.Status.AvailableReplicas = 4
		ro.Status.Replicas = 5
		status, message := GetRolloutPhase(ro)
		assert.Equal(t, v1alpha1.RolloutPhaseProgressing, status)
		assert.Equal(t, "updated replicas are still becoming available", message)
	}
	{
		ro := newCanaryRollout()
		ro.Spec.Replicas = pointer.Int32Ptr(5)
		ro.Status.UpdatedReplicas = 5
		ro.Status.AvailableReplicas = 5
		ro.Status.Replicas = 7
		status, message := GetRolloutPhase(ro)
		assert.Equal(t, v1alpha1.RolloutPhaseProgressing, status)
		assert.Equal(t, "old replicas are pending termination", message)
	}
	{
		ro := newBlueGreenRollout()
		ro.Status.BlueGreen.ActiveSelector = "abc1234"
		ro.Status.StableRS = "abc1234"
		ro.Status.CurrentPodHash = "def5678"
		ro.Spec.Replicas = pointer.Int32Ptr(5)
		ro.Status.Replicas = 5
		ro.Status.UpdatedReplicas = 5
		ro.Status.AvailableReplicas = 5
		status, message := GetRolloutPhase(ro)
		assert.Equal(t, v1alpha1.RolloutPhaseProgressing, status)
		assert.Equal(t, "active service cutover pending", message)
	}
	{
		ro := newBlueGreenRollout()
		ro.Spec.Strategy.BlueGreen.PostPromotionAnalysis = &v1alpha1.RolloutAnalysis{}
		ro.Status.BlueGreen.ActiveSelector = "def5678"
		ro.Status.StableRS = "abc1234"
		ro.Status.CurrentPodHash = "def5678"
		ro.Spec.Replicas = pointer.Int32Ptr(5)
		ro.Status.Replicas = 5
		ro.Status.UpdatedReplicas = 5
		ro.Status.AvailableReplicas = 5
		status, message := GetRolloutPhase(ro)
		assert.Equal(t, v1alpha1.RolloutPhaseProgressing, status)
		assert.Equal(t, "waiting for analysis to complete", message)
	}
	{
		ro := newBlueGreenRollout()
		ro.Status.BlueGreen.ActiveSelector = "def5678"
		ro.Status.StableRS = "abc1234"
		ro.Status.CurrentPodHash = "def5678"
		ro.Spec.Replicas = pointer.Int32Ptr(5)
		ro.Status.Replicas = 5
		ro.Status.UpdatedReplicas = 5
		ro.Status.AvailableReplicas = 5
		status, message := GetRolloutPhase(ro)
		assert.Equal(t, v1alpha1.RolloutPhaseProgressing, status)
		assert.Equal(t, "waiting for post-promotion verification to complete", message)
	}
	{
		// Scenario when a newly created rollout has partially filled in status (with hashes)
		// but no updated replica count
		ro := newCanaryRollout()
		ro.Spec.Replicas = nil
		ro.Status = v1alpha1.RolloutStatus{
			ObservedGeneration: strconv.Itoa(int(ro.Generation)),
			StableRS:           "abc1234",
			CurrentPodHash:     "abc1234",
		}
		status, message := GetRolloutPhase(ro)
		assert.Equal(t, v1alpha1.RolloutPhaseProgressing, status)
		assert.Equal(t, "more replicas need to be updated", message)
	}
	{
		// Rollout observed generation is not updated
		ro := newCanaryRollout()
		ro.Generation = 2
		ro.Spec.Replicas = nil
		ro.Status = v1alpha1.RolloutStatus{
			StableRS:           "abc1234",
			CurrentPodHash:     "abc1234",
			ObservedGeneration: "1",
		}
		status, message := GetRolloutPhase(ro)
		assert.Equal(t, v1alpha1.RolloutPhaseProgressing, status)
		assert.Equal(t, "waiting for rollout spec update to be observed", message)
	}
	{
		// Make sure we skip isGenerationObserved check when rollout is a v0.9 legacy rollout using
		// a hash and not a numeric observed generation
		ro := newCanaryRollout()
		ro.Generation = 2
		ro.Spec.Replicas = nil
		ro.Status = v1alpha1.RolloutStatus{
			StableRS:           "abc1234",
			CurrentPodHash:     "abc1234",
			ObservedGeneration: "7d66d4485f",
		}
		status, message := GetRolloutPhase(ro)
		assert.Equal(t, v1alpha1.RolloutPhaseProgressing, status)
		assert.Equal(t, "more replicas need to be updated", message)
	}
	{
		// Verify isGenerationObserved detects a v0.9 legacy rollout which has an all numeric hash
		ro := newCanaryRollout()
		ro.Generation = 2
		ro.Spec.Replicas = nil
		ro.Status = v1alpha1.RolloutStatus{
			StableRS:           "abc1234",
			CurrentPodHash:     "abc1234",
			ObservedGeneration: "1366344857",
		}
		status, message := GetRolloutPhase(ro)
		assert.Equal(t, v1alpha1.RolloutPhaseProgressing, status)
		assert.Equal(t, "more replicas need to be updated", message)
	}
	{
		// Verify rollout is considered progressing if we did not finish restarting pods
		oneMinuteAgo := metav1.Time{Time: time.Now().Add(-1 * time.Minute)}
		ro := newCanaryRollout()
		ro.Spec.RestartAt = &oneMinuteAgo
		ro.Status.Replicas = 5
		ro.Status.UpdatedReplicas = 5
		ro.Status.AvailableReplicas = 5
		ro.Status.ReadyReplicas = 5
		ro.Status.StableRS = "abc1234"
		ro.Status.CurrentPodHash = "abc1234"
		status, message := GetRolloutPhase(ro)
		assert.Equal(t, v1alpha1.RolloutPhaseProgressing, status)
		assert.Equal(t, "rollout is restarting", message)
	}
	{
		// Rollout observed workload generation is not updated
		ro := newCanaryRollout()
		ro.Spec.TemplateResolvedFromRef = true
		annotations.SetRolloutWorkloadRefGeneration(ro, "2")
		ro.Status = v1alpha1.RolloutStatus{
			WorkloadObservedGeneration: "1",
		}
		status, message := GetRolloutPhase(ro)
		assert.Equal(t, v1alpha1.RolloutPhaseProgressing, status)
		assert.Equal(t, "waiting for rollout spec update to be observed for the reference workload", message)
	}
	{
		ro := newCanaryRollout()

		observed := isWorkloadGenerationObserved(ro)
		assert.True(t, observed)

		annotations.SetRolloutWorkloadRefGeneration(ro, "2")
		ro.Status.WorkloadObservedGeneration = "222222222222222222"
		observed = isWorkloadGenerationObserved(ro)
		assert.True(t, observed)

		ro.Status.WorkloadObservedGeneration = "1"
		observed = isWorkloadGenerationObserved(ro)
		assert.False(t, observed)

		ro.Status.WorkloadObservedGeneration = "2"
		observed = isWorkloadGenerationObserved(ro)
		assert.True(t, observed)
	}
}

func TestRolloutStatusHealthy(t *testing.T) {
	{
		ro := newCanaryRollout()
		ro.Status.Replicas = 5
		ro.Status.UpdatedReplicas = 5
		ro.Status.AvailableReplicas = 5
		ro.Status.ReadyReplicas = 5
		ro.Status.StableRS = "abc1234"
		ro.Status.CurrentPodHash = "abc1234"
		status, message := GetRolloutPhase(ro)
		assert.Equal(t, v1alpha1.RolloutPhaseHealthy, status)
		assert.Equal(t, "", message)
	}
	{
		ro := newBlueGreenRollout()
		ro.Status.Replicas = 5
		ro.Status.UpdatedReplicas = 5
		ro.Status.AvailableReplicas = 5
		ro.Status.ReadyReplicas = 5
		ro.Status.BlueGreen.ActiveSelector = "abc1234"
		ro.Status.CurrentPodHash = "abc1234"
		ro.Status.StableRS = "abc1234"
		status, message := GetRolloutPhase(ro)
		assert.Equal(t, v1alpha1.RolloutPhaseHealthy, status)
		assert.Equal(t, "", message)
	}
	{
		oneMinuteAgo := metav1.Time{Time: time.Now().Add(-1 * time.Minute)}
		ro := newCanaryRollout()
		ro.Spec.RestartAt = &oneMinuteAgo
		ro.Status.RestartedAt = &oneMinuteAgo
		ro.Status.Replicas = 5
		ro.Status.UpdatedReplicas = 5
		ro.Status.AvailableReplicas = 5
		ro.Status.ReadyReplicas = 5
		ro.Status.StableRS = "abc1234"
		ro.Status.CurrentPodHash = "abc1234"
		status, message := GetRolloutPhase(ro)
		assert.Equal(t, v1alpha1.RolloutPhaseHealthy, status)
		assert.Equal(t, "", message)
	}
	{
		// Rollout observed workload generation is updated
		ro := newCanaryRollout()
		annotations.SetRolloutWorkloadRefGeneration(ro, "2")
		ro.Status.Replicas = 5
		ro.Status.UpdatedReplicas = 5
		ro.Status.AvailableReplicas = 5
		ro.Status.ReadyReplicas = 5
		ro.Status.StableRS = "abc1234"
		ro.Status.CurrentPodHash = "abc1234"
		ro.Status.WorkloadObservedGeneration = "2"
		status, message := GetRolloutPhase(ro)
		assert.Equal(t, v1alpha1.RolloutPhaseHealthy, status)
		assert.Equal(t, "", message)
	}
}

func TestCanaryStepString(t *testing.T) {
	ten := intstr.FromInt(10)
	tenS := intstr.FromString("10s")
	tests := []struct {
		step           v1alpha1.CanaryStep
		expectedString string
	}{
		{
			step:           v1alpha1.CanaryStep{SetWeight: pointer.Int32Ptr(20)},
			expectedString: "setWeight: 20",
		},
		{
			step:           v1alpha1.CanaryStep{Pause: &v1alpha1.RolloutPause{}},
			expectedString: "pause",
		},
		{
			step:           v1alpha1.CanaryStep{Pause: &v1alpha1.RolloutPause{Duration: &ten}},
			expectedString: "pause: 10",
		},
		{
			step:           v1alpha1.CanaryStep{Pause: &v1alpha1.RolloutPause{Duration: &tenS}},
			expectedString: "pause: 10s",
		},
		{
			step:           v1alpha1.CanaryStep{Experiment: &v1alpha1.RolloutExperimentStep{}},
			expectedString: "experiment",
		},
		{
			step:           v1alpha1.CanaryStep{Analysis: &v1alpha1.RolloutAnalysis{}},
			expectedString: "analysis",
		},
		{
			step:           v1alpha1.CanaryStep{SetCanaryScale: &v1alpha1.SetCanaryScale{Weight: pointer.Int32Ptr(20)}},
			expectedString: "setCanaryScale{weight: 20}",
		},
		{
			step:           v1alpha1.CanaryStep{SetCanaryScale: &v1alpha1.SetCanaryScale{MatchTrafficWeight: true}},
			expectedString: "setCanaryScale{matchTrafficWeight: true}",
		},
		{
			step:           v1alpha1.CanaryStep{SetCanaryScale: &v1alpha1.SetCanaryScale{Replicas: pointer.Int32Ptr(5)}},
			expectedString: "setCanaryScale{replicas: 5}",
		},
	}
	for _, test := range tests {
		assert.Equal(t, test.expectedString, CanaryStepString(test.step))
	}
}

func TestIsUnpausing(t *testing.T) {
	ro := newCanaryRollout()
	ro.Status.Phase = v1alpha1.RolloutPhasePaused
	ro.Status.Message = "canary pause"
	ro.Status.PauseConditions = []v1alpha1.PauseCondition{
		{
			Reason: v1alpha1.PauseReasonCanaryPauseStep,
		},
	}
	ro.Status.ControllerPause = true
	status, message := GetRolloutPhase(ro)
	assert.Equal(t, v1alpha1.RolloutPhasePaused, status)
	assert.Equal(t, "canary pause", message)

	ro.Status.PauseConditions = nil
	status, message = GetRolloutPhase(ro)
	assert.Equal(t, v1alpha1.RolloutPhaseProgressing, status)
	assert.Equal(t, "waiting for rollout to unpause", message)
}

func TestShouldVerifyWeight(t *testing.T) {
	ro := newCanaryRollout()
	ro.Status.StableRS = "34feab23f"
	ro.Status.CurrentStepIndex = pointer.Int32Ptr(0)
	ro.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
		SetWeight: pointer.Int32Ptr(20),
	}}
	assert.Equal(t, true, ShouldVerifyWeight(ro))

	ro.Status.StableRS = ""
	assert.Equal(t, false, ShouldVerifyWeight(ro))

	ro.Status.StableRS = "34feab23f"
	ro.Status.CurrentStepIndex = nil
	ro.Spec.Strategy.Canary.Steps = nil
	assert.Equal(t, false, ShouldVerifyWeight(ro))
}

func Test_isGenerationObserved(t *testing.T) {
	tests := []struct {
		name string
		ro   *v1alpha1.Rollout
		want bool
	}{
		{
			name: "test parse generation failed",
			ro: &v1alpha1.Rollout{
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "invalid",
				},
			},
			want: true,
		},
		{
			name: "test status.generation more than spec.generation",
			ro: &v1alpha1.Rollout{
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
				},
				ObjectMeta: metav1.ObjectMeta{
					Generation: 9,
				},
			},
			want: true,
		},
		{
			name: "test status.generation equal to spec.generation",
			ro: &v1alpha1.Rollout{
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
				},
				ObjectMeta: metav1.ObjectMeta{
					Generation: 10,
				},
			},
			want: true,
		},
		{
			name: "test status.generation less than spec.generation",
			ro: &v1alpha1.Rollout{
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
				},
				ObjectMeta: metav1.ObjectMeta{
					Generation: 11,
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGenerationObserved(tt.ro); got != tt.want {
				t.Errorf("isGenerationObserved() = %v, want %v", got, tt.want)
			}
		})
	}
}
