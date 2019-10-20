package rollout

import (
	"time"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

type pauseContext struct {
	log             *log.Entry
	controllerPause bool
	pauseStartTime  *metav1.Time
	pauseConditions []v1alpha1.PauseCondition

	addPauseReasons    []v1alpha1.PauseReason
	removePauseReasons []v1alpha1.PauseReason
	clearPauseReasons  bool
}

func (pCtx *pauseContext) AddControllerPause(reason v1alpha1.PauseReason) {
	pCtx.addPauseReasons = append(pCtx.addPauseReasons, reason)
}

func (pCtx *pauseContext) RemoveControllerPause(reason v1alpha1.PauseReason) {
	pCtx.removePauseReasons = append(pCtx.removePauseReasons, reason)
}
func (pCtx *pauseContext) ClearPauseReasons() {
	pCtx.clearPauseReasons = true
}

func (pCtx *pauseContext) CalculatePauseStatus(newStatus *v1alpha1.RolloutStatus) {
	if pCtx.clearPauseReasons {
		return
	}
	statusToRemove := map[v1alpha1.PauseReason]bool{}
	for i := range pCtx.removePauseReasons {
		statusToRemove[pCtx.removePauseReasons[i]] = true
	}

	newPauseConditions := []v1alpha1.PauseCondition{}
	pauseAlreadyExists := map[v1alpha1.PauseReason]bool{}
	for _, cond := range pCtx.pauseConditions {
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
		}
	}

	pauseStartTime := pCtx.pauseStartTime
	paused := pCtx.controllerPause

	if len(newPauseConditions) == 0 {
		return
	}

	if len(pCtx.addPauseReasons) > 0 {
		if pauseStartTime == nil {
			pCtx.log.Infof("Setting PauseStartTime to %s", now.UTC().Format(time.RFC3339))
			pauseStartTime = &now
			paused = true
		}
	}

	newStatus.ControllerPause = paused
	newStatus.PauseStartTime = pauseStartTime
	newStatus.PauseConditions = newPauseConditions
}

func completedPauseStep(roCtx *canaryContext, pause v1alpha1.RolloutPause) bool {
	rollout := roCtx.Rollout()
	logCtx := roCtx.Log()
	if pause.Duration != nil {
		now := metav1.Now()
		if rollout.Status.PauseStartTime != nil {
			expiredTime := rollout.Status.PauseStartTime.Add(time.Duration(*pause.Duration) * time.Second)
			if now.After(expiredTime) {
				logCtx.Info("Rollout has waited the duration of the pause step")
				return true
			}
		}
	} else if rollout.Status.PauseStartTime != nil && !rollout.Status.ControllerPause {
		logCtx.Info("Rollout has been unpaused")
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
