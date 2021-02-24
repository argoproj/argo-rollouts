package rollout

import (
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	serviceutil "github.com/argoproj/argo-rollouts/utils/service"
)

// rolloutBlueGreen implements the logic for rolling a new replica set.
func (c *rolloutContext) rolloutBlueGreen() error {
	previewSvc, activeSvc, err := c.getPreviewAndActiveServices()
	if err != nil {
		return err
	}
	c.newRS, err = c.getAllReplicaSetsAndSyncRevision(true)
	if err != nil {
		return err
	}

	if c.reconcileBlueGreenTemplateChange() {
		c.pauseContext.ClearPauseConditions()
		c.pauseContext.RemoveAbort()
		c.SetRestartedAt()
		c.log.Infof("New pod template or template change detected")
		return c.syncRolloutStatusBlueGreen(previewSvc, activeSvc)
	}

	err = c.podRestarter.Reconcile(c)
	if err != nil {
		return err
	}

	err = c.reconcileBlueGreenReplicaSets(activeSvc)
	if err != nil {
		return err
	}

	err = c.reconcilePreviewService(previewSvc)
	if err != nil {
		return err
	}

	c.reconcileBlueGreenPause(activeSvc, previewSvc)

	err = c.reconcileActiveService(activeSvc)
	if err != nil {
		return err
	}

	err = c.reconcileAnalysisRuns()
	if err != nil {
		return err
	}

	err = c.reconcileEphemeralMetadata()
	if err != nil {
		return err
	}

	return c.syncRolloutStatusBlueGreen(previewSvc, activeSvc)
}

func (c *rolloutContext) reconcileStableReplicaSet(activeSvc *corev1.Service) error {
	if _, ok := activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]; !ok {
		return nil
	}
	activeRS, _ := replicasetutil.GetReplicaSetByTemplateHash(c.allRSs, activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
	if activeRS == nil {
		c.log.Warn("There shouldn't be a nil active replicaset if the active Service selector is set")
		return nil
	}

	c.log.Infof("Reconciling stable ReplicaSet '%s'", activeRS.Name)
	if replicasetutil.HasScaleDownDeadline(activeRS) {
		// SetScaleDownDeadlineAnnotation should be removed from the new RS to ensure a new value is set
		// when the active service changes to a different RS
		err := c.removeScaleDownDelay(activeRS)
		if err != nil {
			return err
		}
	}
	_, _, err := c.scaleReplicaSetAndRecordEvent(activeRS, defaults.GetReplicasOrDefault(c.rollout.Spec.Replicas))
	return err
}

func (c *rolloutContext) reconcileBlueGreenReplicaSets(activeSvc *corev1.Service) error {
	err := c.reconcileStableReplicaSet(activeSvc)
	if err != nil {
		return err
	}
	_, err = c.reconcileNewReplicaSet()
	if err != nil {
		return err
	}
	// Scale down old non-active, non-stable replicasets, if we can.
	_, err = c.reconcileOldReplicaSets(controller.FilterActiveReplicaSets(c.otherRSs))
	if err != nil {
		return err
	}
	if err := c.cleanupRollouts(c.otherRSs); err != nil {
		return err
	}
	return nil
}

// reconcileBlueGreenTemplateChange returns true if we detect there was a change in the pod template
// from our current pod hash, or the newRS does not yet exist
func (c *rolloutContext) reconcileBlueGreenTemplateChange() bool {
	if c.newRS == nil {
		return true
	}
	return c.rollout.Status.CurrentPodHash != c.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
}

// isBlueGreenFastTracked returns true if we should skip the pause step because update has been fast tracked
func (c *rolloutContext) isBlueGreenFastTracked(activeSvc *corev1.Service) bool {
	if replicasetutil.HasScaleDownDeadline(c.newRS) {
		c.log.Infof("Detected scale down annotation for ReplicaSet '%s' and will skip pause", c.newRS.Name)
		return true
	}
	if c.rollout.Status.PromoteFull {
		return true
	}
	if _, ok := activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]; !ok {
		return true
	}
	if activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] == c.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] {
		return true
	}
	return false
}

