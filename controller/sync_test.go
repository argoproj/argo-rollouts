package controller

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
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/rollout-controller/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/rollout-controller/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/rollout-controller/utils/annotations"
)

func intOrStrP(num int) *intstr.IntOrString {
	intstr := intstr.FromInt(num)
	return &intstr
}

func newRolloutWithStatus(name string, replicas int, revisionHistoryLimit *int32, selector map[string]string) *v1alpha1.Rollout {
	rollout := newRollout(name, replicas, revisionHistoryLimit, selector, "", "")
	return rollout
}

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

func TestScale(t *testing.T) {
	newTimestamp := metav1.Date(2016, 5, 20, 3, 0, 0, 0, time.UTC)
	oldTimestamp := metav1.Date(2016, 5, 20, 2, 0, 0, 0, time.UTC)
	olderTimestamp := metav1.Date(2016, 5, 20, 1, 0, 0, 0, time.UTC)
	oldestTimestamp := metav1.Date(2016, 5, 20, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		rollout    *v1alpha1.Rollout
		oldRollout *v1alpha1.Rollout

		newRS  *appsv1.ReplicaSet
		oldRSs []*appsv1.ReplicaSet

		expectedNew  *appsv1.ReplicaSet
		expectedOld  []*appsv1.ReplicaSet
		wasntUpdated map[string]bool

		previewSvc *corev1.Service
		activeSvc  *corev1.Service

		desiredReplicasAnnotations map[string]int32
	}{
		{
			name:       "normal scaling event: 10 -> 12",
			rollout:    newRollout("foo", 12, nil, nil, "", ""),
			oldRollout: newRollout("foo", 10, nil, nil, "", ""),

			newRS:  rs("foo-v1", 10, nil, newTimestamp, nil),
			oldRSs: []*appsv1.ReplicaSet{},

			expectedNew: rs("foo-v1", 12, nil, newTimestamp, nil),
			expectedOld: []*appsv1.ReplicaSet{},
			previewSvc:  nil,
			activeSvc:   nil,
		},
		{
			name:       "normal scaling event: 10 -> 5",
			rollout:    newRollout("foo", 5, nil, nil, "", ""),
			oldRollout: newRollout("foo", 10, nil, nil, "", ""),

			newRS:  rs("foo-v1", 10, nil, newTimestamp, nil),
			oldRSs: []*appsv1.ReplicaSet{},

			expectedNew: rs("foo-v1", 5, nil, newTimestamp, nil),
			expectedOld: []*appsv1.ReplicaSet{},
			previewSvc:  nil,
			activeSvc:   nil,
		},
		{
			name:       "Scale up non-active latest Replicaset",
			rollout:    newRollout("foo", 5, nil, nil, "", ""),
			oldRollout: newRollout("foo", 5, nil, nil, "", ""),

			newRS:  rs("foo-v2", 0, nil, newTimestamp, nil),
			oldRSs: []*appsv1.ReplicaSet{rs("foo-v1", 0, nil, oldTimestamp, nil)},

			expectedNew: rs("foo-v2", 5, nil, newTimestamp, nil),
			expectedOld: []*appsv1.ReplicaSet{rs("foo-v1", 0, nil, newTimestamp, nil)},
			previewSvc:  nil,
			activeSvc:   nil,
		},
		{
			name:       "Scale down older active replica sets",
			rollout:    newRolloutWithStatus("foo", 5, nil, nil),
			oldRollout: newRolloutWithStatus("foo", 5, nil, nil),

			newRS: rs("foo-v2", 5, nil, newTimestamp, nil),
			oldRSs: []*appsv1.ReplicaSet{
				rs("foo-v1", 5, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "foo-v1"}, oldTimestamp, nil),
				rs("foo-v0", 5, nil, olderTimestamp, nil),
			},

			expectedNew: rs("foo-v2", 5, nil, newTimestamp, nil),
			expectedOld: []*appsv1.ReplicaSet{
				rs("foo-v1", 5, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "foo-v1"}, oldTimestamp, nil),
				rs("foo-v0", 0, nil, olderTimestamp, nil),
			},
			previewSvc: nil,
			activeSvc:  newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "foo-v1"}),
		},
		{
			name:       "No updates",
			rollout:    newRolloutWithStatus("foo", 5, nil, nil),
			oldRollout: newRolloutWithStatus("foo", 5, nil, nil),

			newRS: rs("foo-v3", 5, nil, newTimestamp, nil),
			oldRSs: []*appsv1.ReplicaSet{
				rs("foo-v2", 5, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "foo-v2"}, oldTimestamp, nil),
				rs("foo-v1", 5, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "foo-v1"}, olderTimestamp, nil),
				rs("foo-v0", 0, nil, oldestTimestamp, nil),
			},

			expectedNew: rs("foo-v3", 5, nil, newTimestamp, nil),
			expectedOld: []*appsv1.ReplicaSet{
				rs("foo-v2", 5, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "foo-v2"}, oldTimestamp, nil),
				rs("foo-v1", 5, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "foo-v1"}, olderTimestamp, nil),
				rs("foo-v0", 0, nil, oldestTimestamp, nil),
			},
			previewSvc: newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "foo-v2"}),
			activeSvc:  newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "foo-v1"}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_ = olderTimestamp
			rolloutFake := fake.Clientset{}
			k8sFake := k8sfake.Clientset{}
			c := &Controller{
				rolloutsclientset: &rolloutFake,
				kubeclientset:     &k8sFake,
				recorder:          &record.FakeRecorder{},
			}

			if test.newRS != nil {
				desiredReplicas := *(test.oldRollout.Spec.Replicas)
				if desired, ok := test.desiredReplicasAnnotations[test.newRS.Name]; ok {
					desiredReplicas = desired
				}
				annotations.SetReplicasAnnotations(test.newRS, desiredReplicas)
			}
			for i := range test.oldRSs {
				rs := test.oldRSs[i]
				if rs == nil {
					continue
				}
				desiredReplicas := *(test.oldRollout.Spec.Replicas)
				if desired, ok := test.desiredReplicasAnnotations[rs.Name]; ok {
					desiredReplicas = desired
				}
				annotations.SetReplicasAnnotations(rs, desiredReplicas)
			}

			if err := c.scale(test.rollout, test.newRS, test.oldRSs, test.previewSvc, test.activeSvc); err != nil {
				t.Errorf("%s: unexpected error: %v", test.name, err)
				return
			}

			// Construct the nameToSize map that will hold all the sizes we got our of tests
			// Skip updating the map if the replica set wasn't updated since there will be
			// no update action for it.
			nameToSize := make(map[string]int32)
			if test.newRS != nil {
				nameToSize[test.newRS.Name] = *(test.newRS.Spec.Replicas)
			}
			for i := range test.oldRSs {
				rs := test.oldRSs[i]
				nameToSize[rs.Name] = *(rs.Spec.Replicas)
			}
			// Get all the UPDATE actions and update nameToSize with all the updated sizes.
			for _, action := range k8sFake.Actions() {
				rs := action.(testclient.UpdateAction).GetObject().(*appsv1.ReplicaSet)
				if !test.wasntUpdated[rs.Name] {
					nameToSize[rs.Name] = *(rs.Spec.Replicas)
				}
			}

			if test.expectedNew != nil && test.newRS != nil && *(test.expectedNew.Spec.Replicas) != nameToSize[test.newRS.Name] {
				t.Errorf("%s: expected new replicas: %d, got: %d", test.name, *(test.expectedNew.Spec.Replicas), nameToSize[test.newRS.Name])
				return
			}
			if len(test.expectedOld) != len(test.oldRSs) {
				t.Errorf("%s: expected %d old replica sets, got %d", test.name, len(test.expectedOld), len(test.oldRSs))
				return
			}
			for n := range test.oldRSs {
				rs := test.oldRSs[n]
				expected := test.expectedOld[n]
				if *(expected.Spec.Replicas) != nameToSize[rs.Name] {
					t.Errorf("%s: expected old (%s) replicas: %d, got: %d", test.name, rs.Name, *(expected.Spec.Replicas), nameToSize[rs.Name])
				}
			}
		})
	}
}
