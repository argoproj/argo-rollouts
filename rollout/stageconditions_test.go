package rollout

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/record"
)

func TestStageConditionsMergedIntoStatus(t *testing.T) {
	f, ro := newTrafficWeightFixture(t)
	defer f.Close()
	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(errors.New("routing failed"))

	patchIndex := f.expectPatchRolloutAction(ro)
	f.runExpectError(getKey(ro, t), true)
	patched := f.getPatchedRolloutAsObject(patchIndex)
	cond := conditions.GetRolloutCondition(patched.Status, v1alpha1.RolloutReconcileSucceeded)
	assert.NotNil(t, cond)
	assert.Equal(t, corev1.ConditionFalse, cond.Status)
	assert.Equal(t, conditions.TrafficRoutingErrorReason, cond.Reason)

	basic := newCanaryRollout("basic", 1, nil, nil, nil, intstr.FromInt(1), intstr.FromInt(0))
	rs := newReplicaSetWithStatus(basic, 1, 1)
	f2 := newFixture(t)
	defer f2.Close()
	f2.rolloutLister = append(f2.rolloutLister, basic)
	f2.objects = append(f2.objects, basic)
	f2.kubeobjects = append(f2.kubeobjects, rs)
	f2.replicaSetLister = append(f2.replicaSetLister, rs)
	f2.expectPatchRolloutAction(basic)
	f2.run(getKey(basic, t))
}

// TestStageConditionRecoveryRequiresStageSuccess verifies that a previously-False stage condition
// only flips back to True when the owning stage actually re-ran and succeeded this reconcile —
// a pass that never reached the stage must not report a recovery that never happened.
func TestStageConditionRecoveryRequiresStageSuccess(t *testing.T) {
	ro := newCanaryRollout("foo", 1, nil, nil, nil, intstr.FromInt(1), intstr.FromInt(0))
	ro.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
	conditions.SetRolloutCondition(&ro.Status, *conditions.NewRolloutCondition(
		v1alpha1.RolloutReconcileSucceeded, corev1.ConditionFalse, conditions.TrafficRoutingErrorReason, "routing failed"))

	ctx := &rolloutContext{rollout: ro}
	newStatus := ro.Status.DeepCopy()

	// The full pipeline did not complete this pass: the condition must stay False.
	ctx.mergeStageConditions(newStatus)
	cond := conditions.GetRolloutCondition(*newStatus, v1alpha1.RolloutReconcileSucceeded)
	assert.NotNil(t, cond)
	assert.Equal(t, corev1.ConditionFalse, cond.Status, "a pass that did not finish must not report recovery")

	// Once the full pipeline completes without error, the condition recovers.
	ctx.markStageSucceeded()
	ctx.mergeStageConditions(newStatus)
	cond = conditions.GetRolloutCondition(*newStatus, v1alpha1.RolloutReconcileSucceeded)
	assert.NotNil(t, cond)
	assert.Equal(t, corev1.ConditionTrue, cond.Status)
	assert.Equal(t, conditions.StageConditionAppliedReason, cond.Reason)
}

// TestStageConditionNotRecoveredWhenPipelineStopsEarly is the end-to-end variant: a services
// failure aborts the pipeline before traffic routing runs, so ReconcileSucceeded stays False
// with the service failure reason instead of reporting recovery.
func TestStageConditionNotRecoveredWhenPipelineStopsEarly(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{SetWeight: ptr.To[int32](10)}}
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
	r2.Spec.Strategy.Canary.StableService = "stable"
	r2.Spec.Strategy.Canary.CanaryService = "canary"

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	canarySvc := newService("canary", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}, r2)
	stableSvc := newService("stable", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}, r2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 11, 1, 11, false)
	conditions.SetRolloutCondition(&r2.Status, *conditions.NewRolloutCondition(
		v1alpha1.RolloutReconcileSucceeded, corev1.ConditionFalse, conditions.TrafficRoutingErrorReason, "routing failed"))
	f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.serviceLister = append(f.serviceLister, canarySvc, stableSvc)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	c, i, k8sI := f.newController(noResyncPeriodFunc)
	f.kubeclient.Fake.PrependReactor("patch", "services", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("admission webhook denied service update")
	})

	_ = f.expectPatchServiceAction(canarySvc, rs2PodHash)
	patchIndex := f.expectPatchRolloutAction(r2)
	f.runController(getKey(r2, t), true, true, c, i, k8sI)

	patched := f.getPatchedRolloutAsObject(patchIndex)
	cond := conditions.GetRolloutCondition(patched.Status, v1alpha1.RolloutReconcileSucceeded)
	assert.NotNil(t, cond)
	assert.Equal(t, corev1.ConditionFalse, cond.Status)
	assert.Equal(t, conditions.ServiceUpdateErrorReason, cond.Reason)
}

