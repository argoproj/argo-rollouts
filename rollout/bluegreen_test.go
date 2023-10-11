package rollout

import (
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	core "k8s.io/client-go/testing"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/hash"
	rolloututil "github.com/argoproj/argo-rollouts/utils/rollout"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

var (
	noTimestamp = metav1.Time{}
)

func newBlueGreenRollout(name string, replicas int, revisionHistoryLimit *int32, activeSvc string, previewSvc string) *v1alpha1.Rollout {
	rollout := newRollout(name, replicas, revisionHistoryLimit, map[string]string{"foo": "bar"})
	abortScaleDownDelaySeconds := int32(0)
	rollout.Spec.Strategy.BlueGreen = &v1alpha1.BlueGreenStrategy{
		ActiveService:              activeSvc,
		PreviewService:             previewSvc,
		AbortScaleDownDelaySeconds: &abortScaleDownDelaySeconds,
	}
	rollout.Status.CurrentStepHash = conditions.ComputeStepHash(rollout)
	rollout.Status.CurrentPodHash = hash.ComputePodTemplateHash(&rollout.Spec.Template, rollout.Status.CollisionCount)
	return rollout
}

func TestBlueGreenCompletedRolloutRestart(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r.Status.Conditions = []v1alpha1.RolloutCondition{}

	completedHealthyCond := conditions.NewRolloutCondition(v1alpha1.RolloutHealthy, corev1.ConditionFalse, conditions.RolloutHealthyReason, conditions.RolloutNotHealthyMessage)
	conditions.SetRolloutCondition(&r.Status, *completedHealthyCond)
	completedCond, _ := newCompletedCondition(true)
	conditions.SetRolloutCondition(&r.Status, completedCond)

	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)
	previewSvc := newService("preview", 80, nil, r)
	activeSvc := newService("active", 80, nil, r)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc)
	f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

	rs := newReplicaSet(r, 1)
	rsPodHash := rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	generatedConditions := generateConditionsPatchWithCompletedHealthy(false, conditions.ReplicaSetUpdatedReason, rs, false, "", false, true)

	f.expectCreateReplicaSetAction(rs)
	servicePatchIndex := f.expectPatchServiceAction(previewSvc, rsPodHash)
	f.expectUpdateReplicaSetAction(rs) // scale up RS
	updatedRolloutIndex := f.expectUpdateRolloutStatusAction(r)
	expectedPatchWithoutSubs := `{
		"status":{
			"blueGreen" : {
				"previewSelector": "%s"
			},
			"conditions": %s,
			"selector": "foo=bar",
			"stableRS": "%s",
			"phase": "Progressing",
			"message": "more replicas need to be updated"
		}
	}`
	expectedPatch := calculatePatch(r, fmt.Sprintf(expectedPatchWithoutSubs, rsPodHash, generatedConditions, rsPodHash))
	patchRolloutIndex := f.expectPatchRolloutActionWithPatch(r, expectedPatch)
	f.run(getKey(r, t))

	f.verifyPatchedService(servicePatchIndex, rsPodHash, "")

	updatedRollout := f.getUpdatedRollout(updatedRolloutIndex)
	updatedProgressingCondition := conditions.GetRolloutCondition(updatedRollout.Status, v1alpha1.RolloutProgressing)
	assert.NotNil(t, updatedProgressingCondition)
	assert.Equal(t, conditions.NewReplicaSetReason, updatedProgressingCondition.Reason)
	assert.Equal(t, corev1.ConditionTrue, updatedProgressingCondition.Status)
	assert.Equal(t, fmt.Sprintf(conditions.NewReplicaSetMessage, rs.Name), updatedProgressingCondition.Message)

	patch := f.getPatchedRollout(patchRolloutIndex)
	assert.Equal(t, expectedPatch, patch)
}

func TestBlueGreenCreatesReplicaSet(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r.Status.Conditions = []v1alpha1.RolloutCondition{}
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)
	previewSvc := newService("preview", 80, nil, r)
	activeSvc := newService("active", 80, nil, r)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc)
	f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

	rs := newReplicaSet(r, 1)
	rsPodHash := rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	generatedConditions := generateConditionsPatchWithCompleted(false, conditions.ReplicaSetUpdatedReason, rs, false, "", true)

	f.expectCreateReplicaSetAction(rs)
	servicePatchIndex := f.expectPatchServiceAction(previewSvc, rsPodHash)
	f.expectUpdateReplicaSetAction(rs) // scale up RS
	updatedRolloutIndex := f.expectUpdateRolloutStatusAction(r)
	expectedPatchWithoutSubs := `{
		"status":{
			"blueGreen" : {
				"previewSelector": "%s"
			},
			"conditions": %s,
			"selector": "foo=bar",
			"stableRS": "%s",
			"phase": "Progressing",
			"message": "more replicas need to be updated"
		}
	}`
	expectedPatch := calculatePatch(r, fmt.Sprintf(expectedPatchWithoutSubs, rsPodHash, generatedConditions, rsPodHash))
	patchRolloutIndex := f.expectPatchRolloutActionWithPatch(r, expectedPatch)
	f.run(getKey(r, t))

	f.verifyPatchedService(servicePatchIndex, rsPodHash, "")

	updatedRollout := f.getUpdatedRollout(updatedRolloutIndex)
	updatedProgressingCondition := conditions.GetRolloutCondition(updatedRollout.Status, v1alpha1.RolloutProgressing)
	assert.NotNil(t, updatedProgressingCondition)
	assert.Equal(t, conditions.NewReplicaSetReason, updatedProgressingCondition.Reason)
	assert.Equal(t, corev1.ConditionTrue, updatedProgressingCondition.Status)
	assert.Equal(t, fmt.Sprintf(conditions.NewReplicaSetMessage, rs.Name), updatedProgressingCondition.Message)

	patch := f.getPatchedRollout(patchRolloutIndex)
	assert.Equal(t, expectedPatch, patch)
}

