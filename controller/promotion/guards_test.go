package promotion

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	rolloutfake "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"

	"github.com/argoproj/argo-rollouts/controller/promotion/traffic"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestInvalidRequestGuards(t *testing.T) {
	for name, change := range map[string]func(*v1alpha1.RolloutPromotionSpec){
		"action":          func(s *v1alpha1.RolloutPromotionSpec) { s.Action = "unknown" },
		"strategy":        func(s *v1alpha1.RolloutPromotionSpec) { s.Strategy = "unknown" },
		"target-strategy": func(s *v1alpha1.RolloutPromotionSpec) { s.Rollouts[0].Strategy = "unknown" },
		"no-target":       func(s *v1alpha1.RolloutPromotionSpec) { s.Rollouts = nil },
		"empty-name":      func(s *v1alpha1.RolloutPromotionSpec) { s.Rollouts[0].Name = " " },
		"completion-policy": func(s *v1alpha1.RolloutPromotionSpec) {
			s.Canary = &v1alpha1.RolloutPromotionCanary{CompletionPolicy: "unknown"}
		},
		"rollback-mode": func(s *v1alpha1.RolloutPromotionSpec) {
			s.TrafficRollback = &v1alpha1.RolloutPromotionTrafficRollback{Mode: "unknown"}
		},
		"rollback-no-provider": func(s *v1alpha1.RolloutPromotionSpec) {
			s.TrafficRollback = &v1alpha1.RolloutPromotionTrafficRollback{}
		},
		"rollback-target":            func(s *v1alpha1.RolloutPromotionSpec) { s.Rollouts[0].RollbackTarget = "unknown" },
		"rollback-target-on-promote": func(s *v1alpha1.RolloutPromotionSpec) { s.Rollouts[0].RollbackTarget = "stable" },
		"readiness-no-identity": func(s *v1alpha1.RolloutPromotionSpec) {
			s.Rollouts[0].Readiness = &v1alpha1.RolloutPromotionReadiness{RequireCurrentPodHash: true}
		},
		"readiness-no-step": func(s *v1alpha1.RolloutPromotionSpec) {
			s.Strategy = "canary"
			s.Rollouts[0].Readiness = &v1alpha1.RolloutPromotionReadiness{RequirePaused: true}
		},
		"negative-step": func(s *v1alpha1.RolloutPromotionSpec) {
			s.Rollouts[0].Readiness = &v1alpha1.RolloutPromotionReadiness{AllowedCurrentStepIndexes: []int32{-1}}
		},
		"traffic-name": func(s *v1alpha1.RolloutPromotionSpec) { s.Rollouts[0].Traffic = &v1alpha1.RolloutPromotionTraffic{} },
		"traffic-no-wait": func(s *v1alpha1.RolloutPromotionSpec) {
			s.Wait.Enabled = boolPointer(false)
			s.Rollouts[0].Traffic = &v1alpha1.RolloutPromotionTraffic{Provider: traffic.ServiceSelector}
		},
		"traffic-on-retry": func(s *v1alpha1.RolloutPromotionSpec) {
			s.Action = "retry"
			s.Rollouts[0].Traffic = &v1alpha1.RolloutPromotionTraffic{Provider: traffic.ServiceSelector}
		},
		"legacy-on-retry": func(s *v1alpha1.RolloutPromotionSpec) {
			s.Action = "retry"
			s.Rollouts[0].WeightSwitch = &v1alpha1.RolloutPromotionWeightSwitch{AnnotationKey: "example.com/weight", ActiveWeight: "10", InactiveWeight: "0"}
		},
		"legacy-no-wait": func(s *v1alpha1.RolloutPromotionSpec) {
			s.Wait.Enabled = boolPointer(false)
			s.Rollouts[0].WeightSwitch = &v1alpha1.RolloutPromotionWeightSwitch{AnnotationKey: "example.com/weight", ActiveWeight: "10", InactiveWeight: "0"}
		},
		"legacy-invalid": func(s *v1alpha1.RolloutPromotionSpec) {
			s.Rollouts[0].WeightSwitch = &v1alpha1.RolloutPromotionWeightSwitch{}
		},
	} {
		t.Run(name, func(t *testing.T) {
			spec := v1alpha1.RolloutPromotionSpec{Rollouts: []v1alpha1.RolloutPromotionTarget{{Name: "demo"}}}
			change(&spec)
			_, err := normalizeSpec(spec)
			require.Error(t, err)
		})
	}
	for _, config := range []*v1alpha1.RolloutPromotionWeightSwitch{
		{ReapplyDelaySeconds: -1}, {AnnotationKey: "example.com/weight"}, {AnnotationKey: "example.com/weight", ActiveWeight: "10"},
	} {
		_, err := normalizeWeightSwitch(config)
		require.Error(t, err)
	}
	disabled, err := normalizeWeightSwitch(&v1alpha1.RolloutPromotionWeightSwitch{Enabled: boolPointer(false)})
	require.NoError(t, err)
	require.False(t, disabled.Enabled)
}

