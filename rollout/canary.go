package rollout

import (
	"fmt"
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
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

func (c *RolloutController) rolloutCanary(rollout *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet) error {
	exList, err := c.getExperimentsForRollout(rollout)
	if err != nil {
		return err
	}

	arList, err := c.getAnalysisRunsForRollout(rollout)
	if err != nil {
		return err
	}

	newRS := replicasetutil.FindNewReplicaSet(rollout, rsList)
	if replicasetutil.PodTemplateOrStepsChanged(rollout, newRS) {
		newRS, previousRSs, err := c.getAllReplicaSetsAndSyncRevision(rollout, rsList, false)
		if err != nil {
			return err
		}
		roCtx := newCanaryCtx(rollout, newRS, previousRSs, exList, arList)
		return c.syncRolloutStatusCanary(roCtx)
	}

	newRS, previousRSs, err := c.getAllReplicaSetsAndSyncRevision(rollout, rsList, true)
	if err != nil {
		return err
	}

	roCtx := newCanaryCtx(rollout, newRS, previousRSs, exList, arList)
	logCtx := roCtx.Log()

	logCtx.Info("Cleaning up old replicasets, experiments, and analysis runs")
	if err := c.cleanupRollouts(roCtx.OlderRSs(), roCtx); err != nil {
		return err
	}

	if err := c.reconcileStableAndCanaryService(roCtx); err != nil {
		return err
	}

	if err := c.reconcileNetworking(roCtx); err != nil {
		return err
	}

	logCtx.Info("Reconciling Experiment step")
	err = c.reconcileExperiments(roCtx)
	if err != nil {
		return err
	}

	logCtx.Info("Reconciling AnalysisRun step")
	err = c.reconcileAnalysisRuns(roCtx)
	if roCtx.PauseContext().HasAddPause() {
		logCtx.Info("Detected pause due to inconclusive AnalysisRun")
		return c.syncRolloutStatusCanary(roCtx)
	}
	if err != nil {
		return err
	}

	noScalingOccured, err := c.reconcileCanaryReplicaSets(roCtx)
	if err != nil {
		return err
	}
	if noScalingOccured {
		logCtx.Info("Not finished reconciling ReplicaSets")
		return c.syncRolloutStatusCanary(roCtx)
	}

	logCtx.Info("Reconciling Canary Pause")
	stillReconciling := c.reconcileCanaryPause(roCtx)
	if stillReconciling {
		logCtx.Infof("Not finished reconciling Canary Pause")
		return c.syncRolloutStatusCanary(roCtx)
	}

	return c.syncRolloutStatusCanary(roCtx)
}

func (c *RolloutController) reconcileStableRS(roCtx *canaryContext) (bool, error) {
	logCtx := roCtx.Log()
	rollout := roCtx.Rollout()
	newRS := roCtx.NewRS()
	stableRS := roCtx.StableRS()
	olderRSs := roCtx.OlderRSs()
	if !replicasetutil.CheckStableRSExists(newRS, stableRS) {
		logCtx.Info("No StableRS exists to reconcile or matches newRS")
		return false, nil
	}
	_, stableRSReplicaCount := replicasetutil.CalculateReplicaCountsForCanary(rollout, newRS, stableRS, olderRSs)
	scaled, _, err := c.scaleReplicaSetAndRecordEvent(stableRS, stableRSReplicaCount, rollout)
	return scaled, err
}

func (c *RolloutController) reconcileCanaryPause(roCtx *canaryContext) bool {
	rollout := roCtx.Rollout()
	logCtx := roCtx.Log()

	if rollout.Spec.Paused {
		return false
	}

	if len(rollout.Spec.Strategy.Canary.Steps) == 0 {
		logCtx.Info("Rollout does not have any steps")
		return false
	}
	currentStep, currentStepIndex := replicasetutil.GetCurrentCanaryStep(rollout)

	if len(rollout.Spec.Strategy.Canary.Steps) <= int(*currentStepIndex) {
		logCtx.Info("No Steps remain in the canary steps")
		return false
	}

	if currentStep.Pause == nil {
		return false
	}
	cond := roCtx.PauseContext().GetPauseCondition(v1alpha1.PauseReasonCanaryPauseStep)
	if cond == nil {
		// When the pause condition is null, that means the rollout is in an not paused state.
		// As a result,, the controller needs to detect whether a rollout was unpaused or the
		// rollout needs to be paused for the first time. If the ControllerPause is false,
		// the controller has not paused the rollout yet and needs to do so before it
		// can proceed.
		if !rollout.Status.ControllerPause {
			roCtx.PauseContext().AddPauseCondition(v1alpha1.PauseReasonCanaryPauseStep)
		}
		return true
	}
	if currentStep.Pause.Duration == nil {
		return true
	}
	c.checkEnqueueRolloutDuringWait(rollout, cond.StartTime, *currentStep.Pause.Duration)
	return true
}

func (c *RolloutController) reconcileOldReplicaSetsCanary(allRSs []*appsv1.ReplicaSet, oldRSs []*appsv1.ReplicaSet, roCtx *canaryContext) (bool, error) {
	rollout := roCtx.Rollout()
	logCtx := roCtx.Log()
	oldPodsCount := replicasetutil.GetReplicaCountForReplicaSets(oldRSs)
	if oldPodsCount == 0 {
		// Can't scale down further
		return false, nil
	}

	// Clean up unhealthy replicas first, otherwise unhealthy replicas will block rollout
	// and cause timeout. See https://github.com/kubernetes/kubernetes/issues/16737
	oldRSs, cleanupCount, err := c.cleanupUnhealthyReplicas(oldRSs, roCtx)
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

// scaleDownOldReplicaSetsForCanary scales down old replica sets when rollout strategy is "canary".
func (c *RolloutController) scaleDownOldReplicaSetsForCanary(allRSs []*appsv1.ReplicaSet, oldRSs []*appsv1.ReplicaSet, rollout *v1alpha1.Rollout) (int32, error) {
	logCtx := logutil.WithRollout(rollout)
	availablePodCount := replicasetutil.GetAvailableReplicaCountForReplicaSets(allRSs)
	minAvailable := defaults.GetReplicasOrDefault(rollout.Spec.Replicas) - replicasetutil.MaxUnavailable(rollout)
	maxScaleDown := availablePodCount - minAvailable
	if maxScaleDown <= 0 {
		// Cannot scale down.
		return 0, nil
	}
	logCtx.Infof("Found %d available pods, scaling down old RSes", availablePodCount)

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

func completedCurrentCanaryStep(roCtx *canaryContext) bool {
	r := roCtx.Rollout()
	if r.Spec.Paused {
		return false
	}
	logCtx := roCtx.Log()
	currentStep, _ := replicasetutil.GetCurrentCanaryStep(r)
	if currentStep == nil {
		return false
	}
	if currentStep.Pause != nil {
		return roCtx.PauseContext().CompletedPauseStep(*currentStep.Pause)
	}
	if currentStep.SetWeight != nil && replicasetutil.AtDesiredReplicaCountsForCanary(r, roCtx.NewRS(), roCtx.StableRS(), roCtx.OlderRSs()) {
		logCtx.Info("Rollout has reached the desired state for the correct weight")
		return true
	}
	experiment := roCtx.CurrentExperiment()
	if currentStep.Experiment != nil && experiment != nil && experiment.Status.Phase.Completed() && experiment.Status.Phase == v1alpha1.AnalysisPhaseSuccessful {
		return true
	}
	currentArs := roCtx.CurrentAnalysisRuns()
	currentStepAr := analysisutil.GetCurrentStepAnalysisRun(currentArs)
	analysisExistsAndCompleted := currentStepAr != nil && currentStepAr.Status.Phase.Completed()
	if currentStep.Analysis != nil && analysisExistsAndCompleted && currentStepAr.Status.Phase == v1alpha1.AnalysisPhaseSuccessful {
		return true
	}

	return false
}

func (c *RolloutController) syncRolloutStatusCanary(roCtx *canaryContext) error {
	r := roCtx.Rollout()
	logCtx := roCtx.Log()
	newRS := roCtx.NewRS()
	stableRS := roCtx.StableRS()
	allRSs := roCtx.AllRSs()

	newStatus := c.calculateBaseStatus(roCtx)
	newStatus.AvailableReplicas = replicasetutil.GetAvailableReplicaCountForReplicaSets(allRSs)
	newStatus.HPAReplicas = replicasetutil.GetActualReplicaCountForReplicaSets(allRSs)
	newStatus.Selector = metav1.FormatLabelSelector(r.Spec.Selector)

	_, currentStepIndex := replicasetutil.GetCurrentCanaryStep(r)
	newStatus.Canary.StableRS = r.Status.Canary.StableRS
	newStatus.CurrentStepHash = conditions.ComputeStepHash(r)
	stepCount := int32(len(r.Spec.Strategy.Canary.Steps))

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
		roCtx.PauseContext().ClearPauseConditions()
		roCtx.PauseContext().RemoveAbort()
		newStatus = c.calculateRolloutConditions(roCtx, newStatus)
		return c.persistRolloutStatus(roCtx, &newStatus)
	}

	if stableRS == nil {
		msg := fmt.Sprintf("Setting StableRS to CurrentPodHash: StableRS hash: %s", newStatus.CurrentPodHash)
		logCtx.Info(msg)
		newStatus.Canary.StableRS = newStatus.CurrentPodHash
		if stepCount > 0 {
			if stepCount != *currentStepIndex {
				c.recorder.Event(r, corev1.EventTypeNormal, "SettingStableRS", msg)
			}
			newStatus.CurrentStepIndex = &stepCount

		}
		roCtx.PauseContext().ClearPauseConditions()
		roCtx.PauseContext().RemoveAbort()
		newStatus = c.calculateRolloutConditions(roCtx, newStatus)
		return c.persistRolloutStatus(roCtx, &newStatus)
	}

	if roCtx.PauseContext().IsAborted() {
		if stepCount > int32(0) {
			if newStatus.Canary.StableRS == newStatus.CurrentPodHash {
				newStatus.CurrentStepIndex = &stepCount
			} else {
				newStatus.CurrentStepIndex = pointer.Int32Ptr(0)
			}
		}
		newStatus = c.calculateRolloutConditions(roCtx, newStatus)
		return c.persistRolloutStatus(roCtx, &newStatus)
	}

	if currentStepIndex != nil && *currentStepIndex == stepCount {
		logCtx.Info("Rollout has executed every step")
		newStatus.CurrentStepIndex = &stepCount
		if newRS != nil && newRS.Status.AvailableReplicas == defaults.GetReplicasOrDefault(r.Spec.Replicas) {
			logCtx.Info("New RS has successfully progressed")
			newStatus.Canary.StableRS = newStatus.CurrentPodHash
		}
		roCtx.PauseContext().ClearPauseConditions()
		newStatus = c.calculateRolloutConditions(roCtx, newStatus)
		return c.persistRolloutStatus(roCtx, &newStatus)
	}

	if stepCount == 0 {
		logCtx.Info("Rollout has no steps")
		if newRS != nil && newRS.Status.AvailableReplicas == defaults.GetReplicasOrDefault(r.Spec.Replicas) {
			logCtx.Info("New RS has successfully progressed")
			newStatus.Canary.StableRS = newStatus.CurrentPodHash
		}
		newStatus = c.calculateRolloutConditions(roCtx, newStatus)
		return c.persistRolloutStatus(roCtx, &newStatus)
	}

	if completedCurrentCanaryStep(roCtx) {
		*currentStepIndex++
		newStatus.CurrentStepIndex = currentStepIndex
		newStatus.Canary.CurrentStepAnalysisRun = ""
		if int(*currentStepIndex) == len(r.Spec.Strategy.Canary.Steps) {
			c.recorder.Event(r, corev1.EventTypeNormal, "SettingStableRS", "Completed all steps")
		}
		logCtx.Infof("Incrementing the Current Step Index to %d", *currentStepIndex)
		c.recorder.Eventf(r, corev1.EventTypeNormal, "SetStepIndex", "Set Step Index to %d", int(*currentStepIndex))
		roCtx.PauseContext().RemovePauseCondition(v1alpha1.PauseReasonCanaryPauseStep)
		newStatus = c.calculateRolloutConditions(roCtx, newStatus)
		return c.persistRolloutStatus(roCtx, &newStatus)
	}

	newStatus.CurrentStepIndex = currentStepIndex
	newStatus = c.calculateRolloutConditions(roCtx, newStatus)
	return c.persistRolloutStatus(roCtx, &newStatus)
}

func (c *RolloutController) reconcileCanaryReplicaSets(roCtx *canaryContext) (bool, error) {
	logCtx := roCtx.Log()
	logCtx.Info("Reconciling StableRS")
	scaledStableRS, err := c.reconcileStableRS(roCtx)
	if err != nil {
		return false, err
	}
	if scaledStableRS {
		logCtx.Infof("Not finished reconciling stableRS")
		return true, nil
	}

	newRS := roCtx.NewRS()
	olderRSs := roCtx.OlderRSs()
	allRSs := roCtx.AllRSs()
	scaledNewRS, err := c.reconcileNewReplicaSet(roCtx)
	if err != nil {
		return false, err
	}
	if scaledNewRS {
		logCtx.Infof("Not finished reconciling new ReplicaSet '%s'", newRS.Name)
		return true, nil
	}

	logCtx.Info("Reconciling old replica sets")
	scaledDown, err := c.reconcileOldReplicaSetsCanary(allRSs, controller.FilterActiveReplicaSets(olderRSs), roCtx)
	if err != nil {
		return false, err
	}
	if scaledDown {
		logCtx.Info("Not finished reconciling old replica sets")
		return true, nil
	}
	return false, nil
}
