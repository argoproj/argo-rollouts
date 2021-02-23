package rollout

import (
	"context"
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
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/diff"
	experimentutil "github.com/argoproj/argo-rollouts/utils/experiment"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

// getAllReplicaSetsAndSyncRevision returns all the replica sets for the provided rollout (new and all old), with new RS's and rollout's revision updated.
//
// 1. Get all old RSes this rollout targets, and calculate the max revision number among them (maxOldV).
// 2. Get new RS this rollout targets (whose pod template matches rollout's), and update new RS's revision number to (maxOldV + 1),
//    only if its revision number is smaller than (maxOldV + 1). If this step failed, we'll update it in the next rollout sync loop.
// 3. Copy new RS's revision number to rollout (update rollout's revision). If this step failed, we'll update it in the next rollout sync loop.
// 4. If there's no existing new RS and createIfNotExisted is true, create one with appropriate revision number (maxOldRevision + 1) and replicas.
//    Note that the pod-template-hash will be added to adopted RSes and pods.
//
// Note that currently the rollout controller is using caches to avoid querying the server for reads.
// This may lead to stale reads of replica sets, thus incorrect  v status.
func (c *rolloutContext) getAllReplicaSetsAndSyncRevision(createIfNotExisted bool) (*appsv1.ReplicaSet, error) {
	// Get new replica set with the updated revision number
	newRS, err := c.syncReplicaSetRevision()
	if err != nil {
		return nil, err
	}
	if newRS == nil && createIfNotExisted {
		newRS, err = c.createDesiredReplicaSet()
		if err != nil {
			return nil, err
		}
	}
	return newRS, nil
}

// Returns a replica set that matches the intent of the given rollout. Returns nil if the new replica set doesn't exist yet.
// 1. Get existing new RS (the RS that the given rollout targets, whose pod template is the same as rollout's).
// 2. If there's existing new RS, update its revision number if it's smaller than (maxOldRevision + 1), where maxOldRevision is the max revision number among all old RSes.
func (c *rolloutContext) syncReplicaSetRevision() (*appsv1.ReplicaSet, error) {
	if c.newRS == nil {
		return nil, nil
	}
	ctx := context.TODO()

	// Calculate the max revision number among all old RSes
	maxOldRevision := replicasetutil.MaxRevision(c.olderRSs)
	// Calculate revision number for this new replica set
	newRevision := strconv.FormatInt(maxOldRevision+1, 10)

	// Latest replica set exists. We need to sync its annotations (includes copying all but
	// annotationsToSkip from the parent rollout, and update revision and desiredReplicas)
	// and also update the revision annotation in the rollout with the
	// latest revision.
	rsCopy := c.newRS.DeepCopy()

	// Set existing new replica set's annotation
	annotationsUpdated := annotations.SetNewReplicaSetAnnotations(c.rollout, rsCopy, newRevision, true)
	minReadySecondsNeedsUpdate := rsCopy.Spec.MinReadySeconds != c.rollout.Spec.MinReadySeconds
	affinityNeedsUpdate := replicasetutil.IfInjectedAntiAffinityRuleNeedsUpdate(rsCopy.Spec.Template.Spec.Affinity, *c.rollout)

	if annotationsUpdated || minReadySecondsNeedsUpdate || affinityNeedsUpdate {
		rsCopy.Spec.MinReadySeconds = c.rollout.Spec.MinReadySeconds
		rsCopy.Spec.Template.Spec.Affinity = replicasetutil.GenerateReplicaSetAffinity(*c.rollout)
		return c.kubeclientset.AppsV1().ReplicaSets(rsCopy.ObjectMeta.Namespace).Update(ctx, rsCopy, metav1.UpdateOptions{})
	}

	// Should use the revision in existingNewRS's annotation, since it set by before
	if annotations.SetRolloutRevision(c.rollout, rsCopy.Annotations[annotations.RevisionAnnotation]) {
		updatedRollout, err := c.argoprojclientset.ArgoprojV1alpha1().Rollouts(c.rollout.Namespace).Update(ctx, c.rollout, metav1.UpdateOptions{})
		if err != nil {
			c.log.WithError(err).Error("Error: updating rollout revision")
			return nil, err
		}
		c.rollout = updatedRollout
		c.newRollout = updatedRollout
		c.log.Infof("Updated rollout revision annotation to %s", rsCopy.Annotations[annotations.RevisionAnnotation])
	}

	// If no other Progressing condition has been recorded and we need to estimate the progress
	// of this rollout then it is likely that old users started caring about progress. In that
	// case we need to take into account the first time we noticed their new replica set.
	cond := conditions.GetRolloutCondition(c.rollout.Status, v1alpha1.RolloutProgressing)
	if cond == nil {
		msg := fmt.Sprintf(conditions.FoundNewRSMessage, rsCopy.Name)
		condition := conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionTrue, conditions.FoundNewRSReason, msg)
		conditions.SetRolloutCondition(&c.rollout.Status, *condition)
		updatedRollout, err := c.argoprojclientset.ArgoprojV1alpha1().Rollouts(c.rollout.Namespace).UpdateStatus(ctx, c.rollout, metav1.UpdateOptions{})
		if err != nil {
			c.log.WithError(err).Error("Error: updating rollout revision")
			return nil, err
		}
		c.rollout = updatedRollout
		c.newRollout = updatedRollout
		c.log.Infof("Initialized Progressing condition: %v", condition)
	}
	return rsCopy, nil
}

