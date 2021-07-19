package rollout

import (
	"context"
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
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

var controllerKind = v1alpha1.SchemeGroupVersion.WithKind("Rollout")

const (
	addScaleDownAtAnnotationsPatch    = `[{ "op": "add", "path": "/metadata/annotations/%s", "value": "%s"}]`
	removeScaleDownAtAnnotationsPatch = `[{ "op": "remove", "path": "/metadata/annotations/%s"}]`
)

// removeScaleDownDelay removes the `scale-down-deadline` annotation from the ReplicaSet (if it exists)
func (c *rolloutContext) removeScaleDownDelay(rs *appsv1.ReplicaSet) error {
	ctx := context.TODO()
	if !replicasetutil.HasScaleDownDeadline(rs) {
		return nil
	}
	patch := fmt.Sprintf(removeScaleDownAtAnnotationsPatch, v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey)
	_, err := c.kubeclientset.AppsV1().ReplicaSets(rs.Namespace).Patch(ctx, rs.Name, patchtypes.JSONPatchType, []byte(patch), metav1.PatchOptions{})
	if err == nil {
		c.log.Infof("Removed '%s' annotation from RS '%s'", v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey, rs.Name)
	}
	return err
}

// addScaleDownDelay injects the `scale-down-deadline` annotation to the ReplicaSet, or if
// scaleDownDelaySeconds is zero, removes the annotation if it exists
func (c *rolloutContext) addScaleDownDelay(rs *appsv1.ReplicaSet, scaleDownDelaySeconds time.Duration) error {
	if rs == nil {
		return nil
	}
	ctx := context.TODO()
	if scaleDownDelaySeconds == 0 {
		// If scaledown deadline is zero, it means we need to remove any replicasets with the delay
		// This might happen if we switch from canary with traffic routing to basic canary
		if replicasetutil.HasScaleDownDeadline(rs) {
			return c.removeScaleDownDelay(rs)
		}
		return nil
	}
	deadline := metav1.Now().Add(scaleDownDelaySeconds * time.Second).UTC().Format(time.RFC3339)
	patch := fmt.Sprintf(addScaleDownAtAnnotationsPatch, v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey, deadline)
	_, err := c.kubeclientset.AppsV1().ReplicaSets(rs.Namespace).Patch(ctx, rs.Name, patchtypes.JSONPatchType, []byte(patch), metav1.PatchOptions{})
	if err == nil {
		c.log.Infof("Set '%s' annotation on '%s' to %s (%ds)", v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey, rs.Name, deadline, scaleDownDelaySeconds)
	}
	return err
}

func (c *Controller) getReplicaSetsForRollouts(r *v1alpha1.Rollout) ([]*appsv1.ReplicaSet, error) {
	ctx := context.TODO()
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
		fresh, err := c.argoprojclientset.ArgoprojV1alpha1().Rollouts(r.Namespace).Get(ctx, r.Name, metav1.GetOptions{})
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

// removeScaleDownDeadlines removes the scale-down-deadline annotation from the new/stable ReplicaSets,
// in the event that we moved back to an older revision that is still within its scaleDownDelay.
func (c *rolloutContext) removeScaleDownDeadlines() error {
	var toRemove []*appsv1.ReplicaSet
	if c.newRS != nil && !c.isScaleDownOnabort() {
		toRemove = append(toRemove, c.newRS)
	}
	if c.stableRS != nil {
		if len(toRemove) == 0 || c.stableRS.Name != c.newRS.Name {
			toRemove = append(toRemove, c.stableRS)
		}
	}
	for _, rs := range toRemove {
		err := c.removeScaleDownDelay(rs)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *rolloutContext) reconcileNewReplicaSet() (bool, error) {
	if c.newRS == nil {
		return false, nil
	}
	newReplicasCount, err := replicasetutil.NewRSNewReplicas(c.rollout, c.allRSs, c.newRS)
	if err != nil {
		return false, err
	}

	if c.isScaleDownOnabort() {
		c.log.Infof("Scale down new rs '%s' on abort", c.newRS.Name)

		// if the newRS has scale down annotation, check if it should be scaled down now
		if scaleDownAtStr, ok := c.newRS.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey]; ok {
			c.log.Infof("New rs '%s' has scaledown deadline annotation: %s", c.newRS.Name, scaleDownAtStr)
			scaleDownAtTime, err := time.Parse(time.RFC3339, scaleDownAtStr)
			if err != nil {
				c.log.Warnf("Unable to read scaleDownAt label on rs '%s'", c.newRS.Name)
			} else {
				now := metav1.Now()
				scaleDownAt := metav1.NewTime(scaleDownAtTime)
				if scaleDownAt.After(now.Time) {
					c.log.Infof("RS '%s' has not reached the scaleDownTime", c.newRS.Name)
					remainingTime := scaleDownAt.Sub(now.Time)
					if remainingTime < c.resyncPeriod {
						c.enqueueRolloutAfter(c.rollout, remainingTime)
						return false, nil
					}
				} else {
					c.log.Infof("RS '%s' has reached the scaleDownTime", c.newRS.Name)
					newReplicasCount = int32(0)
				}
			}
		} else {
			abortScaleDownDelaySeconds := time.Duration(defaults.GetAbortScaleDownDelaySecondsOrDefault(c.rollout))
			err = c.addScaleDownDelay(c.newRS, abortScaleDownDelaySeconds)
			if err != nil {
				return false, err
			}
		}
	}

	scaled, _, err := c.scaleReplicaSetAndRecordEvent(c.newRS, newReplicasCount)
	return scaled, err
}

func (c *rolloutContext) isScaleDownOnabort() bool {
	abortScaleDownDelaySeconds := time.Duration(defaults.GetAbortScaleDownDelaySecondsOrDefault(c.rollout))
	if c.pauseContext != nil && c.pauseContext.IsAborted() && abortScaleDownDelaySeconds > 0 {
		return true
	}
	return false
}

// reconcileOtherReplicaSets reconciles "other" ReplicaSets.
// Other ReplicaSets are ReplicaSets are neither the new or stable (allRSs - newRS - stableRS)
func (c *rolloutContext) reconcileOtherReplicaSets() (bool, error) {
	otherRSs := controller.FilterActiveReplicaSets(c.otherRSs)
	oldPodsCount := replicasetutil.GetReplicaCountForReplicaSets(otherRSs)
	if oldPodsCount == 0 {
		// Can't scale down further
		return false, nil
	}
	c.log.Infof("Reconciling %d old ReplicaSets (total pods: %d)", len(otherRSs), oldPodsCount)

	var err error
	hasScaled := false
	if c.rollout.Spec.Strategy.Canary != nil {
		// Scale down old replica sets, need check replicasToKeep to ensure we can scale down
		scaledDownCount, err := c.scaleDownOldReplicaSetsForCanary(otherRSs)
		if err != nil {
			return false, nil
		}
		//hasScaled = hasScaled || scaledDownCount > 0
		hasScaled = scaledDownCount > 0
	}

	// Scale down old replica sets
	if c.rollout.Spec.Strategy.BlueGreen != nil {
		hasScaled, err = c.scaleDownOldReplicaSetsForBlueGreen(otherRSs)
		if err != nil {
			return false, nil
		}
	}
	if hasScaled {
		c.log.Infof("Scaled down old RSes")
	}
	return hasScaled, nil
}

// cleanupUnhealthyReplicas will scale down old replica sets with unhealthy replicas, so that all unhealthy replicas will be deleted.
func (c *rolloutContext) cleanupUnhealthyReplicas(oldRSs []*appsv1.ReplicaSet) ([]*appsv1.ReplicaSet, int32, error) {
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
		c.log.Infof("Found %d available pods in old RS %s/%s", targetRS.Status.AvailableReplicas, targetRS.Namespace, targetRS.Name)
		if *(targetRS.Spec.Replicas) == targetRS.Status.AvailableReplicas {
			// no unhealthy replicas found, no scaling required.
			continue
		}

		scaledDownCount := *(targetRS.Spec.Replicas) - targetRS.Status.AvailableReplicas
		newReplicasCount := targetRS.Status.AvailableReplicas
		if newReplicasCount > *(targetRS.Spec.Replicas) {
			return nil, 0, fmt.Errorf("when cleaning up unhealthy replicas, got invalid request to scale down %s/%s %d -> %d", targetRS.Namespace, targetRS.Name, *(targetRS.Spec.Replicas), newReplicasCount)
		}
		_, updatedOldRS, err := c.scaleReplicaSetAndRecordEvent(targetRS, newReplicasCount)
		if err != nil {
			return nil, totalScaledDown, err
		}
		totalScaledDown += scaledDownCount
		oldRSs[i] = updatedOldRS
	}
	return oldRSs, totalScaledDown, nil
}
