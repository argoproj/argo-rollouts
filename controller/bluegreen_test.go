package controller

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	core "k8s.io/client-go/testing"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
)

var (
	noTimestamp = metav1.Time{}
)

func newBlueGreenRollout(name string, replicas int, revisionHistoryLimit *int32, activeSvc string, previewSvc string) *v1alpha1.Rollout {
	rollout := newRollout(name, replicas, revisionHistoryLimit, map[string]string{"foo": "bar"})
	rollout.Spec.Strategy.BlueGreenStrategy = &v1alpha1.BlueGreenStrategy{
		ActiveService:  activeSvc,
		PreviewService: previewSvc,
	}
	rollout.Status.CurrentStepHash = conditions.ComputeStepHash(rollout)
	rollout.Status.CurrentPodHash = controller.ComputeHash(&rollout.Spec.Template, rollout.Status.CollisionCount)
	return rollout
}

func TestBlueGreenHandleResetPreviewAfterActiveSet(t *testing.T) {
	f := newFixture(t)

	r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")

	r2 := bumpVersion(r1)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	r2.Status.BlueGreen.PreviousActiveSelector = rs1PodHash
	now := metav1.Now()
	r2.Status.BlueGreen.ScaleDownDelayStartTime = &now
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	previewSvc := newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash})
	f.kubeobjects = append(f.kubeobjects, previewSvc)

	activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash})
	f.kubeobjects = append(f.kubeobjects, activeSvc)

	f.expectGetServiceAction(previewSvc)
	f.expectGetServiceAction(activeSvc)
	f.expectPatchServiceAction(previewSvc, "")
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
}

func TestBlueGreenCreatesReplicaSet(t *testing.T) {
	f := newFixture(t)

	r := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r.Status.Conditions = []v1alpha1.RolloutCondition{}
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)
	previewSvc := newService("preview", 80, nil)
	activeSvc := newService("active", 80, nil)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc)

	rs := newReplicaSet(r, "foo-895c6c4f9", 1)
	generatedConditions := generateConditionsPatch(false, conditions.NewReplicaSetReason, rs, false)

	f.expectGetServiceAction(activeSvc)
	f.expectGetServiceAction(previewSvc)
	f.expectCreateReplicaSetAction(rs)
	updatedRolloutIndex := f.expectUpdateRolloutAction(r)
	expectedPatchWithoutSubs := `{
		"status":{
			"conditions": %s,
			"selector": "foo=bar"
		}
	}`
	expectedPatch := calculatePatch(r, fmt.Sprintf(expectedPatchWithoutSubs, generatedConditions))
	patchRolloutIndex := f.expectPatchRolloutActionWithPatch(r, expectedPatch)
	f.run(getKey(r, t))

	updatedRollout := f.getUpdatedRollout(updatedRolloutIndex)
	updatedProgressingCondition := conditions.GetRolloutCondition(updatedRollout.Status, v1alpha1.RolloutProgressing)
	assert.NotNil(t, updatedProgressingCondition)
	assert.Equal(t, conditions.NewReplicaSetReason, updatedProgressingCondition.Reason)
	assert.Equal(t, corev1.ConditionTrue, updatedProgressingCondition.Status)
	assert.Equal(t, fmt.Sprintf(conditions.NewReplicaSetMessage, rs.Name), updatedProgressingCondition.Message)

	patch := f.getPatchedRollout(patchRolloutIndex)
	assert.Equal(t, expectedPatch, patch)
}

func TestBlueGreenSetPreviewService(t *testing.T) {
	f := newFixture(t)

	r := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	rs := newReplicaSetWithStatus(r, "foo-895c6c4f9", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs)
	f.replicaSetLister = append(f.replicaSetLister, rs)

	previewSvc := newService("preview", 80, nil)
	selector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "test"}
	activeSvc := newService("active", 80, selector)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc)

	f.expectGetServiceAction(activeSvc)
	f.expectGetServiceAction(previewSvc)
	f.expectPatchServiceAction(previewSvc, "")
	f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))
}