func (c *rolloutContext) createDesiredReplicaSet() (*appsv1.ReplicaSet, error) {
	ctx := context.TODO()
	// Calculate the max revision number among all old RSes
	maxOldRevision := replicasetutil.MaxRevision(c.olderRSs)
	// Calculate revision number for this new replica set
	newRevision := strconv.FormatInt(maxOldRevision+1, 10)

	// new ReplicaSet does not exist, create one.
	newRSTemplate := *c.rollout.Spec.Template.DeepCopy()
	// Add default anti-affinity rule if antiAffinity bool set and RSTemplate meets requirements
	newRSTemplate.Spec.Affinity = replicasetutil.GenerateReplicaSetAffinity(*c.rollout)
	podTemplateSpecHash := controller.ComputeHash(&c.rollout.Spec.Template, c.rollout.Status.CollisionCount)
	newRSTemplate.Labels = labelsutil.CloneAndAddLabel(c.rollout.Spec.Template.Labels, v1alpha1.DefaultRolloutUniqueLabelKey, podTemplateSpecHash)
	// Add podTemplateHash label to selector.
	newRSSelector := labelsutil.CloneSelectorAndAddLabel(c.rollout.Spec.Selector, v1alpha1.DefaultRolloutUniqueLabelKey, podTemplateSpecHash)

	// Create new ReplicaSet
	newRS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            c.rollout.Name + "-" + podTemplateSpecHash,
			Namespace:       c.rollout.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(c.rollout, controllerKind)},
			Labels:          newRSTemplate.Labels,
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas:        new(int32),
			MinReadySeconds: c.rollout.Spec.MinReadySeconds,
			Selector:        newRSSelector,
			Template:        newRSTemplate,
		},
	}
	allRSs := append(c.allRSs, newRS)
	newReplicasCount, err := replicasetutil.NewRSNewReplicas(c.rollout, allRSs, newRS)
	if err != nil {
		return nil, err
	}

	newRS.Spec.Replicas = pointer.Int32Ptr(newReplicasCount)
	// Set new replica set's annotation
	annotations.SetNewReplicaSetAnnotations(c.rollout, newRS, newRevision, false)

	if c.rollout.Spec.Strategy.Canary != nil || c.rollout.Spec.Strategy.BlueGreen != nil {
		var ephemeralMetadata *v1alpha1.PodTemplateMetadata
		if c.stableRS != nil && c.stableRS != c.newRS {
			// If this is a canary rollout, with ephemeral *canary* metadata, and there is a stable RS,
			// then inject the canary metadata so that all the RS's new pods get the canary labels/annotation
			if c.rollout.Spec.Strategy.Canary != nil {
				ephemeralMetadata = c.rollout.Spec.Strategy.Canary.CanaryMetadata
			} else {
				ephemeralMetadata = c.rollout.Spec.Strategy.BlueGreen.PreviewMetadata
			}
		} else {
			// Otherwise, if stableRS is nil, we are in a brand-new rollout and then this replicaset
			// will eventually become the stableRS, so we should inject the stable labels/annotation
			if c.rollout.Spec.Strategy.Canary != nil {
				ephemeralMetadata = c.rollout.Spec.Strategy.Canary.StableMetadata
			} else {
				ephemeralMetadata = c.rollout.Spec.Strategy.BlueGreen.ActiveMetadata
			}
		}
		newRS, _ = replicasetutil.SyncReplicaSetEphemeralPodMetadata(newRS, ephemeralMetadata)
	}

	// Create the new ReplicaSet. If it already exists, then we need to check for possible
	// hash collisions. If there is any other error, we need to report it in the status of
	// the Rollout.
	alreadyExists := false
	createdRS, err := c.kubeclientset.AppsV1().ReplicaSets(c.rollout.Namespace).Create(ctx, newRS, metav1.CreateOptions{})
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
		if controllerRef != nil && controllerRef.UID == c.rollout.UID && replicasetutil.PodTemplateEqualIgnoreHash(&rs.Spec.Template, &c.rollout.Spec.Template) {
			createdRS = rs
			err = nil
			break
		}

		// Matching ReplicaSet is not equal - increment the collisionCount in the RolloutStatus
		// and requeue the Rollout.
		if c.rollout.Status.CollisionCount == nil {
			c.rollout.Status.CollisionCount = new(int32)
		}
		preCollisionCount := *c.rollout.Status.CollisionCount
		*c.rollout.Status.CollisionCount++
		// Update the collisionCount for the Rollout and let it requeue by returning the original
		// error.
		_, roErr := c.argoprojclientset.ArgoprojV1alpha1().Rollouts(c.rollout.Namespace).UpdateStatus(ctx, c.rollout, metav1.UpdateOptions{})
		if roErr == nil {
			c.log.Warnf("Found a hash collision - bumped collisionCount (%d->%d) to resolve it", preCollisionCount, *c.rollout.Status.CollisionCount)
		}
		return nil, err
	case err != nil:
		msg := fmt.Sprintf(conditions.FailedRSCreateMessage, newRS.Name, err)
		c.recorder.Event(c.rollout, corev1.EventTypeWarning, conditions.FailedRSCreateReason, msg)
		newStatus := c.rollout.Status.DeepCopy()
		cond := conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionFalse, conditions.FailedRSCreateReason, msg)
		patchErr := c.patchCondition(c.rollout, newStatus, cond)
		if patchErr != nil {
			c.log.Warnf("Error Patching Rollout: %s", patchErr.Error())
		}
		return nil, err
	default:
		c.log.Infof("Created ReplicaSet %s", createdRS.Name)
	}

	if !alreadyExists && newReplicasCount > 0 {
		c.recorder.Eventf(c.rollout, corev1.EventTypeNormal, "ScalingReplicaSet", "Scaled up replica set %s to %d", createdRS.Name, newReplicasCount)
	}

	if annotations.SetRolloutRevision(c.rollout, newRevision) {
		updatedRollout, err := c.argoprojclientset.ArgoprojV1alpha1().Rollouts(c.rollout.Namespace).Update(ctx, c.rollout, metav1.UpdateOptions{})
		if err != nil {
			return nil, err
		}
		c.rollout = updatedRollout
		c.log.Infof("Updated rollout revision to %s", c.rollout.Annotations[annotations.RevisionAnnotation])
	}
	if !alreadyExists {
		msg := fmt.Sprintf(conditions.NewReplicaSetMessage, createdRS.Name)
		condition := conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionTrue, conditions.NewReplicaSetReason, msg)
		conditions.SetRolloutCondition(&c.rollout.Status, *condition)
		updatedRollout, err := c.argoprojclientset.ArgoprojV1alpha1().Rollouts(c.rollout.Namespace).UpdateStatus(ctx, c.rollout, metav1.UpdateOptions{})
		if err != nil {
			return nil, err
		}
		c.rollout = updatedRollout
		c.newRollout = updatedRollout
		c.log.Infof("Set rollout condition: %v", condition)
	}
	return createdRS, err
}

