package rollout

import (
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	k8sinformers "k8s.io/client-go/informers"

	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

func newCanaryRollout(name string, replicas int, revisionHistoryLimit *int32, steps []v1alpha1.CanaryStep, stepIndex *int32, maxSurge, maxUnavailable intstr.IntOrString) *v1alpha1.Rollout {
	selector := map[string]string{"foo": "bar"}
	rollout := newRollout(name, replicas, revisionHistoryLimit, selector)
	rollout.Spec.Strategy.Canary = &v1alpha1.CanaryStrategy{
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
	newRollout.Generation = newRollout.Generation + 1
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

// TestCanaryRolloutBumpVersion verifies we correctly bump revision of Rollout and new ReplicaSet
func TestCanaryRolloutBumpVersion(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newCanaryRollout("foo", 10, nil, nil, int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	r1.Status.StableRS = rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 := bumpVersion(r1)
	r2.Annotations[annotations.RevisionAnnotation] = "1"
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs2 := newReplicaSetWithStatus(r2, 1, 0)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	createdRSIndex := f.expectCreateReplicaSetAction(rs2)
	updatedRolloutRevisionIndex := f.expectUpdateRolloutAction(r2)         // update rollout revision
	updatedRolloutConditionsIndex := f.expectUpdateRolloutStatusAction(r2) // update rollout conditions
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	createdRS := f.getCreatedReplicaSet(createdRSIndex)
	assert.Equal(t, int32(1), *createdRS.Spec.Replicas)
	assert.Equal(t, "2", createdRS.Annotations[annotations.RevisionAnnotation])

	updatedRollout := f.getUpdatedRollout(updatedRolloutRevisionIndex)
	assert.Equal(t, "2", updatedRollout.Annotations[annotations.RevisionAnnotation])

	updatedRollout = f.getUpdatedRollout(updatedRolloutConditionsIndex)
	progressingCondition := conditions.GetRolloutCondition(updatedRollout.Status, v1alpha1.RolloutProgressing)
	assert.NotNil(t, progressingCondition)
	assert.Equal(t, conditions.NewReplicaSetReason, progressingCondition.Reason)
	assert.Equal(t, corev1.ConditionTrue, progressingCondition.Status)
	assert.Equal(t, fmt.Sprintf(conditions.NewReplicaSetMessage, createdRS.Name), progressingCondition.Message)
}

func TestReconcileCanaryStepsHandleBaseCases(t *testing.T) {
	fake := fake.Clientset{}
	k8sfake := k8sfake.Clientset{}

	// Handle case with no steps
	r := newCanaryRollout("test", 1, nil, nil, nil, intstr.FromInt(0), intstr.FromInt(1))
	roCtx := &rolloutContext{
		rollout: r,
		log:     logutil.WithRollout(r),
		reconcilerBase: reconcilerBase{
			argoprojclientset: &fake,
			kubeclientset:     &k8sfake,
			recorder:          &FakeEventRecorder{},
		},
	}
	stepResult := roCtx.reconcileCanaryPause()
	assert.False(t, stepResult)
	assert.Len(t, fake.Actions(), 0)

	r2 := newCanaryRollout("test", 1, nil, []v1alpha1.CanaryStep{{SetWeight: int32Ptr(10)}}, nil, intstr.FromInt(0), intstr.FromInt(1))
	r2.Status.CurrentStepIndex = int32Ptr(1)
	roCtx2 := &rolloutContext{
		rollout: r2,
		log:     logutil.WithRollout(r),
		reconcilerBase: reconcilerBase{
			argoprojclientset: &fake,
			kubeclientset:     &k8sfake,
			recorder:          &FakeEventRecorder{},
		},
	}
	stepResult = roCtx2.reconcileCanaryPause()
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
		"status":{
			"pauseConditions":[{
				"reason": "%s",
				"startTime": "%s"
			}],
			"conditions": %s,
			"controllerPause": true
		}
	}`

	conditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, r2, false, "")
	now := metav1.Now().UTC().Format(time.RFC3339)
	expectedPatchWithoutObservedGen := fmt.Sprintf(expectedPatchTemplate, v1alpha1.PauseReasonCanaryPauseStep, now, conditions)
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

	progressingCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, r2, "")
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

	progressingCondition, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, r2, "")
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
	_, pausedCondition := newProgressingCondition(conditions.PausedRolloutReason, rs2, "")
	expectedPatch := fmt.Sprintf(`{
		"status": {
			"conditions": [%s]
		}
	}`, pausedCondition)
	assert.Equal(t, calculatePatch(r2, expectedPatch), patch)
}

func TestCanaryRolloutResetProgressDeadlineOnRetry(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)

	progressingCondition, _ := newProgressingCondition(conditions.RolloutAbortedReason, r2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, false)
	r2.Status.Abort = false
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	addPausedConditionPatch := f.expectPatchRolloutAction(r2)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(addPausedConditionPatch)
	_, retryCondition := newProgressingCondition(conditions.RolloutRetryReason, r2, "")
	expectedPatch := fmt.Sprintf(`{
		"status": {
			"conditions": [%s]
		}
	}`, retryCondition)
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
	r2.Status.ControllerPause = true

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	expectedPatchTemplate := `{
	"status":{
		"controllerPause": null,
		"conditions" : %s,
		"currentStepIndex": 1
	}
}`
	generatedConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs2, false, "")
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
			"stableRS": "%s",
			"conditions": %s
		}
	}`

	expectedPatch := fmt.Sprintf(expectedPatchWithoutStableRS, expectedStableRS, generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs2, false, ""))
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
	r2.Spec.Strategy.Canary.Steps = append(steps, v1alpha1.CanaryStep{Pause: &v1alpha1.RolloutPause{}})
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
	newConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, r2, false, "")
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
	newConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, r2, false, "")

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
	updatedRolloutIndex := f.expectUpdateRolloutStatusAction(r)
	patchIndex := f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))

	updatedRollout := f.getUpdatedRollout(updatedRolloutIndex)
	progressingCondition := conditions.GetRolloutCondition(updatedRollout.Status, v1alpha1.RolloutProgressing)
	assert.NotNil(t, progressingCondition)
	assert.Equal(t, conditions.NewReplicaSetReason, progressingCondition.Reason)
	assert.Equal(t, corev1.ConditionTrue, progressingCondition.Status)
	assert.Equal(t, fmt.Sprintf(conditions.NewReplicaSetMessage, rs.Name), progressingCondition.Message)

	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{
		"status":{
			"stableRS":"` + rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] + `",
			"currentPodHash":"` + rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] + `",
			"conditions": %s
		}
	}`

	newConditions := generateConditionsPatch(false, conditions.ReplicaSetUpdatedReason, rs, false, "")

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
	updatedRolloutIndex := f.expectUpdateRolloutStatusAction(r)
	patchIndex := f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))

	updatedRollout := f.getUpdatedRollout(updatedRolloutIndex)
	progressingCondition := conditions.GetRolloutCondition(updatedRollout.Status, v1alpha1.RolloutProgressing)
	assert.NotNil(t, progressingCondition)
	assert.Equal(t, conditions.NewReplicaSetReason, progressingCondition.Reason)
	assert.Equal(t, corev1.ConditionTrue, progressingCondition.Status)
	assert.Equal(t, fmt.Sprintf(conditions.NewReplicaSetMessage, rs.Name), progressingCondition.Message)

	patch := f.getPatchedRollout(patchIndex)
	expectedPatchWithSub := `{
		"status":{
			"stableRS":"` + rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] + `",
			"currentStepIndex":1,
			"currentPodHash":"` + rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] + `",
			"conditions": %s
		}
	}`
	expectedPatch := fmt.Sprintf(expectedPatchWithSub, generateConditionsPatch(false, conditions.ReplicaSetUpdatedReason, rs, false, ""))

	assert.Equal(t, calculatePatch(r, expectedPatch), patch)
}

func TestCanaryRolloutCreateNewReplicaWithCorrectWeight(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r1.Status.StableRS = "895c6c4f9"
	r2 := bumpVersion(r1)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 1, 0)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	createdRSIndex := f.expectCreateReplicaSetAction(rs2)
	updatedRolloutIndex := f.expectUpdateRolloutStatusAction(r2)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	createdRS := f.getCreatedReplicaSet(createdRSIndex)
	assert.Equal(t, int32(1), *createdRS.Spec.Replicas)

	updatedRollout := f.getUpdatedRollout(updatedRolloutIndex)
	progressingCondition := conditions.GetRolloutCondition(updatedRollout.Status, v1alpha1.RolloutProgressing)
	assert.NotNil(t, progressingCondition)
	assert.Equal(t, conditions.NewReplicaSetReason, progressingCondition.Reason)
	assert.Equal(t, corev1.ConditionTrue, progressingCondition.Status)
	assert.Equal(t, fmt.Sprintf(conditions.NewReplicaSetMessage, createdRS.Name), progressingCondition.Message)
}

func TestCanaryRolloutScaleUpNewReplicaWithCorrectWeight(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(40),
	}}
	r1 := newCanaryRollout("foo", 5, nil, steps, int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r1.Status.StableRS = "895c6c4f9"
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
	r1.Status.StableRS = r1.Status.CurrentPodHash

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
	r1.Status.StableRS = r1.Status.CurrentPodHash
	r2 := bumpVersion(r1)
	r3 := bumpVersion(r2)

	rs1 := newReplicaSetWithStatus(r1, 9, 9)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs3 := newReplicaSetWithStatus(r3, 1, 1)

	f.objects = append(f.objects, r3)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2, rs3)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2, rs3)

	updateRSIndex := f.expectUpdateReplicaSetAction(rs2)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	expectedRS2 := rs2.DeepCopy()
	expectedRS2.Spec.Replicas = int32Ptr(0)
	expectedRS2.Annotations[annotations.DesiredReplicasAnnotation] = "10"
	updatedRS := f.getUpdatedReplicaSet(updateRSIndex)

	assert.Equal(t, expectedRS2, updatedRS)
}

// TestCanaryRolloutScaleDownOldRsDontScaleDownTooMuch catches a bug where we scaled down too many old replicasets
// due to miscalculating scaleDownCount
func TestCanaryRolloutScaleDownOldRsDontScaleDownTooMuch(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newCanaryRollout("foo", 4, nil, nil, int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)
	r3 := bumpVersion(r2)
	r3.Status.StableRS = r3.Status.CurrentPodHash

	rs1 := newReplicaSetWithStatus(r1, 5, 5)
	rs2 := newReplicaSetWithStatus(r2, 5, 5)
	rs3 := newReplicaSetWithStatus(r3, 5, 0)

	f.objects = append(f.objects, r3)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2, rs3)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2, rs3)

	updatedRS1Index := f.expectUpdateReplicaSetAction(rs1)
	updatedRS2Index := f.expectUpdateReplicaSetAction(rs2)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	updatedRS1 := f.getUpdatedReplicaSet(updatedRS1Index)
	assert.Equal(t, int32(0), *updatedRS1.Spec.Replicas)
	updatedRS2 := f.getUpdatedReplicaSet(updatedRS2Index)
	assert.Equal(t, int32(4), *updatedRS2.Spec.Replicas)

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
	f.expectUpdateReplicaSetAction(rs1)
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
	newConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs1, false, "")
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
	r2.Spec.Strategy.Canary.MaxSurge = &maxSurge
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
	newConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, r2, false, "")
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
	r2.Spec.Strategy.Canary.Steps[0].SetWeight = pointer.Int32Ptr(20)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	updatedRSIndex := f.expectUpdateReplicaSetAction(rs1)
	f.expectUpdateReplicaSetAction(rs1)
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
	newConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs1, false, "")
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
	newConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs3, false, "")
	assert.Equal(t, calculatePatch(r3, fmt.Sprintf(expectedPatch, newConditions)), patch)
}

func TestSyncRolloutWaitAddToQueue(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: int32Ptr(10),
		}, {
			Pause: &v1alpha1.RolloutPause{
				Duration: v1alpha1.DurationFromInt(10),
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
	progressingCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	r2.Status.ObservedGeneration = strconv.Itoa(int(r2.Generation))
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
				Duration: v1alpha1.DurationFromInt(3600), //1 hour
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
	r2.Status.ObservedGeneration = strconv.Itoa(int(r2.Generation))
	progressingCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, rs2, "")
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
				Duration: v1alpha1.DurationFromInt(5),
			},
		}, {
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	r1.Status.StableRS = "895c6c4f9"

	r2 := bumpVersion(r1)
	rs1 := newReplicaSetWithStatus(r1, 9, 9)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 1, 10, false)
	pausedCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, pausedCondition)

	earlier := metav1.Now()
	earlier.Time = earlier.Add(-10 * time.Second)
	r2.Status.ControllerPause = true
	r2.Status.PauseConditions = []v1alpha1.PauseCondition{{
		Reason:    v1alpha1.PauseReasonCanaryPauseStep,
		StartTime: earlier,
	}}
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{
		"status":{
			"controllerPause": null,
			"pauseConditions": null,
			"currentStepIndex":2
		}
	}`
	assert.Equal(t, calculatePatch(r2, expectedPatch), patch)
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
	progressingCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, r2, "")
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

	rollout := newCanaryRollout("foo", 1, nil, nil, nil, intstr.FromInt(1), intstr.FromInt(0))
	canarySvc := newService("canary", 80, nil, rollout)
	rs := newReplicaSetWithStatus(rollout, 1, 1)
	rollout.Spec.Strategy.Canary.CanaryService = canarySvc.Name

	f.rolloutLister = append(f.rolloutLister, rollout)
	f.objects = append(f.objects, rollout)
	f.kubeobjects = append(f.kubeobjects, canarySvc, rs)
	f.serviceLister = append(f.serviceLister, canarySvc)

	_ = f.expectPatchServiceAction(canarySvc, rollout.Status.CurrentPodHash)
	_ = f.expectPatchRolloutAction(rollout)
	f.run(getKey(rollout, t))
}

