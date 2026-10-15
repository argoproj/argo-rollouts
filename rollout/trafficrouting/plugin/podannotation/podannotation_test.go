package podannotation

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
)

func TestCalculatePerPodWeights(t *testing.T) {
	stable, canary, err := calculatePerPodWeights(3, 3, 20, 10)
	if err != nil {
		t.Fatal(err)
	}
	if stable != 8 || canary != 2 {
		t.Fatalf("expected stable=8 canary=2, got stable=%d canary=%d", stable, canary)
	}

	stable, canary, err = calculatePerPodWeights(3, 1, 20, 10)
	if err != nil {
		t.Fatal(err)
	}
	actual := float64(canary) / float64(3*stable+canary)
	if actual < 0.199 || actual > 0.201 {
		t.Fatalf("expected approximately 20%%, got %.4f with stable=%d canary=%d", actual*100, stable, canary)
	}
}

func TestCalculatePerPodWeightsForRetainedCanaryPolicy(t *testing.T) {
	tests := []struct {
		desired      int
		stableWeight int
		canaryWeight int
	}{
		{desired: 0, stableWeight: 10, canaryWeight: 0},
		{desired: 10, stableWeight: 9, canaryWeight: 1},
		{desired: 30, stableWeight: 7, canaryWeight: 3},
		{desired: 50, stableWeight: 10, canaryWeight: 10},
		{desired: 100, stableWeight: 0, canaryWeight: 10},
	}

	for _, test := range tests {
		t.Run(strconv.Itoa(test.desired), func(t *testing.T) {
			stable, canary, err := calculatePerPodWeights(3, 3, test.desired, 10)
			if err != nil {
				t.Fatal(err)
			}
			if stable != test.stableWeight || canary != test.canaryWeight {
				t.Fatalf("desired=%d: expected stable=%d canary=%d, got stable=%d canary=%d", test.desired, test.stableWeight, test.canaryWeight, stable, canary)
			}
		})
	}
}