// syncReplicasOnly is responsible for reconciling rollouts on scaling events.
func (c *rolloutContext) syncReplicasOnly(isScaling bool) error {
	c.log.Infof("Syncing replicas only (userPaused %v, isScaling: %v)", c.rollout.Spec.Paused, isScaling)
	_, err := c.getAllReplicaSetsAndSyncRevision(false)
	if err != nil {
		return err
	}

	// NOTE: it is possible for newRS to be nil (e.g. when template and replicas changed at same time)
	if c.rollout.Spec.Strategy.BlueGreen != nil {
		previewSvc, activeSvc, err := c.getPreviewAndActiveServices()
		// Keep existing analysis runs if the rollout is paused
		c.SetCurrentAnalysisRuns(c.currentArs)
		if err != nil {
			return nil
		}
		err = c.podRestarter.Reconcile(c)
		if err != nil {
			return err
		}
		if err := c.reconcileBlueGreenReplicaSets(activeSvc); err != nil {
			// If we get an error while trying to scale, the rollout will be requeued
			// so we can abort this resync
			return err
		}
		return c.syncRolloutStatusBlueGreen(previewSvc, activeSvc)
	}
	// The controller wants to use the rolloutCanary method to reconcile the rollout if the rollout is not paused.
	// If there are no scaling events, the rollout should only sync its status
	if c.rollout.Spec.Strategy.Canary != nil {
		err = c.podRestarter.Reconcile(c)
		if err != nil {
			return err
		}

		if isScaling {
			if _, err := c.reconcileCanaryReplicaSets(); err != nil {
				// If we get an error while trying to scale, the rollout will be requeued
				// so we can abort this resync
				return err
			}
		}
		// Reconciling AnalysisRuns to manage Background AnalysisRun if necessary
		err = c.reconcileAnalysisRuns()
		if err != nil {
			return err
		}

		// reconcileCanaryPause will ensure we will requeue this rollout at the appropriate time
		// if we are at a pause step with a duration.
		c.reconcileCanaryPause()
		err = c.reconcileStableAndCanaryService()
		if err != nil {
			return err
		}

		err = c.reconcileTrafficRouting()
		if err != nil {
			return err
		}

		return c.syncRolloutStatusCanary()
	}
	return fmt.Errorf("no rollout strategy provided")
}

