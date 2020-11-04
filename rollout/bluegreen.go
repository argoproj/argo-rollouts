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
	if replicasetutil.HasScaleDownDeadline(activeRS) {
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
	otherRSs := replicasetutil.GetOlderRSs(roCtx.rollout, roCtx.newRS, roCtx.stableRS, roCtx.allRSs)
	err := c.reconcileStableReplicaSet(roCtx, activeSvc)
	if err != nil {
		return err
	}
	_, err = c.reconcileNewReplicaSet(roCtx)
	if err != nil {
		return err
	}

	// Scale down old non-active, non-stable replicasets, if we can.
	_, err = c.reconcileOldReplicaSets(controller.FilterActiveReplicaSets(otherRSs), roCtx)
	if err != nil {
		return err
	}
	if err := c.cleanupRollouts(otherRSs, roCtx); err != nil {
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
	if replicasetutil.HasScaleDownDeadline(newRS) {
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
	newRS := roCtx.NewRS()
	logCtx := roCtx.Log()
	if getPauseCondition(rollout, v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		logCtx.Infof("Cannot scale down old ReplicaSets while paused with inconclusive Analysis ")
		return false, nil
	}
	if rollout.Spec.Strategy.BlueGreen.PostPromotionAnalysis != nil && rollout.Spec.Strategy.BlueGreen.ScaleDownDelaySeconds == nil && !skipPostPromotionAnalysisRun(rollout, newRS) {
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
		if replicasetutil.IsStillReferenced(rollout.Status, targetRS) {
			// We should technically never get here because we shouldn't be passing a replicaset list
			// which includes referenced ReplicaSets. But we check just in case
			logCtx.Warnf("Prevented inadvertent scaleDown of RS '%s'", targetRS.Name)
			continue
		}

		desiredReplicaCount := int32(0)
		if scaleDownAtStr, ok := targetRS.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey]; ok {
			annotationedRSs++
			scaleDownAtTime, err := time.Parse(time.RFC3339, scaleDownAtStr)
			if err != nil {
				logCtx.Warnf("Unable to read scaleDownAt label on rs '%s'", targetRS.Name)
			} else if rollout.Spec.Strategy.BlueGreen.ScaleDownDelayRevisionLimit != nil && annotationedRSs > *rollout.Spec.Strategy.BlueGreen.ScaleDownDelayRevisionLimit {
				logCtx.Infof("At ScaleDownDelayRevisionLimit (%d) and scaling down the rest", *rollout.Spec.Strategy.BlueGreen.ScaleDownDelayRevisionLimit)
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
	logCtx := roCtx.Log()

	if replicasetutil.CheckPodSpecChange(r, newRS) {
		roCtx.PauseContext().ClearPauseConditions()
		roCtx.PauseContext().RemoveAbort()
		roCtx.SetRestartedAt()
		newStatus.BlueGreen.PrePromotionAnalysisRunStatus = nil
		newStatus.BlueGreen.PostPromotionAnalysisRunStatus = nil
	}

	previewSelector := serviceutil.GetRolloutSelectorLabel(previewSvc)
	if previewSelector != r.Status.BlueGreen.PreviewSelector {
		logCtx.Infof("Updating preview selector (%s -> %s)", r.Status.BlueGreen.PreviewSelector, previewSelector)
	}
	newStatus.BlueGreen.PreviewSelector = previewSelector

	activeSelector := serviceutil.GetRolloutSelectorLabel(activeSvc)
	if activeSelector != r.Status.BlueGreen.ActiveSelector {
		logCtx.Infof("Updating active selector (%s -> %s)", r.Status.BlueGreen.ActiveSelector, activeSelector)
	}
	newStatus.BlueGreen.ActiveSelector = activeSelector

	newStatus.StableRS = r.Status.StableRS
	if c.shouldUpdateBlueGreenStable(roCtx, newStatus) {
		logCtx.Infof("Updating stable RS (%s -> %s)", newStatus.StableRS, newStatus.CurrentPodHash)
		newStatus.StableRS = newStatus.CurrentPodHash

		// Now that we've marked the current RS as stable, start the scale-down countdown on the previous stable RS
		previousStableRS, _ := replicasetutil.GetReplicaSetByTemplateHash(oldRSs, r.Status.StableRS)
		if replicasetutil.GetReplicaCountForReplicaSets([]*appsv1.ReplicaSet{previousStableRS}) > 0 {
			err := c.addScaleDownDelay(roCtx, previousStableRS)
			if err != nil {
				return err
			}
		}
	}

	activeRS, _ := replicasetutil.GetReplicaSetByTemplateHash(allRSs, newStatus.BlueGreen.ActiveSelector)
	if activeRS != nil {
		newStatus.HPAReplicas = activeRS.Status.Replicas
		newStatus.Selector = metav1.FormatLabelSelector(activeRS.Spec.Selector)
		newStatus.AvailableReplicas = activeRS.Status.AvailableReplicas
		newStatus.ReadyReplicas = activeRS.Status.ReadyReplicas
	} else {
		// when we do not have an active replicaset, accounting is done on the default rollout selector
		newStatus.HPAReplicas = replicasetutil.GetActualReplicaCountForReplicaSets(allRSs)
		newStatus.Selector = metav1.FormatLabelSelector(r.Spec.Selector)
		newStatus.AvailableReplicas = replicasetutil.GetAvailableReplicaCountForReplicaSets(allRSs)
		// NOTE: setting ready replicas is skipped since it's already performed in c.calculateBaseStatus() and is redundant
		// newStatus.ReadyReplicas = replicasetutil.GetReadyReplicaCountForReplicaSets(c.allRSs)
	}

	newStatus.BlueGreen.ScaleUpPreviewCheckPoint = calculateScaleUpPreviewCheckPoint(roCtx, newStatus.StableRS)

	newStatus = c.calculateRolloutConditions(roCtx, newStatus)
	return c.persistRolloutStatus(roCtx, &newStatus)
}

// shouldUpdateBlueGreenStable makes a determination if the current ReplicaSet should be marked as
// the stable ReplicaSet (for a blue-green rollout). This is true if the active selector is
// pointing at the the current RS, and there are no outstanding post-promotion analysis.
func (c *Controller) shouldUpdateBlueGreenStable(roCtx *blueGreenContext, newStatus v1alpha1.RolloutStatus) bool {
	rollout := roCtx.Rollout()
	logCtx := roCtx.Log()
	newRS := roCtx.NewRS()
	if rollout.Status.StableRS == newStatus.CurrentPodHash {
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
	if rollout.Spec.Strategy.BlueGreen.PostPromotionAnalysis != nil {
		// corner case - we fast-track the StableRS to be updated to CurrentPodHash when we are
		// moving to a ReplicaSet within scaleDownDelay and wish to skip analysis.
		if replicasetutil.HasScaleDownDeadline(newRS) {
			logCtx.Infof("detected rollback to RS '%s' within scaleDownDelay. fast-tracking stable RS to %s", newRS.Name, newStatus.CurrentPodHash)
			return true
		}
		currentPostPromotionAnalysisRun := roCtx.currentArs.BlueGreenPostPromotion
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
func calculateScaleUpPreviewCheckPoint(roCtx *blueGreenContext, stableRSHash string) bool {
	rollout := roCtx.Rollout()
	logCtx := roCtx.Log()
	newRS := roCtx.NewRS()
	prevValue := rollout.Status.BlueGreen.ScaleUpPreviewCheckPoint
	if rollout.Spec.Strategy.BlueGreen.PreviewReplicaCount == nil {
		// previewReplicaCount feature is not being used
		return false
	}
	if rollout.Status.Abort && reconcileBlueGreenTemplateChange(roCtx) {
		if prevValue {
			logCtx.Infof("resetting scaleUpPreviewCheckPoint: post-abort template change detected")
		}
		return false
	}
	if newRS == nil || stableRSHash == "" || stableRSHash == replicasetutil.GetPodTemplateHash(newRS) {
		if prevValue {
			logCtx.Infof("resetting scaleUpPreviewCheckPoint: rollout fully promoted")
		}
		return false
	}
	// Once the ScaleUpPreviewCheckPoint is set to true, the rollout should keep that value until
	// the newRS becomes the new activeRS or there is a template change.
	if rollout.Status.BlueGreen.ScaleUpPreviewCheckPoint {
		return rollout.Status.BlueGreen.ScaleUpPreviewCheckPoint
	}
	if !completedPrePromotionAnalysis(roCtx) {
		// do not set the checkpoint unless prePromotion was successful
		return false
	}
	previewCountAvailable := *rollout.Spec.Strategy.BlueGreen.PreviewReplicaCount == replicasetutil.GetAvailableReplicaCountForReplicaSets([]*appsv1.ReplicaSet{newRS})
	if prevValue != previewCountAvailable {
		logCtx.Infof("setting scaleUpPreviewCheckPoint to %v: preview replica count availability is %v", previewCountAvailable, previewCountAvailable)
	}
	return previewCountAvailable
}
