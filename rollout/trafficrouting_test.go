package rollout

import (
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/apisix"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/dynamic/dynamiclister"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/mocks"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/alb"
	apisixMocks "github.com/argoproj/argo-rollouts/rollout/trafficrouting/apisix/mocks"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/appmesh"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/istio"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/nginx"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/smi"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/traefik"
	traefikMocks "github.com/argoproj/argo-rollouts/rollout/trafficrouting/traefik/mocks"
	testutil "github.com/argoproj/argo-rollouts/test/util"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

// newFakeTrafficRoutingReconciler returns a fake TrafficRoutingReconciler with mocked success return values
func newFakeSingleTrafficRoutingReconciler() *mocks.TrafficRoutingReconciler {
	trafficRoutingReconciler := mocks.TrafficRoutingReconciler{}
	trafficRoutingReconciler.On("Type").Return("fake")
	trafficRoutingReconciler.On("SetWeight", mock.Anything, mock.Anything).Return(nil)
	trafficRoutingReconciler.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	trafficRoutingReconciler.On("SetMirrorRoute", mock.Anything, mock.Anything).Return(nil)
	trafficRoutingReconciler.On("VerifyWeight", mock.Anything).Return(pointer.BoolPtr(true), nil)
	trafficRoutingReconciler.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	trafficRoutingReconciler.On("RemoveManagedRoutes", mock.Anything, mock.Anything).Return(nil)
	return &trafficRoutingReconciler
}

// newUnmockedFakeTrafficRoutingReconciler returns a fake TrafficRoutingReconciler with unmocked
// methods (except Type() mocked)
func newUnmockedFakeTrafficRoutingReconciler() *mocks.TrafficRoutingReconciler {
	trafficRoutingReconciler := mocks.TrafficRoutingReconciler{}
	trafficRoutingReconciler.On("Type").Return("fake")
	return &trafficRoutingReconciler
}

func newTrafficWeightFixture(t *testing.T) (*fixture, *v1alpha1.Rollout) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(10),
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
	r2.Spec.Strategy.Canary.CanaryService = "canary"
	r2.Spec.Strategy.Canary.StableService = "stable"

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	canarySelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	stableSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	canarySvc := newService("canary", 80, canarySelector, r2)
	stableSvc := newService("stable", 80, stableSelector, r2)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, false)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	return f, r2
}

func TestReconcileTrafficRoutingSetWeightErr(t *testing.T) {
	f, ro := newTrafficWeightFixture(t)
	defer f.Close()
	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(errors.New("Error message"))
	f.runExpectError(getKey(ro, t), true)
}

// verify error is not returned when VerifyWeight returns error (so that we can continue reconciling)
func TestReconcileTrafficRoutingVerifyWeightErr(t *testing.T) {
	f, ro := newTrafficWeightFixture(t)
	defer f.Close()
	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(pointer.BoolPtr(false), errors.New("Error message"))
	f.expectPatchRolloutAction(ro)
	f.run(getKey(ro, t))
}

// verify we requeue when VerifyWeight returns false
func TestReconcileTrafficRoutingVerifyWeightFalse(t *testing.T) {
	f, ro := newTrafficWeightFixture(t)
	defer f.Close()
	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(pointer.BoolPtr(false), nil)
	c, i, k8sI := f.newController(noResyncPeriodFunc)
	enqueued := false
	c.enqueueRolloutAfter = func(obj interface{}, duration time.Duration) {
		enqueued = true
	}
	f.expectPatchRolloutAction(ro)
	f.runController(getKey(ro, t), true, false, c, i, k8sI)
	assert.True(t, enqueued)
}

func TestRolloutUseDesiredWeight(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(10),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
	r2.Spec.Strategy.Canary.CanaryService = "canary"
	r2.Spec.Strategy.Canary.StableService = "stable"

	progressingCondition, _ := newProgressingCondition(conditions.RolloutPausedReason, r2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	pausedCondition, _ := newPausedCondition(true)
	conditions.SetRolloutCondition(&r2.Status, pausedCondition)

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	canarySelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	stableSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	canarySvc := newService("canary", 80, canarySelector, r2)
	stableSvc := newService("stable", 80, stableSelector, r2)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, true)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectPatchRolloutAction(r2)

	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(func(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
		// make sure SetWeight was called with correct value
		assert.Equal(t, int32(10), desiredWeight)
		return nil
	})
	f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(pointer.BoolPtr(true), nil)
	f.run(getKey(r2, t))
}