// isScalingEvent checks whether the provided rollout has been updated with a scaling event
// by looking at the desired-replicas annotation in the active replica sets of the rollout.
//
// rsList should come from getReplicaSetsForRollout(r).
func (c *rolloutContext) isScalingEvent() (bool, error) {
	_, err := c.getAllReplicaSetsAndSyncRevision(false)
	if err != nil {
		return false, err
	}

	for _, rs := range controller.FilterActiveReplicaSets(c.allRSs) {
		desired, ok := annotations.GetDesiredReplicasAnnotation(rs)
		if !ok {
			continue
		}
		if desired != defaults.GetReplicasOrDefault(c.rollout.Spec.Replicas) {
			return true, nil
		}
	}
	return false, nil
}

func (c *rolloutContext) scaleReplicaSetAndRecordEvent(rs *appsv1.ReplicaSet, newScale int32) (bool, *appsv1.ReplicaSet, error) {
	// No need to scale
	if *(rs.Spec.Replicas) == newScale && !annotations.ReplicasAnnotationsNeedUpdate(rs, defaults.GetReplicasOrDefault(c.rollout.Spec.Replicas)) {
		return false, rs, nil
	}
	var scalingOperation string
	if *(rs.Spec.Replicas) < newScale {
		scalingOperation = "up"
	} else {
		scalingOperation = "down"
	}
	scaled, newRS, err := c.scaleReplicaSet(rs, newScale, c.rollout, scalingOperation)
	return scaled, newRS, err
}