func (c *rolloutContext) reconcileBlueGreenPause(activeSvc, previewSvc *corev1.Service) {
	if c.rollout.Status.Abort {
		return
	}

	if !replicasetutil.ReadyForPause(c.rollout, c.newRS, c.allRSs) {
		c.log.Infof("New RS '%s' is not ready to pause", c.newRS.Name)
		return
	}
	if c.rollout.Spec.Paused {
		c.log.Info("rollout has been paused by user")
		return
	}
	if c.isBlueGreenFastTracked(activeSvc) {
		c.log.Debug("skipping pause: fast-tracked update")
		c.pauseContext.RemovePauseCondition(v1alpha1.PauseReasonBlueGreenPause)
		return
	}
	newRSPodHash := c.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	if activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] == newRSPodHash {
		c.log.Debug("skipping pause: desired ReplicaSet already active")
		c.pauseContext.RemovePauseCondition(v1alpha1.PauseReasonBlueGreenPause)
		return
	}
	if c.rollout.Status.BlueGreen.ScaleUpPreviewCheckPoint {
		c.log.Debug("skipping pause: scaleUpPreviewCheckPoint passed")
		c.pauseContext.RemovePauseCondition(v1alpha1.PauseReasonBlueGreenPause)
		return
	}
	if !needsBlueGreenControllerPause(c.rollout) {
		c.pauseContext.RemovePauseCondition(v1alpha1.PauseReasonBlueGreenPause)
		return
	}

	// if we get here, the controller should manage the pause/resume
	c.log.Infof("reconciling pause (autoPromotionSeconds: %d)", c.rollout.Spec.Strategy.BlueGreen.AutoPromotionSeconds)
	if !c.completedPrePromotionAnalysis() {
		c.log.Infof("not ready for pause: prePromotionAnalysis incomplete")
		return
	}
	pauseCond := getPauseCondition(c.rollout, v1alpha1.PauseReasonBlueGreenPause)
	if pauseCond == nil && !c.rollout.Status.ControllerPause {
		if pauseCond == nil {
			c.log.Info("pausing")
		}
		c.pauseContext.AddPauseCondition(v1alpha1.PauseReasonBlueGreenPause)
		return
	}

	if !c.pauseContext.CompletedBlueGreenPause() {
		c.log.Info("pause incomplete")
		if c.rollout.Spec.Strategy.BlueGreen.AutoPromotionSeconds > 0 {
			c.checkEnqueueRolloutDuringWait(pauseCond.StartTime, c.rollout.Spec.Strategy.BlueGreen.AutoPromotionSeconds)
		}
	} else {
		c.log.Infof("pause completed")
		c.pauseContext.RemovePauseCondition(v1alpha1.PauseReasonBlueGreenPause)
	}
}

// needsBlueGreenControllerPause indicates if the controller should manage the pause status of the blue-green rollout
func needsBlueGreenControllerPause(ro *v1alpha1.Rollout) bool {
	if ro.Spec.Strategy.BlueGreen.AutoPromotionEnabled != nil {
		if !*ro.Spec.Strategy.BlueGreen.AutoPromotionEnabled {
			return true
		}
	}
	return ro.Spec.Strategy.BlueGreen.AutoPromotionSeconds > 0
}

// scaleDownOldReplicaSetsForBlueGreen scales down old replica sets when rollout strategy is "Blue Green".
func (c *rolloutContext) scaleDownOldReplicaSetsForBlueGreen(oldRSs []*appsv1.ReplicaSet) (bool, error) {
	if getPauseCondition(c.rollout, v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		c.log.Infof("Cannot scale down old ReplicaSets while paused with inconclusive Analysis ")
		return false, nil
	}
	if c.rollout.Spec.Strategy.BlueGreen.PostPromotionAnalysis != nil && c.rollout.Spec.Strategy.BlueGreen.ScaleDownDelaySeconds == nil && !skipPostPromotionAnalysisRun(c.rollout, c.newRS) {
		currentPostAr := c.currentArs.BlueGreenPostPromotion
		if currentPostAr == nil || currentPostAr.Status.Phase != v1alpha1.AnalysisPhaseSuccessful {
			c.log.Infof("Cannot scale down old ReplicaSets while Analysis is running and no ScaleDownDelaySeconds")
			return false, nil
		}
	}
	sort.Sort(sort.Reverse(replicasetutil.ReplicaSetsByRevisionNumber(oldRSs)))

	hasScaled := false
	annotationedRSs := int32(0)
	rolloutReplicas := defaults.GetReplicasOrDefault(c.rollout.Spec.Replicas)
	for _, targetRS := range oldRSs {
		if replicasetutil.IsStillReferenced(c.rollout.Status, targetRS) {
			// We should technically never get here because we shouldn't be passing a replicaset list
			// which includes referenced ReplicaSets. But we check just in case
			c.log.Warnf("Prevented inadvertent scaleDown of RS '%s'", targetRS.Name)
			continue
		}

		desiredReplicaCount := int32(0)
		if scaleDownAtStr, ok := targetRS.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey]; ok {
			annotationedRSs++
			scaleDownAtTime, err := time.Parse(time.RFC3339, scaleDownAtStr)
			if err != nil {
				c.log.Warnf("Unable to read scaleDownAt label on rs '%s'", targetRS.Name)
			} else if c.rollout.Spec.Strategy.BlueGreen.ScaleDownDelayRevisionLimit != nil && annotationedRSs > *c.rollout.Spec.Strategy.BlueGreen.ScaleDownDelayRevisionLimit {
				c.log.Infof("At ScaleDownDelayRevisionLimit (%d) and scaling down the rest", *c.rollout.Spec.Strategy.BlueGreen.ScaleDownDelayRevisionLimit)
			} else {
				now := metav1.Now()
				scaleDownAt := metav1.NewTime(scaleDownAtTime)
				if scaleDownAt.After(now.Time) {
					c.log.Infof("RS '%s' has not reached the scaleDownTime", targetRS.Name)
					remainingTime := scaleDownAt.Sub(now.Time)
					if remainingTime < c.resyncPeriod {
						c.enqueueRolloutAfter(c.rollout, remainingTime)
					}
					desiredReplicaCount = rolloutReplicas
				}
			}
		}
		if *(targetRS.Spec.Replicas) == desiredReplicaCount {
			// at desired account
			continue
		}
		// Scale down.
		_, _, err := c.scaleReplicaSetAndRecordEvent(targetRS, desiredReplicaCount)
		if err != nil {
			return false, err
		}
		hasScaled = true
	}

	return hasScaled, nil
}

