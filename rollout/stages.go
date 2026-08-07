package rollout

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/record"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

type stageOutcome int

const (
	// stageContinue: proceed to the next stage.
	stageContinue stageOutcome = iota
	// stageStop: stop actuating, fall through to status sync. NOT an error.
	stageStop
	// stageStopNoStatus: stop without status sync (pod-restart early exit only).
	stageStopNoStatus
	// stageHold: desired state not applied; record condition, hold progression,
	// continue to status sync. Not returned as a reconcile error.
	stageHold
	// stageFatal: stop actuating, still status-sync, and return the error for workqueue backoff.
	stageFatal
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
	for _, s := range canaryStages {
		res := s.run(c)
		switch res.outcome {
		case stageContinue:
		case stageStop:
			c.log.Infof("stage %s: stopping actuation, proceeding to status sync", s.name)
			return nil
		case stageStopNoStatus:
			c.skipStatusSync = true
			return nil
		case stageHold, stageFatal:
			condType := res.condition
			if condType == "" {
				condType = v1alpha1.RolloutActuationSucceeded
			}
			reason := res.reason
			if reason == "" {
				reason = conditions.ActuationErrorReason
			}
			c.setStageCondition(condType, corev1.ConditionFalse, reason, res.err.Error())
			c.recorder.Warnf(c.rollout, record.EventOptions{EventReason: reason}, "%s", res.err.Error())
			if res.outcome == stageHold {
				c.enqueueRolloutAfter(c.rollout, defaults.GetRolloutVerifyRetryInterval())
				return nil
			}
			return res.err
		}
	}
	return nil
}

func canaryStageSyncRevisionOnChange(c *rolloutContext) stageResult {
	if !replicasetutil.PodTemplateOrStepsChanged(c.rollout, c.newRS) {
		return stageResult{outcome: stageContinue}
	}
	var err error
	c.newRS, err = c.getAllReplicaSetsAndSyncRevision()
	if err != nil {
		return stageResult{
			outcome: stageFatal,
			err:     fmt.Errorf("failed to getAllReplicaSetsAndSyncRevision in rolloutCanary with PodTemplateOrStepsChanged: %w", err),
		}
	}
	return stageResult{outcome: stageStop}
}

func canaryStageSyncReplicaSets(c *rolloutContext) stageResult {
	var err error
	c.newRS, err = c.getAllReplicaSetsAndSyncRevision()
	if err != nil {
		return stageResult{
			outcome: stageFatal,
			err:     fmt.Errorf("failed to getAllReplicaSetsAndSyncRevision in rolloutCanary create true: %w", err),
		}
	}
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
		return stageResult{outcome: stageFatal, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func canaryStageRevisionHistory(c *rolloutContext) stageResult {
	if err := c.reconcileRevisionHistoryLimit(c.otherRSs); err != nil {
		return stageResult{outcome: stageFatal, err: err}
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
	return stageResult{outcome: stageContinue}
}

func canaryStageTrafficRouting(c *rolloutContext) stageResult {
	if err := c.reconcileTrafficRouting(); err != nil {
		return stageResult{
			outcome:   stageHold,
			err:       err,
			condition: v1alpha1.RolloutTrafficRoutingApplied,
			reason:    conditions.TrafficRoutingErrorReason,
		}
	}
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
