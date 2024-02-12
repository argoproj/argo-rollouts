package rollout

import (
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/record"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	rolloututil "github.com/argoproj/argo-rollouts/utils/rollout"
)

func (c *rolloutContext) rolloutCanary() error {
	var err error
	if replicasetutil.PodTemplateOrStepsChanged(c.rollout, c.newRS) {
		c.newRS, err = c.getAllReplicaSetsAndSyncRevision(false)
		if err != nil {
			return fmt.Errorf("failed to getAllReplicaSetsAndSyncRevision in rolloutCanary with PodTemplateOrStepsChanged: %w", err)
		}
		return c.syncRolloutStatusCanary()
	}

	c.newRS, err = c.getAllReplicaSetsAndSyncRevision(true)
	if err != nil {
		return fmt.Errorf("failed to getAllReplicaSetsAndSyncRevision in rolloutCanary create true: %w", err)
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

	if err := c.reconcilePingAndPongService(); err != nil {
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

func (c *rolloutContext) reconcileCanaryStableReplicaSet() (bool, error) {
	if !replicasetutil.CheckStableRSExists(c.newRS, c.stableRS) {
		// we skip this because if they are equal, then it will get reconciled in reconcileNewReplicaSet()
		// making this redundant
		c.log.Info("No StableRS exists to reconcile or matches newRS")
		return false, nil
	}
	var desiredStableRSReplicaCount int32
	if c.rollout.Spec.Strategy.Canary.TrafficRouting == nil {
		_, desiredStableRSReplicaCount = replicasetutil.CalculateReplicaCountsForBasicCanary(c.rollout, c.newRS, c.stableRS, c.otherRSs)
	} else {
		// Note the use of c.rollout.Status.Canary.Weights instead of c.newStatus.Canary.Weights.
		// We don't want to use c.newStatus because that would have been just been modified in
		// reconcileTrafficRouting(). At the end of the canary steps, we switch the service and set
		// stable to 100%. In this scenario, c.newStatus.Canary.Weights.Stable.Weight would be 100,
		// causing us to flap and scale up the stable 100 temporarily (before scaling down to 0 later).
		// Therefore, we send c.rollout.Status.Canary.Weights so that the stable scaling happens in
		// a *susbsequent*, follow-up reconciliation, lagging behind the setWeight and service switch.
		_, desiredStableRSReplicaCount = replicasetutil.CalculateReplicaCountsForTrafficRoutedCanary(c.rollout, c.rollout.Status.Canary.Weights)
	}
	scaled, _, err := c.scaleReplicaSetAndRecordEvent(c.stableRS, desiredStableRSReplicaCount)
	if err != nil {
		return scaled, fmt.Errorf("failed to scaleReplicaSetAndRecordEvent in reconcileCanaryStableReplicaSet:L %w", err)
	}
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
func (c *rolloutContext) scaleDownOldReplicaSetsForCanary(oldRSs []*appsv1.ReplicaSet) (int32, error) {
	// Clean up unhealthy replicas first, otherwise unhealthy replicas will block rollout
	// and cause timeout. See https://github.com/kubernetes/kubernetes/issues/16737
	oldRSs, totalScaledDown, err := c.cleanupUnhealthyReplicas(oldRSs)
	if err != nil {
		return totalScaledDown, nil
	}
	availablePodCount := replicasetutil.GetAvailableReplicaCountForReplicaSets(c.allRSs)
	minAvailable := defaults.GetReplicasOrDefault(c.rollout.Spec.Replicas) - replicasetutil.MaxUnavailable(c.rollout)
	maxScaleDown := availablePodCount - minAvailable
	if maxScaleDown <= 0 {
		// Cannot scale down.
		return 0, nil
	}
	c.log.Infof("Found %d available pods, scaling down old RSes (minAvailable: %d, maxScaleDown: %d)", availablePodCount, minAvailable, maxScaleDown)

	sort.Sort(sort.Reverse(replicasetutil.ReplicaSetsByRevisionNumber(oldRSs)))

	if canProceed, err := c.canProceedWithScaleDownAnnotation(oldRSs); !canProceed || err != nil {
		return 0, err
	}

	annotationedRSs := int32(0)
	for _, targetRS := range oldRSs {
		if c.rollout.Spec.Strategy.Canary.TrafficRouting != nil && c.isReplicaSetReferenced(targetRS) {
			// We might get here if user interrupted an an update in order to move back to stable.
			c.log.Infof("Skip scale down of older RS '%s': still referenced", targetRS.Name)
			continue
		}
		if maxScaleDown <= 0 {
			break
		}
		if *targetRS.Spec.Replicas == 0 {
			// cannot scale down this ReplicaSet.
			continue
		}
		desiredReplicaCount := int32(0)
		if c.rollout.Spec.Strategy.Canary.TrafficRouting == nil {
			// For basic canary, we must scale down all other ReplicaSets because existence of
			// those pods will cause traffic to be served by them
			if *targetRS.Spec.Replicas > maxScaleDown {
				desiredReplicaCount = *targetRS.Spec.Replicas - maxScaleDown
			}
		} else {
			if rolloututil.IsFullyPromoted(c.rollout) || replicasetutil.HasScaleDownDeadline(targetRS) {
				// If we are fully promoted and we encounter an old ReplicaSet, we can infer that
				// this ReplicaSet is likely the previous stable. We should do one of two things:
				if c.rollout.Spec.Strategy.Canary.DynamicStableScale {
					// 1. if we are using dynamic scaling, then this should be scaled down to 0 now
					desiredReplicaCount = 0
				} else {
					// 2. otherwise, honor scaledown delay second and keep replicas of the current step
					annotationedRSs, desiredReplicaCount, err = c.scaleDownDelayHelper(targetRS, annotationedRSs, *targetRS.Spec.Replicas)
					if err != nil {
						return totalScaledDown, err
					}
				}
			} else {
				// If we get here, we are *not* fully promoted and are in the middle of an update.
				// We just encountered a scaled up ReplicaSet which is neither the stable or canary
				// and doesn't yet have scale down deadline. This happens when a user changes their
				// mind in the middle of an V1 -> V2 update, and then applies a V3. We are deciding
				// what to do with the defunct, intermediate V2 ReplicaSet right now.
				// It is safe to scale the intermediate RS down, since no traffic is directed to it.
				c.log.Infof("scaling down intermediate RS '%s'", targetRS.Name)
			}
		}
		if *targetRS.Spec.Replicas == desiredReplicaCount {
			// already at desired account, nothing to do
			continue
		}
		// Scale down.
		_, _, err = c.scaleReplicaSetAndRecordEvent(targetRS, desiredReplicaCount)
		if err != nil {
			return totalScaledDown, fmt.Errorf("failed to scaleReplicaSetAndRecordEvent in scaleDownOldReplicaSetsForCanary: %w", err)
		}
		scaleDownCount := *targetRS.Spec.Replicas - desiredReplicaCount
		maxScaleDown -= scaleDownCount
		totalScaledDown += scaleDownCount
	}

	return totalScaledDown, nil
}

// isDynamicallyRollingBackToStable returns true if we were in the middle of an canary update with
// dynamic stable scaling, but was interrupted and are now rolling back to stable RS. This is similar
// to, but different than aborting. With abort, desired hash != stable hash and so we know the
// two hashes to balance traffic against. But with dynamically rolling back to stable, the
// desired hash == stable hash, and so we must use the *previous* desired hash and balance traffic
// between previous desired vs. stable hash, in order to safely shift traffic back to stable.
// This function also returns the previous desired hash (where we are weighted to)
func isDynamicallyRollingBackToStable(ro *v1alpha1.Rollout, desiredRS *appsv1.ReplicaSet) (bool, string) {
	if rolloututil.IsFullyPromoted(ro) && ro.Spec.Strategy.Canary.TrafficRouting != nil && ro.Spec.Strategy.Canary.DynamicStableScale {
		if ro.Status.Canary.Weights != nil {
			currSelector := ro.Status.Canary.Weights.Canary.PodTemplateHash
			desiredSelector := replicasetutil.GetPodTemplateHash(desiredRS)
			if currSelector != desiredSelector {
				if desiredRS.Status.AvailableReplicas < *ro.Spec.Replicas {
					return true, currSelector
				}
			}
		}
	}
	return false, ""
}

// canProceedWithScaleDownAnnotation returns whether or not it is safe to proceed with annotating
// old replicasets with the scale-down-deadline in the traffic-routed canary strategy.
// This method only matters with ALB canary + the target group verification feature.
// The safety guarantees we provide are that we will not scale down *anything* unless we can verify
// stable target group endpoints are registered properly.
// NOTE: this method was written in a way which avoids AWS API calls.
func (c *rolloutContext) canProceedWithScaleDownAnnotation(oldRSs []*appsv1.ReplicaSet) (bool, error) {
	isALBCanary := c.rollout.Spec.Strategy.Canary != nil && c.rollout.Spec.Strategy.Canary.TrafficRouting != nil && c.rollout.Spec.Strategy.Canary.TrafficRouting.ALB != nil
	if !isALBCanary {
		// Only ALB
		return true, nil
	}

	needToVerifyTargetGroups := false
	for _, targetRS := range oldRSs {
		if *targetRS.Spec.Replicas > 0 && !replicasetutil.HasScaleDownDeadline(targetRS) {
			// We encountered an old ReplicaSet that is not yet scaled down, and is not annotated
			// We only verify target groups if there is something to scale down.
			needToVerifyTargetGroups = true
			break
		}
	}
	if !needToVerifyTargetGroups {
		// All ReplicaSets are either scaled down, or have a scale-down-deadline annotation.
		// The presence of the scale-down-deadline on all oldRSs, implies we can proceed with
		// scale down, because we only add that annotation when target groups have been verified.
		// Therefore, we return true to avoid performing verification again and making unnecessary
		// AWS API calls.
		return true, nil
	}
	stableSvcName, _ := trafficrouting.GetStableAndCanaryServices(c.rollout)
	stableSvc, err := c.servicesLister.Services(c.rollout.Namespace).Get(stableSvcName)
	if err != nil {
		return false, err
	}
	err = c.awsVerifyTargetGroups(stableSvc)
	if err != nil {
		return false, err
	}

	canProceed := c.areTargetsVerified()
	c.log.Infof("Proceed with scaledown: %v", canProceed)
	return canProceed, nil
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
		return replicasetutil.AtDesiredReplicaCountsForCanary(c.rollout, c.newRS, c.stableRS, c.otherRSs, c.newStatus.Canary.Weights)
	case currentStep.SetWeight != nil:
		if !replicasetutil.AtDesiredReplicaCountsForCanary(c.rollout, c.newRS, c.stableRS, c.otherRSs, c.newStatus.Canary.Weights) {
			return false
		}
		if c.newStatus.Canary.Weights != nil && c.newStatus.Canary.Weights.Verified != nil && !*c.newStatus.Canary.Weights.Verified {
			// we haven't yet verified the target weight after the setWeight
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
	case currentStep.SetHeaderRoute != nil:
		return true
	case currentStep.SetMirrorRoute != nil:
		return true
	}
	return false
}

func (c *rolloutContext) syncRolloutStatusCanary() error {
	newStatus := c.calculateBaseStatus()
	newStatus.AvailableReplicas = replicasetutil.GetAvailableReplicaCountForReplicaSets(c.allRSs)
	newStatus.HPAReplicas = replicasetutil.GetActualReplicaCountForReplicaSets(c.allRSs)
	newStatus.Selector = metav1.FormatLabelSelector(c.rollout.Spec.Selector)
	newStatus.Canary.StablePingPong = c.rollout.Status.Canary.StablePingPong

	currentStep, currentStepIndex := replicasetutil.GetCurrentCanaryStep(c.rollout)
	newStatus.StableRS = c.rollout.Status.StableRS
	newStatus.CurrentStepHash = conditions.ComputeStepHash(c.rollout)
	stepCount := int32(len(c.rollout.Spec.Strategy.Canary.Steps))

	if replicasetutil.PodTemplateOrStepsChanged(c.rollout, c.newRS) {
		c.resetRolloutStatus(&newStatus)
		if c.newRS != nil && stepCount > 0 {
			if c.rollout.Status.StableRS == replicasetutil.GetPodTemplateHash(c.newRS) {
				// If we get here, we detected that we've moved back to the stable ReplicaSet
				c.recorder.Eventf(c.rollout, record.EventOptions{EventReason: "SkipSteps"}, "Rollback to stable ReplicaSets")
				newStatus.CurrentStepIndex = &stepCount
			} else if c.isRollbackWithinWindow() && replicasetutil.IsActive(c.newRS) {
				// Else if we get here we detected that we are within the rollback window we can skip steps and move back to the active ReplicaSet
				c.recorder.Eventf(c.rollout, record.EventOptions{EventReason: "SkipSteps"}, "Rollback to active ReplicaSets within RollbackWindow")
				newStatus.CurrentStepIndex = &stepCount
			}
		}

		newStatus = c.calculateRolloutConditions(newStatus)
		return c.persistRolloutStatus(&newStatus)
	}

	if c.rollout.Status.PromoteFull || c.isRollbackWithinWindow() {
		c.pauseContext.ClearPauseConditions()
		c.pauseContext.RemoveAbort()
		if stepCount > 0 {
			currentStepIndex = &stepCount
		}
	}

	if reason := c.shouldFullPromote(newStatus); reason != "" {
		err := c.promoteStable(&newStatus, reason)
		if err != nil {
			return err
		}
		newStatus = c.calculateRolloutConditions(newStatus)
		return c.persistRolloutStatus(&newStatus)
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

	if c.completedCurrentCanaryStep() {
		stepStr := rolloututil.CanaryStepString(*currentStep)
		*currentStepIndex++
		newStatus.Canary.CurrentStepAnalysisRunStatus = nil

		c.recorder.Eventf(c.rollout, record.EventOptions{EventReason: conditions.RolloutStepCompletedReason}, conditions.RolloutStepCompletedMessage, int(*currentStepIndex), stepCount, stepStr)
		c.pauseContext.RemovePauseCondition(v1alpha1.PauseReasonCanaryPauseStep)
	}

	newStatus.CurrentStepIndex = currentStepIndex
	newStatus = c.calculateRolloutConditions(newStatus)
	return c.persistRolloutStatus(&newStatus)
}

func (c *rolloutContext) reconcileCanaryReplicaSets() (bool, error) {
	if haltReason := c.haltProgress(); haltReason != "" {
		c.log.Infof("Skipping canary/stable ReplicaSet reconciliation: %s", haltReason)
		return false, nil
	}
	err := c.removeScaleDownDeadlines()
	if err != nil {
		return false, err
	}
	scaledStableRS, err := c.reconcileCanaryStableReplicaSet()
	if err != nil {
		return false, err
	}
	if scaledStableRS {
		c.log.Infof("Not finished reconciling stableRS")
		return true, nil
	}

	// If we have updated both the replica count and the pod template hash c.newRS will be nil we want to reconcile the newRS so we look at the
	// rollout status to get the newRS to reconcile it.
	if c.newRS == nil && c.rollout.Status.CurrentPodHash != c.rollout.Status.StableRS {
		rs, _ := replicasetutil.GetReplicaSetByTemplateHash(c.allRSs, c.rollout.Status.CurrentPodHash)
		c.newRS = rs
	}

	scaledNewRS, err := c.reconcileNewReplicaSet()
	if err != nil {
		return false, err
	}
	if scaledNewRS {
		c.log.Infof("Not finished reconciling new ReplicaSet '%s'", c.newRS.Name)
		return true, nil
	}

	scaledDown, err := c.reconcileOtherReplicaSets()
	if err != nil {
		return false, err
	}
	if scaledDown {
		c.log.Info("Not finished reconciling old ReplicaSets")
		return true, nil
	}
	return false, nil
}
