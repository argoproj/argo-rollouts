package rollout

import (
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

func (c *rolloutContext) reconcileCanaryPluginStep() error {
	currentStep, currentStepIndex := replicasetutil.GetCurrentCanaryStep(c.rollout)
	if currentStep == nil || currentStep.Plugin == nil {
		return nil
	}

	stepPlugin, err := c.stepPluginResolver.Resolve(*currentStepIndex, *currentStep.Plugin, c.log)
	if err != nil {
		return fmt.Errorf("could not create step plugin at index %d : %w", *currentStepIndex, err)
	}

	if c.rollout.Status.PromoteFull {
		currentStatus := findCurrentStepStatus(c.rollout.Status.Canary.StepPluginStatuses, *currentStepIndex)
		if currentStatus != nil && currentStatus.Phase == v1alpha1.StepPluginPhaseRunning {
			status, err := stepPlugin.Terminate(c.rollout)
			if err != nil {
				return fmt.Errorf("failed to terminate plugin: %w", err)
			}
			c.newStatus.Canary.StepPluginStatuses = updateStepPluginStatus(c.rollout.Status.Canary.StepPluginStatuses, status)
		}

		return nil
	}

	status, result, err := stepPlugin.Run(c.rollout)
	if err != nil {
		return fmt.Errorf("failed to run plugin: %w", err)
	}
	c.newStatus.Canary.StepPluginStatuses = updateStepPluginStatus(c.rollout.Status.Canary.StepPluginStatuses, status)

	if status.Phase == v1alpha1.StepPluginPhaseRunning && result.RequeueAfter != nil {
		c.enqueueRolloutAfter(c.rollout, *result.RequeueAfter)
		return nil
	}

	if status.Phase == v1alpha1.StepPluginPhaseError {
		c.persistRolloutStatus(&c.newStatus)
		return err
	}

	if status.Phase == v1alpha1.StepPluginPhaseFailed {
		// Call each plugin in reverse order?
		c.pauseContext.AddAbort(status.Message)
	}

	return nil
}

func (c *rolloutContext) isStepPluginCompleted(stepIndex int32) bool {
	status := findCurrentStepStatus(c.newStatus.Canary.StepPluginStatuses, stepIndex)
	return status != nil && (status.Phase == v1alpha1.StepPluginPhaseSuccessful ||
		status.Phase == v1alpha1.StepPluginPhaseFailed ||
		status.Phase == v1alpha1.StepPluginPhaseError)
}

func findCurrentStepStatus(status []v1alpha1.StepPluginStatus, stepIndex int32) *v1alpha1.StepPluginStatus {
	for _, s := range status {
		if s.Index == stepIndex {
			return &s
		}
	}
	return nil
}

func updateStepPluginStatus(statuses []v1alpha1.StepPluginStatus, status *v1alpha1.StepPluginStatus) []v1alpha1.StepPluginStatus {
	// Update new status and preserve order
	newStatuses := []v1alpha1.StepPluginStatus{}
	statusAdded := false
	for _, s := range statuses {
		if !statusAdded && s.Index == status.Index {
			newStatuses = append(newStatuses, *status)
			statusAdded = true
			continue
		}
		newStatuses = append(newStatuses, s)
	}
	if !statusAdded {
		newStatuses = append(newStatuses, *status)
	}
	return newStatuses
}
