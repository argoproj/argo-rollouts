package rollout

import (
	"fmt"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	rolloututil "github.com/argoproj/argo-rollouts/utils/rollout"
)

func (c *rolloutContext) reconcileCanaryPluginStep() error {

	//On abort, we need to abort all successful previous steps
	if c.pauseContext.IsAborted() {
		c.stepPluginStatuses = c.rollout.Status.Canary.StepPluginStatuses

		// In an abort, the current step might be the current or last, depending on when the abort happened.
		_, currentStepIndex := replicasetutil.GetCurrentCanaryStep(c.rollout)
		startingIndex := *currentStepIndex
		for i := startingIndex; i >= 0; i-- {
			if i >= int32(len(c.rollout.Spec.Strategy.Canary.Steps)) {
				continue
			}
			currentStep := &c.rollout.Spec.Strategy.Canary.Steps[i]
			if currentStep.Plugin == nil {
				continue
			}

			stepPlugin, err := c.stepPluginResolver.Resolve(i, *currentStep.Plugin, c.log)
			if err != nil {
				return fmt.Errorf("could not create step plugin at index %d : %w", i, err)
			}
			status, err := stepPlugin.Abort(c.rollout)
			if err != nil {
				return fmt.Errorf("failed to abort plugin: %w", err)
			}
			c.stepPluginStatuses = updateStepPluginStatus(c.stepPluginStatuses, status)
		}
		return nil
	}

	// On full promotion, we want to Terminate the last step stuck in Running
	// At this point, the currentStepIndex is the current or last one
	if c.rollout.Status.PromoteFull || rolloututil.IsFullyPromoted(c.rollout) {
		_, currentStepIndex := replicasetutil.GetCurrentCanaryStep(c.rollout)
		startingIndex := *currentStepIndex

		for i := startingIndex; i >= 0; i-- {
			if i >= int32(len(c.rollout.Spec.Strategy.Canary.Steps)) {
				continue
			}
			currentStep := &c.rollout.Spec.Strategy.Canary.Steps[i]
			if currentStep.Plugin == nil {
				continue
			}
			runningStatus := findCurrentStepStatus(c.rollout.Status.Canary.StepPluginStatuses, i, v1alpha1.StepPluginOperationRun)
			if runningStatus == nil || runningStatus.Phase != v1alpha1.StepPluginPhaseRunning {
				continue
			}

			// found the last running step
			stepPlugin, err := c.stepPluginResolver.Resolve(i, *currentStep.Plugin, c.log)
			if err != nil {
				return fmt.Errorf("could not create step plugin at index %d : %w", *currentStepIndex, err)
			}

			status, err := stepPlugin.Terminate(c.rollout)
			if err != nil {
				return fmt.Errorf("failed to terminate plugin: %w", err)
			}
			c.stepPluginStatuses = updateStepPluginStatus(c.rollout.Status.Canary.StepPluginStatuses, status)
			return nil
		}
		return nil
	}

	// Normal execution flow of a step plugin
	currentStep, currentStepIndex := replicasetutil.GetCurrentCanaryStep(c.rollout)
	if currentStep == nil || currentStep.Plugin == nil {
		return nil
	}

	stepPlugin, err := c.stepPluginResolver.Resolve(*currentStepIndex, *currentStep.Plugin, c.log)
	if err != nil {
		return fmt.Errorf("could not create step plugin at index %d : %w", *currentStepIndex, err)
	}
	status, err := stepPlugin.Run(c.rollout)
	if err != nil {
		return fmt.Errorf("failed to run plugin: %w", err)
	}
	c.stepPluginStatuses = updateStepPluginStatus(c.rollout.Status.Canary.StepPluginStatuses, status)

	if status == nil {
		return nil
	}

	if status.Phase == v1alpha1.StepPluginPhaseRunning || status.Phase == v1alpha1.StepPluginPhaseError {
		duration, err := status.Backoff.Duration()
		if err != nil {
			return fmt.Errorf("failed to parse backoff duration: %w", err)
		}
		// Add a little delay to make sure we reconcile after the backoff
		duration += 5 * time.Second
		c.enqueueRolloutAfter(c.rollout, duration)
		return nil
	}

	if status.Phase == v1alpha1.StepPluginPhaseFailed {
		c.pauseContext.AddAbort(status.Message)
	}

	return nil
}

func (c *rolloutContext) calculateStepPluginStatus() []v1alpha1.StepPluginStatus {
	if c.stepPluginStatuses == nil {
		return c.rollout.Status.Canary.StepPluginStatuses
	}

	return c.stepPluginStatuses
}

func (c *rolloutContext) isStepPluginDisabled(stepIndex int32, step *v1alpha1.PluginStep) (bool, error) {
	stepPlugin, err := c.stepPluginResolver.Resolve(stepIndex, *step, c.log)
	if err != nil {
		return false, err
	}
	return !stepPlugin.Enabled(), nil
}

func (c *rolloutContext) isStepPluginCompleted(stepIndex int32, step *v1alpha1.PluginStep) bool {
	disabled, err := c.isStepPluginDisabled(stepIndex, step)
	if err != nil {
		// If there is an error, the plugin might not exist in the config. We do
		c.log.Errorf("cannot resolve step plugin %s at index %d. Assuming it is enabled.", step.Name, stepIndex)
		disabled = false
	}

	updatedPluginStatus := c.calculateStepPluginStatus()
	runStatus := findCurrentStepStatus(updatedPluginStatus, stepIndex, v1alpha1.StepPluginOperationRun)
	isRunning := runStatus != nil && runStatus.Phase == v1alpha1.StepPluginPhaseRunning
	if isRunning {
		terminateStatus := findCurrentStepStatus(updatedPluginStatus, stepIndex, v1alpha1.StepPluginOperationTerminate)
		abortStatus := findCurrentStepStatus(updatedPluginStatus, stepIndex, v1alpha1.StepPluginOperationAbort)
		isRunning = terminateStatus == nil && abortStatus == nil
	}
	return disabled ||
		(runStatus != nil &&
			((!isRunning && runStatus.Phase == v1alpha1.StepPluginPhaseRunning) ||
				runStatus.Phase == v1alpha1.StepPluginPhaseFailed ||
				runStatus.Phase == v1alpha1.StepPluginPhaseSuccessful))
}

func findCurrentStepStatus(status []v1alpha1.StepPluginStatus, stepIndex int32, operation v1alpha1.StepPluginOperation) *v1alpha1.StepPluginStatus {
	for _, s := range status {
		if s.Index == stepIndex && s.Operation == operation {
			return &s
		}
	}
	return nil
}

func updateStepPluginStatus(statuses []v1alpha1.StepPluginStatus, status *v1alpha1.StepPluginStatus) []v1alpha1.StepPluginStatus {
	if status == nil {
		return statuses
	}

	// Update new status and preserve order
	newStatuses := []v1alpha1.StepPluginStatus{}
	statusAdded := false
	for _, s := range statuses {
		if !statusAdded && s.Index == status.Index && s.Operation == status.Operation {
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