// TestBlueGreenSetPreviewService ensures the preview service is set to the desired ReplicaSet
func TestBlueGreenSetPreviewService(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	rs := newReplicaSetWithStatus(r, 1, 1)
	rsPodHash := rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	f.kubeobjects = append(f.kubeobjects, rs)
	f.replicaSetLister = append(f.replicaSetLister, rs)

	previewSvc := newService("preview", 80, nil, r)
	selector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rsPodHash}
	activeSvc := newService("active", 80, selector, r)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc)
	f.serviceLister = append(f.serviceLister, previewSvc, activeSvc)

	servicePatch := f.expectPatchServiceAction(previewSvc, rsPodHash)
	f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))

	f.verifyPatchedService(servicePatch, rsPodHash, "")
}

// TestBlueGreenProgressDeadlineAbort tests aborting an update if it is timeout
func TestBlueGreenProgressDeadlineAbort(t *testing.T) {
	// Two cases to be tested:
	//   1. the rollout is making progress, but timeout just happens
	//   2. the rollout is not making progress due to timeout and the rollout spec
	//      is changed to set ProgressDeadlineAbort
	tests := []bool{true, false}

	var runRolloutProgressDeadlineAbort func(isTimeout bool)
	runRolloutProgressDeadlineAbort = func(isTimeout bool) {
		f := newFixture(t)
		defer f.Close()

		r := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		progressDeadlineSeconds := int32(1)
		r.Spec.ProgressDeadlineSeconds = &progressDeadlineSeconds
		r.Spec.ProgressDeadlineAbort = true

		f.rolloutLister = append(f.rolloutLister, r)
		f.objects = append(f.objects, r)

		rs := newReplicaSetWithStatus(r, 1, 1)
		r.Status.UpdatedReplicas = 1
		r.Status.ReadyReplicas = 1
		r.Status.AvailableReplicas = 1

		rsPodHash := rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		var progressingTimeoutCond *v1alpha1.RolloutCondition
		if isTimeout {
			msg := fmt.Sprintf("ReplicaSet %q has timed out progressing.", "foo-"+rsPodHash)
			progressingTimeoutCond = conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionFalse, conditions.TimedOutReason, msg)
		} else {
			progressingTimeoutCond = conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionTrue, conditions.TimedOutReason, conditions.TimedOutReason)
		}
		conditions.SetRolloutCondition(&r.Status, *progressingTimeoutCond)

		r.Status.BlueGreen.ActiveSelector = rsPodHash
		r.Status.BlueGreen.PreviewSelector = rsPodHash

		previewSvc := newService("preview", 80, nil, r)
		selector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rsPodHash}
		activeSvc := newService("active", 80, selector, r)
		f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc)
		f.serviceLister = append(f.serviceLister, previewSvc, activeSvc)

		f.kubeobjects = append(f.kubeobjects, rs)
		f.replicaSetLister = append(f.replicaSetLister, rs)

		f.expectPatchServiceAction(previewSvc, rsPodHash)
		patchIndex := f.expectPatchRolloutAction(r)
		f.run(getKey(r, t))

		f.verifyPatchedRolloutAborted(patchIndex, "foo-"+rsPodHash)
	}

	for _, tc := range tests {
		runRolloutProgressDeadlineAbort(tc)
	}
}

// TestSetServiceManagedBy ensures the managed by annotation is set in the service is set
func TestSetServiceManagedBy(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	rs := newReplicaSetWithStatus(r, 1, 1)
	rsPodHash := rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	f.kubeobjects = append(f.kubeobjects, rs)
	f.replicaSetLister = append(f.replicaSetLister, rs)

	previewSvc := newService("preview", 80, nil, r)
	selector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rsPodHash}
	activeSvc := newService("active", 80, selector, r)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc)
	f.serviceLister = append(f.serviceLister, previewSvc, activeSvc)

	servicePatch := f.expectPatchServiceAction(previewSvc, rsPodHash)
	f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))

	f.verifyPatchedService(servicePatch, rsPodHash, "")
}