func TestRolloutUseDesiredWeight100(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(10),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(2), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
	r2.Spec.Strategy.Canary.CanaryService = "canary"
	r2.Spec.Strategy.Canary.StableService = "stable"

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 10, 10)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	canarySelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	stableSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	canarySvc := newService("canary", 80, canarySelector, r2)
	stableSvc := newService("stable", 80, stableSelector, r2)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, false)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectPatchRolloutAction(r2)

	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(func(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
		// make sure SetWeight was called with correct value
		assert.Equal(t, int32(100), desiredWeight)
		return nil
	})
	f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(pointer.BoolPtr(true), nil)
	f.run(getKey(r2, t))
}

func TestRolloutWithExperimentStep(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(10),
		},
		{
			Experiment: &v1alpha1.RolloutExperimentStep{
				Templates: []v1alpha1.RolloutExperimentTemplate{{
					Name:     "experiment-template",
					SpecRef:  "canary",
					Replicas: pointer.Int32Ptr(1),
					Weight:   pointer.Int32Ptr(5),
				}},
			},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
	r2.Spec.Strategy.Canary.CanaryService = "canary"
	r2.Spec.Strategy.Canary.StableService = "stable"

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	canarySelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	stableSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	canarySvc := newService("canary", 80, canarySelector, r2)
	stableSvc := newService("stable", 80, stableSelector, r2)
	ex, _ := GetExperimentFromTemplate(r1, rs1, rs2)
	ex.Status.TemplateStatuses = []v1alpha1.TemplateStatus{{
		Name:            "experiment-template",
		ServiceName:     "experiment-service",
		PodTemplateHash: rs2PodHash,
	}}
	r2.Status.Canary.CurrentExperiment = ex.Name

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, false)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2, ex)

	f.expectPatchRolloutAction(r2)

	t.Run("Experiment Running - WeightDestination created", func(t *testing.T) {
		ex.Status.Phase = v1alpha1.AnalysisPhaseRunning
		f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
		f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
		f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(func(desiredWeight int32, weightDestinations ...v1alpha1.WeightDestination) error {
			// make sure SetWeight was called with correct value
			assert.Equal(t, int32(10), desiredWeight)
			assert.Equal(t, int32(5), weightDestinations[0].Weight)
			assert.Equal(t, ex.Status.TemplateStatuses[0].ServiceName, weightDestinations[0].ServiceName)
			assert.Equal(t, ex.Status.TemplateStatuses[0].PodTemplateHash, weightDestinations[0].PodTemplateHash)
			return nil
		})
		f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
		f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(func(desiredWeight int32, weightDestinations ...v1alpha1.WeightDestination) error {
			assert.Equal(t, int32(10), desiredWeight)
			assert.Equal(t, int32(5), weightDestinations[0].Weight)
			assert.Equal(t, ex.Status.TemplateStatuses[0].ServiceName, weightDestinations[0].ServiceName)
			assert.Equal(t, ex.Status.TemplateStatuses[0].PodTemplateHash, weightDestinations[0].PodTemplateHash)
			return nil
		})
		f.run(getKey(r2, t))
	})

	t.Run("Experiment Pending - no WeightDestination created", func(t *testing.T) {
		ex.Status.Phase = v1alpha1.AnalysisPhasePending
		f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
		f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
		f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(func(desiredWeight int32, weightDestinations ...v1alpha1.WeightDestination) error {
			// make sure SetWeight was called with correct value
			assert.Equal(t, int32(10), desiredWeight)
			assert.Len(t, weightDestinations, 0)
			return nil
		})
		f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
		f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(func(desiredWeight int32, weightDestinations ...v1alpha1.WeightDestination) error {
			assert.Equal(t, int32(10), desiredWeight)
			assert.Len(t, weightDestinations, 0)
			return nil
		})
		f.run(getKey(r2, t))
	})
}

func TestRolloutUsePreviousSetWeight(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(10),
		},
		{
			SetWeight: pointer.Int32Ptr(20),
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
	r2.Spec.Strategy.Canary.CanaryService = "canary"
	r2.Spec.Strategy.Canary.StableService = "stable"

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	canarySelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	stableSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	canarySvc := newService("canary", 80, canarySelector, r2)
	stableSvc := newService("stable", 80, stableSelector, r2)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, false)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectUpdateReplicaSetAction(rs2)
	f.expectPatchRolloutAction(r2)

	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(func(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
		// make sure SetWeight was called with correct value
		assert.Equal(t, int32(10), desiredWeight)
		return nil
	})
	f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything, mock.Anything).Return(pointer.BoolPtr(true), nil)
	f.fakeTrafficRouting.On("error patching alb ingress", mock.Anything, mock.Anything).Return(true, nil)
	f.run(getKey(r2, t))
}

