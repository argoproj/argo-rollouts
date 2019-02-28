package controller

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/kubernetes/pkg/controller"
	labelsutil "k8s.io/kubernetes/pkg/util/labels"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

// getAllReplicaSetsAndSyncRevision returns all the replica sets for the provided rollout (new and all old), with new RS's and rollout's revision updated.
//
// rsList should come from getReplicaSetsForRollout(r).
//
// 1. Get all old RSes this rollout targets, and calculate the max revision number among them (maxOldV).
// 2. Get new RS this rollout targets (whose pod template matches rollout's), and update new RS's revision number to (maxOldV + 1),
//    only if its revision number is smaller than (maxOldV + 1). If this step failed, we'll update it in the next rollout sync loop.
// 3. Copy new RS's revision number to rollout (update rollout's revision). If this step failed, we'll update it in the next rollout sync loop.
//
// Note that currently the rollout controller is using caches to avoid querying the server for reads.
// This may lead to stale reads of replica sets, thus incorrect  v status.
func (c *Controller) getAllReplicaSetsAndSyncRevision(rollout *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet, createIfNotExisted bool) (*appsv1.ReplicaSet, []*appsv1.ReplicaSet, error) {
	allOldRSs := replicasetutil.FindOldReplicaSets(rollout, rsList)

	// Get new replica set with the updated revision number
	newRS, err := c.getNewReplicaSet(rollout, rsList, allOldRSs, createIfNotExisted)
	if err != nil {
		return nil, nil, err
	}

	return newRS, allOldRSs, nil
}

