package promotion

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/controller/promotion/traffic"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutfake "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/utils/annotations"
)

func lifecycleFixture(t *testing.T, adoption bool) (*Controller, *v1alpha1.RolloutPromotion, Target) {
	t.Helper()
	ro := newCanaryRollout("test-ns", "example-worker", "green", "blue", 1, []interface{}{map[string]interface{}{"reason": "CanaryPauseStep"}})
	objects := []runtime.Object{}
	for _, hash := range []string{"blue", "green"} {
		rs := newReplicaSet("test-ns", "rs-"+hash, hash)
		rs.Spec.Replicas = int32Pointer(1)
		rs.Status.AvailableReplicas = 1
		pod := newPod("test-ns", "pod-"+hash, hash)
		pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
		objects = append(objects, rs, pod)
	}
	if adoption {
		require.NoError(t, unstructured.SetNestedField(ro.Object, "green", "status", "stableRS"))
		ro.SetAnnotations(map[string]string{
			annotations.LegacyAdoptionStateAnnotation:            annotations.LegacyAdoptionStatePending,
			annotations.LegacyAdoptionBaseSelectorAnnotation:     "app=example,component=data",
			annotations.LegacyAdoptionWeightAnnotationAnnotation: "example.com/weight",
			annotations.LegacyAdoptionActiveWeightAnnotation:     "active",
			annotations.LegacyAdoptionCandidateWeightAnnotation:  "preview",
			annotations.LegacyAdoptionLegacyWeightAnnotation:     "legacy",
			annotations.LegacyAdoptionInactiveWeightAnnotation:   "inactive",
		})
		legacy := newPod("test-ns", "legacy", "")
		delete(legacy.Labels, rolloutHashLabel)
		legacy.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
		objects = append(objects, legacy)
	}
	p := &v1alpha1.RolloutPromotion{ObjectMeta: metav1.ObjectMeta{Name: "promotion", Namespace: "test-ns", Generation: 1}}
	target := Target{Name: "example-worker", Strategy: "canary", Traffic: &v1alpha1.RolloutPromotionTraffic{Provider: traffic.PodAnnotation, Config: map[string]string{
		"annotationKey": "example.com/weight", "baseSelector": "app=example,component=data", "activeWeight": "active", "inactiveWeight": "inactive",
	}}}
	c := &Controller{kubeClient: kubefake.NewSimpleClientset(objects...), dynamicClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), ro), rolloutsClient: rolloutfake.NewSimpleClientset(p)}
	return c, p, target
}

func TestPromotionLifecycleAPIFailures(t *testing.T) {
	ctx := context.Background()
	for _, adoption := range []bool{false, true} {
		for _, operation := range []string{"promote", "capture", "rollback", "abort", "retry", "promoteFull"} {
			t.Run(fmt.Sprintf("adoption-%t/%s", adoption, operation), func(t *testing.T) {
				setup := func() (*Controller, *v1alpha1.RolloutPromotion, Target) {
					c, p, target := lifecycleFixture(t, adoption)
					if operation == "retry" {
						require.NoError(t, c.patchRolloutStatus(ctx, p.Namespace, target.Name, map[string]interface{}{"abort": true}))
					}
					if operation == "rollback" {
						spec := RequestSpec{RequestID: "release", Targets: []Target{target}, TrafficRollback: TrafficRollbackOptions{HistoryLimit: 10}}
						status, err := c.captureTrafficSnapshot(ctx, p, spec, map[string]interface{}{})
						require.NoError(t, err)
						p.Status.TrafficSnapshots = status["trafficSnapshots"].([]v1alpha1.RolloutPromotionTrafficSnapshot)
					}
					c.dynamicClient.(*dynamicfake.FakeDynamicClient).ClearActions()
					c.kubeClient.(*kubefake.Clientset).ClearActions()
					return c, p, target
				}
				call := func(c *Controller, p *v1alpha1.RolloutPromotion, target Target) error {
					var err error
					spec := RequestSpec{RequestID: "release", Action: operation, Mode: "sequential", Targets: []Target{target}, TrafficRollback: TrafficRollbackOptions{Mode: "previousSnapshot", HistoryLimit: 1}}
					switch operation {
					case "promote":
						_, err = c.promoteTarget(ctx, p.Namespace, target)
					case "capture":
						_, err = c.captureTrafficSnapshot(ctx, p, spec, map[string]interface{}{})
					case "rollback":
						_, err = c.executePreviousTrafficSnapshot(ctx, p, spec, map[string]interface{}{})
					default:
						_, err = c.controlTarget(ctx, p.Namespace, spec, target)
					}
					return err
				}
				c, p, target := setup()
				require.NoError(t, call(c, p, target))
				calls := len(c.dynamicClient.(*dynamicfake.FakeDynamicClient).Actions()) + len(c.kubeClient.(*kubefake.Clientset).Actions())
				for failAt := 1; failAt <= calls; failAt++ {
					t.Run(fmt.Sprintf("api-%d", failAt), func(t *testing.T) {
						c, p, target := setup()
						count := 0
						reactor := func(ktesting.Action) (bool, runtime.Object, error) {
							count++
							if count >= failAt {
								return true, nil, errors.New("injected outage")
							}
							return false, nil, nil
						}
						c.dynamicClient.(*dynamicfake.FakeDynamicClient).PrependReactor("*", "*", reactor)
						c.kubeClient.(*kubefake.Clientset).PrependReactor("*", "*", reactor)
						require.ErrorContains(t, call(c, p, target), "injected outage")
						for _, a := range c.kubeClient.(*kubefake.Clientset).Actions() {
							require.NotContains(t, []string{"create", "delete", "update"}, a.GetVerb())
						}
					})
				}
			})
		}
	}
}

