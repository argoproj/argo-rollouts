package rollout

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
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

func TestCanaryStagePipelineSemantics(t *testing.T) {
	t.Run("trafficRouting error returns error and still syncs status", func(t *testing.T) {
		f, ro := newTrafficWeightFixture(t)
		defer f.Close()
		f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
		f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
		f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(assert.AnError)

		enqueued := false
		c, i, k8sI := f.newController(noResyncPeriodFunc)
		c.enqueueRolloutAfter = func(obj any, duration time.Duration) {
			if duration == defaults.GetRolloutVerifyRetryInterval() {
				enqueued = true
			}
		}

		patchIndex := f.expectPatchRolloutAction(ro)
		// The error must propagate for workqueue backoff and error metrics; retry pacing comes
		// from the rate-limited requeue, not a fixed verify-interval requeue.
		f.runController(getKey(ro, t), true, true, c, i, k8sI)
		assert.False(t, enqueued)

		patched := f.getPatchedRolloutAsObject(patchIndex)
		cond := conditions.GetRolloutCondition(patched.Status, v1alpha1.RolloutTrafficRoutingApplied)
		assert.NotNil(t, cond)
		assert.Equal(t, corev1.ConditionFalse, cond.Status)
	})

	t.Run("stageFatal returns error and does not requeue on hold interval", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		steps := []v1alpha1.CanaryStep{{SetWeight: ptr.To[int32](10)}}
		r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(1), intstr.FromInt(0))
		r2 := bumpVersion(r1)
		r2.Spec.Strategy.Canary.StableService = "stable"
		r2.Spec.Strategy.Canary.CanaryService = "canary"

		rs1 := newReplicaSetWithStatus(r1, 10, 10)
		rs2 := newReplicaSetWithStatus(r2, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		canarySvc := newService("canary", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}, r2)
		stableSvc := newService("stable", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}, r2)

		r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 11, 1, 11, false)
		f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
		f.serviceLister = append(f.serviceLister, canarySvc, stableSvc)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.objects = append(f.objects, r2)

		enqueued := false
		c, i, k8sI := f.newController(noResyncPeriodFunc)
		c.enqueueRolloutAfter = func(obj any, duration time.Duration) {
			if duration == defaults.GetRolloutVerifyRetryInterval() {
				enqueued = true
			}
		}
		f.kubeclient.Fake.PrependReactor("patch", "services", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, fmt.Errorf("admission webhook denied service update")
		})

		_ = f.expectPatchServiceAction(canarySvc, rs2PodHash)
		patchIndex := f.expectPatchRolloutAction(r2)
		f.runController(getKey(r2, t), true, true, c, i, k8sI)
		assert.False(t, enqueued)
		patched := f.getPatchedRolloutAsObject(patchIndex)
		cond := conditions.GetRolloutCondition(patched.Status, v1alpha1.RolloutServicesReconciled)
		assert.NotNil(t, cond)
		assert.Equal(t, corev1.ConditionFalse, cond.Status)
	})
}

// TestCanaryStageSyncFailurePreservesNewRS verifies that a failed getAllReplicaSetsAndSyncRevision
// does not clobber c.newRS to nil and skips the status sync (stageFatalNoStatus): a status
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
	assert.Equal(t, stageFatalNoStatus, res.outcome)
	assert.Error(t, res.err)
	assert.NotNil(t, roCtx.newRS, "newRS must not be clobbered by a failed ReplicaSet sync")

	res = canaryStageSyncReplicaSets(roCtx)
	assert.Equal(t, stageFatalNoStatus, res.outcome)
	assert.Error(t, res.err)
	assert.NotNil(t, roCtx.newRS, "newRS must not be clobbered by a failed ReplicaSet sync")
}

// TestStageFatalNoStatusSkipsStatusSync verifies the stageFatalNoStatus plumbing: the error is
// returned for workqueue backoff and the status sync is skipped entirely.
func TestStageFatalNoStatusSkipsStatusSync(t *testing.T) {
	orig := canaryStages
	defer func() { canaryStages = orig }()
	boom := errors.New("replicaset sync failed")
	canaryStages = []canaryStage{{name: "boom", run: func(c *rolloutContext) stageResult {
		return stageResult{outcome: stageFatalNoStatus, err: boom}
	}}}

	// A bare context suffices: if the status sync were not skipped, syncRolloutStatusCanary
	// would dereference nil members and panic.
	ctx := &rolloutContext{}
	err := ctx.rolloutCanary()
	assert.Equal(t, boom, err)
	assert.True(t, ctx.skipStatusSync)
}
