package rollout

import (
	"context"
	"fmt"
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	patchtypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	serviceutil "github.com/argoproj/argo-rollouts/utils/service"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
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
	rs, err := c.kubeclientset.AppsV1().ReplicaSets(rs.Namespace).Patch(ctx, rs.Name, patchtypes.JSONPatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("error removing scale-down-deadline annotation from RS '%s': %w", rs.Name, err)
	}
	c.log.Infof("Removed '%s' annotation from RS '%s'", v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey, rs.Name)
	err = c.replicaSetInformer.GetIndexer().Update(rs)
	if err != nil {
		return fmt.Errorf("error updating replicaset informer in removeScaleDownDelay: %w", err)
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
	deadline := timeutil.MetaNow().Add(scaleDownDelaySeconds).UTC().Format(time.RFC3339)
	patch := fmt.Sprintf(addScaleDownAtAnnotationsPatch, v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey, deadline)
	rs, err := c.kubeclientset.AppsV1().ReplicaSets(rs.Namespace).Patch(ctx, rs.Name, patchtypes.JSONPatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("error adding scale-down-deadline annotation to RS '%s': %w", rs.Name, err)
	}
	c.log.Infof("Set '%s' annotation on '%s' to %s (%s)", v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey, rs.Name, deadline, scaleDownDelaySeconds)
	err = c.replicaSetInformer.GetIndexer().Update(rs)
	if err != nil {
		return fmt.Errorf("error updating replicaset informer in addScaleDownDelay: %w", err)
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
	canAdoptFunc := controller.RecheckDeletionTimestamp(func(ctx context.Context) (metav1.Object, error) {
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
	return cm.ClaimReplicaSets(ctx, rsList)
}

// removeScaleDownDeadlines removes the scale-down-deadline annotation from the new/stable ReplicaSets,
// in the event that we moved back to an older revision that is still within its scaleDownDelay.
func (c *rolloutContext) removeScaleDownDeadlines() error {
	var toRemove []*appsv1.ReplicaSet
	if c.newRS != nil && !c.shouldDelayScaleDownOnAbort() {
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
	newReplicasCount, err := replicasetutil.NewRSNewReplicas(c.rollout, c.allRSs, c.newRS, c.newStatus.Canary.Weights)
	if err != nil {
		return false, err
	}

	if c.shouldDelayScaleDownOnAbort() {
		abortScaleDownDelaySeconds, _ := defaults.GetAbortScaleDownDelaySecondsOrDefault(c.rollout)
		c.log.Infof("Scale down new rs '%s' on abort (%v)", c.newRS.Name, abortScaleDownDelaySeconds)

		// if the newRS has scale down annotation, check if it should be scaled down now
		if scaleDownAtStr, ok := c.newRS.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey]; ok {
			c.log.Infof("New rs '%s' has scaledown deadline annotation: %s", c.newRS.Name, scaleDownAtStr)
			scaleDownAtTime, err := time.Parse(time.RFC3339, scaleDownAtStr)
			if err != nil {
				c.log.Warnf("Unable to read scaleDownAt label on rs '%s'", c.newRS.Name)
			} else {
				now := timeutil.MetaNow()
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
		} else if abortScaleDownDelaySeconds != nil {
			// Don't annotate until need to ensure the stable RS is fully scaled
			if c.stableRS.Status.AvailableReplicas == *c.rollout.Spec.Replicas {
				err = c.addScaleDownDelay(c.newRS, *abortScaleDownDelaySeconds)
				if err != nil {
					return false, err
				}
			}
			// leave newRS scaled up until we annotate
			return false, nil
		}
	}

	scaled, _, err := c.scaleReplicaSetAndRecordEvent(c.newRS, newReplicasCount)
	if err != nil {
		return scaled, fmt.Errorf("failed to scaleReplicaSetAndRecordEvent in reconcileNewReplicaSet: %w", err)
	}
	return scaled, err
}

// shouldDelayScaleDownOnAbort returns if we are aborted and we should delay scaledown of canary or preview
func (c *rolloutContext) shouldDelayScaleDownOnAbort() bool {
	if !c.pauseContext.IsAborted() {
		// only applicable to aborted rollouts
		return false
	}
	if c.stableRS == nil {
		// if there is no stable, don't scale down
		return false
	}
	if c.rollout.Spec.Strategy.Canary != nil && c.rollout.Spec.Strategy.Canary.TrafficRouting == nil {
		// basic canary should not use this
		return false
	}
	abortDelay, abortDelayWasSet := defaults.GetAbortScaleDownDelaySecondsOrDefault(c.rollout)
	if abortDelay == nil {
		// user explicitly set abortScaleDownDelaySeconds: 0, and wishes to leave canary/preview up indefinitely
		return false
	}
	usesDynamicStableScaling := c.rollout.Spec.Strategy.Canary != nil && c.rollout.Spec.Strategy.Canary.DynamicStableScale
	if usesDynamicStableScaling && !abortDelayWasSet {
		// we are using dynamic stable/canary scaling and user did not explicitly set abortScaleDownDelay
		return false
	}
	return true
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
			return nil, totalScaledDown, fmt.Errorf("failed to scaleReplicaSetAndRecordEvent in cleanupUnhealthyReplicas: %w", err)
		}
		totalScaledDown += scaledDownCount
		oldRSs[i] = updatedOldRS
	}
	return oldRSs, totalScaledDown, nil
}

func (c *rolloutContext) scaleDownDelayHelper(rs *appsv1.ReplicaSet, annotationedRSs int32, rolloutReplicas int32) (int32, int32, error) {
	desiredReplicaCount := int32(0)
	scaleDownRevisionLimit := GetScaleDownRevisionLimit(c.rollout)
	if !replicasetutil.HasScaleDownDeadline(rs) && *rs.Spec.Replicas > 0 {
		// This ReplicaSet is scaled up but does not have a scale down deadline. Add one.
		if annotationedRSs < scaleDownRevisionLimit {
			annotationedRSs++
			desiredReplicaCount = *rs.Spec.Replicas
			scaleDownDelaySeconds := defaults.GetScaleDownDelaySecondsOrDefault(c.rollout)
			err := c.addScaleDownDelay(rs, scaleDownDelaySeconds)
			if err != nil {
				return annotationedRSs, desiredReplicaCount, err
			}
			c.enqueueRolloutAfter(c.rollout, scaleDownDelaySeconds)
		}
	} else if replicasetutil.HasScaleDownDeadline(rs) {
		annotationedRSs++
		if annotationedRSs > scaleDownRevisionLimit {
			c.log.Infof("At ScaleDownDelayRevisionLimit (%d) and scaling down the rest", scaleDownRevisionLimit)
		} else {
			remainingTime, err := replicasetutil.GetTimeRemainingBeforeScaleDownDeadline(rs)
			if err != nil {
				c.log.Warnf("%v", err)
			} else if remainingTime != nil {
				c.log.Infof("RS '%s' has not reached the scaleDownTime", rs.Name)
				if *remainingTime < c.resyncPeriod {
					c.enqueueRolloutAfter(c.rollout, *remainingTime)
				}
				desiredReplicaCount = rolloutReplicas
			}
		}
	}

	return annotationedRSs, desiredReplicaCount, nil
}

// isReplicaSetReferenced returns if the given ReplicaSet is still being referenced by any of
// the current, stable, blue-green services. Used to determine if the ReplicaSet can
// safely be scaled to zero, or deleted.
func (c *rolloutContext) isReplicaSetReferenced(rs *appsv1.ReplicaSet) bool {
	rsPodHash := replicasetutil.GetPodTemplateHash(rs)
	if rsPodHash == "" {
		return false
	}
	ro := c.rollout
	referencesToCheck := []string{
		ro.Status.StableRS,
		ro.Status.CurrentPodHash,
		ro.Status.BlueGreen.ActiveSelector,
		ro.Status.BlueGreen.PreviewSelector,
	}
	if ro.Status.Canary.Weights != nil {
		referencesToCheck = append(referencesToCheck, ro.Status.Canary.Weights.Canary.PodTemplateHash, ro.Status.Canary.Weights.Stable.PodTemplateHash)
	}
	for _, ref := range referencesToCheck {
		if ref == rsPodHash {
			return true
		}
	}

	// The above are static, lightweight checks to see if the selectors we record in our status are
	// still referencing the ReplicaSet in question. Those checks aren't always enough. Next, we do
	// a deeper check to look up the actual service objects, and see if they are still referencing
	// the ReplicaSet. If so, we cannot scale it down.
	var servicesToCheck []string
	if ro.Spec.Strategy.Canary != nil {
		servicesToCheck = []string{ro.Spec.Strategy.Canary.CanaryService, ro.Spec.Strategy.Canary.StableService}
	} else {
		servicesToCheck = []string{ro.Spec.Strategy.BlueGreen.ActiveService, ro.Spec.Strategy.BlueGreen.PreviewService}
	}
	for _, svcName := range servicesToCheck {
		if svcName == "" {
			continue
		}
		svc, err := c.servicesLister.Services(c.rollout.Namespace).Get(svcName)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				// service doesn't exist
				continue
			}
			return true
		}
		if serviceutil.GetRolloutSelectorLabel(svc) == rsPodHash {
			return true
		}
	}
	return false
}
