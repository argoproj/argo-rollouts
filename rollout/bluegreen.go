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
func (c *Controller) rolloutBlueGreen(r *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet) error {
	previewSvc, activeSvc, err := c.getPreviewAndActiveServices(r)
	if err != nil {
		return err
	}
	newRS, oldRSs, err := c.getAllReplicaSetsAndSyncRevision(r, rsList, true)
	if err != nil {
		return err
	}

	arList, err := c.getAnalysisRunsForRollout(r)
	if err != nil {
		return err
	}

	roCtx := newBlueGreenCtx(r, newRS, oldRSs, arList)
	logCtx := roCtx.Log()

	if reconcileBlueGreenTemplateChange(roCtx) {
		roCtx.PauseContext().ClearPauseConditions()
		roCtx.PauseContext().RemoveAbort()
		roCtx.SetRestartedAt()
		logCtx.Infof("New pod template or template change detected")
		return c.syncRolloutStatusBlueGreen(previewSvc, activeSvc, roCtx)
	}

	err = c.podRestarter.Reconcile(roCtx)
	if err != nil {
		return err
	}

	err = c.reconcileBlueGreenReplicaSets(roCtx, activeSvc)
	if err != nil {
		return err
	}

	err = c.reconcilePreviewService(roCtx, previewSvc)
	if err != nil {
		return err
	}

	roCtx.log.Info("Reconciling pause")
	c.reconcileBlueGreenPause(activeSvc, previewSvc, roCtx)

	err = c.reconcileActiveService(roCtx, previewSvc, activeSvc)
	if err != nil {
		return err
	}

	err = c.reconcileAnalysisRuns(roCtx)
	if err != nil {
		return err
	}
	return c.syncRolloutStatusBlueGreen(previewSvc, activeSvc, roCtx)
}

func (c *Controller) reconcileStableReplicaSet(roCtx *blueGreenContext, activeSvc *corev1.Service) error {
	rollout := roCtx.Rollout()

	if _, ok := activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]; !ok {
		return nil
	}
	activeRS, _ := replicasetutil.GetReplicaSetByTemplateHash(roCtx.AllRSs(), activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
	if activeRS == nil {
		roCtx.Log().Warn("There shouldn't be a nil active replicaset if the active Service selector is set")
		return nil
	}

	roCtx.Log().Infof("Reconciling stable ReplicaSet '%s'", activeRS.Name)
	if _, ok := activeRS.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey]; ok {
		// SetScaleDownDeadlineAnnotation should be removed from the new RS to ensure a new value is set
		// when the active service changes to a different RS
		err := c.removeScaleDownDelay(roCtx, activeRS)
		if err != nil {
			return err
		}
	}
	_, _, err := c.scaleReplicaSetAndRecordEvent(activeRS, defaults.GetReplicasOrDefault(rollout.Spec.Replicas), rollout)
	return err
}

func (c *Controller) reconcileBlueGreenReplicaSets(roCtx *blueGreenContext, activeSvc *corev1.Service) error {
	logCtx := roCtx.Log()
	oldRSs := roCtx.OlderRSs()
	err := c.reconcileStableReplicaSet(roCtx, activeSvc)
	if err != nil {
		return err
	}
	_, err = c.reconcileNewReplicaSet(roCtx)
	if err != nil {
		return err
	}
	// Scale down old non-active replicasets, if we can.
	_, filteredOldRS := replicasetutil.GetReplicaSetByTemplateHash(oldRSs, activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
	logCtx.Info("Reconciling old replica sets")
	_, err = c.reconcileOldReplicaSets(controller.FilterActiveReplicaSets(filteredOldRS), roCtx)
	if err != nil {
		return err
	}
	logCtx.Info("Cleaning up old replicasets")
	if err := c.cleanupRollouts(filteredOldRS, roCtx); err != nil {
		return err
	}
	return nil
}

// reconcileBlueGreenTemplateChange returns true if we detect there was a change in the pod template
// from our current pod hash, or the newRS does not yet exist
func reconcileBlueGreenTemplateChange(roCtx *blueGreenContext) bool {
	r := roCtx.Rollout()
	newRS := roCtx.NewRS()
	if newRS == nil {
		return true
	}
	return r.Status.CurrentPodHash != newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
}

func skipPause(roCtx *blueGreenContext, activeSvc *corev1.Service) bool {
	rollout := roCtx.Rollout()
	newRS := roCtx.NewRS()
	if _, ok := newRS.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey]; ok {
		roCtx.log.Infof("Detected scale down annotation for ReplicaSet '%s' and will skip pause", newRS.Name)
		return true
	}

	// If a rollout has a PrePromotionAnalysis, the controller only skips the pause after the analysis passes
	if defaults.GetAutoPromotionEnabledOrDefault(rollout) && completedPrePromotionAnalysis(roCtx) {
		return true
	}

	if _, ok := activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]; !ok {
		return true
	}
	if activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] == newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] {
		return true
	}
	return false
}

