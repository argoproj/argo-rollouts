package promotion

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutfake "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/utils/annotations"
)

func TestParseSpec(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{
			"mode": "sequential",
			"wait": map[string]interface{}{
				"enabled":  true,
				"attempts": int64(30),
				"seconds":  int64(2),
			},
			"rollouts": []interface{}{
				map[string]interface{}{
					"name":                      "example-api",
					"expectedRolloutUID":        "rollout-uid-1",
					"activeService":             "example-api-active",
					"previewService":            "example-api-preview",
					"expectedCurrentPodHash":    "new-hash",
					"expectedCurrentStepIndex":  int64(2),
					"expectedStableRS":          "old-hash",
					"expectedActiveServiceHash": "old-hash",
					"weightSwitch": map[string]interface{}{
						"enabled":             true,
						"annotationKey":       "example.com/traffic-weight",
						"baseSelector":        "app=example,component=control",
						"activeWeight":        "[{\"weight\":10}]",
						"inactiveWeight":      "[{\"weight\":0}]",
						"reapplyDelaySeconds": int64(3),
					},
				},
				map[string]interface{}{
					"name":      "example-worker",
					"namespace": "test-ns",
					"strategy":  "blueGreen",
				},
			},
		},
	}}

	spec, err := parseSpec(obj)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Mode != "sequential" {
		t.Fatalf("expected sequential mode, got %s", spec.Mode)
	}
	if spec.Action != "promote" || spec.Strategy != "auto" {
		t.Fatalf("unexpected default action/strategy: action=%s strategy=%s", spec.Action, spec.Strategy)
	}
	if !spec.Wait.Enabled || spec.Wait.Attempts != 30 || spec.Wait.Seconds != 2 {
		t.Fatalf("unexpected wait options: %+v", spec.Wait)
	}
	if len(spec.Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(spec.Targets))
	}
	if spec.Targets[0].ActiveService != "example-api-active" {
		t.Fatalf("unexpected active service: %s", spec.Targets[0].ActiveService)
	}
	if spec.Targets[0].ExpectedRolloutUID != "rollout-uid-1" {
		t.Fatalf("unexpected expected rollout UID: %s", spec.Targets[0].ExpectedRolloutUID)
	}
	if spec.Targets[0].PreviewService != "example-api-preview" {
		t.Fatalf("unexpected preview service: %s", spec.Targets[0].PreviewService)
	}
	if spec.Targets[0].ExpectedCurrentPodHash != "new-hash" {
		t.Fatalf("unexpected expected current hash: %s", spec.Targets[0].ExpectedCurrentPodHash)
	}
	if spec.Targets[0].ExpectedCurrentStepIndex == nil || *spec.Targets[0].ExpectedCurrentStepIndex != 2 {
		t.Fatalf("unexpected expected current step: %v", spec.Targets[0].ExpectedCurrentStepIndex)
	}
	if spec.Targets[0].ExpectedStableRS != "old-hash" {
		t.Fatalf("unexpected expected stable hash: %s", spec.Targets[0].ExpectedStableRS)
	}
	if spec.Targets[0].ExpectedActiveServiceHash != "old-hash" {
		t.Fatalf("unexpected expected active service hash: %s", spec.Targets[0].ExpectedActiveServiceHash)
	}
	if spec.Targets[0].WeightSwitch == nil || !spec.Targets[0].WeightSwitch.Enabled {
		t.Fatal("expected enabled weightSwitch")
	}
	if spec.Targets[0].WeightSwitch.BaseSelector != "app=example,component=control" {
		t.Fatalf("unexpected weightSwitch baseSelector: %s", spec.Targets[0].WeightSwitch.BaseSelector)
	}
	if spec.Targets[0].WeightSwitch.ReapplyDelaySeconds != 3 {
		t.Fatalf("unexpected reapply delay: %d", spec.Targets[0].WeightSwitch.ReapplyDelaySeconds)
	}
	if spec.Targets[1].Namespace != "test-ns" {
		t.Fatalf("unexpected namespace: %s", spec.Targets[1].Namespace)
	}
	if spec.Targets[1].Strategy != "blueGreen" {
		t.Fatalf("unexpected target strategy: %s", spec.Targets[1].Strategy)
	}
}

func TestParseSpecRollbackTarget(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{
			"action":   "rollback",
			"strategy": "canary",
			"rollouts": []interface{}{map[string]interface{}{
				"name":       "example",
				"toRevision": int64(7),
			}},
		},
	}}
	spec, err := parseSpec(obj)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Action != "rollback" || spec.Strategy != "canary" || spec.Targets[0].ToRevision != 7 {
		t.Fatalf("unexpected rollback spec: %+v", spec)
	}

	obj.Object["spec"].(map[string]interface{})["rollouts"] = []interface{}{map[string]interface{}{
		"name":           "example",
		"rollbackTarget": "stable",
	}}
	spec, err = parseSpec(obj)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Targets[0].RollbackTarget != "stable" || spec.Targets[0].ToRevision != 0 {
		t.Fatalf("unexpected stable rollback target: %+v", spec.Targets[0])
	}

	obj.Object["spec"].(map[string]interface{})["rollouts"] = []interface{}{map[string]interface{}{"name": "example"}}
	if _, err := parseSpec(obj); err == nil {
		t.Fatal("expected rollback without a target to fail")
	}

	obj.Object["spec"].(map[string]interface{})["rollouts"] = []interface{}{map[string]interface{}{
		"name":           "example",
		"toRevision":     int64(7),
		"rollbackTarget": "stable",
	}}
	if _, err := parseSpec(obj); err == nil {
		t.Fatal("expected rollback with conflicting targets to fail")
	}
}

