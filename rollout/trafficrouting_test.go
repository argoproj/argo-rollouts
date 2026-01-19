package rollout

import (
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/apisix"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/dynamic/dynamiclister"
	"k8s.io/utils/pointer"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/mocks"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting"
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
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

// newFakeTrafficRoutingReconciler returns a fake TrafficRoutingReconciler with mocked success return values
func newFakeSingleTrafficRoutingReconciler() *mocks.TrafficRoutingReconciler {
	trafficRoutingReconciler := mocks.TrafficRoutingReconciler{}
	trafficRoutingReconciler.On("Type").Return("fake")
	trafficRoutingReconciler.On("SetWeight", mock.Anything, mock.Anything).Return(nil)
	trafficRoutingReconciler.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	trafficRoutingReconciler.On("SetMirrorRoute", mock.Anything, mock.Anything).Return(nil)
	trafficRoutingReconciler.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](true), nil)
	trafficRoutingReconciler.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	trafficRoutingReconciler.On("RemoveManagedRoutes", mock.Anything, mock.Anything).Return(nil)
	trafficRoutingReconciler.On("CanScaleDown", mock.Anything).Return((*bool)(nil), nil)
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
			SetWeight: ptr.To[int32](10),
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(1), intstr.FromInt(0))
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
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](false), errors.New("Error message"))
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
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](false), nil)
	c, i, k8sI := f.newController(noResyncPeriodFunc)
	enqueued := false
	c.enqueueRolloutAfter = func(obj any, duration time.Duration) {
		enqueued = true
	}
	f.expectPatchRolloutAction(ro)
	f.runController(getKey(ro, t), true, false, c, i, k8sI)
	assert.True(t, enqueued)
}

func TestReconcileTrafficRoutingVerifyWeightEndOfRollout(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: ptr.To[int32](10),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](2), intstr.FromInt(1), intstr.FromInt(0))
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

	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(func(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
		// make sure SetWeight was called with correct value
		assert.Equal(t, int32(100), desiredWeight)
		return nil
	})
	f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](false), nil)
	f.runExpectError(getKey(r2, t), true)
}

func TestRolloutUseDesiredWeight(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: ptr.To[int32](10),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
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
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](true), nil)
	f.run(getKey(r2, t))
}

func TestRolloutUseDesiredWeight100(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: ptr.To[int32](10),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](2), intstr.FromInt(1), intstr.FromInt(0))
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
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](true), nil)
	f.run(getKey(r2, t))
}

func TestRolloutWithExperimentStep(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: ptr.To[int32](10),
		},
		{
			Experiment: &v1alpha1.RolloutExperimentStep{
				Templates: []v1alpha1.RolloutExperimentTemplate{{
					Name:     "experiment-template",
					SpecRef:  "canary",
					Replicas: ptr.To[int32](1),
					Weight:   ptr.To[int32](5),
				},
					{
						Name:     "experiment-template-without-weight",
						SpecRef:  "stable",
						Replicas: ptr.To[int32](1),
					}},
			},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		SMI: &v1alpha1.SMITrafficRouting{},
	}
	r2.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{SMI: &v1alpha1.SMITrafficRouting{}}
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
	},
		{
			Name:            "experiment-template-without-weight",
			ServiceName:     "experiment-service-without-weight",
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
			assert.Len(t, weightDestinations, 1)
			assert.Equal(t, ex.Status.TemplateStatuses[0].ServiceName, weightDestinations[0].ServiceName)
			assert.Equal(t, ex.Status.TemplateStatuses[0].PodTemplateHash, weightDestinations[0].PodTemplateHash)
			return nil
		})
		f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
		f.fakeTrafficRouting.On("VerifyWeight", mock.Anything, mock.Anything).Return(ptr.To[bool](true), nil)
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
		f.fakeTrafficRouting.On("VerifyWeight", mock.Anything, mock.Anything).Return(ptr.To[bool](true), nil)
		f.run(getKey(r2, t))
	})
}

func TestRolloutUsePreviousSetWeight(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: ptr.To[int32](10),
		},
		{
			SetWeight: ptr.To[int32](20),
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
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
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything, mock.Anything).Return(ptr.To[bool](true), nil)
	f.fakeTrafficRouting.On("error patching alb ingress", mock.Anything, mock.Anything).Return(true, nil)
	f.run(getKey(r2, t))
}

func TestRolloutUseDynamicWeightOnPromoteFull(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: ptr.To[int32](5),
		},
		{
			SetWeight: ptr.To[int32](25),
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
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
		f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](true), nil)
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
		f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](true), nil)
		f.run(getKey(r2, t))
	})
}

func TestRolloutSetWeightToZeroWhenFullyRolledOut(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: ptr.To[int32](10),
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
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
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](true), nil)
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
			SetWeight: ptr.To[int32](10),
		},
	}

	{
		r := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
		roCtx := &rolloutContext{
			rollout: r,
			log:     logutil.WithRollout(r),
		}
		networkReconciler, err := rc.NewTrafficRoutingReconciler(roCtx)
		assert.Nil(t, err)
		assert.Nil(t, networkReconciler)
	}
	{
		r := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
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
		r := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
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
		r := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
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
		r := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
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
		r := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
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
		r := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
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
		r := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
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
		r := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
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
		r := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
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
		r := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
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
		r := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
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
		SetWeight: ptr.To[int32](10),
	}}, ptr.To[int32](0), intstr.FromInt(1), intstr.FromInt(1))
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
	r2.Status.CurrentStepIndex = ptr.To[int32](1)
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
		SetWeight: ptr.To[int32](10),
	}}, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(1))
	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs1.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inTheFuture
	r1.Spec.Strategy.Canary.ScaleDownDelayRevisionLimit = ptr.To[int32](1)
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
			SetWeight: ptr.To[int32](50),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 5, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(1))
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
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](true), nil)
	f.run(getKey(r1, t))
}

