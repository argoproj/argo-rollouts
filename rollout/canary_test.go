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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
)

func newCanaryRollout(name string, replicas int, revisionHistoryLimit *int32, steps []v1alpha1.CanaryStep, stepIndex *int32, maxSurge, maxUnavailable intstr.IntOrString) *v1alpha1.Rollout {
	selector := map[string]string{"foo": "bar"}
	rollout := newRollout(name, replicas, revisionHistoryLimit, selector)
	rollout.Spec.Strategy.CanaryStrategy = &v1alpha1.CanaryStrategy{
		MaxUnavailable: &maxUnavailable,
		MaxSurge:       &maxSurge,
		Steps:          steps,
	}
	rollout.Status.CurrentStepIndex = stepIndex
	rollout.Status.CurrentStepHash = conditions.ComputeStepHash(rollout)
	rollout.Status.CurrentPodHash = controller.ComputeHash(&rollout.Spec.Template, rollout.Status.CollisionCount)
	rollout.Status.Selector = metav1.FormatLabelSelector(rollout.Spec.Selector)
	return rollout
}

func bumpVersion(rollout *v1alpha1.Rollout) *v1alpha1.Rollout {
	newRollout := rollout.DeepCopy()
	revision := rollout.Annotations[annotations.RevisionAnnotation]
	newRevision, _ := strconv.Atoi(revision)
	newRevision++
	newRevisionStr := strconv.FormatInt(int64(newRevision), 10)
	annotations.SetRolloutRevision(newRollout, newRevisionStr)
	newRollout.Spec.Template.Spec.Containers[0].Image = "foo/bar" + newRevisionStr
	newRollout.Status.CurrentPodHash = controller.ComputeHash(&newRollout.Spec.Template, newRollout.Status.CollisionCount)
	newRollout.Status.CurrentStepHash = conditions.ComputeStepHash(newRollout)
	return newRollout
}

func TestReconcileCanaryStepsHandleBaseCases(t *testing.T) {
	fake := fake.Clientset{}
	k8sfake := k8sfake.Clientset{}
	controller := &RolloutController{
		argoprojclientset: &fake,
		kubeclientset:     &k8sfake,
		recorder:          &record.FakeRecorder{},
	}

	// Handle case with no steps
	r := newCanaryRollout("test", 1, nil, nil, nil, intstr.FromInt(0), intstr.FromInt(1))
	stepResult := controller.reconcileCanaryPause(r)
	assert.False(t, stepResult)
	assert.Len(t, fake.Actions(), 0)

	r2 := newCanaryRollout("test", 1, nil, []v1alpha1.CanaryStep{{SetWeight: int32Ptr(10)}}, nil, intstr.FromInt(0), intstr.FromInt(1))
	r2.Status.CurrentStepIndex = int32Ptr(1)
	stepResult = controller.reconcileCanaryPause(r2)
	assert.False(t, stepResult)
	assert.Len(t, fake.Actions(), 0)

}

func TestCanaryRolloutEnterPauseState(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, false)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(patchIndex)
	expectedPatchTemplate := `{
		"spec":{
			"paused": true
		},
		"status":{
			"pauseStartTime":"%s",
			"conditions": %s
		}
	}`

	conditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, r2, false)
	expectedPatchWithoutObservedGen := fmt.Sprintf(expectedPatchTemplate, metav1.Now().UTC().Format(time.RFC3339), conditions)
	expectedPatch := calculatePatch(r2, expectedPatchWithoutObservedGen)
	assert.Equal(t, expectedPatch, patch)
}

func TestCanaryRolloutNoProgressWhilePaused(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)

	progressingCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, r2)
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, true)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
}

func TestCanaryRolloutUpdatePauseConditionWhilePaused(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)

	progressingCondition, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, r2)
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, true)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	addPausedConditionPatch := f.expectPatchRolloutAction(r2)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(addPausedConditionPatch)
	_, pausedCondition := newProgressingCondition(conditions.PausedRolloutReason, rs2)
	expectedPatch := fmt.Sprintf(`{
		"status": {
			"conditions": [%s]
		}
	}`, pausedCondition)
	assert.Equal(t, calculatePatch(r2, expectedPatch), patch)
}