// Returns a replica set that matches the intent of the given rollout. Returns nil if the new replica set doesn't exist yet.
// 1. Get existing new RS (the RS that the given rollout targets, whose pod template is the same as rollout's).
// 2. If there's existing new RS, update its revision number if it's smaller than (maxOldRevision + 1), where maxOldRevision is the max revision number among all old RSes.
// 3. If there's no existing new RS and createIfNotExisted is true, create one with appropriate revision number (maxOldRevision + 1) and replicas.
// Note that the pod-template-hash will be added to adopted RSes and pods.
func (c *Controller) getNewReplicaSet(rollout *v1alpha1.Rollout, rsList, oldRSs []*appsv1.ReplicaSet, createIfNotExisted bool) (*appsv1.ReplicaSet, error) {
	logCtx := logutil.WithRollout(rollout)
	existingNewRS := replicasetutil.FindNewReplicaSet(rollout, rsList)

	// Calculate the max revision number among all old RSes
	maxOldRevision := replicasetutil.MaxRevision(oldRSs)
	// Calculate revision number for this new replica set
	newRevision := strconv.FormatInt(maxOldRevision+1, 10)

	// Latest replica set exists. We need to sync its annotations (includes copying all but
	// annotationsToSkip from the parent rollout, and update revision and desiredReplicas)
	// and also update the revision annotation in the rollout with the
	// latest revision.
	if existingNewRS != nil {
		rsCopy := existingNewRS.DeepCopy()

		// Set existing new replica set's annotation
		annotationsUpdated := annotations.SetNewReplicaSetAnnotations(rollout, rsCopy, newRevision, true)
		minReadySecondsNeedsUpdate := rsCopy.Spec.MinReadySeconds != rollout.Spec.MinReadySeconds
		if annotationsUpdated || minReadySecondsNeedsUpdate {
			rsCopy.Spec.MinReadySeconds = rollout.Spec.MinReadySeconds
			return c.kubeclientset.AppsV1().ReplicaSets(rsCopy.ObjectMeta.Namespace).Update(rsCopy)
		}

		// Should use the revision in existingNewRS's annotation, since it set by before
		needsUpdate := annotations.SetRolloutRevision(rollout, rsCopy.Annotations[annotations.RevisionAnnotation])
		if needsUpdate {
			var err error
			logCtx.Info("Setting revision annotation after creating a new replicaset")
			if rollout, err = c.rolloutsclientset.ArgoprojV1alpha1().Rollouts(rollout.Namespace).Update(rollout); err != nil {
				logCtx.WithError(err).Errorf("Error: Setting rollout revision annotation after creating a new replicaset")
				return nil, err
			}
		}
		return rsCopy, nil
	}

	if !createIfNotExisted {
		return nil, nil
	}

	// new ReplicaSet does not exist, create one.
	newRSTemplate := *rollout.Spec.Template.DeepCopy()
	podTemplateSpecHash := controller.ComputeHash(&newRSTemplate, rollout.Status.CollisionCount)
	newRSTemplate.Labels = labelsutil.CloneAndAddLabel(rollout.Spec.Template.Labels, v1alpha1.DefaultRolloutUniqueLabelKey, podTemplateSpecHash)
	// Add podTemplateHash label to selector.
	newRSSelector := labelsutil.CloneSelectorAndAddLabel(rollout.Spec.Selector, v1alpha1.DefaultRolloutUniqueLabelKey, podTemplateSpecHash)

	// Create new ReplicaSet
	newRS := appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            rollout.Name + "-" + podTemplateSpecHash,
			Namespace:       rollout.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(rollout, controllerKind)},
			Labels:          newRSTemplate.Labels,
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas:        new(int32),
			MinReadySeconds: rollout.Spec.MinReadySeconds,
			Selector:        newRSSelector,
			Template:        newRSTemplate,
		},
	}
	allRSs := append(oldRSs, &newRS)
	newReplicasCount, err := replicasetutil.NewRSNewReplicas(rollout, allRSs, &newRS)
	if err != nil {
		return nil, err
	}

	*(newRS.Spec.Replicas) = newReplicasCount
	// Set new replica set's annotation
	annotations.SetNewReplicaSetAnnotations(rollout, &newRS, newRevision, false)
	// Create the new ReplicaSet. If it already exists, then we need to check for possible
	// hash collisions. If there is any other error, we need to report it in the status of
	// the Rollout.
	alreadyExists := false
	createdRS, err := c.kubeclientset.AppsV1().ReplicaSets(rollout.Namespace).Create(&newRS)
	switch {
	// We may end up hitting this due to a slow cache or a fast resync of the Rollout.
	case errors.IsAlreadyExists(err):
		alreadyExists = true

		// Fetch a copy of the ReplicaSet.
		rs, rsErr := c.replicaSetLister.ReplicaSets(newRS.Namespace).Get(newRS.Name)
		if rsErr != nil {
			return nil, rsErr
		}

		// If the Rollout owns the ReplicaSet and the ReplicaSet's PodTemplateSpec is semantically
		// deep equal to the PodTemplateSpec of the Rollout, it's the Rollout's new ReplicaSet.
		// Otherwise, this is a hash collision and we need to increment the collisionCount field in
		// the status of the Rollout and requeue to try the creation in the next sync.
		controllerRef := metav1.GetControllerOf(rs)
		replicaSetName := fmt.Sprintf("%s-%s", rollout.Name, controller.ComputeHash(&rollout.Spec.Template, rollout.Status.CollisionCount))
		if controllerRef != nil && controllerRef.UID == rollout.UID && replicaSetName == rs.Name {
			createdRS = rs
			err = nil
			break
		}

		// Matching ReplicaSet is not equal - increment the collisionCount in the RolloutStatus
		// and requeue the Rollout.
		if rollout.Status.CollisionCount == nil {
			rollout.Status.CollisionCount = new(int32)
		}
		preCollisionCount := *rollout.Status.CollisionCount
		*rollout.Status.CollisionCount++
		// Update the collisionCount for the Rollout and let it requeue by returning the original
		// error.
		_, roErr := c.rolloutsclientset.ArgoprojV1alpha1().Rollouts(rollout.Namespace).Update(rollout)
		if roErr == nil {
			logCtx.Warnf("Found a hash collision - bumped collisionCount (%d->%d) to resolve it", preCollisionCount, *rollout.Status.CollisionCount)
		}
		return nil, err
	case err != nil:
		msg := fmt.Sprintf("Failed to create new replica set %q: %v", newRS.Name, err)
		c.recorder.Eventf(rollout, corev1.EventTypeWarning, conditions.FailedRSCreateReason, msg)
		return nil, err
	}
	if !alreadyExists && newReplicasCount > 0 {
		c.recorder.Eventf(rollout, corev1.EventTypeNormal, "ScalingReplicaSet", "Scaled up replica set %s to %d", createdRS.Name, newReplicasCount)
	}

	needsUpdate := annotations.SetRolloutRevision(rollout, newRevision)
	if needsUpdate {
		logCtx.Info("Setting rollout revision annotation after creating new replicaset")
		_, err = c.rolloutsclientset.ArgoprojV1alpha1().Rollouts(rollout.Namespace).Update(rollout)
	}
	return createdRS, err
}