func TestConditionMessageTruncated(t *testing.T) {
	ctx := &rolloutContext{}
	longMsg := strings.Repeat("x", maxStageConditionMessageLen+10)
	ctx.setStageCondition(v1alpha1.RolloutReconcileSucceeded, corev1.ConditionFalse, conditions.RolloutReconciliationErrorReason, longMsg)
	cond := ctx.stageConditions[v1alpha1.RolloutReconcileSucceeded]
	assert.Len(t, cond.Message, maxStageConditionMessageLen)
}

func TestConditionMessageTruncatedOnRuneBoundary(t *testing.T) {
	// Place a multi-byte rune straddling the truncation limit; the cut must back up to a rune
	// boundary instead of persisting invalid UTF-8.
	msg := strings.Repeat("x", maxStageConditionMessageLen-1) + "日本語のエラー"
	truncated := truncateStageConditionMessage(msg)
	assert.LessOrEqual(t, len(truncated), maxStageConditionMessageLen)
	assert.True(t, utf8.ValidString(truncated), "truncated message must remain valid UTF-8")
	assert.Equal(t, maxStageConditionMessageLen-1, len(truncated))
}

// TestReconcileSucceededRecoversAfterPipelineSuccess verifies a stale
// ReconcileSucceeded=False recovers to True once the full pipeline succeeds.
func TestReconcileSucceededRecoversAfterPipelineSuccess(t *testing.T) {
	ro := newCanaryRollout("foo", 1, nil, nil, nil, intstr.FromInt(1), intstr.FromInt(0))
	conditions.SetRolloutCondition(&ro.Status, *conditions.NewRolloutCondition(
		v1alpha1.RolloutReconcileSucceeded, corev1.ConditionFalse, conditions.ServiceUpdateErrorReason, "service update failed"))

	ctx := &rolloutContext{rollout: ro}
	newStatus := ro.Status.DeepCopy()
	ctx.mergeStageConditions(newStatus)
	cond := conditions.GetRolloutCondition(*newStatus, v1alpha1.RolloutReconcileSucceeded)
	assert.NotNil(t, cond)
	assert.Equal(t, corev1.ConditionFalse, cond.Status)

	ctx.markStageSucceeded()
	ctx.mergeStageConditions(newStatus)
	cond = conditions.GetRolloutCondition(*newStatus, v1alpha1.RolloutReconcileSucceeded)
	assert.NotNil(t, cond)
	assert.Equal(t, corev1.ConditionTrue, cond.Status)
}

func TestConditionNoChurnOnRepeatedError(t *testing.T) {
	status := v1alpha1.RolloutStatus{}
	cond := *conditions.NewRolloutCondition(v1alpha1.RolloutReconcileSucceeded, corev1.ConditionFalse, conditions.TrafficRoutingErrorReason, "same error")
	firstTransition := cond.LastTransitionTime
	conditions.SetRolloutCondition(&status, cond)

	repeated := *conditions.NewRolloutCondition(v1alpha1.RolloutReconcileSucceeded, corev1.ConditionFalse, conditions.TrafficRoutingErrorReason, "same error with different suffix")
	updated := conditions.SetRolloutCondition(&status, repeated)
	assert.False(t, updated)
	persisted := conditions.GetRolloutCondition(status, v1alpha1.RolloutReconcileSucceeded)
	assert.Equal(t, firstTransition, persisted.LastTransitionTime)
}

// TestStageFailureEventOnlyOnTransition verifies a persistently failing stage emits its warning
// event only when ReconcileSucceeded transitions (new failure or changed reason), not on every
// backoff retry.
func TestStageFailureEventOnlyOnTransition(t *testing.T) {
	failure := stageResult{outcome: stageStop, err: errors.New("routing failed"), reason: conditions.TrafficRoutingErrorReason}
	newCtx := func(prev *v1alpha1.RolloutCondition) (*rolloutContext, *record.FakeEventRecorder) {
		ro := &v1alpha1.Rollout{}
		if prev != nil {
			conditions.SetRolloutCondition(&ro.Status, *prev)
		}
		recorder := record.NewFakeEventRecorder()
		return &rolloutContext{rollout: ro, reconcilerBase: reconcilerBase{recorder: recorder}}, recorder
	}

	ctx, recorder := newCtx(nil)
	ctx.recordStageFailure(failure)
	assert.Equal(t, []string{conditions.TrafficRoutingErrorReason}, recorder.Events(), "a new failure must emit an event")
	assert.True(t, ctx.progressionBlocked)

	ctx, recorder = newCtx(conditions.NewRolloutCondition(v1alpha1.RolloutReconcileSucceeded, corev1.ConditionFalse, conditions.TrafficRoutingErrorReason, "routing failed"))
	ctx.recordStageFailure(failure)
	assert.Empty(t, recorder.Events(), "a repeated failure with the same reason must not emit an event")
	assert.True(t, ctx.progressionBlocked)

	ctx, recorder = newCtx(conditions.NewRolloutCondition(v1alpha1.RolloutReconcileSucceeded, corev1.ConditionFalse, conditions.ServiceUpdateErrorReason, "service update failed"))
	ctx.recordStageFailure(failure)
	assert.Equal(t, []string{conditions.TrafficRoutingErrorReason}, recorder.Events(), "a changed reason must emit an event")
}
