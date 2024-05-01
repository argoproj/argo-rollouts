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

var (
	defaultControllerErrorBackoff = time.Second * 30
	defaultBackoffDelay           = time.Second * 5
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
		for i := len(spc.stepPluginStatuses) - 1; i >= 0; i-- {
			pluginStatus := spc.stepPluginStatuses[i]
			if pluginStatus.Operation != v1alpha1.StepPluginOperationRun {
				// Only call abort for Run operation.
				continue
			}
			pluginStep := rollout.Spec.Strategy.Canary.Steps[pluginStatus.Index]
			if pluginStep.Plugin == nil {
				continue
			}

			stepPlugin, err := spc.resolver.Resolve(pluginStatus.Index, *pluginStep.Plugin, c.log)
			if err != nil {
				return spc.handleError(c, fmt.Errorf("could not create step plugin at index %d : %w", pluginStatus.Index, err))
			}
			status, err := stepPlugin.Abort(rollout)
			if err != nil {
				return spc.handleError(c, fmt.Errorf("failed to abort plugin: %w", err))
			}
			phaseTransition := spc.updateStepPluginStatus(status)
			if phaseTransition {
				spc.recordPhase(c, status)
			}
		}
		return nil
	}

	// If we retry an aborted rollout, we need to have a clean status
	spc.cleanStatusForRetry(rollout)

	// On full promotion, we want to Terminate only the last step still in Running, if any
	if rollout.Status.PromoteFull || rolloututil.IsFullyPromoted(rollout) {
		stepIndex := spc.getStepToTerminate(rollout)
		if stepIndex == nil {
			return nil
		}

		pluginStep := rollout.Spec.Strategy.Canary.Steps[*stepIndex]
		if pluginStep.Plugin == nil {
			return nil
		}

		stepPlugin, err := spc.resolver.Resolve(*stepIndex, *pluginStep.Plugin, c.log)
		if err != nil {
			return spc.handleError(c, fmt.Errorf("could not create step plugin at index %d : %w", *stepIndex, err))
		}

		status, err := stepPlugin.Terminate(rollout)
		if err != nil {
			return spc.handleError(c, fmt.Errorf("failed to terminate plugin: %w", err))
		}

		phaseTransition := spc.updateStepPluginStatus(status)
		if phaseTransition {
			spc.recordPhase(c, status)
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

	phaseTransition := spc.updateStepPluginStatus(status)
	if phaseTransition {
		spc.recordPhase(c, status)
	}

	if status == nil || status.Disabled {
		return nil
	}

	if status.Phase == v1alpha1.StepPluginPhaseRunning || status.Phase == v1alpha1.StepPluginPhaseError {
		backoff, err := status.Backoff.Duration()
		if err != nil {
			return spc.handleError(c, fmt.Errorf("failed to parse backoff duration: %w", err))
		}

		// Get the remaining time until the backoff + a little buffer
		remaining := time.Until(status.UpdatedAt.Add(backoff)) + defaultBackoffDelay
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
	c.recorder.Warnf(c.rollout, record.EventOptions{EventReason: conditions.RolloutReconciliationErrorReason}, msg)

	c.log.Debugf("queueing up rollout in %s because of transient error", defaultControllerErrorBackoff)
	c.enqueueRolloutAfter(c.rollout, defaultControllerErrorBackoff)

	return nil
}

func (spc *stepPluginContext) recordPhase(c *rolloutContext, status *v1alpha1.StepPluginStatus) {
	if status.Disabled || status.Operation == v1alpha1.StepPluginOperationRun && status.Phase == v1alpha1.StepPluginPhaseSuccessful {
		// If the run status is successful, do not record event because the controller will record the RolloutStepCompleted
		return
	}

	msg := fmt.Sprintf(conditions.StepPluginTransitionRunMessage, status.Name, status.Index+1, status.Phase)
	if status.Operation == v1alpha1.StepPluginOperationAbort {
		msg = fmt.Sprintf(conditions.StepPluginTransitionAbortMessage, status.Name, status.Index+1, status.Phase)
	} else if status.Operation == v1alpha1.StepPluginOperationTerminate {
		msg = fmt.Sprintf(conditions.StepPluginTransitionTerminateMessage, status.Name, status.Index+1, status.Phase)
	}

	if status.Phase == v1alpha1.StepPluginPhaseError || status.Phase == v1alpha1.StepPluginPhaseFailed {
		c.recorder.Warnf(c.rollout, record.EventOptions{EventReason: conditions.StepPluginTransitionReason}, msg)
	} else {
		c.recorder.Eventf(c.rollout, record.EventOptions{EventReason: conditions.StepPluginTransitionReason}, msg)
	}
}

func (spc *stepPluginContext) updateStatus(status *v1alpha1.RolloutStatus) {
	if spc.stepPluginStatuses != nil {
		status.Canary.StepPluginStatuses = spc.stepPluginStatuses
	}
}

func (spc *stepPluginContext) isStepPluginCompleted(stepIndex int32, _ *v1alpha1.PluginStep) bool {
	if spc.hasError {
		// If there was a transient error during the reconcile, we should retry
		return false
	}

	runStatus := spc.findCurrentStepStatus(stepIndex, v1alpha1.StepPluginOperationRun)
	if runStatus != nil && runStatus.Disabled {
		return true
	}

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

func (spc *stepPluginContext) updateStepPluginStatus(status *v1alpha1.StepPluginStatus) bool {
	phaseChanged := false
	if status == nil {
		return phaseChanged
	}

	// Update new status and preserve order
	statusUpdated := false
	for i, s := range spc.stepPluginStatuses {
		if !statusUpdated && s.Index == status.Index && s.Operation == status.Operation {
			spc.stepPluginStatuses[i] = *status
			statusUpdated = true
			phaseChanged = s.Phase != status.Phase
			break
		}
	}
	if !statusUpdated {
		spc.stepPluginStatuses = append(spc.stepPluginStatuses, *status)
		phaseChanged = true
	}

	return phaseChanged
}

func (spc *stepPluginContext) getStepToTerminate(rollout *v1alpha1.Rollout) *int32 {
	for i := len(rollout.Status.Canary.StepPluginStatuses) - 1; i >= 0; i-- {
		pluginStep := rollout.Status.Canary.StepPluginStatuses[i]

		if pluginStep.Operation == v1alpha1.StepPluginOperationTerminate && pluginStep.Phase == v1alpha1.StepPluginPhaseSuccessful {
			// last running step is already terminated
			return nil
		}

		if pluginStep.Operation == v1alpha1.StepPluginOperationRun && pluginStep.Phase == v1alpha1.StepPluginPhaseRunning {
			// found the last running step
			return &pluginStep.Index
		}
	}
	return nil
}

// cleanStatusForRetry is expected to be called on a non-aborted rollout.
// It validates that stepPluginStatuses does not contain outdated results, and remove them
// if it does.
func (spc *stepPluginContext) cleanStatusForRetry(rollout *v1alpha1.Rollout) {
	if len(spc.stepPluginStatuses) == 0 || rollout.Status.Abort {
		return
	}

	currentStep, currentStepIndex := replicasetutil.GetCurrentCanaryStep(rollout)
	if currentStep == nil || int(*currentStepIndex) > 0 {
		// Nothing to clean if rollout steps are completed or in progress
		return
	}

	// if we are at step 0, it could be either that we haven't started, or we are progressing.
	// if step 0 is a plugin, check if it is completed. In that case, we know that we should be at step > 0
	// abd we are retrying
	shouldCleanCurrentStatus := true
	if currentStep.Plugin != nil {
		shouldCleanCurrentStatus = spc.isStepPluginCompleted(*currentStepIndex, currentStep.Plugin)
	}

	if shouldCleanCurrentStatus {
		spc.stepPluginStatuses = []v1alpha1.StepPluginStatus{}
	}
}
