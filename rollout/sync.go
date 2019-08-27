package rollout

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/kubernetes/pkg/controller"
	labelsutil "k8s.io/kubernetes/pkg/util/labels"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/diff"
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
func (c *RolloutController) getAllReplicaSetsAndSyncRevision(rollout *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet, createIfNotExisted bool) (*appsv1.ReplicaSet, []*appsv1.ReplicaSet, error) {
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
func (c *RolloutController) getNewReplicaSet(rollout *v1alpha1.Rollout, rsList, oldRSs []*appsv1.ReplicaSet, createIfNotExisted bool) (*appsv1.ReplicaSet, error) {
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
		// If no other Progressing condition has been recorded and we need to estimate the progress
		// of this rollout then it is likely that old users started caring about progress. In that
		// case we need to take into account the first time we noticed their new replica set.
		cond := conditions.GetRolloutCondition(rollout.Status, v1alpha1.RolloutProgressing)
		if cond == nil {
			msg := fmt.Sprintf(conditions.FoundNewRSMessage, rsCopy.Name)
			condition := conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionTrue, conditions.FoundNewRSReason, msg)
			conditions.SetRolloutCondition(&rollout.Status, *condition)
			needsUpdate = true
		}

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
		if controllerRef != nil && controllerRef.UID == rollout.UID && replicasetutil.PodTemplateEqualIgnoreHash(&rs.Spec.Template, &rollout.Spec.Template) {
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
		msg := fmt.Sprintf(conditions.FailedRSCreateMessage, newRS.Name, err)
		c.recorder.Event(rollout, corev1.EventTypeWarning, conditions.FailedRSCreateReason, msg)
		newStatus := rollout.Status.DeepCopy()
		cond := conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionFalse, conditions.FailedRSCreateReason, msg)
		conditions.SetRolloutCondition(newStatus, *cond)
		c.persistRolloutStatus(rollout, newStatus, &rollout.Spec.Paused)
		return nil, err
	}

	if !alreadyExists && newReplicasCount > 0 {
		c.recorder.Eventf(rollout, corev1.EventTypeNormal, "ScalingReplicaSet", "Scaled up replica set %s to %d", createdRS.Name, newReplicasCount)
	}

	needsUpdate := annotations.SetRolloutRevision(rollout, newRevision)
	if !alreadyExists {
		msg := fmt.Sprintf(conditions.NewReplicaSetMessage, createdRS.Name)
		condition := conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionTrue, conditions.NewReplicaSetReason, msg)
		conditions.SetRolloutCondition(&rollout.Status, *condition)
		needsUpdate = true
	}

	if needsUpdate {
		_, err = c.rolloutsclientset.ArgoprojV1alpha1().Rollouts(rollout.Namespace).Update(rollout)
	}
	return createdRS, err
}

// syncScalingEvent is responsible for reconciling rollouts on scaling events.
func (c *RolloutController) syncScalingEvent(r *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet) error {
	logCtx := logutil.WithRollout(r)
	logCtx.Info("Reconciling scaling event")
	newRS, oldRSs, err := c.getAllReplicaSetsAndSyncRevision(r, rsList, false)
	if err != nil {
		return err
	}
	// NOTE: it is possible for newRS to be nil (e.g. when template and replicas changed at same time)
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
		return c.syncRolloutStatusBlueGreen(oldRSs, newRS, previewSvc, activeSvc, r, r.Spec.Paused)
	}
	return fmt.Errorf("no rollout strategy provided")
}

