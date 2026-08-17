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
	cond := conditions.GetRolloutCondition(patched.Status, v1alpha1.RolloutTrafficRoutingApplied)
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
	patchIndex = f2.expectPatchRolloutAction(basic)
	f2.run(getKey(basic, t))
	patchedBasic := f2.getPatchedRolloutAsObject(patchIndex)
	assert.Nil(t, conditions.GetRolloutCondition(patchedBasic.Status, v1alpha1.RolloutTrafficRoutingApplied))
}

func TestStepHeldWhileStageConditionFalse(t *testing.T) {
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

func TestPromotionHeldWhileStageConditionFalse(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{SetWeight: ptr.To[int32](10)}, {Pause: &v1alpha1.RolloutPause{}}}
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](2), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
	r2.Spec.Strategy.Canary.CanaryService = "canary"
	r2.Spec.Strategy.Canary.StableService = "stable"

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 10, 10)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	canarySvc := newService("canary", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}, r2)
	stableSvc := newService("stable", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}, r2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, false)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](false), nil)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	assert.NotContains(t, f.getPatchedRollout(patchIndex), fmt.Sprintf(`"stableRS":"%s"`, rs2PodHash))
}

// TestPromoteFullHeldEmitsEvent verifies that a user-requested full promotion (promote --full)
// held by the unverified-weights safety gate emits an event explaining why the forced promotion
// did not take effect.
func TestPromoteFullHeldEmitsEvent(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{SetWeight: ptr.To[int32](10)}, {Pause: &v1alpha1.RolloutPause{}}}
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](2), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
	r2.Spec.Strategy.Canary.CanaryService = "canary"
	r2.Spec.Strategy.Canary.StableService = "stable"

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 10, 10)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	canarySvc := newService("canary", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}, r2)
	stableSvc := newService("stable", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}, r2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, false)
	r2.Status.PromoteFull = true
	f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](false), nil)
	f.fakeTrafficRouting.On("RemoveManagedRoutes").Return(nil)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	assert.NotContains(t, f.getPatchedRollout(patchIndex), fmt.Sprintf(`"stableRS":"%s"`, rs2PodHash),
		"unverified weights must still hold the forced promotion")
	assert.Contains(t, strings.Join(f.events, " "), conditions.PromoteFullHeldReason,
		"the held promotion must be surfaced to the operator via an event")
}

// TestStageConditionRecoveryRequiresStageSuccess verifies that a previously-False stage condition
// only flips back to True when the owning stage actually re-ran and succeeded this reconcile —
// a pass that never reached the stage must not report a recovery that never happened.
func TestStageConditionRecoveryRequiresStageSuccess(t *testing.T) {
	ro := newCanaryRollout("foo", 1, nil, nil, nil, intstr.FromInt(1), intstr.FromInt(0))
	ro.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
	conditions.SetRolloutCondition(&ro.Status, *conditions.NewRolloutCondition(
		v1alpha1.RolloutTrafficRoutingApplied, corev1.ConditionFalse, conditions.TrafficRoutingErrorReason, "routing failed"))

	ctx := &rolloutContext{rollout: ro}
	newStatus := ro.Status.DeepCopy()

	// The trafficRouting stage did not run this pass (e.g. an earlier stage stopped the
	// pipeline, or a scaling-only pass ran no stages): the condition must stay False.
	ctx.mergeStageConditions(newStatus)
	cond := conditions.GetRolloutCondition(*newStatus, v1alpha1.RolloutTrafficRoutingApplied)
	assert.NotNil(t, cond)
	assert.Equal(t, corev1.ConditionFalse, cond.Status, "a stage that did not run must not report recovery")

	// Once the owning stage actually re-runs and succeeds, the condition recovers.
	ctx.markStageSucceeded(v1alpha1.RolloutTrafficRoutingApplied)
	ctx.mergeStageConditions(newStatus)
	cond = conditions.GetRolloutCondition(*newStatus, v1alpha1.RolloutTrafficRoutingApplied)
	assert.NotNil(t, cond)
	assert.Equal(t, corev1.ConditionTrue, cond.Status)
	assert.Equal(t, conditions.StageConditionAppliedReason, cond.Reason)
}