func (c *rolloutContext) scaleReplicaSet(rs *appsv1.ReplicaSet, newScale int32, rollout *v1alpha1.Rollout, scalingOperation string) (bool, *appsv1.ReplicaSet, error) {
	ctx := context.TODO()
	sizeNeedsUpdate := *(rs.Spec.Replicas) != newScale
	fullScaleDown := newScale == int32(0)
	rolloutReplicas := defaults.GetReplicasOrDefault(rollout.Spec.Replicas)
	annotationsNeedUpdate := annotations.ReplicasAnnotationsNeedUpdate(rs, rolloutReplicas)

	scaled := false
	var err error
	if sizeNeedsUpdate || annotationsNeedUpdate {
		rsCopy := rs.DeepCopy()
		oldScale := defaults.GetReplicasOrDefault(rs.Spec.Replicas)
		*(rsCopy.Spec.Replicas) = newScale
		annotations.SetReplicasAnnotations(rsCopy, rolloutReplicas)
		if fullScaleDown {
			delete(rsCopy.Annotations, v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey)
		}
		rs, err = c.kubeclientset.AppsV1().ReplicaSets(rsCopy.Namespace).Update(ctx, rsCopy, metav1.UpdateOptions{})
		if err == nil && sizeNeedsUpdate {
			scaled = true
			c.recorder.Eventf(rollout, corev1.EventTypeNormal, "ScalingReplicaSet", "Scaled %s replica set %s from %d to %d", scalingOperation, rs.Name, oldScale, newScale)
		}
	}
	return scaled, rs, err
}

// calculateStatus calculates the common fields for all rollouts by looking into the provided replica sets.
func (c *rolloutContext) calculateBaseStatus() v1alpha1.RolloutStatus {
	prevStatus := c.rollout.Status

	prevCond := conditions.GetRolloutCondition(prevStatus, v1alpha1.InvalidSpec)
	err := c.getRolloutValidationErrors()
	if err == nil && prevCond != nil {
		conditions.RemoveRolloutCondition(&prevStatus, v1alpha1.InvalidSpec)
	}

	var currentPodHash string
	if c.newRS == nil {
		// newRS potentially might be nil when called by syncReplicasOnly(). For this
		// to happen, the user would have had to simultaneously change the number of replicas, and
		// the pod template spec at the same time.
		currentPodHash = controller.ComputeHash(&c.rollout.Spec.Template, c.rollout.Status.CollisionCount)
		c.log.Warnf("Assuming %s for new replicaset pod hash", currentPodHash)
	} else {
		currentPodHash = c.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	}

	newStatus := c.newStatus
	newStatus.CurrentPodHash = currentPodHash
	newStatus.Replicas = replicasetutil.GetActualReplicaCountForReplicaSets(c.allRSs)
	newStatus.UpdatedReplicas = replicasetutil.GetActualReplicaCountForReplicaSets([]*appsv1.ReplicaSet{c.newRS})
	newStatus.ReadyReplicas = replicasetutil.GetReadyReplicaCountForReplicaSets(c.allRSs)
	newStatus.CollisionCount = c.rollout.Status.CollisionCount
	newStatus.Conditions = prevStatus.Conditions
	newStatus.RestartedAt = c.newStatus.RestartedAt
	newStatus.PromoteFull = (newStatus.CurrentPodHash != newStatus.StableRS) && prevStatus.PromoteFull
	return newStatus
}

