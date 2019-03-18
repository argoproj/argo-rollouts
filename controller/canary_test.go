package controller

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
)

func newCanaryRolloutWithStatus(name string, replicas int, revisionHistoryLimit *int32, steps []v1alpha1.CanaryStep, stepIndex *int32, maxSurge, maxUnavailable intstr.IntOrString, stableRS string) *v1alpha1.Rollout {
	ro := newCanaryRollout(name, replicas, revisionHistoryLimit, steps, stepIndex, maxSurge, maxUnavailable)
	ro.Status.Canary.StableRS = stableRS
	return ro
}

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
	controller := &Controller{
		rolloutsclientset: &fake,
		kubeclientset:     &k8sfake,
		recorder:          &record.FakeRecorder{},
	}

	// Handle case with no steps
	r := newCanaryRollout("test", 1, nil, nil, nil, intstr.FromInt(0), intstr.FromInt(1))
	stepResult, err := controller.reconcileCanaryPause(r)
	assert.Nil(t, err)
	assert.False(t, stepResult)
	assert.Len(t, fake.Actions(), 0)

	r2 := newCanaryRollout("test", 1, nil, []v1alpha1.CanaryStep{{SetWeight: int32Ptr(10)}}, nil, intstr.FromInt(0), intstr.FromInt(1))
	r2.Status.CurrentStepIndex = int32Ptr(1)
	stepResult, err = controller.reconcileCanaryPause(r2)
	assert.Nil(t, err)
	assert.False(t, stepResult)
	assert.Len(t, fake.Actions(), 0)

}

func TestCanaryRolloutEnterPauseState(t *testing.T) {
	f := newFixture(t)

	steps := []v1alpha1.CanaryStep{
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 10, 10)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	r2 := bumpVersion(r1)
	r2.Status.Canary.StableRS = "895c6c4f9"
	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 0, 0)
	r2.Status.AvailableReplicas = 10

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)

	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patchBytes := filterInformerActions(f.client.Actions())[0].(core.PatchAction).GetPatch()
	expectedPatchTemplate := `{
    "spec":{
        "paused": true
    },
	"status":{
		"pauseStartTime":"%s"
	}
}`
	expectedPatchWithoutObservedGen := fmt.Sprintf(expectedPatchTemplate, metav1.Now().UTC().Format(time.RFC3339))
	expectedPatch := calculatePatch(r2, expectedPatchWithoutObservedGen)
	assert.Equal(t, expectedPatch, string(patchBytes))
}

func TestCanaryRolloutNoProgressWhilePaused(t *testing.T) {
	f := newFixture(t)

	steps := []v1alpha1.CanaryStep{
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 10, 10)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	r2 := bumpVersion(r1)
	r2.Status.Canary.StableRS = "895c6c4f9"
	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 0, 0)
	r2.Status.AvailableReplicas = 10

	r2.Spec.Paused = true
	now := metav1.Now()
	r2.Status.PauseStartTime = &now

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)

	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
}

func TestCanaryRolloutIncrementStepAfterUnPaused(t *testing.T) {
	f := newFixture(t)

	steps := []v1alpha1.CanaryStep{
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 10, 10)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	r2 := bumpVersion(r1)
	r2.Status.Canary.StableRS = "895c6c4f9"
	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 0, 0)
	r2.Status.AvailableReplicas = 10

	r2.Spec.Paused = false
	now := metav1.Now()
	r2.Status.PauseStartTime = &now

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)

	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	patchBytes := filterInformerActions(f.client.Actions())[0].(core.PatchAction).GetPatch()
	expectedPatchTemplate := `{
	"status":{
		"canary": {
			"stableRS":"5f79b78d7f"
		},
		"pauseStartTime": null,
		"currentStepIndex": 1
	}
}`
	expectedPatch := calculatePatch(r2, expectedPatchTemplate)
	assert.Equal(t, expectedPatch, string(patchBytes))
}

func TestCanaryRolloutUpdateStatusWhenAtEndOfSteps(t *testing.T) {
	f := newFixture(t)

	steps := []v1alpha1.CanaryStep{
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	r2 := bumpVersion(r1)
	expectedStableRS := r2.Status.CurrentPodHash
	r2.Status.Canary.StableRS = "895c6c4f9"
	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 10, 10)
	r2.Status.AvailableReplicas = 10

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)

	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patchBytes := filterInformerActions(f.client.Actions())[0].(core.PatchAction).GetPatch()
	expectedPatchWithoutStableRS := calculatePatch(r2, `{
	"status": {
		"canary": {
			"stableRS": "%s"
		}
	}
}`)
	expectedPatch := fmt.Sprintf(expectedPatchWithoutStableRS, expectedStableRS)
	assert.Equal(t, expectedPatch, string(patchBytes))
}

