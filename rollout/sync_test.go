package rollout

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	testclient "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"

	metricsmocks "github.com/argoproj/argo-rollouts/controller/metrics/mocks"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"

	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"

	"context"
)

func rs(name string, replicas int, selector map[string]string, timestamp metav1.Time, ownerRef *metav1.OwnerReference) *appsv1.ReplicaSet {
	ownerRefs := []metav1.OwnerReference{}
	if ownerRef != nil {
		ownerRefs = append(ownerRefs, *ownerRef)
	}

	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: timestamp,
			Namespace:         metav1.NamespaceDefault,
			OwnerReferences:   ownerRefs,
			Labels:            selector,
			Annotations:       map[string]string{annotations.DesiredReplicasAnnotation: strconv.Itoa(replicas)},
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: func() *int32 { i := int32(replicas); return &i }(),
			Selector: &metav1.LabelSelector{MatchLabels: selector},
			Template: corev1.PodTemplateSpec{},
		},
	}
}

func TestReconcileRevisionHistoryLimit(t *testing.T) {
	now := timeutil.MetaNow()
	before := metav1.Time{Time: now.Add(-time.Minute)}

	newRS := func(name string) *appsv1.ReplicaSet {
		return &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				CreationTimestamp: before,
			},
			Spec:   appsv1.ReplicaSetSpec{Replicas: int32Ptr(0)},
			Status: appsv1.ReplicaSetStatus{Replicas: int32(0)},
		}
	}

	tests := []struct {
		name                 string
		revisionHistoryLimit *int32
		replicaSets          []*appsv1.ReplicaSet
		expectedDeleted      map[string]bool
	}{
		{
			name:                 "No Revision History Limit",
			revisionHistoryLimit: nil,
			replicaSets: []*appsv1.ReplicaSet{
				newRS("foo1"),
				newRS("foo2"),
				newRS("foo3"),
				newRS("foo4"),
				newRS("foo5"),
				newRS("foo6"),
				newRS("foo7"),
				newRS("foo8"),
				newRS("foo9"),
				newRS("foo10"),
				newRS("foo11"),
			},
			expectedDeleted: map[string]bool{"foo1": true},
		},
		{
			name:                 "Avoid deleting RS with deletion timestamp",
			revisionHistoryLimit: int32Ptr(1),
			replicaSets: []*appsv1.ReplicaSet{{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "foo",
					DeletionTimestamp: &now,
				},
			}},
		},
		// {
		// 	name:                 "Return early on failed replicaset delete attempt.",
		// 	revisionHistoryLimit: int32Ptr(1),
		// },
		{
			name:                 "Delete extra replicasets",
			revisionHistoryLimit: int32Ptr(1),
			replicaSets: []*appsv1.ReplicaSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "foo",
						CreationTimestamp: before,
					},
					Spec: appsv1.ReplicaSetSpec{
						Replicas: int32Ptr(0),
					},
					Status: appsv1.ReplicaSetStatus{
						Replicas: int32(0),
					},
				}, {
					ObjectMeta: metav1.ObjectMeta{
						Name:              "bar",
						CreationTimestamp: now,
					},
					Spec: appsv1.ReplicaSetSpec{
						Replicas: int32Ptr(1),
					},
					Status: appsv1.ReplicaSetStatus{
						Replicas: int32(1),
					},
				},
			},
			expectedDeleted: map[string]bool{"foo": true},
		},
		{
			name:                 "Dont delete scaled replicasets",
			revisionHistoryLimit: int32Ptr(1),
			replicaSets: []*appsv1.ReplicaSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "foo",
						CreationTimestamp: before,
					},
					Spec: appsv1.ReplicaSetSpec{
						Replicas: int32Ptr(1),
					},
					Status: appsv1.ReplicaSetStatus{
						Replicas: int32(1),
					},
				}, {
					ObjectMeta: metav1.ObjectMeta{
						Name:              "bar",
						CreationTimestamp: now,
					},
					Spec: appsv1.ReplicaSetSpec{
						Replicas: int32Ptr(1),
					},
					Status: appsv1.ReplicaSetStatus{
						Replicas: int32(1),
					},
				},
			},
			expectedDeleted: map[string]bool{},
		},
		{
			name: "Do not delete any replicasets",
			replicaSets: []*appsv1.ReplicaSet{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "foo",
						CreationTimestamp: before,
					},
					Spec: appsv1.ReplicaSetSpec{
						Replicas: int32Ptr(1),
					},
					Status: appsv1.ReplicaSetStatus{
						Replicas: int32(1),
					},
				}, {
					ObjectMeta: metav1.ObjectMeta{
						Name:              "bar",
						CreationTimestamp: now,
					},
					Spec: appsv1.ReplicaSetSpec{
						Replicas: int32Ptr(1),
					},
					Status: appsv1.ReplicaSetStatus{
						Replicas: int32(1),
					},
				},
			},
			revisionHistoryLimit: int32Ptr(2),
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			r := newBlueGreenRollout("baz", 1, test.revisionHistoryLimit, "", "")
			fake := fake.Clientset{}
			k8sfake := k8sfake.Clientset{}
			roCtx := &rolloutContext{
				rollout:  r,
				log:      logutil.WithRollout(r),
				olderRSs: test.replicaSets,
				reconcilerBase: reconcilerBase{
					argoprojclientset: &fake,
					kubeclientset:     &k8sfake,
					recorder:          record.NewFakeEventRecorder(),
				},
			}
			err := roCtx.reconcileRevisionHistoryLimit(test.replicaSets)
			assert.Nil(t, err)
			assert.Equal(t, len(test.expectedDeleted), len(k8sfake.Actions()))
			for _, action := range k8sfake.Actions() {
				rsName := action.(testclient.DeleteAction).GetName()
				assert.True(t, test.expectedDeleted[rsName])
			}
		})
	}
}

func TestPersistWorkloadRefGeneration(t *testing.T) {
	replica := int32(1)
	r := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Replicas: &replica,
		},
	}
	fake := fake.Clientset{}
	roCtx := &rolloutContext{
		rollout: r,
		log:     logutil.WithRollout(r),
		reconcilerBase: reconcilerBase{
			argoprojclientset: &fake,
			recorder:          record.NewFakeEventRecorder(),
		},
		pauseContext: &pauseContext{
			rollout: r,
		},
	}

	tests := []struct {
		annotatedRefGeneration string
		currentObserved        string
	}{
		{"1", ""},
		{"2", "1"},
		{"", "1"},
	}

	for _, tc := range tests {
		newStatus := &v1alpha1.RolloutStatus{
			UpdatedReplicas:   int32(1),
			AvailableReplicas: int32(1),
		}

		if tc.annotatedRefGeneration != "" {
			annotations.SetRolloutWorkloadRefGeneration(r, tc.annotatedRefGeneration)
			r.Spec.TemplateResolvedFromRef = true

			newStatus.WorkloadObservedGeneration = tc.currentObserved
		} else {
			r.Spec.TemplateResolvedFromRef = false
			annotations.RemoveRolloutWorkloadRefGeneration(r)
		}
		roCtx.persistRolloutStatus(newStatus)
		assert.Equal(t, tc.annotatedRefGeneration, newStatus.WorkloadObservedGeneration)
	}
}

// TestPersistRolloutStatusDurationMetricEmittedAfterPatch verifies that the rollout
// completion duration metric is not emitted when the status patch recording the
// completion fails (the transition is re-detected and retried on the next reconcile),
// and is emitted exactly once when the patch succeeds.
func TestPersistRolloutStatusDurationMetricEmittedAfterPatch(t *testing.T) {
	startedAt := metav1.NewTime(timeutil.MetaNow().Add(-5 * time.Minute))
	newAbortedRollout := func() *v1alpha1.Rollout {
		return &v1alpha1.Rollout{
			ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: metav1.NamespaceDefault},
			Spec:       v1alpha1.RolloutSpec{Replicas: ptr.To(int32(1))},
			Status: v1alpha1.RolloutStatus{
				CurrentPodHash: "abc123", // non-empty so this is not treated as an initial rollout
				Abort:          true,
				Duration: &v1alpha1.RolloutDurationStatus{
					RolloutStartedAt: &startedAt,
				},
			},
		}
	}
	newCtx := func(r *v1alpha1.Rollout, clientset *fake.Clientset, recorder *metricsmocks.MetricsRecorder) *rolloutContext {
		return &rolloutContext{
			rollout: r,
			log:     logutil.WithRollout(r),
			reconcilerBase: reconcilerBase{
				argoprojclientset: clientset,
				recorder:          record.NewFakeEventRecorder(),
				metricsServer:     recorder,
			},
			pauseContext: &pauseContext{rollout: r},
		}
	}

	t.Run("does not emit when the status patch fails", func(t *testing.T) {
		r := newAbortedRollout()
		clientset := fake.NewSimpleClientset(r)
		clientset.PrependReactor("patch", "rollouts", func(action testclient.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("patch failed")
		})
		recorder := metricsmocks.NewMetricsRecorder(t)
		roCtx := newCtx(r, clientset, recorder)

		newStatus := r.Status.DeepCopy()
		err := roCtx.persistRolloutStatus(newStatus)
		assert.Error(t, err)
		recorder.AssertNotCalled(t, "EmitRolloutDuration", mock.Anything)
	})

	t.Run("emits exactly once when the status patch succeeds", func(t *testing.T) {
		r := newAbortedRollout()
		clientset := fake.NewSimpleClientset(r)
		recorder := metricsmocks.NewMetricsRecorder(t)
		recorder.On("EmitRolloutDuration", mock.Anything).Return().Once()
		roCtx := newCtx(r, clientset, recorder)

		newStatus := r.Status.DeepCopy()
		err := roCtx.persistRolloutStatus(newStatus)
		assert.NoError(t, err)
		recorder.AssertNumberOfCalls(t, "EmitRolloutDuration", 1)
		assert.True(t, newStatus.Duration.IsCompleted())
		assert.Equal(t, v1alpha1.CompletionStatusAborted, newStatus.Duration.GetCompletionStatus())
	})
}