func (c *Controller) reconcileBlueGreenPause(activeSvc, previewSvc *corev1.Service, roCtx *blueGreenContext) {
	rollout := roCtx.Rollout()
	newRS := roCtx.NewRS()

	if rollout.Status.Abort {
		return
	}

	allRSs := roCtx.AllRSs()
	if !replicasetutil.ReadyForPause(rollout, newRS, allRSs) {
		roCtx.log.Infof("New RS '%s' is not ready to pause", newRS.Name)
		return
	}
	if rollout.Spec.Paused {
		return
	}

	if skipPause(roCtx, activeSvc) {
		roCtx.PauseContext().RemovePauseCondition(v1alpha1.PauseReasonBlueGreenPause)
		return
	}

	newRSPodHash := newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	cond := getPauseCondition(rollout, v1alpha1.PauseReasonBlueGreenPause)
	// If the rollout is not paused and the active service is not point at the newRS, we should pause the rollout.
	if cond == nil && !rollout.Status.ControllerPause && !rollout.Status.BlueGreen.ScaleUpPreviewCheckPoint && activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] != newRSPodHash {
		roCtx.PauseContext().AddPauseCondition(v1alpha1.PauseReasonBlueGreenPause)
		return
	}

	autoPromoteActiveServiceDelaySeconds := rollout.Spec.Strategy.BlueGreen.AutoPromotionSeconds
	if autoPromoteActiveServiceDelaySeconds != nil && cond != nil {
		c.checkEnqueueRolloutDuringWait(rollout, cond.StartTime, *autoPromoteActiveServiceDelaySeconds)
		switchDeadline := cond.StartTime.Add(time.Duration(*autoPromoteActiveServiceDelaySeconds) * time.Second)
		now := metav1.Now()
		if now.After(switchDeadline) {
			roCtx.PauseContext().RemovePauseCondition(v1alpha1.PauseReasonBlueGreenPause)
		}
	}
}

