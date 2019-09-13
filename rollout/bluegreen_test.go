package rollout

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
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")

	r2 := bumpVersion(r1)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	future := metav1.NewTime(metav1.Now().Add(10 * time.Second)).UTC().Format(time.RFC3339)
	rs1.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = future
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	previewSvc := newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash})
	f.kubeobjects = append(f.kubeobjects, previewSvc)
	f.serviceLister = append(f.serviceLister, previewSvc)

	activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash})
	f.kubeobjects = append(f.kubeobjects, activeSvc)
	f.serviceLister = append(f.serviceLister, activeSvc)

	f.expectPatchServiceAction(previewSvc, "")
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
}

func TestBlueGreenCreatesReplicaSet(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r.Status.Conditions = []v1alpha1.RolloutCondition{}
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)
	previewSvc := newService("preview", 80, nil)
	activeSvc := newService("active", 80, nil)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc)
	f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

	rs := newReplicaSet(r, 1)
	generatedConditions := generateConditionsPatch(false, conditions.NewReplicaSetReason, rs, false)

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
	defer f.Close()

	r := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	rs := newReplicaSetWithStatus(r, 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs)
	f.replicaSetLister = append(f.replicaSetLister, rs)

	previewSvc := newService("preview", 80, nil)
	selector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "test"}
	activeSvc := newService("active", 80, selector)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc)
	f.serviceLister = append(f.serviceLister, previewSvc, activeSvc)

	f.expectPatchServiceAction(previewSvc, "")
	f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))
}