func TestBlueGreenHandlePause(t *testing.T) {
	t.Run("NoPauseIfNewRSIsNotHealthy", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 2, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)

		rs1 := newReplicaSetWithStatus(r1, 2, 2)
		rs2 := newReplicaSetWithStatus(r2, 2, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, rs1PodHash, 2, 2, 4, 2, false, true, false)

		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector, r2)
		previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
		previewSvc := newService("preview", 80, previewSelector, r2)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, activeSvc, previewSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
		f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

		patchRolloutIndex := f.expectPatchRolloutAction(r2)
		f.run(getKey(r2, t))

		patch := f.getPatchedRollout(patchRolloutIndex)
		assert.JSONEq(t, calculatePatch(r2, OnlyObservedGenerationPatch), patch)
	})
	t.Run("AddPause", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)
		rs1 := newReplicaSetWithStatus(r1, 1, 1)
		rs2 := newReplicaSetWithStatus(r2, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, rs1PodHash, 1, 1, 2, 1, false, true, false)
		previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
		previewSvc := newService("preview", 80, previewSelector, r2)
		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector, r2)

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
			"status": {
				"pauseConditions": [{
					"reason": "%s",
					"startTime": "%s"
				}],
				"controllerPause": true,
				"phase": "Paused",
				"message": "BlueGreenPause"
			}
		}`
		now := timeutil.Now().UTC().Format(time.RFC3339)
		assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, v1alpha1.PauseReasonBlueGreenPause, now)), patch)

	})

	t.Run("AddPausedConditionWhilePaused", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)

		rs1 := newReplicaSetWithStatus(r1, 1, 1)
		rs2 := newReplicaSetWithStatus(r2, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, rs1PodHash, 1, 1, 2, 1, true, true, false)

		previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
		previewSvc := newService("preview", 80, previewSelector, r2)
		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector, r2)

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
		addedConditions := generateConditionsPatchWithPause(true, conditions.RolloutPausedReason, rs2, true, "", true, false)
		assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, addedConditions)), patch)
	})

	t.Run("NoActionsAfterPausing", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)

		rs1 := newReplicaSetWithStatus(r1, 1, 1)
		rs2 := newReplicaSetWithStatus(r2, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, rs1PodHash, 1, 1, 2, 1, true, true, false)
		progressingCondition, _ := newProgressingCondition(conditions.RolloutPausedReason, r2, "")
		conditions.SetRolloutCondition(&r2.Status, progressingCondition)

		pausedCondition, _ := newPausedCondition(true)
		conditions.SetRolloutCondition(&r2.Status, pausedCondition)

		completedCondition, _ := newCompletedCondition(false)
		conditions.SetRolloutCondition(&r2.Status, completedCondition)
		r2.Status.Phase, r2.Status.Message = rolloututil.CalculateRolloutPhase(r2.Spec, r2.Status)

		previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
		previewSvc := newService("preview", 80, previewSelector, r2)
		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector, r2)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
		f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

		patchIndex := f.expectPatchRolloutActionWithPatch(r2, OnlyObservedGenerationPatch)
		f.run(getKey(r2, t))
		patch := f.getPatchedRollout(patchIndex)
		assert.JSONEq(t, calculatePatch(r2, OnlyObservedGenerationPatch), patch)
	})

	t.Run("NoActionsAfterPausedOnInconclusiveRun", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)
		r2.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{{
				TemplateName: "test",
			}},
		}

		rs1 := newReplicaSetWithStatus(r1, 1, 1)
		rs2 := newReplicaSetWithStatus(r2, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, rs1PodHash, 1, 1, 2, 1, false, true, false)
		now := timeutil.MetaNow()
		r2.Status.PauseConditions = append(r2.Status.PauseConditions, v1alpha1.PauseCondition{
			Reason:    v1alpha1.PauseReasonInconclusiveAnalysis,
			StartTime: now,
		})
		progressingCondition, _ := newProgressingCondition(conditions.RolloutPausedReason, r2, "")
		conditions.SetRolloutCondition(&r2.Status, progressingCondition)

		pausedCondition, _ := newPausedCondition(true)
		conditions.SetRolloutCondition(&r2.Status, pausedCondition)
		r2.Status.Phase, r2.Status.Message = rolloututil.CalculateRolloutPhase(r2.Spec, r2.Status)

		previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
		previewSvc := newService("preview", 80, previewSelector, r2)
		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector, r2)
		at := analysisTemplate("test")

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
		f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)
		f.analysisTemplateLister = append(f.analysisTemplateLister, at)
		f.objects = append(f.objects, at)

		patchIndex := f.expectPatchRolloutActionWithPatch(r2, OnlyObservedGenerationPatch)
		f.run(getKey(r2, t))
		patch := f.getPatchedRollout(patchIndex)
		assert.JSONEq(t, calculatePatch(r2, OnlyObservedGenerationPatch), patch)
	})

	t.Run("NoAutoPromoteBeforeDelayTimePasses", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r2 := bumpVersion(r1)
		r2.Spec.Strategy.BlueGreen.AutoPromotionSeconds = 10

		rs1 := newReplicaSetWithStatus(r1, 1, 1)
		rs2 := newReplicaSetWithStatus(r2, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, rs1PodHash, 1, 1, 2, 1, true, true, false)
		progressingCondition, _ := newProgressingCondition(conditions.RolloutPausedReason, r2, "")
		conditions.SetRolloutCondition(&r2.Status, progressingCondition)

		pausedCondition, _ := newPausedCondition(true)
		conditions.SetRolloutCondition(&r2.Status, pausedCondition)

		completedCondition, _ := newCompletedCondition(false)
		conditions.SetRolloutCondition(&r2.Status, completedCondition)
		r2.Status.Phase, r2.Status.Message = rolloututil.CalculateRolloutPhase(r2.Spec, r2.Status)

		previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
		previewSvc := newService("preview", 80, previewSelector, r2)
		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector, r2)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
		f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

		patchIndex := f.expectPatchRolloutActionWithPatch(r2, OnlyObservedGenerationPatch)
		f.run(getKey(r2, t))
		patch := f.getPatchedRollout(patchIndex)
		assert.JSONEq(t, calculatePatch(r2, OnlyObservedGenerationPatch), patch)
	})

	t.Run("AutoPromoteAfterDelayTimePasses", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r2 := bumpVersion(r1)
		r2.Spec.Strategy.BlueGreen.AutoPromotionSeconds = 10

		rs1 := newReplicaSetWithStatus(r1, 1, 1)
		rs2 := newReplicaSetWithStatus(r2, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, rs1PodHash, 1, 1, 2, 1, true, true, false)
		now := timeutil.MetaNow()
		before := metav1.NewTime(now.Add(-1 * time.Minute))
		r2.Status.PauseConditions[0].StartTime = before
		r2.Status.ControllerPause = true
		progressingCondition, _ := newProgressingCondition(conditions.RolloutPausedReason, rs2, "")
		conditions.SetRolloutCondition(&r2.Status, progressingCondition)

		pausedCondition, _ := newPausedCondition(true)
		conditions.SetRolloutCondition(&r2.Status, pausedCondition)
		r2.Status.Phase, r2.Status.Message = rolloututil.CalculateRolloutPhase(r2.Spec, r2.Status)

		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector, r2)
		previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
		previewSvc := newService("preview", 80, previewSelector, r2)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, activeSvc, previewSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
		f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

		expectedPatchWithoutSubs := `{
			"status": {
				"blueGreen": {
					"activeSelector": "%s"
				},
				"stableRS": "%s",
				"pauseConditions": null,
				"controllerPause": null,
				"selector": "foo=bar,rollouts-pod-template-hash=%s",
				"phase": "Healthy",
				"message": null
			}
		}`
		expectedPatch := calculatePatch(r2, fmt.Sprintf(expectedPatchWithoutSubs, rs2PodHash, rs2PodHash, rs2PodHash))
		f.expectPatchServiceAction(activeSvc, rs2PodHash)
		patchRolloutIndex := f.expectPatchRolloutActionWithPatch(r2, expectedPatch)
		f.run(getKey(r2, t))

		rolloutPatch := f.getPatchedRolloutWithoutConditions(patchRolloutIndex)
		assert.Equal(t, expectedPatch, rolloutPatch)
	})

	t.Run("NoAutoPromoteAfterDelayTimePassesIfUserPaused", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r2 := bumpVersion(r1)
		r2.Spec.Strategy.BlueGreen.AutoPromotionSeconds = 10

		rs1 := newReplicaSetWithStatus(r1, 1, 1)
		rs2 := newReplicaSetWithStatus(r2, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, rs1PodHash, 1, 1, 2, 1, true, true, false)
		now := timeutil.MetaNow()
		before := metav1.NewTime(now.Add(-1 * time.Minute))
		r2.Status.PauseConditions[0].StartTime = before
		r2.Status.ControllerPause = true
		r2.Spec.Paused = true
		progressingCondition, _ := newProgressingCondition(conditions.RolloutPausedReason, rs2, "")
		conditions.SetRolloutCondition(&r2.Status, progressingCondition)

		pausedCondition, _ := newPausedCondition(true)
		conditions.SetRolloutCondition(&r2.Status, pausedCondition)

		completedCondition, _ := newCompletedCondition(false)
		conditions.SetRolloutCondition(&r2.Status, completedCondition)
		r2.Status.Phase, r2.Status.Message = rolloututil.CalculateRolloutPhase(r2.Spec, r2.Status)

		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector, r2)
		previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
		previewSvc := newService("preview", 80, previewSelector, r2)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, activeSvc, previewSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
		f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

		expectedPatch := calculatePatch(r2, OnlyObservedGenerationPatch)
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

		r2 = updateBlueGreenRolloutStatus(r2, "", rs1PodHash, rs1PodHash, 1, 1, 2, 1, false, true, false)
		r2.Spec.Strategy.BlueGreen.ScaleDownDelaySeconds = pointer.Int32Ptr(10)

		progressingCondition, _ := newProgressingCondition(conditions.NewReplicaSetReason, rs2, "")
		conditions.SetRolloutCondition(&r2.Status, progressingCondition)
		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector, r2)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, activeSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
		f.serviceLister = append(f.serviceLister, activeSvc)

		servicePatchIndex := f.expectPatchServiceAction(activeSvc, rs2PodHash)

		generatedConditions := generateConditionsPatchWithCompleted(true, conditions.ReplicaSetUpdatedReason, rs2, true, "", true)
		newSelector := metav1.FormatLabelSelector(rs2.Spec.Selector)
		expectedPatchWithoutSubs := `{
			"status": {
				"blueGreen": {
					"activeSelector": "%s"
				},
				"stableRS": "%s",
				"conditions": %s,
				"selector": "%s",
				"phase": "Healthy",
				"message": null
			}
		}`
		expectedPatch := calculatePatch(r2, fmt.Sprintf(expectedPatchWithoutSubs, rs2PodHash, rs2PodHash, generatedConditions, newSelector))
		patchIndex := f.expectPatchRolloutActionWithPatch(r2, expectedPatch)
		f.run(getKey(r2, t))
		f.verifyPatchedService(servicePatchIndex, rs2PodHash, "")

		rolloutPatch := f.getPatchedRollout(patchIndex)
		assert.Equal(t, expectedPatch, rolloutPatch)
	})

	t.Run("PauseWhenAutoPromotionEnabledIsFalse", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
		r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)

		rs1 := newReplicaSetWithStatus(r1, 1, 1)
		rs2 := newReplicaSetWithStatus(r2, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, "", rs1PodHash, rs1PodHash, 1, 1, 2, 1, false, true, false)

		progressingCondition, _ := newProgressingCondition(conditions.NewReplicaSetReason, rs2, "")
		conditions.SetRolloutCondition(&r2.Status, progressingCondition)

		completedCondition, _ := newCompletedCondition(false)
		conditions.SetRolloutCondition(&r2.Status, completedCondition)
		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector, r2)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, activeSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
		f.serviceLister = append(f.serviceLister, activeSvc)

		now := timeutil.MetaNow().UTC().Format(time.RFC3339)
		expectedPatchWithoutSubs := `{
			"status": {
				"pauseConditions": [{
					"reason":"%s",
					"startTime": "%s"
				}],
				"controllerPause": true,
				"phase": "Paused",
				"message": "BlueGreenPause"
			}
		}`
		expectedPatch := calculatePatch(r2, fmt.Sprintf(expectedPatchWithoutSubs, v1alpha1.PauseReasonBlueGreenPause, now))
		patchIndex := f.expectPatchRolloutActionWithPatch(r2, expectedPatch)
		f.run(getKey(r2, t))

		rolloutPatch := f.getPatchedRollout(patchIndex)
		assert.Equal(t, expectedPatch, rolloutPatch)
	})

	t.Run("SkipPauseReconcileWhenActiveHasPodHashSelector", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
		r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = pointer.BoolPtr(false)

		rs1 := newReplicaSetWithStatus(r1, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r1 = updateBlueGreenRolloutStatus(r1, "", "", "", 1, 1, 1, 1, false, false, false)

		activeSelector := map[string]string{"foo": "bar"}

		activeSvc := newService("active", 80, activeSelector, r1)

		f.objects = append(f.objects, r1)
		f.kubeobjects = append(f.kubeobjects, activeSvc, rs1)
		f.rolloutLister = append(f.rolloutLister, r1)
		f.replicaSetLister = append(f.replicaSetLister, rs1)
		f.serviceLister = append(f.serviceLister, activeSvc)

		servicePatchIndex := f.expectPatchServiceAction(activeSvc, rs1PodHash)
		expectedPatchWithoutSubs := `{
			"status": {
				"blueGreen": {
					"activeSelector": "%s"
				},
				"stableRS": "%s",
				"conditions": %s,
				"selector": "%s",
				"phase": "Healthy",
				"message": null
			}
		}`

		generateConditions := generateConditionsPatchWithCompleted(true, conditions.ReplicaSetUpdatedReason, rs1, false, "", true)
		newSelector := metav1.FormatLabelSelector(rs1.Spec.Selector)
		expectedPatch := calculatePatch(r1, fmt.Sprintf(expectedPatchWithoutSubs, rs1PodHash, rs1PodHash, generateConditions, newSelector))
		patchRolloutIndex := f.expectPatchRolloutActionWithPatch(r1, expectedPatch)
		f.run(getKey(r1, t))

		f.verifyPatchedService(servicePatchIndex, rs1PodHash, "")

		rolloutPatch := f.getPatchedRollout(patchRolloutIndex)
		assert.Equal(t, expectedPatch, rolloutPatch)
	})

	t.Run("ContinueAfterUnpaused", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = pointer.BoolPtr(false)
		r2 := bumpVersion(r1)

		rs1 := newReplicaSetWithStatus(r1, 1, 1)
		rs2 := newReplicaSetWithStatus(r2, 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2.Spec.Strategy.BlueGreen.ScaleDownDelaySeconds = pointer.Int32Ptr(10)
		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, rs1PodHash, 1, 1, 2, 1, false, true, false)
		r2.Status.ControllerPause = true
		completedCondition, _ := newCompletedCondition(false)
		conditions.SetRolloutCondition(&r2.Status, completedCondition)
		pausedCondition, _ := newProgressingCondition(conditions.RolloutPausedReason, rs2, "")
		conditions.SetRolloutCondition(&r2.Status, pausedCondition)

		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector, r2)
		previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
		previewSvc := newService("preview", 80, previewSelector, r2)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, activeSvc, previewSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
		f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

		servicePatchIndex := f.expectPatchServiceAction(activeSvc, rs2PodHash)
		unpausePatchIndex := f.expectPatchRolloutAction(r2)
		patchRolloutIndex := f.expectPatchRolloutAction(r2)
		f.run(getKey(r2, t))

		f.verifyPatchedService(servicePatchIndex, rs2PodHash, "")
		unpausePatch := f.getPatchedRollout(unpausePatchIndex)
		_, availableCondition := newAvailableCondition(true)
		_, progressingCondition := newProgressingCondition(conditions.RolloutResumedReason, rs2, "")
		_, compCondition := newCompletedCondition(false)
		unpauseConditions := fmt.Sprintf("[%s, %s, %s]", availableCondition, compCondition, progressingCondition)
		expectedUnpausePatch := `{
			"status": {
				"conditions": %s
			}
		}`
		assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedUnpausePatch, unpauseConditions)), unpausePatch)

		generatedConditions := generateConditionsPatchWithCompleted(true, conditions.ReplicaSetUpdatedReason, rs2, true, "", true)
		expected2ndPatchWithoutSubs := `{
			"status": {
				"blueGreen": {
					"activeSelector": "%s"
				},
				"stableRS": "%s",
				"controllerPause":null,
				"conditions": %s,
				"selector": "%s",
				"phase": "Healthy",
				"message": null
			}
		}`
		newSelector := metav1.FormatLabelSelector(rs2.Spec.Selector)
		expected2ndPatch := calculatePatch(r2, fmt.Sprintf(expected2ndPatchWithoutSubs, rs2PodHash, rs2PodHash, generatedConditions, newSelector))
		rollout2ndPatch := f.getPatchedRollout(patchRolloutIndex)
		assert.Equal(t, expected2ndPatch, rollout2ndPatch)
	})
}

func TestBlueGreenAddScaleDownDelayToPreviousActiveReplicaSet(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "bar", "")
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	s := newService("bar", 80, serviceSelector, r2)
	f.kubeobjects = append(f.kubeobjects, s, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2.Spec.Strategy.BlueGreen.ScaleDownDelaySeconds = pointer.Int32Ptr(10)
	r2 = updateBlueGreenRolloutStatus(r2, "", rs1PodHash, rs1PodHash, 1, 1, 2, 1, false, true, false)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.serviceLister = append(f.serviceLister, s)

	f.expectPatchServiceAction(s, rs2PodHash)
	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(patchIndex)
	expectedPatchWithoutSubs := `{
		"status":{
			"blueGreen": {
				"activeSelector": "%s"
			},
			"stableRS": "%s",
			"conditions": %s,
			"selector": "%s",
			"phase": "Healthy",
			"message": null
		}
	}`
	newSelector := metav1.FormatLabelSelector(rs2.Spec.Selector)
	expectedCondition := generateConditionsPatchWithCompleted(true, conditions.ReplicaSetUpdatedReason, rs2, true, "", true)
	expectedPatch := calculatePatch(r2, fmt.Sprintf(expectedPatchWithoutSubs, rs2PodHash, rs2PodHash, expectedCondition, newSelector))
	assert.Equal(t, expectedPatch, patch)
}

func TestBlueGreenRolloutStatusHPAStatusFieldsActiveSelectorSet(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r.Spec.Strategy.BlueGreen.AutoPromotionEnabled = pointer.BoolPtr(false)
	r2 := bumpVersion(r)

	rs1 := newReplicaSetWithStatus(r, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	previewSvc := newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}, r2)
	activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}, r2)

	r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, rs1PodHash, 0, 0, 0, 0, true, false, false)
	r2.Status.Selector = ""
	progressingCondition, _ := newProgressingCondition(conditions.RolloutPausedReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	pausedCondition, _ := newPausedCondition(true)
	conditions.SetRolloutCondition(&r2.Status, pausedCondition)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, previewSvc, activeSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

	expectedPatchWithoutSubs := `{
		"status":{
			"HPAReplicas":1,
			"readyReplicas":1,
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

	rolloutPatch := f.getPatchedRolloutWithoutConditions(patchIndex)
	assert.Equal(t, expectedPatch, rolloutPatch)
}

func TestBlueGreenRolloutStatusHPAStatusFieldsNoActiveSelector(t *testing.T) {
	ro := newBlueGreenRollout("foo", 2, nil, "active", "")
	rs := newReplicaSetWithStatus(ro, 1, 1)
	ro.Status.CurrentPodHash = rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	ro.Status.StableRS = rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: ""}, ro)

	progressingCondition, progressingConditionStr := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs, "")
	conditions.SetRolloutCondition(&ro.Status, progressingCondition)
	ro.Status.Phase, ro.Status.Message = rolloututil.CalculateRolloutPhase(ro.Spec, ro.Status)

	f := newFixture(t)
	defer f.Close()
	f.objects = append(f.objects, ro)
	f.rolloutLister = append(f.rolloutLister, ro)
	f.replicaSetLister = append(f.replicaSetLister, rs)

	ctrl, _, _ := f.newController(noResyncPeriodFunc)
	roCtx, err := ctrl.newRolloutContext(ro)
	assert.NoError(t, err)

	err = roCtx.syncRolloutStatusBlueGreen(nil, activeSvc)
	assert.Nil(t, err)
	assert.Len(t, f.client.Actions(), 1)
	result := f.client.Actions()[0].(core.PatchAction).GetPatch()
	_, availableStr := newAvailableCondition(false)
	_, compCond := newCompletedCondition(true)
	expectedPatchWithoutSub := `{
		"status":{
			"HPAReplicas":1,
			"readyReplicas": 1,
			"availableReplicas": 1,
			"updatedReplicas":1,
			"replicas":1,
			"conditions":[%s, %s, %s],
			"selector":"foo=bar"
		}
	}`
	expectedPatch := calculatePatch(ro, fmt.Sprintf(expectedPatchWithoutSub, progressingConditionStr, availableStr, compCond))
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

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, rs1PodHash, 1, 1, 1, 1, false, true, false)
	f.objects = append(f.objects, r2)
	previewSvc := newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}, r2)
	activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}, r2)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc)
	f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

	f.expectUpdateReplicaSetAction(rs1)
	f.expectUpdateReplicaSetAction(rs2)
	f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
}