// cleanupRollout is responsible for cleaning up a rollout ie. retains all but the latest N old replica sets
// where N=r.Spec.RevisionHistoryLimit. Old replica sets are older versions of the podtemplate of a rollout kept
// around by default 1) for historical reasons.
func (c *rolloutContext) cleanupRollouts(oldRSs []*appsv1.ReplicaSet) error {
	ctx := context.TODO()
	revHistoryLimit := defaults.GetRevisionHistoryLimitOrDefault(c.rollout)

	// Avoid deleting replica set with deletion timestamp set
	aliveFilter := func(rs *appsv1.ReplicaSet) bool {
		return rs != nil && rs.ObjectMeta.DeletionTimestamp == nil
	}
	cleanableRSes := controller.FilterReplicaSets(oldRSs, aliveFilter)

	diff := int32(len(cleanableRSes)) - revHistoryLimit
	if diff <= 0 {
		return nil
	}
	c.log.Infof("Cleaning up %d old replicasets from revision history limit %d", len(cleanableRSes), revHistoryLimit)

	sort.Sort(controller.ReplicaSetsByCreationTimestamp(cleanableRSes))
	podHashToArList := analysisutil.SortAnalysisRunByPodHash(c.otherArs)
	podHashToExList := experimentutil.SortExperimentsByPodHash(c.otherExs)
	c.log.Info("Looking to cleanup old replica sets")
	for i := int32(0); i < diff; i++ {
		rs := cleanableRSes[i]
		// Avoid delete replica set with non-zero replica counts
		if rs.Status.Replicas != 0 || *(rs.Spec.Replicas) != 0 || rs.Generation > rs.Status.ObservedGeneration || rs.DeletionTimestamp != nil {
			continue
		}
		c.log.Infof("Trying to cleanup replica set %q", rs.Name)
		if err := c.kubeclientset.AppsV1().ReplicaSets(rs.Namespace).Delete(ctx, rs.Name, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
			// Return error instead of aggregating and continuing DELETEs on the theory
			// that we may be overloading the api server.
			return err
		}
		if podHash, ok := rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]; ok {
			if ars, ok := podHashToArList[podHash]; ok {
				c.log.Infof("Cleaning up associated analysis runs with ReplicaSet '%s'", rs.Name)
				err := c.deleteAnalysisRuns(ars)
				if err != nil {
					return err
				}
			}
			if exs, ok := podHashToExList[podHash]; ok {
				c.log.Infof("Cleaning up associated experiments with ReplicaSet '%s'", rs.Name)
				err := c.deleteExperiments(exs)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// checkPausedConditions checks if the given rollout is paused or not and adds an appropriate condition.
// These conditions are needed so that we won't accidentally report lack of progress for resumed rollouts
// that were paused for longer than progressDeadlineSeconds.
func (c *rolloutContext) checkPausedConditions() error {
	cond := conditions.GetRolloutCondition(c.rollout.Status, v1alpha1.RolloutProgressing)
	pausedCondExists := cond != nil && cond.Reason == conditions.PausedRolloutReason

	isPaused := len(c.rollout.Status.PauseConditions) > 0 || c.rollout.Spec.Paused
	var updatedCondition *v1alpha1.RolloutCondition
	if isPaused && !pausedCondExists {
		updatedCondition = conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionUnknown, conditions.PausedRolloutReason, conditions.PausedRolloutMessage)
	} else if !isPaused && pausedCondExists {
		updatedCondition = conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionUnknown, conditions.ResumedRolloutReason, conditions.ResumeRolloutMessage)
	}

	abortCondExists := cond != nil && cond.Reason == conditions.RolloutAbortedReason
	if !c.rollout.Status.Abort && abortCondExists {
		updatedCondition = conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionUnknown, conditions.RolloutRetryReason, conditions.RolloutRetryMessage)
	}

	if updatedCondition == nil {
		return nil
	}

	newStatus := c.rollout.Status.DeepCopy()
	err := c.patchCondition(c.rollout, newStatus, updatedCondition)
	return err
}

func (c *rolloutContext) patchCondition(r *v1alpha1.Rollout, newStatus *v1alpha1.RolloutStatus, condition *v1alpha1.RolloutCondition) error {
	ctx := context.TODO()
	conditions.SetRolloutCondition(newStatus, *condition)
	newStatus.ObservedGeneration = strconv.Itoa(int(c.rollout.Generation))
	logCtx := logutil.WithVersionFields(c.log, r)
	patch, modified, err := diff.CreateTwoWayMergePatch(
		&v1alpha1.Rollout{
			Status: r.Status,
		},
		&v1alpha1.Rollout{
			Status: *newStatus,
		}, v1alpha1.Rollout{})
	if err != nil {
		logCtx.Errorf("Error constructing app status patch: %v", err)
		return err
	}
	if !modified {
		logCtx.Info("No status changes. Skipping patch")
		return nil
	}
	_, err = c.argoprojclientset.ArgoprojV1alpha1().Rollouts(r.Namespace).Patch(ctx, r.Name, patchtypes.MergePatchType, patch, metav1.PatchOptions{}, "status")
	if err != nil {
		logCtx.Warnf("Error patching rollout: %v", err)
		return err
	}
	logCtx.Infof("Patched conditions: %s", string(patch))
	return nil
}

// isIndefiniteStep returns whether or not the rollout is at an Experiment or Analysis step which should
// not affect the progressDeadlineSeconds
func isIndefiniteStep(r *v1alpha1.Rollout) bool {
	currentStep, _ := replicasetutil.GetCurrentCanaryStep(r)
	return currentStep != nil && (currentStep.Experiment != nil || currentStep.Analysis != nil)
}