func TestCanarySVCSelectors(t *testing.T) {
	for _, tc := range []struct {
		canaryReplicas      int32
		canaryReadyReplicas int32

		shouldTargetNewRS bool
	}{
		{0, 0, false},
		{2, 0, false},
		{2, 1, false},
		{2, 2, true},
	} {
		namespace := "namespace"
		selectorNewRSVal := "new-rs-xxx"
		stableService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "stable",
				Namespace: namespace,
			},
		}
		canaryService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "canary",
				Namespace: namespace,
			},
		}
		kubeclient := k8sfake.NewSimpleClientset(stableService, canaryService)
		informers := k8sinformers.NewSharedInformerFactory(kubeclient, 0)
		servicesLister := informers.Core().V1().Services().Lister()

		rollout := &v1alpha1.Rollout{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "selector-labels-test",
				Namespace: namespace,
			},
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					Canary: &v1alpha1.CanaryStrategy{
						StableService: stableService.Name,
						CanaryService: canaryService.Name,
					},
				},
			},
		}
		rc := rolloutContext{
			log: logutil.WithRollout(rollout),
			reconcilerBase: reconcilerBase{
				servicesLister: servicesLister,
				kubeclientset:  kubeclient,
				recorder:       &FakeEventRecorder{},
			},
			rollout: rollout,
			newRS: &v1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "canary",
					Namespace: namespace,
					Labels: map[string]string{
						v1alpha1.DefaultRolloutUniqueLabelKey: selectorNewRSVal,
					},
				},
				Spec: v1.ReplicaSetSpec{
					Replicas: pointer.Int32Ptr(tc.canaryReplicas),
				},
				Status: v1.ReplicaSetStatus{
					ReadyReplicas: tc.canaryReadyReplicas,
				},
			},
			stableRS: &v1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "stable",
					Namespace: namespace,
				},
			},
		}
		stopchan := make(chan struct{})
		defer close(stopchan)
		informers.Start(stopchan)
		informers.WaitForCacheSync(stopchan)
		err := rc.reconcileStableAndCanaryService()
		assert.NoError(t, err, "unable to reconcileStableAndCanaryService")
		updatedCanarySVC, err := servicesLister.Services(rc.rollout.Namespace).Get(canaryService.Name)
		assert.NoError(t, err, "unable to get updated canary service")
		if tc.shouldTargetNewRS {
			assert.Equal(t, selectorNewRSVal, updatedCanarySVC.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey],
				"canary SVC should have newRS selector label when newRS has %d replicas and %d ReadyReplicas",
				tc.canaryReplicas, tc.canaryReadyReplicas)
		} else {
			assert.Empty(t, updatedCanarySVC.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey],
				"canary SVC should not have newRS selector label when newRS has %d replicas and %d ReadyReplicas",
				tc.canaryReplicas, tc.canaryReadyReplicas)
		}
	}
}

