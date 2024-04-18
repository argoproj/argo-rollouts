package rollout

import (
	"fmt"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

func (c *rolloutContext) reconcileCanaryPluginStep() error {

	//On abort, we need to abort all successful previous steps
	if c.pauseContext.IsAborted() {
		c.stepPluginStatuses = c.rollout.Status.Canary.StepPluginStatuses

		// In an abort, the current step might be the current or last, depending on when the abort happened.
		_, currentStepIndex := replicasetutil.GetCurrentCanaryStep(c.rollout)
		startingIndex := *currentStepIndex
		for i := startingIndex; i >= 0; i-- {
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

	currentStep, currentStepIndex := replicasetutil.GetCurrentCanaryStep(c.rollout)
	if currentStep == nil || currentStep.Plugin == nil {
		return nil
	}

	stepPlugin, err := c.stepPluginResolver.Resolve(*currentStepIndex, *currentStep.Plugin, c.log)
	if err != nil {
		return fmt.Errorf("could not create step plugin at index %d : %w", *currentStepIndex, err)
	}

	// On full promotion, we want to Terminate the current step
	if c.rollout.Status.PromoteFull {
		status, err := stepPlugin.Terminate(c.rollout)
		if err != nil {
			return fmt.Errorf("failed to terminate plugin: %w", err)
		}
		c.stepPluginStatuses = updateStepPluginStatus(c.rollout.Status.Canary.StepPluginStatuses, status)
		return nil
	}

	// Normal execution of a step plugin
	status, result, err := stepPlugin.Run(c.rollout)
	if err != nil {
		return fmt.Errorf("failed to run plugin: %w", err)
	}
	c.stepPluginStatuses = updateStepPluginStatus(c.rollout.Status.Canary.StepPluginStatuses, status)

	if status == nil {
		return nil
	}

	if status.Phase == v1alpha1.StepPluginPhaseRunning && result != nil && result.RequeueAfter != nil {
		c.enqueueRolloutAfter(c.rollout, *result.RequeueAfter)
		return nil
	}

	if status.Phase == v1alpha1.StepPluginPhaseError {
		// It could be interesting to implement a backoff mechanism
		c.enqueueRolloutAfter(c.rollout, 30*time.Second)
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

func (c *rolloutContext) isStepPluginCompleted(stepIndex int32) bool {
	updatedPluginStatus := c.calculateStepPluginStatus()
	runStatus := findCurrentStepStatus(updatedPluginStatus, stepIndex, v1alpha1.StepPluginOperationRun)
	isRunning := runStatus != nil && runStatus.Phase == v1alpha1.StepPluginPhaseRunning
	if isRunning {
		terminateStatus := findCurrentStepStatus(updatedPluginStatus, stepIndex, v1alpha1.StepPluginOperationTerminate)
		abortStatus := findCurrentStepStatus(updatedPluginStatus, stepIndex, v1alpha1.StepPluginOperationAbort)
		isRunning = terminateStatus == nil && abortStatus == nil
	}
	return runStatus != nil && ((!isRunning && runStatus.Phase == v1alpha1.StepPluginPhaseRunning) || runStatus.Phase == v1alpha1.StepPluginPhaseFailed || runStatus.Phase == v1alpha1.StepPluginPhaseSuccessful)
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
