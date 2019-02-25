package controller

import (
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

func (c *Controller) rolloutCanary(rollout *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet) error {
	logCtx := logutil.WithRollout(rollout)
	if replicasetutil.CheckPodSpecChange(rollout) {
		logCtx.Info("Pod Spec changed and need to reset CurrentStepIndex")
		newRS, previousRSs, err := c.getAllReplicaSetsAndSyncRevision(rollout, rsList, false)
		if err != nil {
			return err
		}
		stableRS, oldRSs := replicasetutil.GetStableRS(rollout, newRS, previousRSs)
		return c.syncRolloutStatusCanary(oldRSs, newRS, stableRS, rollout)
	}

	newRS, previousRSs, err := c.getAllReplicaSetsAndSyncRevision(rollout, rsList, true)
	stableRS, oldRSs := replicasetutil.GetStableRS(rollout, newRS, previousRSs)
	if err != nil {
		return err
	}
	allRSs := append(oldRSs, newRS)
	if stableRS != nil {
		allRSs = append(allRSs, stableRS)
	}

	logCtx.Info("Reconciling StableRS")
	scaledStableRS, err := c.reconcileStableRS(oldRSs, newRS, stableRS, rollout)
	if err != nil {
		return err
	}
	if scaledStableRS {
		logCtx.Infof("Not finished reconciling stableRS")
		return c.syncRolloutStatusCanary(oldRSs, newRS, stableRS, rollout)
	}

	logCtx.Infof("Reconciling new ReplicaSet '%s'", newRS.Name)
	scaledNewRS, err := c.reconcileNewReplicaSet(allRSs, newRS, rollout)
	if err != nil {
		return err
	}
	if scaledNewRS {
		logCtx.Infof("Not finished reconciling new ReplicaSet '%s'", newRS.Name)
		return c.syncRolloutStatusCanary(oldRSs, newRS, stableRS, rollout)
	}

	logCtx.Info("Reconciling old replica sets")
	scaledDown, err := c.reconcileOldReplicaSetsCanary(allRSs, controller.FilterActiveReplicaSets(oldRSs), newRS, rollout)
	if err != nil {
		return err
	}
	if scaledDown {
		logCtx.Info("Not finished reconciling old replica sets")
		return c.syncRolloutStatusCanary(oldRSs, newRS, stableRS, rollout)
	}

	logCtx.Info("Reconciling Canary Step")
	stillReconciling, err := c.reconcilePause(oldRSs, newRS, stableRS, rollout)
	if err != nil {
		return c.syncRolloutStatusCanary(oldRSs, newRS, stableRS, rollout)
	}
	if stillReconciling {
		logCtx.Infof("Not finished reconciling new Canary Steps")
		return c.syncRolloutStatusCanary(oldRSs, newRS, stableRS, rollout)
	}
	return c.syncRolloutStatusCanary(oldRSs, newRS, stableRS, rollout)
}

func (c *Controller) reconcileStableRS(olderRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, stableRS *appsv1.ReplicaSet, rollout *v1alpha1.Rollout) (bool, error) {
	logCtx := logutil.WithRollout(rollout)
	if !replicasetutil.CheckStableRSExists(newRS, stableRS) {
		logCtx.Info("No StableRS to reconcile")
		return false, nil
	}
	_, stableRSReplicaCount := replicasetutil.CalculateReplicaCountsForCanary(rollout, newRS, stableRS, olderRSs)
	scaled, _, err := c.scaleReplicaSetAndRecordEvent(stableRS, stableRSReplicaCount, rollout)
	return scaled, err
}

func (c *Controller) reconcilePause(oldRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, stableRS *appsv1.ReplicaSet, rollout *v1alpha1.Rollout) (bool, error) {
	logCtx := logutil.WithRollout(rollout)
	if len(rollout.Spec.Strategy.CanaryStrategy.Steps) == 0 {
		return false, nil
	}
	currentStep, currentStepIndex := replicasetutil.GetCurrentCanaryStep(rollout)

	if len(rollout.Spec.Strategy.CanaryStrategy.Steps) <= int(currentStepIndex) {
		logCtx.Info("No Steps remain in the canary steps")
		return false, nil
	}

	if currentStep.Pause == nil {
		logCtx.Info("Not at a pause step")
		return false, nil
	}

	if currentStep.Pause.Duration == nil {
		if rollout.Status.SetPause == nil {
			logCtx.Infof("Pausing the canary for step %d in the canary steps", currentStepIndex)
			boolValue := true
			err := c.PatchSetPause(rollout, &boolValue)
			return true, err
		}

		if rollout.Status.SetPause != nil && !*rollout.Status.SetPause {
			currentStepIndex++
			logCtx.Infof("Status.SetPause is false. Canary is ready for step %d", currentStepIndex)
			err := c.PatchSetPause(rollout, nil)
			return true, err
		}

		return true, nil
	}

	if rollout.Status.CanaryStatus.PauseStartTime != nil {
		now := metav1.Now()
		expiredTime := rollout.Status.CanaryStatus.PauseStartTime.Add(time.Duration(*currentStep.Pause.Duration) * time.Second)
		nextResync := now.Add(c.resyncPeriod)
		if nextResync.After(expiredTime) && expiredTime.After(now.Time) {
			timeRemaining := expiredTime.Sub(now.Time)
			c.enqueueRolloutAfter(rollout, timeRemaining)
		}
	}
	return true, nil
}

func (c *Controller) reconcileOldReplicaSetsCanary(allRSs []*appsv1.ReplicaSet, oldRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, rollout *v1alpha1.Rollout) (bool, error) {
	logCtx := logutil.WithRollout(rollout)
	oldPodsCount := replicasetutil.GetReplicaCountForReplicaSets(oldRSs)
	if oldPodsCount == 0 {
		// Can't scale down further
		return false, nil
	}

	//logCtx.Infof("New replica set %s/%s has %d available pods.", newRS.Namespace, newRS.Name, newRS.Status.AvailableReplicas)
	//
	//annotations.GetDesiredReplicasAnnotation(newRS)

	// Clean up unhealthy replicas first, otherwise unhealthy replicas will block rollout
	// and cause timeout. See https://github.com/kubernetes/kubernetes/issues/16737
	oldRSs, cleanupCount, err := c.cleanupUnhealthyReplicas(oldRSs, rollout)
	if err != nil {
		return false, nil
	}
	logCtx.Infof("Cleaned up unhealthy replicas from old RSes by %d", cleanupCount)

	// Scale down old replica sets, need check replicasToKeep to ensure we can scale down
	scaledDownCount, err := c.scaleDownOldReplicaSetsForCanary(allRSs, oldRSs, rollout)
	if err != nil {
		return false, nil
	}
	logCtx.Infof("Scaled down old RSes by %d", scaledDownCount)

	totalScaledDown := cleanupCount + scaledDownCount
	return totalScaledDown > 0, nil
}

// scaleDownOldReplicaSetsForBlueGreen scales down old replica sets when rollout strategy is "Blue Green".
func (c *Controller) scaleDownOldReplicaSetsForCanary(allRSs []*appsv1.ReplicaSet, oldRSs []*appsv1.ReplicaSet, rollout *v1alpha1.Rollout) (int32, error) {
	availablePodCount := replicasetutil.GetAvailableReplicaCountForReplicaSets(allRSs)
	minAvailable := defaults.GetRolloutReplicasOrDefault(rollout) - replicasetutil.MaxUnavailable(rollout)
	maxScaleDown := availablePodCount - minAvailable
	if maxScaleDown <= 0 {
		// Cannot scale down.
		return 0, nil
	}
	logutil.WithRollout(rollout).Infof("Found %d available pods, scaling down old RSes", availablePodCount)

	sort.Sort(controller.ReplicaSetsByCreationTimestamp(oldRSs))

	totalScaledDown := int32(0)
	for _, targetRS := range oldRSs {
		if maxScaleDown <= 0 {
			break
		}
		if *(targetRS.Spec.Replicas) == 0 {
			// cannot scale down this ReplicaSet.
			continue
		}
		scaleDownCount := *(targetRS.Spec.Replicas)
		// Scale down.
		newReplicasCount := int32(0)
		if scaleDownCount > maxScaleDown {
			newReplicasCount = maxScaleDown
		}
		_, _, err := c.scaleReplicaSetAndRecordEvent(targetRS, newReplicasCount, rollout)
		if err != nil {
			return totalScaledDown, err
		}
		maxScaleDown -= newReplicasCount
		totalScaledDown += scaleDownCount
	}

	return totalScaledDown, nil
}

func (c *Controller) syncRolloutStatusCanary(olderRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, stableRS *appsv1.ReplicaSet, r *v1alpha1.Rollout) error {
	logCtx := logutil.WithRollout(r)
	allRSs := append(olderRSs, newRS)
	if replicasetutil.CheckStableRSExists(newRS, stableRS) {
		allRSs = append(allRSs, stableRS)
	}
	newStatus := c.calculateBaseStatus(allRSs, newRS, r)

	setPause := r.Status.SetPause

	currentStep, currentStepIndex := replicasetutil.GetCurrentCanaryStep(r)
	if replicasetutil.CheckPodSpecChange(r) {
		currentStepIndex = 0
		setPause = nil
	} else if currentStep != nil && currentStep.Pause != nil && r.Status.SetPause != nil && !*r.Status.SetPause {
		currentStepIndex++
		logCtx.Infof("Incrementing the Current Step Index to %d", currentStepIndex)
	}

	if currentStep != nil && currentStep.SetWeight != nil && replicasetutil.AtDesiredReplicaCountsForCanary(r, newRS, stableRS, olderRSs) {
		currentStepIndex++
	}

	pauseStartTime := r.Status.CanaryStatus.PauseStartTime
	if currentStep != nil && currentStep.Pause != nil && currentStep.Pause.Duration != nil {
		now := metav1.Now()
		if r.Status.CanaryStatus.PauseStartTime == nil && replicasetutil.AtDesiredReplicaCountsForCanary(r, newRS, stableRS, olderRSs) {
			pauseStartTime = &now
		}
		if r.Status.CanaryStatus.PauseStartTime != nil {
			expiredTime := r.Status.CanaryStatus.PauseStartTime.Add(time.Duration(*currentStep.Pause.Duration) * time.Second)
			if now.After(expiredTime) {
				pauseStartTime = nil
				currentStepIndex++
			}
		}
	}

	newStatus.CanaryStatus.StableRS = r.Status.CanaryStatus.StableRS
	if r.Status.CanaryStatus.StableRS == "" || int(currentStepIndex) == len(r.Spec.Strategy.CanaryStrategy.Steps) {
		newStatus.CanaryStatus.StableRS = newStatus.CurrentPodHash
	}

	newStatus.CanaryStatus.PauseStartTime = pauseStartTime
	newStatus.CurrentStepIndex = &currentStepIndex
	newStatus.SetPause = setPause
	return c.persistRolloutStatus(r, &newStatus)
}

// scaleCanary scales the rollout with a canary strategy on a scaling event. First, it checks if there is only one
// replicaset with more than 0 replicas. If there is only one replicas with more than 0 replicas, it scales that
// replicaset to the rollout's replica value.  Afterwards, it checks if the newRS or the stableRS are at the desired
// number of replicas. If either of them are at the desired state, the function will scale down the rest of the older
// replicas. This will prevent the deadlock in the next steps due to old replicasets taking available replicas.
// Afterwards, the function calculates the number of replicas to add, which can be negative.  From there, the function
// starts to scale the replicasets until they are at the desired state or it cannot scale down any more.
func (c *Controller) scaleCanary(oldRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, stableRS *appsv1.ReplicaSet, rollout *v1alpha1.Rollout) error {
	rolloutReplicas := defaults.GetRolloutReplicasOrDefault(rollout)
	// If there is only one active replica set, then we should scale that up to the full count of the
	// rollout. If there are no active replica sets, and there is only one RS with replicas, then we
	// should scale up the newest replica set.
	previousRS := oldRSs
	if stableRS != nil {
		previousRS = append(previousRS, stableRS)
	}

	if activeOrLatest := replicasetutil.FindActiveOrLatest(newRS, previousRS); activeOrLatest != nil {
		if *(activeOrLatest.Spec.Replicas) != rolloutReplicas {
			_, _, err := c.scaleReplicaSetAndRecordEvent(activeOrLatest, rolloutReplicas, rollout)
			return err
		}
	}

	desiredNewRSReplicaCount, desiredStableRSReplicaCount := replicasetutil.DesiredReplicaCountsForCanary(rollout, newRS, stableRS)

	// If the newRS and/or stableRS are at their desired state, the old replica sets should be fully scaled down.
	// This case handles replica set adoption during a saturated new replica set.
	newRSAtDesired := newRS == nil || desiredNewRSReplicaCount == *newRS.Spec.Replicas
	stableRSAtDesired := stableRS == nil || desiredStableRSReplicaCount == *stableRS.Spec.Replicas
	if newRSAtDesired || stableRSAtDesired {
		for _, old := range controller.FilterActiveReplicaSets(oldRSs) {
			if _, _, err := c.scaleReplicaSetAndRecordEvent(old, 0, rollout); err != nil {
				return err
			}
		}
		return nil
	}

	allRSs := controller.FilterActiveReplicaSets(append(oldRSs, newRS, stableRS))
	allRSsReplicas := replicasetutil.GetReplicaCountForReplicaSets(allRSs)

	rolloutSpecReplicas := defaults.GetRolloutReplicasOrDefault(rollout)
	allowedSize := rolloutSpecReplicas + replicasetutil.MaxSurge(rollout)

	// Number of additional replicas that can be either added or removed from the total
	// replicas count. These replicas should be distributed proportionally to the active
	// replica sets.
	rolloutReplicasToAdd := allowedSize - allRSsReplicas

	// The additional replicas should be distributed proportionally amongst the active
	// replica sets from the larger to the smaller in size replica set. Scaling direction
	// drives what happens in case we are trying to scale replica sets of the same size.
	// In such a case when scaling up, we should scale up stable replica set first, and
	// when scaling down, we should scale down older replica sets  afirst.
	var scalingOperation string
	switch {
	case rolloutReplicasToAdd > 0:
		sort.Sort(controller.ReplicaSetsBySizeNewer(oldRSs))
		allRSs = append([]*appsv1.ReplicaSet{stableRS, newRS}, oldRSs...)
		scalingOperation = "up"

	case rolloutReplicasToAdd < 0:
		sort.Sort(controller.ReplicaSetsBySizeOlder(oldRSs))
		allRSs = append(oldRSs, newRS, stableRS)
		scalingOperation = "down"
	}

	// Iterate over all active replica sets and estimate proportions for each of them.
	// The absolute value of rolloutReplicasAdded should never exceed the absolute
	// value of rolloutReplicasToAdd.
	rolloutReplicasAdded := int32(0)

	// Update all replica sets
	for i := range allRSs {
		rs := allRSs[i]
		if rs == nil {
			continue
		}

		desiredNewReplicaCount := int32(0)
		if stableRS != nil && rs != nil && stableRS.Name == rs.Name {
			desiredNewReplicaCount = desiredStableRSReplicaCount
		}

		if newRS != nil && rs != nil && newRS.Name == rs.Name {
			desiredNewReplicaCount = desiredNewRSReplicaCount
		}

		proportion := replicasetutil.GetProportion(rs, rolloutReplicasToAdd, rolloutReplicasAdded, desiredNewReplicaCount)
		rolloutReplicasAdded = rolloutReplicasAdded + proportion

		if _, _, err := c.scaleReplicaSet(rs, *rs.Spec.Replicas+proportion, rollout, scalingOperation); err != nil {
			// Return as soon as we fail, the rollout is requeued
			return err
		}
	}

	return nil
}