func TestCanaryRolloutIncrementStepAfterUnPaused(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	r2 := bumpVersion(r1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, false)
	r2.Status.AvailableReplicas = 10
	now := metav1.Now()
	r2.Status.PauseStartTime = &now

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	expectedPatchTemplate := `{
	"status":{
		"pauseStartTime": null,
		"conditions" : %s,
		"currentStepIndex": 1
	}
}`
	generatedConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs2, false)
	expectedPatch := calculatePatch(r2, fmt.Sprintf(expectedPatchTemplate, generatedConditions))
	assert.Equal(t, expectedPatch, patch)
}

func TestCanaryRolloutUpdateStatusWhenAtEndOfSteps(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)

	expectedStableRS := r2.Status.CurrentPodHash
	rs1 := newReplicaSetWithStatus(r1, 0, 0)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2 := newReplicaSetWithStatus(r2, 10, 10)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 10, 10, false)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(patchIndex)
	expectedPatchWithoutStableRS := `{
		"status": {
			"canary": {
				"stableRS": "%s"
			},
			"conditions": %s
		}
	}`

	expectedPatch := fmt.Sprintf(expectedPatchWithoutStableRS, expectedStableRS, generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs2, false))
	assert.Equal(t, calculatePatch(r2, expectedPatch), patch)
}

func TestResetCurrentStepIndexOnStepChange(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	steps := []v1alpha1.CanaryStep{
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}

	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(1), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	expectedCurrentPodHash := r2.Status.CurrentPodHash
	r2.Spec.Strategy.CanaryStrategy.Steps = append(steps, v1alpha1.CanaryStep{Pause: &v1alpha1.RolloutPause{}})
	expectedCurrentStepHash := conditions.ComputeStepHash(r2)

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, false)
	r2.Status.CurrentPodHash = rs1PodHash

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(patchIndex)
	expectedPatchWithoutPodHash := `{
		"status": {
			"currentStepIndex":0,
			"currentPodHash": "%s",
			"currentStepHash": "%s",
			"conditions": %s
		}
	}`
	newConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, r2, false)
	expectedPatch := fmt.Sprintf(expectedPatchWithoutPodHash, expectedCurrentPodHash, expectedCurrentStepHash, newConditions)
	assert.Equal(t, calculatePatch(r2, expectedPatch), patch)

}

func TestResetCurrentStepIndexOnPodSpecChange(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	steps := []v1alpha1.CanaryStep{
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}

	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(1), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	expectedCurrentPodHash := r2.Status.CurrentPodHash
	r2.Status.CurrentPodHash = rs1PodHash
	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, false)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(patchIndex)
	expectedPatchWithoutPodHash := `{
		"status": {
			"currentStepIndex":0,
			"currentPodHash": "%s",
			"conditions": %s
		}
	}`
	newConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, r2, false)

	expectedPatch := fmt.Sprintf(expectedPatchWithoutPodHash, expectedCurrentPodHash, newConditions)
	assert.Equal(t, calculatePatch(r2, expectedPatch), patch)

}

