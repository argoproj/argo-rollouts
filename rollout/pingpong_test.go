package rollout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

func newPingPongReuseFixture(t *testing.T) (*fixture, *v1alpha1.Rollout, *corev1.Service, []*appsv1.ReplicaSet) {
	t.Helper()
	f := newFixture(t)
	t.Cleanup(f.Close)
	r1 := newCanaryRollout("foo", 10, nil, []v1alpha1.CanaryStep{{SetWeight: ptr.To[int32](10)}}, ptr.To[int32](0), intstr.FromInt(1), intstr.FromInt(0))
	r1.Spec.Strategy.Canary.PingPong = &v1alpha1.PingPongSpec{PingService: "ping", PongService: "pong"}
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{Plugins: map[string]json.RawMessage{"test": json.RawMessage(`{}`)}}
	r2 := bumpVersion(r1)
	r3 := bumpVersion(r2)
	rss := []*appsv1.ReplicaSet{
		newReplicaSetWithStatus(r1, 10, 10),
		newReplicaSetWithStatus(r2, 5, 5),
		newReplicaSetWithStatus(r3, 0, 0),
	}
	r3.Status.StableRS = replicasetutil.GetPodTemplateHash(rss[0])
	r3.Status.Canary.StablePingPong = "ping"
	_, r3.Status.Canary.Weights = calculateWeightStatus(r3, replicasetutil.GetPodTemplateHash(rss[1]), r3.Status.StableRS, 50)
	ping := newService("ping", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: r3.Status.StableRS}, r3)
	pong := newService("pong", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: replicasetutil.GetPodTemplateHash(rss[1])}, r3)
	f.objects = append(f.objects, r3)
	f.rolloutLister = append(f.rolloutLister, r3)
	f.kubeobjects = append(f.kubeobjects, ping, pong)
	f.serviceLister = append(f.serviceLister, ping, pong)
	for _, rs := range rss {
		f.kubeobjects = append(f.kubeobjects, rs)
		f.replicaSetLister = append(f.replicaSetLister, rs)
	}
	return f, r3, pong, rss
}