func TestBlueGreenHandlePause(t *testing.T) {
	t.Run("AddPause", func(t *testing.T) {
		f := newFixture(t)

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)
		rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
		rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, 2, 1, 1, false, true)
		previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
		previewSvc := newService("preview", 80, previewSelector)
		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

		f.expectGetServiceAction(activeSvc)
		f.expectGetServiceAction(previewSvc)
		addPauseConditionPatchIndex := f.expectPatchRolloutAction(r2)
		f.run(getKey(r2, t))

		patch := f.getPatchedRollout(addPauseConditionPatchIndex)
		f.run(getKey(r2, t))

		expectedPatch := `{
			"spec": {
				"paused": true
			},
			"status": {
				"pauseStartTime": "%s"
			}
		}`
		assert.Equal(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, metav1.Now().UTC().Format(time.RFC3339))), patch)

	})

	t.Run("AddPausedConditionWhilePaused", func(t *testing.T) {
		f := newFixture(t)

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)

		rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
		rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, 2, 1, 1, true, true)

		previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
		previewSvc := newService("preview", 80, previewSelector)
		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

		f.expectGetServiceAction(activeSvc)
		f.expectGetServiceAction(previewSvc)
		addPauseConditionPatchIndex := f.expectPatchRolloutAction(r2)
		f.expectPatchRolloutAction(r2)
		f.run(getKey(r2, t))

		patch := f.getPatchedRollout(addPauseConditionPatchIndex)
		expectedPatch := `{
			"status": {
				"conditions": %s
			}
		}`
		addedConditons := generateConditionsPatch(true, conditions.PausedRolloutReason, rs2, true)
		assert.Equal(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, addedConditons)), patch)
	})

	t.Run("NoActionsAfterPausing", func(t *testing.T) {
		f := newFixture(t)

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)

		rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
		rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, 2, 1, 1, true, true)
		pausedCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, r2)
		conditions.SetRolloutCondition(&r2.Status, pausedCondition)

		previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
		previewSvc := newService("preview", 80, previewSelector)
		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

		f.expectGetServiceAction(activeSvc)
		f.expectGetServiceAction(previewSvc)
		patchIndex := f.expectPatchRolloutActionWithPatch(r2, OnlyObservedGenerationPatch)
		f.run(getKey(r2, t))
		patch := f.getPatchedRollout(patchIndex)
		assert.Equal(t, calculatePatch(r2, OnlyObservedGenerationPatch), patch)
	})

	t.Run("NoAutoPromoteBeforeDelayTimePasses", func(t *testing.T) {
		f := newFixture(t)

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)
		r2.Spec.Strategy.BlueGreenStrategy.AutoPromotionSeconds = pointer.Int32Ptr(10)

		rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
		rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, 2, 1, 1, true, true)
		pausedCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, r2)
		conditions.SetRolloutCondition(&r2.Status, pausedCondition)

		previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
		previewSvc := newService("preview", 80, previewSelector)
		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

		f.expectGetServiceAction(activeSvc)
		f.expectGetServiceAction(previewSvc)
		patchIndex := f.expectPatchRolloutActionWithPatch(r2, OnlyObservedGenerationPatch)
		f.run(getKey(r2, t))
		patch := f.getPatchedRollout(patchIndex)
		assert.Equal(t, calculatePatch(r2, OnlyObservedGenerationPatch), patch)
	})

	t.Run("AutoPromoteAfterDelayTimePasses", func(t *testing.T) {
		f := newFixture(t)

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)
		r2.Spec.Strategy.BlueGreenStrategy.AutoPromotionSeconds = pointer.Int32Ptr(10)

		rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
		rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, 2, 1, 1, true, true)
		now := metav1.Now()
		before := metav1.NewTime(now.Add(-1 * time.Minute))
		r2.Status.PauseStartTime = &before
		pausedCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, rs2)
		conditions.SetRolloutCondition(&r2.Status, pausedCondition)

		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector)
		previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
		previewSvc := newService("preview", 80, previewSelector)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, activeSvc, previewSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

		f.expectGetServiceAction(activeSvc)
		f.expectGetServiceAction(previewSvc)
		expectedPatchWithoutSubs := `{
			"spec": {
				"paused": null
			},
			"status": {
				"pauseStartTime": null
			}
		}`
		expectedPatch := calculatePatch(r2, expectedPatchWithoutSubs)
		patchRolloutIndex := f.expectPatchRolloutActionWithPatch(r2, expectedPatch)
		f.run(getKey(r2, t))

		rolloutPatch := f.getPatchedRollout(patchRolloutIndex)
		assert.Equal(t, expectedPatch, rolloutPatch)
	})

	t.Run("NoPauseWhenAutoPromotionEnabledIsNotSet", func(t *testing.T) {
		f := newFixture(t)

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
		r2 := bumpVersion(r1)

		rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
		rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, "", rs1PodHash, 2, 1, 1, false, true)

		progressingCondition, _ := newProgressingCondition(conditions.NewReplicaSetReason, rs2)
		conditions.SetRolloutCondition(&r2.Status, progressingCondition)
		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, activeSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

		f.expectGetServiceAction(activeSvc)
		servicePatchIndex := f.expectPatchServiceAction(activeSvc, rs2PodHash)

		generatedConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs2, true)
		now := metav1.Now().UTC().Format(time.RFC3339)
		newSelector := metav1.FormatLabelSelector(rs2.Spec.Selector)
		expectedPatchWithoutSubs := `{
			"status": {
				"blueGreen": {
					"activeSelector": "%s",
					"previousActiveSelector": "%s",
					"scaleDownDelayStartTime": "%s"
				},
				"conditions": %s,
				"selector": "%s"
			}
		}`
		expectedPatch := calculatePatch(r2, fmt.Sprintf(expectedPatchWithoutSubs, rs2PodHash, rs1PodHash, now, generatedConditions, newSelector))
		patchIndex := f.expectPatchRolloutActionWithPatch(r2, expectedPatch)
		f.run(getKey(r2, t))

		assert.True(t, f.verifyPatchedService(servicePatchIndex, rs2PodHash))

		rolloutPatch := f.getPatchedRollout(patchIndex)
		assert.Equal(t, expectedPatch, rolloutPatch)
	})

	t.Run("PauseWhenAutoPromotionEnabledIsFalse", func(t *testing.T) {
		f := newFixture(t)

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
		r1.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)

		rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
		rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, "", rs1PodHash, 2, 1, 1, false, true)

		progressingCondition, _ := newProgressingCondition(conditions.NewReplicaSetReason, rs2)
		conditions.SetRolloutCondition(&r2.Status, progressingCondition)
		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, activeSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

		f.expectGetServiceAction(activeSvc)

		now := metav1.Now().UTC().Format(time.RFC3339)
		expectedPatchWithoutSubs := `{
			"spec": {
				"paused": true
			},
			"status": {
				"pauseStartTime": "%s"
			}
		}`
		expectedPatch := calculatePatch(r2, fmt.Sprintf(expectedPatchWithoutSubs, now))
		patchIndex := f.expectPatchRolloutActionWithPatch(r2, expectedPatch)
		f.run(getKey(r2, t))

		rolloutPatch := f.getPatchedRollout(patchIndex)
		assert.Equal(t, expectedPatch, rolloutPatch)
	})

	t.Run("SkipPreviewWhenActiveHasNoSelector", func(t *testing.T) {
		f := newFixture(t)

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled = pointer.BoolPtr(false)

		rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r1 = updateBlueGreenRolloutStatus(r1, "", "", 1, 1, 1, false, false)

		activeSvc := newService("active", 80, nil)
		previewSvc := newService("preview", 80, nil)

		f.objects = append(f.objects, r1)
		f.kubeobjects = append(f.kubeobjects, activeSvc, previewSvc, rs1)
		f.rolloutLister = append(f.rolloutLister, r1)
		f.replicaSetLister = append(f.replicaSetLister, rs1)

		f.expectGetServiceAction(activeSvc)
		f.expectGetServiceAction(previewSvc)
		servicePatchIndex := f.expectPatchServiceAction(activeSvc, rs1PodHash)
		expectedPatchWithoutSubs := `{
			"status": {
				"blueGreen": {
					"activeSelector": "%s"
				},
				"conditions": %s,
				"selector": "%s"
			}
		}`

		generateConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs1, false)
		newSelector := metav1.FormatLabelSelector(rs1.Spec.Selector)
		expectedPatch := calculatePatch(r1, fmt.Sprintf(expectedPatchWithoutSubs, rs1PodHash, generateConditions, newSelector))
		patchRolloutIndex := f.expectPatchRolloutActionWithPatch(r1, expectedPatch)
		f.run(getKey(r1, t))

		assert.True(t, f.verifyPatchedService(servicePatchIndex, rs1PodHash))

		rolloutPatch := f.getPatchedRollout(patchRolloutIndex)
		assert.Equal(t, expectedPatch, rolloutPatch)
	})

	t.Run("ContinueAfterUnpaused", func(t *testing.T) {
		f := newFixture(t)

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)

		rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
		rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, 2, 1, 1, false, true)
		now := metav1.Now()
		r2.Status.PauseStartTime = &now
		pausedCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, rs2)
		conditions.SetRolloutCondition(&r2.Status, pausedCondition)

		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector)
		previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
		previewSvc := newService("preview", 80, previewSelector)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, activeSvc, previewSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

		f.expectGetServiceAction(activeSvc)
		f.expectGetServiceAction(previewSvc)
		servicePatchIndex := f.expectPatchServiceAction(activeSvc, rs2PodHash)
		unpausePatchIndex := f.expectPatchRolloutAction(r2)
		patchRolloutIndex := f.expectPatchRolloutAction(r2)
		f.run(getKey(r2, t))

		assert.True(t, f.verifyPatchedService(servicePatchIndex, rs2PodHash))

		unpausePatch := f.getPatchedRollout(unpausePatchIndex)
		unpauseConditions := generateConditionsPatch(true, conditions.ResumedRolloutReason, rs2, true)
		expectedUnpausePatch := `{
			"status": {
				"conditions": %s
			}
		}`
		assert.Equal(t, calculatePatch(r2, fmt.Sprintf(expectedUnpausePatch, unpauseConditions)), unpausePatch)

		generatedConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs2, true)
		expected2ndPatchWithoutSubs := `{
			"status": {
				"blueGreen": {
					"activeSelector": "%s",
					"previousActiveSelector": "%s",
					"scaleDownDelayStartTime": "%s"
				},
				"pauseStartTime": null,
				"conditions": %s,
				"selector": "%s"
			}
		}`
		newSelector := metav1.FormatLabelSelector(rs2.Spec.Selector)
		expected2ndPatch := calculatePatch(r2, fmt.Sprintf(expected2ndPatchWithoutSubs, rs2PodHash, rs1PodHash, now.UTC().Format(time.RFC3339), generatedConditions, newSelector))
		rollout2ndPatch := f.getPatchedRollout(patchRolloutIndex)
		assert.Equal(t, expected2ndPatch, rollout2ndPatch)
	})
}