func TestCanaryRolloutCreateFirstReplicasetNoSteps(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r := newCanaryRollout("foo", 10, nil, nil, nil, intstr.FromInt(1), intstr.FromInt(0))
	r.Status.CurrentPodHash = ""
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	rs := newReplicaSet(r, 1)

	f.expectCreateReplicaSetAction(rs)
	updatedRolloutIndex := f.expectUpdateRolloutAction(r)
	patchIndex := f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))

	updatedRollout := f.getUpdatedRollout(updatedRolloutIndex)
	progessingCondition := conditions.GetRolloutCondition(updatedRollout.Status, v1alpha1.RolloutProgressing)
	assert.NotNil(t, progessingCondition)
	assert.Equal(t, conditions.NewReplicaSetReason, progessingCondition.Reason)
	assert.Equal(t, corev1.ConditionTrue, progessingCondition.Status)
	assert.Equal(t, fmt.Sprintf(conditions.NewReplicaSetMessage, rs.Name), progessingCondition.Message)

	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{
		"status":{
			"canary":{
				"stableRS":"` + rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] + `"
			},
			"currentPodHash":"` + rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] + `",
			"conditions": %s
		}
	}`

	newConditions := generateConditionsPatch(false, conditions.ReplicaSetUpdatedReason, rs, false)

	assert.Equal(t, calculatePatch(r, fmt.Sprintf(expectedPatch, newConditions)), patch)
}

func TestCanaryRolloutCreateFirstReplicasetWithSteps(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	r := newCanaryRollout("foo", 10, nil, steps, nil, intstr.FromInt(1), intstr.FromInt(0))
	r.Status.CurrentPodHash = ""
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	rs := newReplicaSet(r, 1)

	f.expectCreateReplicaSetAction(rs)
	updatedRolloutIndex := f.expectUpdateRolloutAction(r)
	patchIndex := f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))

	updatedRollout := f.getUpdatedRollout(updatedRolloutIndex)
	progessingCondition := conditions.GetRolloutCondition(updatedRollout.Status, v1alpha1.RolloutProgressing)
	assert.NotNil(t, progessingCondition)
	assert.Equal(t, conditions.NewReplicaSetReason, progessingCondition.Reason)
	assert.Equal(t, corev1.ConditionTrue, progessingCondition.Status)
	assert.Equal(t, fmt.Sprintf(conditions.NewReplicaSetMessage, rs.Name), progessingCondition.Message)

	patch := f.getPatchedRollout(patchIndex)
	expectedPatchWithSub := `{
		"status":{
			"canary":{
				"stableRS":"` + rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] + `"
			},
			"currentStepIndex":1,
			"currentPodHash":"` + rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] + `",
			"conditions": %s
		}
	}`
	expectedPatch := fmt.Sprintf(expectedPatchWithSub, generateConditionsPatch(false, conditions.ReplicaSetUpdatedReason, rs, false))

	assert.Equal(t, calculatePatch(r, expectedPatch), patch)
}

func TestCanaryRolloutCreateNewReplicaWithCorrectWeight(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r1.Status.Canary.StableRS = "895c6c4f9"
	r2 := bumpVersion(r1)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 1, 0)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	createdRSIndex := f.expectCreateReplicaSetAction(rs2)
	updatedRolloutIndex := f.expectUpdateRolloutAction(r2)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	createdRS := f.getCreatedReplicaSet(createdRSIndex)
	assert.Equal(t, int32(1), *createdRS.Spec.Replicas)

	updatedRollout := f.getUpdatedRollout(updatedRolloutIndex)
	progessingCondition := conditions.GetRolloutCondition(updatedRollout.Status, v1alpha1.RolloutProgressing)
	assert.NotNil(t, progessingCondition)
	assert.Equal(t, conditions.NewReplicaSetReason, progessingCondition.Reason)
	assert.Equal(t, corev1.ConditionTrue, progessingCondition.Status)
	assert.Equal(t, fmt.Sprintf(conditions.NewReplicaSetMessage, createdRS.Name), progessingCondition.Message)
}

func TestCanaryRolloutScaleUpNewReplicaWithCorrectWeight(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(40),
	}}
	r1 := newCanaryRollout("foo", 5, nil, steps, int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r1.Status.Canary.StableRS = "895c6c4f9"
	r2 := bumpVersion(r1)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1 := newReplicaSetWithStatus(r1, 3, 3)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)
	updatedRSIndex := f.expectUpdateReplicaSetAction(rs2)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	updatedRS := f.getUpdatedReplicaSet(updatedRSIndex)
	assert.Equal(t, int32(2), *updatedRS.Spec.Replicas)
}

func TestCanaryRolloutScaleDownStableToMatchWeight(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r1.Status.Canary.StableRS = r1.Status.CurrentPodHash

	r2 := bumpVersion(r1)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)
	updatedRSIndex := f.expectUpdateReplicaSetAction(rs1)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	expectedRS1 := rs1.DeepCopy()
	expectedRS1.Spec.Replicas = int32Ptr(9)
	updatedRS := f.getUpdatedReplicaSet(updatedRSIndex)
	assert.Equal(t, expectedRS1, updatedRS)
}

func TestCanaryRolloutScaleDownOldRs(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r1.Status.Canary.StableRS = r1.Status.CurrentPodHash

	r2 := bumpVersion(r1)

	r3 := bumpVersion(r2)
	f.rolloutLister = append(f.rolloutLister, r3)
	f.objects = append(f.objects, r3)

	rs1 := newReplicaSetWithStatus(r1, 9, 9)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)

	rs3 := newReplicaSetWithStatus(r3, 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs3)
	f.replicaSetLister = append(f.replicaSetLister, rs3)

	updateRSIndex := f.expectUpdateReplicaSetAction(rs2)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	expectedRS2 := rs2.DeepCopy()
	expectedRS2.Spec.Replicas = int32Ptr(0)
	expectedRS2.Annotations[annotations.DesiredReplicasAnnotation] = "10"
	updatedRS := f.getUpdatedReplicaSet(updateRSIndex)

	assert.Equal(t, expectedRS2, updatedRS)
}

func TestRollBackToStable(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	r2.Spec.Template = r1.Spec.Template

	rs1 := newReplicaSetWithStatus(r1, 9, 9)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 9, 10, false)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	updatedRSIndex := f.expectUpdateReplicaSetAction(rs1)
	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	expectedRS1 := rs1.DeepCopy()
	expectedRS1.Annotations[annotations.RevisionAnnotation] = "3"
	expectedRS1.Annotations[annotations.RevisionHistoryAnnotation] = "1"
	firstUpdatedRS1 := f.getUpdatedReplicaSet(updatedRSIndex)
	assert.Equal(t, expectedRS1, firstUpdatedRS1)

	expectedPatchWithoutSub := `{
		"status":{
			"currentPodHash": "%s",
			"currentStepIndex":1,
			"conditions": %s
		}
	}`
	newConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs1, false)
	expectedPatch := fmt.Sprintf(expectedPatchWithoutSub, controller.ComputeHash(&r2.Spec.Template, r2.Status.CollisionCount), newConditions)
	patch := f.getPatchedRollout(patchIndex)
	assert.Equal(t, calculatePatch(r2, expectedPatch), patch)
}

func TestGradualShiftToNewStable(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: pointer.Int32Ptr(10),
	}}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(3), intstr.FromInt(0))

	r2 := bumpVersion(r1)
	rs2 := newReplicaSetWithStatus(r2, 4, 4)

	rs1 := newReplicaSetWithStatus(r1, 9, 9)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 13, 4, 13, false)
	maxSurge := intstr.FromInt(3)
	r2.Spec.Strategy.CanaryStrategy.MaxSurge = &maxSurge
	r2.Status.CurrentPodHash = rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	updatedR2SIndex := f.expectUpdateReplicaSetAction(rs1)
	patchIndex := f.expectPatchRolloutAction(r1)
	f.run(getKey(r2, t))

	updatedRS2 := f.getUpdatedReplicaSet(updatedR2SIndex)
	assert.Equal(t, rs1.Name, updatedRS2.Name)
	assert.Equal(t, int32(6), *updatedRS2.Spec.Replicas)

	expectedPatchWithoutSub := `{
		"status":{
			"conditions": %s
		}
	}`
	newConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, r2, false)
	expectedPatch := fmt.Sprintf(expectedPatchWithoutSub, newConditions)
	patch := f.getPatchedRollout(patchIndex)
	assert.Equal(t, calculatePatch(r2, expectedPatch), patch)
}

func TestRollBackToStableAndStepChange(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))

	r2 := bumpVersion(r1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	r2.Spec.Template = r1.Spec.Template

	rs1 := newReplicaSetWithStatus(r1, 9, 9)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 9, 10, false)
	r2.Spec.Strategy.CanaryStrategy.Steps[0].SetWeight = pointer.Int32Ptr(20)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	updatedRSIndex := f.expectUpdateReplicaSetAction(rs1)
	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	updatedReplicaSet := f.getUpdatedReplicaSet(updatedRSIndex)
	assert.Equal(t, "3", updatedReplicaSet.Annotations[annotations.RevisionAnnotation])
	assert.Equal(t, "1", updatedReplicaSet.Annotations[annotations.RevisionHistoryAnnotation])

	expectedPatchWithoutSub := `{
		"status":{
			"currentPodHash": "%s",
			"currentStepHash": "%s",
			"currentStepIndex":1,
			"conditions": %s
		}
	}`
	newPodHash := controller.ComputeHash(&r2.Spec.Template, r2.Status.CollisionCount)
	newStepHash := conditions.ComputeStepHash(r2)
	newConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs1, false)
	expectedPatch := fmt.Sprintf(expectedPatchWithoutSub, newPodHash, newStepHash, newConditions)
	patch := f.getPatchedRollout(patchIndex)
	assert.Equal(t, calculatePatch(r2, expectedPatch), patch)
}

func TestCanaryRolloutIncrementStepIfSetWeightsAreCorrect(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	rs1 := newReplicaSetWithStatus(r1, 9, 9)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	r2 := bumpVersion(r1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	r3 := bumpVersion(r2)
	rs3 := newReplicaSetWithStatus(r3, 1, 1)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, rs3)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2, rs3)

	r3 = updateCanaryRolloutStatus(r3, rs1PodHash, 10, 1, 10, false)
	f.rolloutLister = append(f.rolloutLister, r3)
	f.objects = append(f.objects, r3)

	patchIndex := f.expectPatchRolloutAction(r3)
	f.run(getKey(r3, t))

	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{
		"status":{
			"currentStepIndex":1,
			"conditions": %s
		}
	}`
	newConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs3, false)
	assert.Equal(t, calculatePatch(r3, fmt.Sprintf(expectedPatch, newConditions)), patch)
}