func TestReadinessSeparatesTransientAPIErrorsFromInvalidTargets(t *testing.T) {
	for name, injected := range map[string]error{
		"timeout":        apierrors.NewTimeoutError("slow", 1),
		"server-timeout": apierrors.NewServerTimeout(schema.GroupResource{Resource: "rollouts"}, "get", 1),
		"unavailable":    apierrors.NewServiceUnavailable("down"),
		"throttled":      apierrors.NewTooManyRequests("busy", 1),
		"forbidden":      apierrors.NewForbidden(schema.GroupResource{Resource: "rollouts"}, "demo", errors.New("denied")),
		"missing":        apierrors.NewNotFound(schema.GroupResource{Resource: "rollouts"}, "demo"),
	} {
		t.Run(name, func(t *testing.T) {
			c, p := readinessFixture(t)
			spec, err := normalizeSpec(p.Spec)
			require.NoError(t, err)
			spec.Targets[0].Namespace = p.Namespace
			c.dynamicClient.(*dynamicfake.FakeDynamicClient).PrependReactor("get", "rollouts", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, injected })
			err = c.checkForwardReadiness(context.Background(), p.Namespace, spec)
			var pending *readinessPendingError
			require.Equal(t, name != "forbidden" && name != "missing", errors.As(err, &pending))
			require.Error(t, c.checkCommittedIdentity(context.Background(), p.Namespace, spec.Targets))
		})
	}
}

func TestTrafficGuardsNeverApplyAnUncommittedDestination(t *testing.T) {
	ctx := context.Background()
	c, p, target := lifecycleFixture(t, false)
	for _, state := range []map[string]interface{}{{"promoted": false}, {"promoted": true}} {
		_, err := c.applyTargetTraffic(ctx, p.Namespace, target, state)
		require.Error(t, err)
	}
	_, err := c.applyControlActionTraffic(ctx, p.Namespace, "abort", target, map[string]interface{}{})
	require.ErrorContains(t, err, "stable hash")
	_, err = c.applyPinnedTrafficRollback(ctx, p.Namespace, target, map[string]interface{}{})
	require.ErrorContains(t, err, "no pinned")
	_, err = c.pinTrafficRollbackTarget(ctx, p.Namespace, target, "")
	require.ErrorContains(t, err, "empty active hash")
	target.Strategy = "blueGreen"
	_, err = c.pinTrafficRollbackTarget(ctx, p.Namespace, target, "blue")
	require.ErrorContains(t, err, "strategy safety check")
	target.Strategy = "canary"
	target.ExpectedRolloutUID = "recreated"
	_, _, err = c.trafficTarget(ctx, p.Namespace, target, "blue")
	require.Error(t, err)
	client := c.dynamicClient.(*dynamicfake.FakeDynamicClient)
	for _, action := range client.Actions() {
		require.Equal(t, "get", action.GetVerb())
	}
}

