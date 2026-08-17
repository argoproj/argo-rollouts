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
	// stageContinueWithError: a cosmetic stage failed; record the condition, keep running the
	// remaining stages (nothing downstream depends on it for traffic safety), and surface the
	// error after the status sync for workqueue backoff. Step progression still holds via the condition gates.
	stageContinueWithError
	// stageStop: halt the pipeline and fall through to status sync. When err is set, the failure
	// is recorded, returned for workqueue backoff, and status still syncs (#4626).
	stageStop
	// stageStopNoStatus: halt without status sync. Used for the pod-restart early exit (no err)
	// and ReplicaSet-sync failures (err set), where c.newRS is unreliable and a status computed
	// from it would persist corrupted values.
	stageStopNoStatus
)

type stageResult struct {
	outcome stageOutcome
	err     error
	reason  string
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

// recordStageFailure sets ReconcileSucceeded=False and emits a warning event. The event is emitted
// only when the condition transitions (new failure or changed reason), not on every retry.
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
		c.log.Infof("Finished reconciliation due to %d restarted pods", restarted)
		return stageResult{outcome: stageStopNoStatus}
	}
	return stageResult{outcome: stageContinue}
}

func canaryStageEphemeralMetadata(c *rolloutContext) stageResult {
	if err := c.reconcileEphemeralMetadata(); err != nil {
		// Cosmetic: pod metadata labeling must not block services/traffic/scaling this pass.
		return stageResult{outcome: stageContinueWithError, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func canaryStageRevisionHistory(c *rolloutContext) stageResult {
	if err := c.reconcileRevisionHistoryLimit(c.otherRSs); err != nil {
		// Cosmetic: old-revision cleanup must not block services/traffic/scaling this pass.
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
	return stageResult{outcome: stageContinue}
}

func canaryStageAnalysis(c *rolloutContext) stageResult {
	err := c.reconcileAnalysisRuns()
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
	noScalingOccurred, err := c.reconcileCanaryReplicaSets()
	if err != nil {
		return stageResult{outcome: stageStop, err: err}
	}
	if noScalingOccurred {
		c.log.Info("Not finished reconciling ReplicaSets")
		return stageResult{outcome: stageStop}
	}
	return stageResult{outcome: stageContinue}
}

func canaryStageCanaryPause(c *rolloutContext) stageResult {
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
	return stageResult{outcome: stageContinue}
}

func blueGreenStageEphemeralMetadata(c *rolloutContext) stageResult {
	if err := c.reconcileEphemeralMetadata(); err != nil {
		// Cosmetic: pod metadata labeling must not block the service switch or scaling this pass.
		return stageResult{outcome: stageContinueWithError, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func blueGreenStageRevisionHistory(c *rolloutContext) stageResult {
	if err := c.reconcileRevisionHistoryLimit(c.otherRSs); err != nil {
		// Cosmetic: old-revision cleanup must not block the service switch or scaling this pass.
		return stageResult{outcome: stageContinueWithError, err: err}
	}
	return stageResult{outcome: stageContinue}
}
