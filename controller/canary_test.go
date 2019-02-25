package controller

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/utils/annotations"
)

func newCanaryRollout(name string, replicas int, revisionHistoryLimit *int32, steps []v1alpha1.CanaryStep, stepIndex *int32, maxSurge, maxUnavailable intstr.IntOrString) *v1alpha1.Rollout {
	selector := map[string]string{"foo": "bar"}
	rollout := newRollout(name, replicas, revisionHistoryLimit, selector)
	rollout.Spec.Strategy.Type = v1alpha1.CanaryRolloutStrategyType
	rollout.Spec.Strategy.CanaryStrategy = &v1alpha1.CanaryStrategy{
		MaxUnavailable: &maxUnavailable,
		MaxSurge:       &maxSurge,
		Steps:          steps,
	}
	rollout.Status.CurrentStepIndex = stepIndex
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
	stepResult, err := controller.reconcilePause(nil, nil, nil, r)
	assert.Nil(t, err)
	assert.False(t, stepResult)
	assert.Len(t, fake.Actions(), 0)

	//Handle case where currentStepIndex is greater than the list of steps
	r2 := newCanaryRollout("test", 1, nil, []v1alpha1.CanaryStep{{SetWeight: int32Ptr(10)}}, nil, intstr.FromInt(0), intstr.FromInt(1))
	r2.Status.CurrentStepIndex = int32Ptr(1)
	stepResult, err = controller.reconcilePause(nil, nil, nil, r2)
	assert.Nil(t, err)
	assert.False(t, stepResult)
	assert.Len(t, fake.Actions(), 0)

}

func TestReconcileCanaryStepsHandlePause(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }
	tests := []struct {
		name             string
		setPauseValue    *bool
		steps            []v1alpha1.CanaryStep
		currentStepIndex int32

		expectPatch           bool
		expectedSetPauseValue *bool
	}{
		{
			name:          "Put Canary into pause",
			setPauseValue: nil,
			steps: []v1alpha1.CanaryStep{
				{
					Pause: &v1alpha1.RolloutPause{},
				},
			},

			expectPatch:           true,
			expectedSetPauseValue: boolPtr(true),
		},
		{
			name:          "Do nothing if the canary is paused",
			setPauseValue: boolPtr(true),
			steps: []v1alpha1.CanaryStep{
				{
					Pause: &v1alpha1.RolloutPause{},
				},
			},

			expectPatch: false,
		},
		{
			name:          "Progress Canary after unpausing",
			setPauseValue: boolPtr(false),
			steps: []v1alpha1.CanaryStep{
				{
					Pause: &v1alpha1.RolloutPause{},
				},
			},

			expectPatch:           true,
			expectedSetPauseValue: nil,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			r := newCanaryRollout("test", 1, nil, test.steps, nil, intstr.FromInt(0), intstr.FromInt(1))
			r.Status.CurrentStepIndex = &test.currentStepIndex
			r.Status.SetPause = test.setPauseValue

			fake := fake.Clientset{}
			k8sfake := k8sfake.Clientset{}
			controller := &Controller{
				rolloutsclientset: &fake,
				kubeclientset:     &k8sfake,
				recorder:          &record.FakeRecorder{},
			}
			stepResult, err := controller.reconcilePause(nil, nil, nil, r)
			assert.Nil(t, err)
			assert.True(t, stepResult)
			if test.expectPatch {
				patchRollout := fake.Actions()[0].(core.PatchAction).GetPatch()
				if test.expectedSetPauseValue == nil {
					assert.Equal(t, fmt.Sprintf(setPausePatch, "null"), string(patchRollout))
				} else {
					assert.Equal(t, fmt.Sprintf(setPausePatch, "true"), string(patchRollout))
				}
			} else {
				assert.Len(t, fake.Actions(), 0)
			}

		})
	}
}

func removeWhiteSpace(str string) string {
	noSpaces := strings.Replace(str, "\n", "", -1)
	noTabs := strings.Replace(noSpaces, "\t", "", -1)
	return strings.Replace(noTabs, " ", "", -1)
}

func TestResetCurrentStepIndexOnSpecChange(t *testing.T) {
	expectedPatch := removeWhiteSpace(`{
	"status": {
		"currentPodHash":"5f79b78d7f",
		"currentStepIndex":0,
		"observedGeneration":"6577859b4d"
	}
}`)

	f := newFixture(t)
	steps := []v1alpha1.CanaryStep{
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}

	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(1), intstr.FromInt(0), intstr.FromInt(1))
	r1.Status.CurrentPodHash = "895c6c4f9"
	r1.Status.CanaryStatus.StableRS = "895c6c4f9"
	r1.Status.AvailableReplicas = 10
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 10, 10)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patchBytes := filterInformerActions(f.client.Actions())[0].(core.PatchAction).GetPatch()
	assert.Equal(t, expectedPatch, string(patchBytes))

}

