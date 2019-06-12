package controller

import (
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

func (c *Controller) rolloutCanary(rollout *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet) error {
	logCtx := logutil.WithRollout(rollout)

	if podTemplateOrStepsChanged(rollout, rsList) {
		newRS, previousRSs, err := c.getAllReplicaSetsAndSyncRevision(rollout, rsList, false)
		if err != nil {
			return err
		}
		stableRS, oldRSs := replicasetutil.GetStableRS(rollout, newRS, previousRSs)
		return c.syncRolloutStatusCanary(oldRSs, newRS, stableRS, rollout)
	}

	newRS, previousRSs, err := c.getAllReplicaSetsAndSyncRevision(rollout, rsList, true)
	if err != nil {
		return err
	}
	stableRS, oldRSs := replicasetutil.GetStableRS(rollout, newRS, previousRSs)
	allRSs := append(oldRSs, newRS)
	if stableRS != nil {
		allRSs = append(allRSs, stableRS)
	}

	logCtx.Info("Cleaning up old replicasets")
	if err := c.cleanupRollouts(oldRSs, rollout); err != nil {
		return err
	}

	if err := c.reconcileCanaryService(rollout, newRS); err != nil {
		return err
	}

	logCtx.Info("Reconciling StableRS")
	scaledStableRS, err := c.reconcileStableRS(oldRSs, newRS, stableRS, rollout)
	if err != nil {
		return err
	}
	if scaledStableRS {
		logCtx.Infof("Not finished reconciling stableRS")
		return c.syncRolloutStatusCanary(oldRSs, newRS, stableRS, rollout)
	}

	logCtx.Infof("Reconciling new ReplicaSet '%s'", newRS.Name)
	scaledNewRS, err := c.reconcileNewReplicaSet(allRSs, newRS, rollout)
	if err != nil {
		return err
	}
	if scaledNewRS {
		logCtx.Infof("Not finished reconciling new ReplicaSet '%s'", newRS.Name)
		return c.syncRolloutStatusCanary(oldRSs, newRS, stableRS, rollout)
	}

	logCtx.Info("Reconciling old replica sets")
	scaledDown, err := c.reconcileOldReplicaSetsCanary(allRSs, controller.FilterActiveReplicaSets(oldRSs), newRS, rollout)
	if err != nil {
		return err
	}
	if scaledDown {
		logCtx.Info("Not finished reconciling old replica sets")
		return c.syncRolloutStatusCanary(oldRSs, newRS, stableRS, rollout)
	}

	logCtx.Info("Reconciling Canary Pause")
	stillReconciling := c.reconcileCanaryPause(rollout)
	if stillReconciling {
		logCtx.Infof("Not finished reconciling Canary Pause")
		return c.syncRolloutStatusCanary(oldRSs, newRS, stableRS, rollout)
	}

	return c.syncRolloutStatusCanary(oldRSs, newRS, stableRS, rollout)
}

func (c *Controller) reconcileStableRS(olderRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, stableRS *appsv1.ReplicaSet, rollout *v1alpha1.Rollout) (bool, error) {
	logCtx := logutil.WithRollout(rollout)
	if !replicasetutil.CheckStableRSExists(newRS, stableRS) {
		logCtx.Info("No StableRS exists to reconcile")
		return false, nil
	}
	_, stableRSReplicaCount := replicasetutil.CalculateReplicaCountsForCanary(rollout, newRS, stableRS, olderRSs)
	scaled, _, err := c.scaleReplicaSetAndRecordEvent(stableRS, stableRSReplicaCount, rollout)
	return scaled, err
}

func (c *Controller) reconcileCanaryPause(rollout *v1alpha1.Rollout) bool {
	logCtx := logutil.WithRollout(rollout)
	if len(rollout.Spec.Strategy.CanaryStrategy.Steps) == 0 {
		logCtx.Info("Rollout does not have any steps")
		return false
	}
	currentStep, currentStepIndex := replicasetutil.GetCurrentCanaryStep(rollout)

	if len(rollout.Spec.Strategy.CanaryStrategy.Steps) <= int(*currentStepIndex) {
		logCtx.Info("No Steps remain in the canary steps")
		return false
	}

	if currentStep.Pause == nil {
		return false
	}

	if currentStep.Pause.Duration == nil {
		return true
	}
	if rollout.Status.PauseStartTime == nil {
		return true
	}
	c.checkEnqueueRolloutDuringWait(rollout, *rollout.Status.PauseStartTime, *currentStep.Pause.Duration)
	return true
}

func (c *Controller) reconcileOldReplicaSetsCanary(allRSs []*appsv1.ReplicaSet, oldRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, rollout *v1alpha1.Rollout) (bool, error) {
	logCtx := logutil.WithRollout(rollout)
	oldPodsCount := replicasetutil.GetReplicaCountForReplicaSets(oldRSs)
	if oldPodsCount == 0 {
		// Can't scale down further
		return false, nil
	}

	// Clean up unhealthy replicas first, otherwise unhealthy replicas will block rollout
	// and cause timeout. See https://github.com/kubernetes/kubernetes/issues/16737
	oldRSs, cleanupCount, err := c.cleanupUnhealthyReplicas(oldRSs, rollout)
	if err != nil {
		return false, nil
	}
	logCtx.Infof("Cleaned up unhealthy replicas from old RSes by %d", cleanupCount)

	// Scale down old replica sets, need check replicasToKeep to ensure we can scale down
	scaledDownCount, err := c.scaleDownOldReplicaSetsForCanary(allRSs, oldRSs, rollout)
	if err != nil {
		return false, nil
	}
	logCtx.Infof("Scaled down old RSes by %d", scaledDownCount)

	totalScaledDown := cleanupCount + scaledDownCount
	return totalScaledDown > 0, nil
}

// scaleDownOldReplicaSetsForBlueGreen scales down old replica sets when rollout strategy is "Blue Green".
func (c *Controller) scaleDownOldReplicaSetsForCanary(allRSs []*appsv1.ReplicaSet, oldRSs []*appsv1.ReplicaSet, rollout *v1alpha1.Rollout) (int32, error) {
	availablePodCount := replicasetutil.GetAvailableReplicaCountForReplicaSets(allRSs)
	minAvailable := defaults.GetRolloutReplicasOrDefault(rollout) - replicasetutil.MaxUnavailable(rollout)
	maxScaleDown := availablePodCount - minAvailable
	if maxScaleDown <= 0 {
		// Cannot scale down.
		return 0, nil
	}
	logutil.WithRollout(rollout).Infof("Found %d available pods, scaling down old RSes", availablePodCount)

	sort.Sort(controller.ReplicaSetsByCreationTimestamp(oldRSs))

	totalScaledDown := int32(0)
	for _, targetRS := range oldRSs {
		if maxScaleDown <= 0 {
			break
		}
		if *(targetRS.Spec.Replicas) == 0 {
			// cannot scale down this ReplicaSet.
			continue
		}
		scaleDownCount := *(targetRS.Spec.Replicas)
		// Scale down.
		newReplicasCount := int32(0)
		if scaleDownCount > maxScaleDown {
			newReplicasCount = maxScaleDown
		}
		_, _, err := c.scaleReplicaSetAndRecordEvent(targetRS, newReplicasCount, rollout)
		if err != nil {
			return totalScaledDown, err
		}
		maxScaleDown -= newReplicasCount
		totalScaledDown += scaleDownCount
	}

	return totalScaledDown, nil
}

func completedCurrentCanaryStep(olderRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, stableRS *appsv1.ReplicaSet, r *v1alpha1.Rollout) bool {
	logCtx := logutil.WithRollout(r)
	currentStep, _ := replicasetutil.GetCurrentCanaryStep(r)
	if currentStep == nil {
		return false
	}
	if currentStep.Pause != nil {
		return completedPauseStep(r, currentStep.Pause)
	}
	if currentStep.SetWeight != nil && replicasetutil.AtDesiredReplicaCountsForCanary(r, newRS, stableRS, olderRSs) {
		logCtx.Info("Rollout has reached the desired state for the correct weight")
		return true
	}
	return false
}

func (c *Controller) syncRolloutStatusCanary(olderRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, stableRS *appsv1.ReplicaSet, r *v1alpha1.Rollout) error {
	logCtx := logutil.WithRollout(r)
	allRSs := append(olderRSs, newRS)
	if replicasetutil.CheckStableRSExists(newRS, stableRS) {
		allRSs = append(allRSs, stableRS)
	}
	newStatus := c.calculateBaseStatus(allRSs, newRS, r)
	newStatus.HPAReplicas = replicasetutil.GetActualReplicaCountForReplicaSets(allRSs)
	newStatus.Selector = metav1.FormatLabelSelector(r.Spec.Selector)

	currentStep, currentStepIndex := replicasetutil.GetCurrentCanaryStep(r)
	newStatus.Canary.StableRS = r.Status.Canary.StableRS
	newStatus.CurrentStepHash = conditions.ComputeStepHash(r)
	stepCount := int32(len(r.Spec.Strategy.CanaryStrategy.Steps))

	if podTemplateOrStepsChanged(r, allRSs) {
		newStatus.CurrentStepIndex = resetCurrentStepIndex(r)
		if r.Status.Canary.StableRS == replicasetutil.GetPodTemplateHash(newRS) {
			if newStatus.CurrentStepIndex != nil {
				msg := "Skipping all steps because the newRS is the stableRS."
				logCtx.Info(msg)
				newStatus.CurrentStepIndex = pointer.Int32Ptr(stepCount)
				c.recorder.Event(r, corev1.EventTypeNormal, "SkipSteps", msg)
			}
		}
		newStatus = c.calculateRolloutConditions(r, newStatus, allRSs, newRS)
		return c.persistRolloutStatus(r, &newStatus, pointer.BoolPtr(false))
	}

	if r.Status.Canary.StableRS == "" {
		msg := "Setting StableRS to CurrentPodHash as it is empty beforehand"
		logCtx.Info(msg)
		newStatus.Canary.StableRS = newStatus.CurrentPodHash
		if stepCount > 0 {
			if stepCount != *currentStepIndex {
				c.recorder.Event(r, corev1.EventTypeNormal, "SettingStableRS", msg)
			}
			newStatus.CurrentStepIndex = &stepCount

		}
		newStatus = c.calculateRolloutConditions(r, newStatus, allRSs, newRS)
		return c.persistRolloutStatus(r, &newStatus, pointer.BoolPtr(false))
	}

	if stepCount == 0 {
		logCtx.Info("Rollout has no steps so setting stableRS status to currentPodHash")
		newStatus.Canary.StableRS = newStatus.CurrentPodHash
		newStatus = c.calculateRolloutConditions(r, newStatus, allRSs, newRS)
		return c.persistRolloutStatus(r, &newStatus, pointer.BoolPtr(false))
	}

	if *currentStepIndex == stepCount {
		logCtx.Info("Rollout has executed every step")
		newStatus.CurrentStepIndex = &stepCount
		newStatus.Canary.StableRS = newStatus.CurrentPodHash
		newStatus = c.calculateRolloutConditions(r, newStatus, allRSs, newRS)
		return c.persistRolloutStatus(r, &newStatus, pointer.BoolPtr(false))
	}

	if completedCurrentCanaryStep(olderRSs, newRS, stableRS, r) {
		*currentStepIndex++
		newStatus.CurrentStepIndex = currentStepIndex
		if int(*currentStepIndex) == len(r.Spec.Strategy.CanaryStrategy.Steps) {
			c.recorder.Event(r, corev1.EventTypeNormal, "SettingStableRS", "Completed all steps")
			newStatus.Canary.StableRS = newStatus.CurrentPodHash
		}
		logCtx.Infof("Incrementing the Current Step Index to %d", *currentStepIndex)
		c.recorder.Eventf(r, corev1.EventTypeNormal, "SetStepIndex", "Set Step Index to %d", int(*currentStepIndex))
		newStatus = c.calculateRolloutConditions(r, newStatus, allRSs, newRS)
		return c.persistRolloutStatus(r, &newStatus, pointer.BoolPtr(false))
	}

	addPause := currentStep.Pause != nil
	pauseStartTime, paused := calculatePauseStatus(r, newRS, addPause)
	newStatus.PauseStartTime = pauseStartTime

	newStatus.CurrentStepIndex = currentStepIndex
	newStatus = c.calculateRolloutConditions(r, newStatus, allRSs, newRS)
	return c.persistRolloutStatus(r, &newStatus, &paused)
}

// checkStepHashChange indicates if the rollout's step for the strategy have changed. This causes the rollout to reset the
// currentStepIndex to zero. If there is no previous pod spec to compare to the function defaults to false
func checkStepHashChange(rollout *v1alpha1.Rollout) bool {
	if rollout.Status.CurrentStepHash == "" {
		return false
	}
	// TODO: conditions.ComputeStepHash is not stable and will change
	stepsHash := conditions.ComputeStepHash(rollout)
	if rollout.Status.CurrentStepHash != conditions.ComputeStepHash(rollout) {
		logCtx := logutil.WithRollout(rollout)
		logCtx.Infof("Canary steps change detected (new: %s, old: %s)", stepsHash, rollout.Status.CurrentStepHash)
		return true
	}
	return false
}

// checkPodSpecChange indicates if the rollout spec has changed indicating that the rollout needs to reset the
// currentStepIndex to zero. If there is no previous pod spec to compare to the function defaults to false
func checkPodSpecChange(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet) bool {
	if rollout.Status.CurrentPodHash == "" {
		return false
	}
	podHash := replicasetutil.GetPodTemplateHash(newRS)
	if rollout.Status.CurrentPodHash != podHash {
		logCtx := logutil.WithRollout(rollout)
		logCtx.Infof("Pod template change detected (new: %s, old: %s)", podHash, rollout.Status.CurrentPodHash)
		return true
	}
	return false
}

// podTemplateOrStepsChanged detects if there is a change in either the pod template, or canary steps
func podTemplateOrStepsChanged(rollout *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet) bool {
	newRS := replicasetutil.FindNewReplicaSet(rollout, rsList)
	if newRS == nil {
		return false
	}
	if checkPodSpecChange(rollout, newRS) {
		return true
	}
	if checkStepHashChange(rollout) {
		return true
	}
	return false
}

// resetCurrentStepIndex resets the index back to zero unless there are no steps
func resetCurrentStepIndex(rollout *v1alpha1.Rollout) *int32 {
	if len(rollout.Spec.Strategy.CanaryStrategy.Steps) > 0 {
		return pointer.Int32Ptr(0)
	}
	return nil
}