func TestParseSpecTrafficRollback(t *testing.T) {
	enabled := true
	spec, err := normalizeSpec(v1alpha1.RolloutPromotionSpec{
		RequestID: "first-cutover",
		Action:    "promote",
		Strategy:  "canary",
		TrafficRollback: &v1alpha1.RolloutPromotionTrafficRollback{
			Enabled:      &enabled,
			Mode:         "previousSnapshot",
			HistoryLimit: 12,
		},
		Rollouts: []v1alpha1.RolloutPromotionTarget{{
			Name:                   "example",
			ExpectedCurrentPodHash: "green",
			ExpectedStableRS:       "blue",
			WeightSwitch: &v1alpha1.RolloutPromotionWeightSwitch{
				Enabled:        &enabled,
				AnnotationKey:  "example.com/weight",
				BaseSelector:   "app=example",
				ActiveWeight:   "active",
				InactiveWeight: "inactive",
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.RequestID != "first-cutover" || !spec.TrafficRollback.Enabled || spec.TrafficRollback.HistoryLimit != 12 || spec.TrafficRollback.Mode != "previousSnapshot" {
		t.Fatalf("unexpected traffic rollback options: %+v", spec.TrafficRollback)
	}

	// Helm rollback restores an older non-promote spec before its lifecycle
	// hook requests traffic restoration. Keep rollback enabled on that spec so
	// the request can consume snapshots retained in status.
	apiSpec := v1alpha1.RolloutPromotionSpec{
		RequestID:       "previous-abort-revision",
		Action:          "abort",
		Strategy:        "canary",
		TrafficRollback: &v1alpha1.RolloutPromotionTrafficRollback{Enabled: &enabled},
		Rollouts: []v1alpha1.RolloutPromotionTarget{{
			Name: "example",
			WeightSwitch: &v1alpha1.RolloutPromotionWeightSwitch{
				Enabled:        &enabled,
				AnnotationKey:  "example.com/weight",
				BaseSelector:   "app=example",
				ActiveWeight:   "active",
				InactiveWeight: "inactive",
			},
		}},
	}
	spec, err = normalizeSpec(apiSpec)
	if err != nil {
		t.Fatalf("non-promote revision must retain traffic rollback configuration: %v", err)
	}
	if !spec.TrafficRollback.Enabled {
		t.Fatal("expected traffic rollback to remain enabled on abort revision")
	}
}

func TestNormalizeCanaryCompletionPolicy(t *testing.T) {
	spec, err := normalizeSpec(v1alpha1.RolloutPromotionSpec{
		Action:   "promote",
		Strategy: "canary",
		Canary: &v1alpha1.RolloutPromotionCanary{
			CompletionPolicy: v1alpha1.RolloutPromotionCanaryCompletionFullWeight,
		},
		Rollouts: []v1alpha1.RolloutPromotionTarget{{Name: "example"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.CanaryCompletionPolicy != v1alpha1.RolloutPromotionCanaryCompletionFullWeight {
		t.Fatalf("unexpected completion policy %q", spec.CanaryCompletionPolicy)
	}

	disabled := false
	_, err = normalizeSpec(v1alpha1.RolloutPromotionSpec{
		Action:   "promote",
		Strategy: "canary",
		Canary: &v1alpha1.RolloutPromotionCanary{
			CompletionPolicy: v1alpha1.RolloutPromotionCanaryCompletionFullWeight,
		},
		Wait:     v1alpha1.RolloutPromotionWait{Enabled: &disabled},
		Rollouts: []v1alpha1.RolloutPromotionTarget{{Name: "example"}},
	})
	if err == nil || !strings.Contains(err.Error(), "spec.wait.enabled must be true") {
		t.Fatalf("expected disabled wait to reject final completion, got %v", err)
	}
}

func TestShouldCompleteCanaryAfterFullWeight(t *testing.T) {
	ready := map[string]interface{}{
		"strategy":         "canary",
		"promoted":         false,
		"pauseConditions":  1,
		"currentStepIndex": int64(4),
		"stepCount":        int64(5),
		"canaryWeight":     int64(100),
	}
	complete, err := shouldCompleteCanaryAfterFullWeight(v1alpha1.RolloutPromotionCanaryCompletionFullWeight, ready)
	if err != nil || !complete {
		t.Fatalf("expected final 100%% pause to complete, complete=%t err=%v", complete, err)
	}

	notFinal := map[string]interface{}{}
	for key, value := range ready {
		notFinal[key] = value
	}
	notFinal["currentStepIndex"] = int64(2)
	complete, err = shouldCompleteCanaryAfterFullWeight(v1alpha1.RolloutPromotionCanaryCompletionFullWeight, notFinal)
	if err != nil || complete {
		t.Fatalf("expected an intermediate pause to remain protected, complete=%t err=%v", complete, err)
	}

	complete, err = shouldCompleteCanaryAfterFullWeight(v1alpha1.RolloutPromotionCanaryCompletionNextPause, notFinal)
	if err != nil || complete {
		t.Fatalf("default nextPause policy must preserve existing behavior, complete=%t err=%v", complete, err)
	}
}

func TestResolveActiveTrafficHash(t *testing.T) {
	hash, err := resolveActiveTrafficHash(map[string]interface{}{
		"trafficOverrideHash": "restored",
		"activeServiceHash":   "blue",
		"stableRS":            "blue",
	})
	if err != nil || hash != "restored" {
		t.Fatalf("override must be the active snapshot, got hash=%q err=%v", hash, err)
	}
	hash, err = resolveActiveTrafficHash(map[string]interface{}{
		"strategy":          "canary",
		"activeServiceHash": "green",
		"stableRS":          "blue",
		"currentPodHash":    "green",
	})
	if err != nil || hash != "blue" {
		t.Fatalf("canary snapshot should select stable blue, got hash=%q err=%v", hash, err)
	}
}

func TestTrafficSnapshotRollbackPatchesWeightsWithoutRecreatingPods(t *testing.T) {
	rollout := newCanaryRollout("test-ns", "example-worker", "green", "blue", 2, nil)
	rollout.SetUID(types.UID("rollout-uid"))
	controllerRef := true
	replicas := int32(1)
	readyCondition := []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	owner := []metav1.OwnerReference{{
		APIVersion: "argoproj.io/v1alpha1",
		Kind:       "Rollout",
		Name:       "example-worker",
		UID:        types.UID("rollout-uid"),
		Controller: &controllerRef,
	}}
	stableRS := newReplicaSet("test-ns", "stable-rs", "blue")
	stableRS.OwnerReferences = owner
	stableRS.Spec.Replicas = &replicas
	stableRS.Status.AvailableReplicas = replicas
	canaryRS := newReplicaSet("test-ns", "canary-rs", "green")
	canaryRS.OwnerReferences = owner
	canaryRS.Spec.Replicas = &replicas
	canaryRS.Status.AvailableReplicas = replicas
	stablePod := newPod("test-ns", "stable-pod", "blue")
	stablePod.Status.Conditions = readyCondition
	canaryPod := newPod("test-ns", "canary-pod", "green")
	canaryPod.Status.Conditions = readyCondition

	kubeClient := kubefake.NewSimpleClientset(stableRS, canaryRS, stablePod, canaryPod)
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout)
	controller := &Controller{kubeClient: kubeClient, dynamicClient: dynamicClient}
	target := Target{
		Name:                   "example-worker",
		Strategy:               "canary",
		ExpectedCurrentPodHash: "green",
		ExpectedStableRS:       "blue",
		WeightSwitch: &WeightSwitch{
			Enabled:        true,
			AnnotationKey:  "example.com/traffic-weight",
			BaseSelector:   "app=example,component=data",
			ActiveWeight:   "active",
			InactiveWeight: "inactive",
		},
	}
	state, err := controller.rollbackTrafficTarget(context.Background(), "test-ns", target, "blue")
	if err != nil {
		t.Fatal(err)
	}
	if state["trafficRollbackTargetHash"] != "blue" || state["trafficRollbackApplied"] != true {
		t.Fatalf("unexpected traffic rollback state: %+v", state)
	}
	updatedRollout, err := dynamicClient.Resource(v1alpha1.RolloutGVR).Namespace("test-ns").Get(context.Background(), "example-worker", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if updatedRollout.GetAnnotations()[annotations.TrafficOverrideHashAnnotation] != "blue" {
		t.Fatalf("expected traffic override blue, got %v", updatedRollout.GetAnnotations())
	}
	if updatedRollout.GetAnnotations()[annotations.TrafficOverrideSourceHashAnnotation] != "green" {
		t.Fatalf("expected traffic override source green, got %v", updatedRollout.GetAnnotations())
	}
	stablePod, _ = kubeClient.CoreV1().Pods("test-ns").Get(context.Background(), "stable-pod", metav1.GetOptions{})
	canaryPod, _ = kubeClient.CoreV1().Pods("test-ns").Get(context.Background(), "canary-pod", metav1.GetOptions{})
	if stablePod.Annotations["example.com/traffic-weight"] != "active" || canaryPod.Annotations["example.com/traffic-weight"] != "inactive" {
		t.Fatalf("unexpected weights stable=%q canary=%q", stablePod.Annotations["example.com/traffic-weight"], canaryPod.Annotations["example.com/traffic-weight"])
	}
	for _, action := range kubeClient.Actions() {
		if action.GetVerb() == "create" || action.GetVerb() == "delete" {
			t.Fatalf("traffic rollback must not create or delete workload objects: %s %s", action.GetVerb(), action.GetResource().Resource)
		}
	}
}

func TestForwardPromotionClearsTrafficRollbackState(t *testing.T) {
	rollout := newCanaryRollout("test-ns", "example-worker", "next", "blue", 2, nil)
	rollout.SetAnnotations(map[string]string{
		annotations.TrafficOverrideHashAnnotation:       "blue",
		annotations.TrafficOverrideSourceHashAnnotation: "green",
	})
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout)
	controller := &Controller{dynamicClient: dynamicClient}

	if err := controller.prepareForwardPromotion(context.Background(), "test-ns", []Target{{Name: "example-worker"}}); err != nil {
		t.Fatal(err)
	}
	updated, err := dynamicClient.Resource(v1alpha1.RolloutGVR).Namespace("test-ns").Get(context.Background(), "example-worker", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if updated.GetAnnotations()[annotations.TrafficOverrideHashAnnotation] != "" || updated.GetAnnotations()[annotations.TrafficOverrideSourceHashAnnotation] != "" {
		t.Fatalf("forward promotion must clear traffic rollback annotations: %v", updated.GetAnnotations())
	}
}

func TestForwardPromotionKeepsTrafficRollbackStateWhenSafetyCheckFails(t *testing.T) {
	rollout := newCanaryRollout("test-ns", "example-worker", "next", "blue", 1, nil)
	rollout.SetAnnotations(map[string]string{
		annotations.TrafficOverrideHashAnnotation:       "blue",
		annotations.TrafficOverrideSourceHashAnnotation: "green",
	})
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout)
	controller := &Controller{dynamicClient: dynamicClient}
	target := Target{
		Name:                     "example-worker",
		Strategy:                 "canary",
		ExpectedCurrentStepIndex: int32Pointer(2),
	}

	if err := controller.prepareForwardPromotion(context.Background(), "test-ns", []Target{target}); err == nil {
		t.Fatal("expected a stale step safety check to reject forward promotion")
	}
	updated, err := dynamicClient.Resource(v1alpha1.RolloutGVR).Namespace("test-ns").Get(context.Background(), "example-worker", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if updated.GetAnnotations()[annotations.TrafficOverrideHashAnnotation] != "blue" || updated.GetAnnotations()[annotations.TrafficOverrideSourceHashAnnotation] != "green" {
		t.Fatalf("failed safety check must preserve traffic rollback annotations: %v", updated.GetAnnotations())
	}
}

func TestFirstPromotionCapturesStableTrafficBeforeCutover(t *testing.T) {
	rollout := newCanaryRollout("test-ns", "example-worker", "green", "blue", 1, nil)
	rollout.SetUID(types.UID("rollout-uid"))
	controllerRef := true
	replicas := int32(1)
	stableRS := newReplicaSet("test-ns", "stable-rs", "blue")
	stableRS.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: "argoproj.io/v1alpha1",
		Kind:       "Rollout",
		Name:       "example-worker",
		UID:        types.UID("rollout-uid"),
		Controller: &controllerRef,
	}}
	stableRS.Spec.Replicas = &replicas
	stableRS.Status.AvailableReplicas = replicas
	stablePod := newPod("test-ns", "stable-pod", "blue")
	stablePod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	promotion := &v1alpha1.RolloutPromotion{ObjectMeta: metav1.ObjectMeta{Name: "first-cut", Namespace: "test-ns", Generation: 1}}
	controller := &Controller{
		kubeClient:    kubefake.NewSimpleClientset(stableRS, stablePod),
		dynamicClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout),
	}
	spec := RequestSpec{
		RequestID: "first-cut",
		Action:    "promote",
		Strategy:  "canary",
		TrafficRollback: TrafficRollbackOptions{
			Enabled:      true,
			Mode:         v1alpha1.RolloutPromotionTrafficRollbackPrevious,
			HistoryLimit: 10,
		},
		Targets: []Target{{
			Name:     "example-worker",
			Strategy: "canary",
			WeightSwitch: &WeightSwitch{
				Enabled:        true,
				AnnotationKey:  "example.com/traffic-weight",
				BaseSelector:   "app=example,component=data",
				ActiveWeight:   "active",
				InactiveWeight: "inactive",
			},
		}},
	}
	status, err := controller.captureTrafficSnapshot(context.Background(), promotion, spec, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	snapshots, ok := status["trafficSnapshots"].([]v1alpha1.RolloutPromotionTrafficSnapshot)
	if !ok || len(snapshots) != 1 || len(snapshots[0].Targets) != 1 {
		t.Fatalf("unexpected first traffic snapshot: %#v", status["trafficSnapshots"])
	}
	if snapshots[0].Targets[0].ActiveHash != "blue" {
		t.Fatalf("first cutover must capture stable blue, got %+v", snapshots[0].Targets[0])
	}

	withSnapshot, rollbackStatus, captured, err := controller.ensureTrafficSnapshotForRollback(context.Background(), promotion, spec, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if !captured || len(withSnapshot.Status.TrafficSnapshots) != 1 || withSnapshot.Status.TrafficSnapshots[0].Targets[0].ActiveHash != "blue" {
		t.Fatalf("immediate first rollback must capture the live blue baseline: captured=%t status=%+v", captured, withSnapshot.Status)
	}
	if len(rollbackStatus["trafficSnapshots"].([]v1alpha1.RolloutPromotionTrafficSnapshot)) != 1 {
		t.Fatalf("immediate first rollback status must include the captured baseline: %+v", rollbackStatus)
	}
}

func TestTrafficRollbackRequestInterruptsForwardWait(t *testing.T) {
	enabled := true
	promotion := &v1alpha1.RolloutPromotion{
		ObjectMeta: metav1.ObjectMeta{Name: "first-cut", Namespace: "test-ns"},
		Spec: v1alpha1.RolloutPromotionSpec{
			TrafficRollback: &v1alpha1.RolloutPromotionTrafficRollback{Enabled: &enabled},
		},
	}
	rollout := newCanaryRollout("test-ns", "example-worker", "green", "blue", 1, nil)
	rolloutsClient := rolloutfake.NewSimpleClientset(promotion)
	controller := &Controller{
		kubeClient:     kubefake.NewSimpleClientset(),
		dynamicClient:  dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout),
		rolloutsClient: rolloutsClient,
	}

	updateErr := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		live, err := rolloutsClient.ArgoprojV1alpha1().RolloutPromotions("test-ns").Get(context.Background(), "first-cut", metav1.GetOptions{})
		if err != nil {
			updateErr <- err
			return
		}
		live.Annotations = map[string]string{annotations.TrafficRollbackRequestAnnotation: "rollback-during-first-cut"}
		_, err = rolloutsClient.ArgoprojV1alpha1().RolloutPromotions("test-ns").Update(context.Background(), live, metav1.UpdateOptions{})
		updateErr <- err
	}()

	started := time.Now()
	_, err := controller.waitTarget(
		context.Background(),
		promotion,
		"test-ns",
		Target{Name: "example-worker"},
		WaitOptions{Attempts: 2, Seconds: 5},
		map[string]interface{}{"strategy": "canary", "previousStepIndex": int64(1)},
	)
	var rollbackErr *trafficRollbackRequestedError
	if !errors.As(err, &rollbackErr) {
		t.Fatalf("expected traffic rollback interruption, got %v", err)
	}
	if rollbackErr.requestID != "rollback-during-first-cut" {
		t.Fatalf("unexpected rollback request: %+v", rollbackErr)
	}
	if elapsed := time.Since(started); elapsed >= 3*time.Second {
		t.Fatalf("rollback request should preempt a five-second wait, elapsed=%s", elapsed)
	}
	if err := <-updateErr; err != nil {
		t.Fatal(err)
	}
}

func TestFirstCanaryReconcileCanBeInterruptedByTrafficRollback(t *testing.T) {
	enabled := true
	promotion := &v1alpha1.RolloutPromotion{
		ObjectMeta: metav1.ObjectMeta{Name: "first-cut", Namespace: "test-ns", Generation: 1},
		Spec: v1alpha1.RolloutPromotionSpec{
			RequestID: "first-cut",
			Action:    "promote",
			Strategy:  "canary",
			Mode:      "together",
			TrafficRollback: &v1alpha1.RolloutPromotionTrafficRollback{
				Enabled:      &enabled,
				Mode:         v1alpha1.RolloutPromotionTrafficRollbackPrevious,
				HistoryLimit: 10,
			},
			Wait: v1alpha1.RolloutPromotionWait{Enabled: &enabled, Attempts: 3, Seconds: 5},
			Rollouts: []v1alpha1.RolloutPromotionTarget{{
				Name:     "example-worker",
				Strategy: "canary",
				WeightSwitch: &v1alpha1.RolloutPromotionWeightSwitch{
					Enabled:        &enabled,
					AnnotationKey:  "example.com/traffic-weight",
					BaseSelector:   "app=example,component=data",
					ActiveWeight:   "active",
					InactiveWeight: "inactive",
				},
			}},
		},
	}

	pauseConditions := []interface{}{map[string]interface{}{"reason": "CanaryPauseStep"}}
	rollout := newCanaryRollout("test-ns", "example-worker", "green", "blue", 1, pauseConditions)
	rollout.SetUID(types.UID("rollout-uid"))
	controllerRef := true
	replicas := int32(1)
	readyCondition := []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	owner := []metav1.OwnerReference{{
		APIVersion: "argoproj.io/v1alpha1",
		Kind:       "Rollout",
		Name:       "example-worker",
		UID:        types.UID("rollout-uid"),
		Controller: &controllerRef,
	}}
	stableRS := newReplicaSet("test-ns", "stable-rs", "blue")
	stableRS.OwnerReferences = owner
	stableRS.Spec.Replicas = &replicas
	stableRS.Status.AvailableReplicas = replicas
	canaryRS := newReplicaSet("test-ns", "canary-rs", "green")
	canaryRS.OwnerReferences = owner
	canaryRS.Spec.Replicas = &replicas
	canaryRS.Status.AvailableReplicas = replicas
	stablePod := newPod("test-ns", "stable-pod", "blue")
	stablePod.Status.Conditions = readyCondition
	canaryPod := newPod("test-ns", "canary-pod", "green")
	canaryPod.Status.Conditions = readyCondition

	kubeClient := kubefake.NewSimpleClientset(stableRS, canaryRS, stablePod, canaryPod)
	rolloutsClient := rolloutfake.NewSimpleClientset(promotion)
	informer := cache.NewSharedIndexInformer(nil, &v1alpha1.RolloutPromotion{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	if err := informer.GetIndexer().Add(promotion); err != nil {
		t.Fatal(err)
	}
	controller := &Controller{
		kubeClient:     kubeClient,
		dynamicClient:  dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout),
		rolloutsClient: rolloutsClient,
		informer:       informer,
	}

	updateErr := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		promotions := rolloutsClient.ArgoprojV1alpha1().RolloutPromotions("test-ns")
		live, err := promotions.Get(context.Background(), "first-cut", metav1.GetOptions{})
		if err != nil {
			updateErr <- err
			return
		}
		live.Annotations = map[string]string{annotations.TrafficRollbackRequestAnnotation: "rollback-first-cut"}
		_, err = promotions.Update(context.Background(), live, metav1.UpdateOptions{})
		updateErr <- err
	}()

	if err := controller.reconcile(context.Background(), "test-ns/first-cut"); err != nil {
		t.Fatal(err)
	}
	if err := <-updateErr; err != nil {
		t.Fatal(err)
	}
	live, err := rolloutsClient.ArgoprojV1alpha1().RolloutPromotions("test-ns").Get(context.Background(), "first-cut", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if live.Status.Phase != v1alpha1.RolloutPromotionPhaseRunning || live.Status.Operation == nil || live.Status.Operation.Phase != v1alpha1.RolloutPromotionOperationStalled {
		t.Fatalf("interrupted forward operation must remain recoverable: %+v", live.Status)
	}
	// Simulate a fresh worker after the process which was waiting on the forward
	// action exited. The rollback annotation is a distinct durable transaction.
	if err := controller.reconcile(context.Background(), "test-ns/first-cut"); err != nil {
		t.Fatal(err)
	}
	live, err = rolloutsClient.ArgoprojV1alpha1().RolloutPromotions("test-ns").Get(context.Background(), "first-cut", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if live.Status.Phase != v1alpha1.RolloutPromotionPhaseSucceeded || live.Status.LastTrafficRollbackRequest != "rollback-first-cut" {
		t.Fatalf("unexpected promotion status: %+v", live.Status)
	}
	if !containsString(live.Status.ProcessedRequestIDs, "first-cut") {
		t.Fatalf("an interrupted forward request must be recorded as processed after traffic rollback: %+v", live.Status.ProcessedRequestIDs)
	}
	if len(live.Status.TrafficSnapshots) != 1 || live.Status.TrafficSnapshots[0].Targets[0].ActiveHash != "blue" {
		t.Fatalf("expected first-cut snapshot to retain blue traffic: %+v", live.Status.TrafficSnapshots)
	}
	stablePod, _ = kubeClient.CoreV1().Pods("test-ns").Get(context.Background(), "stable-pod", metav1.GetOptions{})
	canaryPod, _ = kubeClient.CoreV1().Pods("test-ns").Get(context.Background(), "canary-pod", metav1.GetOptions{})
	if stablePod.Annotations["example.com/traffic-weight"] != "active" || canaryPod.Annotations["example.com/traffic-weight"] != "inactive" {
		t.Fatalf("unexpected rollback weights stable=%q canary=%q", stablePod.Annotations["example.com/traffic-weight"], canaryPod.Annotations["example.com/traffic-weight"])
	}
	for _, action := range kubeClient.Actions() {
		if action.GetVerb() == "create" || action.GetVerb() == "delete" {
			t.Fatalf("interrupted traffic rollback must not create or delete workload objects: %s %s", action.GetVerb(), action.GetResource().Resource)
		}
	}

	// The informer intentionally still contains the stale pre-reconcile object.
	// A second queue delivery must consult live status and remain a no-op.
	if err := controller.reconcile(context.Background(), "test-ns/first-cut"); err != nil {
		t.Fatal(err)
	}
	live, err = rolloutsClient.ArgoprojV1alpha1().RolloutPromotions("test-ns").Get(context.Background(), "first-cut", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if live.Status.Phase != v1alpha1.RolloutPromotionPhaseSucceeded || live.Status.LastTrafficRollbackRequest != "rollback-first-cut" {
		t.Fatalf("stale informer replay changed completed promotion status: %+v", live.Status)
	}
}

func TestResolveStableRollbackRevision(t *testing.T) {
	rollout := newCanaryRollout("test-ns", "example-worker", "green", "blue", 1, nil)
	rollout.SetUID(types.UID("rollout-uid"))
	controllerRef := true
	stableRS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "example-worker-blue",
			Labels: map[string]string{
				v1alpha1.DefaultRolloutUniqueLabelKey: "blue",
			},
			Annotations: map[string]string{
				annotations.RevisionAnnotation: "12",
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "argoproj.io/v1alpha1",
				Kind:       "Rollout",
				Name:       "example-worker",
				UID:        types.UID("rollout-uid"),
				Controller: &controllerRef,
			}},
		},
	}
	controller := &Controller{
		kubeClient:    kubefake.NewSimpleClientset(stableRS),
		dynamicClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout),
	}
	target := Target{Name: "example-worker", RollbackTarget: "stable"}
	revision, err := controller.resolveRollbackRevision(context.Background(), "test-ns", target, map[string]interface{}{"stableRS": "blue"})
	if err != nil {
		t.Fatal(err)
	}
	if revision != 12 {
		t.Fatalf("expected stable revision 12, got %d", revision)
	}

	revision, err = controller.resolveRollbackRevision(context.Background(), "test-ns", Target{Name: "example-worker", ToRevision: 7}, nil)
	if err != nil || revision != 7 {
		t.Fatalf("expected explicit revision 7, got revision=%d err=%v", revision, err)
	}
}

func TestControlTargetRollbackToStable(t *testing.T) {
	rollout := newCanaryRollout("test-ns", "example-worker", "green", "blue", 1, nil)
	rollout.SetUID(types.UID("rollout-uid"))
	_ = unstructured.SetNestedMap(rollout.Object, map[string]interface{}{
		"matchLabels": map[string]interface{}{"app": "example"},
	}, "spec", "selector")
	_ = unstructured.SetNestedMap(rollout.Object, map[string]interface{}{
		"metadata": map[string]interface{}{
			"labels": map[string]interface{}{"app": "example"},
		},
		"spec": map[string]interface{}{
			"containers": []interface{}{map[string]interface{}{
				"name":  "app",
				"image": "example:new",
			}},
		},
	}, "spec", "template")

	controllerRef := true
	stableRS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "example-worker-blue",
			Labels: map[string]string{
				"app":                                 "example",
				v1alpha1.DefaultRolloutUniqueLabelKey: "blue",
			},
			Annotations: map[string]string{annotations.RevisionAnnotation: "12"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "argoproj.io/v1alpha1",
				Kind:       "Rollout",
				Name:       "example-worker",
				UID:        types.UID("rollout-uid"),
				Controller: &controllerRef,
			}},
		},
		Spec: appsv1.ReplicaSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
					"app":                                 "example",
					v1alpha1.DefaultRolloutUniqueLabelKey: "blue",
				}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "example:stable"}}},
			},
		},
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout)
	controller := &Controller{
		kubeClient:    kubefake.NewSimpleClientset(stableRS),
		dynamicClient: dynamicClient,
	}
	target := Target{Name: "example-worker", Strategy: "canary", RollbackTarget: "stable"}
	state, err := controller.controlTarget(context.Background(), "test-ns", RequestSpec{Action: "rollback"}, target)
	if err != nil {
		t.Fatal(err)
	}
	if state["resolvedToRevision"] != int64(12) {
		t.Fatalf("expected resolved revision 12, got %+v", state)
	}

	updated, err := dynamicClient.Resource(v1alpha1.RolloutGVR).Namespace("test-ns").Get(context.Background(), "example-worker", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	containers, found, err := unstructured.NestedSlice(updated.Object, "spec", "template", "spec", "containers")
	if err != nil || !found || len(containers) != 1 {
		t.Fatalf("unexpected restored containers: found=%v containers=%+v err=%v", found, containers, err)
	}
	container := containers[0].(map[string]interface{})
	if container["image"] != "example:stable" {
		t.Fatalf("expected stable image, got %+v", container)
	}
}

