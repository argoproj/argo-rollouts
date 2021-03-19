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
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

func (c *rolloutContext) rolloutCanary() error {
	var err error
	if replicasetutil.PodTemplateOrStepsChanged(c.rollout, c.newRS) {
		c.newRS, err = c.getAllReplicaSetsAndSyncRevision(false)
		if err != nil {
			return err
		}
		return c.syncRolloutStatusCanary()
	}

	c.newRS, err = c.getAllReplicaSetsAndSyncRevision(true)
	if err != nil {
		return err
	}

	err = c.podRestarter.Reconcile(c)
	if err != nil {
		return err
	}

	err = c.reconcileEphemeralMetadata()
	if err != nil {
		return err
	}

	if err := c.reconcileRevisionHistoryLimit(c.otherRSs); err != nil {
		return err
	}

	if err := c.reconcileStableAndCanaryService(); err != nil {
		return err
	}

	if err := c.reconcileTrafficRouting(); err != nil {
		return err
	}

	err = c.reconcileExperiments()
	if err != nil {
		return err
	}

	err = c.reconcileAnalysisRuns()
	if c.pauseContext.HasAddPause() {
		c.log.Info("Detected pause due to inconclusive AnalysisRun")
		return c.syncRolloutStatusCanary()
	}
	if err != nil {
		return err
	}

	noScalingOccurred, err := c.reconcileCanaryReplicaSets()
	if err != nil {
		return err
	}
	if noScalingOccurred {
		c.log.Info("Not finished reconciling ReplicaSets")
		return c.syncRolloutStatusCanary()
	}

	stillReconciling := c.reconcileCanaryPause()
	if stillReconciling {
		c.log.Infof("Not finished reconciling Canary Pause")
		return c.syncRolloutStatusCanary()
	}

	return c.syncRolloutStatusCanary()
}

func (c *rolloutContext) reconcileStableRS() (bool, error) {
	if !replicasetutil.CheckStableRSExists(c.newRS, c.stableRS) {
		c.log.Info("No StableRS exists to reconcile or matches newRS")
		return false, nil
	}
	_, stableRSReplicaCount := replicasetutil.CalculateReplicaCountsForCanary(c.rollout, c.newRS, c.stableRS, c.otherRSs)
	scaled, _, err := c.scaleReplicaSetAndRecordEvent(c.stableRS, stableRSReplicaCount)
	return scaled, err
}

func (c *rolloutContext) reconcileCanaryPause() bool {
	if c.rollout.Spec.Paused {
		return false
	}
	if c.rollout.Status.PromoteFull {
		return false
	}
	totalSteps := len(c.rollout.Spec.Strategy.Canary.Steps)
	if totalSteps == 0 {
		c.log.Info("Rollout does not have any steps")
		return false
	}
	currentStep, currentStepIndex := replicasetutil.GetCurrentCanaryStep(c.rollout)

	if totalSteps <= int(*currentStepIndex) {
		c.log.Info("No Steps remain in the canary steps")
		return false
	}

	if currentStep.Pause == nil {
		return false
	}
	c.log.Infof("Reconciling canary pause step (stepIndex: %d/%d)", *currentStepIndex, totalSteps)
	cond := getPauseCondition(c.rollout, v1alpha1.PauseReasonCanaryPauseStep)
	if cond == nil {
		// When the pause condition is null, that means the rollout is in an not paused state.
		// As a result, the controller needs to detect whether a rollout was unpaused or the
		// rollout needs to be paused for the first time. If the ControllerPause is false,
		// the controller has not paused the rollout yet and needs to do so before it
		// can proceed.
		if !c.rollout.Status.ControllerPause {
			c.pauseContext.AddPauseCondition(v1alpha1.PauseReasonCanaryPauseStep)
		}
		return true
	}
	if currentStep.Pause.Duration == nil {
		return true
	}
	c.checkEnqueueRolloutDuringWait(cond.StartTime, currentStep.Pause.DurationSeconds())
	return true
}

