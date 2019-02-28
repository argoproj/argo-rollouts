package controller

import (
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

const (
	verifyingPreviewPatch = `{
	"status": {
		"verifyingPreview": true
	}
}`
)

// rolloutBlueGreen implements the logic for rolling a new replica set.
func (c *Controller) rolloutBlueGreen(r *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet) error {
	logCtx := logutil.WithRollout(r)
	newRS, oldRSs, err := c.getAllReplicaSetsAndSyncRevision(r, rsList, true)
	if err != nil {
		return err
	}
	previewSvc, activeSvc, err := c.getPreviewAndActiveServices(r)
	if err != nil {
		return err
	}
	allRSs := append(oldRSs, newRS)

	// Scale up, if we can.
	logCtx.Infof("Reconciling new ReplicaSet '%s'", newRS.Name)
	scaledUp, err := c.reconcileNewReplicaSet(allRSs, newRS, r)
	if err != nil {
		return err
	}
	if scaledUp {
		logCtx.Infof("Not finished reconciling new ReplicaSet '%s'", newRS.Name)
		return c.syncRolloutStatusBlueGreen(allRSs, newRS, previewSvc, activeSvc, r)
	}

	if previewSvc != nil {
		logCtx.Infof("Reconciling preview service '%s'", previewSvc.Name)
		switchPreviewSvc, err := c.reconcilePreviewService(r, newRS, previewSvc, activeSvc)
		if err != nil {
			return err
		}
		if switchPreviewSvc {
			logCtx.Infof("Not finished reconciling preview service' %s'", previewSvc.Name)
			return c.syncRolloutStatusBlueGreen(allRSs, newRS, previewSvc, activeSvc, r)
		}
		logCtx.Info("Reconciling verifying preview service")
		verfyingPreview := c.reconcileVerifyingPreview(activeSvc, r)
		if verfyingPreview {
			logCtx.Info("Not finished reconciling verifying preview service")
			return c.syncRolloutStatusBlueGreen(allRSs, newRS, previewSvc, activeSvc, r)
		}
	}
	logCtx.Infof("Reconciling active service '%s'", activeSvc.Name)
	switchActiveSvc, err := c.reconcileActiveService(r, newRS, previewSvc, activeSvc)
	if err != nil {
		return err
	}
	if switchActiveSvc {
		logCtx.Infof("Not Finished reconciling active service '%s'", activeSvc.Name)
		return c.syncRolloutStatusBlueGreen(allRSs, newRS, previewSvc, activeSvc, r)
	}
	// Scale down, if we can.
	logCtx.Info("Reconciling old replica sets")
	scaledDown, err := c.reconcileOldReplicaSets(allRSs, controller.FilterActiveReplicaSets(oldRSs), newRS, r)
	if err != nil {
		return err
	}
	if scaledDown {
		logCtx.Info("Not finished reconciling old replica sets")
		return c.syncRolloutStatusBlueGreen(allRSs, newRS, previewSvc, activeSvc, r)
	}
	logCtx.Infof("Confirming rollout is complete")
	if conditions.RolloutComplete(r, &r.Status) {
		logCtx.Info("Cleaning up old replicasets")
		if err := c.cleanupRollouts(oldRSs, r); err != nil {
			return err
		}
	}
	return c.syncRolloutStatusBlueGreen(allRSs, newRS, previewSvc, activeSvc, r)
}

func (c *Controller) reconcileVerifyingPreview(activeSvc *corev1.Service, rollout *v1alpha1.Rollout) bool {
	if rollout.Spec.Strategy.BlueGreenStrategy.PreviewService == "" {
		return false
	}
	if _, ok := activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]; !ok {
		return false
	}

	if rollout.Status.VerifyingPreview == nil {
		return false
	}

	return *rollout.Status.VerifyingPreview
}

// scaleDownOldReplicaSetsForBlueGreen scales down old replica sets when rollout strategy is "Blue Green".
func (c *Controller) scaleDownOldReplicaSetsForBlueGreen(allRSs []*appsv1.ReplicaSet, oldRSs []*appsv1.ReplicaSet, rollout *v1alpha1.Rollout) (int32, error) {
	availablePodCount := replicasetutil.GetAvailableReplicaCountForReplicaSets(allRSs)
	if availablePodCount <= defaults.GetRolloutReplicasOrDefault(rollout) {
		// Cannot scale down.
		return 0, nil
	}
	logutil.WithRollout(rollout).Infof("Found %d available pods, scaling down old RSes", availablePodCount)

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

func (c *Controller) setVerifyingPreview(r *v1alpha1.Rollout) error {
	logutil.WithRollout(r).Infof("Patching setVerifyingPreview to true")
	_, err := c.rolloutsclientset.ArgoprojV1alpha1().Rollouts(r.Namespace).Patch(r.Name, patchtypes.MergePatchType, []byte(verifyingPreviewPatch))
	return err
}

func (c *Controller) syncRolloutStatusBlueGreen(allRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, previewSvc *corev1.Service, activeSvc *corev1.Service, r *v1alpha1.Rollout) error {
	newStatus := c.calculateBaseStatus(allRSs, newRS, r)
	previewSelector, ok := c.getRolloutSelectorLabel(previewSvc)
	if !ok {
		previewSelector = ""
	}
	newStatus.BlueGreenStatus.PreviewSelector = previewSelector
	activeSelector, ok := c.getRolloutSelectorLabel(activeSvc)
	if !ok {
		activeSelector = ""
	}
	newStatus.BlueGreenStatus.ActiveSelector = activeSelector

	prevStatus := r.Status

	activeRS := GetActiveReplicaSet(r, allRSs)
	if activeRS != nil && annotations.IsSaturated(r, activeRS) {
		availability := conditions.NewRolloutCondition(v1alpha1.RolloutAvailable, corev1.ConditionTrue, conditions.Available, "Rollout is serving traffic from the active service.")
		conditions.SetRolloutCondition(&prevStatus, *availability)
	} else {
		noAvailability := conditions.NewRolloutCondition(v1alpha1.RolloutAvailable, corev1.ConditionFalse, conditions.Available, "Rollout is not serving traffic from the active service.")
		conditions.SetRolloutCondition(&prevStatus, *noAvailability)
	}
	newStatus.Conditions = prevStatus.Conditions
	newStatus.VerifyingPreview = r.Status.VerifyingPreview

	return c.persistRolloutStatus(r, &newStatus, nil)
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
	allRS := append([]*appsv1.ReplicaSet{newRS}, oldRSs...)
	activeRS := GetActiveReplicaSet(rollout, allRS)
	if activeRS != nil {
		if *(activeRS.Spec.Replicas) != rolloutReplicas {
			_, _, err := c.scaleReplicaSetAndRecordEvent(activeRS, rolloutReplicas, rollout)
			return err
		}
	}
	// If there is only one replica set with pods, then we should scale that up to the full count of the
	// rollout. If there is no replica set with pods, then we should scale up the newest replica set.
	if activeOrLatest := replicasetutil.FindActiveOrLatest(newRS, oldRSs); activeOrLatest != nil {
		if *(activeOrLatest.Spec.Replicas) != rolloutReplicas {
			_, _, err := c.scaleReplicaSetAndRecordEvent(activeOrLatest, rolloutReplicas, rollout)
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
