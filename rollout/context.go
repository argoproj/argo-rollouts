package rollout

import (
	"context"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
)

type rolloutContext struct {
	reconcilerBase

	log *log.Entry
	// rollout is the rollout being reconciled
	rollout *v1alpha1.Rollout
	// newRollout is the rollout after reconciliation. used to write back to informer
	newRollout *v1alpha1.Rollout
	// newRS is the "new" ReplicaSet. Also referred to as current, or desired.
	// newRS will be nil when the pod template spec changes.
	newRS *appsv1.ReplicaSet
	// stableRS is the "stable" ReplicaSet which will be scaled up upon an abort.
	// stableRS will be nil when a Rollout is first deployed, and will be equal to newRS when fully promoted
	stableRS *appsv1.ReplicaSet
	// allRSs are all the ReplicaSets associated with the Rollout
	allRSs []*appsv1.ReplicaSet
	// olderRSs are "older" ReplicaSets -- anything which is not the newRS
	// this includes the stableRS (when in the middle of an update)
	olderRSs []*appsv1.ReplicaSet
	// otherRSs are ReplicaSets which are neither new or stable (allRSs - newRS - stableRS)
	otherRSs []*appsv1.ReplicaSet

	currentArs analysisutil.CurrentAnalysisRuns
	otherArs   []*v1alpha1.AnalysisRun

	currentEx *v1alpha1.Experiment
	otherExs  []*v1alpha1.Experiment

	newStatus         v1alpha1.RolloutStatus
	pauseContext      *pauseContext
	stepPluginContext *stepPluginContext

	// pendingDurationMetric holds a completed rollout duration stashed by
	// calculateStatusDuration. It is emitted by emitPendingRolloutDuration only
	// after the status change recording the completion transition has been
	// persisted, so that a failed status patch does not double-count the
	// completion when the transition is re-detected on the next reconcile.
	pendingDurationMetric *v1alpha1.RolloutDurationStatus

	// targetsVerified indicates if the pods targets have been verified with underlying LoadBalancer.
	// This is used in pod-aware flat networks where LoadBalancers target Pods and not Nodes.
	// nil indicates the check was unnecessary or not performed.
	// NOTE: we only perform target verification when we are at specific points in time
	// (e.g. a setWeight step, after a blue-green active switch, after stable service switch),
	// since we do not want to continually verify weight in case it could incur rate-limiting or other expenses.
	targetsVerified *bool

	// newRSWithinDelay indicates if the newRS has a valid (non-expired) scale-down-deadline
	// annotation at the start of reconciliation (before it may be removed).
	// Used to detect fast rollbacks where we skip pause/analysis steps.
	newRSWithinDelay bool

	// skipStatusSync ends the pass without a status sync. Set by stageStopNoStatus: the pod-restart
	// early exit and stage errors (see runStages).
	skipStatusSync bool

	// blueGreenPreviewSvc and blueGreenActiveSvc are set for the duration of runBlueGreenStages.
	blueGreenPreviewSvc *corev1.Service
	blueGreenActiveSvc  *corev1.Service
}

func (c *rolloutContext) reconcile() error {
	err := c.checkPausedConditions()
	if err != nil {
		return err
	}
	if c.newRollout != nil {
		// exit early since we modified the rollout
		return nil
	}
	if reset, err := c.resetAnalysisForRetry(); reset || err != nil {
		return err
	}

	isScalingEvent, err := c.isScalingEvent()
	if err != nil {
		return err
	}

	if isScalingEvent {
		return c.syncReplicasOnly()
	}

	if c.rollout.Spec.Strategy.BlueGreen != nil {
		return c.rolloutBlueGreen()
	}

	// Due to the rollout validation before this, when we get here strategy is canary
	return c.rolloutCanary()
}

