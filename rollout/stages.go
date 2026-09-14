package rollout

import (
	"fmt"

	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

type stageOutcome int

const (
	// stageContinue: proceed to the next stage.
	stageContinue stageOutcome = iota
	// stageStop: halt the pipeline and fall through to status sync. err must be nil.
	stageStop
	// stageFatal: halt the pipeline and return the error without status sync.
	stageFatal
	// stageStopNoStatus: halt without status sync. Used for the pod-restart early exit (no err)
	// and ReplicaSet-sync failures (err set).
	stageStopNoStatus
)

type stageResult struct {
	outcome stageOutcome
	err     error
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
}

func (c *rolloutContext) runCanaryStages() error {
	return c.runStages(canaryStages)
}

func (c *rolloutContext) runBlueGreenStages() error {
	return c.runStages(blueGreenStages)
}

func (c *rolloutContext) runStages(stages []strategyStage) error {
	for _, s := range stages {
		res := s.run(c)
		switch res.outcome {
		case stageContinue:
		case stageStop:
			c.log.Infof("stage %s: stopping further changes, proceeding to status sync", s.name)
			return nil
		case stageFatal:
			return res.err
		case stageStopNoStatus:
			c.skipStatusSync = true
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
			outcome: stageStopNoStatus,
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
			outcome: stageStopNoStatus,
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
		return stageResult{outcome: stageFatal, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func canaryStageStableCanaryService(c *rolloutContext) stageResult {
	if err := c.reconcileStableAndCanaryService(); err != nil {
		return stageResult{outcome: stageFatal, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func canaryStageTrafficRouting(c *rolloutContext) stageResult {
	if err := c.reconcileTrafficRouting(); err != nil {
		return stageResult{outcome: stageFatal, err: err}
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

func blueGreenStagePreviewService(c *rolloutContext) stageResult {
	// This must happen right after the new replicaset is created.
	previewSvc, _, err := c.getPreviewAndActiveServices()
	if err != nil {
		return stageResult{outcome: stageFatal, err: err}
	}
	if err := c.reconcilePreviewService(previewSvc); err != nil {
		return stageResult{outcome: stageFatal, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func blueGreenStagePodTemplateChange(c *rolloutContext) stageResult {
	if replicasetutil.CheckPodSpecChange(c.rollout, c.newRS) {
		return stageResult{outcome: stageStop}
	}
	return stageResult{outcome: stageContinue}
}

func blueGreenStagePodRestart(c *rolloutContext) stageResult {
	if _, err := c.podRestarter.Reconcile(c); err != nil {
		return stageResult{outcome: stageFatal, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func blueGreenStageReplicaSets(c *rolloutContext) stageResult {
	_, activeSvc, err := c.getPreviewAndActiveServices()
	if err != nil {
		return stageResult{outcome: stageFatal, err: err}
	}
	if err := c.reconcileBlueGreenReplicaSets(activeSvc); err != nil {
		return stageResult{outcome: stageFatal, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func blueGreenStagePause(c *rolloutContext) stageResult {
	previewSvc, activeSvc, err := c.getPreviewAndActiveServices()
	if err != nil {
		return stageResult{outcome: stageFatal, err: err}
	}
	c.reconcileBlueGreenPause(activeSvc, previewSvc)
	return stageResult{outcome: stageContinue}
}

func blueGreenStageActiveService(c *rolloutContext) stageResult {
	_, activeSvc, err := c.getPreviewAndActiveServices()
	if err != nil {
		return stageResult{outcome: stageFatal, err: err}
	}
	if err := c.reconcileActiveService(activeSvc); err != nil {
		return stageResult{outcome: stageFatal, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func blueGreenStageTargetGroups(c *rolloutContext) stageResult {
	_, activeSvc, err := c.getPreviewAndActiveServices()
	if err != nil {
		return stageResult{outcome: stageFatal, err: err}
	}
	if err := c.awsVerifyTargetGroups(activeSvc); err != nil {
		return stageResult{outcome: stageFatal, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func blueGreenStageAnalysis(c *rolloutContext) stageResult {
	if err := c.reconcileAnalysisRuns(); err != nil {
		return stageResult{outcome: stageFatal, err: err}
	}
	return stageResult{outcome: stageContinue}
}

func blueGreenStageEphemeralMetadata(c *rolloutContext) stageResult {
	if err := c.reconcileEphemeralMetadata(); err != nil {
		return stageResult{outcome: stageFatal, err: err}
	}
	return stageResult{outcome: stageContinue}
}