// sync is responsible for reconciling rollouts on scaling events.
func (c *Controller) sync(r *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet) error {
	newRS, oldRSs, err := c.getAllReplicaSetsAndSyncRevision(r, rsList, false)
	if err != nil {
		return err
	}
	if r.Spec.Strategy.BlueGreenStrategy != nil {
		previewSvc, activeSvc, err := c.getPreviewAndActiveServices(r)
		if err != nil {
			return nil
		}
		if err := c.scaleBlueGreen(r, newRS, oldRSs, previewSvc, activeSvc); err != nil {
			// If we get an error while trying to scale, the rollout will be requeued
			// so we can abort this resync
			return err
		}
		allRSs := append([]*appsv1.ReplicaSet{newRS}, oldRSs...)
		return c.syncRolloutStatusBlueGreen(allRSs, newRS, previewSvc, activeSvc, r)
	}
	if r.Spec.Strategy.CanaryStrategy != nil {
		stableRS, previousRSs := replicasetutil.GetStableRS(r, newRS, oldRSs)

		if err := c.scaleCanary(previousRSs, newRS, stableRS, r); err != nil {
			// If we get an error while trying to scale, the rollout will be requeued
			// so we can abort this resync
			return err
		}
		return c.syncRolloutStatusCanary(previousRSs, newRS, stableRS, r)
	}
	return fmt.Errorf("no rollout strategy provided")
}

// isScalingEvent checks whether the provided rollout has been updated with a scaling event
// by looking at the desired-replicas annotation in the active replica sets of the rollout.
//
// rsList should come from getReplicaSetsForRollout(r).
func (c *Controller) isScalingEvent(rollout *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet) (bool, error) {
	newRS, previousRSs, err := c.getAllReplicaSetsAndSyncRevision(rollout, rsList, false)
	if err != nil {
		return false, err
	}

	allRSs := append(previousRSs, newRS)

	for _, rs := range controller.FilterActiveReplicaSets(allRSs) {
		desired, ok := annotations.GetDesiredReplicasAnnotation(rs)
		if !ok {
			continue
		}
		if desired != defaults.GetRolloutReplicasOrDefault(rollout) {
			return true, nil
		}
	}
	return false, nil
}

func (c *Controller) scaleReplicaSetAndRecordEvent(rs *appsv1.ReplicaSet, newScale int32, rollout *v1alpha1.Rollout) (bool, *appsv1.ReplicaSet, error) {
	// No need to scale
	if *(rs.Spec.Replicas) == newScale {
		return false, rs, nil
	}
	var scalingOperation string
	if *(rs.Spec.Replicas) < newScale {
		scalingOperation = "up"
	} else {
		scalingOperation = "down"
	}
	scaled, newRS, err := c.scaleReplicaSet(rs, newScale, rollout, scalingOperation)
	return scaled, newRS, err
}

func (c *Controller) scaleReplicaSet(rs *appsv1.ReplicaSet, newScale int32, rollout *v1alpha1.Rollout, scalingOperation string) (bool, *appsv1.ReplicaSet, error) {

	sizeNeedsUpdate := *(rs.Spec.Replicas) != newScale
	rolloutReplicas := defaults.GetRolloutReplicasOrDefault(rollout)
	annotationsNeedUpdate := annotations.ReplicasAnnotationsNeedUpdate(rs, rolloutReplicas)

	scaled := false
	var err error
	if sizeNeedsUpdate || annotationsNeedUpdate {
		rsCopy := rs.DeepCopy()
		*(rsCopy.Spec.Replicas) = newScale
		annotations.SetReplicasAnnotations(rsCopy, rolloutReplicas)
		rs, err = c.kubeclientset.AppsV1().ReplicaSets(rsCopy.Namespace).Update(rsCopy)
		if err == nil && sizeNeedsUpdate {
			scaled = true
			c.recorder.Eventf(rollout, corev1.EventTypeNormal, "ScalingReplicaSet", "Scaled %s replica set %s to %d", scalingOperation, rs.Name, newScale)
		}
	}
	return scaled, rs, err
}