func TestPingPongCanaryPromoteStable(t *testing.T) {
	ro := &v1alpha1.Rollout{}
	ro.Spec.Strategy.Canary = &v1alpha1.CanaryStrategy{PingPong: &v1alpha1.PingPongSpec{}}
	ro.Status.Canary.StablePingPong = v1alpha1.PPPing
	roCtx := &rolloutContext{
		pauseContext: &pauseContext{},
		rollout:      ro,
		reconcilerBase: reconcilerBase{
			recorder: record.NewFakeEventRecorder(),
		},
	}
	newStatus := &v1alpha1.RolloutStatus{
		CurrentPodHash: "2f646bf702",
		StableRS:       "15fb5ffc01",
	}

	// test call
	err := roCtx.promoteStable(newStatus, "reason")

	assert.Nil(t, err)
	assert.Equal(t, v1alpha1.PPPong, newStatus.Canary.StablePingPong)
}

// TestCanaryPromoteFull verifies skip pause, analysis, steps when promote full is set for a canary rollout
func TestCanaryPromoteFull(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	// these steps should be ignored
	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: int32Ptr(10),
		},
		{
			Pause: &v1alpha1.RolloutPause{
				Duration: v1alpha1.DurationFromInt(60),
			},
		},
	}

	at := analysisTemplate("bar")
	r1 := newCanaryRollout("foo", 10, nil, steps, int32Ptr(0), intstr.FromInt(10), intstr.FromInt(0))
	r1.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
				{
					TemplateName: at.Name,
				},
			},
		},
	}

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	r1.Status.StableRS = rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	r2 := bumpVersion(r1)
	r2.Status.PromoteFull = true
	r2.Annotations[annotations.RevisionAnnotation] = "1"
	rs2 := newReplicaSetWithStatus(r2, 1, 0)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2, at)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	createdRS2Index := f.expectCreateReplicaSetAction(rs2) // sync 1: create new ReplicaSet (size 0)
	f.expectUpdateRolloutAction(r2)                        // sync 1: update rollout revision
	f.expectUpdateRolloutStatusAction(r2)                  // sync 1: update rollout conditions
	f.expectGetRolloutAction(r2)                           // re-seed between syncs
	patchedRolloutIndex := f.expectPatchRolloutAction(r2)  // sync 2: patch status
	updatedRS2Index := f.expectUpdateReplicaSetAction(rs2) // sync 2: scale new ReplicaSet to 10
	f.runWithSyncs(getKey(r2, t), 2)

	createdRS2 := f.getCreatedReplicaSet(createdRS2Index)
	assert.Equal(t, int32(0), *createdRS2.Spec.Replicas)
	updatedRS2 := f.getUpdatedReplicaSet(updatedRS2Index)
	assert.Equal(t, int32(10), *updatedRS2.Spec.Replicas) // verify we ignored steps and fully scaled it

	patchedRollout := f.getPatchedRolloutAsObject(patchedRolloutIndex)
	assert.Equal(t, int32(2), *patchedRollout.Status.CurrentStepIndex) // verify we updated to last step
	assert.False(t, patchedRollout.Status.PromoteFull)
}

// TesBlueGreenPromoteFull verifies skip pause, analysis when promote full is set for a blue-green rollout
func TestBlueGreenPromoteFull(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	r1 := newBlueGreenRollout("foo", 10, nil, "active", "preview")
	r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = ptr.To[bool](false)
	r1.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.AnalysisTemplateRef{
			{
				TemplateName: at.Name,
			},
		},
	}
	r1.Spec.Strategy.BlueGreen.PostPromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.AnalysisTemplateRef{
			{
				TemplateName: at.Name,
			},
		},
	}
	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	r1.Status.StableRS = rs1PodHash
	r1.Status.BlueGreen.ActiveSelector = rs1PodHash
	r1.Status.BlueGreen.PreviewSelector = rs1PodHash
	activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}, r1)

	// create a replicaset on the verge of promotion
	r2 := bumpVersion(r1)
	r2.Status.PromoteFull = true
	rs2 := newReplicaSetWithStatus(r2, 10, 10)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	r2.Status.BlueGreen.PreviewSelector = rs2PodHash
	previewSvc := newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}, r2)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2, at)
	f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2, previewSvc, activeSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	f.expectPatchServiceAction(activeSvc, rs2PodHash) // update active to rs2
	patchRolloutIdx := f.expectPatchRolloutAction(r2) // update rollout status
	f.run(getKey(r2, t))

	patchedRollout := f.getPatchedRolloutAsObject(patchRolloutIdx)
	assert.Equal(t, rs2PodHash, patchedRollout.Status.StableRS)
	assert.Equal(t, rs2PodHash, patchedRollout.Status.BlueGreen.ActiveSelector)
	assert.False(t, patchedRollout.Status.PromoteFull)
}

// TestSendStateChangeEvents verifies we emit appropriate events on rollout state changes
func TestSendStateChangeEvents(t *testing.T) {
	now := timeutil.MetaNow()
	tests := []struct {
		prevStatus           v1alpha1.RolloutStatus
		newStatus            v1alpha1.RolloutStatus
		expectedEventReasons []string
	}{
		{
			prevStatus: v1alpha1.RolloutStatus{
				PauseConditions: []v1alpha1.PauseCondition{
					{Reason: v1alpha1.PauseReasonBlueGreenPause, StartTime: now},
				},
			},
			newStatus: v1alpha1.RolloutStatus{
				PauseConditions: nil,
			},
			expectedEventReasons: []string{conditions.RolloutResumedReason},
		},
		{
			prevStatus: v1alpha1.RolloutStatus{
				PauseConditions: nil,
			},
			newStatus: v1alpha1.RolloutStatus{
				PauseConditions: []v1alpha1.PauseCondition{
					{Reason: v1alpha1.PauseReasonBlueGreenPause, StartTime: now},
				},
			},
			expectedEventReasons: []string{conditions.RolloutPausedReason},
		},
		{
			prevStatus: v1alpha1.RolloutStatus{
				PauseConditions: []v1alpha1.PauseCondition{
					{Reason: v1alpha1.PauseReasonBlueGreenPause, StartTime: now},
				},
			},
			newStatus: v1alpha1.RolloutStatus{
				PauseConditions: nil,
				AbortedAt:       &now,
			},
			expectedEventReasons: nil,
		},
	}
	for _, test := range tests {
		roCtx := &rolloutContext{}
		roCtx.rollout = &v1alpha1.Rollout{ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		}}
		recorder := record.NewFakeEventRecorder()
		roCtx.recorder = recorder
		roCtx.sendStateChangeEvents(&test.prevStatus, &test.newStatus)
		assert.Equal(t, test.expectedEventReasons, recorder.Events())
	}
}

// TestRollbackWindow verifies the rollback window conditions
func TestRollbackWindow(t *testing.T) {
	now := timeutil.MetaNow()

	replicaSets := []*appsv1.ReplicaSet{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "foo-4",
				CreationTimestamp: metav1.Time{Time: now.Add(-time.Minute * 5)},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "foo-3",
				CreationTimestamp: metav1.Time{Time: now.Add(-time.Minute * 4)},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "foo-2",
				CreationTimestamp: metav1.Time{Time: now.Add(-time.Minute * 3)},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "foo-1",
				CreationTimestamp: metav1.Time{Time: now.Add(-time.Minute * 2)},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "foo-experiment",
				CreationTimestamp: metav1.Time{Time: now.Add(-time.Minute)},
				Annotations: map[string]string{
					v1alpha1.ExperimentNameAnnotationKey: "my-experiment",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "foo",
				CreationTimestamp: now,
			},
		},
	}
	testRuns := []struct {
		stableRS       *appsv1.ReplicaSet
		newRS          *appsv1.ReplicaSet
		revisionWindow int32
		expectedWithin bool
	}{
		{
			replicaSets[0], nil, 1, false,
		},
		{
			replicaSets[0], replicaSets[1], 1, false,
		},
		{
			replicaSets[1], replicaSets[0], 1, true,
		},
		{
			replicaSets[2], replicaSets[0], 2, true,
		},
		{
			replicaSets[3], replicaSets[0], 2, false,
		},
		// from 5->3 the window is 1 because experiments are excluded
		{
			replicaSets[5], replicaSets[3], 1, true,
		},
	}
	for _, test := range testRuns {
		ctx := &rolloutContext{
			allRSs:   replicaSets,
			newRS:    test.newRS,
			stableRS: test.stableRS,
		}

		ctx.rollout = &v1alpha1.Rollout{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: "default",
			},
			Spec: v1alpha1.RolloutSpec{
				RollbackWindow: &v1alpha1.RollbackWindowSpec{
					Revisions: test.revisionWindow,
				},
			},
		}
		ctx.log = logutil.WithRollout(ctx.rollout)
		if test.expectedWithin {
			assert.True(t, ctx.isRollbackWithinWindow())
		} else {
			assert.False(t, ctx.isRollbackWithinWindow())
		}
	}
}

