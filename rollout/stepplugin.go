package rollout

import (
	"fmt"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/steps/plugin"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/record"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	rolloututil "github.com/argoproj/argo-rollouts/utils/rollout"
	log "github.com/sirupsen/logrus"
)

type stepPluginContext struct {
	resolver plugin.Resolver
	log      *log.Entry

	stepPluginStatuses []v1alpha1.StepPluginStatus
	hasError           bool
}

func (spc *stepPluginContext) reconcile(c *rolloutContext) error {
	rollout := c.rollout.DeepCopy()
	spc.stepPluginStatuses = rollout.Status.Canary.StepPluginStatuses

	//On abort, we need to abort all successful previous steps
	if c.pauseContext.IsAborted() {
		// In an abort, the current step might be the current or last, depending on when the abort happened.
		_, currentStepIndex := replicasetutil.GetCurrentCanaryStep(rollout)
		startingIndex := *currentStepIndex
		for i := startingIndex; i >= 0; i-- {
			if i >= int32(len(rollout.Spec.Strategy.Canary.Steps)) {
				continue
			}
			currentStep := &rollout.Spec.Strategy.Canary.Steps[i]
			if currentStep.Plugin == nil {
				continue
			}

			stepPlugin, err := spc.resolver.Resolve(i, *currentStep.Plugin, c.log)
			if err != nil {
				return spc.handleError(c, fmt.Errorf("could not create step plugin at index %d : %w", i, err))
			}
			status, err := stepPlugin.Abort(rollout)
			if err != nil {
				return spc.handleError(c, fmt.Errorf("failed to abort plugin: %w", err))
			}
			spc.updateStepPluginStatus(status)
		}
		return nil
	}

	// On full promotion, we want to Terminate the last step stuck in Running
	// At this point, the currentStepIndex is the current or last one
	if rollout.Status.PromoteFull || rolloututil.IsFullyPromoted(rollout) {
		_, currentStepIndex := replicasetutil.GetCurrentCanaryStep(rollout)
		startingIndex := *currentStepIndex

		for i := startingIndex; i >= 0; i-- {
			if i >= int32(len(rollout.Spec.Strategy.Canary.Steps)) {
				continue
			}
			currentStep := &rollout.Spec.Strategy.Canary.Steps[i]
			if currentStep.Plugin == nil {
				continue
			}
			runningStatus := spc.findCurrentStepStatus(i, v1alpha1.StepPluginOperationRun)
			if runningStatus == nil || runningStatus.Phase != v1alpha1.StepPluginPhaseRunning {
				continue
			}

			// found the last running step
			stepPlugin, err := spc.resolver.Resolve(i, *currentStep.Plugin, c.log)
			if err != nil {
				return spc.handleError(c, fmt.Errorf("could not create step plugin at index %d : %w", *currentStepIndex, err))
			}

			status, err := stepPlugin.Terminate(rollout)
			if err != nil {
				return spc.handleError(c, fmt.Errorf("failed to terminate plugin: %w", err))
			}
			spc.updateStepPluginStatus(status)
			return nil
		}
		return nil
	}

	// Normal execution flow of a step plugin
	currentStep, currentStepIndex := replicasetutil.GetCurrentCanaryStep(rollout)
	if currentStep == nil || currentStep.Plugin == nil {
		return nil
	}

	if rollout.Status.Phase != v1alpha1.RolloutPhaseProgressing {
		spc.log.Debug("Not reconciling step plugin because it is not progressing")
		return nil
	}

	stepPlugin, err := spc.resolver.Resolve(*currentStepIndex, *currentStep.Plugin, c.log)
	if err != nil {
		return spc.handleError(c, fmt.Errorf("could not create step plugin at index %d : %w", *currentStepIndex, err))
	}
	status, err := stepPlugin.Run(rollout)
	if err != nil {
		return spc.handleError(c, fmt.Errorf("failed to run plugin: %w", err))
	}
	spc.updateStepPluginStatus(status)

	if status == nil {
		return nil
	}

	if status.Phase == v1alpha1.StepPluginPhaseRunning || status.Phase == v1alpha1.StepPluginPhaseError {
		backoff, err := status.Backoff.Duration()
		if err != nil {
			return spc.handleError(c, fmt.Errorf("failed to parse backoff duration: %w", err))
		}

		// Get the remaining time until the backoff + a little buffer
		remaining := time.Until(status.UpdatedAt.Add(backoff)) + (5 * time.Second)
		c.log.Debugf("queueing up rollout in %s because step plugin phase is %s", remaining, status.Phase)
		c.enqueueRolloutAfter(rollout, remaining)
		return nil
	}

	if status.Phase == v1alpha1.StepPluginPhaseFailed {
		c.pauseContext.AddAbort(status.Message)
	}

	return nil
}