func TestResetCurrentStepIndexOnStepChange(t *testing.T) {
	f := newFixture(t)
	steps := []v1alpha1.CanaryStep{
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}

	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(1), intstr.FromInt(0), intstr.FromInt(1))
	r1.Status.Canary.StableRS = "895c6c4f9"
	r1.Status.AvailableReplicas = 10
	r2 := bumpVersion(r1)
	expectedCurrentPodHash := r2.Status.CurrentPodHash
	r2.Status.CurrentPodHash = r1.Status.Canary.StableRS
	r2.Spec.Strategy.CanaryStrategy.Steps = append(steps, v1alpha1.CanaryStep{Pause: &v1alpha1.RolloutPause{}})
	expectedCurrentStepHash := conditions.ComputeStepHash(r2)

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 10, 10)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patchBytes := filterInformerActions(f.client.Actions())[0].(core.PatchAction).GetPatch()
	expectedPatchWithoutPodHash := calculatePatch(r2, `{
	"status": {
		"currentStepIndex":0,
		"currentPodHash": "%s",
		"currentStepHash": "%s"
	}
}`)
	expectedPatch := fmt.Sprintf(expectedPatchWithoutPodHash, expectedCurrentPodHash, expectedCurrentStepHash)
	assert.Equal(t, expectedPatch, string(patchBytes))

}

func TestResetCurrentStepIndexOnPodSpecChange(t *testing.T) {
	f := newFixture(t)
	steps := []v1alpha1.CanaryStep{
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}

	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(1), intstr.FromInt(0), intstr.FromInt(1))
	r1.Status.Canary.StableRS = "895c6c4f9"
	r1.Status.AvailableReplicas = 10
	r2 := bumpVersion(r1)
	expectedCurrentPodHash := r2.Status.CurrentPodHash
	r2.Status.CurrentPodHash = r1.Status.Canary.StableRS

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 10, 10)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patchBytes := filterInformerActions(f.client.Actions())[0].(core.PatchAction).GetPatch()
	expectedPatchWithoutPodHash := calculatePatch(r2, `{
	"status": {
		"currentStepIndex":0,
		"currentPodHash": "%s"
	}
}`)
	expectedPatch := fmt.Sprintf(expectedPatchWithoutPodHash, expectedCurrentPodHash)
	assert.Equal(t, expectedPatch, string(patchBytes))

}

func TestCanaryRolloutCreateFirstReplicasetNoSteps(t *testing.T) {
	f := newFixture(t)

	r := newCanaryRollout("foo", 10, nil, nil, nil, intstr.FromInt(1), intstr.FromInt(0))
	r.Status.CurrentPodHash = ""
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	rs := newReplicaSet(r, "foo-895c6c4f9", 1)

	f.expectCreateReplicaSetAction(rs)
	f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))

	patchBytes := filterInformerActions(f.client.Actions())[0].(core.PatchAction).GetPatch()
	expectedPatch := calculatePatch(r, `{
	"status":{
		"canary":{
			"stableRS":"895c6c4f9"
		},
		"currentPodHash":"895c6c4f9"
	}
}`)
	assert.Equal(t, expectedPatch, string(patchBytes))
}

func TestCanaryRolloutCreateFirstReplicasetWithSteps(t *testing.T) {
	f := newFixture(t)
	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	r := newCanaryRollout("foo", 10, nil, steps, nil, intstr.FromInt(1), intstr.FromInt(0))
	r.Status.CurrentPodHash = ""
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	rs := newReplicaSet(r, "foo-895c6c4f9", 1)

	f.expectCreateReplicaSetAction(rs)
	f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))

	patchBytes := filterInformerActions(f.client.Actions())[0].(core.PatchAction).GetPatch()
	expectedPatch := calculatePatch(r, `{
	"status":{
		"canary":{
			"stableRS":"895c6c4f9"
		},
		"currentStepIndex":1,
		"currentPodHash":"895c6c4f9"
	}
}`)
	assert.Equal(t, expectedPatch, string(patchBytes))
}

func TestCanaryRolloutCreateNewReplicaWithCorrectWeight(t *testing.T) {
	f := newFixture(t)

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r1.Status.Canary.StableRS = "895c6c4f9"
	r2 := bumpVersion(r1)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 10, 10)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 0)
	f.expectCreateReplicaSetAction(rs2)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	newReplicaset := filterInformerActions(f.kubeclient.Actions())[0].(core.CreateAction).GetObject().(*appsv1.ReplicaSet)
	assert.Equal(t, int32(1), *newReplicaset.Spec.Replicas)
}

