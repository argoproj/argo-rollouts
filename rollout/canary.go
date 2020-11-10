package rollout

import (
	"context"
	"fmt"
	"reflect"
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

	if err := c.cleanupRollouts(c.otherRSs); err != nil {
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

	if len(c.rollout.Spec.Strategy.Canary.Steps) == 0 {
		c.log.Info("Rollout does not have any steps")
		return false
	}
	currentStep, currentStepIndex := replicasetutil.GetCurrentCanaryStep(c.rollout)

	if len(c.rollout.Spec.Strategy.Canary.Steps) <= int(*currentStepIndex) {
		c.log.Info("No Steps remain in the canary steps")
		return false
	}

	if currentStep.Pause == nil {
		return false
	}
	c.log.Infof("Reconciling canary pause step (stepIndex: %d)", *currentStepIndex)
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

func (c *rolloutContext) reconcileOldReplicaSetsCanary(allRSs []*appsv1.ReplicaSet, oldRSs []*appsv1.ReplicaSet) (bool, error) {
	oldPodsCount := replicasetutil.GetReplicaCountForReplicaSets(oldRSs)
	if oldPodsCount == 0 {
		// Can't scale down further
		return false, nil
	}

	// Clean up unhealthy replicas first, otherwise unhealthy replicas will block rollout
	// and cause timeout. See https://github.com/kubernetes/kubernetes/issues/16737
	oldRSs, cleanedUpRSs, err := c.cleanupUnhealthyReplicas(oldRSs)
	if err != nil {
		return false, nil
	}

	// Scale down old replica sets, need check replicasToKeep to ensure we can scale down
	scaledDownCount, err := c.scaleDownOldReplicaSetsForCanary(allRSs, oldRSs)
	if err != nil {
		return false, nil
	}
	c.log.Infof("Scaled down old RSes by %d", scaledDownCount)

	return cleanedUpRSs || scaledDownCount > 0, nil
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
	c.log.Infof("Found %d available pods, scaling down old RSes", availablePodCount)

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
	if currentStep.Pause != nil {
		return c.pauseContext.CompletedPauseStep(*currentStep.Pause)
	}
	modifyReplicasStep := currentStep.SetWeight != nil || currentStep.SetCanaryScale != nil
	if modifyReplicasStep && replicasetutil.AtDesiredReplicaCountsForCanary(c.rollout, c.newRS, c.stableRS, c.otherRSs) {
		c.log.Info("Rollout has reached the desired state for the correct weight")
		return true
	}
	experiment := c.currentEx
	if currentStep.Experiment != nil && experiment != nil && experiment.Status.Phase.Completed() && experiment.Status.Phase == v1alpha1.AnalysisPhaseSuccessful {
		return true
	}
	currentStepAr := c.currentArs.CanaryStep
	analysisExistsAndCompleted := currentStepAr != nil && currentStepAr.Status.Phase.Completed()
	if currentStep.Analysis != nil && analysisExistsAndCompleted && currentStepAr.Status.Phase == v1alpha1.AnalysisPhaseSuccessful {
		return true
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

	if c.stableRS == nil {
		msg := fmt.Sprintf("Setting StableRS to CurrentPodHash: StableRS hash: %s", newStatus.CurrentPodHash)
		c.log.Info(msg)
		newStatus.StableRS = newStatus.CurrentPodHash
		if stepCount > 0 {
			if stepCount != *currentStepIndex {
				c.recorder.Event(c.rollout, corev1.EventTypeNormal, "SettingStableRS", msg)
			}
			newStatus.CurrentStepIndex = &stepCount
		}
		c.pauseContext.ClearPauseConditions()
		c.pauseContext.RemoveAbort()
		newStatus = c.calculateRolloutConditions(newStatus)
		return c.persistRolloutStatus(&newStatus)
	}

	if c.rollout.Status.PromoteFull {
		c.pauseContext.ClearPauseConditions()
		c.pauseContext.RemoveAbort()
		if stepCount > 0 {
			currentStepIndex = pointer.Int32Ptr(stepCount)
		}
	}

	if c.pauseContext.IsAborted() {
		if stepCount > int32(0) {
			if newStatus.StableRS == newStatus.CurrentPodHash {
				newStatus.CurrentStepIndex = &stepCount
			} else {
				newStatus.CurrentStepIndex = pointer.Int32Ptr(0)
			}
		}
		newStatus = c.calculateRolloutConditions(newStatus)
		return c.persistRolloutStatus(&newStatus)
	}

	if currentStepIndex != nil && *currentStepIndex == stepCount {
		c.log.Info("Rollout has executed every step")
		newStatus.CurrentStepIndex = &stepCount
		if c.newRS != nil && c.newRS.Status.AvailableReplicas == defaults.GetReplicasOrDefault(c.rollout.Spec.Replicas) {
			c.log.Info("New RS has successfully progressed")
			newStatus.StableRS = newStatus.CurrentPodHash
			newStatus.PromoteFull = false
		}
		c.pauseContext.ClearPauseConditions()
		newStatus = c.calculateRolloutConditions(newStatus)
		return c.persistRolloutStatus(&newStatus)
	}

	if stepCount == 0 {
		c.log.Info("Rollout has no steps")
		if c.newRS != nil && c.newRS.Status.AvailableReplicas == defaults.GetReplicasOrDefault(c.rollout.Spec.Replicas) {
			c.log.Info("New RS has successfully progressed")
			newStatus.StableRS = newStatus.CurrentPodHash
		}
		newStatus = c.calculateRolloutConditions(newStatus)
		return c.persistRolloutStatus(&newStatus)
	}

	if c.completedCurrentCanaryStep() {
		*currentStepIndex++
		newStatus.CurrentStepIndex = currentStepIndex
		newStatus.Canary.CurrentStepAnalysisRun = ""
		newStatus.Canary.CurrentStepAnalysisRunStatus = nil
		if int(*currentStepIndex) == len(c.rollout.Spec.Strategy.Canary.Steps) {
			c.recorder.Event(c.rollout, corev1.EventTypeNormal, "SettingStableRS", "Completed all steps")
		}
		c.log.Infof("Incrementing the Current Step Index to %d", *currentStepIndex)
		c.recorder.Eventf(c.rollout, corev1.EventTypeNormal, "SetStepIndex", "Set Step Index to %d", int(*currentStepIndex))
		c.pauseContext.RemovePauseCondition(v1alpha1.PauseReasonCanaryPauseStep)
		newStatus = c.calculateRolloutConditions(newStatus)
		return c.persistRolloutStatus(&newStatus)
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

	c.log.Info("Reconciling old replica sets")
	scaledDown, err := c.reconcileOldReplicaSetsCanary(c.allRSs, controller.FilterActiveReplicaSets(c.otherRSs))
	if err != nil {
		return false, err
	}
	if scaledDown {
		c.log.Info("Not finished reconciling old replica sets")
		return true, nil
	}
	return false, nil
}

// reconcileEphemeralMetadata syncs canary/stable ephemeral metadata to ReplicaSets and pods
func (c *rolloutContext) reconcileEphemeralMetadata() error {
	ctx := context.TODO()
	if c.rollout.Spec.Strategy.Canary == nil {
		return nil
	}
	fullyRolledOut := c.rollout.Status.StableRS == "" || c.rollout.Status.StableRS == replicasetutil.GetPodTemplateHash(c.newRS)

	if fullyRolledOut {
		// We are in a steady-state (fully rolled out). newRS is the stableRS. there is no longer a canary
		err := c.syncEphemeralMetadata(ctx, c.newRS, c.rollout.Spec.Strategy.Canary.StableMetadata)
		if err != nil {
			return err
		}
	} else {
		// we are in a upgrading state. newRS is a canary
		err := c.syncEphemeralMetadata(ctx, c.newRS, c.rollout.Spec.Strategy.Canary.CanaryMetadata)
		if err != nil {
			return err
		}
		// sync stable metadata to the stable rs
		err = c.syncEphemeralMetadata(ctx, c.stableRS, c.rollout.Spec.Strategy.Canary.StableMetadata)
		if err != nil {
			return err
		}
	}

	// Iterate all other ReplicaSets and verify we don't have injected metadata for them
	for _, rs := range c.otherRSs {
		err := c.syncEphemeralMetadata(ctx, rs, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *rolloutContext) syncEphemeralMetadata(ctx context.Context, rs *appsv1.ReplicaSet, podMetadata *v1alpha1.PodTemplateMetadata) error {
	if rs == nil {
		return nil
	}
	modifiedRS, modified := replicasetutil.UpdateEphemeralPodMetadata(rs, podMetadata)
	if !modified {
		return nil
	}
	// 1. Sync ephemeral metadata to pods
	pods, err := replicasetutil.GetPodsOwnedByReplicaSet(ctx, c.kubeclientset, rs)
	if err != nil {
		return err
	}
	for _, pod := range pods {
		if reflect.DeepEqual(pod.Annotations, modifiedRS.Spec.Template.Annotations) &&
			reflect.DeepEqual(pod.Labels, modifiedRS.Spec.Template.Labels) {
			continue
		}
		// if we get here, the pod metadata needs correction
		pod.Annotations = modifiedRS.Spec.Template.Annotations
		pod.Labels = modifiedRS.Spec.Template.Labels
		_, err = c.kubeclientset.CoreV1().Pods(pod.Namespace).Update(ctx, pod, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		c.log.Infof("synced ephemeral metadata %v to Pod %s", podMetadata, pod.Name)
	}

	// 2. Update ReplicaSet so that any new pods it creates will have the metadata
	_, err = c.kubeclientset.AppsV1().ReplicaSets(modifiedRS.Namespace).Update(ctx, modifiedRS, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	c.log.Infof("synced ephemeral metadata %v to ReplicaSet %s", podMetadata, rs.Name)
	return nil
}
