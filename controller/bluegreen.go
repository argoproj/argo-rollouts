package controller

import (
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
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
		return c.syncRolloutStatus(allRSs, newRS, previewSvc, activeSvc, r)
	}

	if previewSvc != nil {
		logCtx.Infof("Reconciling preview service '%s'", previewSvc.Name)
		switchPreviewSvc, err := c.reconcilePreviewService(r, newRS, previewSvc, activeSvc)
		if err != nil {
			return err
		}
		if switchPreviewSvc {
			logCtx.Infof("Not finished reconciling preview service' %s'", previewSvc.Name)
			return c.syncRolloutStatus(allRSs, newRS, previewSvc, activeSvc, r)
		}
		logCtx.Info("Reconciling verifying preview service")
		verfyingPreview := c.reconcileVerifyingPreview(activeSvc, r)
		if verfyingPreview {
			logCtx.Info("Not finished reconciling verifying preview service")
			return c.syncRolloutStatus(allRSs, newRS, previewSvc, activeSvc, r)
		}
	}
	logCtx.Infof("Reconciling active service '%s'", activeSvc.Name)
	switchActiveSvc, err := c.reconcileActiveService(r, newRS, previewSvc, activeSvc)
	if err != nil {
		return err
	}
	if switchActiveSvc {
		logCtx.Infof("Not Finished reconciling active service '%s'", activeSvc.Name)
		return c.syncRolloutStatus(allRSs, newRS, previewSvc, activeSvc, r)
	}
	// Scale down, if we can.
	logCtx.Info("Reconciling old replica sets")
	scaledDown, err := c.reconcileOldReplicaSets(allRSs, controller.FilterActiveReplicaSets(oldRSs), newRS, r)
	if err != nil {
		return err
	}
	if scaledDown {
		logCtx.Info("Not finished reconciling old replica sets")
		return c.syncRolloutStatus(allRSs, newRS, previewSvc, activeSvc, r)
	}
	logCtx.Infof("Confirming rollout is complete")
	if conditions.RolloutComplete(r, &r.Status) {
		logCtx.Info("Cleaning up old replicasets")
		if err := c.cleanupRollouts(oldRSs, r); err != nil {
			return err
		}
	}
	return c.syncRolloutStatus(allRSs, newRS, previewSvc, activeSvc, r)
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
