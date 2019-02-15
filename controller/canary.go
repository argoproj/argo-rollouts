package controller

import (
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	"time"
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
	scaledUp, err := c.reconcileNewReplicaSet(allRSs, newRS, rollout)
	if err != nil {
		return err
	}
	if scaledUp {
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
	stillReconciling, err := c.reconcileCanarySteps(oldRSs, newRS, stableRS, rollout)
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

func (c *Controller) reconcileCanarySteps(oldRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, stableRS *appsv1.ReplicaSet, rollout *v1alpha1.Rollout) (bool, error) {
	logCtx := logutil.WithRollout(rollout)
	if len(rollout.Spec.Strategy.CanaryStrategy.Steps) == 0 {
		return false, nil
	}
	currentStep, currentStepIndex := replicasetutil.GetCurrentCanaryStep(rollout)

	if len(rollout.Spec.Strategy.CanaryStrategy.Steps) <= int(currentStepIndex) {
		logCtx.Info("No Steps remain in the canary steps")
		return false, nil
	}

	if currentStep.Pause != nil {
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

	if currentStep.Wait != nil {
		now := metav1.Now()
		if rollout.Status.CanaryStatus.WaitStartTime != nil {
			expiredTime := rollout.Status.CanaryStatus.WaitStartTime.Add(time.Duration(*currentStep.Wait) * time.Second)
			nextResync := now.Add(time.Duration(DefaultRolloutResyncPeriod) * time.Second)
			if nextResync.After(expiredTime) && expiredTime.After(now.Time){
				timeRemaining := expiredTime.Sub(now.Time)
				c.enqueueRolloutAfter(rollout, timeRemaining)
			}
		}
		return true, nil
	}

	return false, nil
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

	if currentStep != nil && currentStep.SetWeight != nil && replicasetutil.AtDesiredReplicaCountsForCanary(r, newRS, stableRS, olderRSs){
		currentStepIndex++
	}

	waitStartTime := r.Status.CanaryStatus.WaitStartTime
	if currentStep != nil && currentStep.Wait != nil {
		now := metav1.Now()
		if r.Status.CanaryStatus.WaitStartTime == nil && replicasetutil.AtDesiredReplicaCountsForCanary(r, newRS, stableRS, olderRSs) {
			waitStartTime = &now
		}
		if r.Status.CanaryStatus.WaitStartTime != nil {
			expiredTime := r.Status.CanaryStatus.WaitStartTime.Add(time.Duration(*currentStep.Wait) * time.Second)
			if now.After(expiredTime) {
				waitStartTime = nil
				currentStepIndex++
			}
		}
	}

	newStatus.CanaryStatus.StableRS = r.Status.CanaryStatus.StableRS
	if r.Status.CanaryStatus.StableRS == "" || int(currentStepIndex) == len(r.Spec.Strategy.CanaryStrategy.Steps) {
		newStatus.CanaryStatus.StableRS = newStatus.CurrentPodHash
	}

	newStatus.CanaryStatus.WaitStartTime = waitStartTime
	newStatus.CurrentStepIndex = &currentStepIndex
	newStatus.SetPause = setPause
	return c.persistRolloutStatus(r, &newStatus)
}
