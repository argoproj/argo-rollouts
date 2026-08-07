package rollout

import (
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
	t.Run("stageHold swallows error and requeues", func(t *testing.T) {
		f, ro := newTrafficWeightFixture(t)
		defer f.Close()
		f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
		f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
		f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(assert.AnError)

		enqueued := false
		c, i, k8sI := f.newController(noResyncPeriodFunc)
		c.enqueueRolloutAfter = func(obj any, duration time.Duration) {
			enqueued = true
			assert.Equal(t, defaults.GetRolloutVerifyRetryInterval(), duration)
		}

		patchIndex := f.expectPatchRolloutAction(ro)
		f.runController(getKey(ro, t), true, false, c, i, k8sI)
		assert.True(t, enqueued)

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
