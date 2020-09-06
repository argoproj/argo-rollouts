package rollout

import (
	"fmt"
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
	previewSvc, activeSvc, err := c.getPreviewAndActiveServices(c.rollout)
	if err != nil {
		return err
	}
	err = c.getAllReplicaSetsAndSyncRevision(true)
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

	c.log.Info("Reconciling pause")
	c.reconcileBlueGreenPause(activeSvc, previewSvc)

	err = c.reconcileActiveService(previewSvc, activeSvc)
	if err != nil {
		return err
	}

	err = c.reconcileAnalysisRuns()
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
	if _, ok := activeRS.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey]; ok {
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
	// Scale down old non-active replicasets, if we can.
	_, filteredOldRS := replicasetutil.GetReplicaSetByTemplateHash(c.olderRSs, activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
	c.log.Info("Reconciling old replica sets")
	_, err = c.reconcileOldReplicaSets(controller.FilterActiveReplicaSets(filteredOldRS))
	if err != nil {
		return err
	}
	c.log.Info("Cleaning up old replicasets")
	if err := c.cleanupRollouts(filteredOldRS); err != nil {
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

func skipPause(c *rolloutContext, activeSvc *corev1.Service) bool {
	if _, ok := c.newRS.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey]; ok {
		c.log.Infof("Detected scale down annotation for ReplicaSet '%s' and will skip pause", c.newRS.Name)
		return true
	}

	// If a rollout has a PrePromotionAnalysis, the controller only skips the pause after the analysis passes
	if defaults.GetAutoPromotionEnabledOrDefault(c.rollout) && completedPrePromotionAnalysis(c) {
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
		return
	}

	if skipPause(c, activeSvc) {
		c.pauseContext.RemovePauseCondition(v1alpha1.PauseReasonBlueGreenPause)
		return
	}

	newRSPodHash := c.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	cond := getPauseCondition(c.rollout, v1alpha1.PauseReasonBlueGreenPause)
	// If the rollout is not paused and the active service is not point at the newRS, we should pause the rollout.
	if cond == nil && !c.rollout.Status.ControllerPause && !c.rollout.Status.BlueGreen.ScaleUpPreviewCheckPoint && activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] != newRSPodHash {
		c.pauseContext.AddPauseCondition(v1alpha1.PauseReasonBlueGreenPause)
		return
	}

	autoPromoteActiveServiceDelaySeconds := c.rollout.Spec.Strategy.BlueGreen.AutoPromotionSeconds
	if autoPromoteActiveServiceDelaySeconds != nil && cond != nil {
		c.checkEnqueueRolloutDuringWait(cond.StartTime, *autoPromoteActiveServiceDelaySeconds)
		switchDeadline := cond.StartTime.Add(time.Duration(*autoPromoteActiveServiceDelaySeconds) * time.Second)
		now := metav1.Now()
		if now.After(switchDeadline) {
			c.pauseContext.RemovePauseCondition(v1alpha1.PauseReasonBlueGreenPause)
		}
	}
}

// scaleDownOldReplicaSetsForBlueGreen scales down old replica sets when rollout strategy is "Blue Green".
func (c *rolloutContext) scaleDownOldReplicaSetsForBlueGreen(oldRSs []*appsv1.ReplicaSet) (bool, error) {
	if getPauseCondition(c.rollout, v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		c.log.Infof("Cannot scale down old ReplicaSets while paused with inconclusive Analysis ")
		return false, nil
	}
	if c.rollout.Spec.Strategy.BlueGreen.PostPromotionAnalysis != nil && c.rollout.Spec.Strategy.BlueGreen.ScaleDownDelaySeconds == nil && !needPostPromotionAnalysisRun(c.rollout, c.newRS) {
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
		desiredReplicaCount := int32(0)
		if scaleDownAtStr, ok := targetRS.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey]; ok {
			annotationedRSs++
			scaleDownAtTime, err := time.Parse(time.RFC3339, scaleDownAtStr)
			if err != nil {
				c.log.Warnf("Unable to read scaleDownAt label on rs '%s'", targetRS.Name)
			} else if c.rollout.Spec.Strategy.BlueGreen.ScaleDownDelayRevisionLimit != nil && annotationedRSs == *c.rollout.Spec.Strategy.BlueGreen.ScaleDownDelayRevisionLimit {
				c.log.Info("At ScaleDownDelayRevisionLimit and scaling down the rest")
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
	}

	newStatus.AvailableReplicas = replicasetutil.GetAvailableReplicaCountForReplicaSets([]*appsv1.ReplicaSet{c.newRS})
	previewSelector, ok := serviceutil.GetRolloutSelectorLabel(previewSvc)
	if !ok {
		previewSelector = ""
	}
	newStatus.BlueGreen.PreviewSelector = previewSelector
	activeSelector, ok := serviceutil.GetRolloutSelectorLabel(activeSvc)
	if !ok {
		activeSelector = ""
	}

	newStatus.BlueGreen.ActiveSelector = activeSelector
	if newStatus.BlueGreen.ActiveSelector != c.rollout.Status.BlueGreen.ActiveSelector {
		previousActiveRS, _ := replicasetutil.GetReplicaSetByTemplateHash(c.olderRSs, c.rollout.Status.BlueGreen.ActiveSelector)
		if replicasetutil.GetReplicaCountForReplicaSets([]*appsv1.ReplicaSet{previousActiveRS}) > 0 {
			err := c.addScaleDownDelay(previousActiveRS)
			if err != nil {
				return err
			}
		}
	}
	newStatus.StableRS = c.rollout.Status.StableRS
	scaledDownPreviousStableRS := newStatus.BlueGreen.ActiveSelector == newStatus.CurrentPodHash
	stableRSName := fmt.Sprintf("%s-%s", c.rollout.Name, newStatus.StableRS)
	for _, rs := range c.olderRSs {
		if *rs.Spec.Replicas != int32(0) && rs.Name == stableRSName {
			scaledDownPreviousStableRS = false
		}
	}
	postAnalysisRunFinished := false
	if c.rollout.Spec.Strategy.BlueGreen.PostPromotionAnalysis != nil {
		currentPostPromotionAnalysisRun := c.currentArs.BlueGreenPostPromotion
		if currentPostPromotionAnalysisRun != nil {
			postAnalysisRunFinished = currentPostPromotionAnalysisRun.Status.Phase == v1alpha1.AnalysisPhaseSuccessful
		}
	}
	if scaledDownPreviousStableRS || newStatus.StableRS == "" || postAnalysisRunFinished {
		newStatus.StableRS = newStatus.CurrentPodHash
	}

	activeRS, _ := replicasetutil.GetReplicaSetByTemplateHash(c.allRSs, newStatus.BlueGreen.ActiveSelector)
	if activeRS != nil {
		newStatus.HPAReplicas = replicasetutil.GetActualReplicaCountForReplicaSets([]*appsv1.ReplicaSet{activeRS})
		newStatus.Selector = metav1.FormatLabelSelector(activeRS.Spec.Selector)
	} else {
		newStatus.HPAReplicas = replicasetutil.GetActualReplicaCountForReplicaSets(c.allRSs)
		newStatus.Selector = metav1.FormatLabelSelector(c.rollout.Spec.Selector)
	}

	newStatus.BlueGreen.ScaleUpPreviewCheckPoint = c.calculateScaleUpPreviewCheckPoint(activeRS)

	newStatus = c.calculateRolloutConditions(newStatus)
	return c.persistRolloutStatus(&newStatus)
}

func (c *rolloutContext) calculateScaleUpPreviewCheckPoint(activeRS *appsv1.ReplicaSet) bool {
	if c.rollout.Status.Abort && c.reconcileBlueGreenTemplateChange() || c.rollout.Spec.Strategy.BlueGreen.PreviewReplicaCount == nil {
		return false
	}

	if c.newRS == nil || activeRS == nil || activeRS.Name == c.newRS.Name {
		return false
	}

	// Once the ScaleUpPreviewCheckPoint is set to true, the rollout should keep that value until
	// the newRS becomes the new activeRS or there is a template change.
	if c.rollout.Status.BlueGreen.ScaleUpPreviewCheckPoint {
		return c.rollout.Status.BlueGreen.ScaleUpPreviewCheckPoint
	}

	newRSAvailableCount := replicasetutil.GetAvailableReplicaCountForReplicaSets([]*appsv1.ReplicaSet{c.newRS})
	return newRSAvailableCount == *c.rollout.Spec.Strategy.BlueGreen.PreviewReplicaCount
}