func TestPreviewReplicaCountHandleScaleUpPreviewCheckPoint(t *testing.T) {
	t.Run("TrueAfterMeetingMinAvailable", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 5, nil, "active", "")
		r1.Spec.Strategy.BlueGreen.PreviewReplicaCount = pointer.Int32Ptr(3)
		rs1 := newReplicaSetWithStatus(r1, 5, 5)
		r2 := bumpVersion(r1)

		rs2 := newReplicaSetWithStatus(r2, 3, 3)
		f.kubeobjects = append(f.kubeobjects, rs1, rs2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}, r2)

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, rs1PodHash, 3, 3, 8, 5, false, true, false)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, activeSvc)
		f.serviceLister = append(f.serviceLister, activeSvc)

		patchIndex := f.expectPatchRolloutAction(r1)
		f.run(getKey(r2, t))
		patch := f.getPatchedRollout(patchIndex)

		assert.Contains(t, patch, `"scaleUpPreviewCheckPoint":true`)

	})
	t.Run("FalseAfterActiveServiceSwitch", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 5, nil, "active", "")
		r1.Spec.Strategy.BlueGreen.PreviewReplicaCount = pointer.Int32Ptr(3)
		r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = pointer.BoolPtr(false)
		rs1 := newReplicaSetWithStatus(r1, 5, 5)
		r2 := bumpVersion(r1)

		rs2 := newReplicaSetWithStatus(r2, 5, 5)
		f.kubeobjects = append(f.kubeobjects, rs1, rs2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}, r2)

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, rs1PodHash, 5, 5, 8, 5, false, true, false)
		r2.Status.BlueGreen.ScaleUpPreviewCheckPoint = true
		f.rolloutLister = append(f.rolloutLister, r2)
		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, activeSvc)
		f.serviceLister = append(f.serviceLister, activeSvc)

		patchIndex := f.expectPatchRolloutAction(r1)

		f.run(getKey(r2, t))
		patch := f.getPatchedRollout(patchIndex)
		assert.Contains(t, patch, `"scaleUpPreviewCheckPoint":null`)
	})
	t.Run("TrueWhenScalingUpPreview", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		r1 := newBlueGreenRollout("foo", 5, nil, "active", "")
		r1.Spec.Strategy.BlueGreen.PreviewReplicaCount = pointer.Int32Ptr(3)
		rs1 := newReplicaSetWithStatus(r1, 5, 5)
		r2 := bumpVersion(r1)

		rs2 := newReplicaSetWithStatus(r2, 3, 3)
		f.kubeobjects = append(f.kubeobjects, rs1, rs2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}, r2)

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, rs1PodHash, 5, 5, 8, 5, false, true, false)
		r2.Status.BlueGreen.ScaleUpPreviewCheckPoint = true
		f.rolloutLister = append(f.rolloutLister, r2)
		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, activeSvc)
		f.serviceLister = append(f.serviceLister, activeSvc)

		f.expectUpdateReplicaSetAction(rs1)
		f.expectPatchRolloutAction(r2)
		f.run(getKey(r2, t))
	})
}

