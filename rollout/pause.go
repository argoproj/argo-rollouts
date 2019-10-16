package rollout

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

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
	} else if rollout.Status.PauseStartTime != nil && !rollout.Spec.Paused {
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

// calculatePauseStatus finds the fields related to a pause step for a rollout. If the pause is nil,
// the rollout will use the previous values
func calculatePauseStatus(roCtx rolloutContext, addPause bool, currArs []*v1alpha1.AnalysisRun) (*metav1.Time, bool) {
	rollout := roCtx.Rollout()
	logCtx := roCtx.Log()
	pauseStartTime := rollout.Status.PauseStartTime
	paused := rollout.Spec.Paused
	if !paused {
		pauseStartTime = nil
	}
	if rollout.Spec.Strategy.BlueGreenStrategy != nil && defaults.GetAutoPromotionEnabledOrDefault(rollout) {
		return nil, false
	}

	pauseForInconclusiveAnalysisRun := false
	for i := range currArs {
		ar := currArs[i]
		if ar != nil && ar.Status != nil && ar.Status.Status == v1alpha1.AnalysisStatusInconclusive {
			pauseForInconclusiveAnalysisRun = true
		}
	}

	if addPause || pauseForInconclusiveAnalysisRun {
		if pauseStartTime == nil {
			now := metav1.Now()
			logCtx.Infof("Setting PauseStartTime to %s", now.UTC().Format(time.RFC3339))
			pauseStartTime = &now
			paused = true
		}
	}

	if rollout.Spec.Strategy.BlueGreenStrategy != nil {
		bgCtx := roCtx.(*blueGreenContext)
		if reconcileBlueGreenTemplateChange(bgCtx) {
			return nil, false
		}
		if paused && pauseStartTime != nil && rollout.Spec.Strategy.BlueGreenStrategy.AutoPromotionSeconds != nil {
			now := metav1.Now()
			autoPromoteActiveServiceDelaySeconds := *rollout.Spec.Strategy.BlueGreenStrategy.AutoPromotionSeconds
			switchDeadline := pauseStartTime.Add(time.Duration(autoPromoteActiveServiceDelaySeconds) * time.Second)
			if now.After(switchDeadline) {
				return nil, false
			}
			return pauseStartTime, true
		}
	}
	return pauseStartTime, paused
}