func TestRolloutUseDynamicWeightOnPromoteFull(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(5),
		},
		{
			SetWeight: pointer.Int32Ptr(25),
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
	r2.Spec.Strategy.Canary.CanaryService = "canary"
	r2.Spec.Strategy.Canary.StableService = "stable"

	rs1 := newReplicaSetWithStatus(r1, 5, 5)
	rs2 := newReplicaSetWithStatus(r2, 10, 5)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	canarySelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	stableSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	canarySvc := newService("canary", 80, canarySelector, r2)
	stableSvc := newService("stable", 80, stableSelector, r2)

	r2.Status.Canary.Weights = &v1alpha1.TrafficWeights{
		Canary: v1alpha1.WeightDestination{
			Weight:          5,
			ServiceName:     "canary",
			PodTemplateHash: rs2PodHash,
		},
		Stable: v1alpha1.WeightDestination{
			Weight:          95,
			ServiceName:     "stable",
			PodTemplateHash: rs1PodHash,
		},
	}

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 15, 0, 10, false)
	r2.Status.PromoteFull = true
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectUpdateReplicaSetAction(rs2)
	f.expectPatchRolloutAction(r2)

	t.Run("DynamicStableScale true", func(t *testing.T) {
		r2.Spec.Strategy.Canary.DynamicStableScale = true
		f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
		f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
		f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(func(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
			assert.Equal(t, int32(50), desiredWeight)
			return nil
		})
		f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
		f.fakeTrafficRouting.On("RemoveManagedRoutes", mock.Anything, mock.Anything).Return(nil)
		f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(pointer.BoolPtr(true), nil)
		f.run(getKey(r2, t))
	})

	t.Run("DynamicStableScale false", func(t *testing.T) {
		r2.Spec.Strategy.Canary.DynamicStableScale = false
		f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
		f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
		f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(func(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
			assert.Equal(t, int32(5), desiredWeight)
			return nil
		})
		f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
		f.fakeTrafficRouting.On("RemoveManagedRoutes", mock.Anything, mock.Anything).Return(nil)
		f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(pointer.BoolPtr(true), nil)
		f.run(getKey(r2, t))
	})
}