func Test_shouldFullPromote(t *testing.T) {
	now := timeutil.MetaNow()
	before1m := metav1.Time{Time: now.Add(-time.Minute)}
	replicaSets := []*appsv1.ReplicaSet{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "foo",
				CreationTimestamp: now,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "bar",
				CreationTimestamp: before1m,
			},
			Status: v1.ReplicaSetStatus{
				AvailableReplicas: int32(1),
			},
		},
	}
	// test canary
	ctx := &rolloutContext{
		allRSs:   replicaSets,
		stableRS: replicaSets[0],
		newRS:    replicaSets[1],
		rollout: &v1alpha1.Rollout{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: "default",
			},
			Spec: v1alpha1.RolloutSpec{
				RollbackWindow: &v1alpha1.RollbackWindowSpec{
					Revisions: 1,
				},
				Strategy: v1alpha1.RolloutStrategy{
					Canary: &v1alpha1.CanaryStrategy{},
				},
			},
		},
	}
	ctx.pauseContext = &pauseContext{rollout: ctx.rollout}
	ctx.log = logutil.WithRollout(ctx.rollout)
	newStatus := v1alpha1.RolloutStatus{}

	result := ctx.shouldFullPromote(newStatus)
	assert.Equal(t, result, "Rollback within window")

	// test bluegreen
	podHash := "xxx"
	ctx.rollout.Spec.Strategy.Canary = nil
	ctx.rollout.Spec.Strategy.BlueGreen = &v1alpha1.BlueGreenStrategy{}
	newStatus.BlueGreen = v1alpha1.BlueGreenStatus{ActiveSelector: podHash}
	newStatus.CurrentPodHash = podHash

	result = ctx.shouldFullPromote(newStatus)
	assert.Equal(t, result, "Rollback within window")
}

func TestShouldFullPromoteWithReplicaProgressThreshold(t *testing.T) {
	tests := []struct {
		name                     string
		availableReplicas        int32
		threshold                *v1alpha1.ReplicaProgressThreshold
		expectedPromotionMessage string
		description              string
	}{
		{
			name:              "threshold met - 90% available with 90% threshold",
			availableReplicas: 9,
			threshold: &v1alpha1.ReplicaProgressThreshold{
				Type:  v1alpha1.ProgressTypePercentage,
				Value: 90,
			},
			expectedPromotionMessage: "Completed all 1 canary steps",
			description:              "Should promote when threshold is met (9/10 = 90%)",
		},
		{
			name:              "threshold not met - 80% available with 90% threshold",
			availableReplicas: 8,
			threshold: &v1alpha1.ReplicaProgressThreshold{
				Type:  v1alpha1.ProgressTypePercentage,
				Value: 90,
			},
			expectedPromotionMessage: "",
			description:              "Should not promote when threshold is not met (8/10 = 80% < 90%)",
		},
		{
			name:                     "nil threshold - requires 100% availability",
			availableReplicas:        9,
			threshold:                nil,
			expectedPromotionMessage: "",
			description:              "Should not promote with nil threshold, requiring 100% availability (9/10 = 90%)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create stable RS and new RS
			stableRS := &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "stable",
					Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "stablehash"},
				},
				Spec: appsv1.ReplicaSetSpec{
					Replicas: int32Ptr(10),
				},
				Status: v1.ReplicaSetStatus{
					AvailableReplicas: int32(10),
				},
			}

			newRS := &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "new",
					Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "newhash"},
				},
				Spec: appsv1.ReplicaSetSpec{
					Replicas: int32Ptr(10),
				},
				Status: v1.ReplicaSetStatus{
					AvailableReplicas: tt.availableReplicas,
				},
			}

			replicaSets := []*appsv1.ReplicaSet{stableRS, newRS}

			ctx := &rolloutContext{
				allRSs:   replicaSets,
				stableRS: stableRS,
				newRS:    newRS,
				rollout: &v1alpha1.Rollout{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "foo",
						Namespace: "default",
					},
					Spec: v1alpha1.RolloutSpec{
						Replicas: int32Ptr(10),
						Strategy: v1alpha1.RolloutStrategy{
							Canary: &v1alpha1.CanaryStrategy{
								Steps: []v1alpha1.CanaryStep{
									{SetWeight: int32Ptr(100)},
								},
								ReplicaProgressThreshold: tt.threshold,
							},
						},
					},
				},
			}
			ctx.pauseContext = &pauseContext{rollout: ctx.rollout}
			ctx.log = logutil.WithRollout(ctx.rollout)

			// Set to last step to trigger full promotion check
			ctx.rollout.Status.CurrentStepIndex = int32Ptr(1)
			ctx.rollout.Status.StableRS = "stablehash"
			newStatus := v1alpha1.RolloutStatus{}

			result := ctx.shouldFullPromote(newStatus)

			assert.Equal(t, tt.expectedPromotionMessage, result, tt.description)
		})
	}
}

// TestShouldFullPromoteCanaryAvailableVsDesired verifies that promotion is allowed when canary
// has >= desired replicas (e.g. after HPA scales spec.replicas down), and blocked only when
// canary has fewer available than desired.
func TestShouldFullPromoteCanaryAvailableVsDesired(t *testing.T) {
	tests := []struct {
		name                     string
		rolloutReplicas          int32
		canaryAvailableReplicas  int32
		expectedPromotionMessage string
	}{
		{
			name:                     "block when canary has fewer available than desired",
			rolloutReplicas:          10,
			canaryAvailableReplicas:  5,
			expectedPromotionMessage: "",
		},
		{
			name:                     "promote when canary available equals desired",
			rolloutReplicas:          10,
			canaryAvailableReplicas:  10,
			expectedPromotionMessage: "Completed all 1 canary steps",
		},
		{
			name:                     "promote when canary has more available than desired (e.g. HPA scaled down)",
			rolloutReplicas:          1,
			canaryAvailableReplicas:  2,
			expectedPromotionMessage: "Completed all 1 canary steps",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stableRS := &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{Name: "stable", Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "stablehash"}},
				Spec:       appsv1.ReplicaSetSpec{Replicas: &tt.rolloutReplicas},
				Status:     v1.ReplicaSetStatus{AvailableReplicas: tt.rolloutReplicas},
			}
			newRS := &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{Name: "new", Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "newhash"}},
				Spec:       appsv1.ReplicaSetSpec{Replicas: &tt.rolloutReplicas},
				Status:     v1.ReplicaSetStatus{AvailableReplicas: tt.canaryAvailableReplicas},
			}
			replicaSets := []*appsv1.ReplicaSet{stableRS, newRS}

			ctx := &rolloutContext{
				allRSs:   replicaSets,
				stableRS: stableRS,
				newRS:    newRS,
				rollout: &v1alpha1.Rollout{
					ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "default"},
					Spec: v1alpha1.RolloutSpec{
						Replicas: int32Ptr(tt.rolloutReplicas),
						Strategy: v1alpha1.RolloutStrategy{
							Canary: &v1alpha1.CanaryStrategy{
								Steps: []v1alpha1.CanaryStep{{SetWeight: int32Ptr(100)}},
							},
						},
					},
				},
			}
			ctx.pauseContext = &pauseContext{rollout: ctx.rollout}
			ctx.log = logutil.WithRollout(ctx.rollout)
			ctx.rollout.Status.CurrentStepIndex = int32Ptr(1)
			ctx.rollout.Status.StableRS = "stablehash"

			result := ctx.shouldFullPromote(v1alpha1.RolloutStatus{})
			assert.Equal(t, tt.expectedPromotionMessage, result)
		})
	}
}

func TestScaleDownDeploymentOnSuccess(t *testing.T) {
	ctx := createScaleDownRolloutContext(v1alpha1.ScaleDownOnSuccess, 5, true, nil)
	newStatus := &v1alpha1.RolloutStatus{
		CurrentPodHash: "2f646bf702",
		StableRS:       "15fb5ffc01",
	}
	err := ctx.promoteStable(newStatus, "reason")

	assert.Nil(t, err)
	k8sfakeClient := ctx.kubeclientset.(*k8sfake.Clientset)
	updatedDeployment, err := k8sfakeClient.AppsV1().Deployments("default").Get(context.TODO(), "workload-test", metav1.GetOptions{})
	assert.Nil(t, err)
	assert.Equal(t, int32(0), *updatedDeployment.Spec.Replicas)

	// test scale deployment error
	ctx = createScaleDownRolloutContext(v1alpha1.ScaleDownOnSuccess, 5, false, nil)
	newStatus = &v1alpha1.RolloutStatus{
		CurrentPodHash: "2f646bf702",
		StableRS:       "15fb5ffc01",
	}
	err = ctx.promoteStable(newStatus, "reason")

	assert.NotNil(t, err)
}