func TestCanaryRolloutCreateFirstReplicaset(t *testing.T) {
	expectedPatch := removeWhiteSpace(`{
	"status":{
		"canaryStatus":{
			"stableRS":"895c6c4f9"
		},
		"currentPodHash":"895c6c4f9",
		"currentStepIndex":0,
		"observedGeneration":"6fd8456894"
	}
}`)
	f := newFixture(t)

	r := newCanaryRollout("foo", 10, nil, nil, nil, intstr.FromInt(1), intstr.FromInt(0))
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	rs := newReplicaSet(r, "foo-895c6c4f9", 1)

	f.expectCreateReplicaSetAction(rs)
	f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))

	patchBytes := filterInformerActions(f.client.Actions())[0].(core.PatchAction).GetPatch()
	assert.Equal(t, expectedPatch, string(patchBytes))

}

func TestCanaryRolloutCreateNewReplicaWithCorrectWeight(t *testing.T) {
	f := newFixture(t)

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r1.Status.CanaryStatus.StableRS = "895c6c4f9"
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
	r1.Status.CanaryStatus.StableRS = "895c6c4f9"
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
	r1.Status.CanaryStatus.StableRS = "895c6c4f9"

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

	assert.Equal(t, expectedRS1, filterInformerActions(f.kubeclient.Actions())[0].(core.UpdateAction).GetObject().(*appsv1.ReplicaSet))
}

func TestCanaryRolloutScaleDownOldRs(t *testing.T) {
	f := newFixture(t)

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r1.Status.CanaryStatus.StableRS = "895c6c4f9"

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

func TestCanaryRolloutIncrementStepIfSetWeightsAreCorrect(t *testing.T) {
	expectedPatch := removeWhiteSpace(`{
	"status":{
		"availableReplicas":10,
		"canaryStatus":{
			"stableRS":"8cdf7bbb4"
		},
		"currentStepIndex":1,
		"observedGeneration":"667bf85f4"
	}
}`)
	f := newFixture(t)

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r1.Status.CanaryStatus.StableRS = "895c6c4f9"

	r2 := bumpVersion(r1)

	r3 := bumpVersion(r2)
	r3.Status.CurrentPodHash = "8cdf7bbb4"
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
	assert.Equal(t, expectedPatch, string(patchBytes))
}

func TestSyncRolloutsSetWaitStartTime(t *testing.T) {
	expectedPatch := removeWhiteSpace(`{
	"status":{
		"canaryStatus":{
			"waitStartTime": "%s"
		},
		"observedGeneration":"7c5dcf976c"
	}
}`)
	expectedPatch = fmt.Sprintf(expectedPatch, metav1.Now().UTC().Format(time.RFC3339))
	f := newFixture(t)

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: int32Ptr(10),
		}, {
			Wait: int32Ptr(10),
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	r1.Status.CanaryStatus.StableRS = "895c6c4f9"

	r2 := bumpVersion(r1)
	r2.Status.CurrentPodHash = "5f79b78d7f"
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
	assert.Equal(t, expectedPatch, string(patchBytes))
}

func TestSyncRolloutWaitAddToQueue(t *testing.T) {
	f := newFixture(t)

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: int32Ptr(10),
		}, {
			Wait: int32Ptr(10),
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	r1.Status.CanaryStatus.StableRS = "895c6c4f9"

	r2 := bumpVersion(r1)
	r2.Status.CurrentPodHash = "5f79b78d7f"
	r2.Status.AvailableReplicas = 10
	r2.Status.ObservedGeneration = "7c5dcf976c"

	now := metav1.Now()
	r2.Status.CanaryStatus.WaitStartTime = &now
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
		}, {
			Wait: int32Ptr(int32(3600)), //1 hour
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	r1.Status.CanaryStatus.StableRS = "895c6c4f9"

	r2 := bumpVersion(r1)
	r2.Status.CurrentPodHash = "5f79b78d7f"
	now := metav1.Now()
	r2.Status.CanaryStatus.WaitStartTime = &now
	r2.Status.ObservedGeneration = "7bc4488755"
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
	expectedPatch := removeWhiteSpace(`{
	"status":{
		"canaryStatus":{
			"waitStartTime": null
		},
		"currentStepIndex":2,
		"observedGeneration":"b64bd68c6"
	}
}`)

	f := newFixture(t)
	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: int32Ptr(10),
		}, {
			Wait: int32Ptr(5),
		}, {
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	r1.Status.CanaryStatus.StableRS = "895c6c4f9"

	r2 := bumpVersion(r1)
	r2.Status.CurrentPodHash = "5f79b78d7f"
	earlier := metav1.Now()
	earlier.Time = earlier.Add(-10 * time.Second)
	r2.Status.CanaryStatus.WaitStartTime = &earlier
	r2.Status.ObservedGeneration = "6dff5cffc5"
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
	assert.Equal(t, expectedPatch, string(patchBytes))
}
