package rollout

import (
	"strconv"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	testclient "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/utils/annotations"
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

func TestCleanupRollouts(t *testing.T) {
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
			roCtx := newBlueGreenCtx(r, nil, test.replicaSets, nil)
			fake := fake.Clientset{}
			k8sfake := k8sfake.Clientset{}
			c := &RolloutController{
				argoprojclientset: &fake,
				kubeclientset:     &k8sfake,
				recorder:          &record.FakeRecorder{},
			}
			err := c.cleanupRollouts(test.replicaSets, roCtx)
			assert.Nil(t, err)

			assert.Equal(t, len(test.expectedDeleted), len(k8sfake.Actions()))
			for _, action := range k8sfake.Actions() {
				rsName := action.(testclient.DeleteAction).GetName()
				assert.True(t, test.expectedDeleted[rsName])
			}
		})
	}
}

//func TestGetNewReplicaSet(t *testing.T) {
//	f := newFixture(t)
//	defer f.Close()
//
//	r1 := newBlueGreenRollout("foo", 1, nil, "bar", "")
//	r2 := bumpVersion(r1)
//
//	rs2 := newReplicaSetWithStatus(r2, 1, 1)
//	f.kubeobjects = append(f.kubeobjects, rs2)
//	f.replicaSetLister = append(f.replicaSetLister, rs2)
//	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
//
//	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
//	s := newService("bar", 80, serviceSelector)
//	f.kubeobjects = append(f.kubeobjects, s)
//
//	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, rs2PodHash, 1, 1, 1, 1, false, true)
//	r2.Status.ObservedGeneration = conditions.ComputeGenerationHash(r2.Spec)
//
//	f.rolloutLister = append(f.rolloutLister, r2)
//	f.objects = append(f.objects, r2)
//	f.serviceLister = append(f.serviceLister, s)
//
//	patchIndex := f.expectPatchRolloutAction(r1)
//
//	f.run(getKey(r2, t))
//
//	newConditions := generateConditionsPatch(true, conditions.NewRSAvailableReason, rs2, true, "")
//	expectedPatch := fmt.Sprintf(`{
//		"status":{
//			"conditions":%s
//		}
//	}`, newConditions)
//	patch := f.getPatchedRollout(patchIndex)
//	assert.Equal(t, cleanPatch(expectedPatch), patch)
//}
