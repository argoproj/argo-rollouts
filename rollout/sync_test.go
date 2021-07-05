package rollout

import (
	"strconv"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	testclient "k8s.io/client-go/testing"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
	"github.com/stretchr/testify/assert"
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
	now := metav1.Now()
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
			Templates: []v1alpha1.RolloutAnalysisTemplate{
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

	createdRS2Index := f.expectCreateReplicaSetAction(rs2) // create new ReplicaSet (surge to 10)
	f.expectUpdateRolloutAction(r2)                        // update rollout revision
	f.expectUpdateRolloutStatusAction(r2)                  // update rollout conditions
	patchedRolloutIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	createdRS2 := f.getCreatedReplicaSet(createdRS2Index)
	assert.Equal(t, int32(10), *createdRS2.Spec.Replicas) // verify we ignored steps

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
	r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = pointer.BoolPtr(false)
	r1.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.RolloutAnalysisTemplate{
			{
				TemplateName: at.Name,
			},
		},
	}
	r1.Spec.Strategy.BlueGreen.PostPromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.RolloutAnalysisTemplate{
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
	f.expectPatchReplicaSetAction(rs1)                // set scaledown delay on rs1
	patchRolloutIdx := f.expectPatchRolloutAction(r2) // update rollout status
	f.run(getKey(r2, t))

	patchedRollout := f.getPatchedRolloutAsObject(patchRolloutIdx)
	assert.Equal(t, rs2PodHash, patchedRollout.Status.StableRS)
	assert.Equal(t, rs2PodHash, patchedRollout.Status.BlueGreen.ActiveSelector)
	assert.False(t, patchedRollout.Status.PromoteFull)
}

// TestSendStateChangeEvents verifies we emit appropriate events on rollout state changes
func TestSendStateChangeEvents(t *testing.T) {
	now := metav1.Now()
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
		assert.Equal(t, test.expectedEventReasons, recorder.Events)
	}
}