func TestCalculateDirectWeights(t *testing.T) {
	stable, canary, err := calculateWeights(Config{MaxPodWeight: 10, WeightCalculation: "direct"}, 3, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if stable != 8 || canary != 2 {
		t.Fatalf("expected stable=8 canary=2, got stable=%d canary=%d", stable, canary)
	}
}

func TestTrafficWeightScaleMapsToPercent(t *testing.T) {
	config := Config{
		MaxPodWeight:       10,
		TrafficWeightScale: 1000,
		WeightCalculation:  "direct",
	}
	desiredPercent := int(math.Round(float64(250) * 100 / float64(config.TrafficWeightScale)))
	stable, canary, err := calculateWeights(config, 3, 3, desiredPercent)
	if err != nil {
		t.Fatal(err)
	}
	if stable != 8 || canary != 3 {
		t.Fatalf("expected rounded 25%% weights stable=8 canary=3, got stable=%d canary=%d", stable, canary)
	}
}

func TestRenderWeightReplacesGenericPlaceholderAndPrettyPrintsJSON(t *testing.T) {
	template := `[{"host":"preview.example.com","path":"/","weight":10},{"weight":"__ROLLOUT_WEIGHT__"}]`
	rendered, err := renderWeight(template, 4, defaultWeightPlaceholder, true)
	if err != nil {
		t.Fatal(err)
	}
	expected := `[
  {
    "host": "preview.example.com",
    "path": "/",
    "weight": 10
  },
  {
    "weight": 4
  }
]`
	if rendered != expected {
		t.Fatalf("unexpected rendered weight:\n%s", rendered)
	}
}

func TestRenderWeightSupportsArbitraryAnnotationValues(t *testing.T) {
	rendered, err := renderWeight("traffic=__ROLLOUT_WEIGHT__", 7, defaultWeightPlaceholder, false)
	if err != nil {
		t.Fatal(err)
	}
	if rendered != "traffic=7" {
		t.Fatalf("unexpected raw annotation value: %s", rendered)
	}
}

func TestAnnotationValueMatchesEquivalentJSONFormatting(t *testing.T) {
	compact := `[{"host":"preview.example.com","weight":10},{"weight":0}]`
	prettyWithTrailingNewline := "[\n  {\n    \"host\": \"preview.example.com\",\n    \"weight\": 10\n  },\n  {\n    \"weight\": 0\n  }\n]\n"
	if !annotationValueMatches(compact, prettyWithTrailingNewline) {
		t.Fatal("expected semantically equivalent JSON annotation values to match")
	}
	if annotationValueMatches("traffic=10", "traffic=10\n") {
		t.Fatal("non-JSON annotation values must still use exact matching")
	}
}

func TestManagedAnnotationKey(t *testing.T) {
	rollout := testRollout("canary", "stable")
	if key := ManagedAnnotationKey(rollout); key != testAnnotationKey {
		t.Fatalf("expected managed annotation %q, got %q", testAnnotationKey, key)
	}
	rollout.Spec.Strategy.Canary.TrafficRouting.Plugins = nil
	if key := ManagedAnnotationKey(rollout); key != "" {
		t.Fatalf("expected no managed annotation without the plugin, got %q", key)
	}
}

func TestPatchBySelectorSkipsEquivalentJSONFormatting(t *testing.T) {
	patchPods := false
	patchReplicaSets := true
	payload := `[{"host":"preview.example.com","weight":10},{"weight":0}]`
	replicaSet := testReplicaSet("canary-rs", "canary")
	replicaSet.Spec.Template.Annotations = map[string]string{
		testAnnotationKey: "[\n  {\n    \"host\": \"preview.example.com\",\n    \"weight\": 10\n  },\n  {\n    \"weight\": 0\n  }\n]\n",
	}
	replicaSet.Spec.Template.Labels["rollout-role"] = "preview"
	client := fake.NewSimpleClientset(replicaSet)
	reconciler := NewReconciler(client)
	config := Config{
		AnnotationKey:    testAnnotationKey,
		BaseSelector:     "app=example",
		RoleLabelKey:     "rollout-role",
		PatchPods:        &patchPods,
		PatchReplicaSets: &patchReplicaSets,
	}

	if err := reconciler.patchBySelector(context.Background(), "test-ns", "app=example", config, payload, "preview"); err != nil {
		t.Fatal(err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "patch" && action.GetResource().Resource == "replicasets" {
			t.Fatalf("equivalent JSON formatting must not cause a ReplicaSet patch: %#v", action)
		}
	}
	verified, err := reconciler.verifySelector(context.Background(), "test-ns", "app=example", config, payload)
	if err != nil || !verified {
		t.Fatalf("equivalent JSON formatting must verify successfully: verified=%v err=%v", verified, err)
	}
}

func TestSetAndVerifyWeight(t *testing.T) {
	stablePod := testPod("stable-pod", "stable")
	canaryPod := testPod("canary-pod", "canary")
	stableRS := testReplicaSet("stable-rs", "stable")
	canaryRS := testReplicaSet("canary-rs", "canary")
	client := fake.NewSimpleClientset(stablePod, canaryPod, stableRS, canaryRS)
	reconciler := NewReconciler(client)
	rollout := testRollout("canary", "stable")

	if err := reconciler.SetWeight(rollout, 20, nil); err.HasError() {
		t.Fatal(err)
	}
	stablePod, _ = client.CoreV1().Pods("test-ns").Get(context.Background(), "stable-pod", metav1.GetOptions{})
	canaryPod, _ = client.CoreV1().Pods("test-ns").Get(context.Background(), "canary-pod", metav1.GetOptions{})
	stablePayload, _ := renderWeight(testStableTemplate, 8, defaultWeightPlaceholder, true)
	canaryPayload, _ := renderWeight(testCanaryTemplate, 2, defaultWeightPlaceholder, true)
	if stablePod.Annotations[testAnnotationKey] != stablePayload {
		t.Fatalf("unexpected stable payload: %s", stablePod.Annotations[testAnnotationKey])
	}
	if canaryPod.Annotations[testAnnotationKey] != canaryPayload {
		t.Fatalf("unexpected canary payload: %s", canaryPod.Annotations[testAnnotationKey])
	}
	verified, verifyErr := reconciler.VerifyWeight(rollout, 20, nil)
	if verifyErr.HasError() || verified != 1 {
		t.Fatalf("expected verified weight, got verified=%v err=%v", verified, verifyErr)
	}
	stableRS, _ = client.AppsV1().ReplicaSets("test-ns").Get(context.Background(), "stable-rs", metav1.GetOptions{})
	if stableRS.Spec.Template.Annotations[testAnnotationKey] != stablePayload {
		t.Fatalf("stable ReplicaSet template was not patched")
	}
}

func TestSetWeightPatchesCanaryReplicaSetBeforePodsExist(t *testing.T) {
	stablePod := testPod("stable-pod", "stable")
	stableRS := testReplicaSet("stable-rs", "stable")
	canaryRS := testReplicaSet("canary-rs", "canary")
	client := fake.NewSimpleClientset(stablePod, stableRS, canaryRS)
	reconciler := NewReconciler(client)
	rollout := testRollout("canary", "stable")

	if err := reconciler.SetWeight(rollout, 0, nil); err.HasError() {
		t.Fatal(err)
	}
	canaryRS, _ = client.AppsV1().ReplicaSets("test-ns").Get(context.Background(), "canary-rs", metav1.GetOptions{})
	expected, _ := renderWeight(testCanaryTemplate, 0, defaultWeightPlaceholder, true)
	if canaryRS.Spec.Template.Annotations[testAnnotationKey] != expected {
		t.Fatalf("canary ReplicaSet did not receive its safe birth weight: %s", canaryRS.Spec.Template.Annotations[testAnnotationKey])
	}
}

func TestRemoveManagedRoutesKeepsPromotedHashActive(t *testing.T) {
	activePod := testPod("active-pod", "green")
	oldPod := testPod("old-pod", "blue")
	futureRS := testReplicaSet("future-rs", "future")
	zero := int32(0)
	futureRS.Spec.Replicas = &zero
	client := fake.NewSimpleClientset(activePod, oldPod, testReplicaSet("active-rs", "green"), testReplicaSet("old-rs", "blue"), futureRS)
	reconciler := NewReconciler(client)
	rollout := testRollout("green", "green")

	if err := reconciler.RemoveManagedRoutes(rollout); err.HasError() {
		t.Fatal(err)
	}
	activePod, _ = client.CoreV1().Pods("test-ns").Get(context.Background(), "active-pod", metav1.GetOptions{})
	oldPod, _ = client.CoreV1().Pods("test-ns").Get(context.Background(), "old-pod", metav1.GetOptions{})
	activePayload, _ := renderWeight(testPromotedTemplate, 10, defaultWeightPlaceholder, true)
	if activePod.Annotations[testAnnotationKey] != activePayload {
		t.Fatalf("unexpected active payload: %s", activePod.Annotations[testAnnotationKey])
	}
	if oldPod.Annotations[testAnnotationKey] != testInactiveTemplate {
		t.Fatalf("unexpected inactive payload: %s", oldPod.Annotations[testAnnotationKey])
	}
	futureRS, _ = client.AppsV1().ReplicaSets("test-ns").Get(context.Background(), "future-rs", metav1.GetOptions{})
	if _, found := futureRS.Spec.Template.Annotations[testAnnotationKey]; found {
		t.Fatal("a zero-replica future ReplicaSet must not be treated as an inactive revision")
	}
}

func TestRemoveManagedRoutesRestoresStableTrafficOnAbort(t *testing.T) {
	stablePod := testPod("stable-pod", "blue")
	canaryPod := testPod("canary-pod", "green")
	client := fake.NewSimpleClientset(stablePod, canaryPod, testReplicaSet("stable-rs", "blue"), testReplicaSet("canary-rs", "green"))
	reconciler := NewReconciler(client)
	rollout := testRollout("green", "blue")
	rollout.Status.Abort = true

	if err := reconciler.RemoveManagedRoutes(rollout); err.HasError() {
		t.Fatal(err)
	}
	stablePod, _ = client.CoreV1().Pods("test-ns").Get(context.Background(), "stable-pod", metav1.GetOptions{})
	canaryPod, _ = client.CoreV1().Pods("test-ns").Get(context.Background(), "canary-pod", metav1.GetOptions{})
	stablePayload, _ := renderWeight(testStableTemplate, 10, defaultWeightPlaceholder, true)
	canaryPayload, _ := renderWeight(testCanaryTemplate, 0, defaultWeightPlaceholder, true)
	if stablePod.Annotations[testAnnotationKey] != stablePayload {
		t.Fatalf("unexpected stable payload after abort: %s", stablePod.Annotations[testAnnotationKey])
	}
	if canaryPod.Annotations[testAnnotationKey] != canaryPayload {
		t.Fatalf("unexpected canary payload after abort: %s", canaryPod.Annotations[testAnnotationKey])
	}
}

func TestTrafficOverridePinsRetainedRevision(t *testing.T) {
	targetPod := testPod("target-pod", "blue")
	currentPod := testPod("current-pod", "green")
	client := fake.NewSimpleClientset(targetPod, currentPod, testReplicaSet("target-rs", "blue"), testReplicaSet("current-rs", "green"))
	reconciler := NewReconciler(client)
	rollout := testRollout("green", "green")
	rollout.Annotations = map[string]string{annotations.TrafficOverrideHashAnnotation: "blue"}

	if err := reconciler.SetWeight(rollout, 100, nil); err.HasError() {
		t.Fatal(err)
	}
	targetPod, _ = client.CoreV1().Pods("test-ns").Get(context.Background(), "target-pod", metav1.GetOptions{})
	currentPod, _ = client.CoreV1().Pods("test-ns").Get(context.Background(), "current-pod", metav1.GetOptions{})
	expectedActive, _ := renderWeight(testPromotedTemplate, 10, defaultWeightPlaceholder, true)
	if targetPod.Annotations[testAnnotationKey] != expectedActive {
		t.Fatalf("retained target was not made active: %s", targetPod.Annotations[testAnnotationKey])
	}
	if currentPod.Annotations[testAnnotationKey] != testInactiveTemplate {
		t.Fatalf("current revision was not made inactive: %s", currentPod.Annotations[testAnnotationKey])
	}
	verified, verifyErr := reconciler.VerifyWeight(rollout, 100, nil)
	if verifyErr.HasError() || verified != pluginTypes.Verified {
		t.Fatalf("expected traffic override to verify, got verified=%v err=%v", verified, verifyErr)
	}
}

func TestNextRevisionAfterTrafficRollbackKeepsBaselineAndExposesPreview(t *testing.T) {
	baselinePod := testPod("baseline-pod", "blue")
	failedPod := testPod("failed-pod", "green")
	candidatePod := testPod("candidate-pod", "a-plus")
	client := fake.NewSimpleClientset(
		baselinePod,
		failedPod,
		candidatePod,
		testReplicaSet("baseline-rs", "blue"),
		testReplicaSet("failed-rs", "green"),
		testReplicaSet("candidate-rs", "a-plus"),
	)
	reconciler := NewReconciler(client)
	rollout := testRollout("a-plus", "green")
	rollout.Annotations = map[string]string{
		annotations.TrafficOverrideHashAnnotation:       "blue",
		annotations.TrafficOverrideSourceHashAnnotation: "green",
	}

	if err := reconciler.SetWeight(rollout, 100, nil); err.HasError() {
		t.Fatal(err)
	}
	baselinePod, _ = client.CoreV1().Pods("test-ns").Get(context.Background(), "baseline-pod", metav1.GetOptions{})
	failedPod, _ = client.CoreV1().Pods("test-ns").Get(context.Background(), "failed-pod", metav1.GetOptions{})
	candidatePod, _ = client.CoreV1().Pods("test-ns").Get(context.Background(), "candidate-pod", metav1.GetOptions{})
	stablePayload, _ := renderWeight(testStableTemplate, 10, defaultWeightPlaceholder, true)
	candidatePayload, _ := renderWeight(testCanaryTemplate, 0, defaultWeightPlaceholder, true)
	if baselinePod.Annotations[testAnnotationKey] != stablePayload {
		t.Fatalf("rollback baseline must retain production traffic: %s", baselinePod.Annotations[testAnnotationKey])
	}
	if candidatePod.Annotations[testAnnotationKey] != candidatePayload {
		t.Fatalf("next revision must receive preview-only traffic: %s", candidatePod.Annotations[testAnnotationKey])
	}
	if failedPod.Annotations[testAnnotationKey] != testInactiveTemplate {
		t.Fatalf("failed revision must remain inactive: %s", failedPod.Annotations[testAnnotationKey])
	}
	verified, verifyErr := reconciler.VerifyWeight(rollout, 100, nil)
	if verifyErr.HasError() || verified != pluginTypes.Verified {
		t.Fatalf("expected next revision rollback routing to verify, got verified=%v err=%v", verified, verifyErr)
	}
}

func TestLegacyAdoptionRequiresExplicitPromotion(t *testing.T) {
	legacyPod := testLegacyPod("legacy-pod")
	candidatePod := testPod("candidate-pod", "candidate")
	oldManagedPod := testPod("old-managed-pod", "old")
	legacyRS := testLegacyReplicaSet("legacy-rs")
	candidateRS := testReplicaSet("candidate-rs", "candidate")
	oldManagedRS := testReplicaSet("old-managed-rs", "old")
	client := fake.NewSimpleClientset(legacyPod, candidatePod, oldManagedPod, legacyRS, candidateRS, oldManagedRS)
	reconciler := NewReconciler(client)
	rollout := testLegacyAdoptionRollout("candidate")

	if err := reconciler.ReconcileLegacyAdoptionPending(context.Background(), rollout); err != nil {
		t.Fatal(err)
	}
	assertPodWeight(t, client, "legacy-pod", testLegacyAdoptionLegacyWeight)
	assertPodWeight(t, client, "candidate-pod", testLegacyAdoptionCandidateWeight)
	assertPodWeight(t, client, "old-managed-pod", testLegacyAdoptionInactiveWeight)
	assertReplicaSetWeight(t, client, "legacy-rs", "")
	assertReplicaSetWeight(t, client, "candidate-rs", testLegacyAdoptionCandidateWeight)
	verified, err := reconciler.VerifyLegacyAdoptionPending(context.Background(), rollout)
	if err != nil || !verified {
		t.Fatalf("pending adoption must verify without mutating the legacy ReplicaSet template: verified=%v err=%v", verified, err)
	}

	if err := reconciler.PromoteLegacyAdoption(context.Background(), rollout); err != nil {
		t.Fatal(err)
	}
	if err := reconciler.PromoteLegacyAdoption(context.Background(), rollout); err != nil {
		t.Fatalf("explicit promotion must be idempotent: %v", err)
	}
	assertPodWeight(t, client, "legacy-pod", testLegacyAdoptionInactiveWeight)
	assertPodWeight(t, client, "candidate-pod", testLegacyAdoptionActiveWeight)
	assertPodWeight(t, client, "old-managed-pod", testLegacyAdoptionInactiveWeight)
	assertReplicaSetWeight(t, client, "legacy-rs", "")

	legacyPod, _ = client.CoreV1().Pods("test-ns").Get(context.Background(), "legacy-pod", metav1.GetOptions{})
	legacyPod.Annotations[testAnnotationKey] = testLegacyAdoptionLegacyWeight
	_, _ = client.CoreV1().Pods("test-ns").Update(context.Background(), legacyPod, metav1.UpdateOptions{})
	rollout.Annotations[annotations.LegacyAdoptionStateAnnotation] = annotations.LegacyAdoptionStatePromoted
	if err := reconciler.SetWeight(rollout, 0, nil); err.HasError() {
		t.Fatal(err)
	}
	assertPodWeight(t, client, "legacy-pod", testLegacyAdoptionInactiveWeight)
	assertPodWeight(t, client, "candidate-pod", testLegacyAdoptionActiveWeight)
	assertNoPodLifecycleActions(t, client)
}

func TestDurableLegacyAdoptionPromotionOverridesStaleHelmState(t *testing.T) {
	rollout := testLegacyAdoptionRollout("candidate")
	rollout.Annotations[annotations.LegacyAdoptionStateAnnotation] = annotations.LegacyAdoptionStatePending
	rollout.Annotations[annotations.LegacyAdoptionPromotedAnnotation] = "true"

	if LegacyAdoptionPending(rollout) {
		t.Fatal("durable promotion marker must override stale pending state")
	}
	if !legacyAdoptionPromoted(rollout) {
		t.Fatal("durable promotion marker must be treated as promoted")
	}
	if legacyAdoptionPromoting(rollout) {
		t.Fatal("durable promotion marker must override stale promoting state")
	}
}

func TestPromotedLegacyVerificationAllowsScaledDownSourceDeployment(t *testing.T) {
	client := fake.NewSimpleClientset()
	reconciler := NewReconciler(client)
	rollout := testLegacyAdoptionRollout("candidate")
	rollout.Annotations[annotations.LegacyAdoptionStateAnnotation] = annotations.LegacyAdoptionStatePromoted
	rollout.Annotations[annotations.LegacyAdoptionPromotedAnnotation] = "true"

	verified, err := reconciler.verifyPromotedLegacyInactive(context.Background(), rollout)
	if err != nil {
		t.Fatal(err)
	}
	if !verified {
		t.Fatal("a promoted adoption with no live legacy Pods must be verified inactive")
	}
}

func TestRestoreLegacyAdoptionForDeletionDoesNotRecreatePods(t *testing.T) {
	legacyPod := testLegacyPod("legacy-pod")
	legacyPod.Annotations = map[string]string{testAnnotationKey: testLegacyAdoptionInactiveWeight}
	legacyPod.OwnerReferences = []metav1.OwnerReference{replicaSetControllerReference("legacy-rs")}
	candidatePod := testPod("candidate-pod", "candidate")
	candidatePod.Annotations = map[string]string{testAnnotationKey: testLegacyAdoptionActiveWeight}
	legacyRS := testLegacyReplicaSet("legacy-rs")
	legacyRS.Spec.Template.Annotations = map[string]string{"checksum/config": "original"}
	candidateRS := testReplicaSet("candidate-rs", "candidate")
	candidateRS.Spec.Template.Annotations = map[string]string{testAnnotationKey: testLegacyAdoptionActiveWeight}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "test-ns"},
		Spec: appsv1.DeploymentSpec{
			Paused:   false,
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"checksum/config": "original"}}},
		},
	}
	client := fake.NewSimpleClientset(legacyPod, candidatePod, legacyRS, candidateRS, deployment)
	reconciler := NewReconciler(client)
	rollout := testLegacyAdoptionRollout("candidate")
	rollout.Spec.WorkloadRef = &v1alpha1.ObjectRef{APIVersion: "apps/v1", Kind: "Deployment", Name: "example", ScaleDown: "never"}

	for attempt := 0; attempt < 2; attempt++ {
		if err := reconciler.RestoreLegacyAdoptionForDeletion(context.Background(), rollout); err != nil {
			t.Fatalf("cleanup attempt %d failed: %v", attempt+1, err)
		}
	}
	assertPodAnnotationAbsent(t, client, "legacy-pod", testAnnotationKey)
	assertPodWeight(t, client, "candidate-pod", testLegacyAdoptionInactiveWeight)
	legacyRS, _ = client.AppsV1().ReplicaSets("test-ns").Get(context.Background(), "legacy-rs", metav1.GetOptions{})
	if _, found := legacyRS.Spec.Template.Annotations[testAnnotationKey]; found {
		t.Fatal("legacy ReplicaSet template must never receive the traffic annotation")
	}
	deployment, _ = client.AppsV1().Deployments("test-ns").Get(context.Background(), "example", metav1.GetOptions{})
	if deployment.Spec.Paused {
		t.Fatal("legacy Deployment pause state was not restored")
	}
	assertNoPodLifecycleActions(t, client)
}