func TestResumeHonorsPinnedIdentityAndControlCheckpoints(t *testing.T) {
	ctx := context.Background()
	c, p, target := lifecycleFixture(t, false)
	p.Status.Targets = []v1alpha1.RolloutPromotionTargetStatus{{Name: target.Name, Namespace: p.Namespace, ActionApplied: true, BaselineCurrentPodHash: "old-version"}}
	target.Readiness = &v1alpha1.RolloutPromotionReadiness{}
	_, err := c.resumePromoteTarget(ctx, p, target)
	require.ErrorContains(t, err, "safety check")
	client := c.dynamicClient.(*dynamicfake.FakeDynamicClient)
	for _, action := range client.Actions() {
		require.Equal(t, "get", action.GetVerb())
	}

	p.Status.Targets[0].ActionApplied = false
	p.Status.Targets[0].BaselineCurrentPodHash = "green"
	p.Status.Targets[0].PreviousStepIndex = 0
	state, err := c.resumePromoteTarget(ctx, p, target)
	require.NoError(t, err, "advanced step proves the committed action was already applied")
	require.Equal(t, true, state["actionApplied"])
	state, err = c.resumeControlTarget(ctx, p, RequestSpec{Action: "retry"}, target)
	require.NoError(t, err)
	require.Equal(t, true, state["actionApplied"])
	for _, action := range client.Actions() {
		require.Equal(t, "get", action.GetVerb())
	}
}

func TestPromotionWaitChecksIdentityAndControlWaitHonorsCancellation(t *testing.T) {
	ctx := context.Background()
	c, p, target := lifecycleFixture(t, false)
	target.Readiness = &v1alpha1.RolloutPromotionReadiness{}
	target.ExpectedCurrentPodHash = "different"
	_, err := c.waitTarget(ctx, p, p.Namespace, target, WaitOptions{}, map[string]interface{}{})
	require.ErrorContains(t, err, "safety check")
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = c.waitTargetAction(canceled, p, p.Namespace, RequestSpec{Action: "abort"}, target, map[string]interface{}{})
	require.ErrorIs(t, err, context.Canceled)
}

func TestReconcileConflictingOperationPreservesOriginalCheckpoint(t *testing.T) {
	c, p := readinessFixture(t)
	now := metav1.Now()
	p.Status.Operation = &v1alpha1.RolloutPromotionOperationStatus{ID: "forward:previous", RequestID: "previous", Phase: v1alpha1.RolloutPromotionOperationApplying, CommitTime: &now}
	_, err := c.promotionClient(p.Namespace).Update(context.Background(), p, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.NoError(t, c.reconcile(context.Background(), p.Namespace+"/"+p.Name))
	live, err := c.promotionClient(p.Namespace).Get(context.Background(), p.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, p.Status.Operation.ID, live.Status.Operation.ID)
	require.Equal(t, v1alpha1.RolloutPromotionOperationStalled, live.Status.Operation.Phase)
	require.Equal(t, "OperationConflict", live.Status.Conditions[0].Reason)
	require.Empty(t, c.dynamicClient.(*dynamicfake.FakeDynamicClient).Actions())
}

func TestReconcileAPIFailureNeverAcknowledgesAnIncompleteOperation(t *testing.T) {
	ctx := context.Background()
	setup := func() (*Controller, *v1alpha1.RolloutPromotion) {
		c, p := readinessFixture(t)
		require.NoError(t, c.patchRolloutStatus(ctx, p.Namespace, "demo", map[string]interface{}{"currentStepIndex": int64(1), "pauseConditions": []interface{}{map[string]interface{}{"reason": "CanaryPauseStep"}}}))
		c.dynamicClient.(*dynamicfake.FakeDynamicClient).ClearActions()
		return c, p
	}
	c, p := setup()
	require.NoError(t, c.reconcile(ctx, p.Namespace+"/"+p.Name))
	calls := len(c.dynamicClient.(*dynamicfake.FakeDynamicClient).Actions()) + len(c.rolloutsClient.(*rolloutfake.Clientset).Actions())
	for failAt := 1; failAt <= calls; failAt++ {
		c, p := setup()
		count := 0
		reactor := func(ktesting.Action) (bool, runtime.Object, error) {
			count++
			if count >= failAt {
				return true, nil, errors.New("injected outage")
			}
			return false, nil, nil
		}
		c.dynamicClient.(*dynamicfake.FakeDynamicClient).PrependReactor("*", "*", reactor)
		client := c.rolloutsClient.(*rolloutfake.Clientset)
		client.PrependReactor("*", "*", reactor)
		_ = c.reconcile(ctx, p.Namespace+"/"+p.Name)
		obj, err := client.Tracker().Get(v1alpha1.RolloutPromotionGVR, p.Namespace, p.Name)
		require.NoError(t, err)
		live := obj.(*v1alpha1.RolloutPromotion)
		require.NotEqual(t, PhaseSucceeded, live.Status.Phase, "API failure %d must not acknowledge success", failAt)
		require.NotContains(t, live.Status.ProcessedRequestIDs, p.Spec.RequestID)
	}
}

func TestReconcileOldTerminalAndDeletedRequestsDoNotReplay(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"old-terminal", "processed", "deleted", "cache-missing", "wrong-type"} {
		t.Run(mode, func(t *testing.T) {
			c, p := readinessFixture(t)
			switch mode {
			case "old-terminal":
				p.Status.Phase = PhaseSucceeded
				p.Status.ObservedGeneration = p.Generation
			case "processed":
				p.Status.ProcessedRequestIDs = []string{p.Spec.RequestID}
			case "deleted":
				now := metav1.Now()
				p.DeletionTimestamp = &now
			case "cache-missing":
				require.NoError(t, c.informer.GetIndexer().Delete(p))
			case "wrong-type":
				require.NoError(t, c.informer.GetIndexer().Update(&corev1.Pod{ObjectMeta: p.ObjectMeta}))
			}
			_, err := c.promotionClient(p.Namespace).Update(ctx, p, metav1.UpdateOptions{})
			require.NoError(t, err)
			err = c.reconcile(ctx, p.Namespace+"/"+p.Name)
			if mode == "wrong-type" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Empty(t, c.dynamicClient.(*dynamicfake.FakeDynamicClient).Actions())
		})
	}
}