func TestPingPongServiceReuseWaitsForTraffic(t *testing.T) {
	f, ro, canaryService, rss := newPingPongReuseFixture(t)
	router := newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting = router
	remove := router.On("RemoveManagedRoutes").Return(nil)
	reset := router.On("SetWeight", int32(0)).Return(nil).NotBefore(remove)
	verification := router.On("VerifyWeight", int32(0)).Return(ptr.To(false), nil).NotBefore(reset)
	router.On("UpdateHash", replicasetutil.GetPodTemplateHash(rss[2]), ro.Status.StableRS).Return(nil)
	ctrl, _, _ := f.newController(noResyncPeriodFunc)
	roCtx, err := ctrl.newRolloutContext(ro)
	require.NoError(t, err)
	require.NoError(t, roCtx.rolloutCanary())

	svc, err := f.kubeclient.CoreV1().Services(ro.Namespace).Get(context.Background(), canaryService.Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, replicasetutil.GetPodTemplateHash(rss[1]), svc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
	for i, want := range []int32{10, 5, 0} {
		rs, err := f.kubeclient.AppsV1().ReplicaSets(ro.Namespace).Get(context.Background(), rss[i].Name, metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, want, *rs.Spec.Replicas)
	}
	persisted, err := f.client.ArgoprojV1alpha1().Rollouts(ro.Namespace).Get(context.Background(), ro.Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, int32(0), persisted.Status.Canary.Weights.Canary.Weight)
	assert.Equal(t, replicasetutil.GetPodTemplateHash(rss[1]), persisted.Status.Canary.Weights.Canary.PodTemplateHash)
	assert.Equal(t, ptr.To(false), persisted.Status.Canary.Weights.Verified)
	assert.Equal(t, ro.Status.CurrentStepIndex, persisted.Status.CurrentStepIndex)

	// A new reconcile must keep verifying even though the recorded weight is now zero.
	roCtx, err = ctrl.newRolloutContext(persisted)
	require.NoError(t, err)
	require.NoError(t, roCtx.rolloutCanary())
	svc, err = f.kubeclient.CoreV1().Services(ro.Namespace).Get(context.Background(), canaryService.Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, replicasetutil.GetPodTemplateHash(rss[1]), svc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])

	verification.Unset()
	router.On("VerifyWeight", int32(0)).Return(ptr.To(true), nil)
	roCtx, err = ctrl.newRolloutContext(persisted)
	require.NoError(t, err)
	require.NoError(t, roCtx.reconcilePingAndPongService())
	svc, err = f.kubeclient.CoreV1().Services(ro.Namespace).Get(context.Background(), canaryService.Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, replicasetutil.GetPodTemplateHash(rss[2]), svc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
	newRS, err := f.kubeclient.AppsV1().ReplicaSets(ro.Namespace).Get(context.Background(), rss[2].Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Zero(t, *newRS.Spec.Replicas, "selector must switch before pods are created for readiness-gate injection")
	require.NoError(t, roCtx.rolloutCanary())
	newRS, err = f.kubeclient.AppsV1().ReplicaSets(ro.Namespace).Get(context.Background(), rss[2].Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, int32(1), *newRS.Spec.Replicas, "new pods can be created after the service switches")
	router.AssertExpectations(t)
}

func TestPingPongServiceReuseVerificationSupport(t *testing.T) {
	for _, tc := range []struct {
		name     string
		results  []*bool
		switches bool
	}{
		{name: "unsupported verification", results: []*bool{nil}, switches: true},
		{name: "all routers must verify", results: []*bool{ptr.To(false), ptr.To(true)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, ro, canaryService, rss := newPingPongReuseFixture(t)
			ctrl, _, _ := f.newController(noResyncPeriodFunc)
			var routers []trafficrouting.TrafficRoutingReconciler
			for _, result := range tc.results {
				router := newUnmockedFakeTrafficRoutingReconciler()
				router.On("RemoveManagedRoutes").Return(nil)
				router.On("SetWeight", int32(0)).Return(nil)
				router.On("VerifyWeight", int32(0)).Return(result, nil)
				routers = append(routers, router)
			}
			ctrl.newTrafficRoutingReconciler = func(*rolloutContext) ([]trafficrouting.TrafficRoutingReconciler, error) {
				return routers, nil
			}
			roCtx, err := ctrl.newRolloutContext(ro)
			require.NoError(t, err)
			require.NoError(t, roCtx.reconcilePingAndPongService())
			svc, err := f.kubeclient.CoreV1().Services(ro.Namespace).Get(context.Background(), canaryService.Name, metav1.GetOptions{})
			require.NoError(t, err)
			wantRS := rss[1]
			if tc.switches {
				wantRS = rss[2]
			}
			assert.Equal(t, replicasetutil.GetPodTemplateHash(wantRS), svc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
		})
	}
}

func TestPingPongServiceReuseWaitsBeforeFullPromotion(t *testing.T) {
	f, ro, _, rss := newPingPongReuseFixture(t)
	ro.Status.PromoteFull = true
	rss[2].Spec.Replicas = ptr.To[int32](10)
	rss[2].Status.Replicas = 10
	rss[2].Status.AvailableReplicas = 10
	router := newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting = router
	router.On("RemoveManagedRoutes").Return(nil)
	router.On("SetWeight", int32(0)).Return(nil)
	router.On("VerifyWeight", int32(0)).Return(ptr.To(false), nil)
	ctrl, _, _ := f.newController(noResyncPeriodFunc)
	roCtx, err := ctrl.newRolloutContext(ro)
	require.NoError(t, err)
	require.NoError(t, roCtx.rolloutCanary())
	persisted, err := f.client.ArgoprojV1alpha1().Rollouts(ro.Namespace).Get(context.Background(), ro.Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, ro.Status.StableRS, persisted.Status.StableRS)
	assert.Equal(t, ro.Status.Canary.StablePingPong, persisted.Status.Canary.StablePingPong)
	assert.Equal(t, ro.Status.CurrentStepIndex, persisted.Status.CurrentStepIndex)
}

func TestPingPongServiceReuseRestoresStableCapacity(t *testing.T) {
	f, ro, _, rss := newPingPongReuseFixture(t)
	ro.Spec.Strategy.Canary.DynamicStableScale = true
	rss[0].Spec.Replicas = ptr.To[int32](5)
	rss[0].Status.AvailableReplicas = 5
	router := newUnmockedFakeTrafficRoutingReconciler()
	f.fakeTrafficRouting = router
	ctrl, _, _ := f.newController(noResyncPeriodFunc)
	roCtx, err := ctrl.newRolloutContext(ro)
	require.NoError(t, err)
	require.NoError(t, roCtx.rolloutCanary())
	stable, err := f.kubeclient.AppsV1().ReplicaSets(ro.Namespace).Get(context.Background(), rss[0].Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, int32(10), *stable.Spec.Replicas)
	router.AssertNotCalled(t, "SetWeight", mock.Anything)
	assert.Equal(t, int32(50), roCtx.newStatus.Canary.Weights.Canary.Weight)

	// Scaling the stable RS is not sufficient until those pods become available.
	roCtx.stableRS = stable
	require.NoError(t, roCtx.reconcilePingAndPongService())
	router.AssertNotCalled(t, "SetWeight", mock.Anything)
}

func TestPingPongServiceReuseStillEvaluatesProgressDeadline(t *testing.T) {
	for _, tc := range []struct {
		name      string
		prepare   func(*v1alpha1.Rollout, *appsv1.ReplicaSet)
		verifyErr error
		paused    bool
	}{
		{name: "verification pending"},
		{name: "verification error", verifyErr: errors.New("weight lookup failed")},
		{name: "expired scale-down annotation", prepare: func(_ *v1alpha1.Rollout, rs *appsv1.ReplicaSet) {
			rs.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = timeutil.MetaNow().Add(-time.Hour).UTC().Format(time.RFC3339)
		}},
		{name: "pause step not started", prepare: func(ro *v1alpha1.Rollout, _ *appsv1.ReplicaSet) {
			ro.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{Pause: &v1alpha1.RolloutPause{}}}
			ro.Status.CurrentStepHash = conditions.ComputeStepHash(ro)
		}},
		{name: "user pause", paused: true, prepare: func(ro *v1alpha1.Rollout, _ *appsv1.ReplicaSet) {
			ro.Spec.Paused = true
		}},
		{name: "active pause condition", paused: true, prepare: func(ro *v1alpha1.Rollout, _ *appsv1.ReplicaSet) {
			ro.Status.ControllerPause = true
			ro.Status.PauseConditions = []v1alpha1.PauseCondition{{Reason: v1alpha1.PauseReasonInconclusiveAnalysis, StartTime: timeutil.MetaNow()}}
		}},
	} {
		for _, abort := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/abort=%t", tc.name, abort), func(t *testing.T) {
				f, ro, canaryService, rss := newPingPongReuseFixture(t)
				ro.Spec.ProgressDeadlineSeconds = ptr.To[int32](30)
				ro.Spec.ProgressDeadlineAbort = abort
				ro.Status.Replicas = 15
				ro.Status.AvailableReplicas = 15
				ro.Status.ReadyReplicas = 15
				ro.Status.HPAReplicas = 15
				cond := conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionTrue, conditions.ReplicaSetUpdatedReason, "waiting")
				cond.LastUpdateTime = metav1.NewTime(timeutil.MetaNow().Add(-time.Minute))
				ro.Status.Conditions = []v1alpha1.RolloutCondition{*cond}
				if tc.prepare != nil {
					tc.prepare(ro, rss[1])
				}
				router := newUnmockedFakeTrafficRoutingReconciler()
				f.fakeTrafficRouting = router
				router.On("RemoveManagedRoutes").Return(nil)
				router.On("SetWeight", int32(0)).Return(nil)
				router.On("VerifyWeight", int32(0)).Return(ptr.To(false), tc.verifyErr)
				ctrl, _, _ := f.newController(noResyncPeriodFunc)
				for i := 0; i < 2; i++ {
					roCtx, err := ctrl.newRolloutContext(ro)
					require.NoError(t, err)
					err = roCtx.rolloutCanary()
					if tc.verifyErr != nil {
						require.ErrorIs(t, err, tc.verifyErr)
					} else {
						require.NoError(t, err)
					}
					ro, err = f.client.ArgoprojV1alpha1().Rollouts(ro.Namespace).Get(context.Background(), ro.Name, metav1.GetOptions{})
					require.NoError(t, err)
				}
				assert.Equal(t, abort && !tc.paused, ro.Status.Abort)
				progressing := conditions.GetRolloutCondition(ro.Status, v1alpha1.RolloutProgressing)
				require.NotNil(t, progressing)
				switch {
				case tc.paused:
					assert.NotEqual(t, conditions.TimedOutReason, progressing.Reason)
				case abort:
					assert.Equal(t, conditions.RolloutAbortedReason, progressing.Reason)
				default:
					assert.Equal(t, conditions.TimedOutReason, progressing.Reason)
				}
				svc, err := f.kubeclient.CoreV1().Services(ro.Namespace).Get(context.Background(), canaryService.Name, metav1.GetOptions{})
				require.NoError(t, err)
				assert.Equal(t, canaryService.Spec.Selector, svc.Spec.Selector)
			})
		}
	}
}