func TestRestoreLegacyAdoptionForDeletionRestoresOriginalReplicaSetAnnotation(t *testing.T) {
	const originalWeight = "original-weight"
	legacyPod := testLegacyPod("legacy-pod")
	legacyPod.Annotations = map[string]string{testAnnotationKey: testLegacyAdoptionInactiveWeight}
	legacyPod.OwnerReferences = []metav1.OwnerReference{replicaSetControllerReference("legacy-rs")}
	legacyRS := testLegacyReplicaSet("legacy-rs")
	legacyRS.Spec.Template.Annotations = map[string]string{testAnnotationKey: originalWeight}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "test-ns"},
		Spec: appsv1.DeploymentSpec{
			Paused:   true,
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{testAnnotationKey: "deployment-fallback"}}},
		},
	}
	client := fake.NewSimpleClientset(legacyPod, legacyRS, deployment)
	reconciler := NewReconciler(client)
	rollout := testLegacyAdoptionRollout("candidate")
	rollout.Spec.WorkloadRef = &v1alpha1.ObjectRef{APIVersion: "apps/v1", Kind: "Deployment", Name: "example", ScaleDown: "never"}
	rollout.Annotations[annotations.LegacyAdoptionDeploymentPausedAnnotation] = "true"

	for attempt := 0; attempt < 2; attempt++ {
		if err := reconciler.RestoreLegacyAdoptionForDeletion(context.Background(), rollout); err != nil {
			t.Fatalf("cleanup attempt %d failed: %v", attempt+1, err)
		}
	}
	assertPodWeight(t, client, "legacy-pod", originalWeight)
	deployment, _ = client.AppsV1().Deployments("test-ns").Get(context.Background(), "example", metav1.GetOptions{})
	if !deployment.Spec.Paused {
		t.Fatal("legacy Deployment pause state was not restored")
	}
	assertNoPodLifecycleActions(t, client)
}

