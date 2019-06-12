package controller

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
)

// rolloutBlueGreen implements the logic for rolling a new replica set.
func (c *Controller) rolloutBlueGreen(r *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet) error {
	logCtx := logutil.WithRollout(r)
	previewSvc, activeSvc, err := c.getPreviewAndActiveServices(r)
	if err != nil {
		return err
	}
	newRS, oldRSs, err := c.getAllReplicaSetsAndSyncRevision(r, rsList, true)
	if err != nil {
		return err
	}
	if reconcileBlueGreenTemplateChange(r, newRS) {
		logCtx.Infof("New pod template or template change detected")
		return c.syncRolloutStatusBlueGreen(oldRSs, newRS, previewSvc, activeSvc, r, false)
	}
	allRSs := append(oldRSs, newRS)

	// Scale up, if we can.
	logCtx.Infof("Reconciling new ReplicaSet '%s'", newRS.Name)
	scaledUp, err := c.reconcileNewReplicaSet(allRSs, newRS, r)
	if err != nil {
		return err
	}
	// Scale down old non-active replicasets, if we can.
	_, filteredOldRS := replicasetutil.GetReplicaSetByTemplateHash(oldRSs, activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
	if !c.scaleDownPreviousActiveRS(r) {
		logCtx.Info("Filtering out previous active RS while reconciling old replica sets")
		_, filteredOldRS = replicasetutil.GetReplicaSetByTemplateHash(filteredOldRS, r.Status.BlueGreen.PreviousActiveSelector)
	}
	logCtx.Info("Reconciling old replica sets")
	scaledDown, err := c.reconcileOldReplicaSets(controller.FilterActiveReplicaSets(filteredOldRS), newRS, r)
	if err != nil {
		return err
	}
	logCtx.Info("Cleaning up old replicasets")
	if err := c.cleanupRollouts(filteredOldRS, r); err != nil {
		return err
	}

	switchPreviewSvc, err := c.reconcilePreviewService(r, newRS, previewSvc, activeSvc)
	if err != nil {
		return err
	}

	if scaledUp {
		logCtx.Infof("Not finished reconciling new ReplicaSet '%s'", newRS.Name)
		return c.syncRolloutStatusBlueGreen(oldRSs, newRS, previewSvc, activeSvc, r, false)
	}

	if scaledDown {
		logCtx.Info("Not finished reconciling old replica sets")
		return c.syncRolloutStatusBlueGreen(oldRSs, newRS, previewSvc, activeSvc, r, false)
	}
	if switchPreviewSvc {
		logCtx.Infof("Not finished reconciling preview service' %s'", previewSvc.Name)
		return c.syncRolloutStatusBlueGreen(oldRSs, newRS, previewSvc, activeSvc, r, false)
	}

	if !replicasetutil.ReadyForPause(r, newRS, allRSs) {
		logutil.WithRollout(r).Infof("New RS '%s' is not fully saturated", newRS.Name)
		return c.syncRolloutStatusBlueGreen(oldRSs, newRS, previewSvc, activeSvc, r, false)
	}

	if !defaults.GetAutoPromotionEnabledOrDefault(r) {
		logCtx.Info("Reconciling pause")
		pauseBeforeSwitchActive := c.reconcileBlueGreenPause(activeSvc, previewSvc, r, newRS)
		if pauseBeforeSwitchActive {
			logCtx.Info("Not finished reconciling pause")
			return c.syncRolloutStatusBlueGreen(oldRSs, newRS, previewSvc, activeSvc, r, true)
		}
	}

	logCtx.Infof("Reconciling active service '%s'", activeSvc.Name)
	if !annotations.IsSaturated(r, newRS) {
		logutil.WithRollout(r).Infof("New RS '%s' is not fully saturated", newRS.Name)
		return c.syncRolloutStatusBlueGreen(oldRSs, newRS, previewSvc, activeSvc, r, false)
	}
	switchActiveSvc, err := c.reconcileActiveService(r, newRS, previewSvc, activeSvc)
	if err != nil {
		return err
	}
	if switchActiveSvc {
		logCtx.Infof("Not finished reconciling active service '%s'", activeSvc.Name)
		return c.syncRolloutStatusBlueGreen(oldRSs, newRS, previewSvc, activeSvc, r, false)
	}

	return c.syncRolloutStatusBlueGreen(oldRSs, newRS, previewSvc, activeSvc, r, false)
}

// reconcileBlueGreenTemplateChange returns true if we detect there was a change in the pod template
// from our current pod hash.
func reconcileBlueGreenTemplateChange(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet) bool {
	return rollout.Status.CurrentPodHash != newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
}

func (c *Controller) scaleDownPreviousActiveRS(rollout *v1alpha1.Rollout) bool {
	if rollout.Status.BlueGreen.ScaleDownDelayStartTime == nil {
		return true
	}
	scaleDownDelaySeconds := defaults.GetScaleDownDelaySecondsOrDefault(rollout)
	c.checkEnqueueRolloutDuringWait(rollout, *rollout.Status.BlueGreen.ScaleDownDelayStartTime, scaleDownDelaySeconds)
	pauseEnd := metav1.NewTime(rollout.Status.BlueGreen.ScaleDownDelayStartTime.Add(time.Duration(scaleDownDelaySeconds) * time.Second))
	now := metav1.Now()
	return now.After(pauseEnd.Time)
}

func (c *Controller) reconcileBlueGreenPause(activeSvc, previewSvc *corev1.Service, rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet) bool {
	newRSPodHash := newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	if _, ok := activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]; !ok {
		return false
	}
	// If the rollout is not paused and the active service is not point at the newRS, we should pause the rollout.
	if !rollout.Spec.Paused && rollout.Status.PauseStartTime == nil && !rollout.Status.BlueGreen.ScaleUpPreviewCheckPoint && activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] != newRSPodHash {
		return true
	}

	pauseStartTime := rollout.Status.PauseStartTime
	autoPromoteActiveServiceDelaySeconds := rollout.Spec.Strategy.BlueGreenStrategy.AutoPromotionSeconds
	if autoPromoteActiveServiceDelaySeconds != nil && pauseStartTime != nil {
		c.checkEnqueueRolloutDuringWait(rollout, *pauseStartTime, *autoPromoteActiveServiceDelaySeconds)
	}

	return rollout.Spec.Paused && pauseStartTime != nil
}