func TestBlueGreenSkipPreviewUpdateActive(t *testing.T) {
	f := newFixture(t)

	r := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r.Status.AvailableReplicas = 1
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	rs := newReplicaSetWithStatus(r, "foo-895c6c4f9", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs)
	f.replicaSetLister = append(f.replicaSetLister, rs)

	previewSvc := newService("preview", 80, nil)
	activeSvc := newService("active", 80, nil)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc)

	f.expectGetServiceAction(activeSvc)
	f.expectGetServiceAction(previewSvc)
	f.expectPatchServiceAction(activeSvc, rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey])
	f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))
}

func TestBlueGreenAddScaleDownDelayStartTime(t *testing.T) {
	f := newFixture(t)

	r1 := newBlueGreenRollout("foo", 1, nil, "bar", "")
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	s := newService("bar", 80, serviceSelector)
	f.kubeobjects = append(f.kubeobjects, s, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateBlueGreenRolloutStatus(r2, "", rs1PodHash, 2, 1, 1, false, true)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectGetServiceAction(s)
	f.expectPatchServiceAction(s, rs2PodHash)
	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)

	expectedPatchWithoutSubs := `{
		"status":{
			"blueGreen": {
				"activeSelector": "%s",
				"previousActiveSelector": "%s",
				"scaleDownDelayStartTime": "%s"
			},
			"conditions": %s,
			"selector": "%s"
		}
	}`
	newSelector := metav1.FormatLabelSelector(rs2.Spec.Selector)
	expectedCondition := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs2, true)
	expectedPatch := calculatePatch(r2, fmt.Sprintf(expectedPatchWithoutSubs, rs2PodHash, rs1PodHash, metav1.Now().UTC().Format(time.RFC3339), expectedCondition, newSelector))
	assert.Equal(t, expectedPatch, patch)
}

