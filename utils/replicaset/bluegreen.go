package replicaset

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// GetCurrentBlueGreenStep returns the current bluegreen step. If there are no steps or the rollout
// has already executed the last step, the func returns nil
func GetCurrentBlueGreenStep(rollout *v1alpha1.Rollout) (*v1alpha1.BlueGreenStep, *int32) {
	if len(rollout.Spec.Strategy.BlueGreenStrategy.Steps) == 0 {
		return GetDefaultedBlueGreenSteps(rollout)
	}
	currentStepIndex := int32(0)
	if rollout.Status.CurrentStepIndex != nil {
		currentStepIndex = *rollout.Status.CurrentStepIndex
	}
	if len(rollout.Spec.Strategy.BlueGreenStrategy.Steps) <= int(currentStepIndex) {
		return nil, &currentStepIndex
	}
	return &rollout.Spec.Strategy.BlueGreenStrategy.Steps[currentStepIndex], &currentStepIndex
}

func GetDefaultedBlueGreenSteps(rollout *v1alpha1.Rollout) (*v1alpha1.BlueGreenStep, *int32) {
	currentStepIndex := int32(0)
	if rollout.Status.CurrentStepIndex != nil {
		currentStepIndex = *rollout.Status.CurrentStepIndex
	}
	if rollout.Spec.Strategy.BlueGreenStrategy.PreviewService == "" {
		if currentStepIndex == 0 {
			return &v1alpha1.BlueGreenStep{Pause: &v1alpha1.RolloutPause{}}, &currentStepIndex
		}
		if currentStepIndex == 1 {
			return &v1alpha1.BlueGreenStep{SwitchActive: &v1alpha1.SwitchActive{}}, &currentStepIndex
		}
		return nil, &currentStepIndex
	}
	if currentStepIndex == 0 {
		return &v1alpha1.BlueGreenStep{SetPreview: &v1alpha1.SetPreview{}}, &currentStepIndex
	}
	if currentStepIndex == 1 {
		return &v1alpha1.BlueGreenStep{Pause: &v1alpha1.RolloutPause{}}, &currentStepIndex
	}
	if currentStepIndex == 2 {
		return &v1alpha1.BlueGreenStep{SwitchActive: &v1alpha1.SwitchActive{}}, &currentStepIndex
	}
	return nil, &currentStepIndex
}