func TestRequestAndSnapshotValidationBeforeExecution(t *testing.T) {
	ctx := context.Background()
	c, p, target := lifecycleFixture(t, false)
	for _, mode := range []string{"together", "sequential", "invalid"} {
		for _, action := range []string{"promote", "retry"} {
			spec := RequestSpec{Action: action, Mode: mode, Targets: []Target{{Name: "missing"}}}
			_, err := c.execute(ctx, p, spec, map[string]interface{}{})
			require.Error(t, err)
		}
	}
	_, err := c.executePreviousTrafficSnapshot(ctx, p, RequestSpec{}, map[string]interface{}{})
	require.ErrorContains(t, err, "unsupported")
	spec := RequestSpec{Action: "retry", TrafficRollback: TrafficRollbackOptions{Mode: "previousSnapshot"}}
	_, err = c.executePreviousTrafficSnapshot(ctx, p, spec, map[string]interface{}{})
	require.ErrorContains(t, err, "no pre-promotion")
	_, _, _, err = c.ensureTrafficSnapshotForRollback(ctx, p, spec, map[string]interface{}{})
	require.ErrorContains(t, err, "no pre-promotion")
	spec.Action = "promote"
	spec.Targets = []Target{target}
	spec.Targets[0].Traffic = nil
	_, _, _, err = c.ensureTrafficSnapshotForRollback(ctx, p, spec, map[string]interface{}{})
	require.ErrorContains(t, err, "requires a traffic provider")
	_, err = parseSpec(&unstructured.Unstructured{Object: map[string]interface{}{"spec": "not-an-object"}})
	require.Error(t, err)
	_, err = c.controlTarget(ctx, p.Namespace, RequestSpec{Action: "unknown"}, target)
	require.ErrorContains(t, err, "unsupported spec.action")
	target.RollbackTarget = "unknown"
	_, err = c.resolveRollbackRevision(ctx, p.Namespace, target, map[string]interface{}{})
	require.Error(t, err)
	target.RollbackTarget = "stable"
	_, err = c.resolveRollbackRevision(ctx, p.Namespace, target, map[string]interface{}{})
	require.ErrorContains(t, err, "stableRS is empty")
	target.Name = "missing"
	_, err = c.resolveRollbackRevision(ctx, p.Namespace, target, map[string]interface{}{"stableRS": "blue"})
	require.Error(t, err)
}

