package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
)

func newRolloutControllerRef(r *v1alpha1.Rollout) *metav1.OwnerReference {
	isController := true
	return &metav1.OwnerReference{
		APIVersion: "argoproj.io/v1alpha1",
		Kind:       "Rollouts",
		Name:       r.GetName(),
		UID:        r.GetUID(),
		Controller: &isController,
	}
}

func int32Ptr(i int32) *int32 { return &i }

func TestGetReplicaSetsForRollouts(t *testing.T) {
	newTimestamp := metav1.Date(2016, 5, 20, 2, 0, 0, 0, time.UTC)
	selector := map[string]string{
		"app": "ngnix",
	}
	diffSelector := map[string]string{
		"app": "ngnix2",
	}
	rollout := newRollout("foo", 1, int32Ptr(1), selector, "", "")
	diffRollout := newRollout("bar", 1, int32Ptr(1), selector, "", "")
	tests := []struct {
		name        string
		existingRSs []*appsv1.ReplicaSet

		expectedSelectedRSs []*appsv1.ReplicaSet
		expectedError       error
	}{
		{
			name: "Grab corrected owned replicasets",
			existingRSs: []*appsv1.ReplicaSet{
				rs("foo-v2", 1, selector, newTimestamp, newRolloutControllerRef(rollout)),
				rs("foo-v1", 1, selector, newTimestamp, newRolloutControllerRef(diffRollout)),
			},
			expectedSelectedRSs: []*appsv1.ReplicaSet{
				rs("foo-v2", 1, selector, newTimestamp, newRolloutControllerRef(rollout)),
			},
			expectedError: nil,
		},
		{
			name: "Adopt orphaned replica sets",
			existingRSs: []*appsv1.ReplicaSet{
				rs("foo-v1", 1, selector, newTimestamp, nil),
			},
			expectedSelectedRSs: []*appsv1.ReplicaSet{
				rs("foo-v1", 1, selector, newTimestamp, newRolloutControllerRef(rollout)),
			},
			expectedError: nil,
		},
		{
			name:                "No replica sets exist",
			existingRSs:         []*appsv1.ReplicaSet{},
			expectedSelectedRSs: []*appsv1.ReplicaSet{},
			expectedError:       nil,
		},
		{
			name: "No selector provided so no adoption",
			existingRSs: []*appsv1.ReplicaSet{
				rs("foo-v1", 1, nil, newTimestamp, newRolloutControllerRef(diffRollout)),
			},
			expectedSelectedRSs: []*appsv1.ReplicaSet{},
			expectedError:       nil,
		},
		{
			name: "Orphan RS with different selector",
			existingRSs: []*appsv1.ReplicaSet{
				rs("foo-v1", 1, diffSelector, newTimestamp, newRolloutControllerRef(diffRollout)),
			},
			expectedSelectedRSs: []*appsv1.ReplicaSet{},
			expectedError:       nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			f.rolloutLister = append(f.rolloutLister, rollout)
			f.objects = append(f.objects, rollout)
			f.replicaSetLister = append(f.replicaSetLister, test.existingRSs...)
			for _, rs := range test.existingRSs {
				f.kubeobjects = append(f.kubeobjects, rs)
			}

			c, informers, _ := f.newController()
			stopCh := make(chan struct{})
			defer close(stopCh)
			informers.Start(stopCh)
			returnedRSs, err := c.getReplicaSetsForRollouts(rollout)

			assert.Equal(t, test.expectedError, err)
			assert.Equal(t, len(test.expectedSelectedRSs), len(returnedRSs))
			for i, returnedRS := range returnedRSs {
				assert.Equal(t, test.expectedSelectedRSs[i].Name, returnedRS.Name)
			}
		})
	}

}

func TestReconcileNewReplicaSet(t *testing.T) {
	tests := []struct {
		name                string
		rolloutReplicas     int
		newReplicas         int
		scaleExpected       bool
		expectedNewReplicas int
	}{
		{
			name:            "New Replica Set matches rollout replica: No scale",
			rolloutReplicas: 10,
			newReplicas:     10,
			scaleExpected:   false,
		},
		{
			name:                "New Replica Set higher than rollout replica: Scale down",
			rolloutReplicas:     10,
			newReplicas:         12,
			scaleExpected:       true,
			expectedNewReplicas: 10,
		},
		{
			name:                "New Replica Set lower than rollout replica: Scale up",
			rolloutReplicas:     10,
			newReplicas:         8,
			scaleExpected:       true,
			expectedNewReplicas: 10,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			test := tests[i]
			newRS := rs("foo-v2", test.newReplicas, nil, noTimestamp, nil)
			allRSs := []*appsv1.ReplicaSet{newRS}
			rollout := newRollout("foo", test.rolloutReplicas, nil, map[string]string{"foo": "bar"}, "", "")
			fake := fake.Clientset{}
			k8sfake := k8sfake.Clientset{}
			controller := &Controller{
				rolloutsclientset: &fake,
				kubeclientset:     &k8sfake,
				recorder:          &record.FakeRecorder{},
			}
			scaled, err := controller.reconcileNewReplicaSet(allRSs, newRS, rollout)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if !test.scaleExpected {
				if scaled || len(fake.Actions()) > 0 {
					t.Errorf("unexpected scaling: %v", fake.Actions())
				}
				return
			}
			if test.scaleExpected && !scaled {
				t.Errorf("expected scaling to occur")
				return
			}
			if len(k8sfake.Actions()) != 1 {
				t.Errorf("expected 1 action during scale, got: %v", fake.Actions())
				return
			}
			updated := k8sfake.Actions()[0].(core.UpdateAction).GetObject().(*appsv1.ReplicaSet)
			if e, a := test.expectedNewReplicas, int(*(updated.Spec.Replicas)); e != a {
				t.Errorf("expected update to %d replicas, got %d", e, a)
			}
		})
	}
}