func TestPingPongServiceReuseReleasesUnusedCapacity(t *testing.T) {
	for _, missingWeights := range []bool{false, true} {
		t.Run(fmt.Sprintf("missingWeights=%t", missingWeights), func(t *testing.T) {
			f, ro, canaryService, rss := newPingPongReuseFixture(t)
			ro.Spec.Strategy.Canary.DynamicStableScale = true
			ro.Spec.Strategy.Canary.TrafficRouting.ALB = &v1alpha1.ALBTrafficRouting{Ingress: "alb", RootService: "root", ServicePort: 80}
			rootSvc := newService("root", 80, nil, ro)
			ingress := newIngress("alb", canaryService, rootSvc)
			f.kubeobjects = append(f.kubeobjects, rootSvc, ingress)
			f.serviceLister = append(f.serviceLister, rootSvc)
			f.ingressLister = append(f.ingressLister, ingressutil.NewLegacyIngress(ingress))
			if missingWeights {
				ro.Status.Canary.Weights = nil
			} else {
				ro.Status.Canary.Weights.Canary.PodTemplateHash = ro.Status.CurrentPodHash
			}
			rss[0].Status.Replicas = 5
			rss[0].Status.ReadyReplicas = 5
			rss[0].Status.AvailableReplicas = 5
			// The selected canary must retain even its pending pods while traffic still reaches it.
			rss[1].Spec.Replicas = ptr.To[int32](8)
			oldRo := ro.DeepCopy()
			oldRo.Spec.Template.Spec.Containers[0].Image = "other:0"
			oldRo.Annotations[annotations.RevisionAnnotation] = "0"
			oldRS := newReplicaSetWithStatus(oldRo, 10, 10)
			oldRS.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = timeutil.MetaNow().Add(-time.Hour).UTC().Format(time.RFC3339)
			f.kubeobjects = append(f.kubeobjects, oldRS)
			f.replicaSetLister = append(f.replicaSetLister, oldRS)
			ctrl, _, kubeInformers := f.newController(noResyncPeriodFunc)
			for i := 0; i < 2; i++ {
				roCtx, err := ctrl.newRolloutContext(ro)
				require.NoError(t, err)
				require.NoError(t, roCtx.rolloutCanary())
				ro, err = f.client.ArgoprojV1alpha1().Rollouts(ro.Namespace).Get(context.Background(), ro.Name, metav1.GetOptions{})
				require.NoError(t, err)
				list, err := f.kubeclient.AppsV1().ReplicaSets(ro.Namespace).List(context.Background(), metav1.ListOptions{})
				require.NoError(t, err)
				for _, rs := range list.Items {
					require.NoError(t, kubeInformers.Apps().V1().ReplicaSets().Informer().GetIndexer().Update(rs.DeepCopy()))
				}
			}
			for i, rs := range append(rss, oldRS) {
				actual, err := f.kubeclient.AppsV1().ReplicaSets(ro.Namespace).Get(context.Background(), rs.Name, metav1.GetOptions{})
				require.NoError(t, err)
				assert.Equal(t, []int32{10, 8, 0, 0}[i], *actual.Spec.Replicas, rs.Name)
			}
			svc, err := f.kubeclient.CoreV1().Services(ro.Namespace).Get(context.Background(), canaryService.Name, metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, canaryService.Spec.Selector, svc.Spec.Selector)
		})
	}
}

func TestPingPongServiceReuseCannotBeBypassedByScaling(t *testing.T) {
	f, ro, canaryService, rss := newPingPongReuseFixture(t)
	ro.Spec.Replicas = ptr.To[int32](20)
	f.fakeTrafficRouting = newUnmockedFakeTrafficRoutingReconciler()
	ctrl, _, _ := f.newController(noResyncPeriodFunc)
	roCtx, err := ctrl.newRolloutContext(ro)
	require.NoError(t, err)
	require.NoError(t, roCtx.reconcile())
	stable, err := f.kubeclient.AppsV1().ReplicaSets(ro.Namespace).Get(context.Background(), rss[0].Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, int32(20), *stable.Spec.Replicas)
	newRS, err := f.kubeclient.AppsV1().ReplicaSets(ro.Namespace).Get(context.Background(), rss[2].Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Zero(t, *newRS.Spec.Replicas)
	svc, err := f.kubeclient.CoreV1().Services(ro.Namespace).Get(context.Background(), canaryService.Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, replicasetutil.GetPodTemplateHash(rss[1]), svc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
}
