package rolloutplugin

import (
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

type pauseContext struct {
	rolloutPlugin *v1alpha1.RolloutPlugin
	log           *log.Entry

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
	if pCtx.addAbort || pCtx.rolloutPlugin.Status.Abort {
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

func (pCtx *pauseContext) CalculatePauseStatus(newStatus *v1alpha1.RolloutPluginStatus) {
	if pCtx.CalculateAbortStatus(newStatus) {
		return
	}
	if pCtx.clearPauseConditions {
		return
	}
	pCtx.CalculatePauseConditions(newStatus)
}

func (pCtx *pauseContext) CalculateAbortStatus(newStatus *v1alpha1.RolloutPluginStatus) bool {
	now := timeutil.MetaNow()
	// Preserve the original AbortedAt timestamp to avoid reconciliation hot-loop
	newAbortedAt := pCtx.rolloutPlugin.Status.AbortedAt
	if newAbortedAt == nil {
		newAbortedAt = &now
	}
	if pCtx.addAbort || (!pCtx.removeAbort && (pCtx.rolloutPlugin.Status.Abort || pCtx.rolloutPlugin.Status.Aborted)) {
		newStatus.Aborted = true
		newStatus.AbortedAt = newAbortedAt
		newStatus.AbortedRevision = newStatus.UpdatedRevision
		newStatus.Abort = false
		// Clear pause state so CalculateRolloutPluginPhase returns Degraded (not Paused).
		newStatus.PauseConditions = nil
		newStatus.ControllerPause = false
		return true
	}

	newStatus.Abort = false
	newStatus.AbortedAt = nil

	return false
}

func (pCtx *pauseContext) CalculatePauseConditions(newStatus *v1alpha1.RolloutPluginStatus) {
	now := timeutil.MetaNow()
	controllerPause := pCtx.rolloutPlugin.Status.ControllerPause
	statusToRemove := map[v1alpha1.PauseReason]bool{}
	for i := range pCtx.removePauseReasons {
		statusToRemove[pCtx.removePauseReasons[i]] = true
	}

	pauseAlreadyExists := map[v1alpha1.PauseReason]bool{}
	newPauseConditions := []v1alpha1.PauseCondition{}
	for _, cond := range pCtx.rolloutPlugin.Status.PauseConditions {
		if remove := statusToRemove[cond.Reason]; !remove {
			newPauseConditions = append(newPauseConditions, cond)
		}
		pauseAlreadyExists[cond.Reason] = true
	}

	for _, reason := range pCtx.addPauseReasons {
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
		newStatus.PauseConditions = nil
		newStatus.ControllerPause = false
		return
	}
	newStatus.ControllerPause = controllerPause
	newStatus.PauseConditions = newPauseConditions
}

// getRolloutPluginPauseCondition returns the pause condition with the specified reason, or nil if not found.
func getRolloutPluginPauseCondition(rolloutPlugin *v1alpha1.RolloutPlugin, reason v1alpha1.PauseReason) *v1alpha1.PauseCondition {
	for _, cond := range rolloutPlugin.Status.PauseConditions {
		if cond.Reason == reason {
			return &cond
		}
	}
	return nil
}

// CompletedCanaryPauseStep checks if the canary pause step has completed, either by duration
// elapsed or user promotion.
func (pCtx *pauseContext) CompletedCanaryPauseStep(pause v1alpha1.RolloutPause) bool {
	rp := pCtx.rolloutPlugin
	pauseCondition := getRolloutPluginPauseCondition(rp, v1alpha1.PauseReasonCanaryPauseStep)

	if rp.Status.ControllerPause && pauseCondition == nil {
		pCtx.log.Info("Rollout has been unpaused manually")
		return true
	} else if pause.Duration != nil && pauseCondition != nil {
		expiredTime := pauseCondition.StartTime.Add(time.Duration(pause.DurationSeconds()) * time.Second)
		if timeutil.MetaNow().After(expiredTime) {
			pCtx.log.Info("Rollout has waited the duration of the pause step")
			return true
		}
	}
	return false
}