func (c *rolloutContext) calculateRolloutConditions(newStatus v1alpha1.RolloutStatus) v1alpha1.RolloutStatus {
	if len(c.rollout.Status.PauseConditions) > 0 || c.rollout.Spec.Paused {
		return newStatus
	}

	// If there is only one replica set that is active then that means we are not running
	// a new rollout and this is a resync where we don't need to estimate any progress.
	// In such a case, we should simply not estimate any progress for this rollout.
	currentCond := conditions.GetRolloutCondition(c.rollout.Status, v1alpha1.RolloutProgressing)
	isCompleteRollout := newStatus.Replicas == newStatus.AvailableReplicas && currentCond != nil && currentCond.Reason == conditions.NewRSAvailableReason
	// Check for progress only if the latest rollout hasn't completed yet.
	if !isCompleteRollout {
		switch {
		case c.pauseContext.IsAborted():
			var condition *v1alpha1.RolloutCondition
			if c.pauseContext.abortMessage != "" {
				condition = conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionFalse, conditions.RolloutAbortedReason, c.pauseContext.abortMessage)
			} else {
				condition = conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionFalse, conditions.RolloutAbortedReason, conditions.RolloutAbortedMessage)
			}
			conditions.SetRolloutCondition(&newStatus, *condition)
		case conditions.RolloutComplete(c.rollout, &newStatus):
			// Update the rollout conditions with a message for the new replica set that
			// was successfully deployed. If the condition already exists, we ignore this update.
			msg := fmt.Sprintf(conditions.RolloutCompletedMessage, c.rollout.Name)
			if c.newRS != nil {
				msg = fmt.Sprintf(conditions.ReplicaSetCompletedMessage, c.newRS.Name)
			}
			condition := conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionTrue, conditions.NewRSAvailableReason, msg)
			conditions.SetRolloutCondition(&newStatus, *condition)
		case conditions.RolloutProgressing(c.rollout, &newStatus):
			// If there is any progress made, continue by not checking if the rollout failed. This
			// behavior emulates the rolling updater progressDeadline check.
			msg := fmt.Sprintf(conditions.RolloutProgressingMessage, c.rollout.Name)
			if c.newRS != nil {
				msg = fmt.Sprintf(conditions.ReplicaSetProgressingMessage, c.newRS.Name)
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
		case !isIndefiniteStep(c.rollout) && conditions.RolloutTimedOut(c.rollout, &newStatus):
			// Update the rollout with a timeout condition. If the condition already exists,
			// we ignore this update.
			msg := fmt.Sprintf(conditions.RolloutTimeOutMessage, c.rollout.Name)
			if c.newRS != nil {
				msg = fmt.Sprintf(conditions.ReplicaSetTimeOutMessage, c.newRS.Name)
			}
			condition := conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionFalse, conditions.TimedOutReason, msg)
			conditions.SetRolloutCondition(&newStatus, *condition)
		}
	}

	activeRS, _ := replicasetutil.GetReplicaSetByTemplateHash(c.allRSs, newStatus.BlueGreen.ActiveSelector)
	if c.rollout.Spec.Strategy.BlueGreen != nil && activeRS != nil && annotations.IsSaturated(c.rollout, activeRS) {
		availability := conditions.NewRolloutCondition(v1alpha1.RolloutAvailable, corev1.ConditionTrue, conditions.AvailableReason, conditions.AvailableMessage)
		conditions.SetRolloutCondition(&newStatus, *availability)
	} else if c.rollout.Spec.Strategy.Canary != nil && replicasetutil.GetAvailableReplicaCountForReplicaSets(c.allRSs) >= defaults.GetReplicasOrDefault(c.rollout.Spec.Replicas) {
		availability := conditions.NewRolloutCondition(v1alpha1.RolloutAvailable, corev1.ConditionTrue, conditions.AvailableReason, conditions.AvailableMessage)
		conditions.SetRolloutCondition(&newStatus, *availability)
	} else {
		noAvailability := conditions.NewRolloutCondition(v1alpha1.RolloutAvailable, corev1.ConditionFalse, conditions.AvailableReason, conditions.NotAvailableMessage)
		conditions.SetRolloutCondition(&newStatus, *noAvailability)
	}

	// Move failure conditions of all replica sets in rollout conditions. For now,
	// only one failure condition is returned from getReplicaFailures.
	if replicaFailureCond := c.getReplicaFailures(c.allRSs, c.newRS); len(replicaFailureCond) > 0 {
		// There will be only one ReplicaFailure condition on the replica set.
		conditions.SetRolloutCondition(&newStatus, replicaFailureCond[0])
	} else {
		conditions.RemoveRolloutCondition(&newStatus, v1alpha1.RolloutReplicaFailure)
	}
	return newStatus
}