func TestRolloutSetWeightToZeroWhenFullyRolledOut(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(10),
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
	r1.Spec.Strategy.Canary.CanaryService = "canary"
	r1.Spec.Strategy.Canary.StableService = "stable"

	rs1 := newReplicaSetWithStatus(r1, 10, 10)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	canarySelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	stableSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	canarySvc := newService("canary", 80, canarySelector, r1)
	stableSvc := newService("stable", 80, stableSelector, r1)

	f.kubeobjects = append(f.kubeobjects, rs1, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	r1 = updateCanaryRolloutStatus(r1, rs1PodHash, 10, 0, 10, false)
	f.rolloutLister = append(f.rolloutLister, r1)
	f.objects = append(f.objects, r1)

	f.expectPatchRolloutAction(r1)

	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(func(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
		// make sure SetWeight was called with correct value
		assert.Equal(t, int32(0), desiredWeight)
		return nil
	})
	f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("RemoveManagedRoutes", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(pointer.BoolPtr(true), nil)
	f.run(getKey(r1, t))
}

func TestNewTrafficRoutingReconciler(t *testing.T) {
	rc := Controller{}
	dynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(testutil.NewFakeDynamicClient(), 0)
	vsvcGVR := istioutil.GetIstioVirtualServiceGVR()
	druleGVR := istioutil.GetIstioDestinationRuleGVR()
	rc.IstioController = &istio.IstioController{}
	rc.IstioController.VirtualServiceInformer = dynamicInformerFactory.ForResource(vsvcGVR).Informer()
	rc.IstioController.DestinationRuleInformer = dynamicInformerFactory.ForResource(druleGVR).Informer()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(10),
		},
	}

	{
		r := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
		roCtx := &rolloutContext{
			rollout: r,
			log:     logutil.WithRollout(r),
		}
		networkReconciler, err := rc.NewTrafficRoutingReconciler(roCtx)
		assert.Nil(t, err)
		assert.Nil(t, networkReconciler)
	}
	{
		r := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
		r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
		roCtx := &rolloutContext{
			rollout: r,
			log:     logutil.WithRollout(r),
		}
		networkReconciler, err := rc.NewTrafficRoutingReconciler(roCtx)
		assert.Nil(t, err)
		assert.Nil(t, networkReconciler)
	}
	{
		// Without istioVirtualServiceLister
		r := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
		r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			Istio: &v1alpha1.IstioTrafficRouting{},
		}
		roCtx := &rolloutContext{
			rollout: r,
			log:     logutil.WithRollout(r),
		}
		networkReconcilerList, err := rc.NewTrafficRoutingReconciler(roCtx)
		for _, networkReconciler := range networkReconcilerList {
			assert.Nil(t, err)
			assert.NotNil(t, networkReconciler)
			assert.Equal(t, istio.Type, networkReconciler.Type())
		}
	}
	{
		// With istioVirtualServiceLister
		stopCh := make(chan struct{})
		dynamicInformerFactory.Start(stopCh)
		dynamicInformerFactory.WaitForCacheSync(stopCh)
		close(stopCh)
		rc.IstioController.VirtualServiceLister = dynamiclister.New(rc.IstioController.VirtualServiceInformer.GetIndexer(), vsvcGVR)
		r := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
		r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			Istio: &v1alpha1.IstioTrafficRouting{},
		}
		roCtx := &rolloutContext{
			rollout: r,
			log:     logutil.WithRollout(r),
		}
		networkReconcilerList, err := rc.NewTrafficRoutingReconciler(roCtx)
		for _, networkReconciler := range networkReconcilerList {
			assert.Nil(t, err)
			assert.NotNil(t, networkReconciler)
			assert.Equal(t, istio.Type, networkReconciler.Type())
		}
	}
	{
		r := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
		r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			Nginx: &v1alpha1.NginxTrafficRouting{},
		}
		roCtx := &rolloutContext{
			rollout: r,
			log:     logutil.WithRollout(r),
		}
		networkReconcilerList, err := rc.NewTrafficRoutingReconciler(roCtx)
		for _, networkReconciler := range networkReconcilerList {
			assert.Nil(t, err)
			assert.NotNil(t, networkReconciler)
			assert.Equal(t, nginx.Type, networkReconciler.Type())
		}
	}
	{
		r := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
		r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			ALB: &v1alpha1.ALBTrafficRouting{},
		}
		roCtx := &rolloutContext{
			rollout: r,
			log:     logutil.WithRollout(r),
		}
		networkReconcilerList, err := rc.NewTrafficRoutingReconciler(roCtx)
		for _, networkReconciler := range networkReconcilerList {
			assert.Nil(t, err)
			assert.NotNil(t, networkReconciler)
			assert.Equal(t, alb.Type, networkReconciler.Type())
		}
	}
	{
		tsController := Controller{}
		r := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
		r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			SMI: &v1alpha1.SMITrafficRouting{},
		}
		roCtx := &rolloutContext{
			rollout: r,
			log:     logutil.WithRollout(r),
		}
		networkReconcilerList, err := tsController.NewTrafficRoutingReconciler(roCtx)
		for _, networkReconciler := range networkReconcilerList {
			assert.Nil(t, err)
			assert.NotNil(t, networkReconciler)
			assert.Equal(t, smi.Type, networkReconciler.Type())
		}
	}
	{
		tsController := Controller{}
		r := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
		r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			AppMesh: &v1alpha1.AppMeshTrafficRouting{},
		}
		roCtx := &rolloutContext{
			rollout: r,
			log:     logutil.WithRollout(r),
		}
		networkReconcilerList, err := tsController.NewTrafficRoutingReconciler(roCtx)
		for _, networkReconciler := range networkReconcilerList {
			assert.Nil(t, err)
			assert.NotNil(t, networkReconciler)
			assert.Equal(t, appmesh.Type, networkReconciler.Type())
		}
	}
	{
		tsController := Controller{
			reconcilerBase: reconcilerBase{
				dynamicclientset: &traefikMocks.FakeDynamicClient{},
			},
		}
		r := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
		r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			Traefik: &v1alpha1.TraefikTrafficRouting{
				WeightedTraefikServiceName: "traefik-service",
			},
		}
		roCtx := &rolloutContext{
			rollout: r,
			log:     logutil.WithRollout(r),
		}
		networkReconcilerList, err := tsController.NewTrafficRoutingReconciler(roCtx)
		for _, networkReconciler := range networkReconcilerList {
			assert.Nil(t, err)
			assert.NotNil(t, networkReconciler)
			assert.Equal(t, traefik.Type, networkReconciler.Type())
		}
	}
	{
		tsController := Controller{
			reconcilerBase: reconcilerBase{
				dynamicclientset: &apisixMocks.FakeDynamicClient{},
			},
		}
		r := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
		r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			Apisix: &v1alpha1.ApisixTrafficRouting{
				Route: &v1alpha1.ApisixRoute{
					Name: "apisix-route",
				},
			},
		}
		roCtx := &rolloutContext{
			rollout: r,
			log:     logutil.WithRollout(r),
		}
		networkReconcilerList, err := tsController.NewTrafficRoutingReconciler(roCtx)
		for _, networkReconciler := range networkReconcilerList {
			assert.Nil(t, err)
			assert.NotNil(t, networkReconciler)
			assert.Equal(t, apisix.Type, networkReconciler.Type())
		}
	}
	{
		// (2) Multiple Reconcilers (Nginx + SMI)
		tsController := Controller{}
		r := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
		r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			Nginx: &v1alpha1.NginxTrafficRouting{},
			SMI:   &v1alpha1.SMITrafficRouting{},
		}
		roCtx := &rolloutContext{
			rollout: r,
			log:     logutil.WithRollout(r),
		}
		networkReconcilerList, err := tsController.NewTrafficRoutingReconciler(roCtx)
		for position, networkReconciler := range networkReconcilerList {
			if position == 0 {
				assert.Equal(t, nginx.Type, networkReconciler.Type())
			} else if position == 1 {
				assert.Equal(t, smi.Type, networkReconciler.Type())
			}
			assert.Nil(t, err)
			assert.NotNil(t, networkReconciler)
		}
	}
	{
		// (3) Multiple Reconcilers (ALB + Nginx + SMI)
		tsController := Controller{}
		r := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
		r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			ALB:   &v1alpha1.ALBTrafficRouting{},
			Nginx: &v1alpha1.NginxTrafficRouting{},
			SMI:   &v1alpha1.SMITrafficRouting{},
		}
		roCtx := &rolloutContext{
			rollout: r,
			log:     logutil.WithRollout(r),
		}
		networkReconcilerList, err := tsController.NewTrafficRoutingReconciler(roCtx)
		for position, networkReconciler := range networkReconcilerList {
			if position == 0 {
				assert.Equal(t, nginx.Type, networkReconciler.Type())
			} else if position == 1 {
				assert.Equal(t, alb.Type, networkReconciler.Type())
			} else if position == 2 {
				assert.Equal(t, smi.Type, networkReconciler.Type())
			}
			assert.Nil(t, err)
			assert.NotNil(t, networkReconciler)
		}
	}
}