func TestCanaryRolloutWithInvalidCanaryServiceName(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	rollout := newCanaryRollout("foo", 0, nil, nil, nil, intstr.FromInt(1), intstr.FromInt(0))
	canarySvc := newService("invalid-canary", 80, make(map[string]string), rollout)
	rs := newReplicaSetWithStatus(rollout, 0, 0)
	rollout.Spec.Strategy.Canary.CanaryService = canarySvc.Name

	f.rolloutLister = append(f.rolloutLister, rollout)
	f.objects = append(f.objects, rollout)
	f.kubeobjects = append(f.kubeobjects, rs)

	patchIndex := f.expectPatchRolloutAction(rollout)
	f.run(getKey(rollout, t))

	patch := make(map[string]interface{})
	patchData := f.getPatchedRollout(patchIndex)
	err := json.Unmarshal([]byte(patchData), &patch)
	assert.NoError(t, err)

	c, ok, err := unstructured.NestedSlice(patch, "status", "conditions")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Len(t, c, 2)

	condition, ok := c[1].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, conditions.InvalidSpecReason, condition["reason"])
	assert.Equal(t, "The Rollout \"foo\" is invalid: spec.strategy.canary.canaryService: Invalid value: \"invalid-canary\": service \"invalid-canary\" not found", condition["message"])
}