// isScalingEvent checks whether the provided rollout has been updated with a scaling event
// by looking at the desired-replicas annotation in the active replica sets of the rollout.
//
// rsList should come from getReplicaSetsForRollout(r).
func (c *RolloutController) isScalingEvent(rollout *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet) (bool, error) {
	if rollout.Spec.Strategy.CanaryStrategy != nil {
		return false, nil
	}
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

func (c *RolloutController) scaleReplicaSetAndRecordEvent(rs *appsv1.ReplicaSet, newScale int32, rollout *v1alpha1.Rollout) (bool, *appsv1.ReplicaSet, error) {
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

func (c *RolloutController) scaleReplicaSet(rs *appsv1.ReplicaSet, newScale int32, rollout *v1alpha1.Rollout, scalingOperation string) (bool, *appsv1.ReplicaSet, error) {

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
func (c *RolloutController) calculateBaseStatus(allRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, rollout *v1alpha1.Rollout) v1alpha1.RolloutStatus {
	prevStatus := rollout.Status

	prevCond := conditions.GetRolloutCondition(prevStatus, v1alpha1.InvalidSpec)
	invalidSpecCond := conditions.VerifyRolloutSpec(rollout, prevCond)
	if prevCond != nil && invalidSpecCond == nil {
		conditions.RemoveRolloutCondition(&prevStatus, v1alpha1.InvalidSpec)
	}

	var currentPodHash string
	if newRS == nil {
		// newRS potentially might be nil when called by Controller::syncScalingEvent(). For this
		// to happen, the user would have had to simultaneously change the number of replicas, and
		// the pod template spec at the same time.
		currentPodHash = controller.ComputeHash(&rollout.Spec.Template, rollout.Status.CollisionCount)
		logutil.WithRollout(rollout).Warnf("Assuming %s for new replicaset pod hash", currentPodHash)
	} else {
		currentPodHash = newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	}

	return v1alpha1.RolloutStatus{
		CurrentPodHash:  currentPodHash,
		Replicas:        replicasetutil.GetActualReplicaCountForReplicaSets(allRSs),
		UpdatedReplicas: replicasetutil.GetActualReplicaCountForReplicaSets([]*appsv1.ReplicaSet{newRS}),
		ReadyReplicas:   replicasetutil.GetReadyReplicaCountForReplicaSets(allRSs),
		CollisionCount:  rollout.Status.CollisionCount,
		Conditions:      prevStatus.Conditions,
	}
}

// cleanupRollout is responsible for cleaning up a rollout ie. retains all but the latest N old replica sets
// where N=r.Spec.RevisionHistoryLimit. Old replica sets are older versions of the podtemplate of a rollout kept
// around by default 1) for historical reasons.
func (c *RolloutController) cleanupRollouts(oldRSs []*appsv1.ReplicaSet, rollout *v1alpha1.Rollout) error {
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

// checkPausedConditions checks if the given rollout is paused or not and adds an appropriate condition.
// These conditions are needed so that we won't accidentally report lack of progress for resumed rollouts
// that were paused for longer than progressDeadlineSeconds.
func (c *RolloutController) checkPausedConditions(r *v1alpha1.Rollout) error {
	cond := conditions.GetRolloutCondition(r.Status, v1alpha1.RolloutProgressing)
	if cond != nil && cond.Reason == conditions.TimedOutReason {
		// If we have reported lack of progress, do not overwrite it with a paused condition.
		return nil
	}
	pausedCondExists := cond != nil && cond.Reason == conditions.PausedRolloutReason

	newStatus := r.Status.DeepCopy()
	needsUpdate := false
	if r.Spec.Paused && !pausedCondExists {
		condition := conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionUnknown, conditions.PausedRolloutReason, conditions.PausedRolloutMessage)
		conditions.SetRolloutCondition(newStatus, *condition)
		needsUpdate = true
	} else if !r.Spec.Paused && pausedCondExists {
		condition := conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionUnknown, conditions.ResumedRolloutReason, conditions.ResumeRolloutMessage)
		conditions.SetRolloutCondition(newStatus, *condition)
		needsUpdate = true
	}

	if !needsUpdate {
		return nil
	}

	err := c.persistRolloutStatus(r, newStatus, &r.Spec.Paused)
	return err
}

func (c *RolloutController) calculateRolloutConditions(r *v1alpha1.Rollout, newStatus v1alpha1.RolloutStatus, allRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet) v1alpha1.RolloutStatus {
	if r.Spec.Paused {
		return newStatus
	}

	// If there is only one replica set that is active then that means we are not running
	// a new rollout and this is a resync where we don't need to estimate any progress.
	// In such a case, we should simply not estimate any progress for this rollout.
	currentCond := conditions.GetRolloutCondition(r.Status, v1alpha1.RolloutProgressing)
	isCompleteRollout := newStatus.Replicas == newStatus.UpdatedReplicas && currentCond != nil && currentCond.Reason == conditions.NewRSAvailableReason
	// Check for progress only if the latest rollout hasn't completed yet.
	if !isCompleteRollout {
		switch {
		case conditions.RolloutComplete(r, &newStatus):
			// Update the rollout conditions with a message for the new replica set that
			// was successfully deployed. If the condition already exists, we ignore this update.
			msg := fmt.Sprintf(conditions.RolloutCompletedMessage, r.Name)
			if newRS != nil {
				msg = fmt.Sprintf(conditions.ReplicaSetCompletedMessage, newRS.Name)
			}
			condition := conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionTrue, conditions.NewRSAvailableReason, msg)
			conditions.SetRolloutCondition(&newStatus, *condition)

		case conditions.RolloutProgressing(r, &newStatus):
			// If there is any progress made, continue by not checking if the rollout failed. This
			// behavior emulates the rolling updater progressDeadline check.
			msg := fmt.Sprintf(conditions.RolloutProgressingMessage, r.Name)
			if newRS != nil {
				msg = fmt.Sprintf(conditions.ReplicaSetProgressingMessage, newRS.Name)
			}
			condition := conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionTrue, conditions.ReplicaSetUpdatedReason, msg)
			// Update the current Progressing condition or add a new one if it doesn't exist.
			// If a Progressing condition with status=true already exists, we should update
			// everything but lastTransitionTime. SetRolloutCondition already does that but
			// it also is not updating conditions when the reason of the new condition is the
			// same as the old. The Progressing condition is a special case because we want to
			// update with the same reason and change just lastUpdateTime iff we notice any
			// progress. That's why we handle it here.
			if currentCond != nil {
				if currentCond.Status == corev1.ConditionTrue {
					condition.LastTransitionTime = currentCond.LastTransitionTime
				}
				conditions.RemoveRolloutCondition(&newStatus, v1alpha1.RolloutProgressing)
			}
			conditions.SetRolloutCondition(&newStatus, *condition)

		case conditions.RolloutTimedOut(r, &newStatus):
			// Update the rollout with a timeout condition. If the condition already exists,
			// we ignore this update.
			msg := fmt.Sprintf(conditions.RolloutTimeOutMessage, r.Name)
			if newRS != nil {
				msg = fmt.Sprintf(conditions.ReplicaSetTimeOutMessage, newRS.Name)
			}
			condition := conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionFalse, conditions.TimedOutReason, msg)
			conditions.SetRolloutCondition(&newStatus, *condition)
		}
	}

	activeRS, _ := replicasetutil.GetReplicaSetByTemplateHash(allRSs, newStatus.BlueGreen.ActiveSelector)
	if r.Spec.Strategy.BlueGreenStrategy != nil && activeRS != nil && annotations.IsSaturated(r, activeRS) {
		availability := conditions.NewRolloutCondition(v1alpha1.RolloutAvailable, corev1.ConditionTrue, conditions.AvailableReason, conditions.AvailableMessage)
		conditions.SetRolloutCondition(&newStatus, *availability)
	} else if r.Spec.Strategy.CanaryStrategy != nil && replicasetutil.GetAvailableReplicaCountForReplicaSets(allRSs) >= defaults.GetRolloutReplicasOrDefault(r) {
		availability := conditions.NewRolloutCondition(v1alpha1.RolloutAvailable, corev1.ConditionTrue, conditions.AvailableReason, conditions.AvailableMessage)
		conditions.SetRolloutCondition(&newStatus, *availability)
	} else {
		noAvailability := conditions.NewRolloutCondition(v1alpha1.RolloutAvailable, corev1.ConditionFalse, conditions.AvailableReason, conditions.NotAvailableMessage)
		conditions.SetRolloutCondition(&newStatus, *noAvailability)
	}

	// Move failure conditions of all replica sets in rollout conditions. For now,
	// only one failure condition is returned from getReplicaFailures.
	if replicaFailureCond := c.getReplicaFailures(allRSs, newRS); len(replicaFailureCond) > 0 {
		// There will be only one ReplicaFailure condition on the replica set.
		conditions.SetRolloutCondition(&newStatus, replicaFailureCond[0])
	} else {
		conditions.RemoveRolloutCondition(&newStatus, v1alpha1.RolloutReplicaFailure)
	}
	return newStatus
}