// Verifies with a canary using traffic routing, we add a scaledown delay to the old ReplicaSet
// after promoting desired ReplicaSet to stable.
// NOTE: As of v1.1, scale down delays are added to  ReplicaSets on *subsequent* reconciliations
// after the desired RS has been promoted to stable
func TestCanaryWithTrafficRoutingAddScaleDownDelay(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newCanaryRollout("foo", 1, nil, []v1alpha1.CanaryStep{{
		SetWeight: pointer.Int32(10),
	}}, pointer.Int32(0), intstr.FromInt(1), intstr.FromInt(1))
	r1.Spec.Strategy.Canary.CanaryService = "canary"
	r1.Spec.Strategy.Canary.StableService = "stable"
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		SMI: &v1alpha1.SMITrafficRouting{},
	}
	r2 := bumpVersion(r1)
	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	r2 = updateCanaryRolloutStatus(r2, rs2PodHash, 2, 1, 2, false)
	r2.Status.ObservedGeneration = strconv.Itoa(int(r2.Generation))
	r2.Status.CurrentStepIndex = pointer.Int32(1)
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)
	completedCondition, _ := newCompletedCondition(true)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)
	_, r2.Status.Canary.Weights = calculateWeightStatus(r2, rs2PodHash, rs2PodHash, 0)

	selector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	canarySvc := newService("canary", 80, selector, r2)
	stableSvc := newService("stable", 80, selector, r2)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1Patch := f.expectPatchReplicaSetAction(rs1) // set scale-down-deadline annotation
	f.run(getKey(r2, t))

	f.verifyPatchedReplicaSet(rs1Patch, 30)
}

