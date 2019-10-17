package rollout

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

type pauseContext struct {
	addControllerPause    bool
	removeControllerPause bool
}

func (pCtx *pauseContext) AddControllerPause() {
	pCtx.addControllerPause = true
}

func (pCtx *pauseContext) RemoveControllerPause() {
	pCtx.removeControllerPause = true
}

func completedPauseStep(rollout *v1alpha1.Rollout, pause v1alpha1.RolloutPause) bool {
	logCtx := logutil.WithRollout(rollout)
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

// calculatePauseStatus determines if the rollout should be paused by the controller.
func calculatePauseStatus(roCtx rolloutContext) (*metav1.Time, bool) {
	rollout := roCtx.Rollout()
	logCtx := roCtx.Log()
	pauseCtx := roCtx.PauseContext()
	pauseStartTime := rollout.Status.PauseStartTime
	paused := rollout.Status.ControllerPause
	if !paused {
		pauseStartTime = nil
	}
	if pauseCtx.addControllerPause {
		if pauseStartTime == nil {
			now := metav1.Now()
			logCtx.Infof("Setting PauseStartTime to %s", now.UTC().Format(time.RFC3339))
			pauseStartTime = &now
			paused = true
		}
	}
	if pauseCtx.removeControllerPause {
		return nil, false
	}

	return pauseStartTime, paused
}