// TestStageConditionNotRecoveredWhenPipelineStopsEarly is the end-to-end variant: a services
// failure aborts the pipeline before the trafficRouting stage runs, so a pre-existing
// TrafficRoutingApplied=False must survive the status sync unchanged instead of flipping True.
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
		v1alpha1.RolloutTrafficRoutingApplied, corev1.ConditionFalse, conditions.TrafficRoutingErrorReason, "routing failed"))
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
	servicesCond := conditions.GetRolloutCondition(patched.Status, v1alpha1.RolloutServicesReconciled)
	assert.NotNil(t, servicesCond)
	assert.Equal(t, corev1.ConditionFalse, servicesCond.Status)
	trCond := conditions.GetRolloutCondition(patched.Status, v1alpha1.RolloutTrafficRoutingApplied)
	if trCond != nil {
		assert.Equal(t, corev1.ConditionFalse, trCond.Status,
			"trafficRouting stage never ran this pass; its condition must not report recovery")
	}
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

// TestStageConditionsOnStrategyMigration verifies canary-only stage conditions are handled when
// a rollout is migrated to blue-green: TrafficRoutingApplied is removed as inapplicable, and a
// stale ServicesReconciled=False recovers once the blue-green service stages succeed.
func TestStageConditionsOnStrategyMigration(t *testing.T) {
	ro := newCanaryRollout("foo", 1, nil, nil, nil, intstr.FromInt(1), intstr.FromInt(0))
	ro.Spec.Strategy.Canary = nil
	ro.Spec.Strategy.BlueGreen = &v1alpha1.BlueGreenStrategy{}
	conditions.SetRolloutCondition(&ro.Status, *conditions.NewRolloutCondition(
		v1alpha1.RolloutServicesReconciled, corev1.ConditionFalse, conditions.ServiceUpdateErrorReason, "service update failed"))
	conditions.SetRolloutCondition(&ro.Status, *conditions.NewRolloutCondition(
		v1alpha1.RolloutTrafficRoutingApplied, corev1.ConditionFalse, conditions.TrafficRoutingErrorReason, "routing failed"))

	ctx := &rolloutContext{rollout: ro}
	newStatus := ro.Status.DeepCopy()
	ctx.mergeStageConditions(newStatus)
	assert.Nil(t, conditions.GetRolloutCondition(*newStatus, v1alpha1.RolloutTrafficRoutingApplied),
		"TrafficRoutingApplied must not apply to blue-green")

	ctx.markStageSucceeded(v1alpha1.RolloutServicesReconciled)
	ctx.mergeStageConditions(newStatus)
	servicesCond := conditions.GetRolloutCondition(*newStatus, v1alpha1.RolloutServicesReconciled)
	assert.NotNil(t, servicesCond)
	assert.Equal(t, corev1.ConditionTrue, servicesCond.Status,
		"ServicesReconciled must recover once blue-green service stages succeed")
}

func TestConditionNoChurnOnRepeatedError(t *testing.T) {
	status := v1alpha1.RolloutStatus{}
	cond := *conditions.NewRolloutCondition(v1alpha1.RolloutTrafficRoutingApplied, corev1.ConditionFalse, conditions.TrafficRoutingErrorReason, "same error")
	firstTransition := cond.LastTransitionTime
	conditions.SetRolloutCondition(&status, cond)

	repeated := *conditions.NewRolloutCondition(v1alpha1.RolloutTrafficRoutingApplied, corev1.ConditionFalse, conditions.TrafficRoutingErrorReason, "same error with different suffix")
	updated := conditions.SetRolloutCondition(&status, repeated)
	assert.False(t, updated)
	persisted := conditions.GetRolloutCondition(status, v1alpha1.RolloutTrafficRoutingApplied)
	assert.Equal(t, firstTransition, persisted.LastTransitionTime)
}