// Verifies with a canary using traffic routing, we scale down old ReplicaSets which exceed our limit
// after promoting desired ReplicaSet to stable
func TestCanaryWithTrafficRoutingScaleDownLimit(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	inTheFuture := timeutil.MetaNow().Add(10 * time.Second).UTC().Format(time.RFC3339)

	r1 := newCanaryRollout("foo", 1, nil, []v1alpha1.CanaryStep{{
		SetWeight: pointer.Int32Ptr(10),
	}}, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(1))
	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs1.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inTheFuture
	r1.Spec.Strategy.Canary.ScaleDownDelayRevisionLimit = pointer.Int32Ptr(1)
	r1.Spec.Strategy.Canary.CanaryService = "canary"
	r1.Spec.Strategy.Canary.StableService = "stable"
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		SMI: &v1alpha1.SMITrafficRouting{},
	}

	r2 := bumpVersion(r1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs2.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inTheFuture

	r3 := bumpVersion(r2)
	rs3 := newReplicaSetWithStatus(r3, 1, 1)
	rs3PodHash := rs3.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	r3 = updateCanaryRolloutStatus(r3, rs3PodHash, 2, 2, 2, false)

	r3.Status.ObservedGeneration = strconv.Itoa(int(r3.Generation))
	canarySelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs3PodHash}
	stableSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs3PodHash}
	canarySvc := newService("canary", 80, canarySelector, r3)
	stableSvc := newService("stable", 80, stableSelector, r3)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, rs3, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2, rs3)
	f.rolloutLister = append(f.rolloutLister, r3)
	f.objects = append(f.objects, r3)

	rs1ScaleDownIndex := f.expectUpdateReplicaSetAction(rs1) // scale down ReplicaSet
	_ = f.expectPatchRolloutAction(r3)                       // updates the rollout status
	f.run(getKey(r3, t))

	rs1Updated := f.getUpdatedReplicaSet(rs1ScaleDownIndex)
	assert.Equal(t, int32(0), *rs1Updated.Spec.Replicas)
	_, ok := rs1Updated.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey]
	assert.False(t, ok, "annotation not removed")
}

// TestDynamicScalingDontIncreaseWeightWhenAborted verifies we don't increase the traffic weight if
// we are aborted, using dynamic scaling, and available stable replicas is less than desired
func TestDynamicScalingDontIncreaseWeightWhenAborted(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(50),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 5, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(1))
	r1.Spec.Strategy.Canary.DynamicStableScale = true
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		SMI: &v1alpha1.SMITrafficRouting{},
	}
	r1.Spec.Strategy.Canary.CanaryService = "canary"
	r1.Spec.Strategy.Canary.StableService = "stable"
	r1.Status.ReadyReplicas = 4
	r1.Status.AvailableReplicas = 4
	r1.Status.Abort = true
	r1.Status.AbortedAt = &metav1.Time{Time: time.Now().Add(-1 * time.Minute)}
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 5, 4) // have less available than desired to test calculation
	rs2 := newReplicaSetWithStatus(r2, 0, 0)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	canarySelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	stableSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	canarySvc := newService("canary", 80, canarySelector, r1)
	stableSvc := newService("stable", 80, stableSelector, r1)
	r2.Status.StableRS = rs1PodHash
	r2.Status.Canary.Weights = &v1alpha1.TrafficWeights{
		Canary: v1alpha1.WeightDestination{
			Weight:          0,
			ServiceName:     "canary",
			PodTemplateHash: rs2PodHash,
		},
		Stable: v1alpha1.WeightDestination{
			Weight:          100,
			ServiceName:     "stable",
			PodTemplateHash: rs1PodHash,
		},
	}

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectPatchRolloutAction(r2)
	f.expectPatchServiceAction(canarySvc, rs1PodHash)

	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(func(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
		// make sure SetWeight was called with correct value
		assert.Equal(t, int32(0), desiredWeight)
		return nil
	})
	f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("RemoveManagedRoutes", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(pointer.BoolPtr(true), nil)
	f.run(getKey(r1, t))
}