// scaleDownOldReplicaSetsForCanary scales down old replica sets when rollout strategy is "canary".
func (c *rolloutContext) scaleDownOldReplicaSetsForCanary(allRSs []*appsv1.ReplicaSet, oldRSs []*appsv1.ReplicaSet) (int32, error) {
	availablePodCount := replicasetutil.GetAvailableReplicaCountForReplicaSets(allRSs)
	minAvailable := defaults.GetReplicasOrDefault(c.rollout.Spec.Replicas) - replicasetutil.MaxUnavailable(c.rollout)
	maxScaleDown := availablePodCount - minAvailable
	if maxScaleDown <= 0 {
		// Cannot scale down.
		return 0, nil
	}
	c.log.Infof("Found %d available pods, scaling down old RSes (minAvailable: %d, maxScaleDown: %d)", availablePodCount, minAvailable, maxScaleDown)

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
		// Scale down.
		newReplicasCount := int32(0)
		if *(targetRS.Spec.Replicas) > maxScaleDown {
			newReplicasCount = *(targetRS.Spec.Replicas) - maxScaleDown
		}
		_, _, err := c.scaleReplicaSetAndRecordEvent(targetRS, newReplicasCount)
		if err != nil {
			return totalScaledDown, err
		}
		scaleDownCount := *targetRS.Spec.Replicas - newReplicasCount
		maxScaleDown -= scaleDownCount
		totalScaledDown += scaleDownCount
	}

	return totalScaledDown, nil
}

func (c *rolloutContext) completedCurrentCanaryStep() bool {
	if c.rollout.Spec.Paused {
		return false
	}
	currentStep, _ := replicasetutil.GetCurrentCanaryStep(c.rollout)
	if currentStep == nil {
		return false
	}
	switch {
	case currentStep.Pause != nil:
		return c.pauseContext.CompletedCanaryPauseStep(*currentStep.Pause)
	case currentStep.SetCanaryScale != nil:
		return replicasetutil.AtDesiredReplicaCountsForCanary(c.rollout, c.newRS, c.stableRS, c.otherRSs)
	case currentStep.SetWeight != nil:
		if !replicasetutil.AtDesiredReplicaCountsForCanary(c.rollout, c.newRS, c.stableRS, c.otherRSs) {
			return false
		}
		if c.weightVerified != nil && !*c.weightVerified {
			return false
		}
		return true
	case currentStep.Experiment != nil:
		experiment := c.currentEx
		return experiment != nil && experiment.Status.Phase == v1alpha1.AnalysisPhaseSuccessful
	case currentStep.Analysis != nil:
		currentStepAr := c.currentArs.CanaryStep
		analysisExistsAndCompleted := currentStepAr != nil && currentStepAr.Status.Phase.Completed()
		return analysisExistsAndCompleted && currentStepAr.Status.Phase == v1alpha1.AnalysisPhaseSuccessful
	}
	return false
}

