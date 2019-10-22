package rollout

import (
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	serviceutil "github.com/argoproj/argo-rollouts/utils/service"
)

// rolloutBlueGreen implements the logic for rolling a new replica set.
func (c *RolloutController) rolloutBlueGreen(r *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet) error {
	previewSvc, activeSvc, err := c.getPreviewAndActiveServices(r)
	if err != nil {
		return err
	}
	newRS, oldRSs, err := c.getAllReplicaSetsAndSyncRevision(r, rsList, true)
	if err != nil {
		return err
	}

	roCtx := newBlueGreenCtx(r, newRS, oldRSs)
	logCtx := roCtx.Log()
	allRSs := roCtx.AllRSs()
	if reconcileBlueGreenTemplateChange(roCtx) {
		roCtx.PauseContext().ClearPauseConditions()
		logCtx.Infof("New pod template or template change detected")
		return c.syncRolloutStatusBlueGreen(previewSvc, activeSvc, roCtx)
	}

	// Scale up, if we can.
	logCtx.Infof("Reconciling new ReplicaSet '%s'", newRS.Name)
	scaledUp, err := c.reconcileNewReplicaSet(roCtx)
	if err != nil {
		return err
	}
	// Scale down old non-active replicasets, if we can.
	_, filteredOldRS := replicasetutil.GetReplicaSetByTemplateHash(oldRSs, activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
	logCtx.Info("Reconciling old replica sets")
	scaledDown, err := c.reconcileOldReplicaSets(controller.FilterActiveReplicaSets(filteredOldRS), roCtx)
	if err != nil {
		return err
	}
	logCtx.Info("Cleaning up old replicasets")
	if err := c.cleanupRollouts(filteredOldRS, roCtx); err != nil {
		return err
	}

	switchPreviewSvc, err := c.reconcilePreviewService(roCtx, previewSvc, activeSvc)
	if err != nil {
		return err
	}

	if scaledUp {
		logCtx.Infof("Not finished reconciling new ReplicaSet '%s'", newRS.Name)
		return c.syncRolloutStatusBlueGreen(previewSvc, activeSvc, roCtx)
	}

	if scaledDown {
		logCtx.Info("Not finished reconciling old replica sets")
		return c.syncRolloutStatusBlueGreen(previewSvc, activeSvc, roCtx)
	}
	if switchPreviewSvc {
		logCtx.Infof("Not finished reconciling preview service' %s'", previewSvc.Name)
		return c.syncRolloutStatusBlueGreen(previewSvc, activeSvc, roCtx)
	}

	if !replicasetutil.ReadyForPause(r, newRS, allRSs) {
		logutil.WithRollout(r).Infof("New RS '%s' is not fully saturated", newRS.Name)
		return c.syncRolloutStatusBlueGreen(previewSvc, activeSvc, roCtx)
	}

	noFastRollback := true
	if newRS != nil {
		_, hasScaleDownDeadlineAnnotationKey := newRS.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey]
		if hasScaleDownDeadlineAnnotationKey {
			logCtx.Infof("Detected scale down annotation for ReplicaSet '%s' and will skip pause", newRS.Name)
			noFastRollback = false
		}
	}
	if noFastRollback {
		logCtx.Info("Reconciling pause")
		pauseBeforeSwitchActive := c.reconcileBlueGreenPause(activeSvc, previewSvc, roCtx)
		if pauseBeforeSwitchActive {
			logCtx.Info("Not finished reconciling pause")
			return c.syncRolloutStatusBlueGreen(previewSvc, activeSvc, roCtx)
		}
	}

	logCtx.Infof("Reconciling active service '%s'", activeSvc.Name)
	if !annotations.IsSaturated(r, newRS) {
		logCtx.Infof("New RS '%s' is not fully saturated", newRS.Name)
		return c.syncRolloutStatusBlueGreen(previewSvc, activeSvc, roCtx)
	}
	switchActiveSvc, err := c.reconcileActiveService(roCtx, previewSvc, activeSvc)
	if err != nil {
		return err
	}
	if switchActiveSvc {
		logCtx.Infof("Not finished reconciling active service '%s'", activeSvc.Name)
		return c.syncRolloutStatusBlueGreen(previewSvc, activeSvc, roCtx)
	}

	if _, ok := newRS.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey]; ok {
		// SetScaleDownDeadlineAnnotation should be removed from the new RS to ensure a new value is set
		// when the active service changes to a different RS
		err := c.removeScaleDownDelay(roCtx, newRS)
		if err != nil {
			return err
		}
	}

	return c.syncRolloutStatusBlueGreen(previewSvc, activeSvc, roCtx)
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

func (c *RolloutController) reconcileBlueGreenPause(activeSvc, previewSvc *corev1.Service, roCtx *blueGreenContext) bool {
	rollout := roCtx.Rollout()
	if rollout.Spec.Paused {
		return false
	}

	if defaults.GetAutoPromotionEnabledOrDefault(rollout) {
		return false
	}

	newRS := roCtx.NewRS()
	newRSPodHash := newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	if _, ok := activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]; !ok {
		return false
	}

	cond := roCtx.PauseContext().GetPauseCondition(v1alpha1.PauseReasonBlueGreenPause)
	// If the rollout is not paused and the active service is not point at the newRS, we should pause the rollout.
	if cond == nil && !rollout.Status.ControllerPause && !rollout.Status.BlueGreen.ScaleUpPreviewCheckPoint && activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] != newRSPodHash {
		roCtx.PauseContext().AddPauseCondition(v1alpha1.PauseReasonBlueGreenPause)
		return true
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

	return cond != nil && rollout.Status.ControllerPause
}