func TestObserveCanaryState(t *testing.T) {
	rollout := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Rollout",
		"metadata": map[string]interface{}{
			"namespace": "test-ns",
			"name":      "example",
		},
		"spec": map[string]interface{}{
			"strategy": map[string]interface{}{
				"canary": map[string]interface{}{
					"stableService": "example-stable",
					"canaryService": "example-canary",
					"steps": []interface{}{
						map[string]interface{}{"setWeight": int64(10)},
						map[string]interface{}{"pause": map[string]interface{}{}},
					},
				},
			},
		},
		"status": map[string]interface{}{
			"currentPodHash":   "green",
			"stableRS":         "blue",
			"currentStepIndex": int64(1),
			"pauseConditions":  []interface{}{map[string]interface{}{"reason": "CanaryPauseStep"}},
		},
	}}
	rollout.SetGroupVersionKind(v1alpha1.SchemeGroupVersion.WithKind("Rollout"))
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "example-stable", Namespace: "test-ns"}, Spec: corev1.ServiceSpec{Selector: map[string]string{rolloutHashLabel: "blue"}}}
	controller := &Controller{
		kubeClient:    kubefake.NewSimpleClientset(service),
		dynamicClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout),
	}
	state, err := controller.observeTarget(context.Background(), "test-ns", Target{Name: "example"})
	if err != nil {
		t.Fatal(err)
	}
	if state["strategy"] != "canary" || state["currentStepIndex"] != int64(1) || state["promoted"] != false {
		t.Fatalf("unexpected canary state: %+v", state)
	}
}