// This tests validates that when there are old replicasets that have miss matched desired-count annotations aka they do
// not match the spec.replicas field. When this happens we get stuck in a reconcile loop of only trying to scale replicasets
// only because of a supposed scaling event. This test validates that we do not get stuck in this loop by checking that status.replicas
// and status.readyreplicas are updated because those will not be updated in that loop.
func TestIsScalingEventMissMatchedDesiredOldReplicas(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	// these steps should be ignored
	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: int32Ptr(10),
		},
		{
			Pause: &v1alpha1.RolloutPause{
				Duration: v1alpha1.DurationFromInt(60),
			},
		},
	}

	r0 := newCanaryRollout("foo", 10, int32Ptr(4), steps, int32Ptr(0), intstr.FromInt(10), intstr.FromInt(0))
	r0.Annotations[annotations.RevisionAnnotation] = "1"
	oldRs := newReplicaSetWithStatus(r0, 3, 3)
	oldRs.Annotations[annotations.DesiredReplicasAnnotation] = "2"

	r1 := bumpVersion(r0)

	// Desired rs will be created during reconcile
	stableRs := newReplicaSetWithStatus(r1, 10, 10)
	r1.Status.StableRS = stableRs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	r2 := bumpVersion(r1)
	r2.Annotations[annotations.RevisionAnnotation] = "2"

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.kubeobjects = append(f.kubeobjects, oldRs, stableRs)
	f.replicaSetLister = append(f.replicaSetLister, oldRs, stableRs)

	createdRS2Index := f.expectCreateReplicaSetAction(stableRs) // sync 1: create RS
	f.expectUpdateRolloutAction(r2)                             // sync 1: update rollout revision
	f.expectUpdateRolloutStatusAction(r2)                       // sync 1: update status
	f.expectGetRolloutAction(r2)                                // re-seed between syncs
	updatedROIndex := f.expectPatchRolloutAction(r2)            // sync 2: patch status
	updatedRS2Index := f.expectUpdateReplicaSetAction(stableRs) // sync 2: scale RS
	f.runWithSyncs(getKey(r2, t), 2)

	createdRS2 := f.getCreatedReplicaSet(createdRS2Index)
	assert.Equal(t, int32(0), *createdRS2.Spec.Replicas)
	updatedRS2 := f.getUpdatedReplicaSet(updatedRS2Index)
	assert.Equal(t, int32(1), *updatedRS2.Spec.Replicas)

	updateRO := f.getPatchedRollout(updatedROIndex)

	//Will only contain status
	roStatus := v1alpha1.Rollout{}
	err := json.Unmarshal([]byte(updateRO), &roStatus)
	assert.Nil(t, err)

	// We have two ReplicaSets, one with 3 pods and one with 10
	assert.Equal(t, int32(13), roStatus.Status.Replicas)
	assert.Equal(t, int32(13), roStatus.Status.ReadyReplicas)
	assert.Equal(t, int32(0), roStatus.Status.UpdatedReplicas)
}

// scaleConflictFixture holds a rolloutContext whose kube client contains the live ReplicaSet, and the older copy of
// that ReplicaSet the controller would have read from its informer.
type scaleConflictFixture struct {
	roCtx      *rolloutContext
	kubeclient *k8sfake.Clientset
	rollout    *v1alpha1.Rollout
	// cached is the copy the controller read at the start of the reconcile.
	cached *appsv1.ReplicaSet
}

// newScaleConflictFixture creates a 10-replica canary Rollout and its ReplicaSet. mutateCached changes the
// ReplicaSet before the controller "reads" it, mutateLive changes the one in the API afterwards.
func newScaleConflictFixture(t *testing.T, mutateCached func(*appsv1.ReplicaSet), mutateLive func(*appsv1.ReplicaSet)) *scaleConflictFixture {
	t.Helper()
	steps := []v1alpha1.CanaryStep{{SetWeight: ptr.To[int32](10)}}
	ro := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt32(1), intstr.FromInt32(0))

	cached := newReplicaSetWithStatus(ro, 10, 10)
	cached.Generation = 3
	cached.ResourceVersion = "100"
	if mutateCached != nil {
		mutateCached(cached)
	}

	// By default the live object differs from the cached one only in what a status update changes.
	live := cached.DeepCopy()
	live.ResourceVersion = "250"
	live.Status.ReadyReplicas = 7
	live.Status.AvailableReplicas = 7
	if mutateLive != nil {
		mutateLive(live)
	}

	kubeclient := k8sfake.NewSimpleClientset(live)
	roCtx := &rolloutContext{
		log:     logutil.WithRollout(ro),
		rollout: ro,
		reconcilerBase: reconcilerBase{
			kubeclientset: kubeclient,
			recorder:      record.NewFakeEventRecorder(),
		},
		pauseContext: &pauseContext{rollout: ro},
	}
	return &scaleConflictFixture{roCtx: roCtx, kubeclient: kubeclient, rollout: ro, cached: cached}
}

// failReplicaSetRequests makes every ReplicaSet request with the given verb fail with the error newErr returns.
func (f *scaleConflictFixture) failReplicaSetRequests(verb string, newErr func(name string) error) {
	f.kubeclient.PrependReactor(verb, "replicasets", func(action testclient.Action) (bool, runtime.Object, error) {
		return true, nil, newErr(f.cached.Name)
	})
}

func replicaSetConflict(name string) error {
	return k8serrors.NewConflict(appsv1.Resource("replicasets"), name, fmt.Errorf("the object has been modified; please apply your changes to the latest version and try again"))
}

func replicaSetForbidden(name string) error {
	return k8serrors.NewForbidden(appsv1.Resource("replicasets"), name, fmt.Errorf("not allowed"))
}

// replicaSetVerbs returns the verbs of the ReplicaSet requests that were made, in order.
func (f *scaleConflictFixture) replicaSetVerbs() []string {
	var verbs []string
	for _, action := range f.kubeclient.Actions() {
		if action.GetResource().Resource == "replicasets" {
			verbs = append(verbs, action.GetVerb())
		}
	}
	return verbs
}

// replicaSetPatches returns the ReplicaSet patch requests that were made.
func (f *scaleConflictFixture) replicaSetPatches() []testclient.PatchAction {
	var patches []testclient.PatchAction
	for _, action := range f.kubeclient.Actions() {
		if patch, ok := action.(testclient.PatchAction); ok && action.GetResource().Resource == "replicasets" {
			patches = append(patches, patch)
		}
	}
	return patches
}

func (f *scaleConflictFixture) liveReplicaSet(t *testing.T) *appsv1.ReplicaSet {
	t.Helper()
	live, err := f.kubeclient.AppsV1().ReplicaSets(f.cached.Namespace).Get(context.Background(), f.cached.Name, metav1.GetOptions{})
	require.NoError(t, err)
	return live
}

func (f *scaleConflictFixture) events() []string {
	return f.roCtx.recorder.(*record.FakeEventRecorder).Events()
}

// A conflict that was caused by status updates is resolved by patching the scale.
func TestScaleReplicaSetPatchesScaleAfterStatusConflict(t *testing.T) {
	f := newScaleConflictFixture(t, func(rs *appsv1.ReplicaSet) {
		// An outdated desired-replicas annotation, to show that the patch corrects it.
		rs.Annotations[annotations.DesiredReplicasAnnotation] = "20"
	}, nil)
	f.failReplicaSetRequests("update", replicaSetConflict)

	scaled, updated, err := f.roCtx.scaleReplicaSet(f.cached, 4, f.rollout, "down")

	require.NoError(t, err)
	assert.True(t, scaled)
	assert.Equal(t, []string{"update", "get", "patch"}, f.replicaSetVerbs())

	patches := f.replicaSetPatches()
	require.Len(t, patches, 1)
	assert.Equal(t, types.MergePatchType, patches[0].GetPatchType())
	assert.Empty(t, patches[0].GetSubresource())
	assert.JSONEq(t, fmt.Sprintf(
		`{"metadata":{"annotations":{"rollout.argoproj.io/desired-replicas":"10"},"uid":"%s"},"spec":{"replicas":4}}`,
		f.cached.UID), string(patches[0].GetPatch()))

	live := f.liveReplicaSet(t)
	assert.Equal(t, int32(4), *live.Spec.Replicas)
	assert.Equal(t, "10", live.Annotations[annotations.DesiredReplicasAnnotation])
	// Everything the patch does not name is left as it was in the API.
	assert.Equal(t, int32(7), live.Status.ReadyReplicas)
	assert.Equal(t, f.cached.Annotations[annotations.RevisionAnnotation], live.Annotations[annotations.RevisionAnnotation])
	assert.Equal(t, f.cached.Spec.Template, live.Spec.Template)

	// The caller gets the patched object, and the cached copy is not modified.
	assert.Equal(t, int32(4), *updated.Spec.Replicas)
	assert.Equal(t, int32(7), updated.Status.ReadyReplicas)
	assert.Equal(t, int32(10), *f.cached.Spec.Replicas)
	assert.Equal(t, "20", f.cached.Annotations[annotations.DesiredReplicasAnnotation])

	assert.Contains(t, f.events(), conditions.ScalingReplicaSetReason)
}

// A conflict when only the desired-replicas annotation has to change is patched too.
func TestScaleReplicaSetPatchesAnnotationOnlyChangeAfterConflict(t *testing.T) {
	f := newScaleConflictFixture(t, func(rs *appsv1.ReplicaSet) {
		rs.Annotations[annotations.DesiredReplicasAnnotation] = "20"
	}, nil)
	f.failReplicaSetRequests("update", replicaSetConflict)

	scaled, _, err := f.roCtx.scaleReplicaSet(f.cached, 10, f.rollout, "down")

	require.NoError(t, err)
	assert.False(t, scaled, "the replica count did not change")
	assert.Equal(t, []string{"update", "get", "patch"}, f.replicaSetVerbs())
	live := f.liveReplicaSet(t)
	assert.Equal(t, int32(10), *live.Spec.Replicas)
	assert.Equal(t, "10", live.Annotations[annotations.DesiredReplicasAnnotation])
	assert.Empty(t, f.events())
}