func TestBlueGreenWaitForScaleDownDelay(t *testing.T) {
	f := newFixture(t)

	r1 := newBlueGreenRollout("foo", 1, nil, "bar", "")
	r2 := bumpVersion(r1)

	before := metav1.Now().Add(-1 * time.Second)
	r2.Status.BlueGreen.ScaleDownDelayStartTime = &metav1.Time{Time: before}

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2.Status.BlueGreen.PreviousActiveSelector = rs1PodHash
	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, 2, 1, 1, false, true)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	s := newService("bar", 80, serviceSelector)
	f.kubeobjects = append(f.kubeobjects, s)

	expRS := rs2.DeepCopy()
	expRS.Annotations[annotations.DesiredReplicasAnnotation] = "0"
	f.expectGetServiceAction(s)
	patchIndex := f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	assert.Equal(t, calculatePatch(r2, OnlyObservedGenerationPatch), patch)
}

func TestBlueGreenScaleDownOldRS(t *testing.T) {
	f := newFixture(t)

	r1 := newBlueGreenRollout("foo", 1, nil, "bar", "")

	r2 := bumpVersion(r1)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	before := metav1.Now().Add(-1 * time.Minute)
	r2.Status.BlueGreen.ScaleDownDelayStartTime = &metav1.Time{Time: before}

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	s := newService("bar", 80, serviceSelector)
	f.kubeobjects = append(f.kubeobjects, s)

	expRS := rs2.DeepCopy()
	expRS.Annotations[annotations.DesiredReplicasAnnotation] = "0"
	f.expectGetServiceAction(s)
	f.expectUpdateReplicaSetAction(expRS)
	f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
}