func TestRestoreLegacyAdoptionForDeletionAllowsMissingDeployment(t *testing.T) {
	client := fake.NewSimpleClientset()
	reconciler := NewReconciler(client)
	rollout := testLegacyAdoptionRollout("candidate")
	rollout.Spec.WorkloadRef = &v1alpha1.ObjectRef{APIVersion: "apps/v1", Kind: "Deployment", Name: "deleted", ScaleDown: "never"}

	for attempt := 0; attempt < 2; attempt++ {
		if err := reconciler.RestoreLegacyAdoptionForDeletion(context.Background(), rollout); err != nil {
			t.Fatalf("cleanup attempt %d must tolerate an already deleted Deployment: %v", attempt+1, err)
		}
	}
	assertNoPodLifecycleActions(t, client)
}

func TestSetDeploymentPausedRetriesConflict(t *testing.T) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "test-ns"},
		Spec:       appsv1.DeploymentSpec{Paused: true},
	}
	client := fake.NewSimpleClientset(deployment)
	conflicts := 0
	client.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if conflicts == 0 {
			conflicts++
			return true, nil, k8serrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, "example", errors.New("concurrent update"))
		}
		return false, nil, nil
	})

	updated, err := setDeploymentPaused(context.Background(), client.AppsV1().Deployments("test-ns"), "example", false)
	if err != nil {
		t.Fatalf("expected conflict retry to succeed: %v", err)
	}
	if updated.Spec.Paused {
		t.Fatal("expected Deployment to be unpaused after retry")
	}
	if conflicts != 1 {
		t.Fatalf("expected one injected conflict, got %d", conflicts)
	}
}