// persistRolloutStatus persists updates to rollout status. If no changes were made, it is a no-op
func (c *RolloutController) persistRolloutStatus(orig *v1alpha1.Rollout, newStatus *v1alpha1.RolloutStatus, newPause *bool) error {
	specCopy := orig.Spec.DeepCopy()
	paused := specCopy.Paused
	if newPause != nil {
		paused = *newPause
		specCopy.Paused = *newPause
	}
	newStatus.ObservedGeneration = conditions.ComputeGenerationHash(*specCopy)

	logCtx := logutil.WithRollout(orig)
	patch, modified, err := diff.CreateTwoWayMergePatch(
		&v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Paused: orig.Spec.Paused,
			},
			Status: orig.Status,
		},
		&v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Paused: paused,
			},
			Status: *newStatus,
		}, v1alpha1.Rollout{})
	if err != nil {
		logCtx.Errorf("Error constructing app status patch: %v", err)
		return err
	}
	if !modified {
		logCtx.Info("No status changes. Skipping patch")
		c.requeueStuckRollout(orig, *newStatus)
		return nil
	}
	logCtx.Debugf("Rollout Patch: %s", patch)
	_, err = c.rolloutsclientset.ArgoprojV1alpha1().Rollouts(orig.Namespace).Patch(orig.Name, patchtypes.MergePatchType, patch)
	if err != nil {
		logCtx.Warningf("Error updating application: %v", err)
		return err
	}
	logCtx.Info("Patch status successfully")
	return nil
}

