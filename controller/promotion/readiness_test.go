package promotion

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutfake "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/utils/annotations"
)

func TestReadinessDoesNotRelaxSafety(t *testing.T) {
	for _, tc := range []struct {
		name    string
		key     string
		value   interface{}
		pending bool
		failure bool
	}{
		{"ready", "promotionPaused", true, false, false},
		{"not-paused", "promotionPaused", false, true, true},
		{"earlier-step", "currentStepIndex", int64(0), true, true},
		{"later-step", "currentStepIndex", int64(2), false, true},
		{"missing-hash", "currentPodHash", "", true, true},
		{"wrong-hash", "currentPodHash", "other", false, true},
		{"recreated", "rolloutUID", "other", false, true},
		{"new-template", "podTemplateHash", "other", false, true},
		{"stale-generation", "observedGeneration", "0", true, true},
		{"aborted", "aborted", true, false, true},
		{"degraded", "rolloutPhase", "Degraded", false, true},
		{"baseline-unready", "activeServiceHash", "", true, true},
		{"wrong-baseline", "activeServiceHash", "other", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := Target{Name: "demo", Strategy: "canary", ExpectedRolloutUID: "uid", ExpectedPodTemplateHash: "template", ExpectedCurrentPodHash: "green", ExpectedStableRS: "blue", ExpectedActiveServiceHash: "blue", ExpectedCurrentStepIndex: int32Pointer(1), Readiness: &v1alpha1.RolloutPromotionReadiness{RequireCurrentPodHash: true, RequirePaused: true, AllowedCurrentStepIndexes: []int32{1}}}
			state := map[string]interface{}{"namespace": "demo", "strategy": "canary", "rolloutUID": "uid", "podTemplateHash": "template", "currentPodHash": "green", "stableRS": "blue", "activeServiceHash": "blue", "currentStepIndex": int64(1), "promotionPaused": true, "generation": int64(1), "observedGeneration": "1"}
			state[tc.key] = tc.value
			err := validateForwardReadiness(target, state, "promote")
			if (err != nil) != tc.failure {
				t.Fatalf("unexpected error: %v", err)
			}
			_, pending := err.(*readinessPendingError)
			if pending != tc.pending {
				t.Fatalf("pending=%v error=%v", pending, err)
			}
		})
	}
}

func readinessFixture(t *testing.T) (*Controller, *v1alpha1.RolloutPromotion) {
	t.Helper()
	wait := false
	p := &v1alpha1.RolloutPromotion{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "test-ns", Generation: 1}, Spec: v1alpha1.RolloutPromotionSpec{RequestID: "revision-2", Action: "promote", Strategy: "canary", Wait: v1alpha1.RolloutPromotionWait{Enabled: &wait, Seconds: 1, Attempts: 30}, Rollouts: []v1alpha1.RolloutPromotionTarget{{Name: "demo", ExpectedRolloutUID: "uid", ExpectedCurrentPodHash: "green", ExpectedStableRS: "blue", ExpectedCurrentStepIndex: int32Pointer(1), Readiness: &v1alpha1.RolloutPromotionReadiness{RequireCurrentPodHash: true, RequirePaused: true, AllowedCurrentStepIndexes: []int32{1}}}}}}
	ro := newCanaryRollout("test-ns", "demo", "green", "blue", 0, nil)
	ro.SetUID(types.UID("uid"))
	informer := cache.NewSharedIndexInformer(nil, &v1alpha1.RolloutPromotion{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	if err := informer.GetIndexer().Add(p); err != nil {
		t.Fatal(err)
	}
	c := &Controller{kubeClient: kubefake.NewSimpleClientset(), dynamicClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), ro), rolloutsClient: rolloutfake.NewSimpleClientset(p), informer: informer, queue: workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())}
	t.Cleanup(c.queue.ShutDown)
	return c, p
}