const (
	testAnnotationKey                 = "example.com/traffic-weight"
	testStableTemplate                = `[{"host":"preview.example.com","weight":0},{"weight":"__ROLLOUT_WEIGHT__"}]`
	testCanaryTemplate                = `[{"host":"preview.example.com","weight":10},{"weight":"__ROLLOUT_WEIGHT__"}]`
	testPromotedTemplate              = `[{"host":"preview.example.com","weight":10},{"weight":"__ROLLOUT_WEIGHT__"}]`
	testInactiveTemplate              = `[{"host":"preview.example.com","weight":0},{"weight":0}]`
	testLegacyAdoptionActiveWeight    = "active-weight"
	testLegacyAdoptionCandidateWeight = "candidate-weight"
	testLegacyAdoptionLegacyWeight    = "legacy-weight"
	testLegacyAdoptionInactiveWeight  = "inactive-weight"
)

func testRollout(currentHash, stableHash string) *v1alpha1.Rollout {
	prettyJSON := true
	config, _ := json.Marshal(Config{
		AnnotationKey:          testAnnotationKey,
		BaseSelector:           "app=example",
		MaxPodWeight:           10,
		StableWeightTemplate:   testStableTemplate,
		CanaryWeightTemplate:   testCanaryTemplate,
		PromotedWeightTemplate: testPromotedTemplate,
		InactiveWeightTemplate: testInactiveTemplate,
		PrettyJSON:             &prettyJSON,
	})
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "test-ns"},
		Spec: v1alpha1.RolloutSpec{Strategy: v1alpha1.RolloutStrategy{Canary: &v1alpha1.CanaryStrategy{
			TrafficRouting: &v1alpha1.RolloutTrafficRouting{Plugins: map[string]json.RawMessage{PluginName: config}},
		}}},
		Status: v1alpha1.RolloutStatus{CurrentPodHash: currentHash, StableRS: stableHash},
	}
}