// TestDynamicScalingDecreaseWeightAccordingToStepWeightsAvailabilityWhenAborted verifies we decrease the weight
// to the canary depending on the weight in previous Step when aborting
func TestDynamicScalingDecreaseWeightAccordingToStepWeightsAvailabilityWhenAborted(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: ptr.To[int32](50),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 5, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(1))
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

	rs1 := newReplicaSetWithStatus(r1, 5, 3)
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
	f.expectUpdateReplicaSetAction(rs1)

	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(func(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
		// make sure SetWeight was called with correct value
		assert.Equal(t, int32(50), desiredWeight)
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
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](true), nil)
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
			SetWeight: ptr.To[int32](50),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 5, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(1))
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
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](true), nil)
	f.run(getKey(r1, t))
}

func TestRolloutReplicaIsAvailableAndGenerationNotBeModifiedShouldModifyVirtualServiceSHeaderRoute(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	steps := []v1alpha1.CanaryStep{
		{
			SetCanaryScale: &v1alpha1.SetCanaryScale{
				Replicas: ptr.To[int32](1),
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
	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(1))
	vs := istio.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: r1.Namespace,
		},
		Spec: istio.VirtualServiceSpec{
			HTTP: []istio.VirtualServiceHTTPRoute{{
				Name: "primary",
				Route: []istio.VirtualServiceRouteDestination{{
					Destination: istio.VirtualServiceDestination{
						Host: "stable",
						//Port: &istio.Port{
						//	Number: 80,
						//},
					},
					Weight: 100,
				}, {
					Destination: istio.VirtualServiceDestination{
						Host: "canary",
						//Port: &istio.Port{
						//	Number: 80,
						//},
					},
					Weight: 0,
				}},
			}},
		},
	}
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		Istio: &v1alpha1.IstioTrafficRouting{
			VirtualService: &v1alpha1.IstioVirtualService{
				Name: "test",
				Routes: []string{
					"primary",
				},
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
	r1.Spec.SelectorResolvedFromRef = false
	r1.Spec.TemplateResolvedFromRef = true
	r1.Spec.Strategy.Canary.CanaryService = "canary"
	r1.Spec.Strategy.Canary.StableService = "stable"
	r1.Spec.Selector = &metav1.LabelSelector{
		MatchLabels: map[string]string{"app": "test"},
	}
	r1.Labels = map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "testsha"}
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

	mapObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&vs)
	assert.Nil(t, err)

	unstructuredObj := &unstructured.Unstructured{Object: mapObj}
	unstructuredObj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   istioutil.GetIstioVirtualServiceGVR().Group,
		Version: istioutil.GetIstioVirtualServiceGVR().Version,
		Kind:    "VirtualService",
	})

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
	f.serviceLister = append(f.serviceLister, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2, unstructuredObj)
	f.expectUpdateRolloutAction(r2)
	f.expectUpdateRolloutStatusAction(r2)
	f.expectPatchRolloutAction(r2)
	f.expectCreateReplicaSetAction(rs2)
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
			SetWeight: ptr.To[int32](90),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(1))
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
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](true), nil)
	f.run(getKey(r1, t))

	// Make sure we scale up stable ReplicaSet to 10
	rs1Updated := f.getUpdatedReplicaSet(scaleUpIndex)
	assert.Equal(t, int32(10), *rs1Updated.Spec.Replicas)
}