// handleError handles any error that should not cause the rollout reconciliation to fail
func (spc *stepPluginContext) handleError(c *rolloutContext, e error) error {
	spc.hasError = true

	msg := fmt.Sprintf(conditions.RolloutReconciliationErrorMessage, e.Error())
	c.recorder.Eventf(c.rollout, record.EventOptions{EventReason: conditions.RolloutReconciliationErrorReason}, msg)

	c.log.Debug("queueing up rollout in 30s because of transient error")
	c.enqueueRolloutAfter(c.rollout, 30*time.Second)

	return nil
}

func (spc *stepPluginContext) updateStatus(status *v1alpha1.RolloutStatus) {
	if spc.stepPluginStatuses != nil {
		status.Canary.StepPluginStatuses = spc.stepPluginStatuses
	}
}

func (spc *stepPluginContext) isStepPluginDisabled(stepIndex int32, step *v1alpha1.PluginStep) (bool, error) {
	stepPlugin, err := spc.resolver.Resolve(stepIndex, *step, spc.log)
	if err != nil {
		return false, err
	}
	return !stepPlugin.Enabled(), nil
}

func (spc *stepPluginContext) isStepPluginCompleted(stepIndex int32, step *v1alpha1.PluginStep) bool {
	if spc.hasError {
		// If there was a transient error during the reconcile, we should retry
		return false
	}

	if disabled, err := spc.isStepPluginDisabled(stepIndex, step); err != nil {
		// If there is an error, the plugin might not exist in the config. Assume it is not disabled.
		spc.log.Errorf("cannot resolve step plugin %s at index %d. Assuming it is enabled.", step.Name, stepIndex)
	} else if disabled {
		return true
	}

	runStatus := spc.findCurrentStepStatus(stepIndex, v1alpha1.StepPluginOperationRun)
	isRunning := runStatus != nil && runStatus.Phase == v1alpha1.StepPluginPhaseRunning
	if isRunning {
		terminateStatus := spc.findCurrentStepStatus(stepIndex, v1alpha1.StepPluginOperationTerminate)
		abortStatus := spc.findCurrentStepStatus(stepIndex, v1alpha1.StepPluginOperationAbort)
		isRunning = terminateStatus == nil && abortStatus == nil
	}
	return runStatus != nil &&
		((!isRunning && runStatus.Phase == v1alpha1.StepPluginPhaseRunning) ||
			runStatus.Phase == v1alpha1.StepPluginPhaseFailed ||
			runStatus.Phase == v1alpha1.StepPluginPhaseSuccessful)
}

func (spc *stepPluginContext) findCurrentStepStatus(stepIndex int32, operation v1alpha1.StepPluginOperation) *v1alpha1.StepPluginStatus {
	for _, s := range spc.stepPluginStatuses {
		if s.Index == stepIndex && s.Operation == operation {
			return &s
		}
	}
	return nil
}

func (spc *stepPluginContext) updateStepPluginStatus(status *v1alpha1.StepPluginStatus) {
	if status == nil {
		return
	}

	// Update new status and preserve order
	statusUpdated := false
	for i, s := range spc.stepPluginStatuses {
		if !statusUpdated && s.Index == status.Index && s.Operation == status.Operation {
			spc.stepPluginStatuses[i] = *status
			statusUpdated = true
			break
		}
	}
	if !statusUpdated {
		spc.stepPluginStatuses = append(spc.stepPluginStatuses, *status)
	}
}