// resetAnalysisForRetry clears the aborted attempt's analysis references and,
// when pre-promotion analysis is configured, its Blue/Green preview checkpoint.
// The reset must be persisted before status updates clear AbortedAt;
// otherwise a later reconciliation can still treat an old run as current.
// Persisting the full status with the observed resourceVersion lets Kubernetes reject
// stale retry cleanup rather than overwrite a newer attempt.
func (c *rolloutContext) resetAnalysisForRetry() (bool, error) {
	if c.rollout.Status.Abort || c.rollout.Status.AbortedAt == nil {
		return false, nil
	}
	next := c.rollout.DeepCopy()
	var previous []*v1alpha1.AnalysisRun
	if bg := next.Spec.Strategy.BlueGreen; bg != nil {
		status := &next.Status.BlueGreen
		hasPreAnalysis := bg.PrePromotionAnalysis != nil && len(bg.PrePromotionAnalysis.Templates) > 0
		resetPreview := hasPreAnalysis && status.ScaleUpPreviewCheckPoint
		if status.PrePromotionAnalysisRunStatus == nil && status.PostPromotionAnalysisRunStatus == nil && !resetPreview {
			return false, nil
		}
		previous = []*v1alpha1.AnalysisRun{c.currentArs.BlueGreenPrePromotion, c.currentArs.BlueGreenPostPromotion}
		status.PrePromotionAnalysisRunStatus = nil
		status.PostPromotionAnalysisRunStatus = nil
		if hasPreAnalysis {
			status.ScaleUpPreviewCheckPoint = false
		}
	} else if next.Spec.Strategy.Canary != nil {
		status := &next.Status.Canary
		if status.CurrentStepAnalysisRunStatus == nil && status.CurrentBackgroundAnalysisRunStatus == nil {
			return false, nil
		}
		previous = []*v1alpha1.AnalysisRun{c.currentArs.CanaryStep, c.currentArs.CanaryBackground}
		status.CurrentStepAnalysisRunStatus = nil
		status.CurrentBackgroundAnalysisRunStatus = nil
	}
	if err := c.cancelAnalysisRuns(previous); err != nil {
		return false, err
	}
	updated, err := c.argoprojclientset.ArgoprojV1alpha1().Rollouts(next.Namespace).UpdateStatus(context.TODO(), next, metav1.UpdateOptions{})
	if err != nil {
		return false, err
	}
	c.newRollout = updated
	return true, nil
}

func (c *rolloutContext) SetRestartedAt() {
	c.newStatus.RestartedAt = c.rollout.Spec.RestartAt
}

func (c *rolloutContext) SetCurrentExperiment(ex *v1alpha1.Experiment) {
	c.currentEx = ex
	c.newStatus.Canary.CurrentExperiment = ex.Name
	for i, otherEx := range c.otherExs {
		if otherEx.Name == ex.Name {
			c.log.Infof("Rescued %s from inadvertent termination", ex.Name)
			c.otherExs = append(c.otherExs[:i], c.otherExs[i+1:]...)
			break
		}
	}
}

func (c *rolloutContext) SetCurrentAnalysisRuns(currARs analysisutil.CurrentAnalysisRuns) {
	c.currentArs = currARs

	if c.rollout.Spec.Strategy.Canary != nil {
		currBackgroundAr := currARs.CanaryBackground
		if currBackgroundAr != nil {
			c.newStatus.Canary.CurrentBackgroundAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
				Name:    currBackgroundAr.Name,
				Status:  currBackgroundAr.Status.Phase,
				Message: currBackgroundAr.Status.Message,
			}
		}
		currStepAr := currARs.CanaryStep
		if currStepAr != nil {
			c.newStatus.Canary.CurrentStepAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
				Name:    currStepAr.Name,
				Status:  currStepAr.Status.Phase,
				Message: currStepAr.Status.Message,
			}
		}
	} else if c.rollout.Spec.Strategy.BlueGreen != nil {
		currPrePromoAr := currARs.BlueGreenPrePromotion
		if currPrePromoAr != nil {
			c.newStatus.BlueGreen.PrePromotionAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
				Name:    currPrePromoAr.Name,
				Status:  currPrePromoAr.Status.Phase,
				Message: currPrePromoAr.Status.Message,
			}
		}
		currPostPromoAr := currARs.BlueGreenPostPromotion
		if currPostPromoAr != nil {
			c.newStatus.BlueGreen.PostPromotionAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
				Name:    currPostPromoAr.Name,
				Status:  currPostPromoAr.Status.Phase,
				Message: currPostPromoAr.Status.Message,
			}
		}
	}
}

// haltProgress returns a reason on whether or not we should halt all progress with an update
// to ReplicaSet counts (e.g. due to canary steps or blue-green promotion). This is either because
// user explicitly paused the rollout by setting `spec.paused`, or the analysis was inconclusive
func (c *rolloutContext) haltProgress() string {
	if c.rollout.Spec.Paused {
		return "user paused"
	}
	if getPauseCondition(c.rollout, v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		return "inconclusive analysis"
	}
	return ""
}