// scaleDownOldReplicaSetsForBlueGreen scales down old replica sets when rollout strategy is "Blue Green".
func (c *Controller) scaleDownOldReplicaSetsForBlueGreen(oldRSs []*appsv1.ReplicaSet, roCtx *blueGreenContext) (bool, error) {
	rollout := roCtx.Rollout()
	logCtx := roCtx.Log()
	if getPauseCondition(rollout, v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		logCtx.Infof("Cannot scale down old ReplicaSets while paused with inconclusive Analysis ")
		return false, nil
	}
	if rollout.Spec.Strategy.BlueGreen.PostPromotionAnalysis != nil && rollout.Spec.Strategy.BlueGreen.ScaleDownDelaySeconds == nil {
		currentPostAr := roCtx.CurrentAnalysisRuns().BlueGreenPostPromotion
		if currentPostAr == nil || currentPostAr.Status.Phase != v1alpha1.AnalysisPhaseSuccessful {
			logCtx.Infof("Cannot scale down old ReplicaSets while Analysis is running and no ScaleDownDelaySeconds")
			return false, nil
		}
	}
	sort.Sort(sort.Reverse(replicasetutil.ReplicaSetsByRevisionNumber(oldRSs)))

	hasScaled := false
	annotationedRSs := int32(0)
	rolloutReplicas := defaults.GetReplicasOrDefault(rollout.Spec.Replicas)
	for _, targetRS := range oldRSs {
		desiredReplicaCount := int32(0)
		if scaleDownAtStr, ok := targetRS.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey]; ok {
			annotationedRSs++
			scaleDownAtTime, err := time.Parse(time.RFC3339, scaleDownAtStr)
			if err != nil {
				logCtx.Warnf("Unable to read scaleDownAt label on rs '%s'", targetRS.Name)
			} else if rollout.Spec.Strategy.BlueGreen.ScaleDownDelayRevisionLimit != nil && annotationedRSs == *rollout.Spec.Strategy.BlueGreen.ScaleDownDelayRevisionLimit {
				logCtx.Info("At ScaleDownDelayRevisionLimit and scaling down the rest")
			} else {
				now := metav1.Now()
				scaleDownAt := metav1.NewTime(scaleDownAtTime)
				if scaleDownAt.After(now.Time) {
					logCtx.Infof("RS '%s' has not reached the scaleDownTime", targetRS.Name)
					remainingTime := scaleDownAt.Sub(now.Time)
					if remainingTime < c.resyncPeriod {
						c.enqueueRolloutAfter(rollout, remainingTime)
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
		_, _, err := c.scaleReplicaSetAndRecordEvent(targetRS, desiredReplicaCount, rollout)
		if err != nil {
			return false, err
		}
		hasScaled = true
	}

	return hasScaled, nil
}

func (c *Controller) syncRolloutStatusBlueGreen(previewSvc *corev1.Service, activeSvc *corev1.Service, roCtx *blueGreenContext) error {
	r := roCtx.Rollout()
	newRS := roCtx.NewRS()
	oldRSs := roCtx.OlderRSs()
	allRSs := roCtx.AllRSs()
	newStatus := c.calculateBaseStatus(roCtx)

	if replicasetutil.CheckPodSpecChange(r, newRS) {
		roCtx.PauseContext().ClearPauseConditions()
		roCtx.PauseContext().RemoveAbort()
		roCtx.SetRestartedAt()
	}

	newStatus.AvailableReplicas = replicasetutil.GetAvailableReplicaCountForReplicaSets([]*appsv1.ReplicaSet{newRS})
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
	if newStatus.BlueGreen.ActiveSelector != r.Status.BlueGreen.ActiveSelector {
		previousActiveRS, _ := replicasetutil.GetReplicaSetByTemplateHash(oldRSs, r.Status.BlueGreen.ActiveSelector)
		if replicasetutil.GetReplicaCountForReplicaSets([]*appsv1.ReplicaSet{previousActiveRS}) > 0 {
			err := c.addScaleDownDelay(roCtx, previousActiveRS)
			if err != nil {
				return err
			}
		}
	}
	newStatus.StableRS = r.Status.StableRS
	scaledDownPreviousStableRS := newStatus.BlueGreen.ActiveSelector == newStatus.CurrentPodHash
	stableRSName := fmt.Sprintf("%s-%s", r.Name, newStatus.StableRS)
	for _, rs := range oldRSs {
		if *rs.Spec.Replicas != int32(0) && rs.Name == stableRSName {
			scaledDownPreviousStableRS = false
		}
	}
	postAnalysisRunFinished := false
	if r.Spec.Strategy.BlueGreen.PostPromotionAnalysis != nil {
		currentPostPromotionAnalysisRun := roCtx.CurrentAnalysisRuns().BlueGreenPostPromotion
		if currentPostPromotionAnalysisRun != nil {
			postAnalysisRunFinished = currentPostPromotionAnalysisRun.Status.Phase == v1alpha1.AnalysisPhaseSuccessful
		}
	}
	if scaledDownPreviousStableRS || newStatus.StableRS == "" || postAnalysisRunFinished {
		newStatus.StableRS = newStatus.CurrentPodHash
	}

	activeRS, _ := replicasetutil.GetReplicaSetByTemplateHash(allRSs, newStatus.BlueGreen.ActiveSelector)
	if activeRS != nil {
		newStatus.HPAReplicas = replicasetutil.GetActualReplicaCountForReplicaSets([]*appsv1.ReplicaSet{activeRS})
		newStatus.Selector = metav1.FormatLabelSelector(activeRS.Spec.Selector)
	} else {
		newStatus.HPAReplicas = replicasetutil.GetActualReplicaCountForReplicaSets(allRSs)
		newStatus.Selector = metav1.FormatLabelSelector(r.Spec.Selector)
	}

	newStatus.BlueGreen.ScaleUpPreviewCheckPoint = calculateScaleUpPreviewCheckPoint(roCtx, activeRS)

	newStatus = c.calculateRolloutConditions(roCtx, newStatus)
	return c.persistRolloutStatus(roCtx, &newStatus)
}

func calculateScaleUpPreviewCheckPoint(roCtx *blueGreenContext, activeRS *appsv1.ReplicaSet) bool {
	r := roCtx.Rollout()
	newRS := roCtx.NewRS()

	if r.Status.Abort && reconcileBlueGreenTemplateChange(roCtx) || r.Spec.Strategy.BlueGreen.PreviewReplicaCount == nil {
		return false
	}

	if newRS == nil || activeRS == nil || activeRS.Name == newRS.Name {
		return false
	}

	// Once the ScaleUpPreviewCheckPoint is set to true, the rollout should keep that value until
	// the newRS becomes the new activeRS or there is a template change.
	if r.Status.BlueGreen.ScaleUpPreviewCheckPoint {
		return r.Status.BlueGreen.ScaleUpPreviewCheckPoint
	}

	newRSAvailableCount := replicasetutil.GetAvailableReplicaCountForReplicaSets([]*appsv1.ReplicaSet{newRS})
	return newRSAvailableCount == *r.Spec.Strategy.BlueGreen.PreviewReplicaCount
}