func (c *rolloutContext) syncRolloutStatusBlueGreen(previewSvc *corev1.Service, activeSvc *corev1.Service) error {
	newStatus := c.calculateBaseStatus()

	if replicasetutil.CheckPodSpecChange(c.rollout, c.newRS) {
		c.pauseContext.ClearPauseConditions()
		c.pauseContext.RemoveAbort()
		c.SetRestartedAt()
		newStatus.BlueGreen.PrePromotionAnalysisRunStatus = nil
		newStatus.BlueGreen.PostPromotionAnalysisRunStatus = nil
		newStatus.PromoteFull = false
	}

	if c.rollout.Status.PromoteFull {
		c.pauseContext.ClearPauseConditions()
		c.pauseContext.RemoveAbort()
	}

	previewSelector := serviceutil.GetRolloutSelectorLabel(previewSvc)
	if previewSelector != c.rollout.Status.BlueGreen.PreviewSelector {
		c.log.Infof("Updating preview selector (%s -> %s)", c.rollout.Status.BlueGreen.PreviewSelector, previewSelector)
	}
	newStatus.BlueGreen.PreviewSelector = previewSelector

	activeSelector := serviceutil.GetRolloutSelectorLabel(activeSvc)
	if activeSelector != c.rollout.Status.BlueGreen.ActiveSelector {
		c.log.Infof("Updating active selector (%s -> %s)", c.rollout.Status.BlueGreen.ActiveSelector, activeSelector)
	}
	newStatus.BlueGreen.ActiveSelector = activeSelector

	newStatus.StableRS = c.rollout.Status.StableRS
	if c.shouldUpdateBlueGreenStable(newStatus) {
		c.log.Infof("Updating stable RS (%s -> %s)", newStatus.StableRS, newStatus.CurrentPodHash)
		newStatus.StableRS = newStatus.CurrentPodHash
		newStatus.PromoteFull = false

		// Now that we've marked the current RS as stable, start the scale-down countdown on the previous stable RS
		previousStableRS, _ := replicasetutil.GetReplicaSetByTemplateHash(c.olderRSs, c.rollout.Status.StableRS)
		if replicasetutil.GetReplicaCountForReplicaSets([]*appsv1.ReplicaSet{previousStableRS}) > 0 {
			err := c.addScaleDownDelay(previousStableRS)
			if err != nil {
				return err
			}
		}
	}

	activeRS, _ := replicasetutil.GetReplicaSetByTemplateHash(c.allRSs, newStatus.BlueGreen.ActiveSelector)
	if activeRS != nil {
		newStatus.HPAReplicas = activeRS.Status.Replicas
		newStatus.Selector = metav1.FormatLabelSelector(activeRS.Spec.Selector)
		newStatus.AvailableReplicas = activeRS.Status.AvailableReplicas
		newStatus.ReadyReplicas = activeRS.Status.ReadyReplicas
	} else {
		// when we do not have an active replicaset, accounting is done on the default rollout selector
		newStatus.HPAReplicas = replicasetutil.GetActualReplicaCountForReplicaSets(c.allRSs)
		newStatus.Selector = metav1.FormatLabelSelector(c.rollout.Spec.Selector)
		newStatus.AvailableReplicas = replicasetutil.GetAvailableReplicaCountForReplicaSets(c.allRSs)
		// NOTE: setting ready replicas is skipped since it's already performed in c.calculateBaseStatus() and is redundant
		// newStatus.ReadyReplicas = replicasetutil.GetReadyReplicaCountForReplicaSets(c.allRSs)
	}

	newStatus.BlueGreen.ScaleUpPreviewCheckPoint = c.calculateScaleUpPreviewCheckPoint(newStatus.StableRS)

	newStatus = c.calculateRolloutConditions(newStatus)
	return c.persistRolloutStatus(&newStatus)
}