func TestSnapshotHistoryBoundAndInvalidTrafficBaseline(t *testing.T) {
	ctx := context.Background()
	c, p, target := lifecycleFixture(t, false)
	p.Status.TrafficSnapshots = []v1alpha1.RolloutPromotionTrafficSnapshot{{RequestID: "previous"}}
	spec := RequestSpec{RequestID: "current", Targets: []Target{target}, TrafficRollback: TrafficRollbackOptions{HistoryLimit: 1}}
	status, err := c.captureTrafficSnapshot(ctx, p, spec, map[string]interface{}{})
	require.NoError(t, err)
	snapshots := status["trafficSnapshots"].([]v1alpha1.RolloutPromotionTrafficSnapshot)
	require.Len(t, snapshots, 1)
	require.Equal(t, "current", snapshots[0].RequestID)
	p.Status.TrafficSnapshots = snapshots
	_, err = c.captureTrafficSnapshot(ctx, p, spec, status)
	require.NoError(t, err, "the same request must not append a second baseline")
	_, err = c.executePreviousTrafficSnapshot(ctx, p, RequestSpec{Mode: "invalid", TrafficRollback: TrafficRollbackOptions{Mode: "previousSnapshot"}}, map[string]interface{}{})
	require.ErrorContains(t, err, "unsupported spec.mode")

	for _, scenario := range []string{"wrong-namespace", "wrong-provider", "wrong-uid"} {
		bad := p.DeepCopy()
		switch scenario {
		case "wrong-namespace":
			bad.Status.TrafficSnapshots[0].Targets[0].Namespace = "other"
		case "wrong-provider":
			bad.Status.TrafficSnapshots[0].Targets[0].Traffic.Provider = "unknown"
		case "wrong-uid":
			bad.Status.TrafficSnapshots[0].Targets[0].Traffic.RolloutUID = "recreated"
		}
		_, err = c.executePreviousTrafficSnapshot(ctx, bad, RequestSpec{Mode: "together", TrafficRollback: TrafficRollbackOptions{Mode: "previousSnapshot"}}, map[string]interface{}{})
		require.Error(t, err)
	}
}

func TestRetainedTargetMustHaveAllOwnedPodsReady(t *testing.T) {
	ctx := context.Background()
	for _, scenario := range []string{"scaled-down", "unavailable", "unready", "wrong-owner", "terminating"} {
		c, p, target := lifecycleFixture(t, false)
		client := c.kubeClient.(*kubefake.Clientset)
		rs, err := client.AppsV1().ReplicaSets(p.Namespace).Get(ctx, "rs-blue", metav1.GetOptions{})
		require.NoError(t, err)
		pod, err := client.CoreV1().Pods(p.Namespace).Get(ctx, "pod-blue", metav1.GetOptions{})
		require.NoError(t, err)
		switch scenario {
		case "scaled-down":
			rs.Spec.Replicas = int32Pointer(0)
		case "unavailable":
			rs.Status.AvailableReplicas = 0
		case "unready":
			pod.Status.Conditions = nil
		case "wrong-owner":
			rs.OwnerReferences = nil
		case "terminating":
			now := metav1.Now()
			pod.DeletionTimestamp = &now
		}
		_, err = client.AppsV1().ReplicaSets(p.Namespace).Update(ctx, rs, metav1.UpdateOptions{})
		require.NoError(t, err)
		_, err = client.CoreV1().Pods(p.Namespace).Update(ctx, pod, metav1.UpdateOptions{})
		require.NoError(t, err)
		require.Error(t, c.validateRetainedTrafficTarget(ctx, p.Namespace, target, "blue"), scenario)
	}
}