// calculateStatus calculates the common fields for all rollouts by looking into the provided replica sets.
func (c *Controller) calculateBaseStatus(allRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, rollout *v1alpha1.Rollout) v1alpha1.RolloutStatus {
	prevStatus := rollout.Status

	prevCond := conditions.GetRolloutCondition(prevStatus, v1alpha1.InvalidSpec)
	invalidSpecCond := conditions.VerifyRolloutSpec(rollout, prevCond)
	if prevCond != nil && invalidSpecCond == nil {
		conditions.RemoveRolloutCondition(&prevStatus, v1alpha1.InvalidSpec)
	}

	return v1alpha1.RolloutStatus{
		CurrentPodHash:    controller.ComputeHash(&rollout.Spec.Template, rollout.Status.CollisionCount),
		Replicas:          replicasetutil.GetActualReplicaCountForReplicaSets(allRSs),
		UpdatedReplicas:   replicasetutil.GetActualReplicaCountForReplicaSets([]*appsv1.ReplicaSet{newRS}),
		ReadyReplicas:     replicasetutil.GetReadyReplicaCountForReplicaSets(allRSs),
		AvailableReplicas: replicasetutil.GetAvailableReplicaCountForReplicaSets(allRSs),
		CollisionCount:    rollout.Status.CollisionCount,
		Conditions:        prevStatus.Conditions,
	}
}

// cleanupRollout is responsible for cleaning up a rollout ie. retains all but the latest N old replica sets
// where N=r.Spec.RevisionHistoryLimit. Old replica sets are older versions of the podtemplate of a rollout kept
// around by default 1) for historical reasons.
func (c *Controller) cleanupRollouts(oldRSs []*appsv1.ReplicaSet, rollout *v1alpha1.Rollout) error {
	logCtx := logutil.WithRollout(rollout)
	if !conditions.HasRevisionHistoryLimit(rollout) {
		return nil
	}

	// Avoid deleting replica set with deletion timestamp set
	aliveFilter := func(rs *appsv1.ReplicaSet) bool {
		return rs != nil && rs.ObjectMeta.DeletionTimestamp == nil
	}
	cleanableRSes := controller.FilterReplicaSets(oldRSs, aliveFilter)

	diff := int32(len(cleanableRSes)) - *rollout.Spec.RevisionHistoryLimit
	if diff <= 0 {
		return nil
	}

	sort.Sort(controller.ReplicaSetsByCreationTimestamp(cleanableRSes))
	logCtx.Info("Looking to cleanup old replica sets")

	for i := int32(0); i < diff; i++ {
		rs := cleanableRSes[i]
		// Avoid delete replica set with non-zero replica counts
		if rs.Status.Replicas != 0 || *(rs.Spec.Replicas) != 0 || rs.Generation > rs.Status.ObservedGeneration || rs.DeletionTimestamp != nil {
			continue
		}
		logCtx.Infof("Trying to cleanup replica set %q", rs.Name)
		if err := c.kubeclientset.AppsV1().ReplicaSets(rs.Namespace).Delete(rs.Name, nil); err != nil && !errors.IsNotFound(err) {
			// Return error instead of aggregating and continuing DELETEs on the theory
			// that we may be overloading the api server.
			return err
		}
	}

	return nil
}

// CreateTwoWayMergePatch is a helper to construct a two-way merge patch from objects (instead of bytes)
func CreateTwoWayMergePatch(orig, new, dataStruct interface{}) ([]byte, bool, error) {
	origBytes, err := json.Marshal(orig)
	if err != nil {
		return nil, false, err
	}
	newBytes, err := json.Marshal(new)
	if err != nil {
		return nil, false, err
	}
	patch, err := strategicpatch.CreateTwoWayMergePatch(origBytes, newBytes, dataStruct)
	if err != nil {
		return nil, false, err
	}
	return patch, string(patch) != "{}", nil
}

// persistRolloutStatus persists updates to rollout status. If no changes were made, it is a no-op
func (c *Controller) persistRolloutStatus(orig *v1alpha1.Rollout, newStatus *v1alpha1.RolloutStatus, pause *bool) error {
	specCopy := orig.Spec.DeepCopy()
	specCopy.Pause = pause
	newStatus.ObservedGeneration = conditions.ComputeGenerationHash(*specCopy)

	logCtx := logutil.WithRollout(orig)
	patch, modified, err := CreateTwoWayMergePatch(
		&v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Pause: orig.Spec.Pause,
			},
			Status: orig.Status,
		},
		&v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Pause: pause,
			},
			Status: *newStatus,
		}, v1alpha1.Rollout{})
	if err != nil {
		logCtx.Errorf("Error constructing app status patch: %v", err)
		return err
	}
	if !modified {
		logCtx.Infof("No status changes. Skipping patch")
		return nil
	}
	_, err = c.rolloutsclientset.ArgoprojV1alpha1().Rollouts(orig.Namespace).Patch(orig.Name, patchtypes.MergePatchType, patch)
	if err != nil {
		logCtx.Warningf("Error updating application: %v", err)
		return err
	}
	logCtx.Infof("Patch status successfully")
	return nil
}
