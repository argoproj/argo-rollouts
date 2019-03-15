package replicaset

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func newBlueGreenRollout(specReplicas int32, currentPodHash, activeSelector, preivewSelector string) *v1alpha1.Rollout {
	rollout := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Replicas: &specReplicas,
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreenStrategy: &v1alpha1.BlueGreenStrategy{
					ActiveService:  "active",
					PreviewService: "preview",
					Steps: []v1alpha1.BlueGreenStep{
						{
							SetPreview: &v1alpha1.SetPreview{},
						}, {
							SwitchActive: &v1alpha1.SwitchActive{},
						},
					},
				},
			},
		},
		Status: v1alpha1.RolloutStatus{
			CurrentPodHash: currentPodHash,
		},
	}
	if preivewSelector != "" {
		rollout.Status.BlueGreen.PreviewSelector = preivewSelector
	}
	if activeSelector != "" {
		rollout.Status.BlueGreen.ActiveSelector = activeSelector
	}
	return rollout
}

func TestGetCurrentBlueGreenStep(t *testing.T) {
	rollout := newBlueGreenRollout(10, "abcd", "active", "preview")
	rollout.Status.CurrentStepIndex = pointer.Int32Ptr(0)

	currentStep, index := GetCurrentBlueGreenStep(rollout)
	assert.NotNil(t, currentStep)
	assert.Equal(t, int32(0), *index)

	rollout.Status.CurrentStepIndex = pointer.Int32Ptr(2)
	noMoreStep, _ := GetCurrentBlueGreenStep(rollout)
	assert.Nil(t, noMoreStep)
}

func TestDefaultedBlueGreenSteps(t *testing.T) {
	rollout := newBlueGreenRollout(10, "abcd", "active", "preview")
	rollout.Spec.Strategy.BlueGreenStrategy.Steps = nil

	currentStep, index := GetCurrentBlueGreenStep(rollout)
	assert.Equal(t, v1alpha1.BlueGreenStep{SetPreview: &v1alpha1.SetPreview{}}, *currentStep)
	assert.Equal(t, int32(0), *index)

	rollout.Status.CurrentStepIndex = pointer.Int32Ptr(0)
	currentStep, index = GetCurrentBlueGreenStep(rollout)
	assert.Equal(t, v1alpha1.BlueGreenStep{SetPreview: &v1alpha1.SetPreview{}}, *currentStep)
	assert.Equal(t, int32(0), *index)

	rollout.Status.CurrentStepIndex = pointer.Int32Ptr(1)
	currentStep, index = GetCurrentBlueGreenStep(rollout)
	assert.Equal(t, v1alpha1.BlueGreenStep{Pause: &v1alpha1.RolloutPause{}}, *currentStep)
	assert.Equal(t, int32(1), *index)

	rollout.Status.CurrentStepIndex = pointer.Int32Ptr(2)
	currentStep, index = GetCurrentBlueGreenStep(rollout)
	assert.Equal(t, v1alpha1.BlueGreenStep{SwitchActive: &v1alpha1.SwitchActive{}}, *currentStep)
	assert.Equal(t, int32(2), *index)

	rollout.Status.CurrentStepIndex = pointer.Int32Ptr(3)
	currentStep, index = GetCurrentBlueGreenStep(rollout)
	assert.Nil(t, currentStep)
	assert.Equal(t, int32(3), *index)

	rollout.Spec.Strategy.BlueGreenStrategy.PreviewService = ""
	rollout.Status.CurrentStepIndex = pointer.Int32Ptr(0)
	currentStep, index = GetCurrentBlueGreenStep(rollout)
	assert.Equal(t, v1alpha1.BlueGreenStep{Pause: &v1alpha1.RolloutPause{}}, *currentStep)
	assert.Equal(t, int32(0), *index)

	rollout.Status.CurrentStepIndex = pointer.Int32Ptr(1)
	currentStep, index = GetCurrentBlueGreenStep(rollout)
	assert.Equal(t, v1alpha1.BlueGreenStep{SwitchActive: &v1alpha1.SwitchActive{}}, *currentStep)
	assert.Equal(t, int32(1), *index)

	rollout.Status.CurrentStepIndex = pointer.Int32Ptr(2)
	currentStep, index = GetCurrentBlueGreenStep(rollout)
	assert.Nil(t, currentStep)
	assert.Equal(t, int32(2), *index)

}