// persistRolloutStatus persists updates to rollout status. If no changes were made, it is a no-op
func (c *rolloutContext) persistRolloutStatus(newStatus *v1alpha1.RolloutStatus) error {
	ctx := context.TODO()
	c.pauseContext.CalculatePauseStatus(newStatus)
	newStatus.ObservedGeneration = strconv.Itoa(int(c.rollout.Generation))
	logCtx := logutil.WithVersionFields(c.log, c.rollout)
	patch, modified, err := diff.CreateTwoWayMergePatch(
		&v1alpha1.Rollout{
			Status: c.rollout.Status,
		},
		&v1alpha1.Rollout{
			Status: *newStatus,
		}, v1alpha1.Rollout{})
	if err != nil {
		logCtx.Errorf("Error constructing app status patch: %v", err)
		return err
	}
	if !modified {
		logCtx.Info("No status changes. Skipping patch")
		c.requeueStuckRollout(*newStatus)
		return nil
	}
	newRollout, err := c.argoprojclientset.ArgoprojV1alpha1().Rollouts(c.rollout.Namespace).Patch(ctx, c.rollout.Name, patchtypes.MergePatchType, patch, metav1.PatchOptions{}, "status")
	if err != nil {
		logCtx.Warningf("Error updating rollout: %v", err)
		return err
	}
	logCtx.Infof("Patched: %s", patch)
	c.newRollout = newRollout
	return nil
}

// used for unit testing
var nowFn = func() time.Time { return time.Now() }

// requeueStuckRollout checks whether the provided rollout needs to be synced for a progress
// check. It returns the time after the rollout will be requeued for the progress check, 0 if it
// will be requeued now, or -1 if it does not need to be requeued.
func (c *rolloutContext) requeueStuckRollout(newStatus v1alpha1.RolloutStatus) time.Duration {
	currentCond := conditions.GetRolloutCondition(c.rollout.Status, v1alpha1.RolloutProgressing)
	// Can't estimate progress if there is no deadline in the spec or progressing condition in the current status.
	if currentCond == nil {
		return time.Duration(-1)
	}
	// No need to estimate progress if the rollout is complete or already timed out.
	isPaused := len(c.rollout.Status.PauseConditions) > 0 || c.rollout.Spec.Paused
	if conditions.RolloutComplete(c.rollout, &newStatus) || currentCond.Reason == conditions.TimedOutReason || isPaused || c.rollout.Status.Abort || isIndefiniteStep(c.rollout) {
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
	progressDeadlineSeconds := defaults.GetProgressDeadlineSecondsOrDefault(c.rollout)
	after := currentCond.LastUpdateTime.Time.Add(time.Duration(progressDeadlineSeconds) * time.Second).Sub(nowFn())
	// If the remaining time is less than a second, then requeue the deployment immediately.
	// Make it ratelimited so we stay on the safe side, eventually the Deployment should
	// transition either to a Complete or to a TimedOut condition.
	if after < time.Second {
		c.log.Infof("Queueing up Rollout for a progress check now")
		c.enqueueRollout(c.rollout)
		return time.Duration(0)
	}
	c.log.Infof("Queueing up rollout for a progress after %ds", int(after.Seconds()))
	// Add a second to avoid milliseconds skew in AddAfter.
	// See https://github.com/kubernetes/kubernetes/issues/39785#issuecomment-279959133 for more info.
	c.enqueueRolloutAfter(c.rollout, after+time.Second)
	return after
}

// getReplicaFailures will convert replica failure conditions from replica sets
// to rollout conditions.
func (c *rolloutContext) getReplicaFailures(allRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet) []v1alpha1.RolloutCondition {
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
