package rollout

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
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

// TestStageErrorEndsPassWithoutStatusSync verifies the current error semantics: a stage error
// stops the remaining stages, is returned for workqueue backoff, and ends the pass without a
// status sync. A normal stop (e.g. waiting for scaling) still syncs status.
func TestStageErrorEndsPassWithoutStatusSync(t *testing.T) {
	orig := canaryStages
	defer func() { canaryStages = orig }()
	boom := errors.New("service update failed")

	t.Run("stage error", func(t *testing.T) {
		laterStageRan := false
		canaryStages = []strategyStage{
			{"failing", func(c *rolloutContext) stageResult {
				return stageResult{outcome: stageStopNoStatus, err: boom}
			}},
			{"later", func(c *rolloutContext) stageResult {
				laterStageRan = true
				return stageResult{outcome: stageContinue}
			}},
		}
		// A bare context suffices: if the status sync were not skipped, syncRolloutStatusCanary
		// would dereference nil members and panic.
		ctx := &rolloutContext{}
		err := ctx.rolloutCanary()
		assert.ErrorIs(t, err, boom)
		assert.True(t, ctx.skipStatusSync)
		assert.False(t, laterStageRan, "a stage error must stop the remaining stages")
	})

	t.Run("normal stop", func(t *testing.T) {
		canaryStages = []strategyStage{
			{"waiting", func(c *rolloutContext) stageResult {
				return stageResult{outcome: stageStop}
			}},
		}
		ctx := &rolloutContext{log: logutil.WithRollout(&v1alpha1.Rollout{})}
		err := ctx.runCanaryStages()
		assert.NoError(t, err)
		assert.False(t, ctx.skipStatusSync, "a normal stop must still sync status")
	})
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
