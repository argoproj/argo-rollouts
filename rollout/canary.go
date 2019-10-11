package rollout

import (
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	experimentutil "github.com/argoproj/argo-rollouts/utils/experiment"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

func (c *RolloutController) rolloutCanary(rollout *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet) error {
	logCtx := logutil.WithRollout(rollout)
	exList, err := c.getExperimentsForRollout(rollout)
	if err != nil {
		return err
	}
	currentEx := experimentutil.GetCurrentExperiment(rollout, exList)
	otherExs := experimentutil.GetOldExperiments(rollout, exList)

	arList, err := c.getAnalysisRunsForRollout(rollout)
	if err != nil {
		return err
	}
	currentArs, otherArs := analysisutil.FilterCurrentRolloutAnalysisRuns(arList, rollout)

	newRS := replicasetutil.FindNewReplicaSet(rollout, rsList)
	if replicasetutil.PodTemplateOrStepsChanged(rollout, newRS) {
		newRS, previousRSs, err := c.getAllReplicaSetsAndSyncRevision(rollout, rsList, false)
		if err != nil {
			return err
		}
		stableRS, oldRSs := replicasetutil.GetStableRS(rollout, newRS, previousRSs)
		return c.syncRolloutStatusCanary(oldRSs, newRS, stableRS, currentEx, currentArs, rollout)
	}

	newRS, previousRSs, err := c.getAllReplicaSetsAndSyncRevision(rollout, rsList, true)
	if err != nil {
		return err
	}
	stableRS, oldRSs := replicasetutil.GetStableRS(rollout, newRS, previousRSs)

	logCtx.Info("Cleaning up old replicasets")
	if err := c.cleanupRollouts(oldRSs, rollout); err != nil {
		return err
	}

	if err := c.reconcileCanaryService(rollout, newRS); err != nil {
		return err
	}

	logCtx.Info("Reconciling Experiment step")
	currentEx, err = c.reconcileExperiments(rollout, stableRS, newRS, currentEx, otherExs)
	if err != nil {
		return err
	}

	logCtx.Info("Reconciling AnalysisRun step")
	currentArs, err = c.reconcileAnalysisRuns(rollout, currentArs, otherArs, stableRS, newRS)
	if err != nil {
		return err
	}

	noScalingOccured, err := c.reconcileCanaryReplicaSets(rollout, newRS, stableRS, oldRSs)
	if err != nil {
		return err
	}
	if noScalingOccured {
		logCtx.Info("Not finished reconciling ReplicaSets")
		return c.syncRolloutStatusCanary(oldRSs, newRS, stableRS, currentEx, currentArs, rollout)
	}

	logCtx.Info("Reconciling Canary Pause")
	stillReconciling := c.reconcileCanaryPause(rollout)
	if stillReconciling {
		logCtx.Infof("Not finished reconciling Canary Pause")
		return c.syncRolloutStatusCanary(oldRSs, newRS, stableRS, currentEx, currentArs, rollout)
	}

	return c.syncRolloutStatusCanary(oldRSs, newRS, stableRS, currentEx, currentArs, rollout)
}

func (c *RolloutController) reconcileStableRS(olderRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, stableRS *appsv1.ReplicaSet, rollout *v1alpha1.Rollout) (bool, error) {
	logCtx := logutil.WithRollout(rollout)
	if !replicasetutil.CheckStableRSExists(newRS, stableRS) {
		logCtx.Info("No StableRS exists to reconcile or matches newRS")
		return false, nil
	}
	_, stableRSReplicaCount := replicasetutil.CalculateReplicaCountsForCanary(rollout, newRS, stableRS, olderRSs)
	scaled, _, err := c.scaleReplicaSetAndRecordEvent(stableRS, stableRSReplicaCount, rollout)
	return scaled, err
}

func (c *RolloutController) reconcileCanaryPause(rollout *v1alpha1.Rollout) bool {
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

func (c *RolloutController) reconcileOldReplicaSetsCanary(allRSs []*appsv1.ReplicaSet, oldRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, rollout *v1alpha1.Rollout) (bool, error) {
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
func (c *RolloutController) scaleDownOldReplicaSetsForCanary(allRSs []*appsv1.ReplicaSet, oldRSs []*appsv1.ReplicaSet, rollout *v1alpha1.Rollout) (int32, error) {
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

func completedCurrentCanaryStep(olderRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, stableRS *appsv1.ReplicaSet, experiment *v1alpha1.Experiment, currentStepAr *v1alpha1.AnalysisRun, r *v1alpha1.Rollout) bool {
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
	if currentStep.Experiment != nil && experiment != nil && conditions.ExperimentCompleted(experiment.Status) && !conditions.ExperimentTimeOut(experiment, experiment.Status) {
		return true
	}
	analysisExistsAndCompleted := currentStepAr != nil && currentStepAr.Status != nil && currentStepAr.Status.Status.Completed()
	if currentStep.Analysis != nil && analysisExistsAndCompleted && currentStepAr.Status.Status == v1alpha1.AnalysisStatusSuccessful {
		return true
	}

	return false
}

func (c *RolloutController) syncRolloutStatusCanary(olderRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, stableRS *appsv1.ReplicaSet, currExp *v1alpha1.Experiment, currArs []*v1alpha1.AnalysisRun, r *v1alpha1.Rollout) error {
	logCtx := logutil.WithRollout(r)
	allRSs := append(olderRSs, newRS)
	if replicasetutil.CheckStableRSExists(newRS, stableRS) {
		allRSs = append(allRSs, stableRS)
	}
	newStatus := c.calculateBaseStatus(allRSs, newRS, r)
	newStatus.AvailableReplicas = replicasetutil.GetAvailableReplicaCountForReplicaSets(allRSs)
	newStatus.HPAReplicas = replicasetutil.GetActualReplicaCountForReplicaSets(allRSs)
	newStatus.Selector = metav1.FormatLabelSelector(r.Spec.Selector)

	currentStep, currentStepIndex := replicasetutil.GetCurrentCanaryStep(r)
	newStatus.Canary.StableRS = r.Status.Canary.StableRS
	newStatus.CurrentStepHash = conditions.ComputeStepHash(r)
	stepCount := int32(len(r.Spec.Strategy.CanaryStrategy.Steps))

	if replicasetutil.PodTemplateOrStepsChanged(r, newRS) {
		newStatus.CurrentStepIndex = replicasetutil.ResetCurrentStepIndex(r)
		if newRS != nil && r.Status.Canary.StableRS == replicasetutil.GetPodTemplateHash(newRS) {
			if newStatus.CurrentStepIndex != nil {
				msg := "Skipping all steps because the newRS is the stableRS."
				logCtx.Info(msg)
				newStatus.CurrentStepIndex = pointer.Int32Ptr(stepCount)
				c.recorder.Event(r, corev1.EventTypeNormal, "SkipSteps", msg)
			}
		}
		newStatus = c.calculateRolloutConditions(r, newStatus, allRSs, newRS, currExp, currArs)
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
		newStatus = c.calculateRolloutConditions(r, newStatus, allRSs, newRS, currExp, currArs)
		return c.persistRolloutStatus(r, &newStatus, pointer.BoolPtr(false))
	}

	currStepAr := analysisutil.GetCurrentStepAnalysisRun(currArs)
	if currStepAr != nil {
		if currStepAr.Status == nil || !currStepAr.Status.Status.Completed() || analysisutil.IsTerminating(currStepAr) {
			newStatus.Canary.CurrentStepAnalysisRun = currStepAr.Name
		}

	}
	currBackgroundAr := analysisutil.GetCurrentBackgroundAnalysisRun(currArs)
	if currBackgroundAr != nil {
		if currBackgroundAr.Status == nil || !currBackgroundAr.Status.Status.Completed() || analysisutil.IsTerminating(currBackgroundAr) {
			newStatus.Canary.CurrentBackgroundAnalysisRun = currBackgroundAr.Name
		}

	}

	if !r.Spec.Paused {
		if stepCount == 0 {
			logCtx.Info("Rollout has no steps")
			if newRS != nil && newRS.Status.AvailableReplicas == defaults.GetRolloutReplicasOrDefault(r) {
				logCtx.Info("New RS has successfully progressed")
				newStatus.Canary.StableRS = newStatus.CurrentPodHash
			}
			newStatus = c.calculateRolloutConditions(r, newStatus, allRSs, newRS, currExp, currArs)
			return c.persistRolloutStatus(r, &newStatus, pointer.BoolPtr(false))
		}

		if *currentStepIndex == stepCount {
			logCtx.Info("Rollout has executed every step")
			newStatus.CurrentStepIndex = &stepCount
			if newRS != nil && newRS.Status.AvailableReplicas == defaults.GetRolloutReplicasOrDefault(r) {
				logCtx.Info("New RS has successfully progressed")
				newStatus.Canary.StableRS = newStatus.CurrentPodHash
			}
			newStatus = c.calculateRolloutConditions(r, newStatus, allRSs, newRS, currExp, currArs)
			return c.persistRolloutStatus(r, &newStatus, pointer.BoolPtr(false))
		}

		if completedCurrentCanaryStep(olderRSs, newRS, stableRS, currExp, currStepAr, r) {
			*currentStepIndex++
			newStatus.CurrentStepIndex = currentStepIndex
			if int(*currentStepIndex) == len(r.Spec.Strategy.CanaryStrategy.Steps) {
				c.recorder.Event(r, corev1.EventTypeNormal, "SettingStableRS", "Completed all steps")
			}
			logCtx.Infof("Incrementing the Current Step Index to %d", *currentStepIndex)
			c.recorder.Eventf(r, corev1.EventTypeNormal, "SetStepIndex", "Set Step Index to %d", int(*currentStepIndex))
			newStatus = c.calculateRolloutConditions(r, newStatus, allRSs, newRS, currExp, currArs)
			return c.persistRolloutStatus(r, &newStatus, pointer.BoolPtr(false))
		}
		if currExp != nil {
			newStatus.Canary.CurrentExperiment = currExp.Name
			if conditions.ExperimentTimeOut(currExp, currExp.Status) {
				newStatus.Canary.ExperimentFailed = true
			}
		}
	}

	addPause := currentStep.Pause != nil
	pauseStartTime, paused := calculatePauseStatus(r, newRS, addPause, currArs)
	newStatus.PauseStartTime = pauseStartTime

	newStatus.CurrentStepIndex = currentStepIndex
	newStatus = c.calculateRolloutConditions(r, newStatus, allRSs, newRS, currExp, currArs)
	return c.persistRolloutStatus(r, &newStatus, &paused)
}

func (c *RolloutController) reconcileCanaryReplicaSets(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, stableRS *appsv1.ReplicaSet, olderRSs []*appsv1.ReplicaSet) (bool, error) {
	allRSs := append(olderRSs, newRS)
	if stableRS != nil {
		allRSs = append(allRSs, stableRS)
	}

	logCtx := logutil.WithRollout(rollout)
	logCtx.Info("Reconciling StableRS")
	scaledStableRS, err := c.reconcileStableRS(olderRSs, newRS, stableRS, rollout)
	if err != nil {
		return false, err
	}
	if scaledStableRS {
		logCtx.Infof("Not finished reconciling stableRS")
		return true, nil
	}

	logCtx.Infof("Reconciling new ReplicaSet '%s'", newRS.Name)
	scaledNewRS, err := c.reconcileNewReplicaSet(allRSs, newRS, rollout)
	if err != nil {
		return false, err
	}
	if scaledNewRS {
		logCtx.Infof("Not finished reconciling new ReplicaSet '%s'", newRS.Name)
		return true, nil
	}

	logCtx.Info("Reconciling old replica sets")
	scaledDown, err := c.reconcileOldReplicaSetsCanary(allRSs, controller.FilterActiveReplicaSets(olderRSs), newRS, rollout)
	if err != nil {
		return false, err
	}
	if scaledDown {
		logCtx.Info("Not finished reconciling old replica sets")
		return true, nil
	}
	return false, nil
}
