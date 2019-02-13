package controller

import (
	appsv1 "k8s.io/api/apps/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	canaryutil "github.com/argoproj/argo-rollouts/utils/canary"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

func (c *Controller) rolloutCanary(rollout *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet) error {
	logCtx := logutil.WithRollout(rollout)

	if replicasetutil.CheckPodSpecChange(rollout) {
		logCtx.Info("Pod Spec changed and need to reset CurrentStepIndex")
		return c.syncRolloutStatusCanary(nil, nil, rollout)
	}

	logCtx.Info("Reconciling Canary Step")
	stillReconciling, err := c.reconcileCanarySteps(rollout, rsList)
	if err != nil {
		return c.syncRolloutStatusCanary(nil, nil, rollout)
	}
	if stillReconciling {
		logCtx.Infof("Not finished reconciling new Canary Steps")
		return c.syncRolloutStatusCanary(nil, nil, rollout)
	}
	return c.syncRolloutStatusCanary(nil, nil, rollout)
}

func (c *Controller) reconcileCanarySteps(rollout *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet) (bool, error) {
	logCtx := logutil.WithRollout(rollout)
	if len(rollout.Spec.Strategy.CanaryStrategy.Steps) == 0 {
		return false, nil
	}
	currentStep, currentStepIndex := canaryutil.GetCurrentCanaryStep(rollout)

	if len(rollout.Spec.Strategy.CanaryStrategy.Steps) <= int(currentStepIndex) {
		logCtx.Info("No Steps remain in the canary steps")
		return false, nil
	}

	if currentStep.Pause != nil {
		if rollout.Status.SetPause == nil {
			logCtx.Infof("Pausing the canary for step %d in the canary steps", currentStepIndex)
			boolValue := true
			err := c.PatchSetPause(rollout, &boolValue)
			return true, err
		}

		if rollout.Status.SetPause != nil && !*rollout.Status.SetPause {
			currentStepIndex++
			logCtx.Infof("Status.SetPause is false. Canary is ready for step %d", currentStepIndex)
			err := c.PatchSetPause(rollout, nil)
			return true, err
		}

		return true, nil
	}

	return false, nil
}

func (c *Controller) syncRolloutStatusCanary(allRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, r *v1alpha1.Rollout) error {
	logCtx := logutil.WithRollout(r)
	newStatus := c.calculateBaseStatus(allRSs, newRS, r)

	setPause := r.Status.SetPause

	currentStep, currentStepIndex := canaryutil.GetCurrentCanaryStep(r)
	if replicasetutil.CheckPodSpecChange(r) {
		currentStepIndex = 0
		setPause = nil
	} else if currentStep != nil && currentStep.Pause != nil && r.Status.SetPause != nil && !*r.Status.SetPause {
		currentStepIndex++
		logCtx.Infof("Incrementing the Current Step Index to %d", currentStepIndex)
	}

	newStatus.CurrentStepIndex = &currentStepIndex
	newStatus.SetPause = setPause

	return c.persistRolloutStatus(r, &newStatus)
}