func TestCanaryRolloutWithStableService(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	rollout := newCanaryRollout("foo", 1, nil, nil, nil, intstr.FromInt(1), intstr.FromInt(0))
	stableSvc := newService("stable", 80, nil, rollout)
	rs := newReplicaSetWithStatus(rollout, 1, 1)
	rollout.Spec.Strategy.Canary.StableService = stableSvc.Name
	rollout.Status.StableRS = rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	f.rolloutLister = append(f.rolloutLister, rollout)
	f.objects = append(f.objects, rollout)
	f.kubeobjects = append(f.kubeobjects, stableSvc, rs)
	f.serviceLister = append(f.serviceLister, stableSvc)

	_ = f.expectPatchServiceAction(stableSvc, rollout.Status.CurrentPodHash)
	_ = f.expectPatchRolloutAction(rollout)
	f.run(getKey(rollout, t))
}

func TestCanaryRolloutWithInvalidStableServiceName(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	rollout := newCanaryRollout("foo", 0, nil, nil, nil, intstr.FromInt(1), intstr.FromInt(0))
	rs := newReplicaSetWithStatus(rollout, 0, 0)
	rollout.Spec.Strategy.Canary.StableService = "invalid-stable"
	rollout.Status.StableRS = rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	f.rolloutLister = append(f.rolloutLister, rollout)
	f.objects = append(f.objects, rollout)
	f.kubeobjects = append(f.kubeobjects, rs)

	patchIndex := f.expectPatchRolloutAction(rollout)
	f.run(getKey(rollout, t))

	patch := make(map[string]interface{})
	patchData := f.getPatchedRollout(patchIndex)
	err := json.Unmarshal([]byte(patchData), &patch)
	assert.NoError(t, err)

	c, ok, err := unstructured.NestedSlice(patch, "status", "conditions")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Len(t, c, 2)

	condition, ok := c[1].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, conditions.InvalidSpecReason, condition["reason"])
	assert.Equal(t, "The Rollout \"foo\" is invalid: spec.strategy.canary.stableService: Invalid value: \"invalid-stable\": service \"invalid-stable\" not found", condition["message"])
}