func TestBlueGreenHandlePause(t *testing.T) {
	t.Run("AddPause", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)
		rs1 := newReplicaSetWithStatus(r1, 1, 1)
		rs2 := newReplicaSetWithStatus(r2, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, 1, 1, 2, 1, false, true)
		previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
		previewSvc := newService("preview", 80, previewSelector)
		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
		f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

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
		defer f.Close()
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)

		rs1 := newReplicaSetWithStatus(r1, 1, 1)
		rs2 := newReplicaSetWithStatus(r2, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, 1, 1, 2, 1, true, true)

		previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
		previewSvc := newService("preview", 80, previewSelector)
		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
		f.serviceLister = append(f.serviceLister, previewSvc, activeSvc)

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
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)

		rs1 := newReplicaSetWithStatus(r1, 1, 1)
		rs2 := newReplicaSetWithStatus(r2, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, 1, 1, 2, 1, true, true)
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
		f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

		patchIndex := f.expectPatchRolloutActionWithPatch(r2, OnlyObservedGenerationPatch)
		f.run(getKey(r2, t))
		patch := f.getPatchedRollout(patchIndex)
		assert.Equal(t, calculatePatch(r2, OnlyObservedGenerationPatch), patch)
	})

	t.Run("NoAutoPromoteBeforeDelayTimePasses", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)
		r2.Spec.Strategy.BlueGreenStrategy.AutoPromotionSeconds = pointer.Int32Ptr(10)

		rs1 := newReplicaSetWithStatus(r1, 1, 1)
		rs2 := newReplicaSetWithStatus(r2, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, 1, 1, 2, 1, true, true)
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
		f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

		patchIndex := f.expectPatchRolloutActionWithPatch(r2, OnlyObservedGenerationPatch)
		f.run(getKey(r2, t))
		patch := f.getPatchedRollout(patchIndex)
		assert.Equal(t, calculatePatch(r2, OnlyObservedGenerationPatch), patch)
	})

	t.Run("AutoPromoteAfterDelayTimePasses", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)
		r2.Spec.Strategy.BlueGreenStrategy.AutoPromotionSeconds = pointer.Int32Ptr(10)

		rs1 := newReplicaSetWithStatus(r1, 1, 1)
		rs2 := newReplicaSetWithStatus(r2, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, 1, 1, 2, 1, true, true)
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
		f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

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
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
		r2 := bumpVersion(r1)

		rs1 := newReplicaSetWithStatus(r1, 1, 1)
		rs2 := newReplicaSetWithStatus(r2, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, "", rs1PodHash, 1, 1, 2, 1, false, true)
		r2.Spec.Strategy.BlueGreenStrategy.ScaleDownDelaySeconds = pointer.Int32Ptr(10)

		progressingCondition, _ := newProgressingCondition(conditions.NewReplicaSetReason, rs2)
		conditions.SetRolloutCondition(&r2.Status, progressingCondition)
		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, activeSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
		f.serviceLister = append(f.serviceLister, activeSvc)

		servicePatchIndex := f.expectPatchServiceAction(activeSvc, rs2PodHash)
		patchedRSIndex := f.expectPatchReplicaSetAction(rs1)

		generatedConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs2, true)
		newSelector := metav1.FormatLabelSelector(rs2.Spec.Selector)
		expectedPatchWithoutSubs := `{
			"status": {
				"blueGreen": {
					"activeSelector": "%s"
				},
				"conditions": %s,
				"selector": "%s"
			}
		}`
		expectedPatch := calculatePatch(r2, fmt.Sprintf(expectedPatchWithoutSubs, rs2PodHash, generatedConditions, newSelector))
		patchIndex := f.expectPatchRolloutActionWithPatch(r2, expectedPatch)
		f.run(getKey(r2, t))
		assert.True(t, f.verifyPatchedService(servicePatchIndex, rs2PodHash))
		assert.True(t, f.verifyPatchedReplicaSet(patchedRSIndex, 10))

		rolloutPatch := f.getPatchedRollout(patchIndex)
		assert.Equal(t, expectedPatch, rolloutPatch)
	})

	t.Run("PauseWhenAutoPromotionEnabledIsFalse", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
		r1.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)

		rs1 := newReplicaSetWithStatus(r1, 1, 1)
		rs2 := newReplicaSetWithStatus(r2, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, "", rs1PodHash, 1, 1, 2, 1, false, true)

		progressingCondition, _ := newProgressingCondition(conditions.NewReplicaSetReason, rs2)
		conditions.SetRolloutCondition(&r2.Status, progressingCondition)
		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, activeSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
		f.serviceLister = append(f.serviceLister, activeSvc)

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
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled = pointer.BoolPtr(false)

		rs1 := newReplicaSetWithStatus(r1, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r1 = updateBlueGreenRolloutStatus(r1, "", "", 1, 1, 1, 1, false, false)

		activeSvc := newService("active", 80, nil)
		previewSvc := newService("preview", 80, nil)

		f.objects = append(f.objects, r1)
		f.kubeobjects = append(f.kubeobjects, activeSvc, previewSvc, rs1)
		f.rolloutLister = append(f.rolloutLister, r1)
		f.replicaSetLister = append(f.replicaSetLister, rs1)
		f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

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
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)

		rs1 := newReplicaSetWithStatus(r1, 1, 1)
		rs2 := newReplicaSetWithStatus(r2, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2.Spec.Strategy.BlueGreenStrategy.ScaleDownDelaySeconds = pointer.Int32Ptr(10)
		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, 1, 1, 2, 1, false, true)
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
		f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

		servicePatchIndex := f.expectPatchServiceAction(activeSvc, rs2PodHash)
		patchedRSIndex := f.expectPatchReplicaSetAction(rs1)
		unpausePatchIndex := f.expectPatchRolloutAction(r2)
		patchRolloutIndex := f.expectPatchRolloutAction(r2)
		f.run(getKey(r2, t))

		assert.True(t, f.verifyPatchedService(servicePatchIndex, rs2PodHash))
		assert.True(t, f.verifyPatchedReplicaSet(patchedRSIndex, 10))
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
					"activeSelector": "%s"
				},
				"pauseStartTime": null,
				"conditions": %s,
				"selector": "%s"
			}
		}`
		newSelector := metav1.FormatLabelSelector(rs2.Spec.Selector)
		expected2ndPatch := calculatePatch(r2, fmt.Sprintf(expected2ndPatchWithoutSubs, rs2PodHash, generatedConditions, newSelector))
		rollout2ndPatch := f.getPatchedRollout(patchRolloutIndex)
		assert.Equal(t, expected2ndPatch, rollout2ndPatch)
	})
}

func TestBlueGreenSkipPreviewUpdateActive(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r.Status.AvailableReplicas = 1
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	rs := newReplicaSetWithStatus(r, 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs)
	f.replicaSetLister = append(f.replicaSetLister, rs)

	previewSvc := newService("preview", 80, nil)
	activeSvc := newService("active", 80, nil)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc)
	f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

	f.expectPatchServiceAction(activeSvc, rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey])
	f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))
}

func TestBlueGreenAddScaleDownDelayToPreviousActiveService(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "bar", "")
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	s := newService("bar", 80, serviceSelector)
	f.kubeobjects = append(f.kubeobjects, s, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2.Spec.Strategy.BlueGreenStrategy.ScaleDownDelaySeconds = pointer.Int32Ptr(10)
	r2 = updateBlueGreenRolloutStatus(r2, "", rs1PodHash, 1, 1, 2, 1, false, true)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.serviceLister = append(f.serviceLister, s)

	f.expectPatchServiceAction(s, rs2PodHash)
	patchedRSIndex := f.expectPatchReplicaSetAction(rs1)
	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	assert.True(t, f.verifyPatchedReplicaSet(patchedRSIndex, 10))

	patch := f.getPatchedRollout(patchIndex)
	expectedPatchWithoutSubs := `{
		"status":{
			"blueGreen": {
				"activeSelector": "%s"
			},
			"conditions": %s,
			"selector": "%s"
		}
	}`
	newSelector := metav1.FormatLabelSelector(rs2.Spec.Selector)
	expectedCondition := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs2, true)
	expectedPatch := calculatePatch(r2, fmt.Sprintf(expectedPatchWithoutSubs, rs2PodHash, expectedCondition, newSelector))
	assert.Equal(t, expectedPatch, patch)
}

func TestBlueGreenRolloutStatusHPAStatusFieldsActiveSelectorSet(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r.Spec.Strategy.BlueGreenStrategy.AutoPromotionEnabled = pointer.BoolPtr(false)
	r2 := bumpVersion(r)

	rs1 := newReplicaSetWithStatus(r, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	previewSvc := newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash})
	activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash})

	r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, 0, 0, 0, 0, true, false)
	r2.Status.Selector = ""
	pausedCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, rs2)
	conditions.SetRolloutCondition(&r2.Status, pausedCondition)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, previewSvc, activeSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

	expectedPatchWithoutSubs := `{
		"status":{
			"HPAReplicas":1,
			"availableReplicas":1,
			"updatedReplicas":1,
			"replicas":2,
			"selector":"foo=bar,rollouts-pod-template-hash=%s"
		}
	}`
	//_, availableStr := newAvailableCondition(true)
	expectedPatch := calculatePatch(r2, fmt.Sprintf(expectedPatchWithoutSubs, rs1PodHash))

	patchIndex := f.expectPatchRolloutActionWithPatch(r2, expectedPatch)
	f.run(getKey(r2, t))

	rolloutPatch := f.getPatchedRollout(patchIndex)
	assert.Equal(t, expectedPatch, rolloutPatch)
}

func TestBlueGreenRolloutStatusHPAStatusFieldsNoActiveSelector(t *testing.T) {
	ro := newBlueGreenRollout("foo", 2, nil, "active", "")
	rs := newReplicaSetWithStatus(ro, 1, 1)
	ro.Status.CurrentPodHash = rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: ""})

	progressingCondition, progressingConditionStr := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs)
	conditions.SetRolloutCondition(&ro.Status, progressingCondition)

	f := newFixture(t)
	defer f.Close()
	f.objects = append(f.objects, ro)
	f.rolloutLister = append(f.rolloutLister, ro)
	f.replicaSetLister = append(f.replicaSetLister, rs)

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
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	r2 := bumpVersion(r1)

	rs2 := newReplicaSetWithStatus(r2, 1, 1)
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
	f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

	f.expectUpdateReplicaSetAction(rs1)
	f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
}

func TestBlueGreenRolloutIgnoringScalingUsePreviewRSCount(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r1.Spec.Strategy.BlueGreenStrategy.PreviewReplicaCount = pointer.Int32Ptr(3)
	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs1.Spec.Replicas = pointer.Int32Ptr(2)
	r2 := bumpVersion(r1)

	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	previewSvc := newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash})
	activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash})

	r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, 2, 1, 1, 1, false, true)
	// Scaling up the rollout
	r2.Spec.Replicas = pointer.Int32Ptr(2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc)
	f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

	rs2idx := f.expectUpdateReplicaSetAction(rs2)
	f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
	rs2Updated := f.getUpdatedReplicaSet(rs2idx)
	assert.Equal(t, int32(3), *rs2Updated.Spec.Replicas)
}

func TestBlueGreenRolloutScalePreviewActiveRS(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	rs1 := newReplicaSetWithStatus(r1, 2, 2)
	r2 := bumpVersion(r1)

	rs2 := newReplicaSetWithStatus(r2, 1, 1)
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
	f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

	f.expectUpdateReplicaSetAction(rs2)
	f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
}

func TestBlueGreenRolloutCompleted(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "bar", "")
	r2 := bumpVersion(r1)

	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	s := newService("bar", 80, serviceSelector)
	f.kubeobjects = append(f.kubeobjects, s)

	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, 1, 1, 1, 1, false, true)
	r2.Status.ObservedGeneration = conditions.ComputeGenerationHash(r2.Spec)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.serviceLister = append(f.serviceLister, s)

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

func TestBlueGreenUnableToReadScaleDownAt(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "bar", "")
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	rs1.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = "Abcd123"

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	s := newService("bar", 80, serviceSelector)
	f.kubeobjects = append(f.kubeobjects, s, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, 1, 1, 2, 1, false, true)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.serviceLister = append(f.serviceLister, s)

	updatedRSIndex := f.expectUpdateReplicaSetAction(rs2)
	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	updatedRS := f.getUpdatedReplicaSet(updatedRSIndex)
	assert.Equal(t, int32(0), *updatedRS.Spec.Replicas)
	patch := f.getPatchedRollout(patchIndex)

	expectedPatch := calculatePatch(r2, OnlyObservedGenerationPatch)
	assert.Equal(t, expectedPatch, patch)

}

func TestBlueGreenNotReadyToScaleDownOldReplica(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "bar", "")
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	inTheFuture := metav1.Now().Add(10 * time.Second).UTC().Format(time.RFC3339)

	rs1.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inTheFuture

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	s := newService("bar", 80, serviceSelector)
	f.kubeobjects = append(f.kubeobjects, s, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, 1, 1, 2, 1, false, true)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.serviceLister = append(f.serviceLister, s)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := calculatePatch(r2, OnlyObservedGenerationPatch)
	assert.Equal(t, expectedPatch, patch)
}

func TestBlueGreenReadyToScaleDownOldReplica(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "bar", "")
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	inThePast := metav1.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339)

	rs1.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inThePast

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	s := newService("bar", 80, serviceSelector)
	f.kubeobjects = append(f.kubeobjects, s, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, 1, 1, 2, 1, false, true)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.serviceLister = append(f.serviceLister, s)

	updatedRSIndex := f.expectUpdateReplicaSetAction(rs2)
	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	updatedRS := f.getUpdatedReplicaSet(updatedRSIndex)
	assert.Equal(t, int32(0), *updatedRS.Spec.Replicas)

	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := calculatePatch(r2, OnlyObservedGenerationPatch)
	assert.Equal(t, expectedPatch, patch)
}

func TestFastRollback(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "bar", "")
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	//Setting the scaleDownAt time
	inTheFuture := metav1.Now().Add(10 * time.Second).UTC().Format(time.RFC3339)
	rs1.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inTheFuture
	rs2.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inTheFuture

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	s := newService("bar", 80, serviceSelector)
	f.kubeobjects = append(f.kubeobjects, s, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	// Switch back to version 1
	r2.Spec.Template = r1.Spec.Template
	r2.Annotations[annotations.RevisionAnnotation] = "3"
	r2.Status.CurrentPodHash = rs1PodHash
	rs1.Annotations[annotations.RevisionAnnotation] = "3"

	r2 = updateBlueGreenRolloutStatus(r2, "", rs1PodHash, 1, 1, 2, 1, false, true)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.serviceLister = append(f.serviceLister, s)

	f.expectPatchReplicaSetAction(rs1)
	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := calculatePatch(r2, OnlyObservedGenerationPatch)
	assert.Equal(t, expectedPatch, patch)
}

func TestScaleDownLimit(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "bar", "")
	r2 := bumpVersion(r1)
	r3 := bumpVersion(r2)
	r3.Spec.Strategy.BlueGreenStrategy.ScaleDownDelayRevisionLimit = pointer.Int32Ptr(2)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs3 := newReplicaSetWithStatus(r3, 1, 1)
	rs3PodHash := rs3.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	//Setting the scaleDownAt time
	inTheFuture := metav1.Now().Add(10 * time.Second).UTC().Format(time.RFC3339)
	rs1.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inTheFuture
	rs2.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inTheFuture

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs3PodHash}
	s := newService("bar", 80, serviceSelector)
	f.kubeobjects = append(f.kubeobjects, s, rs1, rs2, rs3)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2, rs3)

	r3 = updateBlueGreenRolloutStatus(r3, "", rs3PodHash, 1, 1, 3, 1, false, true)
	f.rolloutLister = append(f.rolloutLister, r3)
	f.objects = append(f.objects, r3)
	f.serviceLister = append(f.serviceLister, s)

	updateRSIndex := f.expectUpdateReplicaSetAction(rs1)
	patchIndex := f.expectPatchRolloutAction(r3)
	f.run(getKey(r3, t))

	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := calculatePatch(r3, OnlyObservedGenerationPatch)
	assert.Equal(t, expectedPatch, patch)

	updatedRS := f.getUpdatedReplicaSet(updateRSIndex)
	assert.Equal(t, int32(0), *updatedRS.Spec.Replicas)
	assert.Equal(t, rs1.Name, updatedRS.Name)

}