// TestDontWeightOrHaveManagedRoutesDuringInterruptedUpdate builds off of TestCanaryDontScaleDownOldRsDuringInterruptedUpdate
// in canary_test when we scale down an intermediate V2 ReplicaSet when applying a V3 spec in the middle of updating.
// We want to make sure that traffic routing is cleared in both weight AND managed routes when the V2 rs is
// nil or has 0 available replicas.
func TestDontWeightOrHaveManagedRoutesDuringInterruptedUpdate(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetHeaderRoute: &v1alpha1.SetHeaderRoute{
				Name: "test-header",
				Match: []v1alpha1.HeaderRoutingMatch{
					{
						HeaderName: "test",
						HeaderValue: &v1alpha1.StringMatch{
							Exact: "test",
						},
					},
				},
			},
		},
		{
			SetWeight: ptr.To[int32](90),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 5, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		ALB: &v1alpha1.ALBTrafficRouting{
			Ingress: "test-ingress",
		},
		ManagedRoutes: []v1alpha1.MangedRoutes{
			{Name: "test-header"},
		},
	}

	r1.Spec.Strategy.Canary.StableService = "stable-svc"
	r1.Spec.Strategy.Canary.CanaryService = "canary-svc"
	r2 := bumpVersion(r1)
	r3 := bumpVersion(r2)

	stableSvc := newService("stable-svc", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: r1.Status.CurrentPodHash}, r1)
	canarySvc := newService("canary-svc", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: r3.Status.CurrentPodHash}, r3)
	r3.Status.StableRS = r1.Status.CurrentPodHash

	ingress := newIngress("test-ingress", canarySvc, stableSvc)
	ingress.Spec.Rules[0].HTTP.Paths[0].Backend.ServiceName = stableSvc.Name

	rs1 := newReplicaSetWithStatus(r1, 5, 5)
	rs2 := newReplicaSetWithStatus(r2, 5, 5)
	rs3 := newReplicaSetWithStatus(r3, 5, 0)
	r3.Status.Canary.Weights = &v1alpha1.TrafficWeights{
		Canary: v1alpha1.WeightDestination{
			PodTemplateHash: replicasetutil.GetPodTemplateHash(rs2),
		},
	}

	f.objects = append(f.objects, r3)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2, rs3, canarySvc, stableSvc, ingress)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2, rs3)
	f.serviceLister = append(f.serviceLister, canarySvc, stableSvc)
	f.ingressLister = append(f.ingressLister, ingressutil.NewLegacyIngress(ingress))

	f.expectPatchRolloutAction(r3)
	f.run(getKey(r3, t))

	r3.Status.Canary.Weights = &v1alpha1.TrafficWeights{
		Canary: v1alpha1.WeightDestination{
			PodTemplateHash: replicasetutil.GetPodTemplateHash(rs3),
		},
	}

	f.expectUpdateReplicaSetAction(rs3)
	f.run(getKey(r3, t))

	// Make sure that our weight is zero
	assert.Equal(t, int32(0), r3.Status.Canary.Weights.Canary.Weight)
	assert.Equal(t, replicasetutil.GetPodTemplateHash(rs3), r3.Status.Canary.Weights.Canary.PodTemplateHash)
	// Make sure that RemoveManagedRoutes was called
	f.fakeTrafficRouting.AssertCalled(t, "RemoveManagedRoutes", mock.Anything, mock.Anything)

}

// This test verifies that if we are shifting traffic to stable replicaset without the stable replicaset being available proportional to the weight, the traffic shouldn't be switched immediately to the stable replicaset.
func TestCheckReplicaSetAvailable(t *testing.T) {
	fix := newFixture(t)
	defer fix.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: ptr.To[int32](60),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}

	rollout1 := newCanaryRollout("test-rollout", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(1))
	rollout1.Spec.Strategy.Canary.DynamicStableScale = true
	rollout1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		SMI: &v1alpha1.SMITrafficRouting{},
	}
	rollout1.Spec.Strategy.Canary.CanaryService = "canary-service"
	rollout1.Spec.Strategy.Canary.StableService = "stable-service"
	rollout1.Status.ReadyReplicas = 10
	rollout1.Status.AvailableReplicas = 10

	rollout2 := bumpVersion(rollout1)

	replicaSet1 := newReplicaSetWithStatus(rollout1, 1, 1)
	replicaSet2 := newReplicaSetWithStatus(rollout2, 9, 9)

	replicaSet1Hash := replicaSet1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	replicaSet2Hash := replicaSet2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	canarySelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: replicaSet2Hash}
	stableSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: replicaSet1Hash}
	canarySvc := newService("canary-service", 80, canarySelector, rollout1)
	stableSvc := newService("stable-service", 80, stableSelector, rollout1)

	rollout2.Spec = rollout1.Spec
	rollout2.Status.StableRS = replicaSet1Hash
	rollout2.Status.CurrentPodHash = replicaSet1Hash
	rollout2.Status.Canary.Weights = &v1alpha1.TrafficWeights{
		Canary: v1alpha1.WeightDestination{
			Weight:          10,
			ServiceName:     "canary-service",
			PodTemplateHash: replicaSet2Hash,
		},
		Stable: v1alpha1.WeightDestination{
			Weight:          90,
			ServiceName:     "stable-service",
			PodTemplateHash: replicaSet1Hash,
		},
	}

	fix.kubeobjects = append(fix.kubeobjects, replicaSet1, replicaSet2, canarySvc, stableSvc)
	fix.replicaSetLister = append(fix.replicaSetLister, replicaSet1, replicaSet2)

	fix.rolloutLister = append(fix.rolloutLister, rollout2)
	fix.objects = append(fix.objects, rollout2)

	fix.expectUpdateReplicaSetAction(replicaSet1)
	fix.expectUpdateRolloutAction(rollout2)
	fix.expectUpdateReplicaSetAction(replicaSet1)
	fix.expectPatchRolloutAction(rollout2)
	fix.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	fix.fakeTrafficRouting.On("RemoveManagedRoutes", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	fix.run(getKey(rollout1, t))
}

