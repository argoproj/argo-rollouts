package rollout

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
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

func TestStagePipelineSemantics(t *testing.T) {
	newTestContext := func() *rolloutContext {
		return &rolloutContext{
			rollout: &v1alpha1.Rollout{},
			log:     logutil.WithRollout(&v1alpha1.Rollout{}),
			reconcilerBase: reconcilerBase{
				recorder: record.NewFakeEventRecorder(),
			},
		}
	}

	t.Run("stageContinue runs all stages", func(t *testing.T) {
		ran := []string{}
		stages := []strategyStage{
			{"a", func(c *rolloutContext) stageResult {
				ran = append(ran, "a")
				return stageResult{outcome: stageContinue}
			}},
			{"b", func(c *rolloutContext) stageResult {
				ran = append(ran, "b")
				return stageResult{outcome: stageContinue}
			}},
		}
		ctx := newTestContext()
		err := ctx.runStages(stages)
		require.NoError(t, err)
		assert.False(t, ctx.skipStatusSync)
		assert.Equal(t, []string{"a", "b"}, ran)
		assertStrategyWouldSyncStatus(t, ctx, err, true)
	})

	t.Run("stageStop halts pipeline without error", func(t *testing.T) {
		ran := []string{}
		stages := []strategyStage{
			{"a", func(c *rolloutContext) stageResult {
				ran = append(ran, "a")
				return stageResult{outcome: stageContinue}
			}},
			{"b", func(c *rolloutContext) stageResult { ran = append(ran, "b"); return stageResult{outcome: stageStop} }},
			{"c", func(c *rolloutContext) stageResult {
				ran = append(ran, "c")
				return stageResult{outcome: stageContinue}
			}},
		}
		ctx := newTestContext()
		err := ctx.runStages(stages)
		require.NoError(t, err)
		assert.False(t, ctx.skipStatusSync)
		assert.Equal(t, []string{"a", "b"}, ran)
		assertStrategyWouldSyncStatus(t, ctx, err, true)
	})

	t.Run("stageFatal halts pipeline and returns error", func(t *testing.T) {
		fatalErr := errors.New("stage failed")
		ran := []string{}
		stages := []strategyStage{
			{"a", func(c *rolloutContext) stageResult {
				ran = append(ran, "a")
				return stageResult{outcome: stageContinue}
			}},
			{"b", func(c *rolloutContext) stageResult {
				ran = append(ran, "b")
				return stageResult{outcome: stageFatal, err: fatalErr}
			}},
			{"c", func(c *rolloutContext) stageResult {
				ran = append(ran, "c")
				return stageResult{outcome: stageContinue}
			}},
		}
		ctx := newTestContext()
		err := ctx.runStages(stages)
		assert.ErrorIs(t, err, fatalErr)
		assert.False(t, ctx.skipStatusSync)
		assert.Equal(t, []string{"a", "b"}, ran)
		assertStrategyWouldSyncStatus(t, ctx, err, false)
	})

	t.Run("stageStopNoStatus with nil error skips status sync", func(t *testing.T) {
		ran := []string{}
		stages := []strategyStage{
			{"a", func(c *rolloutContext) stageResult {
				ran = append(ran, "a")
				return stageResult{outcome: stageStopNoStatus}
			}},
			{"b", func(c *rolloutContext) stageResult {
				ran = append(ran, "b")
				return stageResult{outcome: stageContinue}
			}},
		}
		ctx := newTestContext()
		err := ctx.runStages(stages)
		require.NoError(t, err)
		assert.True(t, ctx.skipStatusSync)
		assert.Equal(t, []string{"a"}, ran)
		assertStrategyWouldSyncStatus(t, ctx, err, false)
	})

	t.Run("stageStopNoStatus with error skips status sync and returns error", func(t *testing.T) {
		syncErr := errors.New("replicaset sync failed")
		stages := []strategyStage{
			{"a", func(c *rolloutContext) stageResult {
				return stageResult{outcome: stageStopNoStatus, err: syncErr}
			}},
		}
		ctx := newTestContext()
		err := ctx.runStages(stages)
		assert.ErrorIs(t, err, syncErr)
		assert.True(t, ctx.skipStatusSync)
		assertStrategyWouldSyncStatus(t, ctx, err, false)
	})
}

// assertStrategyWouldSyncStatus mirrors the rolloutCanary/rolloutBlueGreen wrapper contract.
func assertStrategyWouldSyncStatus(t *testing.T, ctx *rolloutContext, stageErr error, wantSync bool) {
	t.Helper()
	gotSync := !ctx.skipStatusSync && stageErr == nil
	assert.Equal(t, wantSync, gotSync)
}