// scaleDownOldReplicaSetsForBlueGreen scales down old replica sets when rollout strategy is "Blue Green".
func (c *Controller) scaleDownOldReplicaSetsForBlueGreen(oldRSs []*appsv1.ReplicaSet, rollout *v1alpha1.Rollout) (int32, error) {
	sort.Sort(controller.ReplicaSetsByCreationTimestamp(oldRSs))

	totalScaledDown := int32(0)
	for _, targetRS := range oldRSs {
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

func (c *Controller) syncRolloutStatusBlueGreen(oldRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, previewSvc *corev1.Service, activeSvc *corev1.Service, r *v1alpha1.Rollout, addPause bool) error {
	allRSs := append(oldRSs, newRS)
	newStatus := c.calculateBaseStatus(allRSs, newRS, r)
	previewSelector, ok := c.getRolloutSelectorLabel(previewSvc)
	if !ok {
		previewSelector = ""
	}
	newStatus.BlueGreen.PreviewSelector = previewSelector
	activeSelector, ok := c.getRolloutSelectorLabel(activeSvc)
	if !ok {
		activeSelector = ""
	}
	newStatus.BlueGreen.ActiveSelector = activeSelector

	newStatus.BlueGreen.PreviousActiveSelector = r.Status.BlueGreen.PreviousActiveSelector
	newStatus.BlueGreen.ScaleDownDelayStartTime = r.Status.BlueGreen.ScaleDownDelayStartTime
	if newStatus.BlueGreen.ActiveSelector != r.Status.BlueGreen.ActiveSelector {
		previousActiveRS, _ := replicasetutil.GetReplicaSetByTemplateHash(oldRSs, r.Status.BlueGreen.ActiveSelector)
		if replicasetutil.GetReplicaCountForReplicaSets([]*appsv1.ReplicaSet{previousActiveRS}) > 0 {
			newStatus.BlueGreen.PreviousActiveSelector = r.Status.BlueGreen.ActiveSelector
			now := metav1.Now()
			newStatus.BlueGreen.ScaleDownDelayStartTime = &now
		}
	} else if c.scaleDownPreviousActiveRS(r) {
		newStatus.BlueGreen.PreviousActiveSelector = ""
		newStatus.BlueGreen.ScaleDownDelayStartTime = nil
	}

	activeRS, _ := replicasetutil.GetReplicaSetByTemplateHash(allRSs, newStatus.BlueGreen.ActiveSelector)
	if activeRS != nil {
		newStatus.HPAReplicas = replicasetutil.GetActualReplicaCountForReplicaSets([]*appsv1.ReplicaSet{activeRS})
		newStatus.Selector = metav1.FormatLabelSelector(activeRS.Spec.Selector)
	} else {
		newStatus.HPAReplicas = replicasetutil.GetActualReplicaCountForReplicaSets(allRSs)
		newStatus.Selector = metav1.FormatLabelSelector(r.Spec.Selector)
	}

	pauseStartTime, paused := calculatePauseStatus(r, newRS, addPause)
	newStatus.PauseStartTime = pauseStartTime
	newStatus.BlueGreen.ScaleUpPreviewCheckPoint = calculateScaleUpPreviewCheckPoint(r, newRS, activeRS)
	newStatus = c.calculateRolloutConditions(r, newStatus, allRSs, newRS)
	return c.persistRolloutStatus(r, &newStatus, &paused)
}

func calculateScaleUpPreviewCheckPoint(r *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, activeRS *appsv1.ReplicaSet) bool {
	newRSAvailableCount := replicasetutil.GetAvailableReplicaCountForReplicaSets([]*appsv1.ReplicaSet{newRS})
	if r.Spec.Strategy.BlueGreenStrategy.PreviewReplicaCount != nil && newRSAvailableCount == *r.Spec.Strategy.BlueGreenStrategy.PreviewReplicaCount {
		return true
	} else if reconcileBlueGreenTemplateChange(r, newRS) {
		return false
	} else if newRS != nil && activeRS != nil && activeRS.Name == newRS.Name {
		return false
	}
	return r.Status.BlueGreen.ScaleUpPreviewCheckPoint
}

// Should run only on scaling events and not during the normal rollout process.
func (c *Controller) scaleBlueGreen(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, oldRSs []*appsv1.ReplicaSet, previewSvc *corev1.Service, activeSvc *corev1.Service) error {
	rolloutReplicas := defaults.GetRolloutReplicasOrDefault(rollout)
	previewSelector, ok := c.getRolloutSelectorLabel(previewSvc)
	if !ok {
		previewSelector = ""
	}
	activeSelector, ok := c.getRolloutSelectorLabel(activeSvc)
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
		if rollout.Spec.Strategy.BlueGreenStrategy.PreviewReplicaCount != nil && !rollout.Status.BlueGreen.ScaleUpPreviewCheckPoint {
			previewReplicas = *rollout.Spec.Strategy.BlueGreenStrategy.PreviewReplicaCount
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
