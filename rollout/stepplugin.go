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

	stepPlugin, err := c.stepPluginResolver.Resolve(*currentStepIndex, *currentStep.Plugin)
	if err != nil {
		return fmt.Errorf("could not create step plugin at index %d : %w", *currentStepIndex, err)
	}

	status, result, err := stepPlugin.Run(c.rollout)
	if err != nil {
		return fmt.Errorf("Error calling Run on plugin: %w", err)
	}

	// Update new status and preserve order
	pluginStatuses := []v1alpha1.StepPluginStatus{}
	statusAdded := false
	for _, s := range c.rollout.Status.Canary.StepPluginStatuses {
		if s.Index == *currentStepIndex {
			pluginStatuses = append(pluginStatuses, *status)
			statusAdded = true
			continue
		}
		pluginStatuses = append(pluginStatuses, s)
	}
	if !statusAdded {
		pluginStatuses = append(pluginStatuses, *status)
	}
	c.newStatus.Canary.StepPluginStatuses = pluginStatuses

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

func (c *rolloutContext) isPluginStepCompleted(stepIndex int32) bool {
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
