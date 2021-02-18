package rollout

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/dynamic/dynamiclister"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/mocks"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/alb"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/istio"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/nginx"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/smi"
	testutil "github.com/argoproj/argo-rollouts/test/util"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

// newFakeTrafficRoutingReconciler returns a fake TrafficRoutingReconciler with mocked success return values
func newFakeTrafficRoutingReconciler() *mocks.TrafficRoutingReconciler {
	r := mocks.TrafficRoutingReconciler{}
	r.On("Type").Return("fake")
	r.On("SetWeight", mock.Anything).Return(nil)
	r.On("VerifyWeight", mock.Anything).Return(true, nil)
	r.On("UpdateHash", mock.Anything, mock.Anything).Return(nil)
	return &r
}

// newUnmockedFakeTrafficRoutingReconciler returns a fake TrafficRoutingReconciler with unmocked
// methods (except Type() mocked)
func newUnmockedFakeTrafficRoutingReconciler() *mocks.TrafficRoutingReconciler {
	r := mocks.TrafficRoutingReconciler{}
	r.On("Type").Return("fake")
	return &r
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
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything).Return(errors.New("Error message"))
	f.runExpectError(getKey(ro, t), true)
}

// verify error is returned when VerifyWeight returns error
func TestReconcileTrafficRoutingVerifyWeightErr(t *testing.T) {
	f, ro := newTrafficWeightFixture(t)
	defer f.Close()
	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(false, errors.New("Error message"))
	f.runExpectError(getKey(ro, t), true)
}

// verify we requeue when VerifyWeight returns false
func TestReconcileTrafficRoutingVerifyWeightFalse(t *testing.T) {
	f, ro := newTrafficWeightFixture(t)
	defer f.Close()
	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(false, nil)
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

	progressingCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, r2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

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
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything).Return(func(desiredWeight int32) error {
		// make sure SetWeight was called with correct value
		assert.Equal(t, int32(10), desiredWeight)
		return nil
	})
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(true, nil)
	f.run(getKey(r2, t))
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
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything).Return(func(desiredWeight int32) error {
		// make sure SetWeight was called with correct value
		assert.Equal(t, int32(10), desiredWeight)
		return nil
	})
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(true, nil)
	f.run(getKey(r2, t))
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
	f.fakeTrafficRouting.On("UpdateHash", mock.Anything, mock.Anything).Return(nil)
	f.fakeTrafficRouting.On("SetWeight", mock.Anything).Return(func(desiredWeight int32) error {
		// make sure SetWeight was called with correct value
		assert.Equal(t, int32(0), desiredWeight)
		return nil
	})
	f.fakeTrafficRouting.On("VerifyWeight", mock.Anything).Return(true, nil)
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
		networkReconciler, err := rc.NewTrafficRoutingReconciler(roCtx)
		assert.Nil(t, err)
		assert.NotNil(t, networkReconciler)
		assert.Equal(t, istio.Type, networkReconciler.Type())
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
		networkReconciler, err := rc.NewTrafficRoutingReconciler(roCtx)
		assert.Nil(t, err)
		assert.NotNil(t, networkReconciler)
		assert.Equal(t, istio.Type, networkReconciler.Type())
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
		networkReconciler, err := rc.NewTrafficRoutingReconciler(roCtx)
		assert.Nil(t, err)
		assert.NotNil(t, networkReconciler)
		assert.Equal(t, nginx.Type, networkReconciler.Type())
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
		networkReconciler, err := rc.NewTrafficRoutingReconciler(roCtx)
		assert.Nil(t, err)
		assert.NotNil(t, networkReconciler)
		assert.Equal(t, alb.Type, networkReconciler.Type())
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
		networkReconciler, err := tsController.NewTrafficRoutingReconciler(roCtx)
		assert.Nil(t, err)
		assert.NotNil(t, networkReconciler)
		assert.Equal(t, smi.Type, networkReconciler.Type())
	}
}