// used for unit testing
var nowFn = func() time.Time { return time.Now() }

// requeueStuckRollout checks whether the provided rollout needs to be synced for a progress
// check. It returns the time after the rollout will be requeued for the progress check, 0 if it
// will be requeued now, or -1 if it does not need to be requeued.
func (c *RolloutController) requeueStuckRollout(r *v1alpha1.Rollout, newStatus v1alpha1.RolloutStatus) time.Duration {
	logctx := logutil.WithRollout(r)
	currentCond := conditions.GetRolloutCondition(r.Status, v1alpha1.RolloutProgressing)
	// Can't estimate progress if there is no deadline in the spec or progressing condition in the current status.
	if currentCond == nil {
		return time.Duration(-1)
	}
	// No need to estimate progress if the rollout is complete or already timed out.
	if conditions.RolloutComplete(r, &newStatus) || currentCond.Reason == conditions.TimedOutReason || r.Spec.Paused {
		return time.Duration(-1)
	}
	// If there is no sign of progress at this point then there is a high chance that the
	// rollout is stuck. We should resync this rollout at some point in the future[1]
	// and check whether it has timed out. We definitely need this, otherwise we depend on the
	// controller resync interval. See https://github.com/kubernetes/kubernetes/issues/34458.
	//
	// [1] ProgressingCondition.LastUpdatedTime + progressDeadlineSeconds - time.Now()
	//
	// For example, if a Rollout updated its Progressing condition 3 minutes ago and has a
	// deadline of 10 minutes, it would need to be resynced for a progress check after 7 minutes.
	//
	// lastUpdated: 			00:00:00
	// now: 					00:03:00
	// progressDeadlineSeconds: 600 (10 minutes)
	//
	// lastUpdated + progressDeadlineSeconds - now => 00:00:00 + 00:10:00 - 00:03:00 => 07:00
	progressDeadlineSeconds := defaults.GetProgressDeadlineSecondsOrDefault(r)
	after := currentCond.LastUpdateTime.Time.Add(time.Duration(progressDeadlineSeconds) * time.Second).Sub(nowFn())
	// If the remaining time is less than a second, then requeue the deployment immediately.
	// Make it ratelimited so we stay on the safe side, eventually the Deployment should
	// transition either to a Complete or to a TimedOut condition.
	if after < time.Second {
		logctx.Infof("Queueing up Rollout for a progress check now")
		c.enqueueRollout(r)
		return time.Duration(0)
	}
	logctx.Infof("Queueing up rollout for a progress after %ds", int(after.Seconds()))
	// Add a second to avoid milliseconds skew in AddAfter.
	// See https://github.com/kubernetes/kubernetes/issues/39785#issuecomment-279959133 for more info.
	c.enqueueRolloutAfter(r, after+time.Second)
	return after
}

// getReplicaFailures will convert replica failure conditions from replica sets
// to rollout conditions.
func (c *RolloutController) getReplicaFailures(allRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet) []v1alpha1.RolloutCondition {
	var errorConditions []v1alpha1.RolloutCondition
	if newRS != nil {
		for _, c := range newRS.Status.Conditions {
			if c.Type != appsv1.ReplicaSetReplicaFailure {
				continue
			}
			errorConditions = append(errorConditions, conditions.ReplicaSetToRolloutCondition(c))
		}
	}

	// Return failures for the new replica set over failures from old replica sets.
	if len(errorConditions) > 0 {
		return errorConditions
	}

	for i := range allRSs {
		rs := allRSs[i]
		if rs == nil {
			continue
		}

		for _, c := range rs.Status.Conditions {
			if c.Type != appsv1.ReplicaSetReplicaFailure {
				continue
			}
			errorConditions = append(errorConditions, conditions.ReplicaSetToRolloutCondition(c))
		}
	}
	return errorConditions
}
