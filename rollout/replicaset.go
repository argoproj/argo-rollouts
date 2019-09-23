package rollout

import (
	"fmt"
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	patchtypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

var controllerKind = v1alpha1.SchemeGroupVersion.WithKind("Rollout")

const (
	addScaleDownAtAnnotationsPatch    = `[{ "op": "add", "path": "/metadata/annotations/%s", "value": "%s"}]`
	removeScaleDownAtAnnotationsPatch = `[{ "op": "remove", "path": "/metadata/annotations/%s"}]`
)

func (c *RolloutController) removeScaleDownDelay(r *v1alpha1.Rollout, rs *appsv1.ReplicaSet) error {
	logCtx := logutil.WithRollout(r)
	logCtx.Infof("Removing '%s' annotation on RS '%s'", v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey, rs.Name)
	patch := fmt.Sprintf(removeScaleDownAtAnnotationsPatch, v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey)
	_, err := c.kubeclientset.AppsV1().ReplicaSets(rs.Namespace).Patch(rs.Name, patchtypes.JSONPatchType, []byte(patch))
	return err
}

func (c *RolloutController) addScaleDownDelay(r *v1alpha1.Rollout, rs *appsv1.ReplicaSet) error {
	logCtx := logutil.WithRollout(r)
	logCtx.Infof("Adding '%s' annotation to RS '%s'", v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey, rs.Name)
	scaleDownDelaySeconds := time.Duration(defaults.GetScaleDownDelaySecondsOrDefault(r))
	now := metav1.Now().Add(scaleDownDelaySeconds * time.Second).UTC().Format(time.RFC3339)
	patch := fmt.Sprintf(addScaleDownAtAnnotationsPatch, v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey, now)
	_, err := c.kubeclientset.AppsV1().ReplicaSets(rs.Namespace).Patch(rs.Name, patchtypes.JSONPatchType, []byte(patch))
	return err
}

func (c *RolloutController) getReplicaSetsForRollouts(r *v1alpha1.Rollout) ([]*appsv1.ReplicaSet, error) {
	// List all ReplicaSets to find those we own but that no longer match our
	// selector. They will be orphaned by ClaimReplicaSets().
	rsList, err := c.replicaSetLister.ReplicaSets(r.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	replicaSetSelector, err := metav1.LabelSelectorAsSelector(r.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("rollout %s/%s has invalid label selector: %v", r.Namespace, r.Name, err)
	}
	// If any adoptions are attempted, we should first recheck for deletion with
	// an uncached quorum read sometime after listing ReplicaSets (see #42639).
	canAdoptFunc := controller.RecheckDeletionTimestamp(func() (metav1.Object, error) {
		fresh, err := c.argoprojclientset.ArgoprojV1alpha1().Rollouts(r.Namespace).Get(r.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if fresh.UID != r.UID {
			return nil, fmt.Errorf("original Rollout %v/%v is gone: got uid %v, wanted %v", r.Namespace, r.Name, fresh.UID, r.UID)
		}
		return fresh, nil
	})
	cm := controller.NewReplicaSetControllerRefManager(c.replicaSetControl, r, replicaSetSelector, controllerKind, canAdoptFunc)
	return cm.ClaimReplicaSets(rsList)
}

func (c *RolloutController) reconcileNewReplicaSet(allRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, rollout *v1alpha1.Rollout) (bool, error) {
	if rollout.Spec.Strategy.BlueGreenStrategy != nil {
		rolloutReplicas := defaults.GetRolloutReplicasOrDefault(rollout)
		if *(newRS.Spec.Replicas) == rolloutReplicas {
			// Scaling not required.
			return false, nil
		}
		if *(newRS.Spec.Replicas) > rolloutReplicas {
			// Scale down.
			scaled, _, err := c.scaleReplicaSetAndRecordEvent(newRS, rolloutReplicas, rollout)
			return scaled, err
		}
	}
	newReplicasCount, err := replicasetutil.NewRSNewReplicas(rollout, allRSs, newRS)
	if err != nil {
		return false, err
	}
	scaled, _, err := c.scaleReplicaSetAndRecordEvent(newRS, newReplicasCount, rollout)
	return scaled, err
}

func (c *RolloutController) reconcileOldReplicaSets(oldRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, rollout *v1alpha1.Rollout) (bool, error) {
	logCtx := logutil.WithRollout(rollout)

	oldPodsCount := replicasetutil.GetReplicaCountForReplicaSets(oldRSs)
	if oldPodsCount == 0 {
		// Can't scale down further
		return false, nil
	}

	// Clean up unhealthy replicas first, otherwise unhealthy replicas will block rollout
	// and cause timeout. See https://github.com/kubernetes/kubernetes/issues/16737
	oldRSs, cleanupCount, err := c.cleanupUnhealthyReplicas(oldRSs, rollout)
	if err != nil {
		return false, nil
	}
	logCtx.Infof("Cleaned up unhealthy replicas from old RSes by %d", cleanupCount)

	// Scale down old replica sets
	scaledDownCount := int32(0)
	if rollout.Spec.Strategy.BlueGreenStrategy != nil {
		scaledDownCount, err = c.scaleDownOldReplicaSetsForBlueGreen(oldRSs, rollout)
		if err != nil {
			return false, nil
		}
	}
	logCtx.Infof("Scaled down old RSes by %d", scaledDownCount)

	totalScaledDown := cleanupCount + scaledDownCount
	return totalScaledDown > 0, nil
}

// cleanupUnhealthyReplicas will scale down old replica sets with unhealthy replicas, so that all unhealthy replicas will be deleted.
func (c *RolloutController) cleanupUnhealthyReplicas(oldRSs []*appsv1.ReplicaSet, rollout *v1alpha1.Rollout) ([]*appsv1.ReplicaSet, int32, error) {
	logCtx := logutil.WithRollout(rollout)
	sort.Sort(controller.ReplicaSetsByCreationTimestamp(oldRSs))
	// Safely scale down all old replica sets with unhealthy replicas. Replica set will sort the pods in the order
	// such that not-ready < ready, unscheduled < scheduled, and pending < running. This ensures that unhealthy replicas will
	// been deleted first and won't increase unavailability.
	totalScaledDown := int32(0)
	for i, targetRS := range oldRSs {
		if *(targetRS.Spec.Replicas) == 0 {
			// cannot scale down this replica set.
			continue
		}
		logCtx.Infof("Found %d available pods in old RS %s/%s", targetRS.Status.AvailableReplicas, targetRS.Namespace, targetRS.Name)
		if *(targetRS.Spec.Replicas) == targetRS.Status.AvailableReplicas {
			// no unhealthy replicas found, no scaling required.
			continue
		}

		scaledDownCount := *(targetRS.Spec.Replicas) - targetRS.Status.AvailableReplicas
		newReplicasCount := targetRS.Status.AvailableReplicas
		if newReplicasCount > *(targetRS.Spec.Replicas) {
			return nil, 0, fmt.Errorf("when cleaning up unhealthy replicas, got invalid request to scale down %s/%s %d -> %d", targetRS.Namespace, targetRS.Name, *(targetRS.Spec.Replicas), newReplicasCount)
		}
		_, updatedOldRS, err := c.scaleReplicaSetAndRecordEvent(targetRS, newReplicasCount, rollout)
		if err != nil {
			return nil, totalScaledDown, err
		}
		totalScaledDown += scaledDownCount
		oldRSs[i] = updatedOldRS
	}
	return oldRSs, totalScaledDown, nil
}