func TestCheckReplicasAvailableWithReplicaProgressThreshold(t *testing.T) {
	tests := []struct {
		name              string
		availableReplicas int
		threshold         *v1alpha1.ReplicaProgressThreshold
		expectedResult    bool
		description       string
	}{
		{
			name:              "threshold met - 80% available with 80% threshold",
			availableReplicas: 4,
			threshold: &v1alpha1.ReplicaProgressThreshold{
				Type:  v1alpha1.ProgressTypePercentage,
				Value: 80,
			},
			expectedResult: true,
			description:    "Should return true when threshold is met (4/5 = 80% >= 80%)",
		},
		{
			name:              "threshold not met - 60% available with 80% threshold",
			availableReplicas: 3,
			threshold: &v1alpha1.ReplicaProgressThreshold{
				Type:  v1alpha1.ProgressTypePercentage,
				Value: 80,
			},
			expectedResult: false,
			description:    "Should return false when threshold is not met (3/5 = 60% < 80%)",
		},
		{
			name:              "nil threshold - requires 100% availability",
			availableReplicas: 4,
			threshold:         nil,
			expectedResult:    false,
			description:       "Should return false with nil threshold, requiring 100% availability (4/5 = 80%)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newCanaryRollout("foo", 10, nil, nil, ptr.To[int32](0), intstr.FromInt(1), intstr.FromInt(0))
			r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
				SMI: &v1alpha1.SMITrafficRouting{},
			}
			r.Spec.Strategy.Canary.ReplicaProgressThreshold = tt.threshold

			roCtx := &rolloutContext{
				rollout: r,
				log:     logutil.WithRollout(r),
			}

			// Create a ReplicaSet with specified available replicas out of 5
			// For 50% weight on 10 total replicas = 5 desired replicas
			rs := newReplicaSetWithStatus(r, 5, tt.availableReplicas)

			// Check if replicas are available for 50% weight
			result := roCtx.checkReplicasAvailable(rs, 50)

			assert.Equal(t, tt.expectedResult, result, tt.description)
		})
	}
}

// TestDynamicScalingAbortWithZeroReplicas verifies we handle zero replicas gracefully during abort
func TestDynamicScalingAbortWithZeroReplicas(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: ptr.To[int32](50),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 0, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(1))
	r1.Spec.Strategy.Canary.DynamicStableScale = true
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		SMI: &v1alpha1.SMITrafficRouting{},
	}
	r1.Spec.Strategy.Canary.CanaryService = "canary"
	r1.Spec.Strategy.Canary.StableService = "stable"
	r1.Status.Abort = true
	r1.Status.AbortedAt = &metav1.Time{Time: time.Now().Add(-1 * time.Minute)}
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 0, 0)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	canarySelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	stableSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	canarySvc := newService("canary", 80, canarySelector, r1)
	stableSvc := newService("stable", 80, stableSelector, r1)
	r2.Status.StableRS = rs1PodHash

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectPatchRolloutAction(r2)
	f.expectPatchServiceAction(canarySvc, rs1PodHash) // Expect canary service to switch to stable RS

	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(func(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
		// With zero replicas, we should fall back to immediate rollback (weight = 0)
		assert.Equal(t, int32(0), desiredWeight, "Should set weight to 0 when replicas is zero")
		return nil
	})
	f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("RemoveManagedRoutes", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](true), nil)

	// This should not panic or cause division by zero
	f.run(getKey(r1, t))
}