func TestCanaryRolloutScaleUpNewReplicaWithCorrectWeight(t *testing.T) {
	f := newFixture(t)

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(40),
	}}
	r1 := newCanaryRollout("foo", 5, nil, steps, int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r1.Status.Canary.StableRS = "895c6c4f9"
	r2 := bumpVersion(r1)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 3, 3)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)
	f.expectUpdateReplicaSetAction(rs2)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	newReplicaset := filterInformerActions(f.kubeclient.Actions())[0].(core.UpdateAction).GetObject().(*appsv1.ReplicaSet)
	assert.Equal(t, int32(2), *newReplicaset.Spec.Replicas)
}

func TestCanaryRolloutScaleDownStableToMatchWeight(t *testing.T) {
	f := newFixture(t)

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r1.Status.Canary.StableRS = "895c6c4f9"

	r2 := bumpVersion(r1)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 10, 10)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)
	f.expectUpdateReplicaSetAction(rs1)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	expectedRS1 := rs1.DeepCopy()
	expectedRS1.Spec.Replicas = int32Ptr(9)
	assert.Len(t, filterInformerActions(f.kubeclient.Actions()), 1)
	assert.Equal(t, expectedRS1, filterInformerActions(f.kubeclient.Actions())[0].(core.UpdateAction).GetObject().(*appsv1.ReplicaSet))
}

func TestCanaryRolloutScaleDownOldRs(t *testing.T) {
	f := newFixture(t)

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r1.Status.Canary.StableRS = "895c6c4f9"

	r2 := bumpVersion(r1)

	r3 := bumpVersion(r2)
	f.rolloutLister = append(f.rolloutLister, r3)
	f.objects = append(f.objects, r3)

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 9, 9)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)

	rs3 := newReplicaSetWithStatus(r3, "foo-8cdf7bbb4", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs3)
	f.replicaSetLister = append(f.replicaSetLister, rs3)

	f.expectUpdateReplicaSetAction(rs2)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	expectedRS2 := rs2.DeepCopy()
	expectedRS2.Spec.Replicas = int32Ptr(0)
	expectedRS2.Annotations[annotations.DesiredReplicasAnnotation] = "10"

	assert.Equal(t, expectedRS2, filterInformerActions(f.kubeclient.Actions())[0].(core.UpdateAction).GetObject().(*appsv1.ReplicaSet))
}

func TestRollBackToStable(t *testing.T) {
	f := newFixture(t)

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r1.Status.Canary.StableRS = "895c6c4f9"

	r2 := bumpVersion(r1)
	r2.Spec.Template = r1.Spec.Template
	r2.Status.AvailableReplicas = 10
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 9, 9)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)

	f.expectUpdateReplicaSetAction(rs1)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	expectedRS1 := rs1.DeepCopy()
	expectedRS1.Annotations[annotations.RevisionAnnotation] = "3"
	expectedRS1.Annotations[annotations.RevisionHistoryAnnotation] = "1"
	firstUpdatedRS1 := filterInformerActions(f.kubeclient.Actions())[0].(core.UpdateAction).GetObject().(*appsv1.ReplicaSet)
	assert.Equal(t, expectedRS1, firstUpdatedRS1)

	expectedPatchWithoutCurrPodHash := calculatePatch(r2, `{
	"status":{
		"currentPodHash": "%s",
		"currentStepIndex":1
    }
}`)
	expectedPatch := fmt.Sprintf(expectedPatchWithoutCurrPodHash, controller.ComputeHash(&r2.Spec.Template, r2.Status.CollisionCount))
	patchBytes := filterInformerActions(f.client.Actions())[0].(core.PatchAction).GetPatch()
	assert.Equal(t, expectedPatch, string(patchBytes))
}

func TestRollBackToStableAndStepChange(t *testing.T) {
	f := newFixture(t)

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r1.Status.Canary.StableRS = "895c6c4f9"

	r2 := bumpVersion(r1)
	r2.Spec.Template = r1.Spec.Template
	r2.Status.AvailableReplicas = 10
	r2.Spec.Strategy.CanaryStrategy.Steps[0].SetWeight = pointer.Int32Ptr(20)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 9, 9)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)

	f.expectUpdateReplicaSetAction(rs1)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	expectedRS1 := rs1.DeepCopy()
	expectedRS1.Annotations[annotations.RevisionAnnotation] = "3"
	expectedRS1.Annotations[annotations.RevisionHistoryAnnotation] = "1"
	firstUpdatedRS1 := filterInformerActions(f.kubeclient.Actions())[0].(core.UpdateAction).GetObject().(*appsv1.ReplicaSet)
	assert.Equal(t, expectedRS1, firstUpdatedRS1)

	expectedPatchWithoutCurrPodHash := calculatePatch(r2, `{
	"status":{
		"currentPodHash": "%s",
		"currentStepHash": "%s",
		"currentStepIndex":1
    }
}`)
	newPodHash := controller.ComputeHash(&r2.Spec.Template, r2.Status.CollisionCount)
	newStepHash := conditions.ComputeStepHash(r2)
	expectedPatch := fmt.Sprintf(expectedPatchWithoutCurrPodHash, newPodHash, newStepHash)
	patchBytes := filterInformerActions(f.client.Actions())[0].(core.PatchAction).GetPatch()
	assert.Equal(t, expectedPatch, string(patchBytes))
}