func TestWaitTargetForCanaryStepHonorsPollingInterval(t *testing.T) {
	rollout := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Rollout",
		"metadata": map[string]interface{}{
			"namespace": "test-ns",
			"name":      "example",
		},
		"spec": map[string]interface{}{
			"strategy": map[string]interface{}{
				"canary": map[string]interface{}{
					"steps": []interface{}{
						map[string]interface{}{"setWeight": int64(0)},
						map[string]interface{}{"pause": map[string]interface{}{}},
						map[string]interface{}{"setWeight": int64(10)},
						map[string]interface{}{"pause": map[string]interface{}{}},
					},
				},
			},
		},
		"status": map[string]interface{}{
			"currentPodHash":   "green",
			"stableRS":         "blue",
			"currentStepIndex": int64(1),
			"pauseConditions":  []interface{}{map[string]interface{}{"reason": "CanaryPauseStep"}},
		},
	}}
	rollout.SetGroupVersionKind(v1alpha1.SchemeGroupVersion.WithKind("Rollout"))
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout)
	controller := &Controller{
		kubeClient:    kubefake.NewSimpleClientset(),
		dynamicClient: dynamicClient,
	}

	updateErr := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		resource := dynamicClient.Resource(v1alpha1.RolloutGVR).Namespace("test-ns")
		updated, err := resource.Get(context.Background(), "example", metav1.GetOptions{})
		if err != nil {
			updateErr <- err
			return
		}
		_ = unstructured.SetNestedField(updated.Object, int64(3), "status", "currentStepIndex")
		_, err = resource.Update(context.Background(), updated, metav1.UpdateOptions{})
		updateErr <- err
	}()

	state, err := controller.waitTarget(
		context.Background(),
		nil,
		"test-ns",
		Target{Name: "example"},
		WaitOptions{Attempts: 3, Seconds: 1},
		map[string]interface{}{"strategy": "canary", "previousStepIndex": int64(1)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if currentStepIndex, _ := state["currentStepIndex"].(int64); currentStepIndex != 3 {
		t.Fatalf("expected currentStepIndex=3, got state=%+v", state)
	}
	if err := <-updateErr; err != nil {
		t.Fatal(err)
	}
}

func TestObserveTargetPromoted(t *testing.T) {
	rollout := newRollout("test-ns", "example-worker", "abc", "abc", "abc", nil)
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "example-worker-active",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				v1alpha1.DefaultRolloutUniqueLabelKey: "abc",
			},
		},
	}

	controller := &Controller{
		kubeClient:    kubefake.NewSimpleClientset(service),
		dynamicClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout),
	}

	state, err := controller.observeTarget(context.Background(), "test-ns", Target{
		Name:          "example-worker",
		ActiveService: "example-worker-active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if promoted, _ := state["promoted"].(bool); !promoted {
		t.Fatalf("expected promoted state, got %+v", state)
	}
}

func TestObserveTargetPausedIsNotPromoted(t *testing.T) {
	rollout := newRollout("test-ns", "example-worker", "abc", "abc", "abc", []interface{}{
		map[string]interface{}{"reason": "BlueGreenPause"},
	})
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "example-worker-active",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				v1alpha1.DefaultRolloutUniqueLabelKey: "abc",
			},
		},
	}

	controller := &Controller{
		kubeClient:    kubefake.NewSimpleClientset(service),
		dynamicClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout),
	}

	state, err := controller.observeTarget(context.Background(), "test-ns", Target{
		Name:          "example-worker",
		ActiveService: "example-worker-active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if promoted, _ := state["promoted"].(bool); promoted {
		t.Fatalf("expected paused rollout to be unpromoted, got %+v", state)
	}
}

func TestFirstAdoptionStaysPendingUntilExplicitPromotion(t *testing.T) {
	rollout := newCanaryRollout("test-ns", "example-worker", "baseline", "baseline", 3, nil)
	rollout.SetAnnotations(map[string]string{
		annotations.LegacyAdoptionStateAnnotation:            annotations.LegacyAdoptionStatePending,
		annotations.LegacyAdoptionBaseSelectorAnnotation:     "app=example,component=data",
		annotations.LegacyAdoptionWeightAnnotationAnnotation: "example.com/traffic-weight",
		annotations.LegacyAdoptionActiveWeightAnnotation:     "active",
		annotations.LegacyAdoptionCandidateWeightAnnotation:  "preview",
		annotations.LegacyAdoptionLegacyWeightAnnotation:     "legacy",
		annotations.LegacyAdoptionInactiveWeightAnnotation:   "inactive",
	})
	legacyPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Namespace: "test-ns", Name: "legacy-pod", Labels: map[string]string{"app": "example", "component": "data"},
	}}
	legacyRS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test-ns", Name: "legacy-rs", Labels: map[string]string{"app": "example", "component": "data"}},
		Spec:       appsv1.ReplicaSetSpec{Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}},
	}
	candidatePod := newPod("test-ns", "candidate-pod", "baseline")
	candidateRS := newReplicaSet("test-ns", "candidate-rs", "baseline")
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout)
	kubeClient := kubefake.NewSimpleClientset(legacyPod, legacyRS, candidatePod, candidateRS)
	controller := &Controller{kubeClient: kubeClient, dynamicClient: dynamicClient}
	target := Target{
		Name:                   "example-worker",
		Strategy:               "canary",
		ExpectedCurrentPodHash: "baseline",
		ExpectedStableRS:       "baseline",
	}

	state, err := controller.observeTarget(context.Background(), "test-ns", target)
	if err != nil {
		t.Fatal(err)
	}
	if state["promoted"] == true || state["legacyAdoptionPending"] != true {
		t.Fatalf("first adoption must remain pending before an explicit request: %+v", state)
	}
	promotion := &v1alpha1.RolloutPromotion{
		ObjectMeta: metav1.ObjectMeta{Name: "first-adoption", Namespace: "test-ns"},
		Status: v1alpha1.RolloutPromotionStatus{Targets: []v1alpha1.RolloutPromotionTargetStatus{{
			Name:              "example-worker",
			Phase:             "Promoted",
			ActionApplied:     true,
			PreviousStepIndex: 3,
		}}},
	}
	state, err = controller.resumePromoteTarget(context.Background(), promotion, target)
	if err != nil {
		t.Fatal(err)
	}
	if state["legacyAdoptionPending"] == true {
		t.Fatalf("explicit promotion did not complete adoption: %+v", state)
	}
	legacyPod, _ = kubeClient.CoreV1().Pods("test-ns").Get(context.Background(), "legacy-pod", metav1.GetOptions{})
	candidatePod, _ = kubeClient.CoreV1().Pods("test-ns").Get(context.Background(), "candidate-pod", metav1.GetOptions{})
	if legacyPod.Annotations["example.com/traffic-weight"] != "inactive" || candidatePod.Annotations["example.com/traffic-weight"] != "active" {
		t.Fatalf("explicit promotion did not switch first-adoption traffic: legacy=%q candidate=%q", legacyPod.Annotations["example.com/traffic-weight"], candidatePod.Annotations["example.com/traffic-weight"])
	}
	updated, err := dynamicClient.Resource(v1alpha1.RolloutGVR).Namespace("test-ns").Get(context.Background(), "example-worker", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if updated.GetAnnotations()[annotations.LegacyAdoptionStateAnnotation] != annotations.LegacyAdoptionStatePromoted {
		t.Fatalf("promotion state was not persisted: %+v", updated.GetAnnotations())
	}
	if updated.GetAnnotations()[annotations.LegacyAdoptionPromotedAnnotation] != "true" {
		t.Fatalf("durable promotion marker was not persisted: %+v", updated.GetAnnotations())
	}
}