// TestNewRolloutResetsWeightBeforeUpdatingHash verifies that when a new rollout is triggered
// while one is already in progress with canary weight > 0, the VirtualService weight is reset
// to 0 BEFORE the DestinationRule subsets are updated to point to the new canary.
//
// This is critical because if the order is reversed (hash updated first, then weight reset),
// there's a window where traffic is routed to a canary subset that points to pods that don't
// exist yet (or have 0 replicas).
//
// Scenario from production logs showing the problematic order:
//
//	10:11:25.678 - Created ReplicaSet myservice-5c88b7f59c
//	10:11:25.912 - DestinationRule myservice subset updated (canary: 5c88b7f59c, stable: cff675f55)
//	10:11:25.921 - VirtualService `myservice` set to desiredWeight '0'
//	10:11:25.939 - Traffic weight updated from 5 to 0
//
// The correct order should be:
//  1. SetWeight to 0 (stop sending traffic to canary subset)
//  2. UpdateHash (update DestinationRule to point to new canary pods)
//  3. Scale up new canary pods
//  4. SetWeight to desired canary weight
func TestNewRolloutResetsWeightBeforeUpdatingHash(t *testing.T) {
	const (
		canaryService = "myservice-canary"
		stableService = "myservice-stable"
	)

	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{SetWeight: ptr.To[int32](5)},
		{Pause: &v1alpha1.RolloutPause{}},
		{SetWeight: ptr.To[int32](50)},
		{Pause: &v1alpha1.RolloutPause{}},
	}

	// r1 is the stable version
	r1 := newCanaryRollout("myservice", 10, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		// Use a generic traffic routing config (SMI) that uses the mock reconciler
		SMI: &v1alpha1.SMITrafficRouting{},
	}
	r1.Spec.Strategy.Canary.CanaryService = canaryService
	r1.Spec.Strategy.Canary.StableService = stableService

	// r2 is the "previous canary" - rollout in progress at 5% weight
	r2 := bumpVersion(r1)

	// r3 is the "new canary" - just triggered, should reset to 0%
	r3 := bumpVersion(r2)

	rs1 := newReplicaSetWithStatus(r1, 10, 10) // stable - fully available
	rs2 := newReplicaSetWithStatus(r2, 1, 1)   // previous canary - 1 replica at 5%
	rs3 := newReplicaSetWithStatus(r3, 0, 0)   // new canary - 0 replicas (just created)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs3PodHash := rs3.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	canarySelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs3PodHash}
	stableSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	canarySvc := newService(canaryService, 80, canarySelector, r3)
	stableSvc := newService(stableService, 80, stableSelector, r3)

	// Status reflects the previous rollout was in progress at 5% weight
	r3.Status.StableRS = rs1PodHash
	r3.Status.CurrentPodHash = rs3PodHash
	r3.Status.CurrentStepIndex = ptr.To[int32](0) // Reset to beginning
	r3.Status.Canary.Weights = &v1alpha1.TrafficWeights{
		Canary: v1alpha1.WeightDestination{
			Weight:          5, // Previous canary weight - this is the bug scenario
			ServiceName:     canaryService,
			PodTemplateHash: rs2PodHash, // Still pointing to previous canary!
		},
		Stable: v1alpha1.WeightDestination{
			Weight:          95,
			ServiceName:     stableService,
			PodTemplateHash: rs1PodHash,
		},
	}

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, rs3, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2, rs3)
	f.rolloutLister = append(f.rolloutLister, r3)
	f.objects = append(f.objects, r3)

	// Track the order of operations
	var operationOrder []string

	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()

	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(func(canaryHash, stableHash string, additionalDestinations ...v1alpha1.WeightDestination) error {
		operationOrder = append(operationOrder, fmt.Sprintf("UpdateHash(canary=%s)", canaryHash))
		t.Logf("UpdateHash called: canary=%s, stable=%s", canaryHash, stableHash)
		return nil
	})

	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(func(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
		operationOrder = append(operationOrder, fmt.Sprintf("SetWeight(%d)", desiredWeight))
		t.Logf("SetWeight called: weight=%d", desiredWeight)
		return nil
	})

	f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("RemoveManagedRoutes", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](true), nil)

	f.expectPatchRolloutAction(r3)
	f.expectUpdateReplicaSetAction(rs3) // Scale up new canary
	f.run(getKey(r3, t))

	t.Logf("Operation order: %v", operationOrder)
	t.Logf("New canary hash: %s, Previous canary hash: %s", rs3PodHash, rs2PodHash)

	// Find the indices of the key operations (first occurrence of each)
	firstSetWeightZeroIndex := -1
	updateHashNewCanaryIndex := -1

	for i, op := range operationOrder {
		if op == "SetWeight(0)" && firstSetWeightZeroIndex == -1 {
			firstSetWeightZeroIndex = i
		}
		if op == fmt.Sprintf("UpdateHash(canary=%s)", rs3PodHash) && updateHashNewCanaryIndex == -1 {
			updateHashNewCanaryIndex = i
		}
	}

	// CRITICAL ASSERTION: SetWeight(0) must happen BEFORE UpdateHash with new canary
	// If UpdateHash happens first, there's a window where 5% of traffic goes to
	// a canary subset pointing to pods that don't exist yet.
	// Note: SetWeight is idempotent, so it may be called multiple times (before and after UpdateHash).
	assert.NotEqual(t, -1, firstSetWeightZeroIndex,
		"SetWeight(0) should have been called to reset traffic before updating hash")
	assert.NotEqual(t, -1, updateHashNewCanaryIndex,
		"UpdateHash with new canary hash should have been called")

	if firstSetWeightZeroIndex != -1 && updateHashNewCanaryIndex != -1 {
		assert.Less(t, firstSetWeightZeroIndex, updateHashNewCanaryIndex,
			"SetWeight(0) must be called BEFORE UpdateHash with new canary hash to avoid "+
				"routing traffic to non-existent pods. Current order: %v", operationOrder)
	}
}

// TestTrafficRoutingErrorsWhenNewCanaryHasNoReplicas verifies that errors from
// SetWeight and RemoveManagedRoutes are properly propagated when the new canary
// has no available replicas.
func TestTrafficRoutingErrorsWhenNewCanaryHasNoReplicas(t *testing.T) {
	const (
		canaryService = "myservice-canary"
		stableService = "myservice-stable"
	)

	tests := []struct {
		name                   string
		previousCanaryWeights  *v1alpha1.TrafficWeights
		setWeightErr           error
		removeManagedRoutesErr error
		expectedCall           string
	}{
		{
			name: "SetWeight error when resetting canary weight",
			previousCanaryWeights: &v1alpha1.TrafficWeights{
				Canary: v1alpha1.WeightDestination{Weight: 5, ServiceName: canaryService, PodTemplateHash: "oldcanary"},
				Stable: v1alpha1.WeightDestination{Weight: 95, ServiceName: stableService},
			},
			setWeightErr:           fmt.Errorf("failed to set weight: connection refused"),
			removeManagedRoutesErr: nil,
			expectedCall:           "SetWeight",
		},
		{
			name:                   "RemoveManagedRoutes error",
			previousCanaryWeights:  nil,
			setWeightErr:           nil,
			removeManagedRoutesErr: fmt.Errorf("failed to remove managed routes: API server unavailable"),
			expectedCall:           "RemoveManagedRoutes",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			defer f.Close()

			steps := []v1alpha1.CanaryStep{
				{SetWeight: ptr.To[int32](5)},
				{Pause: &v1alpha1.RolloutPause{}},
			}

			r1 := newCanaryRollout("myservice", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(1), intstr.FromInt(0))
			r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
				SMI: &v1alpha1.SMITrafficRouting{},
			}
			r1.Spec.Strategy.Canary.CanaryService = canaryService
			r1.Spec.Strategy.Canary.StableService = stableService

			r2 := bumpVersion(r1)

			rs1 := newReplicaSetWithStatus(r1, 10, 10)
			rs2 := newReplicaSetWithStatus(r2, 0, 0) // New canary with 0 replicas

			rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
			rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

			canarySelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
			stableSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
			canarySvc := newService(canaryService, 80, canarySelector, r2)
			stableSvc := newService(stableService, 80, stableSelector, r2)

			r2.Status.StableRS = rs1PodHash
			r2.Status.CurrentPodHash = rs2PodHash
			r2.Status.CurrentStepIndex = ptr.To[int32](0)
			r2.Status.Canary.Weights = tc.previousCanaryWeights

			f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
			f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
			f.rolloutLister = append(f.rolloutLister, r2)
			f.objects = append(f.objects, r2)

			f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
			f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(tc.setWeightErr)
			f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
			f.fakeTrafficRouting.On("RemoveManagedRoutes").Return(tc.removeManagedRoutesErr)
			f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](true), nil)

			f.runExpectError(getKey(r2, t), true)

			f.fakeTrafficRouting.AssertCalled(t, tc.expectedCall, mock.Anything, mock.Anything)
		})
	}
}