func TestBlueGreenRolloutIgnoringScalingUsePreviewRSCount(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r1.Spec.Strategy.BlueGreen.PreviewReplicaCount = pointer.Int32Ptr(3)
	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs1.Spec.Replicas = pointer.Int32Ptr(2)
	rs1.Annotations[annotations.DesiredReplicasAnnotation] = "2"
	r2 := bumpVersion(r1)

	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	previewSvc := newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}, r2)
	activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}, r2)

	r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, rs1PodHash, 2, 1, 1, 1, false, true, true)
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
	assert.Equal(t, "2", rs2Updated.Annotations[annotations.DesiredReplicasAnnotation])
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
	s := newService("bar", 80, serviceSelector, r2)
	f.kubeobjects = append(f.kubeobjects, s)

	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, rs2PodHash, 1, 1, 1, 1, false, true, true)
	r2.Status.ObservedGeneration = strconv.Itoa(int(r2.Generation))

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.serviceLister = append(f.serviceLister, s)

	patchIndex := f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))

	newConditions := generateConditionsPatchWithHealthy(true, conditions.NewRSAvailableReason, rs2, true, "", true, true)
	expectedPatch := fmt.Sprintf(`{
		"status":{
			"conditions":%s
		}
	}`, newConditions)
	patch := f.getPatchedRollout(patchIndex)
	assert.Equal(t, cleanPatch(expectedPatch), patch)
}

