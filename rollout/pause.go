package rollout

import (
	"time"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

type pauseContext struct {
	rollout *v1alpha1.Rollout
	log     *log.Entry

	addPauseReasons      []v1alpha1.PauseReason
	removePauseReasons   []v1alpha1.PauseReason
	clearPauseConditions bool
	addAbort             bool
	removeAbort          bool
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

func (pCtx *pauseContext) AddAbort() {
	pCtx.addAbort = true
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
	if pCtx.addAbort {
		newStatus.Abort = true
		return
	}
	if !pCtx.removeAbort && pCtx.rollout.Status.Abort {
		newStatus.Abort = true
		return
	}
	newStatus.Abort = false

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

	now := metav1.Now()
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

func (pCtx *pauseContext) GetPauseCondition(reason v1alpha1.PauseReason) *v1alpha1.PauseCondition {
	for i := range pCtx.rollout.Status.PauseConditions {
		cond := pCtx.rollout.Status.PauseConditions[i]
		if cond.Reason == reason {
			return &cond
		}
	}
	return nil
}

func (pCtx *pauseContext) CompletedPauseStep(pause v1alpha1.RolloutPause) bool {
	rollout := pCtx.rollout
	pauseCondition := pCtx.GetPauseCondition(v1alpha1.PauseReasonCanaryPauseStep)

	if pause.Duration != nil {
		now := metav1.Now()
		if pauseCondition != nil {
			expiredTime := pauseCondition.StartTime.Add(time.Duration(*pause.Duration) * time.Second)
			if now.After(expiredTime) {
				pCtx.log.Info("Rollout has waited the duration of the pause step")
				return true
			}
		}
	} else if rollout.Status.ControllerPause && pauseCondition == nil {
		pCtx.log.Info("Rollout has been unpaused")
		return true
	}
	return false
}

func (c *RolloutController) checkEnqueueRolloutDuringWait(rollout *v1alpha1.Rollout, startTime metav1.Time, durationInSeconds int32) {
	logCtx := logutil.WithRollout(rollout)
	now := metav1.Now()
	expiredTime := startTime.Add(time.Duration(durationInSeconds) * time.Second)
	nextResync := now.Add(c.resyncPeriod)
	if nextResync.After(expiredTime) && expiredTime.After(now.Time) {
		timeRemaining := expiredTime.Sub(now.Time)
		logCtx.Infof("Enqueueing Rollout in %s seconds", timeRemaining.String())
		c.enqueueRolloutAfter(rollout, timeRemaining)
	}
}