// If the spec changed after the controller read the ReplicaSet, the scale it computed may be wrong, so the conflict
// is returned and nothing is patched.
func TestScaleReplicaSetDoesNotPatchWhenSpecChanged(t *testing.T) {
	tests := []struct {
		name       string
		mutateLive func(*appsv1.ReplicaSet)
	}{
		{
			name: "generation changed",
			mutateLive: func(rs *appsv1.ReplicaSet) {
				rs.Generation = 4
				rs.Spec.Replicas = ptr.To[int32](8)
			},
		},
		{
			name: "deleted and recreated with the same name",
			mutateLive: func(rs *appsv1.ReplicaSet) {
				rs.UID = types.UID("a-different-uid")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newScaleConflictFixture(t, nil, test.mutateLive)
			before := f.liveReplicaSet(t)
			f.kubeclient.ClearActions()
			f.failReplicaSetRequests("update", replicaSetConflict)

			scaled, _, err := f.roCtx.scaleReplicaSet(f.cached, 4, f.rollout, "down")

			require.Error(t, err)
			assert.True(t, k8serrors.IsConflict(err), "the original conflict is returned: %v", err)
			assert.False(t, scaled)
			assert.Equal(t, []string{"update", "get"}, f.replicaSetVerbs())
			assert.Equal(t, before, f.liveReplicaSet(t))
			assert.Empty(t, f.events())
		})
	}
}

// If the ReplicaSet no longer exists there is nothing to patch.
func TestScaleReplicaSetDoesNotPatchWhenReplicaSetIsGone(t *testing.T) {
	f := newScaleConflictFixture(t, nil, nil)
	require.NoError(t, f.kubeclient.AppsV1().ReplicaSets(f.cached.Namespace).Delete(context.Background(), f.cached.Name, metav1.DeleteOptions{}))
	f.kubeclient.ClearActions()
	f.failReplicaSetRequests("update", replicaSetConflict)

	scaled, _, err := f.roCtx.scaleReplicaSet(f.cached, 4, f.rollout, "down")

	require.Error(t, err)
	assert.True(t, k8serrors.IsNotFound(err), "got: %v", err)
	assert.False(t, scaled)
	assert.Equal(t, []string{"update", "get"}, f.replicaSetVerbs())
}

// If the patch is rejected, its error is returned and no scaling event is recorded.
func TestScaleReplicaSetReturnsPatchError(t *testing.T) {
	f := newScaleConflictFixture(t, nil, nil)
	f.failReplicaSetRequests("update", replicaSetConflict)
	f.failReplicaSetRequests("patch", replicaSetForbidden)

	scaled, _, err := f.roCtx.scaleReplicaSet(f.cached, 4, f.rollout, "down")

	require.Error(t, err)
	assert.True(t, k8serrors.IsForbidden(err), "got: %v", err)
	assert.False(t, scaled)
	assert.Equal(t, []string{"update", "get", "patch"}, f.replicaSetVerbs())
	assert.Equal(t, int32(10), *f.liveReplicaSet(t).Spec.Replicas)
	assert.Empty(t, f.events())
}

// Update errors other than a conflict are returned without reading or patching the ReplicaSet.
func TestScaleReplicaSetReturnsOtherUpdateErrors(t *testing.T) {
	f := newScaleConflictFixture(t, nil, nil)
	f.failReplicaSetRequests("update", replicaSetForbidden)

	scaled, _, err := f.roCtx.scaleReplicaSet(f.cached, 4, f.rollout, "down")

	require.Error(t, err)
	assert.True(t, k8serrors.IsForbidden(err), "got: %v", err)
	assert.False(t, scaled)
	assert.Equal(t, []string{"update"}, f.replicaSetVerbs())
	assert.Equal(t, int32(10), *f.liveReplicaSet(t).Spec.Replicas)
}

// Without a conflict the ReplicaSet is scaled with a single Update.
func TestScaleReplicaSetUpdatesWithoutConflict(t *testing.T) {
	f := newScaleConflictFixture(t, nil, nil)

	scaled, updated, err := f.roCtx.scaleReplicaSet(f.cached, 4, f.rollout, "down")

	require.NoError(t, err)
	assert.True(t, scaled)
	assert.Equal(t, []string{"update"}, f.replicaSetVerbs())
	assert.Equal(t, int32(4), *updated.Spec.Replicas)
	assert.Equal(t, int32(4), *f.liveReplicaSet(t).Spec.Replicas)
}

// Scaling to zero removes the scale-down-deadline annotation, as the Update does.
func TestScaleReplicaSetPatchRemovesScaleDownDeadlineOnFullScaleDown(t *testing.T) {
	deadline := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	f := newScaleConflictFixture(t, func(rs *appsv1.ReplicaSet) {
		rs.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = deadline
	}, nil)
	f.failReplicaSetRequests("update", replicaSetConflict)

	scaled, _, err := f.roCtx.scaleReplicaSet(f.cached, 0, f.rollout, "down")

	require.NoError(t, err)
	assert.True(t, scaled)
	patches := f.replicaSetPatches()
	require.Len(t, patches, 1)
	assert.JSONEq(t, fmt.Sprintf(
		`{"metadata":{"annotations":{"rollout.argoproj.io/desired-replicas":"10","scale-down-deadline":null},"uid":"%s"},"spec":{"replicas":0}}`,
		f.cached.UID), string(patches[0].GetPatch()))

	live := f.liveReplicaSet(t)
	assert.Equal(t, int32(0), *live.Spec.Replicas)
	assert.NotContains(t, live.Annotations, v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey)
	assert.Contains(t, live.Annotations, annotations.RevisionAnnotation)
}

// An aborted Rollout that delays the scale-down of its canary keeps the deadline when it finally scales the canary
// to zero. The Update keeps it, so the patch must too.
func TestScaleReplicaSetPatchKeepsScaleDownDeadlineWhenAbortDelaysScaleDown(t *testing.T) {
	deadline := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	f := newScaleConflictFixture(t, func(rs *appsv1.ReplicaSet) {
		rs.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = deadline
	}, nil)
	f.rollout.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
	f.rollout.Status.Abort = true
	f.roCtx.stableRS = f.cached
	require.True(t, f.roCtx.shouldDelayScaleDownOnAbort())
	f.failReplicaSetRequests("update", replicaSetConflict)

	scaled, _, err := f.roCtx.scaleReplicaSet(f.cached, 0, f.rollout, "down")

	require.NoError(t, err)
	assert.True(t, scaled)
	patches := f.replicaSetPatches()
	require.Len(t, patches, 1)
	assert.NotContains(t, string(patches[0].GetPatch()), v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey)

	live := f.liveReplicaSet(t)
	assert.Equal(t, int32(0), *live.Spec.Replicas)
	assert.Equal(t, deadline, live.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey])
}