func TestBlueGreenRolloutCompletedFalse(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "bar", "")
	completedCondition, _ := newHealthyCondition(true)
	conditions.SetRolloutCondition(&r1.Status, completedCondition)

	r2 := bumpVersion(r1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	progressingCondition, _ := newProgressingCondition(conditions.RolloutPausedReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)
	pausedCondition, _ := newPausedCondition(true)
	conditions.SetRolloutCondition(&r2.Status, pausedCondition)
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)

	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	s := newService("bar", 80, serviceSelector, r2)
	f.kubeobjects = append(f.kubeobjects, s)

	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, rs2PodHash, 1, 1, 1, 1, true, false, false)
	r2.Status.ObservedGeneration = strconv.Itoa(int(r2.Generation))

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.serviceLister = append(f.serviceLister, s)

	patchIndex := f.expectPatchRolloutAction(r1)
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(patchIndex)
	rolloutPatch := v1alpha1.Rollout{}
	err := json.Unmarshal([]byte(patch), &rolloutPatch)
	assert.NoError(t, err)

	index := len(rolloutPatch.Status.Conditions) - 3
	assert.Equal(t, v1alpha1.RolloutHealthy, rolloutPatch.Status.Conditions[index].Type)
	assert.Equal(t, corev1.ConditionFalse, rolloutPatch.Status.Conditions[index].Status)
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
	s := newService("bar", 80, serviceSelector, r2)
	f.kubeobjects = append(f.kubeobjects, s, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, rs2PodHash, 1, 1, 2, 1, false, true, true)
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

	inTheFuture := timeutil.Now().Add(10 * time.Second).UTC().Format(time.RFC3339)

	rs1.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inTheFuture

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	s := newService("bar", 80, serviceSelector, r2)
	f.kubeobjects = append(f.kubeobjects, s, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, rs2PodHash, 1, 1, 2, 1, false, true, true)
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

	inThePast := timeutil.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339)

	rs1.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inThePast

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	s := newService("bar", 80, serviceSelector, r2)
	f.kubeobjects = append(f.kubeobjects, s, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, rs2PodHash, 1, 1, 2, 1, false, true, true)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.serviceLister = append(f.serviceLister, s)

	updatedRSIndex := f.expectUpdateReplicaSetAction(rs2)
	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	updatedRS := f.getUpdatedReplicaSet(updatedRSIndex)
	assert.Equal(t, int32(0), *updatedRS.Spec.Replicas)
	assert.Equal(t, "", updatedRS.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey])

	patch := f.getPatchedRollout(patchIndex)
	//TODO: fix
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
	inTheFuture := timeutil.Now().Add(10 * time.Second).UTC().Format(time.RFC3339)
	rs1.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inTheFuture
	rs2.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inTheFuture

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	s := newService("bar", 80, serviceSelector, r2)
	f.kubeobjects = append(f.kubeobjects, s, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	// Switch back to version 1
	r2.Spec.Template = r1.Spec.Template
	r2.Annotations[annotations.RevisionAnnotation] = "3"
	r2.Status.CurrentPodHash = rs1PodHash
	rs1.Annotations[annotations.RevisionAnnotation] = "3"

	r2 = updateBlueGreenRolloutStatus(r2, "", rs1PodHash, rs1PodHash, 1, 1, 2, 1, false, true, true)
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

func TestBlueGreenScaleDownLimit(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "bar", "")
	r2 := bumpVersion(r1)
	r3 := bumpVersion(r2)
	r3.Spec.Strategy.BlueGreen.ScaleDownDelayRevisionLimit = pointer.Int32Ptr(1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs3 := newReplicaSetWithStatus(r3, 1, 1)
	rs3PodHash := rs3.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	//Setting the scaleDownAt time
	inTheFuture := timeutil.MetaNow().Add(10 * time.Second).UTC().Format(time.RFC3339)
	rs1.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inTheFuture
	rs2.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inTheFuture

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs3PodHash}
	s := newService("bar", 80, serviceSelector, r2)
	f.kubeobjects = append(f.kubeobjects, s, rs1, rs2, rs3)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2, rs3)

	r3 = updateBlueGreenRolloutStatus(r3, "", rs3PodHash, rs3PodHash, 1, 1, 3, 1, false, true, true)
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