func TestSyncRolloutsSetPauseStartTime(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: int32Ptr(10),
		}, {
			Pause: &v1alpha1.RolloutPause{
				Duration: int32Ptr(10),
			},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 9, 9)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2 := newReplicaSetWithStatus(r2, 1, 1)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 1, 10, false)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	expectedPatchWithoutTime := `{
		"spec" :{
			"paused": true
		},
		"status":{
			"pauseStartTime": "%s",
			"conditions": %s
		}
	}`
	condtions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, r2, false)
	expectedPatch := fmt.Sprintf(expectedPatchWithoutTime, metav1.Now().UTC().Format(time.RFC3339), condtions)

	index := f.expectPatchRolloutActionWithPatch(r2, expectedPatch)
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(index)
	assert.Equal(t, calculatePatch(r2, expectedPatch), patch)
}

func TestSyncRolloutWaitAddToQueue(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: int32Ptr(10),
		}, {
			Pause: &v1alpha1.RolloutPause{
				Duration: int32Ptr(10),
			},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 9, 9)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 1, 10, true)
	progressingCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, rs2)
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	r2.Status.ObservedGeneration = conditions.ComputeGenerationHash(r2.Spec)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	key := fmt.Sprintf("%s/%s", r2.Namespace, r2.Name)
	c, i, k8sI := f.newController(func() time.Duration { return 30 * time.Minute })
	f.runController(key, true, false, c, i, k8sI)

	//When the controller starts, it will enqueue the rollout while syncing the informer and during the reconciliation step
	assert.Equal(t, 2, f.enqueuedObjects[key])

}