// scaleDownOldReplicaSetsForBlueGreen scales down old replica sets when rollout strategy is "Blue Green".
func (c *RolloutController) scaleDownOldReplicaSetsForBlueGreen(oldRSs []*appsv1.ReplicaSet, rollout *v1alpha1.Rollout) (int32, error) {
	logCtx := logutil.WithRollout(rollout)
	sort.Sort(sort.Reverse(replicasetutil.ReplicaSetsByRevisionNumber(oldRSs)))

	totalScaledDown := int32(0)
	annotationedRSs := int32(0)
	for _, targetRS := range oldRSs {
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
					continue
				}
			}
		}
		if *(targetRS.Spec.Replicas) == 0 {
			// cannot scale down this ReplicaSet.
			continue
		}
		scaleDownCount := *(targetRS.Spec.Replicas)
		// Scale down.
		newReplicasCount := int32(0)
		_, _, err := c.scaleReplicaSetAndRecordEvent(targetRS, newReplicasCount, rollout)
		if err != nil {
			return totalScaledDown, err
		}

		totalScaledDown += scaleDownCount
	}

	return totalScaledDown, nil
}

func (c *RolloutController) syncRolloutStatusBlueGreen(previewSvc *corev1.Service, activeSvc *corev1.Service, roCtx *blueGreenContext) error {
	r := roCtx.Rollout()
	newRS := roCtx.NewRS()
	oldRSs := roCtx.OlderRSs()
	allRSs := roCtx.AllRSs()
	newStatus := c.calculateBaseStatus(allRSs, newRS, r)
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
	newRSAvailableCount := replicasetutil.GetAvailableReplicaCountForReplicaSets([]*appsv1.ReplicaSet{newRS})
	if r.Spec.Strategy.BlueGreen.PreviewReplicaCount != nil && newRSAvailableCount == *r.Spec.Strategy.BlueGreen.PreviewReplicaCount {
		return true
	} else if reconcileBlueGreenTemplateChange(roCtx) {
		return false
	} else if newRS != nil && activeRS != nil && activeRS.Name == newRS.Name {
		return false
	}
	return r.Status.BlueGreen.ScaleUpPreviewCheckPoint
}

// Should run only on scaling events and not during the normal rollout process.
func (c *RolloutController) scaleBlueGreen(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, oldRSs []*appsv1.ReplicaSet, previewSvc *corev1.Service, activeSvc *corev1.Service) error {
	rolloutReplicas := defaults.GetRolloutReplicasOrDefault(rollout)
	previewSelector, ok := serviceutil.GetRolloutSelectorLabel(previewSvc)
	if !ok {
		previewSelector = ""
	}
	activeSelector, ok := serviceutil.GetRolloutSelectorLabel(activeSvc)
	if !ok {
		activeSelector = ""
	}

	// If there is only one replica set with pods, then we should scale that up to the full count of the
	// rollout. If there is no replica set with pods, then we should scale up the newest replica set.
	if activeOrLatest := replicasetutil.FindActiveOrLatest(newRS, oldRSs); activeOrLatest != nil {
		if *(activeOrLatest.Spec.Replicas) != rolloutReplicas {
			_, _, err := c.scaleReplicaSetAndRecordEvent(activeOrLatest, rolloutReplicas, rollout)
			return err
		}
	}

	allRS := append([]*appsv1.ReplicaSet{newRS}, oldRSs...)
	activeRS, _ := replicasetutil.GetReplicaSetByTemplateHash(allRS, rollout.Status.BlueGreen.ActiveSelector)
	if activeRS != nil {
		if *(activeRS.Spec.Replicas) != rolloutReplicas {
			_, _, err := c.scaleReplicaSetAndRecordEvent(activeRS, rolloutReplicas, rollout)
			return err
		}
	}

	previewRS, _ := replicasetutil.GetReplicaSetByTemplateHash(allRS, rollout.Status.BlueGreen.PreviewSelector)
	if previewRS != nil {
		previewReplicas := rolloutReplicas
		if rollout.Spec.Strategy.BlueGreen.PreviewReplicaCount != nil && !rollout.Status.BlueGreen.ScaleUpPreviewCheckPoint {
			previewReplicas = *rollout.Spec.Strategy.BlueGreen.PreviewReplicaCount
		}
		if *(previewRS.Spec.Replicas) != previewReplicas {
			_, _, err := c.scaleReplicaSetAndRecordEvent(previewRS, previewReplicas, rollout)
			return err
		}
	}

	if newRS != nil {
		newRSReplicaCount, err := replicasetutil.NewRSNewReplicas(rollout, allRS, newRS)
		if err != nil {
			return err
		}
		if *(newRS.Spec.Replicas) != newRSReplicaCount {
			_, _, err := c.scaleReplicaSetAndRecordEvent(newRS, newRSReplicaCount, rollout)
			return err
		}
	}

	// Old replica sets should be fully scaled down if they aren't receiving traffic from the active or
	// preview service. This case handles replica set adoption during a saturated new replica set.
	for _, old := range controller.FilterActiveReplicaSets(oldRSs) {
		oldLabel, ok := old.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		if !ok || (oldLabel != activeSelector && oldLabel != previewSelector) {
			if _, _, err := c.scaleReplicaSetAndRecordEvent(old, 0, rollout); err != nil {
				return err
			}
		}
	}
	return nil
}