// TestBlueGreenAbort Switches active service back to previous ReplicaSet when Rollout is aborted
func TestBlueGreenAbort(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "bar", "")
	r2 := bumpVersion(r1)
	r2.Status.Abort = true
	now := timeutil.MetaNow()
	r2.Status.AbortedAt = &now

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	s := newService("bar", 80, serviceSelector, r2)
	f.kubeobjects = append(f.kubeobjects, s, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, rs1PodHash, 1, 1, 2, 1, false, true, true)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.serviceLister = append(f.serviceLister, s)

	f.expectPatchServiceAction(s, rs1PodHash)
	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	expectedConditions := generateConditionsPatch(true, conditions.RolloutAbortedReason, r2, true, "", false)
	expectedPatch := fmt.Sprintf(`{
		"status": {
			"blueGreen": {
				"activeSelector": "%s"
			},
			"conditions": %s,
			"selector": "foo=bar,rollouts-pod-template-hash=%s",
			"phase": "Degraded",
			"message": "%s: %s"
		}	
	}`, rs1PodHash, expectedConditions, rs1PodHash, conditions.RolloutAbortedReason, fmt.Sprintf(conditions.RolloutAbortedMessage, 2))
	patch := f.getPatchedRollout(patchIndex)
	assert.JSONEq(t, calculatePatch(r2, expectedPatch), patch)
}