func (c *rolloutContext) syncRolloutStatusCanary() error {
	newStatus := c.calculateBaseStatus()
	newStatus.AvailableReplicas = replicasetutil.GetAvailableReplicaCountForReplicaSets(c.allRSs)
	newStatus.HPAReplicas = replicasetutil.GetActualReplicaCountForReplicaSets(c.allRSs)
	newStatus.Selector = metav1.FormatLabelSelector(c.rollout.Spec.Selector)

	_, currentStepIndex := replicasetutil.GetCurrentCanaryStep(c.rollout)
	newStatus.StableRS = c.rollout.Status.StableRS
	newStatus.CurrentStepHash = conditions.ComputeStepHash(c.rollout)
	stepCount := int32(len(c.rollout.Spec.Strategy.Canary.Steps))

	if replicasetutil.PodTemplateOrStepsChanged(c.rollout, c.newRS) {
		newStatus.CurrentStepIndex = replicasetutil.ResetCurrentStepIndex(c.rollout)
		if c.newRS != nil && c.rollout.Status.StableRS == replicasetutil.GetPodTemplateHash(c.newRS) {
			if newStatus.CurrentStepIndex != nil {
				msg := "Skipping all steps because the newRS is the stableRS."
				c.log.Info(msg)
				newStatus.CurrentStepIndex = pointer.Int32Ptr(stepCount)
				c.recorder.Event(c.rollout, corev1.EventTypeNormal, "SkipSteps", msg)
			}
		}
		c.pauseContext.ClearPauseConditions()
		c.pauseContext.RemoveAbort()
		c.SetRestartedAt()
		newStatus = c.calculateRolloutConditions(newStatus)
		newStatus.Canary.CurrentStepAnalysisRunStatus = nil
		newStatus.Canary.CurrentBackgroundAnalysisRunStatus = nil
		newStatus.PromoteFull = false
		return c.persistRolloutStatus(&newStatus)
	}

	var promoteToStableReason string
	if c.stableRS == nil {
		promoteToStableReason = "Initial deploy"
	} else if c.rollout.Status.PromoteFull {
		promoteToStableReason = "Full promotion requested"
	} else if c.pauseContext.IsAborted() {
		if stepCount > int32(0) {
			if newStatus.StableRS == newStatus.CurrentPodHash {
				newStatus.CurrentStepIndex = &stepCount
			} else {
				newStatus.CurrentStepIndex = pointer.Int32Ptr(0)
			}
		}
		newStatus = c.calculateRolloutConditions(newStatus)
		return c.persistRolloutStatus(&newStatus)
	} else if stepCount == 0 || (currentStepIndex != nil && *currentStepIndex == stepCount) {
		if c.newRS != nil && c.newRS.Status.AvailableReplicas == defaults.GetReplicasOrDefault(c.rollout.Spec.Replicas) {
			promoteToStableReason = fmt.Sprintf("Completed all %d steps", stepCount)
		}
	}

	if promoteToStableReason != "" {
		c.pauseContext.ClearPauseConditions()
		c.pauseContext.RemoveAbort()
		if stepCount > 0 {
			newStatus.CurrentStepIndex = &stepCount
		}
		msg := fmt.Sprintf("Setting stable to %s: %s", newStatus.CurrentPodHash, promoteToStableReason)
		c.log.Info(msg)
		c.recorder.Event(c.rollout, corev1.EventTypeNormal, "SettingStableRS", msg)
		newStatus.StableRS = newStatus.CurrentPodHash
		newStatus.PromoteFull = false
		newStatus = c.calculateRolloutConditions(newStatus)
		return c.persistRolloutStatus(&newStatus)
		// // Now that we've marked the current RS as stable, start the scale-down countdown on the previous stable RS
		// previousStableRS, _ := replicasetutil.GetReplicaSetByTemplateHash(c.olderRSs, c.rollout.Status.StableRS)
		// if replicasetutil.GetReplicaCountForReplicaSets([]*appsv1.ReplicaSet{previousStableRS}) > 0 {
		// 	err := c.addScaleDownDelay(previousStableRS)
		// 	if err != nil {
		// 		return err
		// 	}
		// }
	}

	if c.completedCurrentCanaryStep() {
		*currentStepIndex++
		newStatus.Canary.CurrentStepAnalysisRunStatus = nil
		c.log.Infof("Incrementing the Current Step Index to %d", *currentStepIndex)
		c.recorder.Eventf(c.rollout, corev1.EventTypeNormal, "SetStepIndex", "Set Step Index to %d", int(*currentStepIndex))
		c.pauseContext.RemovePauseCondition(v1alpha1.PauseReasonCanaryPauseStep)
	}

	newStatus.CurrentStepIndex = currentStepIndex
	newStatus = c.calculateRolloutConditions(newStatus)
	return c.persistRolloutStatus(&newStatus)
}

func (c *rolloutContext) reconcileCanaryReplicaSets() (bool, error) {
	c.log.Info("Reconciling StableRS")
	scaledStableRS, err := c.reconcileStableRS()
	if err != nil {
		return false, err
	}
	if scaledStableRS {
		c.log.Infof("Not finished reconciling stableRS")
		return true, nil
	}

	scaledNewRS, err := c.reconcileNewReplicaSet()
	if err != nil {
		return false, err
	}
	if scaledNewRS {
		c.log.Infof("Not finished reconciling new ReplicaSet '%s'", c.newRS.Name)
		return true, nil
	}

	scaledDown, err := c.reconcileOldReplicaSets()
	if err != nil {
		return false, err
	}
	if scaledDown {
		c.log.Info("Not finished reconciling old replica sets")
		return true, nil
	}
	return false, nil
}