func TestReadinessWaitResumesWithoutNewRequest(t *testing.T) {
	c, p := readinessFixture(t)
	ctx := context.Background()
	for range 3 {
		if err := c.reconcile(ctx, "test-ns/demo"); err != nil {
			t.Fatal(err)
		}
		live, _ := c.promotionClient(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
		if live.Status.Phase != PhasePending || live.Status.Operation.CommitTime != nil || len(live.Status.ProcessedRequestIDs) != 0 || len(live.Status.TrafficSnapshots) != 0 {
			t.Fatalf("wait must not commit or consume request: %+v", live.Status)
		}
	}
	for _, action := range c.dynamicClient.(*dynamicfake.FakeDynamicClient).Actions() {
		if action.GetVerb() != "get" {
			t.Fatalf("waiting mutated target: %v", action)
		}
	}
	_, err := c.rolloutClient(p.Namespace).Patch(ctx, "demo", types.MergePatchType, []byte(`{"status":{"currentStepIndex":1,"pauseConditions":[{"reason":"CanaryPauseStep"}]}}`), metav1.PatchOptions{}, "status")
	if err != nil {
		t.Fatal(err)
	}
	// A fresh controller with the same durable objects must resume preparation.
	restarted := *c
	if err := restarted.reconcile(ctx, "test-ns/demo"); err != nil {
		t.Fatal(err)
	}
	live, _ := c.promotionClient(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
	if live.Status.Phase != PhaseSucceeded || live.Status.Operation.CommitTime == nil {
		t.Fatalf("did not resume: %+v", live.Status)
	}
	before := len(c.dynamicClient.(*dynamicfake.FakeDynamicClient).Actions())
	if err := restarted.reconcile(ctx, "test-ns/demo"); err != nil {
		t.Fatal(err)
	}
	for _, action := range c.dynamicClient.(*dynamicfake.FakeDynamicClient).Actions()[before:] {
		if action.GetVerb() == "patch" {
			t.Fatalf("replay mutated rollout: %v", action)
		}
	}
}

func TestReadinessDeadlineSurvivesRestart(t *testing.T) {
	c, p := readinessFixture(t)
	ctx := context.Background()
	if err := c.reconcile(ctx, "test-ns/demo"); err != nil {
		t.Fatal(err)
	}
	live, _ := c.promotionClient(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
	live.Status.StartTime = &metav1.Time{Time: time.Now().Add(-time.Hour)}
	if _, err := c.promotionClient(p.Namespace).UpdateStatus(ctx, live, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := c.reconcile(ctx, "test-ns/demo"); err != nil {
		t.Fatal(err)
	}
	live, _ = c.promotionClient(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
	if live.Status.Phase != PhaseFailed || !strings.Contains(live.Status.Conditions[0].Message, "deadline exceeded") || live.Status.Operation.CommitTime != nil {
		t.Fatalf("unexpected deadline result: %+v", live.Status)
	}
}

func TestNewReadinessRequestClearsPreviousCommit(t *testing.T) {
	c, p := readinessFixture(t)
	ctx := context.Background()
	now := metav1.Now()
	p.Status = v1alpha1.RolloutPromotionStatus{Phase: PhaseSucceeded, StartTime: &now, CompletionTime: &now, Operation: &v1alpha1.RolloutPromotionOperationStatus{ID: "forward/old-request", RequestID: "old-request", Kind: "Forward", Action: "promote", Phase: "Succeeded", Attempt: 3, CommitTime: &now, CompletionTime: &now}}
	if _, err := c.promotionClient(p.Namespace).UpdateStatus(ctx, p, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := c.reconcile(ctx, "test-ns/demo"); err != nil {
			t.Fatal(err)
		}
		live, _ := c.promotionClient(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
		if live.Status.Phase != PhasePending || live.Status.CompletionTime != nil || live.Status.Operation.CommitTime != nil || live.Status.Operation.CompletionTime != nil || live.Status.Operation.Attempt != 0 {
			t.Fatalf("previous operation leaked into readiness: %+v %+v", live.Status, live.Status.Operation)
		}
	}
}

func TestReadinessCompletedVersionIsNoOp(t *testing.T) {
	c, p := readinessFixture(t)
	ctx := context.Background()
	_, err := c.rolloutClient(p.Namespace).Patch(ctx, "demo", types.MergePatchType, []byte(`{"status":{"currentStepIndex":3,"stableRS":"green","phase":"Healthy"}}`), metav1.PatchOptions{}, "status")
	if err != nil {
		t.Fatal(err)
	}
	before := len(c.dynamicClient.(*dynamicfake.FakeDynamicClient).Actions())
	if err := c.reconcile(ctx, "test-ns/demo"); err != nil {
		t.Fatal(err)
	}
	live, _ := c.promotionClient(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
	if live.Status.Phase != PhaseSucceeded || live.Status.Conditions[0].Reason != "AlreadyPromoted" {
		t.Fatalf("not a no-op: %+v", live.Status)
	}
	for _, action := range c.dynamicClient.(*dynamicfake.FakeDynamicClient).Actions()[before:] {
		if action.GetVerb() != "get" {
			t.Fatalf("no-op mutated rollout: %v", action)
		}
	}
}

func TestReadinessWorkloadRefMustBeObserved(t *testing.T) {
	c, p := readinessFixture(t)
	ctx := context.Background()
	workload := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]interface{}{"name": "source", "namespace": p.Namespace, "generation": int64(2)}, "spec": map[string]interface{}{"template": map[string]interface{}{"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "demo"}}}}}}
	_, err := c.dynamicClient.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}).Namespace(p.Namespace).Create(ctx, workload, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.rolloutClient(p.Namespace).Patch(ctx, "demo", types.MergePatchType, []byte(`{"spec":{"workloadRef":{"apiVersion":"apps/v1","kind":"Deployment","name":"source"}},"status":{"workloadObservedGeneration":"1"}}`), metav1.PatchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.reconcile(ctx, "test-ns/demo"); err != nil {
		t.Fatal(err)
	}
	live, _ := c.promotionClient(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
	if live.Status.Phase != PhasePending || !strings.Contains(live.Status.Conditions[0].Message, "generation") {
		t.Fatalf("did not wait for Deployment: %+v", live.Status)
	}
}

func TestReadinessPendingCanBeTrafficRolledBack(t *testing.T) {
	c, p := readinessFixture(t)
	ctx := context.Background()
	enabled := true
	p.Spec.TrafficRollback = &v1alpha1.RolloutPromotionTrafficRollback{Enabled: &enabled}
	p.Spec.Wait.Enabled = &enabled
	p.Spec.Rollouts[0].WeightSwitch = &v1alpha1.RolloutPromotionWeightSwitch{Enabled: &enabled, AnnotationKey: "example.com/weight", BaseSelector: "app=demo", ActiveWeight: "10", InactiveWeight: "0"}
	if _, err := c.promotionClient(p.Namespace).Update(ctx, p, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{"blue", "green"} {
		pod := newPod(p.Namespace, "demo-"+hash, hash)
		pod.Labels["app"] = "demo"
		pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
		if _, err := c.kubeClient.CoreV1().Pods(p.Namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		rs := newReplicaSet(p.Namespace, "demo-"+hash, hash)
		rs.Labels["app"] = "demo"
		rs.OwnerReferences = []metav1.OwnerReference{{APIVersion: "argoproj.io/v1alpha1", Kind: "Rollout", Name: "demo", UID: "uid", Controller: &enabled}}
		rs.Spec.Replicas = int32Pointer(1)
		rs.Status.AvailableReplicas = 1
		if _, err := c.kubeClient.AppsV1().ReplicaSets(p.Namespace).Create(ctx, rs, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.reconcile(ctx, "test-ns/demo"); err != nil {
		t.Fatal(err)
	}
	live, _ := c.promotionClient(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
	if live.Status.Phase != PhasePending {
		t.Fatalf("not pending: %+v", live.Status)
	}
	live.Annotations = map[string]string{annotations.TrafficRollbackRequestAnnotation: "cancel-before-ready"}
	if _, err := c.promotionClient(p.Namespace).Update(ctx, live, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := c.reconcile(ctx, "test-ns/demo"); err != nil {
		t.Fatal(err)
	}
	live, _ = c.promotionClient(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
	if live.Status.Phase != PhaseSucceeded || live.Status.LastTrafficRollbackRequest != "cancel-before-ready" {
		t.Fatalf("rollback did not preempt readiness: %+v", live.Status)
	}
	for _, hash := range []string{"blue", "green"} {
		pod, _ := c.kubeClient.CoreV1().Pods(p.Namespace).Get(ctx, "demo-"+hash, metav1.GetOptions{})
		want := "0"
		if hash == "blue" {
			want = "10"
		}
		if pod.Annotations["example.com/weight"] != want {
			t.Fatalf("wrong %s weight: %v", hash, pod.Annotations)
		}
	}
	for _, action := range c.kubeClient.(*kubefake.Clientset).Actions() {
		if action.GetVerb() == "delete" {
			t.Fatalf("rollback deleted resource: %v", action)
		}
	}
	if err := c.reconcile(ctx, "test-ns/demo"); err != nil {
		t.Fatal(err)
	}
}

func TestReadinessAllTargetsWaitBeforeMutation(t *testing.T) {
	for _, mode := range []string{"together", "sequential"} {
		t.Run(mode, func(t *testing.T) {
			c, p := readinessFixture(t)
			ctx := context.Background()
			p.Spec.Mode = mode
			other := p.Spec.Rollouts[0]
			other.Name = "other"
			other.ExpectedRolloutUID = "other-uid"
			p.Spec.Rollouts = append(p.Spec.Rollouts, other)
			if _, err := c.promotionClient(p.Namespace).Update(ctx, p, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			ro := newCanaryRollout(p.Namespace, "other", "green", "blue", 0, nil)
			ro.SetUID("other-uid")
			if _, err := c.rolloutClient(p.Namespace).Create(ctx, ro, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := c.rolloutClient(p.Namespace).Patch(ctx, "demo", types.MergePatchType, []byte(`{"status":{"currentStepIndex":1,"pauseConditions":[{"reason":"CanaryPauseStep"}]}}`), metav1.PatchOptions{}, "status"); err != nil {
				t.Fatal(err)
			}
			before := len(c.dynamicClient.(*dynamicfake.FakeDynamicClient).Actions())
			if err := c.reconcile(ctx, "test-ns/demo"); err != nil {
				t.Fatal(err)
			}
			live, _ := c.promotionClient(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
			if live.Status.Phase != PhasePending {
				t.Fatalf("not waiting: %+v", live.Status)
			}
			for _, action := range c.dynamicClient.(*dynamicfake.FakeDynamicClient).Actions()[before:] {
				if action.GetVerb() != "get" {
					t.Fatalf("one target mutated before the other is ready: %v", action)
				}
			}
		})
	}
}

func TestReadinessPinsInitiallyMissingHash(t *testing.T) {
	c, p := readinessFixture(t)
	ctx := context.Background()
	spec, _ := normalizeSpec(p.Spec)
	state, err := c.observeTarget(ctx, p.Namespace, spec.Targets[0])
	if err != nil {
		t.Fatal(err)
	}
	p.Spec.Rollouts[0].ExpectedCurrentPodHash = ""
	p.Spec.Rollouts[0].ExpectedPodTemplateHash = state["podTemplateHash"].(string)
	if _, err := c.promotionClient(p.Namespace).Update(ctx, p, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.rolloutClient(p.Namespace).Patch(ctx, "demo", types.MergePatchType, []byte(`{"status":{"currentPodHash":null}}`), metav1.PatchOptions{}, "status"); err != nil {
		t.Fatal(err)
	}
	if err := c.reconcile(ctx, "test-ns/demo"); err != nil {
		t.Fatal(err)
	}
	live, _ := c.promotionClient(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
	if live.Status.Phase != PhasePending {
		t.Fatalf("not waiting: %+v", live.Status)
	}
	if _, err := c.rolloutClient(p.Namespace).Patch(ctx, "demo", types.MergePatchType, []byte(`{"status":{"currentPodHash":"green","currentStepIndex":1,"pauseConditions":[{"reason":"CanaryPauseStep"}]}}`), metav1.PatchOptions{}, "status"); err != nil {
		t.Fatal(err)
	}
	if err := c.reconcile(ctx, "test-ns/demo"); err != nil {
		t.Fatal(err)
	}
	live, _ = c.promotionClient(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
	if live.Status.Phase != PhaseSucceeded || live.Status.Targets[0].BaselineCurrentPodHash != "green" {
		t.Fatalf("hash not durably bound: %+v", live.Status)
	}
}