func TestCanaryRolloutScaleWhilePaused(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(20),
		},
	}
	r1 := newCanaryRollout("foo", 5, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 5, 5)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 5, 0, 5, true)
	r2.Spec.Replicas = pointer.Int32Ptr(10)
	pausedCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, pausedCondition)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	updatedIndex := f.expectUpdateReplicaSetAction(rs1)
	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	updatedRS := f.getUpdatedReplicaSet(updatedIndex)
	assert.Equal(t, int32(8), *updatedRS.Spec.Replicas)

	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := calculatePatch(r2, OnlyObservedGenerationPatch)
	assert.Equal(t, expectedPatch, patch)
}

func TestResumeRolloutAfterPauseDuration(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(10),
		},
		{
			Pause: &v1alpha1.RolloutPause{
				Duration: v1alpha1.DurationFromInt(60),
			},
		},
		{
			SetWeight: pointer.Int32Ptr(20),
		},
	}
	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 1, 1, true)
	overAMinuteAgo := metav1.Time{Time: time.Now().Add(-61 * time.Second)}
	r2.Status.ObservedGeneration = strconv.Itoa(int(r2.Generation))
	r2.Status.PauseConditions = []v1alpha1.PauseCondition{{
		Reason:    v1alpha1.PauseReasonCanaryPauseStep,
		StartTime: overAMinuteAgo,
	}}
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	_ = f.expectPatchRolloutAction(r2)           // this just sets a conditions. ignore for now
	patchIndex := f.expectPatchRolloutAction(r2) // this patch should resume the rollout
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(patchIndex)
	var patchObj map[string]interface{}
	err := json.Unmarshal([]byte(patch), &patchObj)
	assert.NoError(t, err)

	status := patchObj["status"].(map[string]interface{})
	assert.Equal(t, float64(2), status["currentStepIndex"])
	controllerPause, ok := status["controllerPause"]
	assert.True(t, ok)
	assert.Nil(t, controllerPause)
}

