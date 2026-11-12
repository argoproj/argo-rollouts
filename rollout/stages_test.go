package rollout

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

func TestCanaryStageTableMatchesLegacyOrder(t *testing.T) {
	expected := []string{
		"syncRevisionOnChange",
		"syncReplicaSets",
		"podRestart",
		"ephemeralMetadata",
		"revisionHistory",
		"pingPongService",
		"stableCanaryService",
		"trafficRouting",
		"experiments",
		"analysis",
		"replicaSetScaling",
		"canaryPause",
		"stepPlugins",
	}
	names := make([]string, len(canaryStages))
	for i, stage := range canaryStages {
		names[i] = stage.name
	}
	assert.Equal(t, expected, names)
}

// TestBlueGreenStageTableMatchesLegacyOrder guards against accidental reordering of the
// blue-green reconcile pipeline.
func TestBlueGreenStageTableMatchesLegacyOrder(t *testing.T) {
	expected := []string{
		"previewService",
		"podTemplateChange",
		"podRestart",
		"replicaSets",
		"pause",
		"activeService",
		"targetGroups",
		"analysis",
		"ephemeralMetadata",
	}
	names := make([]string, len(blueGreenStages))
	for i, stage := range blueGreenStages {
		names[i] = stage.name
	}
	assert.Equal(t, expected, names)
}

func TestStageFailureBlocksProgression(t *testing.T) {
	orig := canaryStages
	defer func() { canaryStages = orig }()
	boom := errors.New("service update failed")

	t.Run("stageStop with error blocks progression", func(t *testing.T) {
		laterStageRan := false
		canaryStages = []strategyStage{
			{"failing", func(c *rolloutContext) stageResult {
				return stageResult{outcome: stageStop, err: boom}
			}},
			{"later", func(c *rolloutContext) stageResult {
				laterStageRan = true
				return stageResult{outcome: stageContinue}
			}},
		}
		ctx := &rolloutContext{}
		err := ctx.runCanaryStages()
		assert.ErrorIs(t, err, boom)
		assert.True(t, ctx.progressionBlocked)
		assert.False(t, laterStageRan, "a stage failure must stop the remaining stages")
		assert.False(t, ctx.skipStatusSync, "a stage failure must still sync status")
	})

	t.Run("stageStop without error does not block progression", func(t *testing.T) {
		canaryStages = []strategyStage{
			{"waiting", func(c *rolloutContext) stageResult {
				return stageResult{outcome: stageStop}
			}},
		}
		ctx := &rolloutContext{log: logutil.WithRollout(&v1alpha1.Rollout{})}
		err := ctx.runCanaryStages()
		assert.NoError(t, err)
		assert.False(t, ctx.progressionBlocked)
	})
}

func TestStepHeldWhileStageFailed(t *testing.T) {
	f, ro := newTrafficWeightFixture(t)
	defer f.Close()
	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(errors.New("routing failed"))

	patchIndex := f.expectPatchRolloutAction(ro)
	f.runExpectError(getKey(ro, t), true)
	patched := f.getPatchedRolloutAsObject(patchIndex)
	assert.Nil(t, patched.Status.CurrentStepIndex)
}

func TestPromoteFullHeldWhileStageFailed(t *testing.T) {
	f, ro := newTrafficWeightFixture(t)
	defer f.Close()
	ro.Status.PromoteFull = true
	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("RemoveManagedRoutes").Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(errors.New("routing failed"))

	patchIndex := f.expectPatchRolloutAction(ro)
	f.runExpectError(getKey(ro, t), true)
	patch := f.getPatchedRollout(patchIndex)
	assert.NotContains(t, patch, fmt.Sprintf(`"stableRS":"%s"`, ro.Status.CurrentPodHash),
		"a stage failure must hold the forced promotion")
	assert.NotContains(t, patch, `"currentStepIndex"`,
		"a held full promotion must not rewrite the step index; patch: %s", patch)
	assert.NotContains(t, patch, `"promoteFull":false`,
		"PromoteFull stays set until the promotion is applied; patch: %s", patch)
	assert.Contains(t, f.events, conditions.PromoteFullHeldReason,
		"the held promotion must be surfaced to the operator via an event")
}

// TestCanaryStageSyncFailurePreservesNewRS verifies that a failed getAllReplicaSetsAndSyncRevision
// does not clobber c.newRS to nil and skips the status sync (stageStopNoStatus with err): a status
// computed from a nil newRS would reset abort/pause state and record a phantom CurrentPodHash on
// a template change, or flap UpdatedReplicas to 0 on a transient API error.
func TestCanaryStageSyncFailurePreservesNewRS(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{SetWeight: ptr.To[int32](10)}, {Pause: &v1alpha1.RolloutPause{}}}
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	// Force syncReplicaSetRevision down the updateReplicaSet path so the injected failure hits.
	rs2.Annotations[annotations.RevisionAnnotation] = "1"

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 11, 1, 11, false)
	// Simulate a pod template change whose status sync has not happened yet.
	r2.Status.CurrentPodHash = rs1PodHash

	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	ctrl, _, _ := f.newController(noResyncPeriodFunc)
	f.kubeclient.PrependReactor("update", "replicasets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("api server unavailable")
	})
	roCtx, err := ctrl.newRolloutContext(r2)
	assert.NoError(t, err)

	res := canaryStageSyncRevisionOnChange(roCtx)
	assert.Equal(t, stageStopNoStatus, res.outcome)
	assert.Error(t, res.err)
	assert.NotNil(t, roCtx.newRS, "newRS must not be clobbered by a failed ReplicaSet sync")

	res = canaryStageSyncReplicaSets(roCtx)
	assert.Equal(t, stageStopNoStatus, res.outcome)
	assert.Error(t, res.err)
	assert.NotNil(t, roCtx.newRS, "newRS must not be clobbered by a failed ReplicaSet sync")
}