func TestBlueGreenRolloutStatusHPAStatusFieldsActiveSelectorSet(t *testing.T) {
	f := newFixture(t)

	r := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled = pointer.BoolPtr(false)
	r2 := bumpVersion(r)

	rs1 := newReplicaSetWithStatus(r, "foo-867bc46cdc", 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	previewSvc := newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash})
	activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash})

	r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, 0, 0, 0, true, false)
	r2.Status.Selector = ""
	pausedCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, rs2)
	conditions.SetRolloutCondition(&r2.Status, pausedCondition)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, previewSvc, activeSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	expectedPatchWithoutSubs := `{
		"status":{
			"HPAReplicas":1,
			"availableReplicas":2,
			"updatedReplicas":1,
			"replicas":2,
			"selector":"foo=bar,rollouts-pod-template-hash=%s"
		}
	}`
	//_, availableStr := newAvailableCondition(true)
	expectedPatch := calculatePatch(r2, fmt.Sprintf(expectedPatchWithoutSubs, rs1PodHash))

	f.expectGetServiceAction(activeSvc)
	f.expectGetServiceAction(previewSvc)
	patchIndex := f.expectPatchRolloutActionWithPatch(r2, expectedPatch)
	f.run(getKey(r2, t))

	rolloutPatch := f.getPatchedRollout(patchIndex)
	assert.Equal(t, expectedPatch, rolloutPatch)
}