func TestNoResumeAfterPauseDurationIfUserPaused(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(10),
		},
		{
			Pause: &v1alpha1.RolloutPause{
				Duration: v1alpha1.DurationFromInt(60),
			},
		},
		{
			SetWeight: pointer.Int32Ptr(20),
		},
	}
	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(1))
	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	r1 = updateCanaryRolloutStatus(r1, rs1PodHash, 1, 1, 1, true)
	overAMinuteAgo := metav1.Time{Time: time.Now().Add(-63 * time.Second)}
	r1.Status.PauseConditions = []v1alpha1.PauseCondition{{
		Reason:    v1alpha1.PauseReasonCanaryPauseStep,
		StartTime: overAMinuteAgo,
	}}
	pausedCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, rs1, "")
	conditions.SetRolloutCondition(&r1.Status, pausedCondition)
	r1.Spec.Paused = true
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)
	f.rolloutLister = append(f.rolloutLister, r1)
	f.objects = append(f.objects, r1)

	patchIndex := f.expectPatchRolloutAction(r1)
	f.run(getKey(r1, t))
	patch := f.getPatchedRollout(patchIndex)
	assert.Equal(t, calculatePatch(r1, OnlyObservedGenerationPatch), patch)
}

func TestHandleNilNewRSOnScaleAndImageChange(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(10),
		},
		{
			Pause: &v1alpha1.RolloutPause{
				Duration: v1alpha1.DurationFromInt(60),
			},
		},
		{
			SetWeight: pointer.Int32Ptr(20),
		},
	}
	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(1))
	rs1 := newReplicaSetWithStatus(r1, 3, 3)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	r2 := bumpVersion(r1)
	r2.Spec.Replicas = pointer.Int32Ptr(3)
	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 3, 0, 3, true)
	pausedCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, rs1, "")
	conditions.SetRolloutCondition(&r2.Status, pausedCondition)

	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectUpdateReplicaSetAction(rs1)
	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	assert.Equal(t, calculatePatch(r2, OnlyObservedGenerationPatch), patch)
}