func testLegacyAdoptionRollout(currentHash string) *v1alpha1.Rollout {
	rollout := testRollout(currentHash, currentHash)
	rollout.Annotations = map[string]string{
		annotations.LegacyAdoptionStateAnnotation:            annotations.LegacyAdoptionStatePending,
		annotations.LegacyAdoptionBaseSelectorAnnotation:     "app=example",
		annotations.LegacyAdoptionWeightAnnotationAnnotation: testAnnotationKey,
		annotations.LegacyAdoptionActiveWeightAnnotation:     testLegacyAdoptionActiveWeight,
		annotations.LegacyAdoptionCandidateWeightAnnotation:  testLegacyAdoptionCandidateWeight,
		annotations.LegacyAdoptionLegacyWeightAnnotation:     testLegacyAdoptionLegacyWeight,
		annotations.LegacyAdoptionInactiveWeightAnnotation:   testLegacyAdoptionInactiveWeight,
		annotations.LegacyAdoptionDeploymentPausedAnnotation: "false",
	}
	return rollout
}

func testPod(name, hash string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: "test-ns",
		Labels: map[string]string{"app": "example", rolloutPodTemplateHashKey: hash},
	}}
}

func testReplicaSet(name, hash string) *appsv1.ReplicaSet {
	replicas := int32(1)
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test-ns", Labels: map[string]string{"app": "example", rolloutPodTemplateHashKey: hash}},
		Spec:       appsv1.ReplicaSetSpec{Replicas: &replicas, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{}}}},
	}
}