func TestBlueGreenRolloutStatusHPAStatusFieldsNoActiveSelector(t *testing.T) {
	ro := newBlueGreenRollout("foo", 2, nil, "active", "")
	rs := newReplicaSetWithStatus(ro, "foo-1", 1, 1)
	ro.Status.CurrentPodHash = rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: ""})

	progressingCondition, progressingConditionStr := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs)
	conditions.SetRolloutCondition(&ro.Status, progressingCondition)

	f := newFixture(t)
	c, _, _ := f.newController(noResyncPeriodFunc)

	err := c.syncRolloutStatusBlueGreen([]*appsv1.ReplicaSet{}, rs, nil, activeSvc, ro, false)
	assert.Nil(t, err)
	assert.Len(t, f.client.Actions(), 1)
	result := f.client.Actions()[0].(core.PatchAction).GetPatch()
	_, availableStr := newAvailableCondition(false)
	expectedPatchWithoutSub := `{
		"status":{
			"HPAReplicas":1,
			"availableReplicas": 1,
			"updatedReplicas":1,
			"replicas":1,
			"conditions":[%s, %s],
			"selector":"foo=bar"
		}
	}`
	expectedPatch := calculatePatch(ro, fmt.Sprintf(expectedPatchWithoutSub, progressingConditionStr, availableStr))
	assert.Equal(t, expectedPatch, string(result))
}

func TestBlueGreenRolloutScaleUpdateActiveRS(t *testing.T) {
	f := newFixture(t)

	r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
	r2 := bumpVersion(r1)

	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2.Spec.Replicas = pointer.Int32Ptr(2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	previewSvc := newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash})
	activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash})
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc)

	f.expectGetServiceAction(previewSvc)
	f.expectGetServiceAction(activeSvc)
	f.expectUpdateReplicaSetAction(rs1)
	f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
}

func TestBlueGreenRolloutScaleUpdatePreviewRS(t *testing.T) {
	f := newFixture(t)

	r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r1.Spec.Strategy.BlueGreenStrategy.PreviewReplicaCount = pointer.Int32Ptr(123)
	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
	rs1.Spec.Replicas = pointer.Int32Ptr(2)
	r2 := bumpVersion(r1)

	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2.Spec.Replicas = pointer.Int32Ptr(2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	previewSvc := newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash})
	activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash})

	r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, 2, 1, 1, false, true)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc)

	f.expectGetServiceAction(previewSvc)
	f.expectGetServiceAction(activeSvc)
	rs2idx := f.expectUpdateReplicaSetAction(rs2)
	f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
	rs2Updated := f.getUpdatedReplicaSet(rs2idx)
	assert.Equal(t, int32(123), *rs2Updated.Spec.Replicas)
}

func TestBlueGreenRolloutScalePreviewActiveRS(t *testing.T) {
	f := newFixture(t)

	r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 2, 2)
	r2 := bumpVersion(r1)

	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2.Spec.Replicas = pointer.Int32Ptr(2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	previewSvc := newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash})
	activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash})
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc)

	f.expectGetServiceAction(previewSvc)
	f.expectGetServiceAction(activeSvc)
	f.expectUpdateReplicaSetAction(rs2)
	f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
}

func TestBlueGreenRolloutCompleted(t *testing.T) {
	f := newFixture(t)

	r1 := newBlueGreenRollout("foo", 1, nil, "bar", "")
	r2 := bumpVersion(r1)

	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	s := newService("bar", 80, serviceSelector)
	f.kubeobjects = append(f.kubeobjects, s)

	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, 1, 1, 1, false, true)
	r2.Status.ObservedGeneration = conditions.ComputeGenerationHash(r2.Spec)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectGetServiceAction(s)
	patchIndex := f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))

	newConditions := generateConditionsPatch(true, conditions.NewRSAvailableReason, rs2, true)
	expectedPatch := fmt.Sprintf(`{
		"status":{
			"conditions":%s
		}
	}`, newConditions)
	patch := f.getPatchedRollout(patchIndex)
	assert.Equal(t, cleanPatch(expectedPatch), patch)
}