func TestBlueGreenHandlePauseAutoPromoteWithConditions(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.AutoPromotionSeconds = 10

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, rs1PodHash, 1, 1, 2, 1, true, true, true)
	now := timeutil.MetaNow()
	before := metav1.NewTime(now.Add(-1 * time.Minute))
	r2.Status.PauseConditions[0].StartTime = before
	r2.Status.ControllerPause = true
	progressingCondition, _ := newProgressingCondition(conditions.RolloutPausedReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	pausedCondition, _ := newPausedCondition(true)
	conditions.SetRolloutCondition(&r2.Status, pausedCondition)
	r2.Status.Phase, r2.Status.Message = rolloututil.CalculateRolloutPhase(r2.Spec, r2.Status)

	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)
	//completedCondition, _ := newCompletedCondition(true)
	//conditions.SetRolloutCondition(&r2.Status, completedCondition)
	r2.Status.Phase, r2.Status.Message = rolloututil.CalculateRolloutPhase(r2.Spec, r2.Status)

	activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	activeSvc := newService("active", 80, activeSelector, r2)
	previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	previewSvc := newService("preview", 80, previewSelector, r2)

	f.objects = append(f.objects, r2)
	f.kubeobjects = append(f.kubeobjects, activeSvc, previewSvc, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

	expectedPatchWithoutSubs := `{
			"status": {
				"blueGreen": {
					"activeSelector": "%s"
				},
				"conditions": [%s, %s, %s, %s],
				"stableRS": "%s",
				"pauseConditions": null,
				"controllerPause": null,
				"selector": "foo=bar,rollouts-pod-template-hash=%s",
				"phase": "Healthy",
				"message": null
			}
		}`
	availableCondBytes, err := json.Marshal(r2.Status.Conditions[0])
	assert.Nil(t, err)
	updatedProgressingCond, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs2, fmt.Sprintf("ReplicaSet \"%s\" is progressing.", rs2.Name))
	progressingCondBytes, err := json.Marshal(updatedProgressingCond)
	assert.Nil(t, err)
	pausedCondBytes, err := json.Marshal(r2.Status.Conditions[3])
	assert.Nil(t, err)
	completeCond, _ := newCompletedCondition(true)
	completeCondBytes, err := json.Marshal(completeCond)
	assert.Nil(t, err)
	expectedPatch := calculatePatch(r2, fmt.Sprintf(expectedPatchWithoutSubs, rs2PodHash, string(availableCondBytes), string(completeCondBytes), string(pausedCondBytes), string(progressingCondBytes), rs2PodHash, rs2PodHash))
	f.expectPatchServiceAction(activeSvc, rs2PodHash)
	patchRolloutIndex := f.expectPatchRolloutActionWithPatch(r2, expectedPatch)
	f.run(getKey(r2, t))

	rolloutPatch := f.getPatchedRollout(patchRolloutIndex)
	assert.Equal(t, expectedPatch, rolloutPatch)
}

// Verifies with blue-green, we add a scaledown delay to the old ReplicaSet after promoting desired
// ReplicaSet to stable.
// NOTE: As of v1.1, scale down delays are added to  ReplicaSets on *subsequent* reconciliations
// after the desired RS has been promoted to stable
func TestBlueGreenAddScaleDownDelay(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	r2.Status.ObservedGeneration = strconv.Itoa(int(r2.Generation))
	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, rs2PodHash, 1, 1, 2, 1, false, true, true)
	completedCondition, _ := newHealthyCondition(true)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)
	progressingCondition, _ := newProgressingCondition(conditions.NewRSAvailableReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	activeSvc := newService("active", 80, activeSelector, r2)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, activeSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1Patch := f.expectPatchReplicaSetAction(rs1) // set scale-down-deadline annotation
	f.run(getKey(r2, t))

	f.verifyPatchedReplicaSet(rs1Patch, 30)
}