func TestSyncRolloutIgnoreWaitOutsideOfReconciliationPeriod(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: int32Ptr(10),
		},
		{
			Pause: &v1alpha1.RolloutPause{
				Duration: int32Ptr(int32(3600)), //1 hour
			},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 9, 9)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2 := newReplicaSetWithStatus(r2, 1, 1)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 1, 10, true)
	r2.Status.ObservedGeneration = conditions.ComputeGenerationHash(r2.Spec)
	progressingCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, rs2)
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	key := fmt.Sprintf("%s/%s", r2.Namespace, r2.Name)
	c, i, k8sI := f.newController(func() time.Duration { return 30 * time.Minute })
	f.runController(key, true, false, c, i, k8sI)
	//When the controller starts, it will enqueue the rollout so we expect the rollout to enqueue at least once.
	assert.Equal(t, 1, f.enqueuedObjects[key])

}

func TestSyncRolloutWaitIncrementStepIndex(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: int32Ptr(10),
		},
		{
			Pause: &v1alpha1.RolloutPause{
				Duration: int32Ptr(5),
			},
		}, {
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	r1.Status.Canary.StableRS = "895c6c4f9"

	r2 := bumpVersion(r1)
	rs1 := newReplicaSetWithStatus(r1, 9, 9)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 1, 10, false)
	earlier := metav1.Now()
	earlier.Time = earlier.Add(-10 * time.Second)
	r2.Status.PauseStartTime = &earlier

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{
		"status":{
			"pauseStartTime": null,
			"currentStepIndex":2,
			"conditions": %s
		}
	}`
	newCondtions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs2, false)
	assert.Equal(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, newCondtions)), patch)
}

func TestCanaryRolloutStatusHPAStatusFields(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(20),
		}, {
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 5, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	r1.Status.Selector = ""
	r2 := bumpVersion(r1)
	progressingCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, r2)
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	rs1 := newReplicaSetWithStatus(r1, 4, 4)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 5, 1, 10, true)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	expectedPatchWithSub := `{
		"status":{
			"HPAReplicas":5,
			"selector":"foo=bar"
		}
	}`

	index := f.expectPatchRolloutActionWithPatch(r2, expectedPatchWithSub)
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(index)
	assert.Equal(t, calculatePatch(r2, expectedPatchWithSub), patch)
}

func TestCanaryRolloutWithCanaryService(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	canarySvc := newService("canary", 80, nil)
	rollout := newCanaryRollout("foo", 0, nil, nil, nil, intstr.FromInt(1), intstr.FromInt(0))
	rs := newReplicaSetWithStatus(rollout, 0, 0)
	rollout.Spec.Strategy.CanaryStrategy.CanaryService = canarySvc.Name

	f.rolloutLister = append(f.rolloutLister, rollout)
	f.objects = append(f.objects, rollout)
	f.kubeobjects = append(f.kubeobjects, canarySvc, rs)
	f.serviceLister = append(f.serviceLister, canarySvc)

	_ = f.expectPatchServiceAction(canarySvc, rollout.Status.CurrentPodHash)
	_ = f.expectPatchRolloutAction(rollout)
	f.run(getKey(rollout, t))
}

func TestCanaryRolloutWithInvalidCanaryServiceName(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	canarySvc := newService("invalid-canary", 80, make(map[string]string))
	rollout := newCanaryRollout("foo", 0, nil, nil, nil, intstr.FromInt(1), intstr.FromInt(0))
	rs := newReplicaSetWithStatus(rollout, 0, 0)
	rollout.Spec.Strategy.CanaryStrategy.CanaryService = canarySvc.Name

	f.rolloutLister = append(f.rolloutLister, rollout)
	f.objects = append(f.objects, rollout)
	f.kubeobjects = append(f.kubeobjects, rs)

	patchIndex := f.expectPatchRolloutAction(rollout)
	f.runExpectError(getKey(rollout, t), true)

	patch := make(map[string]interface{})
	patchData := f.getPatchedRollout(patchIndex)
	err := json.Unmarshal([]byte(patchData), &patch)
	assert.NoError(t, err)

	c, ok, err := unstructured.NestedSlice(patch, "status", "conditions")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Len(t, c, 1)

	condition, ok := c[0].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, condition["reason"], conditions.ServiceNotFoundReason)
}