func TestReplicaSetScalePatch(t *testing.T) {
	tests := []struct {
		name                    string
		uid                     types.UID
		removeScaleDownDeadline bool
		want                    string
	}{
		{
			name: "scale",
			uid:  "abc",
			want: `{"metadata":{"annotations":{"rollout.argoproj.io/desired-replicas":"100"},"uid":"abc"},"spec":{"replicas":90}}`,
		},
		{
			name:                    "scale and remove the scale-down deadline",
			uid:                     "abc",
			removeScaleDownDeadline: true,
			want:                    `{"metadata":{"annotations":{"rollout.argoproj.io/desired-replicas":"100","scale-down-deadline":null},"uid":"abc"},"spec":{"replicas":90}}`,
		},
		{
			name: "no uid precondition when the uid is unknown",
			want: `{"metadata":{"annotations":{"rollout.argoproj.io/desired-replicas":"100"}},"spec":{"replicas":90}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			patch, err := replicaSetScalePatch(test.uid, 90, 100, test.removeScaleDownDeadline)
			require.NoError(t, err)
			// Compared as exact bytes: the patch must contain nothing else, in particular no resourceVersion.
			assert.Equal(t, test.want, string(patch))
		})
	}
}

// A whole reconcile: a Rollout that was scaled from 10 to 2 replicas, and whose ReplicaSet Update is rejected with a
// conflict, still scales its ReplicaSet.
func TestSyncRolloutPatchesScaleAfterUpdateConflict(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{SetWeight: ptr.To[int32](10)},
		{Pause: &v1alpha1.RolloutPause{Duration: v1alpha1.DurationFromInt(10)}},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt32(1), intstr.FromInt32(0))
	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	r1.Spec.Replicas = ptr.To[int32](2)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)
	f.rolloutLister = append(f.rolloutLister, r1)
	f.objects = append(f.objects, r1)

	f.expectPatchRolloutAction(r1)
	f.expectUpdateReplicaSetAction(rs1) // the scale, rejected with a conflict
	f.expectGetReplicaSetAction(rs1)    // the live ReplicaSet is read
	patchIndex := f.expectPatchReplicaSetAction(rs1)

	key := fmt.Sprintf("%s/%s", r1.Namespace, r1.Name)
	c, i, k8sI := f.newController(func() time.Duration { return 30 * time.Minute })
	f.kubeclient.PrependReactor("update", "replicasets", func(action testclient.Action) (bool, runtime.Object, error) {
		return true, nil, replicaSetConflict(rs1.Name)
	})

	f.runController(key, true, false, c, i, k8sI)

	patched := f.getPatchedReplicaSet(patchIndex)
	assert.Equal(t, int32(2), *patched.Spec.Replicas)
	assert.Equal(t, "2", patched.Annotations[annotations.DesiredReplicasAnnotation])
	live, err := f.kubeclient.AppsV1().ReplicaSets(rs1.Namespace).Get(context.Background(), rs1.Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, int32(2), *live.Spec.Replicas)
}

// The tests below cover the patch that replaces an Update of a ReplicaSet's revision or ephemeral metadata after a
// conflict. See patchReplicaSetAfterConflict.

var (
	canaryRole = &v1alpha1.PodTemplateMetadata{Labels: map[string]string{"role": "canary"}}
	stableRole = &v1alpha1.PodTemplateMetadata{Labels: map[string]string{"role": "stable"}}
)

// withEphemeralMetadata returns the ReplicaSet as the controller leaves it after it has
// applied the metadata.
func withEphemeralMetadata(rs *appsv1.ReplicaSet, metadata *v1alpha1.PodTemplateMetadata) *appsv1.ReplicaSet {
	rs, _ = replicasetutil.SyncReplicaSetEphemeralPodMetadata(rs, metadata)
	return rs
}

// replicaSetPatchFixture holds a rolloutContext whose kube client contains the "live"
// ReplicaSet, and the stale copy the controller would have read from its informer.
type replicaSetPatchFixture struct {
	roCtx      *rolloutContext
	kubeclient *k8sfake.Clientset
	rollout    *v1alpha1.Rollout
	cached     *appsv1.ReplicaSet
}

// newReplicaSetPatchFixture creates a 10-replica canary Rollout with canary and stable
// metadata, and its ReplicaSet. The live ReplicaSet differs from the cached one in what
// a status write changes, and in whatever mutateLive changes.
func newReplicaSetPatchFixture(t *testing.T, mutateCached, mutateLive func(*appsv1.ReplicaSet)) *replicaSetPatchFixture {
	t.Helper()
	ro := newCanaryRollout("foo", 10, nil, []v1alpha1.CanaryStep{{SetWeight: ptr.To[int32](10)}}, ptr.To[int32](0), intstr.FromInt32(1), intstr.FromInt32(0))
	ro.Spec.Strategy.Canary.CanaryMetadata = canaryRole
	ro.Spec.Strategy.Canary.StableMetadata = stableRole

	cached := newReplicaSetWithStatus(ro, 10, 10)
	cached.Generation = 3
	cached.ResourceVersion = "100"
	cached.Annotations["someone-elses/annotation"] = "kept"
	if mutateCached != nil {
		mutateCached(cached)
	}
	live := cached.DeepCopy()
	live.ResourceVersion = "250"
	live.Status.ReadyReplicas = 7
	live.Status.AvailableReplicas = 7
	if mutateLive != nil {
		mutateLive(live)
	}

	kubeclient := k8sfake.NewSimpleClientset(live)
	roCtx := &rolloutContext{
		log:     logutil.WithRollout(ro),
		rollout: ro,
		reconcilerBase: reconcilerBase{
			kubeclientset:               kubeclient,
			argoprojclientset:           fake.NewSimpleClientset(ro),
			recorder:                    record.NewFakeEventRecorder(),
			ephemeralMetadataThreads:    DefaultEphemeralMetadataThreads,
			ephemeralMetadataPodRetries: DefaultEphemeralMetadataPodRetries,
		},
		pauseContext: &pauseContext{rollout: ro},
	}
	return &replicaSetPatchFixture{roCtx: roCtx, kubeclient: kubeclient, rollout: ro, cached: cached}
}

// conflictOnUpdate makes every Update of a ReplicaSet fail with a conflict.
func (f *replicaSetPatchFixture) conflictOnUpdate() {
	f.kubeclient.PrependReactor("update", "replicasets", func(action testclient.Action) (bool, runtime.Object, error) {
		rs := action.(testclient.UpdateAction).GetObject().(*appsv1.ReplicaSet)
		return true, nil, replicaSetConflict(rs.Name)
	})
}

// replicaSetVerbs returns the verbs of the requests made for ReplicaSets, in order.
func (f *replicaSetPatchFixture) replicaSetVerbs() []string {
	var verbs []string
	for _, action := range f.kubeclient.Actions() {
		if action.GetResource().Resource == "replicasets" {
			verbs = append(verbs, action.GetVerb())
		}
	}
	return verbs
}

// patch returns the one ReplicaSet patch that was sent.
func (f *replicaSetPatchFixture) patch(t *testing.T) string {
	t.Helper()
	var patches []testclient.PatchAction
	for _, action := range f.kubeclient.Actions() {
		if patch, ok := action.(testclient.PatchAction); ok && action.GetResource().Resource == "replicasets" {
			patches = append(patches, patch)
		}
	}
	require.Len(t, patches, 1)
	assert.Equal(t, types.MergePatchType, patches[0].GetPatchType())
	assert.Empty(t, patches[0].GetSubresource())
	return string(patches[0].GetPatch())
}

func (f *replicaSetPatchFixture) live(t *testing.T) *appsv1.ReplicaSet {
	t.Helper()
	rs, err := f.kubeclient.AppsV1().ReplicaSets(f.cached.Namespace).Get(context.Background(), f.cached.Name, metav1.GetOptions{})
	require.NoError(t, err)
	return rs
}

// The stable metadata replaces the canary metadata of a promoted ReplicaSet. The Update
// conflicts with a status write, so the change is patched.
func TestSyncEphemeralMetadataPatchesAfterConflict(t *testing.T) {
	f := newReplicaSetPatchFixture(t, func(rs *appsv1.ReplicaSet) {
		*rs = *withEphemeralMetadata(rs, canaryRole)
	}, nil)
	f.conflictOnUpdate()

	err := f.roCtx.syncEphemeralMetadata(context.Background(), f.cached, stableRole)

	require.NoError(t, err)
	assert.Equal(t, []string{"update", "get", "patch"}, f.replicaSetVerbs())
	assert.JSONEq(t, fmt.Sprintf(`{
		"metadata": {"uid": "%s", "annotations": {"rollout.argoproj.io/ephemeral-metadata": "{\"labels\":{\"role\":\"stable\"}}"}},
		"spec": {"template": {"metadata": {"labels": {"role": "stable"}}}}
	}`, f.cached.UID), f.patch(t))

	live := f.live(t)
	assert.Equal(t, "stable", live.Spec.Template.Labels["role"])
	assert.Equal(t, `{"labels":{"role":"stable"}}`, live.Annotations[replicasetutil.EphemeralMetadataAnnotation])
	// Everything the patch does not name is left as the API had it.
	assert.Equal(t, int32(7), live.Status.ReadyReplicas)
	assert.Equal(t, int32(10), *live.Spec.Replicas)
	assert.Equal(t, "kept", live.Annotations["someone-elses/annotation"])
	assert.Equal(t, f.cached.Annotations[annotations.DesiredReplicasAnnotation], live.Annotations[annotations.DesiredReplicasAnnotation])
	assert.Equal(t, f.cached.Spec.Template.Labels["foo"], live.Spec.Template.Labels["foo"])
	assert.Equal(t, f.cached.Spec.Template.Spec, live.Spec.Template.Spec)
}

// Removing the metadata from a ReplicaSet that is no longer the stable one. After an abort
// or a rollback that ReplicaSet is the abandoned canary, which still has its pods.
func TestSyncEphemeralMetadataPatchRemovesMetadata(t *testing.T) {
	f := newReplicaSetPatchFixture(t, func(rs *appsv1.ReplicaSet) {
		*rs = *withEphemeralMetadata(rs, stableRole)
	}, nil)
	f.conflictOnUpdate()

	err := f.roCtx.syncEphemeralMetadata(context.Background(), f.cached, nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"update", "get", "patch"}, f.replicaSetVerbs())
	assert.JSONEq(t, fmt.Sprintf(`{
		"metadata": {"uid": "%s", "annotations": {"rollout.argoproj.io/ephemeral-metadata": null}},
		"spec": {"template": {"metadata": {"labels": {"role": null}}}}
	}`, f.cached.UID), f.patch(t))

	live := f.live(t)
	assert.NotContains(t, live.Spec.Template.Labels, "role")
	assert.NotContains(t, live.Annotations, replicasetutil.EphemeralMetadataAnnotation)
	assert.Equal(t, "kept", live.Annotations["someone-elses/annotation"])
	assert.Equal(t, "bar", live.Spec.Template.Labels["foo"])
	assert.Equal(t, int32(7), live.Status.ReadyReplicas)
}

// The pods are synced after the ReplicaSet was patched, as they are after an Update.
func TestSyncEphemeralMetadataSyncsPodsAfterPatch(t *testing.T) {
	f := newReplicaSetPatchFixture(t, func(rs *appsv1.ReplicaSet) {
		*rs = *withEphemeralMetadata(rs, canaryRole)
	}, nil)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name:            "foo-pod",
		Namespace:       f.cached.Namespace,
		Labels:          map[string]string{"foo": "bar", "role": "canary", v1alpha1.DefaultRolloutUniqueLabelKey: f.cached.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]},
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(f.cached, schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"})},
	}}
	_, err := f.kubeclient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	require.NoError(t, err)
	f.kubeclient.ClearActions()
	f.conflictOnUpdate()

	err = f.roCtx.syncEphemeralMetadata(context.Background(), f.cached, stableRole)

	require.NoError(t, err)
	updated, err := f.kubeclient.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "stable", updated.Labels["role"])
}

// The patch is computed from the live ReplicaSet, not from the controller's copy.
func TestSyncEphemeralMetadataPatchUsesLiveReplicaSet(t *testing.T) {
	t.Run("the live ReplicaSet already has the change", func(t *testing.T) {
		f := newReplicaSetPatchFixture(t, func(rs *appsv1.ReplicaSet) {
			*rs = *withEphemeralMetadata(rs, canaryRole)
		}, func(rs *appsv1.ReplicaSet) {
			*rs = *withEphemeralMetadata(rs, stableRole)
		})
		before := f.live(t)
		f.kubeclient.ClearActions()
		f.conflictOnUpdate()

		err := f.roCtx.syncEphemeralMetadata(context.Background(), f.cached, stableRole)

		require.NoError(t, err)
		assert.Equal(t, []string{"update", "get"}, f.replicaSetVerbs())
		assert.Equal(t, before, f.live(t))
	})
	t.Run("another writer changed other fields", func(t *testing.T) {
		f := newReplicaSetPatchFixture(t, func(rs *appsv1.ReplicaSet) {
			*rs = *withEphemeralMetadata(rs, canaryRole)
		}, func(rs *appsv1.ReplicaSet) {
			// A scale and a label that the controller's copy does not have.
			rs.Generation = 4
			rs.Spec.Replicas = ptr.To[int32](4)
			rs.Annotations[annotations.DesiredReplicasAnnotation] = "4"
			rs.Spec.Template.Labels["added-by"] = "someone-else"
		})
		f.conflictOnUpdate()

		err := f.roCtx.syncEphemeralMetadata(context.Background(), f.cached, stableRole)

		require.NoError(t, err)
		assert.Equal(t, []string{"update", "get", "patch"}, f.replicaSetVerbs())
		live := f.live(t)
		assert.Equal(t, "stable", live.Spec.Template.Labels["role"])
		assert.Equal(t, "someone-else", live.Spec.Template.Labels["added-by"])
		assert.Equal(t, int32(4), *live.Spec.Replicas)
		assert.Equal(t, "4", live.Annotations[annotations.DesiredReplicasAnnotation])
	})
}

// Cases in which the conflict is returned and nothing is patched.
func TestSyncEphemeralMetadataDoesNotPatch(t *testing.T) {
	t.Run("the ReplicaSet was deleted and recreated", func(t *testing.T) {
		f := newReplicaSetPatchFixture(t, func(rs *appsv1.ReplicaSet) {
			*rs = *withEphemeralMetadata(rs, canaryRole)
		}, func(rs *appsv1.ReplicaSet) {
			rs.UID = types.UID("a-different-uid")
		})
		before := f.live(t)
		f.kubeclient.ClearActions()
		f.conflictOnUpdate()

		err := f.roCtx.syncEphemeralMetadata(context.Background(), f.cached, stableRole)

		require.Error(t, err)
		assert.True(t, k8serrors.IsConflict(err), "the original conflict is returned: %v", err)
		assert.Equal(t, []string{"update", "get"}, f.replicaSetVerbs())
		assert.Equal(t, before, f.live(t))
	})
	t.Run("the ReplicaSet is gone", func(t *testing.T) {
		f := newReplicaSetPatchFixture(t, func(rs *appsv1.ReplicaSet) {
			*rs = *withEphemeralMetadata(rs, canaryRole)
		}, nil)
		require.NoError(t, f.kubeclient.AppsV1().ReplicaSets(f.cached.Namespace).Delete(context.Background(), f.cached.Name, metav1.DeleteOptions{}))
		f.kubeclient.ClearActions()
		f.conflictOnUpdate()

		err := f.roCtx.syncEphemeralMetadata(context.Background(), f.cached, stableRole)

		require.Error(t, err)
		assert.True(t, k8serrors.IsNotFound(err), "got: %v", err)
		assert.Equal(t, []string{"update", "get"}, f.replicaSetVerbs())
	})
	t.Run("the Update fails for another reason", func(t *testing.T) {
		f := newReplicaSetPatchFixture(t, func(rs *appsv1.ReplicaSet) {
			*rs = *withEphemeralMetadata(rs, canaryRole)
		}, nil)
		f.kubeclient.PrependReactor("update", "replicasets", func(action testclient.Action) (bool, runtime.Object, error) {
			return true, nil, replicaSetForbidden("foo")
		})

		err := f.roCtx.syncEphemeralMetadata(context.Background(), f.cached, stableRole)

		require.Error(t, err)
		assert.True(t, k8serrors.IsForbidden(err), "got: %v", err)
		assert.Equal(t, []string{"update"}, f.replicaSetVerbs())
	})
}

// Without a conflict the ReplicaSet is updated exactly as upstream does it.
func TestSyncEphemeralMetadataUpdatesWithoutConflict(t *testing.T) {
	f := newReplicaSetPatchFixture(t, func(rs *appsv1.ReplicaSet) {
		*rs = *withEphemeralMetadata(rs, canaryRole)
	}, func(rs *appsv1.ReplicaSet) {
		rs.ResourceVersion = "100"
	})

	err := f.roCtx.syncEphemeralMetadata(context.Background(), f.cached, stableRole)

	require.NoError(t, err)
	assert.Equal(t, []string{"update"}, f.replicaSetVerbs())
	assert.Equal(t, "stable", f.live(t).Spec.Template.Labels["role"])
}

// syncReplicaSetRevision: the revision annotations, the annotations copied from the
// Rollout and minReadySeconds are patched after a conflict.
func TestSyncReplicaSetRevisionPatchesAfterConflict(t *testing.T) {
	f := newReplicaSetPatchFixture(t, func(rs *appsv1.ReplicaSet) {
		rs.Annotations[annotations.RevisionAnnotation] = "1"
	}, nil)
	f.rollout.Annotations["copied-from/rollout"] = "yes"
	f.rollout.Spec.MinReadySeconds = 10
	// A ReplicaSet at revision 3, so the new ReplicaSet has to become revision 4.
	older := newReplicaSetWithStatus(f.rollout, 0, 0)
	older.Name = "foo-older"
	older.Annotations[annotations.RevisionAnnotation] = "3"
	f.roCtx.newRS = f.cached
	f.roCtx.olderRSs = []*appsv1.ReplicaSet{older}
	f.conflictOnUpdate()

	rs, err := f.roCtx.syncReplicaSetRevision()

	require.NoError(t, err)
	assert.Equal(t, []string{"update", "get", "patch"}, f.replicaSetVerbs())
	assert.JSONEq(t, fmt.Sprintf(`{
		"metadata": {"uid": "%s", "annotations": {
			"rollout.argoproj.io/revision": "4",
			"rollout.argoproj.io/revision-history": "1",
			"copied-from/rollout": "yes"}},
		"spec": {"minReadySeconds": 10}
	}`, f.cached.UID), f.patch(t))

	live := f.live(t)
	assert.Equal(t, "4", live.Annotations[annotations.RevisionAnnotation])
	assert.Equal(t, "1", live.Annotations[annotations.RevisionHistoryAnnotation])
	assert.Equal(t, "yes", live.Annotations["copied-from/rollout"])
	assert.Equal(t, int32(10), live.Spec.MinReadySeconds)
	assert.Equal(t, "kept", live.Annotations["someone-elses/annotation"])
	assert.Equal(t, int32(7), live.Status.ReadyReplicas)
	assert.Equal(t, int32(10), *live.Spec.Replicas)
	// The caller gets the patched ReplicaSet.
	assert.Equal(t, "4", rs.Annotations[annotations.RevisionAnnotation])
	assert.Equal(t, int32(7), rs.Status.ReadyReplicas)
}

// The revision is computed again from the live ReplicaSet: if it already moved on, the
// revision history is not written a second time.
func TestSyncReplicaSetRevisionPatchUsesLiveReplicaSet(t *testing.T) {
	f := newReplicaSetPatchFixture(t, func(rs *appsv1.ReplicaSet) {
		rs.Annotations[annotations.RevisionAnnotation] = "1"
	}, func(rs *appsv1.ReplicaSet) {
		rs.Annotations[annotations.RevisionAnnotation] = "4"
		rs.Annotations[annotations.RevisionHistoryAnnotation] = "1"
	})
	older := newReplicaSetWithStatus(f.rollout, 0, 0)
	older.Name = "foo-older"
	older.Annotations[annotations.RevisionAnnotation] = "3"
	f.roCtx.newRS = f.cached
	f.roCtx.olderRSs = []*appsv1.ReplicaSet{older}
	before := f.live(t)
	f.kubeclient.ClearActions()
	f.conflictOnUpdate()

	rs, err := f.roCtx.syncReplicaSetRevision()

	require.NoError(t, err)
	assert.Equal(t, []string{"update", "get"}, f.replicaSetVerbs())
	assert.Equal(t, before, f.live(t))
	assert.Equal(t, "4", rs.Annotations[annotations.RevisionAnnotation])
	assert.Equal(t, "1", rs.Annotations[annotations.RevisionHistoryAnnotation])
}

func TestSyncReplicaSetRevisionUpdatesWithoutConflict(t *testing.T) {
	f := newReplicaSetPatchFixture(t, nil, func(rs *appsv1.ReplicaSet) {
		rs.ResourceVersion = "100"
	})
	f.rollout.Spec.MinReadySeconds = 10
	f.roCtx.newRS = f.cached

	_, err := f.roCtx.syncReplicaSetRevision()

	require.NoError(t, err)
	assert.Equal(t, []string{"update"}, f.replicaSetVerbs())
	assert.Equal(t, int32(10), f.live(t).Spec.MinReadySeconds)
}

func TestReplicaSetChangePatch(t *testing.T) {
	base := func() *appsv1.ReplicaSet {
		return &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{Name: "rs", UID: "abc", Annotations: map[string]string{"a": "1", "b": "2"}, Labels: map[string]string{"l": "1"}},
			Spec: appsv1.ReplicaSetSpec{
				Replicas: ptr.To[int32](3),
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "x", "role": "canary"}},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "i"}}},
				},
			},
		}
	}
	tests := []struct {
		name   string
		change func(*appsv1.ReplicaSet)
		want   string
	}{
		{
			name: "set, change and remove annotations",
			change: func(rs *appsv1.ReplicaSet) {
				rs.Annotations = map[string]string{"a": "changed", "c": "new"}
			},
			want: `{"metadata":{"annotations":{"a":"changed","b":null,"c":"new"},"uid":"abc"}}`,
		},
		{
			name: "pod template labels and annotations",
			change: func(rs *appsv1.ReplicaSet) {
				rs.Spec.Template.Labels = map[string]string{"app": "x", "role": "stable"}
				rs.Spec.Template.Annotations = map[string]string{"t": "1"}
			},
			want: `{"metadata":{"uid":"abc"},"spec":{"template":{"metadata":{"annotations":{"t":"1"},"labels":{"role":"stable"}}}}}`,
		},
		{
			name:   "minReadySeconds",
			change: func(rs *appsv1.ReplicaSet) { rs.Spec.MinReadySeconds = 10 },
			want:   `{"metadata":{"uid":"abc"},"spec":{"minReadySeconds":10}}`,
		},
		{
			name: "nothing to change",
			change: func(rs *appsv1.ReplicaSet) {
				rs.Spec.Template.Annotations = map[string]string{}
			},
			want: "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			live, desired := base(), base()
			test.change(desired)
			patch, err := replicaSetChangePatch(live, desired)
			require.NoError(t, err)
			// Exact bytes: the patch must contain nothing else, in particular no resourceVersion.
			assert.Equal(t, test.want, string(patch))
		})
	}

	// A change to any other field is refused, so that it is never applied in part.
	unsupported := map[string]func(*appsv1.ReplicaSet){
		"replicas":        func(rs *appsv1.ReplicaSet) { rs.Spec.Replicas = ptr.To[int32](5) },
		"labels":          func(rs *appsv1.ReplicaSet) { rs.Labels["l"] = "2" },
		"container image": func(rs *appsv1.ReplicaSet) { rs.Spec.Template.Spec.Containers[0].Image = "other" },
		"selector":        func(rs *appsv1.ReplicaSet) { rs.Spec.Selector = &metav1.LabelSelector{} },
		"owner":           func(rs *appsv1.ReplicaSet) { rs.OwnerReferences = []metav1.OwnerReference{{Name: "o"}} },
	}
	for name, change := range unsupported {
		t.Run("refuses a change of "+name, func(t *testing.T) {
			live, desired := base(), base()
			change(desired)
			desired.Annotations["a"] = "changed"
			patch, err := replicaSetChangePatch(live, desired)
			assert.ErrorIs(t, err, errUnsupportedReplicaSetChange)
			assert.Nil(t, patch)
		})
	}
}

// The affinity is the one nested object the patch carries. Applying the patch to the
// live affinity has to give exactly the desired affinity, including removed rules.
func TestReplicaSetChangePatchAffinity(t *testing.T) {
	term := func(hash string) corev1.PodAffinityTerm {
		return corev1.PodAffinityTerm{
			TopologyKey: "kubernetes.io/hostname",
			LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: v1alpha1.DefaultRolloutUniqueLabelKey, Operator: metav1.LabelSelectorOpIn, Values: []string{hash}},
			}},
		}
	}
	required := func(hash string) *corev1.Affinity {
		return &corev1.Affinity{PodAntiAffinity: &corev1.PodAntiAffinity{RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{term(hash)}}}
	}
	withNodeAffinity := required("old")
	withNodeAffinity.NodeAffinity = &corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
		NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "pool", Operator: corev1.NodeSelectorOpIn, Values: []string{"a"}}}}},
	}}
	onlyNodeAffinity := &corev1.Affinity{NodeAffinity: withNodeAffinity.NodeAffinity}

	tests := []struct {
		name          string
		live, desired *corev1.Affinity
	}{
		{name: "added", live: nil, desired: required("new")},
		{name: "removed", live: required("old"), desired: nil},
		{name: "the injected rule points at another ReplicaSet", live: required("old"), desired: required("new")},
		{name: "the injected rule is removed and the rest is kept", live: withNodeAffinity, desired: onlyNodeAffinity},
		{name: "the injected rule is added to an existing affinity", live: onlyNodeAffinity, desired: withNodeAffinity},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			live := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "rs", UID: "abc"}}
			live.Spec.Template.Spec.Affinity = test.live
			desired := live.DeepCopy()
			desired.Spec.Template.Spec.Affinity = test.desired

			patch, err := replicaSetChangePatch(live, desired)
			require.NoError(t, err)

			// Apply the patch the way the API server does, and compare.
			liveJSON, err := json.Marshal(live)
			require.NoError(t, err)
			patchedJSON, err := jsonpatch.MergePatch(liveJSON, patch)
			require.NoError(t, err)
			patched := &appsv1.ReplicaSet{}
			require.NoError(t, json.Unmarshal(patchedJSON, patched))
			assert.Equal(t, test.desired, patched.Spec.Template.Spec.Affinity)
			assert.Equal(t, desired, patched)
		})
	}
}

// The whole reconcile after an abort: the canary ReplicaSet is no longer the new or the
// stable one, so its canary metadata is removed. That Update conflicts and is patched,
// and the reconcile goes on to sync the pods instead of failing.
func TestSyncRolloutPatchesEphemeralMetadataAfterUpdateConflict(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newCanaryRollout("foo", 3, nil, nil, ptr.To[int32](1), intstr.FromInt32(1), intstr.FromInt32(1))
	r1.Spec.Strategy.Canary.CanaryMetadata = canaryRole
	r1.Spec.Strategy.Canary.StableMetadata = stableRole
	r2 := bumpVersion(r1)
	// The Rollout is back on the pod template of revision 1, which is the stable one.
	r3 := r2.DeepCopy()
	r3.Spec.Template = r1.Spec.Template
	r3.Status.StableRS = r1.Status.CurrentPodHash
	r3.Status.CurrentPodHash = r1.Status.CurrentPodHash
	r3.Annotations[annotations.RevisionAnnotation] = "3"

	rs1 := withEphemeralMetadata(newReplicaSetWithStatus(r1, 3, 3), stableRole)
	rs1.Annotations[annotations.RevisionAnnotation] = "3"
	// The abandoned canary: scaled down, but still carrying the canary metadata.
	rs2 := withEphemeralMetadata(newReplicaSetWithStatus(r2, 0, 0), canaryRole)
	rs2.Annotations[annotations.RevisionAnnotation] = "2"

	f.rolloutLister = append(f.rolloutLister, r3)
	f.objects = append(f.objects, r3)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	f.expectUpdateReplicaSetAction(rs2) // remove the canary metadata, rejected with a conflict
	f.expectGetReplicaSetAction(rs2)    // the live ReplicaSet is read
	patchIndex := f.expectPatchReplicaSetAction(rs2)
	f.expectPatchRolloutAction(r3)

	key := fmt.Sprintf("%s/%s", r3.Namespace, r3.Name)
	c, i, k8sI := f.newController(func() time.Duration { return 30 * time.Minute })
	f.kubeclient.PrependReactor("update", "replicasets", func(action testclient.Action) (bool, runtime.Object, error) {
		rs := action.(testclient.UpdateAction).GetObject().(*appsv1.ReplicaSet)
		return true, nil, replicaSetConflict(rs.Name)
	})

	f.runController(key, true, false, c, i, k8sI)

	patch := filterInformerActions(f.kubeclient.Actions())[patchIndex].(testclient.PatchAction)
	assert.JSONEq(t, fmt.Sprintf(`{
		"metadata": {"uid": "%s", "annotations": {"rollout.argoproj.io/ephemeral-metadata": null}},
		"spec": {"template": {"metadata": {"labels": {"role": null}}}}
	}`, rs2.UID), string(patch.GetPatch()))
	live, err := f.kubeclient.AppsV1().ReplicaSets(rs2.Namespace).Get(context.Background(), rs2.Name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.NotContains(t, live.Spec.Template.Labels, "role")
	assert.NotContains(t, live.Annotations, replicasetutil.EphemeralMetadataAnnotation)
}