// TestDynamicScalingDecreaseWeightAccordingToStableAvailabilityWhenAborted verifies we decrease the weight
// to the canary depending on the availability of the stable ReplicaSet when aborting
func TestDynamicScalingDecreaseWeightAccordingToStableAvailabilityWhenAborted(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(50),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 5, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(1))
	r1.Spec.Strategy.Canary.DynamicStableScale = true
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		SMI: &v1alpha1.SMITrafficRouting{},
	}
	r1.Spec.Strategy.Canary.CanaryService = "canary"
	r1.Spec.Strategy.Canary.StableService = "stable"
	r1.Status.ReadyReplicas = 5
	r1.Status.AvailableReplicas = 5
	r1.Status.Abort = true
	r1.Status.AbortedAt = &metav1.Time{Time: time.Now().Add(-1 * time.Minute)}
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 5, 1)
	rs2 := newReplicaSetWithStatus(r2, 4, 4)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	canarySelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	stableSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	canarySvc := newService("canary", 80, canarySelector, r1)
	stableSvc := newService("stable", 80, stableSelector, r1)
	r2.Status.StableRS = rs1PodHash
	r2.Status.Canary.Weights = &v1alpha1.TrafficWeights{
		Canary: v1alpha1.WeightDestination{
			Weight:          100,
			ServiceName:     "canary",
			PodTemplateHash: rs2PodHash,
		},
		Stable: v1alpha1.WeightDestination{
			Weight:          0,
			ServiceName:     "stable",
			PodTemplateHash: rs1PodHash,
		},
	}

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectPatchRolloutAction(r2)

	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(func(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
		// make sure SetWeight was called with correct value
		assert.Equal(t, int32(80), desiredWeight)
		return nil
	})
	f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("RemoveManagedRoutes", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(pointer.BoolPtr(true), nil)
	f.run(getKey(r1, t))
}

// TestDynamicScalingDecreaseWeightAccordingToStableAvailabilityWhenAbortedAndResetService verifies we decrease the weight
// to the canary depending on the availability of the stable ReplicaSet when aborting and also that at the end of the abort
// we reset the canary service selectors back to the stable service
func TestDynamicScalingDecreaseWeightAccordingToStableAvailabilityWhenAbortedAndResetService(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(50),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 5, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(1))
	r1.Spec.Strategy.Canary.DynamicStableScale = true
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		SMI: &v1alpha1.SMITrafficRouting{},
	}
	r1.Spec.Strategy.Canary.CanaryService = "canary"
	r1.Spec.Strategy.Canary.StableService = "stable"
	r1.Status.ReadyReplicas = 5
	r1.Status.AvailableReplicas = 5
	r1.Status.Abort = true
	r1.Status.AbortedAt = &metav1.Time{Time: time.Now().Add(-1 * time.Minute)}
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 5, 5)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	canarySelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	stableSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	canarySvc := newService("canary", 80, canarySelector, r1)
	stableSvc := newService("stable", 80, stableSelector, r1)
	r2.Status.StableRS = rs1PodHash
	r2.Status.Canary.Weights = &v1alpha1.TrafficWeights{
		Canary: v1alpha1.WeightDestination{
			Weight:          20,
			ServiceName:     "canary",
			PodTemplateHash: rs2PodHash,
		},
		Stable: v1alpha1.WeightDestination{
			Weight:          80,
			ServiceName:     "stable",
			PodTemplateHash: rs1PodHash,
		},
	}

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectPatchRolloutAction(r2)
	f.expectPatchServiceAction(canarySvc, rs1PodHash)

	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(func(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
		// make sure SetWeight was called with correct value
		assert.Equal(t, int32(0), desiredWeight)
		return nil
	})
	f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("RemoveManagedRoutes", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(pointer.BoolPtr(true), nil)
	f.run(getKey(r1, t))
}