func TestCanaryRolloutIncrementStepIfSetWeightsAreCorrect(t *testing.T) {
	f := newFixture(t)

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r1.Status.Canary.StableRS = "895c6c4f9"

	r2 := bumpVersion(r1)

	r3 := bumpVersion(r2)
	f.rolloutLister = append(f.rolloutLister, r3)
	f.objects = append(f.objects, r3)

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 9, 9)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)

	rs3 := newReplicaSetWithStatus(r3, "foo-8cdf7bbb4", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs3)
	f.replicaSetLister = append(f.replicaSetLister, rs3)

	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patchBytes := filterInformerActions(f.client.Actions())[0].(core.PatchAction).GetPatch()
	expectedPatch := calculatePatch(r3, `{
	"status":{
		"availableReplicas":10,
		"canary":{
			"stableRS":"8cdf7bbb4"
		},
		"currentStepIndex":1
    }
}`)
	assert.Equal(t, expectedPatch, string(patchBytes))
}

func TestSyncRolloutsSetPauseStartTime(t *testing.T) {
	f := newFixture(t)

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
	r1.Status.Canary.StableRS = "895c6c4f9"

	r2 := bumpVersion(r1)
	r2.Status.AvailableReplicas = 10
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 9, 9)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)

	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patchBytes := filterInformerActions(f.client.Actions())[0].(core.PatchAction).GetPatch()
	expectedPatch := calculatePatch(r2, `{
    "spec" :{
		"paused": true
	},
	"status":{
		"pauseStartTime": "%s"
	}
}`)
	expectedPatch = fmt.Sprintf(expectedPatch, metav1.Now().UTC().Format(time.RFC3339))
	assert.Equal(t, expectedPatch, string(patchBytes))
}

func TestSyncRolloutWaitAddToQueue(t *testing.T) {
	f := newFixture(t)

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
	r1.Status.Canary.StableRS = "895c6c4f9"

	r2 := bumpVersion(r1)
	r2.Status.AvailableReplicas = 10
	r2.Spec.Paused = true
	r2.Status.ObservedGeneration = conditions.ComputeGenerationHash(r2.Spec)

	now := metav1.Now()
	r2.Status.PauseStartTime = &now
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 9, 9)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)

	key := fmt.Sprintf("%s/%s", r2.Namespace, r2.Name)
	c, i, k8sI := f.newController(func() time.Duration { return 30 * time.Minute })
	f.runController(key, true, false, c, i, k8sI)

	//When the controller starts, it will enqueue the rollout while syncing the informer and during the reconciliation step
	assert.Equal(t, 2, f.enqueuedObjects[key])

}

func TestSyncRolloutIgnoreWaitOutsideOfReconciliationPeriod(t *testing.T) {
	f := newFixture(t)

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
	r1.Status.Canary.StableRS = "895c6c4f9"

	r2 := bumpVersion(r1)
	now := metav1.Now()
	r2.Status.PauseStartTime = &now
	r2.Spec.Paused = true
	r2.Status.ObservedGeneration = conditions.ComputeGenerationHash(r2.Spec)
	r2.Status.AvailableReplicas = 10
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 9, 9)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)

	key := fmt.Sprintf("%s/%s", r2.Namespace, r2.Name)
	c, i, k8sI := f.newController(func() time.Duration { return 30 * time.Minute })
	f.runController(key, true, false, c, i, k8sI)
	//When the controller starts, it will enqueue the rollout so we expect the rollout to enqueue at least once.
	assert.Equal(t, 1, f.enqueuedObjects[key])

}

func TestSyncRolloutWaitIncrementStepIndex(t *testing.T) {

	f := newFixture(t)
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
	earlier := metav1.Now()
	earlier.Time = earlier.Add(-10 * time.Second)
	r2.Status.PauseStartTime = &earlier
	r2.Status.AvailableReplicas = 10
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 9, 9)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)

	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patchBytes := filterInformerActions(f.client.Actions())[0].(core.PatchAction).GetPatch()
	expectedPatch := calculatePatch(r2, `{
	"status":{
		"pauseStartTime": null,
		"currentStepIndex":2
	}
}`)
	assert.Equal(t, expectedPatch, string(patchBytes))
}