// shouldUpdateBlueGreenStable makes a determination if the current ReplicaSet should be marked as
// the stable ReplicaSet (for a blue-green rollout). This is true if the active selector is
// pointing at the the current RS, and there are no outstanding post-promotion analysis.
func (c *rolloutContext) shouldUpdateBlueGreenStable(newStatus v1alpha1.RolloutStatus) bool {
	if c.rollout.Status.StableRS == newStatus.CurrentPodHash {
		return false
	}
	if newStatus.BlueGreen.ActiveSelector == "" {
		// corner case - initial deployments won't update the active selector until stable is set.
		// We must allow current to be marked stable, so that active can be marked to current, and
		// subsequently stable marked to current too. (chicken and egg problem)
		return true
	}
	if newStatus.BlueGreen.ActiveSelector != newStatus.CurrentPodHash {
		// haven't service performed cutover yet
		return false
	}
	if newStatus.PromoteFull {
		return true
	}
	if c.rollout.Spec.Strategy.BlueGreen.PostPromotionAnalysis != nil {
		// corner case - we fast-track the StableRS to be updated to CurrentPodHash when we are
		// moving to a ReplicaSet within scaleDownDelay and wish to skip analysis.
		if replicasetutil.HasScaleDownDeadline(c.newRS) {
			c.log.Infof("detected rollback to RS '%s' within scaleDownDelay. fast-tracking stable RS to %s", c.newRS.Name, newStatus.CurrentPodHash)
			return true
		}
		currentPostPromotionAnalysisRun := c.currentArs.BlueGreenPostPromotion
		if currentPostPromotionAnalysisRun == nil || currentPostPromotionAnalysisRun.Status.Phase != v1alpha1.AnalysisPhaseSuccessful {
			// we have yet to start post-promotion analysis or post-promotion was not successful
			return false
		}
	}
	return true
}

// calculateScaleUpPreviewCheckPoint calculates the correct value of status.blueGreen.scaleUpPreviewCheckPoint
// which is used by the blueGreen.previewReplicaCount feature. scaleUpPreviewCheckPoint is a single
// direction trip-wire, initialized to false, and gets flipped true as soon as the preview replicas
// matches scaleUpPreviewCheckPoint and prePromotionAnalysis (if used) completes. It get reset to
// false when the pod template changes, or the rollout fully promotes (stableRS == newRS)
func (c *rolloutContext) calculateScaleUpPreviewCheckPoint(stableRSHash string) bool {
	prevValue := c.rollout.Status.BlueGreen.ScaleUpPreviewCheckPoint
	if c.rollout.Spec.Strategy.BlueGreen.PreviewReplicaCount == nil {
		// previewReplicaCount feature is not being used
		return false
	}
	if c.rollout.Status.Abort && c.reconcileBlueGreenTemplateChange() {
		if prevValue {
			c.log.Infof("resetting scaleUpPreviewCheckPoint: post-abort template change detected")
		}
		return false
	}
	if c.newRS == nil || stableRSHash == "" || stableRSHash == replicasetutil.GetPodTemplateHash(c.newRS) {
		if prevValue {
			c.log.Infof("resetting scaleUpPreviewCheckPoint: rollout fully promoted")
		}
		return false
	}
	// Once the ScaleUpPreviewCheckPoint is set to true, the rollout should keep that value until
	// the newRS becomes the new stableRS or there is a template change.
	if c.rollout.Status.BlueGreen.ScaleUpPreviewCheckPoint {
		return c.rollout.Status.BlueGreen.ScaleUpPreviewCheckPoint
	}
	if !c.completedPrePromotionAnalysis() || !c.pauseContext.CompletedBlueGreenPause() {
		// do not set the checkpoint unless prePromotionAnalysis was successful and we completed our pause
		return false
	}
	previewCountAvailable := *c.rollout.Spec.Strategy.BlueGreen.PreviewReplicaCount == replicasetutil.GetAvailableReplicaCountForReplicaSets([]*appsv1.ReplicaSet{c.newRS})
	if prevValue != previewCountAvailable {
		c.log.Infof("setting scaleUpPreviewCheckPoint to %v: preview replica count availability is %v", previewCountAvailable, previewCountAvailable)
	}
	return previewCountAvailable
}