func TestHandleCanaryAbort(t *testing.T) {
	t.Run("Scale up stable ReplicaSet", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		steps := []v1alpha1.CanaryStep{
			{SetWeight: int32Ptr(10)},
			{SetWeight: int32Ptr(20)},
			{SetWeight: int32Ptr(30)},
		}
		r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
		rs1 := newReplicaSetWithStatus(r1, 9, 9)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		r2 := bumpVersion(r1)
		rs2 := newReplicaSetWithStatus(r2, 1, 1)

		f.kubeobjects = append(f.kubeobjects, rs1, rs2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

		r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 1, 10, false)
		r2.Status.Abort = true
		now := metav1.Now()
		r2.Status.AbortedAt = &now
		f.rolloutLister = append(f.rolloutLister, r2)
		f.objects = append(f.objects, r2)

		rsIndex := f.expectUpdateReplicaSetAction(rs2)
		patchIndex := f.expectPatchRolloutAction(r2)
		f.run(getKey(r2, t))

		updatedRS := f.getUpdatedReplicaSet(rsIndex)
		assert.Equal(t, int32(10), *updatedRS.Spec.Replicas)

		patch := f.getPatchedRollout(patchIndex)
		expectedPatch := `{
			"status":{
				"currentStepIndex": 0,
				"conditions": %s
			}
		}`
		newConditions := generateConditionsPatch(true, conditions.RolloutAbortedReason, r2, false, "")
		assert.Equal(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, newConditions)), patch)
	})

	t.Run("Do not reset currentStepCount if newRS is stableRS", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		steps := []v1alpha1.CanaryStep{
			{SetWeight: int32Ptr(10)},
			{SetWeight: int32Ptr(20)},
			{SetWeight: int32Ptr(30)},
		}
		r1 := newCanaryRollout("foo", 2, nil, steps, int32Ptr(3), intstr.FromInt(1), intstr.FromInt(0))
		r1.Status.Abort = true
		now := metav1.Now()
		r1.Status.AbortedAt = &now
		rs1 := newReplicaSetWithStatus(r1, 2, 2)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		r1 = updateCanaryRolloutStatus(r1, rs1PodHash, 2, 2, 2, false)

		f.kubeobjects = append(f.kubeobjects, rs1)
		f.replicaSetLister = append(f.replicaSetLister, rs1)

		f.rolloutLister = append(f.rolloutLister, r1)
		f.objects = append(f.objects, r1)

		patchIndex := f.expectPatchRolloutAction(r1)
		f.run(getKey(r1, t))
		patch := f.getPatchedRollout(patchIndex)
		expectedPatch := `{
			"status":{
				"conditions": %s
			}
		}`
		newConditions := generateConditionsPatch(true, conditions.RolloutAbortedReason, r1, false, "")
		assert.Equal(t, calculatePatch(r1, fmt.Sprintf(expectedPatch, newConditions)), patch)
	})
}
