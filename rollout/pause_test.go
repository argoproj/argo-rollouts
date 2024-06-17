package rollout

import (
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	intstr "k8s.io/apimachinery/pkg/util/intstr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestHasAddPause(t *testing.T) {
	work := &pauseContext{
		rollout: &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				ControllerPause: false,
				PauseConditions: []v1alpha1.PauseCondition{},
			},
		},
		log: log.WithFields(log.Fields{}),

		addPauseReasons:    []v1alpha1.PauseReason{v1alpha1.PauseReasonCanaryPauseStep},
		removePauseReasons: []v1alpha1.PauseReason{},
	}

	result := work.HasAddPause()
	assert.Equal(t, true, result)
}

func TestHasAddPauseNoReasons(t *testing.T) {
	work := &pauseContext{
		rollout: &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				ControllerPause: false,
				PauseConditions: []v1alpha1.PauseCondition{},
			},
		},
		log: log.WithFields(log.Fields{}),

		addPauseReasons:    []v1alpha1.PauseReason{},
		removePauseReasons: []v1alpha1.PauseReason{},
	}

	result := work.HasAddPause()
	assert.Equal(t, false, result)
}

func TestCalculatePauseStatus(t *testing.T) {
	work := &pauseContext{
		rollout: &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				ControllerPause: false,
				PauseConditions: []v1alpha1.PauseCondition{},
			},
		},
		log: log.WithFields(log.Fields{}),

		addPauseReasons:    []v1alpha1.PauseReason{v1alpha1.PauseReasonCanaryPauseStep},
		removePauseReasons: []v1alpha1.PauseReason{},
	}

	newStatus := &v1alpha1.RolloutStatus{}
	work.CalculatePauseStatus(newStatus)

	assert.Equal(t, true, newStatus.ControllerPause)
	assert.Len(t, newStatus.PauseConditions, 1)
	assert.Equal(t, v1alpha1.PauseReasonCanaryPauseStep, newStatus.PauseConditions[0].Reason)
}

func TestCalculatePauseStatusRemovePause(t *testing.T) {
	now := v1.NewTime(time.Now())
	work := &pauseContext{
		rollout: &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				ControllerPause: true,
				PauseConditions: []v1alpha1.PauseCondition{
					{
						Reason:    v1alpha1.PauseReasonCanaryPauseStep,
						StartTime: v1.NewTime(now.Add(-1 * time.Minute)),
					},
				},
			},
		},
		log: log.WithFields(log.Fields{}),

		addPauseReasons:    []v1alpha1.PauseReason{},
		removePauseReasons: []v1alpha1.PauseReason{v1alpha1.PauseReasonCanaryPauseStep},
	}

	newStatus := &v1alpha1.RolloutStatus{}
	work.CalculatePauseStatus(newStatus)

	assert.Equal(t, false, newStatus.ControllerPause)
	assert.Len(t, newStatus.PauseConditions, 0)
}

func TestCompletedBlueGreenPause(t *testing.T) {
	now := v1.NewTime(time.Now())
	work := &pauseContext{
		rollout: &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					BlueGreen: &v1alpha1.BlueGreenStrategy{},
				},
			},
			Status: v1alpha1.RolloutStatus{
				ControllerPause: true,
				PauseConditions: []v1alpha1.PauseCondition{
					{
						Reason:    v1alpha1.PauseReasonBlueGreenPause,
						StartTime: now,
					},
				},
				BlueGreen: v1alpha1.BlueGreenStatus{
					ScaleUpPreviewCheckPoint: true,
				},
			},
		},
		log: log.WithFields(log.Fields{}),
	}

	result := work.CompletedBlueGreenPause()
	assert.Equal(t, true, result)
}

func TestCompletedBlueGreenPauseAutoPromotionDisabled(t *testing.T) {
	autoPromotionEnabled := false
	now := v1.NewTime(time.Now())
	work := &pauseContext{
		rollout: &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					BlueGreen: &v1alpha1.BlueGreenStrategy{
						AutoPromotionEnabled: &autoPromotionEnabled,
					},
				},
			},
			Status: v1alpha1.RolloutStatus{
				ControllerPause: true,
				PauseConditions: []v1alpha1.PauseCondition{
					{
						Reason:    v1alpha1.PauseReasonBlueGreenPause,
						StartTime: now,
					},
				},
				BlueGreen: v1alpha1.BlueGreenStatus{
					ScaleUpPreviewCheckPoint: false,
				},
			},
		},
		log: log.WithFields(log.Fields{}),
	}

	result := work.CompletedBlueGreenPause()
	assert.Equal(t, false, result)
}

func TestCompletedCanaryPauseStep(t *testing.T) {
	now := v1.NewTime(time.Now())
	work := &pauseContext{
		rollout: &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				ControllerPause: true,
				PauseConditions: []v1alpha1.PauseCondition{
					{
						Reason:    v1alpha1.PauseReasonCanaryPauseStep,
						StartTime: now,
					},
				},
			},
		},
		log: log.WithFields(log.Fields{}),
	}

	pause := v1alpha1.RolloutPause{
		Duration: &intstr.IntOrString{IntVal: intstr.FromInt(0).IntVal},
	}

	result := work.CompletedCanaryPauseStep(pause)
	assert.Equal(t, true, result)
}

func TestCompletedCanaryPauseStepInProgress(t *testing.T) {
	now := v1.NewTime(time.Now())
	work := &pauseContext{
		rollout: &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				ControllerPause: true,
				PauseConditions: []v1alpha1.PauseCondition{
					{
						Reason:    v1alpha1.PauseReasonCanaryPauseStep,
						StartTime: now,
					},
				},
			},
		},
		log: log.WithFields(log.Fields{}),
	}

	pauseDuration := int((2 * time.Hour).Seconds())
	pause := v1alpha1.RolloutPause{
		Duration: &intstr.IntOrString{IntVal: intstr.FromInt(pauseDuration).IntVal},
	}

	result := work.CompletedCanaryPauseStep(pause)
	assert.Equal(t, false, result)
}
