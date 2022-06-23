package rollout

import (
	"time"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

type pauseContext struct {
	rollout *v1alpha1.Rollout
	log     *log.Entry

	addPauseReasons      []v1alpha1.PauseReason
	removePauseReasons   []v1alpha1.PauseReason
	clearPauseConditions bool
	addAbort             bool
	removeAbort          bool
	abortMessage         string
}

func (pCtx *pauseContext) HasAddPause() bool {
	return len(pCtx.addPauseReasons) > 0
}

func (pCtx *pauseContext) IsAborted() bool {
	if pCtx.removeAbort {
		return false
	}
	if pCtx.addAbort || pCtx.rollout.Status.Abort {
		return true
	}
	return false
}

func (pCtx *pauseContext) AddAbort(message string) {
	pCtx.addAbort = true
	pCtx.abortMessage = message
}

func (pCtx *pauseContext) RemoveAbort() {
	pCtx.removeAbort = true
}

func (pCtx *pauseContext) AddPauseCondition(reason v1alpha1.PauseReason) {
	pCtx.addPauseReasons = append(pCtx.addPauseReasons, reason)
}

func (pCtx *pauseContext) RemovePauseCondition(reason v1alpha1.PauseReason) {
	pCtx.removePauseReasons = append(pCtx.removePauseReasons, reason)
}
func (pCtx *pauseContext) ClearPauseConditions() {
	pCtx.clearPauseConditions = true
}

func (pCtx *pauseContext) CalculatePauseStatus(newStatus *v1alpha1.RolloutStatus) {
	now := timeutil.MetaNow()
	// if we are already aborted, preserve the original timestamp, otherwise we'll cause a
	// reconciliation hot-loop.
	newAbortedAt := pCtx.rollout.Status.AbortedAt
	if newAbortedAt == nil {
		newAbortedAt = &now
	}
	if pCtx.addAbort {
		newStatus.Abort = true
		newStatus.AbortedAt = newAbortedAt
		return
	}
	if !pCtx.removeAbort && pCtx.rollout.Status.Abort {
		newStatus.Abort = true
		newStatus.AbortedAt = newAbortedAt
		return
	}
	newStatus.Abort = false
	newStatus.AbortedAt = nil

	if pCtx.clearPauseConditions {
		return
	}

	controllerPause := pCtx.rollout.Status.ControllerPause
	statusToRemove := map[v1alpha1.PauseReason]bool{}
	for i := range pCtx.removePauseReasons {
		statusToRemove[pCtx.removePauseReasons[i]] = true
	}

	newPauseConditions := []v1alpha1.PauseCondition{}
	pauseAlreadyExists := map[v1alpha1.PauseReason]bool{}
	for _, cond := range pCtx.rollout.Status.PauseConditions {
		if remove := statusToRemove[cond.Reason]; !remove {
			newPauseConditions = append(newPauseConditions, cond)
		}
		pauseAlreadyExists[cond.Reason] = true
	}

	for i := range pCtx.addPauseReasons {
		reason := pCtx.addPauseReasons[i]
		if exists := pauseAlreadyExists[reason]; !exists {
			pCtx.log.Infof("Adding pause reason %s with start time %s", reason, now.UTC().Format(time.RFC3339))
			cond := v1alpha1.PauseCondition{
				Reason:    reason,
				StartTime: now,
			}
			newPauseConditions = append(newPauseConditions, cond)
			controllerPause = true
		}
	}

	if len(newPauseConditions) == 0 {
		return
	}
	newStatus.ControllerPause = controllerPause
	newStatus.PauseConditions = newPauseConditions
}

func getPauseCondition(rollout *v1alpha1.Rollout, reason v1alpha1.PauseReason) *v1alpha1.PauseCondition {
	for i := range rollout.Status.PauseConditions {
		cond := rollout.Status.PauseConditions[i]
		if cond.Reason == reason {
			return &cond
		}
	}
	return nil
}

// completedPrePromotionAnalysis checks if the Pre Promotion Analysis has completed successfully
func (c *rolloutContext) completedPrePromotionAnalysis() bool {
	if c.rollout.Spec.Strategy.BlueGreen == nil || c.rollout.Spec.Strategy.BlueGreen.PrePromotionAnalysis == nil {
		return true
	}
	currentAr := c.currentArs.BlueGreenPrePromotion
	if currentAr != nil && currentAr.Status.Phase == v1alpha1.AnalysisPhaseSuccessful {
		return true
	}
	return false
}

// CompletedBlueGreenPause returns true if we have already completed our automated pause, either
// because a human has resumed the rollout, or we surpassed autoPromotionSeconds.
func (pCtx *pauseContext) CompletedBlueGreenPause() bool {
	rollout := pCtx.rollout
	if pCtx.HasAddPause() {
		// return false if we just added a pause condition as part of this reconciliation
		return false
	}
	if rollout.Status.BlueGreen.ScaleUpPreviewCheckPoint {
		return true
	}
	if !needsBlueGreenControllerPause(rollout) {
		return true
	}
	pauseCond := getPauseCondition(rollout, v1alpha1.PauseReasonBlueGreenPause)
	if rollout.Spec.Strategy.BlueGreen.AutoPromotionEnabled == nil || *rollout.Spec.Strategy.BlueGreen.AutoPromotionEnabled {
		// autoPromotion is enabled. check if we surpassed the delay
		autoPromotionSeconds := rollout.Spec.Strategy.BlueGreen.AutoPromotionSeconds
		if autoPromotionSeconds == 0 {
			return true
		}
		if rollout.Status.BlueGreen.ScaleUpPreviewCheckPoint {
			return true
		}
		if pauseCond != nil {
			switchDeadline := pauseCond.StartTime.Add(time.Duration(autoPromotionSeconds) * time.Second)
			now := timeutil.MetaNow()
			if now.After(switchDeadline) {
				return true
			}
			return false
		}
		// we never paused the rollout
		return false
	} else {
		// autoPromotion is disabled. the presence of a pause condition means human has not resumed it
		if rollout.Status.ControllerPause {
			return pauseCond == nil
		}
		// status.controllerPause has not yet been set
		return false
	}
}

func (pCtx *pauseContext) CompletedCanaryPauseStep(pause v1alpha1.RolloutPause) bool {
	rollout := pCtx.rollout
	pauseCondition := getPauseCondition(rollout, v1alpha1.PauseReasonCanaryPauseStep)

	if rollout.Status.ControllerPause && pauseCondition == nil {
		pCtx.log.Info("Rollout has been unpaused")
		return true
	} else if pause.Duration != nil {
		now := timeutil.MetaNow()
		if pauseCondition != nil {
			expiredTime := pauseCondition.StartTime.Add(time.Duration(pause.DurationSeconds()) * time.Second)
			if now.After(expiredTime) {
				pCtx.log.Info("Rollout has waited the duration of the pause step")
				return true
			}
		}
	}
	return false
}

func (c *rolloutContext) checkEnqueueRolloutDuringWait(startTime metav1.Time, durationInSeconds int32) {
	now := timeutil.MetaNow()
	expiredTime := startTime.Add(time.Duration(durationInSeconds) * time.Second)
	nextResync := now.Add(c.resyncPeriod)
	if nextResync.After(expiredTime) && expiredTime.After(now.Time) {
		timeRemaining := expiredTime.Sub(now.Time)
		c.log.Infof("Enqueueing Rollout in %s seconds", timeRemaining.String())
		c.enqueueRolloutAfter(c.rollout, timeRemaining)
	}
}