func TestLegacyRollbackValidatesReadyPodsBeforeAnyTrafficWrite(t *testing.T) {
	ctx := context.Background()
	for _, scenario := range []string{"missing", "unready", "terminating"} {
		t.Run(scenario, func(t *testing.T) {
			c, p, target := lifecycleFixture(t, true)
			client := c.kubeClient.(*kubefake.Clientset)
			pod, err := client.CoreV1().Pods(p.Namespace).Get(ctx, "legacy", metav1.GetOptions{})
			require.NoError(t, err)
			switch scenario {
			case "missing":
				require.NoError(t, client.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), p.Namespace, pod.Name))
			case "unready":
				pod.Status.Conditions = nil
				require.NoError(t, client.Tracker().Update(corev1.SchemeGroupVersion.WithResource("pods"), pod, p.Namespace))
			case "terminating":
				now := metav1.Now()
				pod.DeletionTimestamp = &now
				require.NoError(t, client.Tracker().Update(corev1.SchemeGroupVersion.WithResource("pods"), pod, p.Namespace))
			}
			client.ClearActions()
			_, err = c.rollbackTrafficTarget(ctx, p.Namespace, target, legacyTrafficTarget)
			require.ErrorContains(t, err, "no Ready Pods")
			for _, a := range client.Actions() {
				require.Equal(t, "list", a.GetVerb())
			}
		})
	}
}

func TestPromoteStatusFallbackAndAnalysisPause(t *testing.T) {
	ctx := context.Background()
	c, p, target := lifecycleFixture(t, false)
	require.NoError(t, c.patchRolloutStatus(ctx, p.Namespace, target.Name, map[string]interface{}{
		"controllerPause": true,
		"canary":          map[string]interface{}{"currentStepAnalysisRunStatus": map[string]interface{}{"status": "Inconclusive"}},
	}))
	client := c.dynamicClient.(*dynamicfake.FakeDynamicClient)
	client.PrependReactor("patch", "rollouts", func(a ktesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() == "status" {
			return true, nil, errors.New("status subresource unavailable")
		}
		return false, nil, nil
	})
	state, err := c.promoteTarget(ctx, p.Namespace, target)
	require.NoError(t, err)
	require.Equal(t, int64(2), state["currentStepIndex"])
	require.Equal(t, 0, state["pauseConditions"])
	require.False(t, state["controllerPause"].(bool))
}