// TestCanScaleDownRS tests the canScaleDownRS function which checks if traffic routers
// allow scale-down of a ReplicaSet
func TestCanScaleDownRS(t *testing.T) {
	tests := []struct {
		name                string
		hasTrafficRouting   bool
		canScaleDownReturn  *bool
		canScaleDownErr     error
		expectedResult      *bool
		expectedErrContains string
	}{
		{
			name:               "No traffic routing configured",
			hasTrafficRouting:  false,
			canScaleDownReturn: nil,
			canScaleDownErr:    nil,
			expectedResult:     nil,
		},
		{
			name:               "Traffic router returns not implemented (nil)",
			hasTrafficRouting:  true,
			canScaleDownReturn: nil,
			canScaleDownErr:    nil,
			expectedResult:     nil,
		},
		{
			name:               "Traffic router allows scale-down",
			hasTrafficRouting:  true,
			canScaleDownReturn: ptr.To[bool](true),
			canScaleDownErr:    nil,
			expectedResult:     ptr.To[bool](true),
		},
		{
			name:               "Traffic router blocks scale-down",
			hasTrafficRouting:  true,
			canScaleDownReturn: ptr.To[bool](false),
			canScaleDownErr:    nil,
			expectedResult:     ptr.To[bool](false),
		},
		{
			name:                "Traffic router returns error",
			hasTrafficRouting:   true,
			canScaleDownReturn:  nil,
			canScaleDownErr:     errors.New("connection error"),
			expectedResult:      nil,
			expectedErrContains: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			defer f.Close()

			steps := []v1alpha1.CanaryStep{
				{SetWeight: ptr.To[int32](10)},
			}
			r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(1), intstr.FromInt(0))

			if tc.hasTrafficRouting {
				r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
					SMI: &v1alpha1.SMITrafficRouting{},
				}
				r1.Spec.Strategy.Canary.CanaryService = "canary-svc"
				r1.Spec.Strategy.Canary.StableService = "stable-svc"
			}

			rs1 := newReplicaSetWithStatus(r1, 10, 10)
			rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

			r1.Status.StableRS = rs1PodHash

			f.kubeobjects = append(f.kubeobjects, rs1)
			f.replicaSetLister = append(f.replicaSetLister, rs1)
			f.rolloutLister = append(f.rolloutLister, r1)
			f.objects = append(f.objects, r1)

			c, i, k8sI := f.newController(noResyncPeriodFunc)
			f.runController(getKey(r1, t), true, false, c, i, k8sI)

			// Create the rollout context
			roCtx, err := c.newRolloutContext(r1)
			assert.NoError(t, err)

			if tc.hasTrafficRouting {
				f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
				f.fakeTrafficRouting.On("CanScaleDown", mock.Anything).Return(tc.canScaleDownReturn, tc.canScaleDownErr)

				roCtx.newTrafficRoutingReconciler = func(roCtx *rolloutContext) ([]trafficrouting.TrafficRoutingReconciler, error) {
					return []trafficrouting.TrafficRoutingReconciler{f.fakeTrafficRouting}, nil
				}
			}

			result, err := roCtx.canScaleDownRS(rs1PodHash)

			if tc.expectedErrContains != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedErrContains)
			} else {
				// Note: errors from CanScaleDown are logged as warnings but don't fail the operation
				if tc.canScaleDownErr != nil {
					// When there's an error but we continue, result should be nil
					assert.Nil(t, result)
				} else if tc.expectedResult == nil {
					assert.Nil(t, result)
				} else {
					assert.NotNil(t, result)
					assert.Equal(t, *tc.expectedResult, *result)
				}
			}
		})
	}
}

