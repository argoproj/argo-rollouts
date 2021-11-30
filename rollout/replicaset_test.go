package rollout

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
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
		"app": "nginx",
	}
	diffSelector := map[string]string{
		"app": "nginx2",
	}
	rollout := newRollout("foo", 1, int32Ptr(1), selector)
	diffRollout := newRollout("bar", 1, int32Ptr(1), selector)
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
			defer f.Close()
			f.rolloutLister = append(f.rolloutLister, rollout)
			f.objects = append(f.objects, rollout)
			f.replicaSetLister = append(f.replicaSetLister, test.existingRSs...)
			for _, rs := range test.existingRSs {
				f.kubeobjects = append(f.kubeobjects, rs)
			}

			c, informers, _ := f.newController(noResyncPeriodFunc)
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
		name                       string
		rolloutReplicas            int
		newReplicas                int
		scaleExpected              bool
		abortScaleDownDelaySeconds int32
		abortScaleDownAnnotated    bool
		abortScaleDownDelayPassed  bool
		expectedNewReplicas        int
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

		{
			name:            "New Replica scaled down to 0: scale down on abort - deadline passed",
			rolloutReplicas: 10,
			newReplicas:     10,
			// ScaleDownOnAbort:           true,
			abortScaleDownDelaySeconds: 5,
			abortScaleDownAnnotated:    true,
			abortScaleDownDelayPassed:  true,
			scaleExpected:              true,
			expectedNewReplicas:        0,
		},
		{
			name:            "New Replica scaled down to 0: scale down on abort - deadline not passed",
			rolloutReplicas: 10,
			newReplicas:     8,
			// ScaleDownOnAbort:           true,
			abortScaleDownDelaySeconds: 5,
			abortScaleDownAnnotated:    true,
			abortScaleDownDelayPassed:  false,
			scaleExpected:              false,
			expectedNewReplicas:        0,
		},
		{
			name:                       "New Replica scaled down to 0: scale down on abort - add annotation",
			rolloutReplicas:            10,
			newReplicas:                10,
			abortScaleDownDelaySeconds: 5,
			abortScaleDownAnnotated:    false,
			scaleExpected:              false,
			expectedNewReplicas:        0,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			test := tests[i]
			oldRS := rs("foo-v1", test.newReplicas, nil, noTimestamp, nil)
			newRS := rs("foo-v2", test.newReplicas, nil, noTimestamp, nil)
			rollout := newBlueGreenRollout("foo", test.rolloutReplicas, nil, "", "")
			fake := fake.Clientset{}
			k8sfake := k8sfake.Clientset{}
			roCtx := rolloutContext{
				log:      logutil.WithRollout(rollout),
				rollout:  rollout,
				newRS:    newRS,
				stableRS: oldRS,
				reconcilerBase: reconcilerBase{
					argoprojclientset: &fake,
					kubeclientset:     &k8sfake,
					recorder:          record.NewFakeEventRecorder(),
					resyncPeriod:      30 * time.Second,
				},
				pauseContext: &pauseContext{
					rollout: rollout,
				},
			}
			roCtx.enqueueRolloutAfter = func(obj interface{}, duration time.Duration) {}
			if test.abortScaleDownDelaySeconds > 0 {
				rollout.Status.Abort = true
				rollout.Spec.Strategy = v1alpha1.RolloutStrategy{
					BlueGreen: &v1alpha1.BlueGreenStrategy{
						AbortScaleDownDelaySeconds: &test.abortScaleDownDelaySeconds,
					},
				}

				if test.abortScaleDownAnnotated {
					var deadline string
					if test.abortScaleDownDelayPassed {
						deadline = timeutil.Now().Add(-time.Duration(test.abortScaleDownDelaySeconds) * time.Second).UTC().Format(time.RFC3339)
					} else {
						deadline = timeutil.Now().Add(time.Duration(test.abortScaleDownDelaySeconds) * time.Second).UTC().Format(time.RFC3339)
					}
					roCtx.newRS.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = deadline
				}
			}

			scaled, err := roCtx.reconcileNewReplicaSet()
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

func TestReconcileOldReplicaSet(t *testing.T) {
	tests := []struct {
		name                string
		rolloutReplicas     int
		oldReplicas         int
		newReplicas         int
		readyPodsFromOldRS  int
		readyPodsFromNewRS  int
		scaleExpected       bool
		expectedOldReplicas int
	}{
		{
			name:               "No pods to scale down",
			rolloutReplicas:    10,
			oldReplicas:        0,
			newReplicas:        10,
			readyPodsFromOldRS: 0,
			readyPodsFromNewRS: 0,
			scaleExpected:      false,
		},
		{
			name:                "Clean up unhealthy pods",
			rolloutReplicas:     10,
			oldReplicas:         10,
			newReplicas:         10,
			readyPodsFromOldRS:  8,
			readyPodsFromNewRS:  10,
			scaleExpected:       true,
			expectedOldReplicas: 0,
		},
		{
			name:                "Normal scale down when new ReplicaSet is healthy",
			rolloutReplicas:     10,
			oldReplicas:         10,
			newReplicas:         10,
			readyPodsFromOldRS:  10,
			readyPodsFromNewRS:  10,
			scaleExpected:       true,
			expectedOldReplicas: 0,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			newSelector := map[string]string{"foo": "new"}
			oldSelector := map[string]string{"foo": "old"}
			newRS := rs("foo-new", test.newReplicas, newSelector, noTimestamp, nil)
			newRS.Annotations = map[string]string{annotations.DesiredReplicasAnnotation: strconv.Itoa(test.newReplicas)}
			newRS.Status.AvailableReplicas = int32(test.readyPodsFromNewRS)
			oldRS := rs("foo-old", test.oldReplicas, oldSelector, noTimestamp, nil)
			oldRS.Annotations = map[string]string{annotations.DesiredReplicasAnnotation: strconv.Itoa(test.oldReplicas)}
			oldRS.Status.AvailableReplicas = int32(test.readyPodsFromOldRS)
			rollout := newBlueGreenRollout("foo", test.rolloutReplicas, nil, "", "")
			rollout.Spec.Strategy.BlueGreen.ScaleDownDelayRevisionLimit = pointer.Int32Ptr(0)
			rollout.Spec.Selector = &metav1.LabelSelector{MatchLabels: newSelector}
			f := newFixture(t)
			defer f.Close()
			f.objects = append(f.objects, rollout)
			f.replicaSetLister = append(f.replicaSetLister, oldRS, newRS)
			f.kubeobjects = append(f.kubeobjects, oldRS, newRS)
			c, informers, _ := f.newController(noResyncPeriodFunc)
			stopCh := make(chan struct{})
			informers.Start(stopCh)
			informers.WaitForCacheSync(stopCh)
			close(stopCh)
			roCtx, err := c.newRolloutContext(rollout)
			assert.NoError(t, err)
			roCtx.otherRSs = []*appsv1.ReplicaSet{oldRS}
			scaled, err := roCtx.reconcileOtherReplicaSets()
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if !test.scaleExpected && scaled {
				t.Errorf("unexpected scaling: %v", f.kubeclient.Actions())
			}
			if test.scaleExpected && !scaled {
				t.Errorf("expected scaling to occur")
				return
			}
		})
	}
}