func testLegacyPod(name string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: "test-ns", Labels: map[string]string{"app": "example"},
	}}
}

func testLegacyReplicaSet(name string) *appsv1.ReplicaSet {
	replicas := int32(1)
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test-ns", Labels: map[string]string{"app": "example"}},
		Spec:       appsv1.ReplicaSetSpec{Replicas: &replicas, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "example"}}}},
	}
}

func replicaSetControllerReference(name string) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: name, Controller: &controller}
}

func assertPodWeight(t *testing.T, client *fake.Clientset, name, expected string) {
	t.Helper()
	pod, err := client.CoreV1().Pods("test-ns").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if actual := pod.Annotations[testAnnotationKey]; actual != expected {
		t.Fatalf("pod %s weight mismatch\nexpected: %s\nactual:   %s", name, expected, actual)
	}
}

func assertPodAnnotationAbsent(t *testing.T, client *fake.Clientset, name, key string) {
	t.Helper()
	pod, err := client.CoreV1().Pods("test-ns").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, found := pod.Annotations[key]; found {
		t.Fatalf("pod %s annotation %s must be absent", name, key)
	}
}

func assertReplicaSetWeight(t *testing.T, client *fake.Clientset, name, expected string) {
	t.Helper()
	rs, err := client.AppsV1().ReplicaSets("test-ns").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if actual := rs.Spec.Template.Annotations[testAnnotationKey]; actual != expected {
		t.Fatalf("ReplicaSet %s weight mismatch\nexpected: %s\nactual:   %s", name, expected, actual)
	}
}

func assertNoPodLifecycleActions(t *testing.T, client *fake.Clientset) {
	t.Helper()
	for _, action := range client.Actions() {
		if action.GetResource().Resource == "pods" && (action.GetVerb() == "create" || action.GetVerb() == "delete") {
			t.Fatalf("legacy adoption must not %s Pods: %#v", action.GetVerb(), action)
		}
	}
}