// TestCanScaleDownRSMultipleReconcilers tests canScaleDownRS with multiple traffic reconcilers
func TestCanScaleDownRSMultipleReconcilers(t *testing.T) {
	tests := []struct {
		name            string
		reconciler1Resp *bool
		reconciler2Resp *bool
		expectedResult  *bool
	}{
		{
			name:            "Both return not implemented",
			reconciler1Resp: nil,
			reconciler2Resp: nil,
			expectedResult:  nil,
		},
		{
			name:            "First allows, second not implemented",
			reconciler1Resp: ptr.To[bool](true),
			reconciler2Resp: nil,
			expectedResult:  ptr.To[bool](true),
		},
		{
			name:            "First not implemented, second allows",
			reconciler1Resp: nil,
			reconciler2Resp: ptr.To[bool](true),
			expectedResult:  ptr.To[bool](true),
		},
		{
			name:            "Both allow",
			reconciler1Resp: ptr.To[bool](true),
			reconciler2Resp: ptr.To[bool](true),
			expectedResult:  ptr.To[bool](true),
		},
		{
			name:            "First allows, second blocks",
			reconciler1Resp: ptr.To[bool](true),
			reconciler2Resp: ptr.To[bool](false),
			expectedResult:  ptr.To[bool](false),
		},
		{
			name:            "First blocks, second allows",
			reconciler1Resp: ptr.To[bool](false),
			reconciler2Resp: ptr.To[bool](true),
			expectedResult:  ptr.To[bool](false),
		},
		{
			name:            "First not implemented, second blocks",
			reconciler1Resp: nil,
			reconciler2Resp: ptr.To[bool](false),
			expectedResult:  ptr.To[bool](false),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			defer f.Close()

			steps := []v1alpha1.CanaryStep{
				{SetWeight: ptr.To[int32](10)},
			}
			r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(1), intstr.FromInt(0))
			r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
				SMI: &v1alpha1.SMITrafficRouting{},
			}
			r1.Spec.Strategy.Canary.CanaryService = "canary-svc"
			r1.Spec.Strategy.Canary.StableService = "stable-svc"

			rs1 := newReplicaSetWithStatus(r1, 10, 10)
			rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
			r1.Status.StableRS = rs1PodHash

			f.kubeobjects = append(f.kubeobjects, rs1)
			f.replicaSetLister = append(f.replicaSetLister, rs1)
			f.rolloutLister = append(f.rolloutLister, r1)
			f.objects = append(f.objects, r1)

			c, i, k8sI := f.newController(noResyncPeriodFunc)
			f.runController(getKey(r1, t), true, false, c, i, k8sI)

			roCtx, err := c.newRolloutContext(r1)
			assert.NoError(t, err)

			// Create two mock reconcilers
			reconciler1 := &mocks.TrafficRoutingReconciler{}
			reconciler1.On("Type").Return("reconciler1")
			reconciler1.On("CanScaleDown", mock.Anything).Return(tc.reconciler1Resp, nil)

			reconciler2 := &mocks.TrafficRoutingReconciler{}
			reconciler2.On("Type").Return("reconciler2")
			reconciler2.On("CanScaleDown", mock.Anything).Return(tc.reconciler2Resp, nil)

			roCtx.newTrafficRoutingReconciler = func(roCtx *rolloutContext) ([]trafficrouting.TrafficRoutingReconciler, error) {
				return []trafficrouting.TrafficRoutingReconciler{reconciler1, reconciler2}, nil
			}

			result, err := roCtx.canScaleDownRS(rs1PodHash)
			assert.NoError(t, err)

			if tc.expectedResult == nil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.Equal(t, *tc.expectedResult, *result)
			}
		})
	}
}

// TestCanScaleDownBlocksIntermediateRSScaleDown tests that when a traffic router blocks scale-down
// via CanScaleDown, the intermediate RS is not scaled down during an interrupted update (rainbow deployment)
func TestCanScaleDownBlocksIntermediateRSScaleDown(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: ptr.To[int32](100),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 5, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		SMI: &v1alpha1.SMITrafficRouting{},
	}
	r1.Spec.Strategy.Canary.StableService = "stable-svc"
	r1.Spec.Strategy.Canary.CanaryService = "canary-svc"
	r2 := bumpVersion(r1)
	r3 := bumpVersion(r2)

	stableSvc := newService("stable-svc", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: r1.Status.CurrentPodHash}, r1)
	canarySvc := newService("canary-svc", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: r3.Status.CurrentPodHash}, r3)
	r3.Status.StableRS = r1.Status.CurrentPodHash

	rs1 := newReplicaSetWithStatus(r1, 5, 5)
	rs2 := newReplicaSetWithStatus(r2, 5, 5) // This is the intermediate RS that should be blocked
	rs3 := newReplicaSetWithStatus(r3, 5, 5)

	f.objects = append(f.objects, r3)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2, rs3, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2, rs3)
	f.serviceLister = append(f.serviceLister, canarySvc, stableSvc)

	// Mock traffic router to block scale-down
	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](true), nil)
	f.fakeTrafficRouting.On("RemoveManagedRoutes", mock.Anything).Return(nil)
	// Block scale-down for the intermediate RS
	f.fakeTrafficRouting.On("CanScaleDown", mock.Anything).Return(ptr.To[bool](false), nil)

	f.expectPatchRolloutAction(r3)
	// We should NOT see an update to rs2 because CanScaleDown blocks it
	f.run(getKey(r3, t))

	// Verify rs2 was NOT scaled down (no ReplicaSet update action)
	for _, action := range f.kubeclient.Actions() {
		if action.GetVerb() == "update" && action.GetResource().Resource == "replicasets" {
			updateAction := action.(k8stesting.UpdateAction)
			rs := updateAction.GetObject().(*appsv1.ReplicaSet)
			// If rs2 was updated, it should still have 5 replicas (not 0)
			if rs.Name == rs2.Name {
				assert.Equal(t, int32(5), *rs.Spec.Replicas, "rs2 should not be scaled down when CanScaleDown returns false")
			}
		}
	}
}

