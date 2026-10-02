package rollout

import (
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/record"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

type stageOutcome int

const (
	// stageContinue: proceed to the next stage.
	stageContinue stageOutcome = iota
	// stageContinueWithError: a cosmetic stage failed. Record ReconcileSucceeded=False, keep
	// running the remaining stages (nothing downstream depends on it for traffic safety), and
	// surface the error after the status sync for workqueue backoff. Unlike a stageStop failure,
	// it does not block step advancement or full promotion.
	stageContinueWithError
	// stageStop: halt the pipeline and fall through to status sync. When err is set, the failure
	// is recorded (ReconcileSucceeded=False), blocks step advancement and full promotion for this
	// pass, is returned for workqueue backoff, and status still syncs (#4626).
	stageStop
	// stageStopNoStatus: halt without status sync. Used for the pod-restart early exit (no err)
	// and ReplicaSet-sync failures (err set), where c.newRS is unreliable and a status computed
	// from it would persist corrupted values.
	stageStopNoStatus
)

type stageResult struct {
	outcome stageOutcome
	err     error
	// reason is the ReconcileSucceeded=False reason (and event reason) recorded for a failure.
	// Defaults to ReconciliationError.
	reason string
}

type strategyStage struct {
	name string
	run  func(c *rolloutContext) stageResult
}

var canaryStages = []strategyStage{
	{"syncRevisionOnChange", canaryStageSyncRevisionOnChange},
	{"syncReplicaSets", canaryStageSyncReplicaSets},
	{"podRestart", canaryStagePodRestart},
	{"ephemeralMetadata", canaryStageEphemeralMetadata},
	{"revisionHistory", canaryStageRevisionHistory},
	{"pingPongService", canaryStagePingPongService},
	{"stableCanaryService", canaryStageStableCanaryService},
	{"trafficRouting", canaryStageTrafficRouting},
	{"experiments", canaryStageExperiments},
	{"analysis", canaryStageAnalysis},
	{"replicaSetScaling", canaryStageReplicaSetScaling},
	{"canaryPause", canaryStageCanaryPause},
	{"stepPlugins", canaryStageStepPlugins},
}

var blueGreenStages = []strategyStage{
	{"previewService", blueGreenStagePreviewService},
	{"podTemplateChange", blueGreenStagePodTemplateChange},
	{"podRestart", blueGreenStagePodRestart},
	{"replicaSets", blueGreenStageReplicaSets},
	{"pause", blueGreenStagePause},
	{"activeService", blueGreenStageActiveService},
	{"targetGroups", blueGreenStageTargetGroups},
	{"analysis", blueGreenStageAnalysis},
	{"ephemeralMetadata", blueGreenStageEphemeralMetadata},
	{"revisionHistory", blueGreenStageRevisionHistory},
}

func (c *rolloutContext) runCanaryStages() error {
	return c.runStages(canaryStages)
}

func (c *rolloutContext) runBlueGreenStages(previewSvc, activeSvc *corev1.Service) error {
	c.blueGreenPreviewSvc = previewSvc
	c.blueGreenActiveSvc = activeSvc
	defer func() {
		c.blueGreenPreviewSvc = nil
		c.blueGreenActiveSvc = nil
	}()
	return c.runStages(blueGreenStages)
}

func (c *rolloutContext) runStages(stages []strategyStage) error {
	// errs collects stageContinueWithError failures so they survive a later stop.
	var errs []error
	for _, s := range stages {
		res := s.run(c)
		switch res.outcome {
		case stageContinue:
		case stageContinueWithError:
			c.recordStageFailure(res)
			errs = append(errs, res.err)
		case stageStop:
			if res.err != nil {
				c.recordStageFailure(res)
				// The cluster may not match the state that step advancement or full promotion
				// would persist; hold both for the rest of this pass.
				c.progressionBlocked = true
				errs = append(errs, res.err)
			} else {
				c.log.Infof("stage %s: stopping further changes, proceeding to status sync", s.name)
			}
			return errors.Join(errs...)
		case stageStopNoStatus:
			c.skipStatusSync = true
			if res.err != nil {
				errs = append(errs, res.err)
			}
			return errors.Join(errs...)
		}
	}
	if len(errs) == 0 {
		// Reconcile work completed without error; ReconcileSucceeded may recover to True.
		c.markStageSucceeded()
	}
	return errors.Join(errs...)
}

// recordStageFailure records ReconcileSucceeded=False and emits a warning event. The event is
// emitted only when the condition transitions (new failure or changed reason), not on every
// retry.
func (c *rolloutContext) recordStageFailure(res stageResult) {
	reason := res.reason
	if reason == "" {
		reason = conditions.RolloutReconciliationErrorReason
	}
	c.setStageCondition(v1alpha1.RolloutReconcileSucceeded, corev1.ConditionFalse, reason, res.err.Error())
	prevCond := conditions.GetRolloutCondition(c.rollout.Status, v1alpha1.RolloutReconcileSucceeded)
	if prevCond == nil || prevCond.Status != corev1.ConditionFalse || prevCond.Reason != reason {
		c.recorder.Warnf(c.rollout, record.EventOptions{EventReason: reason}, "%s", res.err.Error())
	}
}

// carryOverUnreconciledStatus keeps the previous values of status fields owned by stages that did
// not complete this pass. The current Experiment and AnalysisRuns are only written into
// c.newStatus by the experiments and analysis stages, and the controller identifies the current
// runs by the names recorded there. Syncing them as empty after an earlier stage failed would make
// the next pass treat the current runs as old ones: a completed AnalysisRun would be re-created
// under a new name, and a failed one would be replaced before it could abort the rollout. Before
// the pipeline, a stage error skipped the status sync entirely, which left these fields untouched.
func (c *rolloutContext) carryOverUnreconciledStatus() {
	prev := c.rollout.Status
	if c.rollout.Spec.Strategy.Canary != nil {
		if !c.experimentsReconciled {
			c.newStatus.Canary.CurrentExperiment = prev.Canary.CurrentExperiment
		}
		if !c.analysisReconciled {
			c.newStatus.Canary.CurrentStepAnalysisRunStatus = prev.Canary.CurrentStepAnalysisRunStatus
			c.newStatus.Canary.CurrentBackgroundAnalysisRunStatus = prev.Canary.CurrentBackgroundAnalysisRunStatus
		}
	} else if c.rollout.Spec.Strategy.BlueGreen != nil && !c.analysisReconciled {
		c.newStatus.BlueGreen.PrePromotionAnalysisRunStatus = prev.BlueGreen.PrePromotionAnalysisRunStatus
		c.newStatus.BlueGreen.PostPromotionAnalysisRunStatus = prev.BlueGreen.PostPromotionAnalysisRunStatus
	}
}

func canaryStageSyncRevisionOnChange(c *rolloutContext) stageResult {
	if !replicasetutil.PodTemplateOrStepsChanged(c.rollout, c.newRS) {
		return stageResult{outcome: stageContinue}
	}
	newRS, err := c.getAllReplicaSetsAndSyncRevision()
	if err != nil {
		// Leave c.newRS untouched and skip the status sync: syncing now would evaluate
		// PodTemplateOrStepsChanged against a nil newRS and persist a reset status (cleared
		// abort/pause state, phantom CurrentPodHash) for a ReplicaSet that was never synced.
		return stageResult{
			outcome: stageStopNoStatus,
			err:     fmt.Errorf("failed to getAllReplicaSetsAndSyncRevision in rolloutCanary with PodTemplateOrStepsChanged: %w", err),
		}
	}
	c.newRS = newRS
	return stageResult{outcome: stageStop}
}

func canaryStageSyncReplicaSets(c *rolloutContext) stageResult {
	newRS, err := c.getAllReplicaSetsAndSyncRevision()
	if err != nil {
		// Leave c.newRS untouched and skip the status sync: syncing now would compute replica
		// counts from a nil newRS (e.g. UpdatedReplicas flapping to 0 on a transient API error).
		return stageResult{
			outcome: stageStopNoStatus,
			err:     fmt.Errorf("failed to getAllReplicaSetsAndSyncRevision in rolloutCanary create true: %w", err),
		}
	}
	c.newRS = newRS
	return stageResult{outcome: stageContinue}
}

func canaryStagePodRestart(c *rolloutContext) stageResult {
	restarted, err := c.podRestarter.Reconcile(c)
	if err != nil {
		return stageResult{outcome: stageStop, err: err}
	}
	if restarted > 0 {
		// If we restarted any pods, we can no longer trust the current availability counts of our
		// ReplicaSets, since those counts do not factor in the unavailability of pods we just
		// restarted. We would cause downtime if we continue the reconciliation and *also* scale
		// down a ReplicaSet (e.g. because of a canary update scaling). Therefore, we return early,
		// so that the *next* reconciliation will have an accurate availability count to calculate
		// the safe number of pods to scale down for the update.
		c.log.Infof("Finished reconciliation due to %d restarted pods", restarted)
		return stageResult{outcome: stageStopNoStatus}
	}
	return stageResult{outcome: stageContinue}
}

func canaryStageEphemeralMetadata(c *rolloutContext) stageResult {
	if err := c.reconcileEphemeralMetadata(); err != nil {
		// Cosmetic: pod metadata labeling must not block services/traffic/scaling or progression.
		return stageResult{outcome: stageContinueWithError, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func canaryStageRevisionHistory(c *rolloutContext) stageResult {
	if err := c.reconcileRevisionHistoryLimit(c.otherRSs); err != nil {
		// Cosmetic: old-revision cleanup must not block services/traffic/scaling or progression.
		return stageResult{outcome: stageContinueWithError, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func canaryStagePingPongService(c *rolloutContext) stageResult {
	if err := c.reconcilePingAndPongService(); err != nil {
		return stageResult{
			outcome: stageStop,
			err:     err,
			reason:  conditions.ServiceUpdateErrorReason,
		}
	}
	return stageResult{outcome: stageContinue}
}

func canaryStageStableCanaryService(c *rolloutContext) stageResult {
	if err := c.reconcileStableAndCanaryService(); err != nil {
		return stageResult{
			outcome: stageStop,
			err:     err,
			reason:  conditions.ServiceUpdateErrorReason,
		}
	}
	return stageResult{outcome: stageContinue}
}

func canaryStageTrafficRouting(c *rolloutContext) stageResult {
	if err := c.reconcileTrafficRouting(); err != nil {
		return stageResult{
			outcome: stageStop,
			err:     err,
			reason:  conditions.TrafficRoutingErrorReason,
		}
	}
	return stageResult{outcome: stageContinue}
}

func canaryStageExperiments(c *rolloutContext) stageResult {
	if err := c.reconcileExperiments(); err != nil {
		return stageResult{outcome: stageStop, err: err}
	}
	c.experimentsReconciled = true
	return stageResult{outcome: stageContinue}
}

func canaryStageAnalysis(c *rolloutContext) stageResult {
	err := c.reconcileAnalysisRuns()
	if err == nil {
		c.analysisReconciled = true
	}
	if c.pauseContext.HasAddPause() {
		c.log.Info("Detected pause due to inconclusive AnalysisRun")
		return stageResult{outcome: stageStop}
	}
	if err != nil {
		return stageResult{outcome: stageStop, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func canaryStageReplicaSetScaling(c *rolloutContext) stageResult {
	// isReconciling if changes are made to canary or stable RS
	isReconciling, err := c.reconcileCanaryReplicaSets()
	if err != nil {
		return stageResult{outcome: stageStop, err: err}
	}
	if isReconciling {
		c.log.Info("Not finished reconciling ReplicaSets")
		return stageResult{outcome: stageStop}
	}
	return stageResult{outcome: stageContinue}
}

func canaryStageCanaryPause(c *rolloutContext) stageResult {
	// isReconciling if a new paused condition is added
	if c.reconcileCanaryPause() {
		c.log.Infof("Not finished reconciling Canary Pause")
		return stageResult{outcome: stageStop}
	}
	return stageResult{outcome: stageContinue}
}

func canaryStageStepPlugins(c *rolloutContext) stageResult {
	if err := c.stepPluginContext.reconcile(c); err != nil {
		return stageResult{outcome: stageStop, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func blueGreenStagePreviewService(c *rolloutContext) stageResult {
	// This must happen right after the new replicaset is created.
	if err := c.reconcilePreviewService(c.blueGreenPreviewSvc); err != nil {
		return stageResult{
			outcome: stageStop,
			err:     err,
			reason:  conditions.ServiceUpdateErrorReason,
		}
	}
	return stageResult{outcome: stageContinue}
}

func blueGreenStagePodTemplateChange(c *rolloutContext) stageResult {
	if replicasetutil.CheckPodSpecChange(c.rollout, c.newRS) {
		// A pod template change is handled entirely by the status sync.
		return stageResult{outcome: stageStop}
	}
	return stageResult{outcome: stageContinue}
}

func blueGreenStagePodRestart(c *rolloutContext) stageResult {
	if _, err := c.podRestarter.Reconcile(c); err != nil {
		return stageResult{outcome: stageStop, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func blueGreenStageReplicaSets(c *rolloutContext) stageResult {
	if err := c.reconcileBlueGreenReplicaSets(c.blueGreenActiveSvc); err != nil {
		return stageResult{outcome: stageStop, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func blueGreenStagePause(c *rolloutContext) stageResult {
	c.reconcileBlueGreenPause(c.blueGreenActiveSvc, c.blueGreenPreviewSvc)
	return stageResult{outcome: stageContinue}
}

func blueGreenStageActiveService(c *rolloutContext) stageResult {
	if err := c.reconcileActiveService(c.blueGreenActiveSvc); err != nil {
		return stageResult{
			outcome: stageStop,
			err:     err,
			reason:  conditions.ServiceUpdateErrorReason,
		}
	}
	return stageResult{outcome: stageContinue}
}

func blueGreenStageTargetGroups(c *rolloutContext) stageResult {
	if err := c.awsVerifyTargetGroups(c.blueGreenActiveSvc); err != nil {
		return stageResult{outcome: stageStop, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func blueGreenStageAnalysis(c *rolloutContext) stageResult {
	if err := c.reconcileAnalysisRuns(); err != nil {
		return stageResult{outcome: stageStop, err: err}
	}
	c.analysisReconciled = true
	return stageResult{outcome: stageContinue}
}

func blueGreenStageEphemeralMetadata(c *rolloutContext) stageResult {
	if err := c.reconcileEphemeralMetadata(); err != nil {
		// Cosmetic: pod metadata labeling must not block the service switch, scaling, or promotion.
		return stageResult{outcome: stageContinueWithError, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func blueGreenStageRevisionHistory(c *rolloutContext) stageResult {
	if err := c.reconcileRevisionHistoryLimit(c.otherRSs); err != nil {
		// Cosmetic: old-revision cleanup must not block the service switch, scaling, or promotion.
		return stageResult{outcome: stageContinueWithError, err: err}
	}
	return stageResult{outcome: stageContinue}
}