func TestExecuteModesAndControlResume(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"together", "sequential"} {
		for _, action := range []string{"promote", "abort", "retry", "promoteFull"} {
			for _, wait := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/wait-%t", mode, action, wait), func(t *testing.T) {
					c, p, target := lifecycleFixture(t, false)
					// No traffic provider is necessary for native rollout actions.
					target.Traffic = nil
					spec := RequestSpec{Action: action, Mode: mode, Targets: []Target{target}, Wait: WaitOptions{Enabled: wait, Attempts: 1, Seconds: 1}}
					if wait || action == "promoteFull" {
						require.NoError(t, c.patchRolloutStatus(ctx, p.Namespace, target.Name, map[string]interface{}{"stableRS": "green", "pauseConditions": nil, "currentStepIndex": int64(3)}))
					}
					if action == "retry" {
						require.NoError(t, c.patchRolloutStatus(ctx, p.Namespace, target.Name, map[string]interface{}{"abort": true}))
					}
					status, err := c.execute(ctx, p, spec, map[string]interface{}{})
					require.NoError(t, err)
					require.NotEmpty(t, status["targets"])
					// Recovery uses the committed checkpoint and must not repeat an action.
					p.Status.Targets = []v1alpha1.RolloutPromotionTargetStatus{{Name: target.Name, Namespace: p.Namespace, ActionApplied: true, BaselineCurrentPodHash: "green", ResolvedToRevision: 2}}
					client := c.dynamicClient.(*dynamicfake.FakeDynamicClient)
					client.ClearActions()
					_, err = c.execute(ctx, p, spec, map[string]interface{}{})
					require.NoError(t, err)
					for _, a := range client.Actions() {
						require.Equal(t, "get", a.GetVerb())
					}
				})
			}
		}
	}
}

func TestFinalWeightCompletionDoesNotSkipIntermediateManualPause(t *testing.T) {
	ctx := context.Background()
	for _, scenario := range []string{"final", "intermediate", "already-complete", "identity-changed", "patch-status-fails", "patch-spec-fails", "canceled", "observe-fails", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			c, p, target := lifecycleFixture(t, false)
			spec := RequestSpec{CanaryCompletionPolicy: v1alpha1.RolloutPromotionCanaryCompletionFullWeight, Wait: WaitOptions{Attempts: 1, Seconds: 1}}
			state := map[string]interface{}{"strategy": "canary", "currentStepIndex": int64(2), "stepCount": int64(3), "canaryWeight": int64(100), "pauseConditions": 1, "currentPodHash": "green"}
			wantError := false
			switch scenario {
			case "intermediate":
				state["currentStepIndex"] = int64(1)
				state["canaryWeight"] = int64(30)
			case "already-complete":
				state["promoted"] = true
			case "identity-changed":
				target.Readiness = &v1alpha1.RolloutPromotionReadiness{}
				target.ExpectedCurrentPodHash = "different"
				wantError = true
			case "patch-status-fails", "patch-spec-fails":
				c.dynamicClient.(*dynamicfake.FakeDynamicClient).PrependReactor("patch", "rollouts", func(a ktesting.Action) (bool, runtime.Object, error) {
					if (scenario == "patch-status-fails") == (a.GetSubresource() == "status") {
						return true, nil, errors.New("write failed")
					}
					return false, nil, nil
				})
				wantError = true
			case "canceled", "observe-fails", "timeout":
				state["pauseConditions"] = 0
				state["canaryWeight"] = int64(30)
				wantError = true
			}
			if scenario == "final" {
				require.NoError(t, c.patchRolloutStatus(ctx, p.Namespace, target.Name, map[string]interface{}{"stableRS": "green", "pauseConditions": nil, "currentStepIndex": int64(3)}))
			}
			if scenario == "observe-fails" {
				c.dynamicClient.(*dynamicfake.FakeDynamicClient).PrependReactor("get", "rollouts", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, errors.New("read failed") })
			}
			localCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			if scenario == "canceled" {
				cancel()
			}
			_, err := c.completeCanaryAfterFullWeight(localCtx, p, spec, target, state)
			if wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestWorkerRunAndEventFiltering(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watch.NewRaceFreeFake()
	informer := cache.NewSharedIndexInformer(&cache.ListWatch{
		ListFunc:  func(metav1.ListOptions) (runtime.Object, error) { return &v1alpha1.RolloutPromotionList{}, nil },
		WatchFunc: func(metav1.ListOptions) (watch.Interface, error) { return events, nil },
	}, &v1alpha1.RolloutPromotion{}, 0, cache.Indexers{})
	client := rolloutfake.NewSimpleClientset()
	c := NewController("test-ns", kubefake.NewSimpleClientset(), dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()), client, informer)
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, 1) }()
	require.Eventually(t, informer.HasSynced, 5*time.Second, time.Millisecond)
	p := &v1alpha1.RolloutPromotion{ObjectMeta: metav1.ObjectMeta{Name: "deleted", Namespace: "test-ns", ResourceVersion: "1", Generation: 1}}
	events.Add(p.DeepCopy())
	gets := func() int {
		count := 0
		for _, a := range client.Actions() {
			if a.GetVerb() == "get" {
				count++
			}
		}
		return count
	}
	require.Eventually(t, func() bool { return gets() == 1 }, 5*time.Second, time.Millisecond)
	p.ResourceVersion = "2"
	p.Generation++
	events.Modify(p.DeepCopy())
	require.Eventually(t, func() bool { return gets() == 2 }, 5*time.Second, time.Millisecond)
	p.ResourceVersion = "3"
	p.Annotations = map[string]string{annotations.TrafficRollbackRequestAnnotation: "restore"}
	events.Modify(p.DeepCopy())
	require.Eventually(t, func() bool { return gets() == 3 }, 5*time.Second, time.Millisecond)
	p.ResourceVersion = "4"
	now := metav1.Now()
	p.DeletionTimestamp = &now
	events.Modify(p.DeepCopy())
	require.Eventually(t, func() bool { return gets() == 4 }, 5*time.Second, time.Millisecond)
	c.enqueue(42)
	cancel()
	require.NoError(t, <-done)
}