func TestRolloutReplicaIsAvailableAndGenerationNotBeModifiedShouldModifyVirtualServiceSHeaderRoute(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	steps := []v1alpha1.CanaryStep{
		{
			SetCanaryScale: &v1alpha1.SetCanaryScale{
				Replicas: pointer.Int32(1),
			},
		},
		{
			SetHeaderRoute: &v1alpha1.SetHeaderRoute{
				Name: "test-header",
				Match: []v1alpha1.HeaderRoutingMatch{
					{
						HeaderName: "test",
						HeaderValue: &v1alpha1.StringMatch{
							Prefix: "test",
						},
					},
				},
			},
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32(1), intstr.FromInt(1), intstr.FromInt(1))
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		Istio: &v1alpha1.IstioTrafficRouting{
			VirtualService: &v1alpha1.IstioVirtualService{
				Name: "test",
				Routes: []string{
					"primary",
				},
			},
			DestinationRule: &v1alpha1.IstioDestinationRule{
				Name:             "test",
				StableSubsetName: "stable",
				CanarySubsetName: "canary",
			},
		},
		ManagedRoutes: []v1alpha1.MangedRoutes{
			{
				Name: "test-header",
			},
		},
	}
	r1.Spec.WorkloadRef = &v1alpha1.ObjectRef{
		Name:       "test",
		APIVersion: "apps/v1",
		Kind:       "Deployment",
	}
	r1.Spec.SelectorResolvedFromRef = true
	r1.Spec.TemplateResolvedFromRef = true
	r2 := bumpVersion(r1)

	// if set WorkloadRef it does not change the generation
	r2.ObjectMeta.Generation = r2.ObjectMeta.Generation - 1

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	canarySelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	stableSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	canarySvc := newService("canary", 80, canarySelector, r1)
	stableSvc := newService("stable", 80, stableSelector, r1)
	r2.Status.StableRS = rs1PodHash
	r2.Status.Canary.Weights = &v1alpha1.TrafficWeights{
		Canary: v1alpha1.WeightDestination{
			Weight:          0,
			ServiceName:     "canary",
			PodTemplateHash: rs2PodHash,
		},
		Stable: v1alpha1.WeightDestination{
			Weight:          100,
			ServiceName:     "stable",
			PodTemplateHash: rs1PodHash,
		},
	}
	f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.expectPatchRolloutAction(r2)
	f.expectPatchReplicaSetAction(rs1)
	f.expectPatchReplicaSetAction(rs2)
	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("SetHeaderRoute", &v1alpha1.SetHeaderRoute{
		Name: "test-header",
		Match: []v1alpha1.HeaderRoutingMatch{
			{
				HeaderName: "test",
				HeaderValue: &v1alpha1.StringMatch{
					Prefix: "test",
				},
			},
		},
	}).Once().Return(nil)
	f.run(getKey(r1, t))
}

// This makes sure we don't set weight to zero if we are rolling back to stable with DynamicStableScale
func TestDontWeightToZeroWhenDynamicallyRollingBackToStable(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32(90),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32(1), intstr.FromInt(1), intstr.FromInt(1))
	r1.Spec.Strategy.Canary.DynamicStableScale = true
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		SMI: &v1alpha1.SMITrafficRouting{},
	}
	r1.Spec.Strategy.Canary.CanaryService = "canary"
	r1.Spec.Strategy.Canary.StableService = "stable"
	r1.Status.ReadyReplicas = 10
	r1.Status.AvailableReplicas = 10
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 9, 9)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	canarySelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	stableSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	canarySvc := newService("canary", 80, canarySelector, r1)
	stableSvc := newService("stable", 80, stableSelector, r1)

	// simulate rollback to stable
	r2.Spec = r1.Spec
	r2.Status.StableRS = rs1PodHash
	r2.Status.CurrentPodHash = rs1PodHash // will cause IsFullyPromoted() to be true
	r2.Status.Canary.Weights = &v1alpha1.TrafficWeights{
		Canary: v1alpha1.WeightDestination{
			Weight:          10,
			ServiceName:     "canary",
			PodTemplateHash: rs2PodHash,
		},
		Stable: v1alpha1.WeightDestination{
			Weight:          90,
			ServiceName:     "stable",
			PodTemplateHash: rs1PodHash,
		},
	}

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectUpdateReplicaSetAction(rs1)                 // Updates the revision annotation from 1 to 3 from func isScalingEvent
	f.expectUpdateRolloutAction(r2)                     // Update the rollout revision from 1 to 3
	scaleUpIndex := f.expectUpdateReplicaSetAction(rs1) // Scale The replicaset from 1 to 10 from func scaleReplicaSet
	f.expectPatchRolloutAction(r2)                      // Updates the rollout status from the scaling to 10 action

	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(func(canaryHash, stableHash string, additionalDestinations ...v1alpha1.WeightDestination) error {
		// make sure UpdateHash was called with previous desired hash (not current pod hash)
		if canaryHash != rs2PodHash {
			return fmt.Errorf("UpdateHash was called with canary hash: %s. Expected: %s", canaryHash, rs2PodHash)
		}
		if stableHash != rs1PodHash {
			return fmt.Errorf("UpdateHash was called with stable hash: %s. Expected: %s", canaryHash, rs1PodHash)
		}
		return nil

	})
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(func(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
		// make sure SetWeight was not changed
		if desiredWeight != 10 {
			return fmt.Errorf("SetWeight was called with unexpected weight: %d. Expected: 10", desiredWeight)
		}
		return nil
	})
	f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("RemoveManagedRoutes", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(pointer.BoolPtr(true), nil)
	f.run(getKey(r1, t))

	// Make sure we scale up stable ReplicaSet to 10
	rs1Updated := f.getUpdatedReplicaSet(scaleUpIndex)
	assert.Equal(t, int32(10), *rs1Updated.Spec.Replicas)
}