func TestStageStopNoStatusSkipsStatusSync(t *testing.T) {
	orig := canaryStages
	defer func() { canaryStages = orig }()
	boom := errors.New("replicaset sync failed")
	canaryStages = []strategyStage{{name: "boom", run: func(c *rolloutContext) stageResult {
		return stageResult{outcome: stageStopNoStatus, err: boom}
	}}}

	// A bare context would panic in syncRolloutStatusCanary if the sync weren't skipped.
	ctx := &rolloutContext{}
	err := ctx.rolloutCanary()
	assert.ErrorIs(t, err, boom)
	assert.True(t, ctx.skipStatusSync)
}

func TestStageFailurePreservesCurrentAnalysisRun(t *testing.T) {
	for _, phase := range []v1alpha1.AnalysisPhase{v1alpha1.AnalysisPhaseRunning, v1alpha1.AnalysisPhaseFailed} {
		t.Run(string(phase), func(t *testing.T) {
			f := newFixture(t)
			defer f.Close()

			at := analysisTemplate("bar")
			steps := []v1alpha1.CanaryStep{{Analysis: &v1alpha1.RolloutAnalysis{Templates: []v1alpha1.AnalysisTemplateRef{{TemplateName: at.Name}}}}}
			r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](0), intstr.FromInt(1), intstr.FromInt(1))
			r1.Spec.Strategy.Canary.StableService = "stable"
			r1.Spec.Strategy.Canary.CanaryService = "canary"
			r2 := bumpVersion(r1)
			ar := analysisRun(at, v1alpha1.RolloutTypeStepLabel, r2)
			ar.Status.Phase = phase

			rs1 := newReplicaSetWithStatus(r1, 1, 1)
			rs2 := newReplicaSetWithStatus(r2, 1, 1)
			rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
			rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
			// stale canary selector forces a service patch, which the reactor below fails
			canarySvc := newService("canary", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}, r2)
			stableSvc := newService("stable", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}, r2)

			r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 2, 1, 2, false)
			r2.Status.Canary.CurrentStepAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
				Name:   ar.Name,
				Status: v1alpha1.AnalysisPhaseRunning,
			}

			f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
			f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
			f.serviceLister = append(f.serviceLister, canarySvc, stableSvc)
			f.rolloutLister = append(f.rolloutLister, r2)
			f.analysisTemplateLister = append(f.analysisTemplateLister, at)
			f.analysisRunLister = append(f.analysisRunLister, ar)
			f.objects = append(f.objects, r2, at, ar)

			c, i, k8sI := f.newController(noResyncPeriodFunc)
			f.kubeclient.PrependReactor("patch", "services", func(action k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, fmt.Errorf("admission webhook denied service update")
			})
			_ = f.expectPatchServiceAction(canarySvc, rs2PodHash)
			patchIndex := f.expectPatchRolloutAction(r2)
			f.runController(getKey(r2, t), true, true, c, i, k8sI)

			patch := f.getPatchedRollout(patchIndex)
			assert.NotContains(t, patch, "currentStepAnalysisRunStatus",
				"the current AnalysisRun must stay recorded in status; patch: %s", patch)
		})
	}
}

func TestCarryOverUnreconciledStatus(t *testing.T) {
	newCtx := func() *rolloutContext {
		ro := newCanaryRollout("foo", 1, nil, nil, nil, intstr.FromInt(1), intstr.FromInt(0))
		ro.Status.Canary.CurrentExperiment = "foo-experiment"
		ro.Status.Canary.CurrentStepAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{Name: "foo-step"}
		ro.Status.Canary.CurrentBackgroundAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{Name: "foo-background"}
		return &rolloutContext{rollout: ro}
	}

	t.Run("stages that did not complete keep the previous values", func(t *testing.T) {
		ctx := newCtx()
		ctx.carryOverUnreconciledStatus()
		assert.Equal(t, "foo-experiment", ctx.newStatus.Canary.CurrentExperiment)
		assert.Equal(t, "foo-step", ctx.newStatus.Canary.CurrentStepAnalysisRunStatus.Name)
		assert.Equal(t, "foo-background", ctx.newStatus.Canary.CurrentBackgroundAnalysisRunStatus.Name)
	})

	t.Run("stages that completed keep the values they set", func(t *testing.T) {
		ctx := newCtx()
		ctx.experimentsReconciled = true
		ctx.analysisReconciled = true
		ctx.carryOverUnreconciledStatus()
		assert.Empty(t, ctx.newStatus.Canary.CurrentExperiment, "a completed stage may clear the current Experiment")
		assert.Nil(t, ctx.newStatus.Canary.CurrentStepAnalysisRunStatus, "a completed stage may clear the current AnalysisRun")
		assert.Nil(t, ctx.newStatus.Canary.CurrentBackgroundAnalysisRunStatus)
	})
}