func TestWorkerRetriesErrorsAndRejectsInvalidRequests(t *testing.T) {
	c, p := readinessFixture(t)
	client := c.rolloutsClient.(*rolloutfake.Clientset)
	fail := true
	client.PrependReactor("get", "rolloutpromotions", func(ktesting.Action) (bool, runtime.Object, error) {
		if fail {
			return true, nil, errors.New("transient API failure")
		}
		return false, nil, nil
	})
	c.enqueue(p)
	require.True(t, c.processNextWorkItem(context.Background()))
	require.Equal(t, 1, c.queue.NumRequeues("test-ns/demo"))
	fail = false
	// An invalid request is durable Failed, not an infinitely retried worker error.
	p.Spec.Mode = "invalid"
	_, err := c.promotionClient(p.Namespace).Update(context.Background(), p, metav1.UpdateOptions{})
	require.NoError(t, err)
	c.queue.Add("test-ns/demo")
	require.True(t, c.processNextWorkItem(context.Background()))
	live, err := c.promotionClient(p.Namespace).Get(context.Background(), p.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, PhaseFailed, live.Status.Phase)
	require.Equal(t, 0, c.queue.NumRequeues("test-ns/demo"))
	c.queue.ShutDown()
	for c.queue.Len() > 0 {
		require.True(t, c.processNextWorkItem(context.Background()))
	}
	require.False(t, c.processNextWorkItem(context.Background()))
}

func TestControlActionRecoveryObservations(t *testing.T) {
	for _, tc := range []struct {
		action   string
		state    map[string]interface{}
		observed bool
	}{
		{"abort", map[string]interface{}{"aborted": true, "strategy": "canary", "canaryWeight": int64(30)}, true},
		{"retry", map[string]interface{}{"aborted": false}, true},
		{"retry", map[string]interface{}{"aborted": true}, false},
		{"promoteFull", map[string]interface{}{"promoted": true}, true},
		{"promoteFull", map[string]interface{}{"currentPodHash": "green", "stableRS": "blue", "pauseConditions": 0}, true},
		{"promoteFull", map[string]interface{}{"currentPodHash": "green", "stableRS": "blue", "pauseConditions": 1}, false},
		{"rollback", map[string]interface{}{"currentPodHash": "blue"}, true},
		{"rollback", map[string]interface{}{"currentPodHash": "green"}, false},
		{"unsupported", map[string]interface{}{}, false},
	} {
		require.Equal(t, tc.observed, controlActionObservedAfterCommit(tc.action, "green", tc.state), "%s: %v", tc.action, tc.state)
	}
}
