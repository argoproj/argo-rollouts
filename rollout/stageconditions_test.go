package rollout

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
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
	f.run(getKey(ro, t))
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
	f.run(getKey(ro, t))
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

func TestConditionMessageTruncated(t *testing.T) {
	ctx := &rolloutContext{}
	longMsg := strings.Repeat("x", maxStageConditionMessageLen+10)
	ctx.setStageCondition(v1alpha1.RolloutActuationSucceeded, corev1.ConditionFalse, conditions.ActuationErrorReason, longMsg)
	cond := ctx.stageConditions[v1alpha1.RolloutActuationSucceeded]
	assert.Len(t, cond.Message, maxStageConditionMessageLen)
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