// TestCanScaleDownAllowsIntermediateRSScaleDown tests that when a traffic router allows scale-down
// via CanScaleDown, the intermediate RS is scaled down during an interrupted update
func TestCanScaleDownAllowsIntermediateRSScaleDown(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: ptr.To[int32](100),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 5, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(0))
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		SMI: &v1alpha1.SMITrafficRouting{},
	}
	r1.Spec.Strategy.Canary.StableService = "stable-svc"
	r1.Spec.Strategy.Canary.CanaryService = "canary-svc"
	r2 := bumpVersion(r1)
	r3 := bumpVersion(r2)

	stableSvc := newService("stable-svc", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: r1.Status.CurrentPodHash}, r1)
	canarySvc := newService("canary-svc", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: r3.Status.CurrentPodHash}, r3)
	r3.Status.StableRS = r1.Status.CurrentPodHash

	rs1 := newReplicaSetWithStatus(r1, 5, 5)
	rs2 := newReplicaSetWithStatus(r2, 5, 5) // This is the intermediate RS that should be scaled down
	rs3 := newReplicaSetWithStatus(r3, 5, 5)

	f.objects = append(f.objects, r3)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2, rs3, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2, rs3)
	f.serviceLister = append(f.serviceLister, canarySvc, stableSvc)

	// Mock traffic router to allow scale-down
	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](true), nil)
	f.fakeTrafficRouting.On("RemoveManagedRoutes", mock.Anything).Return(nil)
	// Allow scale-down
	f.fakeTrafficRouting.On("CanScaleDown", mock.Anything).Return(ptr.To[bool](true), nil)

	f.expectPatchRolloutAction(r3)
	updatedRSIndex := f.expectUpdateReplicaSetAction(rs2)
	f.run(getKey(r3, t))

	// Verify rs2 WAS scaled down to 0
	updatedRs2 := f.getUpdatedReplicaSet(updatedRSIndex)
	assert.Equal(t, int32(0), *updatedRs2.Spec.Replicas)
}

// TestCanScaleDownBlocksScaleDownAtRevisionLimit tests that when exceeding scaleDownDelayRevisionLimit,
// the traffic router's CanScaleDown is still respected
func TestCanScaleDownBlocksScaleDownAtRevisionLimit(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{SetWeight: ptr.To[int32](100)},
	}
	r1 := newCanaryRollout("foo", 5, nil, steps, ptr.To[int32](0), intstr.FromInt(1), intstr.FromInt(0))
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		SMI: &v1alpha1.SMITrafficRouting{},
	}
	r1.Spec.Strategy.Canary.StableService = "stable-svc"
	r1.Spec.Strategy.Canary.CanaryService = "canary-svc"
	// Set revision limit to 1, so the second RS exceeds the limit
	r1.Spec.Strategy.Canary.ScaleDownDelayRevisionLimit = ptr.To[int32](1)

	r2 := bumpVersion(r1)
	r3 := bumpVersion(r2)

	stableSvc := newService("stable-svc", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: r3.Status.CurrentPodHash}, r3)
	canarySvc := newService("canary-svc", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: r3.Status.CurrentPodHash}, r3)

	rs1 := newReplicaSetWithStatus(r1, 5, 5)
	rs2 := newReplicaSetWithStatus(r2, 5, 5)
	rs3 := newReplicaSetWithStatus(r3, 5, 5)

	// Add scale-down deadline to both old RSs (they're waiting to be scaled down)
	now := timeutil.MetaNow()
	pastDeadline := now.Add(-1 * time.Minute).UTC().Format(time.RFC3339)
	rs1.Annotations = map[string]string{v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey: pastDeadline}
	rs2.Annotations = map[string]string{v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey: pastDeadline}

	// Mark rollout as fully promoted (stable = canary = rs3)
	r3.Status.StableRS = replicasetutil.GetPodTemplateHash(rs3)
	r3.Status.CurrentPodHash = replicasetutil.GetPodTemplateHash(rs3)

	f.objects = append(f.objects, r3)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2, rs3, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2, rs3)
	f.serviceLister = append(f.serviceLister, canarySvc, stableSvc)

	// Mock traffic router to block scale-down
	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetHeaderRoute", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(ptr.To[bool](true), nil)
	f.fakeTrafficRouting.On("RemoveManagedRoutes", mock.Anything).Return(nil)
	// Block scale-down
	f.fakeTrafficRouting.On("CanScaleDown", mock.Anything).Return(ptr.To[bool](false), nil)

	f.expectPatchRolloutAction(r3)
	// We should NOT see any RS scaled down because CanScaleDown blocks it
	f.run(getKey(r3, t))

	// Verify neither rs1 nor rs2 was scaled down
	for _, action := range f.kubeclient.Actions() {
		if action.GetVerb() == "update" && action.GetResource().Resource == "replicasets" {
			updateAction := action.(k8stesting.UpdateAction)
			rs := updateAction.GetObject().(*appsv1.ReplicaSet)
			if rs.Name == rs1.Name || rs.Name == rs2.Name {
				assert.Equal(t, int32(5), *rs.Spec.Replicas, "RS should not be scaled down when CanScaleDown returns false")
			}
		}
	}
}
