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
	// stageContinueWithError: a cosmetic stage failed; record the condition, keep actuating
	// (nothing downstream depends on it for traffic safety), and surface the error after the
	// status sync for workqueue backoff. Step progression still holds via the condition gates.
	stageContinueWithError
	// stageStop: stop actuating, fall through to status sync. NOT an error.
	stageStop
	// stageStopNoStatus: stop without status sync (pod-restart early exit only).
	stageStopNoStatus
	// stageFatal: stop actuating, still status-sync, and return the error for workqueue backoff.
	stageFatal
	// stageFatalNoStatus: stop actuating, skip status sync, and return the error for workqueue
	// backoff. Reserved for ReplicaSet-sync failures that leave c.newRS unreliable: a status
	// computed from a nil/stale newRS would persist corrupted values (e.g. clear abort state and
	// record a CurrentPodHash for a ReplicaSet that was never created).
	stageFatalNoStatus
)

type stageResult struct {
	outcome   stageOutcome
	err       error
	condition v1alpha1.RolloutConditionType
	reason    string
}

type canaryStage struct {
	name string
	run  func(c *rolloutContext) stageResult
}

var canaryStages = []canaryStage{
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

func (c *rolloutContext) runCanaryStages() error {
	var errs []error
	for _, s := range canaryStages {
		res := s.run(c)
		switch res.outcome {
		case stageContinue:
		case stageContinueWithError:
			c.recordStageFailure(res)
			errs = append(errs, res.err)
		case stageStop:
			c.log.Infof("stage %s: stopping actuation, proceeding to status sync", s.name)
			return errors.Join(errs...)
		case stageStopNoStatus:
			c.skipStatusSync = true
			return errors.Join(errs...)
		case stageFatalNoStatus:
			c.skipStatusSync = true
			errs = append(errs, res.err)
			return errors.Join(errs...)
		case stageFatal:
			c.recordStageFailure(res)
			errs = append(errs, res.err)
			return errors.Join(errs...)
		}
	}
	if len(errs) == 0 {
		// Every stage ran without failure, so the catch-all actuation condition is allowed to
		// recover to True in mergeStageConditions.
		c.markStageSucceeded(v1alpha1.RolloutActuationSucceeded)
	}
	return errors.Join(errs...)
}

// recordStageFailure records the failing stage's condition and emits a warning event. A
// persistently failing stage retries on workqueue backoff; the event is emitted only when the
// condition actually transitions (new failure or changed reason), not on every retry pass.
func (c *rolloutContext) recordStageFailure(res stageResult) {
	condType := res.condition
	if condType == "" {
		condType = v1alpha1.RolloutActuationSucceeded
	}
	reason := res.reason
	if reason == "" {
		reason = conditions.ActuationErrorReason
	}
	c.setStageCondition(condType, corev1.ConditionFalse, reason, res.err.Error())
	prevCond := conditions.GetRolloutCondition(c.rollout.Status, condType)
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
			outcome: stageFatalNoStatus,
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
			outcome: stageFatalNoStatus,
			err:     fmt.Errorf("failed to getAllReplicaSetsAndSyncRevision in rolloutCanary create true: %w", err),
		}
	}
	c.newRS = newRS
	return stageResult{outcome: stageContinue}
}

func canaryStagePodRestart(c *rolloutContext) stageResult {
	restarted, err := c.podRestarter.Reconcile(c)
	if err != nil {
		return stageResult{outcome: stageFatal, err: err}
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
			outcome:   stageFatal,
			err:       err,
			condition: v1alpha1.RolloutServicesReconciled,
			reason:    conditions.ServiceUpdateErrorReason,
		}
	}
	return stageResult{outcome: stageContinue}
}

func canaryStageStableCanaryService(c *rolloutContext) stageResult {
	if err := c.reconcileStableAndCanaryService(); err != nil {
		return stageResult{
			outcome:   stageFatal,
			err:       err,
			condition: v1alpha1.RolloutServicesReconciled,
			reason:    conditions.ServiceUpdateErrorReason,
		}
	}
	// pingPongService runs before this stage and any failure there aborts the pipeline, so both
	// owners of the ServicesReconciled condition succeeded this pass.
	c.markStageSucceeded(v1alpha1.RolloutServicesReconciled)
	return stageResult{outcome: stageContinue}
}

func canaryStageTrafficRouting(c *rolloutContext) stageResult {
	if err := c.reconcileTrafficRouting(); err != nil {
		return stageResult{
			outcome:   stageFatal,
			err:       err,
			condition: v1alpha1.RolloutTrafficRoutingApplied,
			reason:    conditions.TrafficRoutingErrorReason,
		}
	}
	c.markStageSucceeded(v1alpha1.RolloutTrafficRoutingApplied)
	return stageResult{outcome: stageContinue}
}

func canaryStageExperiments(c *rolloutContext) stageResult {
	if err := c.reconcileExperiments(); err != nil {
		return stageResult{outcome: stageFatal, err: err}
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
		return stageResult{outcome: stageFatal, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func canaryStageReplicaSetScaling(c *rolloutContext) stageResult {
	noScalingOccurred, err := c.reconcileCanaryReplicaSets()
	if err != nil {
		return stageResult{outcome: stageFatal, err: err}
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
		return stageResult{outcome: stageFatal, err: err}
	}
	return stageResult{outcome: stageContinue}
}