func TestTerminalForwardRepairsStaleLegacyAdoptionState(t *testing.T) {
	waitEnabled := false
	commitTime := metav1.Now()
	promotion := &v1alpha1.RolloutPromotion{
		ObjectMeta: metav1.ObjectMeta{Name: "terminal-adoption-heal", Namespace: "test-ns", Generation: 2},
		Spec: v1alpha1.RolloutPromotionSpec{
			RequestID: "first-adoption",
			Action:    "promote",
			Strategy:  "canary",
			Wait:      v1alpha1.RolloutPromotionWait{Enabled: &waitEnabled},
			Rollouts:  []v1alpha1.RolloutPromotionTarget{{Name: "example-worker", Strategy: "canary"}},
		},
		Status: v1alpha1.RolloutPromotionStatus{
			ObservedGeneration: 1,
			Phase:              v1alpha1.RolloutPromotionPhaseSucceeded,
			ProcessedRequestIDs: []string{
				"first-adoption",
			},
			Operation: &v1alpha1.RolloutPromotionOperationStatus{
				ID: "forward/first-adoption", RequestID: "first-adoption", Kind: v1alpha1.RolloutPromotionOperationForward,
				Action: "promote", Phase: v1alpha1.RolloutPromotionOperationSucceeded, ObservedGeneration: 1,
				Attempt: 1, CommitTime: &commitTime,
			},
			Targets: []v1alpha1.RolloutPromotionTargetStatus{{
				Name: "example-worker", Strategy: "canary", ActionApplied: true, Verified: true, Promoted: true,
				CurrentPodHash: "baseline", StableRS: "baseline",
			}},
		},
	}
	rollout := newCanaryRollout("test-ns", "example-worker", "baseline", "baseline", 3, nil)
	rollout.SetAnnotations(map[string]string{
		annotations.LegacyAdoptionStateAnnotation:            annotations.LegacyAdoptionStatePending,
		annotations.LegacyAdoptionBaseSelectorAnnotation:     "app=example,component=data",
		annotations.LegacyAdoptionWeightAnnotationAnnotation: "example.com/traffic-weight",
		annotations.LegacyAdoptionActiveWeightAnnotation:     "active",
		annotations.LegacyAdoptionCandidateWeightAnnotation:  "preview",
		annotations.LegacyAdoptionLegacyWeightAnnotation:     "legacy",
		annotations.LegacyAdoptionInactiveWeightAnnotation:   "inactive",
	})
	legacyPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Namespace: "test-ns", Name: "legacy-pod", Labels: map[string]string{"app": "example", "component": "data"},
		Annotations: map[string]string{"example.com/traffic-weight": "legacy"},
	}}
	candidatePod := newPod("test-ns", "candidate-pod", "baseline")
	candidatePod.Labels["app"] = "example"
	candidatePod.Labels["component"] = "data"
	candidatePod.Annotations = map[string]string{"example.com/traffic-weight": "preview"}
	candidateRS := newReplicaSet("test-ns", "candidate-rs", "baseline")
	candidateRS.Labels["app"] = "example"
	candidateRS.Labels["component"] = "data"
	if candidateRS.Spec.Template.Labels == nil {
		candidateRS.Spec.Template.Labels = map[string]string{}
	}
	candidateRS.Spec.Template.Labels["app"] = "example"
	candidateRS.Spec.Template.Labels["component"] = "data"
	rolloutsClient := rolloutfake.NewSimpleClientset(promotion)
	informer := cache.NewSharedIndexInformer(nil, &v1alpha1.RolloutPromotion{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	if err := informer.GetIndexer().Add(promotion); err != nil {
		t.Fatal(err)
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout)
	kubeClient := kubefake.NewSimpleClientset(legacyPod, candidatePod, candidateRS)
	controller := &Controller{kubeClient: kubeClient, dynamicClient: dynamicClient, rolloutsClient: rolloutsClient, informer: informer}

	if err := controller.reconcile(context.Background(), "test-ns/terminal-adoption-heal"); err != nil {
		t.Fatal(err)
	}
	updated, err := dynamicClient.Resource(v1alpha1.RolloutGVR).Namespace("test-ns").Get(context.Background(), "example-worker", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if updated.GetAnnotations()[annotations.LegacyAdoptionStateAnnotation] != annotations.LegacyAdoptionStatePromoted || updated.GetAnnotations()[annotations.LegacyAdoptionPromotedAnnotation] != "true" {
		t.Fatalf("terminal forward did not repair durable adoption state: %+v", updated.GetAnnotations())
	}
	legacyPod, _ = kubeClient.CoreV1().Pods("test-ns").Get(context.Background(), "legacy-pod", metav1.GetOptions{})
	candidatePod, _ = kubeClient.CoreV1().Pods("test-ns").Get(context.Background(), "candidate-pod", metav1.GetOptions{})
	if legacyPod.Annotations["example.com/traffic-weight"] != "inactive" || candidatePod.Annotations["example.com/traffic-weight"] != "active" {
		t.Fatalf("terminal forward did not repair traffic: legacy=%q candidate=%q", legacyPod.Annotations["example.com/traffic-weight"], candidatePod.Annotations["example.com/traffic-weight"])
	}
	livePromotion, err := rolloutsClient.ArgoprojV1alpha1().RolloutPromotions("test-ns").Get(context.Background(), promotion.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if livePromotion.Status.ObservedGeneration != 2 || livePromotion.Status.Operation == nil || livePromotion.Status.Operation.Phase != v1alpha1.RolloutPromotionOperationSucceeded {
		t.Fatalf("healing must preserve terminal operation and acknowledge generation: %+v", livePromotion.Status)
	}
}

func TestLegacyTrafficRollbackClearsDurablePromotionMarker(t *testing.T) {
	rollout := newCanaryRollout("test-ns", "example-worker", "baseline", "baseline", 3, nil)
	rollout.SetAnnotations(map[string]string{
		annotations.LegacyAdoptionStateAnnotation:    annotations.LegacyAdoptionStatePromoted,
		annotations.LegacyAdoptionPromotedAnnotation: "true",
	})
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout)
	controller := &Controller{dynamicClient: dynamicClient}

	if err := controller.patchLegacyAdoptionState(context.Background(), "test-ns", "example-worker", annotations.LegacyAdoptionStatePending); err != nil {
		t.Fatal(err)
	}
	updated, err := dynamicClient.Resource(v1alpha1.RolloutGVR).Namespace("test-ns").Get(context.Background(), "example-worker", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if updated.GetAnnotations()[annotations.LegacyAdoptionStateAnnotation] != annotations.LegacyAdoptionStatePending {
		t.Fatalf("legacy rollback state was not persisted: %+v", updated.GetAnnotations())
	}
	if _, found := updated.GetAnnotations()[annotations.LegacyAdoptionPromotedAnnotation]; found {
		t.Fatalf("legacy rollback must clear the durable promotion marker: %+v", updated.GetAnnotations())
	}
}

func TestTerminalForwardDoesNotHealRecreatedRollout(t *testing.T) {
	waitEnabled := false
	commitTime := metav1.Now()
	completionTime := metav1.NewTime(commitTime.Add(time.Minute))
	promotion := &v1alpha1.RolloutPromotion{
		ObjectMeta: metav1.ObjectMeta{Name: "terminal-old-lifecycle", Namespace: "test-ns", Generation: 1},
		Spec: v1alpha1.RolloutPromotionSpec{
			RequestID: "old-adoption", Action: "promote", Strategy: "canary",
			Wait:     v1alpha1.RolloutPromotionWait{Enabled: &waitEnabled},
			Rollouts: []v1alpha1.RolloutPromotionTarget{{Name: "example-worker", Strategy: "canary"}},
		},
		Status: v1alpha1.RolloutPromotionStatus{
			ObservedGeneration: 1, Phase: v1alpha1.RolloutPromotionPhaseSucceeded,
			Operation: &v1alpha1.RolloutPromotionOperationStatus{
				ID: "forward/old-adoption", RequestID: "old-adoption", Kind: v1alpha1.RolloutPromotionOperationForward,
				Action: "promote", Phase: v1alpha1.RolloutPromotionOperationSucceeded, CommitTime: &commitTime, CompletionTime: &completionTime,
			},
			Targets: []v1alpha1.RolloutPromotionTargetStatus{{
				Name: "example-worker", ActionApplied: true, Verified: true, Promoted: true,
				CurrentPodHash: "baseline", StableRS: "baseline", RolloutUID: "old-rollout-uid",
			}},
		},
	}
	rollout := newCanaryRollout("test-ns", "example-worker", "baseline", "baseline", 3, nil)
	rollout.SetUID(types.UID("new-rollout-uid"))
	rollout.SetCreationTimestamp(metav1.NewTime(completionTime.Add(time.Minute)))
	rollout.SetAnnotations(map[string]string{
		annotations.LegacyAdoptionStateAnnotation:        annotations.LegacyAdoptionStatePending,
		annotations.LegacyAdoptionBaseSelectorAnnotation: "app=example",
	})
	rolloutsClient := rolloutfake.NewSimpleClientset(promotion)
	informer := cache.NewSharedIndexInformer(nil, &v1alpha1.RolloutPromotion{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	if err := informer.GetIndexer().Add(promotion); err != nil {
		t.Fatal(err)
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout)
	controller := &Controller{kubeClient: kubefake.NewSimpleClientset(), dynamicClient: dynamicClient, rolloutsClient: rolloutsClient, informer: informer}

	if err := controller.reconcile(context.Background(), "test-ns/terminal-old-lifecycle"); err != nil {
		t.Fatal(err)
	}
	updated, err := dynamicClient.Resource(v1alpha1.RolloutGVR).Namespace("test-ns").Get(context.Background(), "example-worker", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if updated.GetAnnotations()[annotations.LegacyAdoptionStateAnnotation] != annotations.LegacyAdoptionStatePending {
		t.Fatalf("old lifecycle changed the recreated Rollout: %+v", updated.GetAnnotations())
	}
	if _, found := updated.GetAnnotations()[annotations.LegacyAdoptionPromotedAnnotation]; found {
		t.Fatalf("old lifecycle marked the recreated Rollout as promoted: %+v", updated.GetAnnotations())
	}
}

func TestTogetherPromotionPreflightsEveryTargetBeforeMutation(t *testing.T) {
	enabled := false
	promotion := &v1alpha1.RolloutPromotion{
		ObjectMeta: metav1.ObjectMeta{Name: "multi-target", Namespace: "test-ns", Generation: 1},
		Spec: v1alpha1.RolloutPromotionSpec{
			RequestID: "multi-target",
			Action:    "promote",
			Strategy:  "blueGreen",
			Mode:      "together",
			Wait:      v1alpha1.RolloutPromotionWait{Enabled: &enabled},
			Rollouts: []v1alpha1.RolloutPromotionTarget{
				{
					Name:                      "component-a",
					ActiveService:             "component-a-active",
					ExpectedCurrentPodHash:    "a-green",
					ExpectedStableRS:          "a-blue",
					ExpectedActiveServiceHash: "a-blue",
				},
				{
					Name:                      "component-b",
					ActiveService:             "component-b-active",
					ExpectedCurrentPodHash:    "b-green",
					ExpectedStableRS:          "b-blue",
					ExpectedActiveServiceHash: "wrong-hash",
				},
			},
		},
	}
	pauseConditions := []interface{}{map[string]interface{}{"reason": "BlueGreenPause"}}
	rolloutA := newRollout("test-ns", "component-a", "a-green", "a-blue", "a-blue", pauseConditions)
	rolloutB := newRollout("test-ns", "component-b", "b-green", "b-blue", "b-blue", pauseConditions)
	for _, rollout := range []*unstructured.Unstructured{rolloutA, rolloutB} {
		if err := unstructured.SetNestedMap(rollout.Object, map[string]interface{}{}, "spec", "strategy", "blueGreen"); err != nil {
			t.Fatal(err)
		}
	}
	serviceA := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "component-a-active", Namespace: "test-ns"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{rolloutHashLabel: "a-blue"}},
	}
	serviceB := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "component-b-active", Namespace: "test-ns"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{rolloutHashLabel: "b-blue"}},
	}
	rolloutsClient := rolloutfake.NewSimpleClientset(promotion)
	informer := cache.NewSharedIndexInformer(nil, &v1alpha1.RolloutPromotion{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	if err := informer.GetIndexer().Add(promotion); err != nil {
		t.Fatal(err)
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rolloutA, rolloutB)
	controller := &Controller{
		kubeClient:     kubefake.NewSimpleClientset(serviceA, serviceB),
		dynamicClient:  dynamicClient,
		rolloutsClient: rolloutsClient,
		informer:       informer,
	}

	if err := controller.reconcile(context.Background(), "test-ns/multi-target"); err != nil {
		t.Fatal(err)
	}
	livePromotion, err := rolloutsClient.ArgoprojV1alpha1().RolloutPromotions("test-ns").Get(context.Background(), "multi-target", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if livePromotion.Status.Phase != v1alpha1.RolloutPromotionPhaseFailed {
		t.Fatalf("expected failed safety preflight, got phase %q", livePromotion.Status.Phase)
	}
	liveA, err := dynamicClient.Resource(v1alpha1.RolloutGVR).Namespace("test-ns").Get(context.Background(), "component-a", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if paused, found, err := unstructured.NestedBool(liveA.Object, "spec", "paused"); err != nil || found {
		t.Fatalf("first target must not be mutated when a later preflight fails: paused=%v found=%v err=%v", paused, found, err)
	}
	remainingPause, _, err := unstructured.NestedSlice(liveA.Object, "status", "pauseConditions")
	if err != nil {
		t.Fatal(err)
	}
	if len(remainingPause) != 1 {
		t.Fatalf("first target pause must remain intact, got %#v", remainingPause)
	}
}

func TestTogetherAbortPreflightsEveryTargetBeforeMutation(t *testing.T) {
	enabled := false
	promotion := &v1alpha1.RolloutPromotion{
		ObjectMeta: metav1.ObjectMeta{Name: "multi-target-abort", Namespace: "test-ns", Generation: 1},
		Spec: v1alpha1.RolloutPromotionSpec{
			RequestID: "multi-target-abort",
			Action:    "abort",
			Strategy:  "blueGreen",
			Mode:      "together",
			Wait:      v1alpha1.RolloutPromotionWait{Enabled: &enabled},
			Rollouts: []v1alpha1.RolloutPromotionTarget{
				{
					Name:                      "component-a",
					ActiveService:             "component-a-active",
					ExpectedCurrentPodHash:    "a-green",
					ExpectedStableRS:          "a-blue",
					ExpectedActiveServiceHash: "a-blue",
				},
				{
					Name:                      "component-b",
					ActiveService:             "component-b-active",
					ExpectedCurrentPodHash:    "b-green",
					ExpectedStableRS:          "b-blue",
					ExpectedActiveServiceHash: "wrong-hash",
				},
			},
		},
	}
	pauseConditions := []interface{}{map[string]interface{}{"reason": "BlueGreenPause"}}
	rolloutA := newRollout("test-ns", "component-a", "a-green", "a-blue", "a-blue", pauseConditions)
	rolloutB := newRollout("test-ns", "component-b", "b-green", "b-blue", "b-blue", pauseConditions)
	for _, rollout := range []*unstructured.Unstructured{rolloutA, rolloutB} {
		if err := unstructured.SetNestedMap(rollout.Object, map[string]interface{}{}, "spec", "strategy", "blueGreen"); err != nil {
			t.Fatal(err)
		}
	}
	serviceA := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "component-a-active", Namespace: "test-ns"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{rolloutHashLabel: "a-blue"}},
	}
	serviceB := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "component-b-active", Namespace: "test-ns"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{rolloutHashLabel: "b-blue"}},
	}
	rolloutsClient := rolloutfake.NewSimpleClientset(promotion)
	informer := cache.NewSharedIndexInformer(nil, &v1alpha1.RolloutPromotion{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	if err := informer.GetIndexer().Add(promotion); err != nil {
		t.Fatal(err)
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rolloutA, rolloutB)
	controller := &Controller{
		kubeClient:     kubefake.NewSimpleClientset(serviceA, serviceB),
		dynamicClient:  dynamicClient,
		rolloutsClient: rolloutsClient,
		informer:       informer,
	}

	if err := controller.reconcile(context.Background(), "test-ns/multi-target-abort"); err != nil {
		t.Fatal(err)
	}
	liveA, err := dynamicClient.Resource(v1alpha1.RolloutGVR).Namespace("test-ns").Get(context.Background(), "component-a", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if aborted, found, err := unstructured.NestedBool(liveA.Object, "status", "abort"); err != nil || found {
		t.Fatalf("first target must not be aborted when a later preflight fails: abort=%v found=%v err=%v", aborted, found, err)
	}
}

func TestPromotionCommitsBeforeMutatingRollout(t *testing.T) {
	waitEnabled := false
	promotion := &v1alpha1.RolloutPromotion{
		ObjectMeta: metav1.ObjectMeta{Name: "commit-before-mutation", Namespace: "test-ns", Generation: 1},
		Spec: v1alpha1.RolloutPromotionSpec{
			RequestID: "commit-before-mutation",
			Action:    "promote",
			Strategy:  "canary",
			Wait:      v1alpha1.RolloutPromotionWait{Enabled: &waitEnabled},
			Rollouts: []v1alpha1.RolloutPromotionTarget{{
				Name:                     "example-worker",
				Strategy:                 "canary",
				ExpectedCurrentPodHash:   "green",
				ExpectedStableRS:         "blue",
				ExpectedCurrentStepIndex: int32Pointer(1),
			}},
		},
	}
	rollout := newCanaryRollout("test-ns", "example-worker", "green", "blue", 1, []interface{}{map[string]interface{}{"reason": "CanaryPauseStep"}})
	rolloutsClient := rolloutfake.NewSimpleClientset(promotion)
	informer := cache.NewSharedIndexInformer(nil, &v1alpha1.RolloutPromotion{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	if err := informer.GetIndexer().Add(promotion); err != nil {
		t.Fatal(err)
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout)
	mutationObserved := false
	dynamicClient.PrependReactor("patch", "rollouts", func(action k8stesting.Action) (bool, runtime.Object, error) {
		live, err := rolloutsClient.ArgoprojV1alpha1().RolloutPromotions("test-ns").Get(context.Background(), promotion.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if live.Status.Operation == nil || (live.Status.Operation.Phase != v1alpha1.RolloutPromotionOperationCommitted && live.Status.Operation.Phase != v1alpha1.RolloutPromotionOperationApplying) {
			t.Fatalf("Rollout mutation occurred without a durable commit: %+v", live.Status.Operation)
		}
		mutationObserved = true
		return false, nil, nil
	})
	controller := &Controller{
		kubeClient:     kubefake.NewSimpleClientset(),
		dynamicClient:  dynamicClient,
		rolloutsClient: rolloutsClient,
		informer:       informer,
	}

	if err := controller.reconcile(context.Background(), "test-ns/commit-before-mutation"); err != nil {
		t.Fatal(err)
	}
	if !mutationObserved {
		t.Fatal("expected the committed operation to mutate the Rollout")
	}
}

func TestTerminalOperationAcknowledgesReappliedGenerationWithoutReplay(t *testing.T) {
	waitEnabled := false
	commitTime := metav1.Now()
	promotion := &v1alpha1.RolloutPromotion{
		ObjectMeta: metav1.ObjectMeta{Name: "terminal-reapply", Namespace: "test-ns", Generation: 2},
		Spec: v1alpha1.RolloutPromotionSpec{
			RequestID: "same-request",
			Action:    "promote",
			Strategy:  "canary",
			Wait:      v1alpha1.RolloutPromotionWait{Enabled: &waitEnabled},
			Rollouts:  []v1alpha1.RolloutPromotionTarget{{Name: "example-worker", Strategy: "canary"}},
		},
		Status: v1alpha1.RolloutPromotionStatus{
			ObservedGeneration: 1,
			Phase:              v1alpha1.RolloutPromotionPhaseSucceeded,
			ProcessedRequestIDs: []string{
				"same-request",
			},
			Operation: &v1alpha1.RolloutPromotionOperationStatus{
				ID: "forward/same-request", RequestID: "same-request", Kind: v1alpha1.RolloutPromotionOperationForward,
				Action: "promote", Phase: v1alpha1.RolloutPromotionOperationSucceeded, ObservedGeneration: 1,
				Attempt: 3, CommitTime: &commitTime,
			},
		},
	}
	rolloutsClient := rolloutfake.NewSimpleClientset(promotion)
	informer := cache.NewSharedIndexInformer(nil, &v1alpha1.RolloutPromotion{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	if err := informer.GetIndexer().Add(promotion); err != nil {
		t.Fatal(err)
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	controller := &Controller{
		kubeClient:     kubefake.NewSimpleClientset(),
		dynamicClient:  dynamicClient,
		rolloutsClient: rolloutsClient,
		informer:       informer,
	}

	if err := controller.reconcile(context.Background(), "test-ns/terminal-reapply"); err != nil {
		t.Fatal(err)
	}
	live, err := rolloutsClient.ArgoprojV1alpha1().RolloutPromotions("test-ns").Get(context.Background(), promotion.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if live.Status.ObservedGeneration != 2 {
		t.Fatalf("expected observedGeneration 2, got %d", live.Status.ObservedGeneration)
	}
	if live.Status.Operation == nil || live.Status.Operation.Attempt != 3 || live.Status.Operation.Phase != v1alpha1.RolloutPromotionOperationSucceeded {
		t.Fatalf("terminal operation must remain unchanged: %+v", live.Status.Operation)
	}
	if len(dynamicClient.Actions()) != 0 {
		t.Fatalf("terminal request reapply must not mutate Rollouts: %#v", dynamicClient.Actions())
	}
}

func TestCanaryResumeAfterActionDoesNotAdvanceNextPause(t *testing.T) {
	waitEnabled := false
	commitTime := metav1.Now()
	promotion := &v1alpha1.RolloutPromotion{
		ObjectMeta: metav1.ObjectMeta{Name: "resume-canary", Namespace: "test-ns", Generation: 1},
		Spec: v1alpha1.RolloutPromotionSpec{
			RequestID: "resume-canary",
			Action:    "promote",
			Strategy:  "canary",
			Wait:      v1alpha1.RolloutPromotionWait{Enabled: &waitEnabled},
			Rollouts: []v1alpha1.RolloutPromotionTarget{{
				Name:                     "example-worker",
				Strategy:                 "canary",
				ExpectedCurrentPodHash:   "green",
				ExpectedStableRS:         "blue",
				ExpectedCurrentStepIndex: int32Pointer(1),
			}},
		},
		Status: v1alpha1.RolloutPromotionStatus{
			Phase: v1alpha1.RolloutPromotionPhaseRunning,
			Operation: &v1alpha1.RolloutPromotionOperationStatus{
				ID: "forward/resume-canary", RequestID: "resume-canary", Kind: v1alpha1.RolloutPromotionOperationForward,
				Action: "promote", Phase: v1alpha1.RolloutPromotionOperationApplying, ObservedGeneration: 1, CommitTime: &commitTime,
			},
			Targets: []v1alpha1.RolloutPromotionTargetStatus{{
				Name: "example-worker", Strategy: "canary", Phase: v1alpha1.RolloutPromotionOperationPrepared,
				BaselineCurrentPodHash: "green", PreviousStepIndex: 1,
			}},
		},
	}
	// The core Rollouts controller already consumed the first promote and reached
	// the next pause before promotion-controller persisted ActionApplied.
	rollout := newCanaryRollout("test-ns", "example-worker", "green", "blue", 2, []interface{}{map[string]interface{}{"reason": "CanaryPauseStep"}})
	rolloutsClient := rolloutfake.NewSimpleClientset(promotion)
	informer := cache.NewSharedIndexInformer(nil, &v1alpha1.RolloutPromotion{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	if err := informer.GetIndexer().Add(promotion); err != nil {
		t.Fatal(err)
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout)
	controller := &Controller{kubeClient: kubefake.NewSimpleClientset(), dynamicClient: dynamicClient, rolloutsClient: rolloutsClient, informer: informer}

	if err := controller.reconcile(context.Background(), "test-ns/resume-canary"); err != nil {
		t.Fatal(err)
	}
	for _, action := range dynamicClient.Actions() {
		if action.GetVerb() == "patch" && action.GetResource().Resource == "rollouts" {
			t.Fatalf("recovery must not promote the next canary pause: %#v", action)
		}
	}
	live, err := rolloutsClient.ArgoprojV1alpha1().RolloutPromotions("test-ns").Get(context.Background(), promotion.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if live.Status.Phase != v1alpha1.RolloutPromotionPhaseSucceeded || live.Status.Operation == nil || live.Status.Operation.Phase != v1alpha1.RolloutPromotionOperationSucceeded {
		t.Fatalf("expected recovered operation to complete: %+v", live.Status)
	}
}

func TestCommittedTimeoutStallsAndResumesSameRequest(t *testing.T) {
	waitEnabled := true
	promotion := &v1alpha1.RolloutPromotion{
		ObjectMeta: metav1.ObjectMeta{Name: "stalled-canary", Namespace: "test-ns", Generation: 1},
		Spec: v1alpha1.RolloutPromotionSpec{
			RequestID: "stalled-canary",
			Action:    "promote",
			Strategy:  "canary",
			Wait:      v1alpha1.RolloutPromotionWait{Enabled: &waitEnabled, Attempts: 1, Seconds: 1},
			Rollouts:  []v1alpha1.RolloutPromotionTarget{{Name: "example-worker", Strategy: "canary", ExpectedCurrentStepIndex: int32Pointer(1)}},
		},
	}
	rollout := newCanaryRollout("test-ns", "example-worker", "green", "blue", 1, []interface{}{map[string]interface{}{"reason": "CanaryPauseStep"}})
	rolloutsClient := rolloutfake.NewSimpleClientset(promotion)
	informer := cache.NewSharedIndexInformer(nil, &v1alpha1.RolloutPromotion{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	if err := informer.GetIndexer().Add(promotion); err != nil {
		t.Fatal(err)
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout)
	controller := &Controller{kubeClient: kubefake.NewSimpleClientset(), dynamicClient: dynamicClient, rolloutsClient: rolloutsClient, informer: informer}

	if err := controller.reconcile(context.Background(), "test-ns/stalled-canary"); err == nil {
		t.Fatal("expected the fake core controller to leave the committed operation stalled")
	}
	live, err := rolloutsClient.ArgoprojV1alpha1().RolloutPromotions("test-ns").Get(context.Background(), promotion.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if live.Status.Phase != v1alpha1.RolloutPromotionPhaseRunning || live.Status.Operation == nil || live.Status.Operation.Phase != v1alpha1.RolloutPromotionOperationStalled || containsString(live.Status.ProcessedRequestIDs, promotion.Spec.RequestID) {
		t.Fatalf("timeout must remain replayable: %+v", live.Status)
	}
	updated, err := dynamicClient.Resource(v1alpha1.RolloutGVR).Namespace("test-ns").Get(context.Background(), "example-worker", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := unstructured.SetNestedField(updated.Object, int64(2), "status", "currentStepIndex"); err != nil {
		t.Fatal(err)
	}
	if err := unstructured.SetNestedSlice(updated.Object, []interface{}{map[string]interface{}{"reason": "CanaryPauseStep"}}, "status", "pauseConditions"); err != nil {
		t.Fatal(err)
	}
	if _, err = dynamicClient.Resource(v1alpha1.RolloutGVR).Namespace("test-ns").Update(context.Background(), updated, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := controller.reconcile(context.Background(), "test-ns/stalled-canary"); err != nil {
		t.Fatal(err)
	}
	live, err = rolloutsClient.ArgoprojV1alpha1().RolloutPromotions("test-ns").Get(context.Background(), promotion.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if live.Status.Phase != v1alpha1.RolloutPromotionPhaseSucceeded || !containsString(live.Status.ProcessedRequestIDs, promotion.Spec.RequestID) {
		t.Fatalf("same committed request did not resume: %+v", live.Status)
	}
}

func TestValidateTargetExpectations(t *testing.T) {
	state := map[string]interface{}{
		"namespace":         "test-ns",
		"currentPodHash":    "green",
		"currentStepIndex":  int64(2),
		"stableRS":          "blue",
		"activeServiceHash": "blue",
	}
	target := Target{
		Name:                      "example-worker",
		ExpectedCurrentPodHash:    "green",
		ExpectedCurrentStepIndex:  int32Pointer(2),
		ExpectedStableRS:          "blue",
		ExpectedActiveServiceHash: "blue",
	}
	if err := validateTargetExpectations(target, state); err != nil {
		t.Fatalf("expected matching safety check, got %v", err)
	}

	target.ExpectedCurrentPodHash = "blue"
	if err := validateTargetExpectations(target, state); err == nil {
		t.Fatal("expected safety check mismatch")
	}
	target.ExpectedCurrentPodHash = "green"
	target.ExpectedCurrentStepIndex = int32Pointer(4)
	if err := validateTargetExpectations(target, state); err == nil {
		t.Fatal("expected current step safety check mismatch")
	}
}

func TestValidateTargetExpectationsForCompletedAbortIgnoresChangedStep(t *testing.T) {
	state := map[string]interface{}{
		"namespace":            "test-ns",
		"strategy":             "canary",
		"aborted":              true,
		"canaryWeight":         int64(0),
		"canaryWeightVerified": false,
		"currentPodHash":       "green",
		"currentStepIndex":     int64(0),
		"stableRS":             "blue",
		"activeServiceHash":    "blue",
	}
	target := Target{
		Name:                      "example-worker",
		ExpectedCurrentPodHash:    "green",
		ExpectedCurrentStepIndex:  int32Pointer(2),
		ExpectedStableRS:          "blue",
		ExpectedActiveServiceHash: "blue",
	}
	if err := validateTargetExpectationsForAction("abort", target, state); err != nil {
		t.Fatalf("expected completed abort to tolerate its reset step index, got %v", err)
	}

	target.ExpectedStableRS = "other"
	if err := validateTargetExpectationsForAction("abort", target, state); err == nil {
		t.Fatal("expected completed abort to keep enforcing hash identity checks")
	}
}

func TestTargetAlreadyPromotedToExpectedSkipsOldStableGuard(t *testing.T) {
	state := map[string]interface{}{
		"promoted":          true,
		"currentPodHash":    "green",
		"stableRS":          "green",
		"activeServiceHash": "green",
	}
	target := Target{
		Name:                      "example-worker",
		ExpectedCurrentPodHash:    "green",
		ExpectedStableRS:          "blue",
		ExpectedActiveServiceHash: "blue",
	}
	if !targetAlreadyPromotedToExpected(target, state) {
		t.Fatal("expected already-promoted target to be idempotent")
	}
	target.ExpectedCurrentPodHash = "other"
	if targetAlreadyPromotedToExpected(target, state) {
		t.Fatal("expected mismatched current hash to remain unsafe")
	}
}

func TestReconcileAlreadyPromotedRequestSucceedsWithoutMutation(t *testing.T) {
	waitEnabled := true
	trafficRollbackEnabled := true
	rollout := newCanaryRollout("test-ns", "example-worker", "stable", "stable", 3, nil)
	rollout.SetUID(types.UID("rollout-uid"))
	if err := unstructured.SetNestedField(rollout.Object, "Healthy", "status", "phase"); err != nil {
		t.Fatal(err)
	}
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test-ns", Name: "example-worker-active"},
		Spec: corev1.ServiceSpec{Selector: map[string]string{
			v1alpha1.DefaultRolloutUniqueLabelKey: "stable",
		}},
	}
	promotion := &v1alpha1.RolloutPromotion{
		ObjectMeta: metav1.ObjectMeta{Name: "already-promoted", Namespace: "test-ns", Generation: 1},
		Spec: v1alpha1.RolloutPromotionSpec{
			RequestID: "already-promoted-request",
			Action:    "promote",
			Strategy:  "canary",
			Mode:      "together",
			Wait:      v1alpha1.RolloutPromotionWait{Enabled: &waitEnabled},
			TrafficRollback: &v1alpha1.RolloutPromotionTrafficRollback{
				Enabled: &trafficRollbackEnabled,
			},
			Rollouts: []v1alpha1.RolloutPromotionTarget{{
				Name:                      "example-worker",
				Strategy:                  "canary",
				ActiveService:             "example-worker-active",
				ExpectedRolloutUID:        "rollout-uid",
				ExpectedCurrentPodHash:    "stable",
				ExpectedStableRS:          "stable",
				ExpectedActiveServiceHash: "stable",
				ExpectedCurrentStepIndex:  int32Pointer(3),
				WeightSwitch: &v1alpha1.RolloutPromotionWeightSwitch{
					Enabled:        &trafficRollbackEnabled,
					AnnotationKey:  "example.com/traffic-weight",
					BaseSelector:   "app=example",
					ActiveWeight:   "active",
					InactiveWeight: "inactive",
				},
			}},
		},
	}
	rolloutsClient := rolloutfake.NewSimpleClientset(promotion)
	informer := cache.NewSharedIndexInformer(nil, &v1alpha1.RolloutPromotion{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	if err := informer.GetIndexer().Add(promotion); err != nil {
		t.Fatal(err)
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout)
	controller := &Controller{
		kubeClient:     kubefake.NewSimpleClientset(service),
		dynamicClient:  dynamicClient,
		rolloutsClient: rolloutsClient,
		informer:       informer,
	}

	if err := controller.reconcile(context.Background(), "test-ns/already-promoted"); err != nil {
		t.Fatal(err)
	}
	live, err := rolloutsClient.ArgoprojV1alpha1().RolloutPromotions("test-ns").Get(context.Background(), promotion.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if live.Status.Phase != v1alpha1.RolloutPromotionPhaseSucceeded {
		t.Fatalf("expected already-promoted request to succeed, got %+v", live.Status)
	}
	if len(live.Status.Conditions) != 1 || live.Status.Conditions[0].Reason != "AlreadyPromoted" {
		t.Fatalf("expected AlreadyPromoted condition, got %+v", live.Status.Conditions)
	}
	if live.Status.Operation == nil || live.Status.Operation.Phase != v1alpha1.RolloutPromotionOperationSucceeded {
		t.Fatalf("expected terminal no-op operation, got %+v", live.Status.Operation)
	}
	if len(live.Status.Targets) != 1 || !live.Status.Targets[0].ActionAlreadyComplete || live.Status.Targets[0].ActionApplied {
		t.Fatalf("expected an explicit no-op target result, got %+v", live.Status.Targets)
	}
	if len(live.Status.TrafficSnapshots) != 0 {
		t.Fatalf("no-op request must not capture a rollback snapshot: %+v", live.Status.TrafficSnapshots)
	}
	if !containsString(live.Status.ProcessedRequestIDs, promotion.Spec.RequestID) {
		t.Fatalf("no-op request was not recorded as processed: %+v", live.Status.ProcessedRequestIDs)
	}
	for _, action := range dynamicClient.Actions() {
		if action.GetVerb() == "patch" || action.GetVerb() == "update" {
			t.Fatalf("already-promoted request mutated the Rollout: %#v", action)
		}
	}
}

func TestForwardPromotionAlreadyCompleteRequiresHealthyConvergedTraffic(t *testing.T) {
	tests := []struct {
		name            string
		phase           string
		overrideHash    string
		serviceHash     string
		alreadyPromoted bool
	}{
		{name: "healthy and converged", phase: "Healthy", serviceHash: "stable", alreadyPromoted: true},
		{name: "rollout still progressing", phase: "Progressing", serviceHash: "stable"},
		{name: "traffic rollback override remains", phase: "Healthy", overrideHash: "previous", serviceHash: "stable"},
		{name: "active service has not converged", phase: "Healthy", serviceHash: "previous"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rollout := newCanaryRollout("test-ns", "example-worker", "stable", "stable", 3, nil)
			if err := unstructured.SetNestedField(rollout.Object, tt.phase, "status", "phase"); err != nil {
				t.Fatal(err)
			}
			if tt.overrideHash != "" {
				rollout.SetAnnotations(map[string]string{annotations.TrafficOverrideHashAnnotation: tt.overrideHash})
			}
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Namespace: "test-ns", Name: "example-worker-active"},
				Spec: corev1.ServiceSpec{Selector: map[string]string{
					v1alpha1.DefaultRolloutUniqueLabelKey: tt.serviceHash,
				}},
			}
			controller := &Controller{
				kubeClient:    kubefake.NewSimpleClientset(service),
				dynamicClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout),
			}
			target := Target{
				Name:                   "example-worker",
				Strategy:               "canary",
				ActiveService:          "example-worker-active",
				ExpectedCurrentPodHash: "stable",
			}
			alreadyPromoted, _, err := controller.forwardPromotionAlreadyComplete(context.Background(), "test-ns", []Target{target})
			if err != nil {
				t.Fatal(err)
			}
			if alreadyPromoted != tt.alreadyPromoted {
				t.Fatalf("expected alreadyPromoted=%v, got %v", tt.alreadyPromoted, alreadyPromoted)
			}
		})
	}
}

func TestPromoteTargetTreatsSafetySnapshotWithoutPendingRevisionAsNoOp(t *testing.T) {
	rollout := newRollout("test-ns", "example-worker", "stable", "stable", "stable", nil)
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test-ns", Name: "example-worker-active"},
		Spec: corev1.ServiceSpec{Selector: map[string]string{
			v1alpha1.DefaultRolloutUniqueLabelKey: "stable",
		}},
	}
	controller := &Controller{
		kubeClient:    kubefake.NewSimpleClientset(service),
		dynamicClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout),
	}
	target := Target{
		Name:                      "example-worker",
		ActiveService:             "example-worker-active",
		ExpectedCurrentPodHash:    "stable",
		ExpectedStableRS:          "stable",
		ExpectedActiveServiceHash: "stable",
	}

	state, err := controller.promoteTarget(context.Background(), "test-ns", target)
	if err != nil {
		t.Fatal(err)
	}
	if alreadyComplete, _ := state["actionAlreadyComplete"].(bool); !alreadyComplete {
		t.Fatalf("expected a promotion request without a pending revision to be a no-op, got %+v", state)
	}
}

func TestPromoteTargetRejectsCanaryWithoutPause(t *testing.T) {
	rollout := newCanaryRollout("test-ns", "example-worker", "green", "blue", 0, nil)
	controller := &Controller{
		kubeClient:    kubefake.NewSimpleClientset(),
		dynamicClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout),
	}
	target := Target{
		Name:                     "example-worker",
		Strategy:                 "canary",
		ExpectedCurrentPodHash:   "green",
		ExpectedStableRS:         "blue",
		ExpectedCurrentStepIndex: int32Pointer(0),
	}

	state, err := controller.promoteTarget(context.Background(), "test-ns", target)
	if err == nil || !strings.Contains(err.Error(), "is not paused at canary step 0") {
		t.Fatalf("expected an unpaused canary promotion to be rejected, got state=%+v err=%v", state, err)
	}
	if state["currentStepIndex"] != int64(0) {
		t.Fatalf("expected rejected promotion to preserve step 0, got %+v", state)
	}
}

func TestPromoteTargetClearsCanaryPauseWithoutSkippingStep(t *testing.T) {
	pauseConditions := []interface{}{map[string]interface{}{"reason": "CanaryPauseStep"}}
	rollout := newCanaryRollout("test-ns", "example-worker", "green", "blue", 1, pauseConditions)
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout)
	controller := &Controller{
		kubeClient:    kubefake.NewSimpleClientset(),
		dynamicClient: dynamicClient,
	}
	target := Target{
		Name:                     "example-worker",
		Strategy:                 "canary",
		ExpectedCurrentPodHash:   "green",
		ExpectedStableRS:         "blue",
		ExpectedCurrentStepIndex: int32Pointer(1),
	}

	state, err := controller.promoteTarget(context.Background(), "test-ns", target)
	if err != nil {
		t.Fatal(err)
	}
	if state["previousStepIndex"] != int64(1) {
		t.Fatalf("expected promotion to start from step 1, got %+v", state)
	}
	updated, err := dynamicClient.Resource(v1alpha1.RolloutGVR).Namespace("test-ns").Get(context.Background(), "example-worker", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	step, _, err := unstructured.NestedInt64(updated.Object, "status", "currentStepIndex")
	if err != nil {
		t.Fatal(err)
	}
	if step != 1 {
		t.Fatalf("expected promotion to clear the pause without changing currentStepIndex, got %d", step)
	}
	conditions, _, err := unstructured.NestedSlice(updated.Object, "status", "pauseConditions")
	if err != nil {
		t.Fatal(err)
	}
	if len(conditions) != 0 {
		t.Fatalf("expected pause conditions to be cleared, got %+v", conditions)
	}
}

func TestApplyTargetWeightSwitch(t *testing.T) {
	activePod := newPod("test-ns", "active-pod", "green")
	inactivePod := newPod("test-ns", "inactive-pod", "blue")
	activeRS := newReplicaSet("test-ns", "active-rs", "green")
	inactiveRS := newReplicaSet("test-ns", "inactive-rs", "blue")
	controller := &Controller{
		kubeClient:    kubefake.NewSimpleClientset(activePod, inactivePod, activeRS, inactiveRS),
		dynamicClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), newCanaryRollout("test-ns", "example-worker", "green", "blue", 2, nil)),
	}
	state := map[string]interface{}{
		"currentPodHash":    "green",
		"activeServiceHash": "green",
		"promoted":          true,
	}
	target := Target{
		Name: "example-worker",
		WeightSwitch: &WeightSwitch{
			Enabled:        true,
			AnnotationKey:  "example.com/traffic-weight",
			BaseSelector:   "app=example,component=data",
			ActiveWeight:   "active",
			InactiveWeight: "inactive",
		},
	}

	state, err := controller.applyTargetTraffic(context.Background(), "test-ns", target, state)
	if err != nil {
		t.Fatal(err)
	}
	if applied, _ := state["weightSwitchApplied"].(bool); !applied {
		t.Fatalf("expected weight switch status, got %+v", state)
	}

	gotActivePod, err := controller.kubeClient.CoreV1().Pods("test-ns").Get(context.Background(), "active-pod", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if gotActivePod.Annotations["example.com/traffic-weight"] != "active" {
		t.Fatalf("unexpected active pod weight: %q", gotActivePod.Annotations["example.com/traffic-weight"])
	}
	gotInactivePod, err := controller.kubeClient.CoreV1().Pods("test-ns").Get(context.Background(), "inactive-pod", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if gotInactivePod.Annotations["example.com/traffic-weight"] != "inactive" {
		t.Fatalf("unexpected inactive pod weight: %q", gotInactivePod.Annotations["example.com/traffic-weight"])
	}
	gotActiveRS, err := controller.kubeClient.AppsV1().ReplicaSets("test-ns").Get(context.Background(), "active-rs", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if gotActiveRS.Spec.Template.Annotations["example.com/traffic-weight"] != "active" {
		t.Fatalf("unexpected active rs template weight: %q", gotActiveRS.Spec.Template.Annotations["example.com/traffic-weight"])
	}
	gotInactiveRS, err := controller.kubeClient.AppsV1().ReplicaSets("test-ns").Get(context.Background(), "inactive-rs", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if gotInactiveRS.Spec.Template.Annotations["example.com/traffic-weight"] != "inactive" {
		t.Fatalf("unexpected inactive rs template weight: %q", gotInactiveRS.Spec.Template.Annotations["example.com/traffic-weight"])
	}
}

func TestTargetActionCompletedAbortUsesAppliedWeight(t *testing.T) {
	state := map[string]interface{}{
		"strategy":             "canary",
		"aborted":              true,
		"canaryWeight":         int64(0),
		"canaryWeightVerified": false,
	}
	if !targetActionCompleted("abort", "", state) {
		t.Fatal("expected abort to complete when Argo reports abort=true and canary weight is zero")
	}

	state["canaryWeight"] = int64(10)
	if targetActionCompleted("abort", "", state) {
		t.Fatal("expected abort to remain pending while canary weight is non-zero")
	}
}

func TestAbortConvergesAllTrafficToStableHash(t *testing.T) {
	enabled := true
	promotion := &v1alpha1.RolloutPromotion{
		ObjectMeta: metav1.ObjectMeta{Name: "abort-weight", Namespace: "test-ns", Generation: 1},
		Spec: v1alpha1.RolloutPromotionSpec{
			RequestID: "abort-weight",
			Action:    "abort",
			Strategy:  "canary",
			Mode:      "together",
			Wait:      v1alpha1.RolloutPromotionWait{Enabled: &enabled, Attempts: 1},
		},
	}
	rollout := newCanaryRollout("test-ns", "example-worker", "green", "blue", 2, []interface{}{map[string]interface{}{"reason": "CanaryPauseStep"}})
	activePod := newPod("test-ns", "stable-pod", "blue")
	inactivePod := newPod("test-ns", "canary-pod", "green")
	activeRS := newReplicaSet("test-ns", "stable-rs", "blue")
	inactiveRS := newReplicaSet("test-ns", "canary-rs", "green")
	rolloutsClient := rolloutfake.NewSimpleClientset(promotion)
	controller := &Controller{
		kubeClient:     kubefake.NewSimpleClientset(activePod, inactivePod, activeRS, inactiveRS),
		dynamicClient:  dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), rollout),
		rolloutsClient: rolloutsClient,
	}
	target := Target{
		Name:     "example-worker",
		Strategy: "canary",
		WeightSwitch: &WeightSwitch{
			Enabled:             true,
			ApplyOnPromotion:    false,
			ApplyOnPromotionSet: true,
			AnnotationKey:       "example.com/traffic-weight",
			BaseSelector:        "app=example,component=data",
			ActiveWeight:        "active",
			InactiveWeight:      "inactive",
			ReapplyDelaySeconds: 0,
		},
	}
	spec := RequestSpec{Action: "abort", Strategy: "canary", Mode: "together", Wait: WaitOptions{Enabled: true, Attempts: 1}, Targets: []Target{target}}
	if _, err := controller.executeControlAction(context.Background(), promotion, spec, map[string]interface{}{}); err != nil {
		t.Fatal(err)
	}

	stable, err := controller.kubeClient.CoreV1().Pods("test-ns").Get(context.Background(), "stable-pod", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	canary, err := controller.kubeClient.CoreV1().Pods("test-ns").Get(context.Background(), "canary-pod", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if stable.Annotations["example.com/traffic-weight"] != "active" || canary.Annotations["example.com/traffic-weight"] != "inactive" {
		t.Fatalf("abort did not converge traffic to stable: stable=%q canary=%q", stable.Annotations["example.com/traffic-weight"], canary.Annotations["example.com/traffic-weight"])
	}
}

func newRollout(namespace, name, currentHash, activeHash, stableHash string, pauseConditions []interface{}) *unstructured.Unstructured {
	rollout := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Rollout",
		"metadata": map[string]interface{}{
			"namespace": namespace,
			"name":      name,
			"uid":       "rollout-uid",
		},
		"status": map[string]interface{}{
			"currentPodHash": currentHash,
			"stableRS":       stableHash,
			"blueGreen": map[string]interface{}{
				"activeSelector": activeHash,
			},
		},
	}}
	if pauseConditions != nil {
		_, _, _ = unstructured.NestedSlice(rollout.Object, "status", "pauseConditions")
		_ = unstructured.SetNestedSlice(rollout.Object, pauseConditions, "status", "pauseConditions")
	}
	rollout.SetGroupVersionKind(v1alpha1.SchemeGroupVersion.WithKind("Rollout"))
	return rollout
}

func newCanaryRollout(namespace, name, currentHash, stableHash string, stepIndex int64, pauseConditions []interface{}) *unstructured.Unstructured {
	rollout := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Rollout",
		"metadata": map[string]interface{}{
			"namespace": namespace,
			"name":      name,
			"uid":       "rollout-uid",
		},
		"spec": map[string]interface{}{
			"strategy": map[string]interface{}{
				"canary": map[string]interface{}{
					"steps": []interface{}{
						map[string]interface{}{"setWeight": int64(0)},
						map[string]interface{}{"pause": map[string]interface{}{}},
						map[string]interface{}{"setWeight": int64(10)},
					},
				},
			},
		},
		"status": map[string]interface{}{
			"currentPodHash":   currentHash,
			"stableRS":         stableHash,
			"currentStepIndex": stepIndex,
		},
	}}
	if pauseConditions != nil {
		_ = unstructured.SetNestedSlice(rollout.Object, pauseConditions, "status", "pauseConditions")
	}
	rollout.SetGroupVersionKind(v1alpha1.SchemeGroupVersion.WithKind("Rollout"))
	return rollout
}

func newPod(namespace, name, hash string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            name,
			UID:             types.UID("pod-" + name),
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "rs-" + hash, UID: types.UID("rs-" + hash), Controller: boolPointer(true)}},
			Labels: map[string]string{
				"app":            "example",
				"component":      "data",
				rolloutHashLabel: hash,
			},
		},
	}
}

func newReplicaSet(namespace, name, hash string) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            name,
			UID:             types.UID("rs-" + hash),
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "argoproj.io/v1alpha1", Kind: "Rollout", Name: "example-worker", UID: types.UID("rollout-uid"), Controller: boolPointer(true)}},
			Labels: map[string]string{
				"app":            "example",
				"component":      "data",
				rolloutHashLabel: hash,
			},
		},
		Spec: appsv1.ReplicaSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
		},
	}
}

func int32Pointer(value int32) *int32 {
	return &value
}

func boolPointer(value bool) *bool { return &value }
