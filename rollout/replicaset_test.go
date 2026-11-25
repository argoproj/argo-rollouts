package rollout

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/dynamic/dynamiclister"
	k8sinformers "k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/istio"
	testutil "github.com/argoproj/argo-rollouts/test/util"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
	resourceversionutil "github.com/argoproj/argo-rollouts/utils/resourceversion"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"
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

// TestGetReplicaSetsForRolloutsReturnsCopy ensures that getReplicaSetsForRollouts returns fresh
// DeepCopies of the ReplicaSets on every call, rather than the shared pointers held by the informer
// cache. The Rollout reconciliation mutates these objects, so handing out the cached pointers would
// corrupt the shared cache.
func TestGetReplicaSetsForRollouts_ReturnsCopy(t *testing.T) {
	selector := map[string]string{"app": "nginx"}
	timestamp := metav1.Date(2016, 5, 20, 2, 0, 0, 0, time.UTC)
	rollout := newRollout("foo", 1, int32Ptr(1), selector)
	existingRS := rs("foo-v1", 1, selector, timestamp, newRolloutControllerRef(rollout))

	f := newFixture(t)
	defer f.Close()
	f.rolloutLister = append(f.rolloutLister, rollout)
	f.objects = append(f.objects, rollout)
	f.replicaSetLister = append(f.replicaSetLister, existingRS)
	f.kubeobjects = append(f.kubeobjects, existingRS)

	c, _, _ := f.newController(noResyncPeriodFunc)

	first, err := c.getReplicaSetsForRollouts(rollout)
	assert.NoError(t, err)
	assert.Len(t, first, 1)

	// The returned ReplicaSet must be a copy, not the object stored in the informer cache, so callers
	// can mutate the result without corrupting the shared cache.
	assert.NotSame(t, existingRS, first[0], "returned ReplicaSet must point to a different object than the one in the informer cache")
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
		failRSUpdate               bool
		abort                      bool
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
			abort:                      true,
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
			abort:                      true,
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
			abort:                      true,
			abortScaleDownAnnotated:    false,
			scaleExpected:              false,
			expectedNewReplicas:        0,
		},
		{
			name:                       "Fail to update RS: No scale and add default annotation",
			rolloutReplicas:            10,
			newReplicas:                10,
			scaleExpected:              false,
			failRSUpdate:               true,
			abort:                      true,
			abortScaleDownDelaySeconds: -1,
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

			if test.failRSUpdate {
				k8sfake.PrependReactor("patch", "replicasets", func(action core.Action) (handled bool, ret runtime.Object, err error) {
					return true, &appsv1.ReplicaSet{}, fmt.Errorf("should not patch replica set")
				})
			}

			f := newFixture(t)
			defer f.Close()
			f.objects = append(f.objects, rollout)
			f.replicaSetLister = append(f.replicaSetLister, oldRS, newRS)
			f.kubeobjects = append(f.kubeobjects, oldRS, newRS)
			_, informers, k8sInformer := f.newController(noResyncPeriodFunc)
			stopCh := make(chan struct{})
			informers.Start(stopCh)
			informers.WaitForCacheSync(stopCh)
			close(stopCh)

			roCtx := rolloutContext{
				log:      logutil.WithRollout(rollout),
				rollout:  rollout,
				newRS:    newRS,
				stableRS: oldRS,
				reconcilerBase: reconcilerBase{
					argoprojclientset:  &fake,
					kubeclientset:      &k8sfake,
					recorder:           record.NewFakeEventRecorder(),
					resyncPeriod:       30 * time.Second,
					replicaSetInformer: k8sInformer.Apps().V1().ReplicaSets().Informer(),
				},
				pauseContext: &pauseContext{
					rollout: rollout,
				},
			}
			roCtx.enqueueRolloutAfter = func(obj any, duration time.Duration) {}

			rollout.Status.Abort = test.abort
			roCtx.stableRS.Status.AvailableReplicas = int32(test.rolloutReplicas)
			rollout.Spec.Strategy = v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{
					AbortScaleDownDelaySeconds: &test.abortScaleDownDelaySeconds,
				},
			}

			if test.abortScaleDownDelaySeconds > 0 {
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

			if test.abortScaleDownDelaySeconds < 0 {
				rollout.Spec.Strategy = v1alpha1.RolloutStrategy{
					BlueGreen: &v1alpha1.BlueGreenStrategy{
						AbortScaleDownDelaySeconds: nil,
					},
				}
			}

			scaled, err := roCtx.reconcileNewReplicaSet()
			if test.failRSUpdate {
				assert.Error(t, err)
				return
			}
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

// TestReconcileNewReplicaSetAbortDelaySyncsReplicasAnnotation verifies that the abort
// scale-down-delay paths, which intentionally hold the newRS at its current size, still sync
// the desired-replicas annotation with spec.replicas. isScalingEvent() compares that
// annotation to spec.replicas, so leaving it stale would short-circuit every reconcile to
// syncReplicasOnly() (which skips traffic routing) until the scale-down deadline elapsed.
func TestReconcileNewReplicaSetAbortDelaySyncsReplicasAnnotation(t *testing.T) {
	tests := []struct {
		name              string
		deadlineAnnotated bool
	}{
		{name: "before the scale-down deadline is added", deadlineAnnotated: false},
		{name: "while waiting out the scale-down deadline", deadlineAnnotated: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stableRS := rs("foo-v1", 5, nil, noTimestamp, nil)
			newRS := rs("foo-v2", 5, nil, noTimestamp, nil)
			// HPA scaled the rollout from 5 to 4 mid-abort: the newRS desired-replicas
			// annotation (5) no longer matches spec.replicas (4)
			rollout := newBlueGreenRollout("foo", 4, nil, "", "")
			rollout.Status.Abort = true
			abortScaleDownDelaySeconds := int32(30)
			rollout.Spec.Strategy = v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{
					AbortScaleDownDelaySeconds: &abortScaleDownDelaySeconds,
				},
			}
			stableRS.Status.AvailableReplicas = 4
			if test.deadlineAnnotated {
				deadline := timeutil.Now().Add(time.Duration(abortScaleDownDelaySeconds) * time.Second).UTC().Format(time.RFC3339)
				newRS.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = deadline
			}

			fake := fake.Clientset{}
			k8sfake := k8sfake.Clientset{}
			f := newFixture(t)
			defer f.Close()
			f.objects = append(f.objects, rollout)
			f.replicaSetLister = append(f.replicaSetLister, stableRS, newRS)
			f.kubeobjects = append(f.kubeobjects, stableRS, newRS)
			_, informers, k8sInformer := f.newController(noResyncPeriodFunc)
			stopCh := make(chan struct{})
			informers.Start(stopCh)
			informers.WaitForCacheSync(stopCh)
			close(stopCh)

			roCtx := rolloutContext{
				log:      logutil.WithRollout(rollout),
				rollout:  rollout,
				newRS:    newRS,
				stableRS: stableRS,
				reconcilerBase: reconcilerBase{
					argoprojclientset:  &fake,
					kubeclientset:      &k8sfake,
					recorder:           record.NewFakeEventRecorder(),
					resyncPeriod:       15 * time.Minute,
					replicaSetInformer: k8sInformer.Apps().V1().ReplicaSets().Informer(),
				},
				pauseContext: &pauseContext{
					rollout: rollout,
				},
			}
			roCtx.enqueueRolloutAfter = func(obj any, duration time.Duration) {}

			scaled, err := roCtx.reconcileNewReplicaSet()
			assert.NoError(t, err)
			assert.False(t, scaled)

			actions := k8sfake.Actions()
			assert.NotEmpty(t, actions, "expected an update syncing the desired-replicas annotation")
			updated := actions[0].(core.UpdateAction).GetObject().(*appsv1.ReplicaSet)
			assert.Equal(t, int32(5), *updated.Spec.Replicas, "newRS must be held at scale during the abort delay")
			assert.Equal(t, "4", updated.Annotations[annotations.DesiredReplicasAnnotation], "desired-replicas annotation must track spec.replicas so isScalingEvent() terminates")
		})
	}
}

// TestAbortScaleDownDelayDoesNotWedgeScalingEvent reconciles an aborted traffic-routed canary
// that received a spec.replicas change (e.g. from an HPA) mid-abort. reconcile() short-circuits
// to syncReplicasOnly, which must terminate the scaling event by syncing the newRS
// desired-replicas annotation even though the canary is held at scale by the abort
// scale-down delay. If the annotation stayed stale, every subsequent reconcile would
// short-circuit too, freezing traffic routing at the pre-abort weight until the delay elapsed.
func TestAbortScaleDownDelayDoesNotWedgeScalingEvent(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{SetWeight: ptr.To[int32](50)},
		{Pause: &v1alpha1.RolloutPause{}},
	}
	r1 := newCanaryRollout("foo", 5, nil, steps, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(1))
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{SMI: &v1alpha1.SMITrafficRouting{}}
	r1.Spec.Strategy.Canary.CanaryService = "canary"
	r1.Spec.Strategy.Canary.StableService = "stable"
	r1.Status.ReadyReplicas = 5
	r1.Status.AvailableReplicas = 5
	r2 := bumpVersion(r1)

	// stable RS was already synced to the new size on a previous reconcile; the canary RS is
	// held at 5 by the abort scale-down delay with a stale desired-replicas annotation, so
	// isScalingEvent() is true
	rs1 := newReplicaSetWithStatus(r1, 4, 4)
	rs2 := newReplicaSetWithStatus(r2, 5, 5)
	rs1.Annotations[annotations.DesiredReplicasAnnotation] = "4"
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	canarySvc := newService("canary", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}, r1)
	stableSvc := newService("stable", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}, r1)

	r2.Status.Abort = true
	r2.Status.AbortedAt = &metav1.Time{Time: timeutil.Now().Add(-1 * time.Minute)}
	r2.Status.StableRS = rs1PodHash
	r2.Status.Canary.Weights = &v1alpha1.TrafficWeights{
		Canary: v1alpha1.WeightDestination{Weight: 50, ServiceName: "canary", PodTemplateHash: rs2PodHash},
		Stable: v1alpha1.WeightDestination{Weight: 50, ServiceName: "stable", PodTemplateHash: rs1PodHash},
	}
	// HPA scaling event during the abort
	r2.Spec.Replicas = ptr.To[int32](4)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.serviceLister = append(f.serviceLister, canarySvc, stableSvc)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	updatedRSIndex := f.expectUpdateReplicaSetAction(rs2) // desired-replicas annotation sync
	f.expectPatchReplicaSetAction(rs2)                    // scale-down-deadline added
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	updatedRS := f.getUpdatedReplicaSet(updatedRSIndex)
	assert.Equal(t, int32(5), *updatedRS.Spec.Replicas, "canary RS must be held at scale during the abort delay")
	assert.Equal(t, "4", updatedRS.Annotations[annotations.DesiredReplicasAnnotation], "desired-replicas annotation must be synced so the scaling event terminates and the next reconcile takes the full path")
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

			rollout := newBlueGreenRollout("foo", test.rolloutReplicas, nil, "active-service", "preview-service")
			rollout.Spec.Strategy.BlueGreen.ScaleDownDelayRevisionLimit = ptr.To[int32](0)
			rollout.Spec.Selector = &metav1.LabelSelector{MatchLabels: newSelector}

			activeService := newService("active-service", 80, nil, nil)
			previewService := newService("preview-service", 80, nil, nil)
			rollout.Spec.Template.Labels["foo"] = "new"

			f := newFixture(t)
			defer f.Close()
			f.objects = append(f.objects, rollout)
			f.replicaSetLister = append(f.replicaSetLister, oldRS, newRS)
			f.serviceLister = append(f.serviceLister, activeService, previewService)
			f.kubeobjects = append(f.kubeobjects, oldRS, newRS, activeService, previewService)
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

func TestIsReplicaSetReferenced(t *testing.T) {
	newRSWithPodTemplateHash := func(hash string) *appsv1.ReplicaSet {
		return &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					v1alpha1.DefaultRolloutUniqueLabelKey: hash,
				},
			},
		}
	}

	testCases := []struct {
		name           string
		status         v1alpha1.RolloutStatus
		canaryService  string
		stableService  string
		activeService  string
		previewService string
		services       []runtime.Object
		rsHash         string
		expectedResult bool
	}{
		{
			name:           "empty hash",
			status:         v1alpha1.RolloutStatus{StableRS: "abc123"},
			rsHash:         "",
			expectedResult: false,
		},
		{
			name:           "not referenced",
			status:         v1alpha1.RolloutStatus{StableRS: "abc123"},
			rsHash:         "def456",
			expectedResult: false,
		},
		{
			name:           "stable rs referenced",
			status:         v1alpha1.RolloutStatus{StableRS: "abc123"},
			rsHash:         "abc123",
			expectedResult: true,
		},
		{
			name:           "current rs referenced",
			status:         v1alpha1.RolloutStatus{CurrentPodHash: "abc123"},
			rsHash:         "abc123",
			expectedResult: true,
		},
		{
			name:           "active referenced",
			status:         v1alpha1.RolloutStatus{BlueGreen: v1alpha1.BlueGreenStatus{ActiveSelector: "abc123"}},
			rsHash:         "abc123",
			expectedResult: true,
		},
		{
			name:           "active referenced",
			status:         v1alpha1.RolloutStatus{BlueGreen: v1alpha1.BlueGreenStatus{PreviewSelector: "abc123"}},
			rsHash:         "abc123",
			expectedResult: true,
		},
		{
			name: "traffic routed canary current pod hash",
			status: v1alpha1.RolloutStatus{Canary: v1alpha1.CanaryStatus{Weights: &v1alpha1.TrafficWeights{
				Canary: v1alpha1.WeightDestination{
					PodTemplateHash: "abc123",
				},
			}}},
			rsHash:         "abc123",
			expectedResult: true,
		},
		{
			name: "traffic routed canary current pod hash",
			status: v1alpha1.RolloutStatus{Canary: v1alpha1.CanaryStatus{Weights: &v1alpha1.TrafficWeights{
				Stable: v1alpha1.WeightDestination{
					PodTemplateHash: "abc123",
				},
			}}},
			rsHash:         "abc123",
			expectedResult: true,
		},
		{
			name: "canary service still referenced",
			status: v1alpha1.RolloutStatus{
				CurrentPodHash: "abc123",
				StableRS:       "abc123",
			},
			canaryService:  "mysvc",
			services:       []runtime.Object{newService("mysvc", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "def456"}, nil)},
			rsHash:         "def456",
			expectedResult: true,
		},
		{
			name: "stable service still referenced",
			status: v1alpha1.RolloutStatus{
				CurrentPodHash: "abc123",
				StableRS:       "abc123",
			},
			stableService:  "mysvc",
			services:       []runtime.Object{newService("mysvc", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "def456"}, nil)},
			rsHash:         "def456",
			expectedResult: true,
		},
		{
			name: "active service still referenced",
			status: v1alpha1.RolloutStatus{
				CurrentPodHash: "abc123",
				StableRS:       "abc123",
			},
			activeService:  "mysvc",
			services:       []runtime.Object{newService("mysvc", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "def456"}, nil)},
			rsHash:         "def456",
			expectedResult: true,
		},
		{
			name: "preview service still referenced",
			status: v1alpha1.RolloutStatus{
				CurrentPodHash: "abc123",
				StableRS:       "abc123",
			},
			activeService:  "mysvc",
			previewService: "mysvc2",
			services:       []runtime.Object{newService("mysvc2", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "def456"}, nil)},
			rsHash:         "def456",
			expectedResult: true,
		},
		{
			name: "service not found",
			status: v1alpha1.RolloutStatus{
				CurrentPodHash: "abc123",
				StableRS:       "abc123",
			},
			activeService:  "mysvc",
			previewService: "mysvc2",
			rsHash:         "def456",
			expectedResult: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			fake := fake.Clientset{}
			k8sfake := k8sfake.NewSimpleClientset(tc.services...)
			informers := k8sinformers.NewSharedInformerFactory(k8sfake, 0)
			servicesLister := informers.Core().V1().Services().Lister()
			stopchan := make(chan struct{})
			defer close(stopchan)
			informers.Start(stopchan)
			informers.WaitForCacheSync(stopchan)

			var r *v1alpha1.Rollout
			if tc.activeService != "" {
				r = newBlueGreenRollout("test", 1, nil, tc.activeService, tc.previewService)
			} else {
				r = newCanaryRollout("test", 1, nil, nil, nil, intstr.FromInt(0), intstr.FromInt(1))
				r.Spec.Strategy.Canary.CanaryService = tc.canaryService
				r.Spec.Strategy.Canary.StableService = tc.stableService
			}
			r.Status = tc.status

			roCtx := &rolloutContext{
				rollout: r,
				log:     logutil.WithRollout(r),
				reconcilerBase: reconcilerBase{
					servicesLister:    servicesLister,
					argoprojclientset: &fake,
					kubeclientset:     k8sfake,
					recorder:          record.NewFakeEventRecorder(),
				},
			}
			rs := newRSWithPodTemplateHash(tc.rsHash)
			stillReferenced := roCtx.isReplicaSetReferenced(rs)

			assert.Equal(
				t,
				tc.expectedResult,
				stillReferenced,
			)
		})
	}
}

func TestIsReplicaSetReferencedByIstioDestinationRule(t *testing.T) {
	const testDestRuleName = "test-destrule"

	newRSWithPodTemplateHash := func(hash string) *appsv1.ReplicaSet {
		return &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					v1alpha1.DefaultRolloutUniqueLabelKey: hash,
				},
			},
		}
	}

	// Helper to create a rollout with Istio traffic routing and destination rule
	newRolloutWithIstioDestinationRule := func(destRuleName string) *v1alpha1.Rollout {
		r := newCanaryRollout("test", 1, nil, nil, nil, intstr.FromInt(0), intstr.FromInt(1))
		r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			Istio: &v1alpha1.IstioTrafficRouting{
				VirtualService: &v1alpha1.IstioVirtualService{
					Name: "vsvc",
				},
				DestinationRule: &v1alpha1.IstioDestinationRule{
					Name:             destRuleName,
					CanarySubsetName: "canary",
					StableSubsetName: "stable",
				},
			},
		}
		return r
	}

	// Helper to create a rollout with Istio virtual service only (no destination rule)
	newRolloutWithIstioVSvcOnly := func() *v1alpha1.Rollout {
		r := newCanaryRollout("test", 1, nil, nil, nil, intstr.FromInt(0), intstr.FromInt(1))
		r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			Istio: &v1alpha1.IstioTrafficRouting{
				VirtualService: &v1alpha1.IstioVirtualService{
					Name: "vsvc",
				},
			},
		}
		return r
	}

	// Helper to create destination rule YAML with given subsets
	newDestinationRuleYAML := func(name, stableHash, canaryHash string) string {
		return fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: %s
  namespace: default
spec:
  host: test-service
  subsets:
  - name: stable
    labels:
      rollouts-pod-template-hash: %s
  - name: canary
    labels:
      rollouts-pod-template-hash: %s
`, name, stableHash, canaryHash)
	}

	// Helper to create a rollout with TrafficRouting but no Istio config
	newRolloutWithTrafficRoutingNoIstio := func() *v1alpha1.Rollout {
		r := newCanaryRollout("test", 1, nil, nil, nil, intstr.FromInt(0), intstr.FromInt(1))
		r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
		return r
	}

	testCases := []struct {
		name              string
		rollout           *v1alpha1.Rollout
		destinationRule   string
		rsHash            string
		expectedResult    bool
		noIstioController bool
	}{
		{
			name:           "no istio traffic routing configured",
			rollout:        newCanaryRollout("test", 1, nil, nil, nil, intstr.FromInt(0), intstr.FromInt(1)),
			rsHash:         "abc123",
			expectedResult: false,
		},
		{
			name:           "traffic routing configured but no istio",
			rollout:        newRolloutWithTrafficRoutingNoIstio(),
			rsHash:         "abc123",
			expectedResult: false,
		},
		{
			name:           "no destination rule configured",
			rollout:        newRolloutWithIstioVSvcOnly(),
			rsHash:         "abc123",
			expectedResult: false,
		},
		{
			name:            "destination rule references hash - should return true",
			rollout:         newRolloutWithIstioDestinationRule(testDestRuleName),
			destinationRule: newDestinationRuleYAML(testDestRuleName, "abc123", "def456"),
			rsHash:          "abc123",
			expectedResult:  true,
		},
		{
			name:            "destination rule does not reference hash - should return false",
			rollout:         newRolloutWithIstioDestinationRule(testDestRuleName),
			destinationRule: newDestinationRuleYAML(testDestRuleName, "abc123", "def456"),
			rsHash:          "xyz789",
			expectedResult:  false,
		},
		{
			name:            "destination rule not found - should return false",
			rollout:         newRolloutWithIstioDestinationRule("non-existent-destrule"),
			destinationRule: newDestinationRuleYAML(testDestRuleName, "abc123", "def456"),
			rsHash:          "abc123",
			expectedResult:  false,
		},
		{
			name:              "no istio controller - should return false",
			rollout:           newRolloutWithIstioDestinationRule(testDestRuleName),
			noIstioController: true,
			rsHash:            "abc123",
			expectedResult:    false,
		},
		{
			name:            "canary subset references hash - should return true",
			rollout:         newRolloutWithIstioDestinationRule(testDestRuleName),
			destinationRule: newDestinationRuleYAML(testDestRuleName, "stable-hash", "canary-hash"),
			rsHash:          "canary-hash",
			expectedResult:  true,
		},
		{
			name:    "destination rule with no subsets - should return false",
			rollout: newRolloutWithIstioDestinationRule(testDestRuleName),
			destinationRule: `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: ` + testDestRuleName + `
  namespace: default
spec:
  host: test-service
`,
			rsHash:         "abc123",
			expectedResult: false,
		},
		{
			name:    "destination rule with subset missing labels - should return false",
			rollout: newRolloutWithIstioDestinationRule(testDestRuleName),
			destinationRule: `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: ` + testDestRuleName + `
  namespace: default
spec:
  host: test-service
  subsets:
  - name: stable
`,
			rsHash:         "abc123",
			expectedResult: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var objs []runtime.Object
			if tc.destinationRule != "" {
				dRule := unstructuredutil.StrToUnstructuredUnsafe(tc.destinationRule)
				objs = append(objs, dRule)
			}

			dynamicClient := testutil.NewFakeDynamicClient(objs...)
			var istioController *istio.IstioController

			if !tc.noIstioController {
				druleGVR := istioutil.GetIstioDestinationRuleGVR()
				vsvcGVR := istioutil.GetIstioVirtualServiceGVR()
				dynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 0)
				destinationRuleInformer := dynamicInformerFactory.ForResource(druleGVR).Informer()
				virtualServiceInformer := dynamicInformerFactory.ForResource(vsvcGVR).Informer()
				stopCh := make(chan struct{})
				dynamicInformerFactory.Start(stopCh)
				dynamicInformerFactory.WaitForCacheSync(stopCh)
				close(stopCh)
				druleLister := dynamiclister.New(destinationRuleInformer.GetIndexer(), druleGVR)

				istioController = &istio.IstioController{
					IstioControllerConfig: istio.IstioControllerConfig{
						DynamicClientSet:        dynamicClient,
						DestinationRuleInformer: destinationRuleInformer,
						VirtualServiceInformer:  virtualServiceInformer,
					},
				}
				istioController.DestinationRuleLister = druleLister
			}

			roCtx := &rolloutContext{
				rollout: tc.rollout,
				log:     logutil.WithRollout(tc.rollout),
				reconcilerBase: reconcilerBase{
					argoprojclientset: &fake.Clientset{},
					kubeclientset:     k8sfake.NewSimpleClientset(),
					recorder:          record.NewFakeEventRecorder(),
					IstioController:   istioController,
				},
			}

			rs := newRSWithPodTemplateHash(tc.rsHash)
			result := roCtx.isReplicaSetReferencedByIstioDestinationRule(tc.rsHash)

			assert.Equal(t, tc.expectedResult, result, "Test case: %s", tc.name)

			// Also test via isReplicaSetReferenced which should call the Istio check
			if tc.expectedResult {
				stillReferenced := roCtx.isReplicaSetReferenced(rs)
				assert.True(t, stillReferenced, "isReplicaSetReferenced should return true when DestinationRule references hash")
			}
		})
	}
}

func TestScaleDownProgressively(t *testing.T) {

	tests := []struct {
		name                       string
		deploymentReplicas         int32
		newRSReplicas              int
		newRSRevision              string
		rolloutReplicas            int32
		rolloutReadyReplicas       int32
		rolloutPhase               v1alpha1.RolloutPhase
		abortScaleDownDelaySeconds int32
		expectedDeploymentReplicas int32
	}{
		{
			name:                       "Scale down deployment",
			deploymentReplicas:         5,
			newRSReplicas:              5,
			newRSRevision:              "1",
			rolloutReplicas:            5,
			rolloutReadyReplicas:       3,
			rolloutPhase:               v1alpha1.RolloutPhaseProgressing,
			abortScaleDownDelaySeconds: 0,
			expectedDeploymentReplicas: 2,
		},
		{
			name:                       "Scale up deployment",
			deploymentReplicas:         0,
			newRSReplicas:              5,
			newRSRevision:              "1",
			rolloutReplicas:            5,
			rolloutReadyReplicas:       1,
			rolloutPhase:               v1alpha1.RolloutPhaseProgressing,
			abortScaleDownDelaySeconds: 0,
			expectedDeploymentReplicas: 4,
		},
		{
			name:                       "Do not scale deployment",
			deploymentReplicas:         5,
			newRSReplicas:              5,
			newRSRevision:              "2",
			rolloutReplicas:            5,
			rolloutReadyReplicas:       3,
			rolloutPhase:               v1alpha1.RolloutPhaseHealthy,
			abortScaleDownDelaySeconds: 0,
			expectedDeploymentReplicas: 5,
		},
		{
			name:                       "Rollout healthy - Deployment scaled to 0",
			deploymentReplicas:         2,
			newRSReplicas:              5,
			newRSRevision:              "1",
			rolloutReplicas:            5,
			rolloutReadyReplicas:       5,
			rolloutPhase:               v1alpha1.RolloutPhaseHealthy,
			abortScaleDownDelaySeconds: 0,
			expectedDeploymentReplicas: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := createScaleDownRolloutContext(v1alpha1.ScaleDownProgressively, test.deploymentReplicas, true, nil)
			ctx.rollout.Spec.Strategy = v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{
					AbortScaleDownDelaySeconds: &test.abortScaleDownDelaySeconds,
				},
			}
			ctx.newRS = rs("foo-v2", test.newRSReplicas, nil, noTimestamp, nil)
			ctx.newRS.ObjectMeta.Annotations[annotations.RevisionAnnotation] = test.newRSRevision
			ctx.pauseContext.removeAbort = true
			ctx.rollout.Spec.Replicas = &test.rolloutReplicas
			ctx.rollout.Status.ReadyReplicas = test.rolloutReadyReplicas
			ctx.rollout.Status.Phase = test.rolloutPhase // Set the appropriate phase

			_, err := ctx.reconcileNewReplicaSet()
			assert.Nil(t, err)
			k8sfakeClient := ctx.kubeclientset.(*k8sfake.Clientset)
			updatedDeployment, err := k8sfakeClient.AppsV1().Deployments("default").Get(context.TODO(), "workload-test", metav1.GetOptions{})
			assert.Nil(t, err)
			assert.Equal(t, test.expectedDeploymentReplicas, *updatedDeployment.Spec.Replicas)
		})
	}
}

func TestStaleReplicaSet(t *testing.T) {
	newRS := func(name, resourceVersion string) *appsv1.ReplicaSet {
		return &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Namespace: metav1.NamespaceDefault, Name: name, ResourceVersion: resourceVersion}}
	}
	base := reconcilerBase{replicaSetVersionTracker: resourceversionutil.NewTracker()}
	// The controller wrote to foo and got resourceVersion 100 back. It never wrote to bar.
	base.recordReplicaSetWrite(newRS("foo", "100"))

	tests := []struct {
		name      string
		informer  []*appsv1.ReplicaSet
		wantStale string
	}{
		{name: "the informer has not observed the write", informer: []*appsv1.ReplicaSet{newRS("bar", "5"), newRS("foo", "99")}, wantStale: "foo"},
		{name: "resourceVersions are compared as numbers", informer: []*appsv1.ReplicaSet{newRS("foo", "9")}, wantStale: "foo"},
		{name: "the informer has observed the write", informer: []*appsv1.ReplicaSet{newRS("bar", "5"), newRS("foo", "100")}},
		{name: "the informer is ahead of the write", informer: []*appsv1.ReplicaSet{newRS("foo", "250")}},
		{name: "a ReplicaSet that was not written to", informer: []*appsv1.ReplicaSet{newRS("bar", "5")}},
		{name: "the same name in another namespace", informer: []*appsv1.ReplicaSet{{ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: "foo", ResourceVersion: "99"}}}},
		// A ReplicaSet that was written to and is not in the informer may have been deleted: it does not hold up
		// the reconcile.
		{name: "the ReplicaSet is not in the informer", informer: nil},
		{name: "a ReplicaSet without a resourceVersion", informer: []*appsv1.ReplicaSet{newRS("foo", "")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stale := base.staleReplicaSet(test.informer)
			if test.wantStale == "" {
				assert.Nil(t, stale)
				return
			}
			require.NotNil(t, stale)
			assert.Equal(t, test.wantStale, stale.Name)
		})
	}

	t.Run("forgotten after the ReplicaSet is deleted", func(t *testing.T) {
		base := reconcilerBase{replicaSetVersionTracker: resourceversionutil.NewTracker()}
		base.recordReplicaSetWrite(newRS("foo", "100"))
		base.forgetReplicaSet(newRS("foo", "100"))
		assert.Nil(t, base.staleReplicaSet([]*appsv1.ReplicaSet{newRS("foo", "99")}))
	})
	t.Run("without a tracker nothing is stale", func(t *testing.T) {
		base := reconcilerBase{}
		base.recordReplicaSetWrite(newRS("foo", "100"))
		base.forgetReplicaSet(newRS("foo", "100"))
		assert.Nil(t, base.staleReplicaSet([]*appsv1.ReplicaSet{newRS("foo", "99")}))
	})
}

// Every write to a ReplicaSet is recorded with the resourceVersion the API server returned for it.
func TestReplicaSetWritesAreRecorded(t *testing.T) {
	deadline := timeutil.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	tests := []struct {
		name  string
		write func(t *testing.T, roCtx *rolloutContext, rs *appsv1.ReplicaSet)
	}{
		{
			name: "updateReplicaSet",
			write: func(t *testing.T, roCtx *rolloutContext, rs *appsv1.ReplicaSet) {
				rs.Spec.MinReadySeconds = 5
				_, err := roCtx.updateReplicaSet(context.Background(), rs)
				require.NoError(t, err)
			},
		},
		{
			name: "addScaleDownDelay",
			write: func(t *testing.T, roCtx *rolloutContext, rs *appsv1.ReplicaSet) {
				delete(rs.Annotations, v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey)
				require.NoError(t, roCtx.addScaleDownDelay(rs, 30*time.Second))
			},
		},
		{
			name: "removeScaleDownDelay",
			write: func(t *testing.T, roCtx *rolloutContext, rs *appsv1.ReplicaSet) {
				require.NoError(t, roCtx.removeScaleDownDelay(rs))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ro := newCanaryRollout("foo", 10, nil, nil, ptr.To[int32](0), intstr.FromInt32(1), intstr.FromInt32(0))
			rs := newReplicaSetWithStatus(ro, 10, 10)
			rs.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = deadline
			rs.ResourceVersion = "250"
			roCtx := &rolloutContext{
				log:     logutil.WithRollout(ro),
				rollout: ro,
				reconcilerBase: reconcilerBase{
					kubeclientset:            k8sfake.NewSimpleClientset(rs.DeepCopy()),
					recorder:                 record.NewFakeEventRecorder(),
					replicaSetVersionTracker: resourceversionutil.NewTracker(),
				},
				pauseContext: &pauseContext{rollout: ro},
			}
			older := rs.DeepCopy()
			older.ResourceVersion = "249"
			require.Nil(t, roCtx.staleReplicaSet([]*appsv1.ReplicaSet{older}))

			test.write(t, roCtx, rs.DeepCopy())

			assert.Equal(t, older, roCtx.staleReplicaSet([]*appsv1.ReplicaSet{older}), "a copy from before the write is stale")
			assert.Nil(t, roCtx.staleReplicaSet([]*appsv1.ReplicaSet{rs}), "a copy with the resourceVersion of the write is not")
		})
	}
}

// The record of a write is dropped when the ReplicaSet is deleted, by this controller or by anything else, so that
// the tracker does not grow with every ReplicaSet the controller has ever written to.
func TestDeletedReplicaSetIsForgotten(t *testing.T) {
	newOldReplicaSet := func() (*v1alpha1.Rollout, *appsv1.ReplicaSet, *appsv1.ReplicaSet) {
		r1 := newCanaryRollout("foo", 1, ptr.To[int32](0), nil, nil, intstr.FromInt32(1), intstr.FromInt32(0))
		r2 := bumpVersion(r1)
		oldRS := newReplicaSetWithStatus(r1, 0, 0)
		oldRS.ResourceVersion = "100"
		older := oldRS.DeepCopy()
		older.ResourceVersion = "99"
		return r2, oldRS, older
	}

	t.Run("deleted by the revision history limit", func(t *testing.T) {
		ro, oldRS, older := newOldReplicaSet()
		roCtx := &rolloutContext{
			log:     logutil.WithRollout(ro),
			rollout: ro,
			reconcilerBase: reconcilerBase{
				kubeclientset:            k8sfake.NewSimpleClientset(oldRS.DeepCopy()),
				argoprojclientset:        &fake.Clientset{},
				recorder:                 record.NewFakeEventRecorder(),
				replicaSetVersionTracker: resourceversionutil.NewTracker(),
			},
			pauseContext: &pauseContext{rollout: ro},
		}
		roCtx.recordReplicaSetWrite(oldRS)
		require.NotNil(t, roCtx.staleReplicaSet([]*appsv1.ReplicaSet{older}))

		require.NoError(t, roCtx.reconcileRevisionHistoryLimit([]*appsv1.ReplicaSet{oldRS}))

		_, err := roCtx.kubeclientset.AppsV1().ReplicaSets(oldRS.Namespace).Get(context.Background(), oldRS.Name, metav1.GetOptions{})
		require.True(t, k8serrors.IsNotFound(err), "the ReplicaSet should have been deleted: %v", err)
		assert.Nil(t, roCtx.staleReplicaSet([]*appsv1.ReplicaSet{older}))
	})

	t.Run("delete event from the informer", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()
		ro, oldRS, older := newOldReplicaSet()
		f.kubeobjects = append(f.kubeobjects, oldRS)
		f.rolloutLister = append(f.rolloutLister, ro)
		f.objects = append(f.objects, ro)
		c, _, k8sI := f.newController(noResyncPeriodFunc)
		stopCh := make(chan struct{})
		defer close(stopCh)
		k8sI.Start(stopCh)
		require.True(t, cache.WaitForCacheSync(stopCh, c.replicaSetSynced))

		c.recordReplicaSetWrite(oldRS)
		require.NotNil(t, c.staleReplicaSet([]*appsv1.ReplicaSet{older}))

		require.NoError(t, f.kubeclient.AppsV1().ReplicaSets(oldRS.Namespace).Delete(context.Background(), oldRS.Name, metav1.DeleteOptions{}))

		assert.Eventually(t, func() bool {
			return c.staleReplicaSet([]*appsv1.ReplicaSet{older}) == nil
		}, 5*time.Second, 10*time.Millisecond)
	})
}

// A reconcile does not start from a ReplicaSet that is older than the controller's own last write to it.
//
// The Rollout is a traffic-routed canary. V1 was its stable ReplicaSet until V2 was fully promoted, and the
// controller then gave V1 a scale-down-deadline, with a patch. V3 was applied right after. The informer has not
// delivered that patch yet, so V1 looks like an intermediate ReplicaSet without a deadline, and those are scaled to
// zero at once. The reconcile has to wait instead.
func TestSyncHandlerWaitsForReplicaSetInformerToObserveOwnWrite(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{SetWeight: ptr.To[int32](10)}, {Pause: &v1alpha1.RolloutPause{}}}
	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](1), intstr.FromInt32(1), intstr.FromInt32(1))
	r1.Spec.Strategy.Canary.CanaryService = "canary"
	r1.Spec.Strategy.Canary.StableService = "stable"
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{SMI: &v1alpha1.SMITrafficRouting{}}
	r2 := bumpVersion(r1)
	r3 := bumpVersion(r2)

	rs2 := newReplicaSetWithStatus(r2, 1, 1) // stable
	rs3 := newReplicaSetWithStatus(r3, 1, 1) // canary, at the scale of its step
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs3PodHash := rs3.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	// V1 as the informer has it, and as it is in the API after the controller's patch.
	cachedRS1 := newReplicaSetWithStatus(r1, 1, 1)
	cachedRS1.ResourceVersion = "100"
	liveRS1 := cachedRS1.DeepCopy()
	liveRS1.ResourceVersion = "101"
	deadline := timeutil.Now().Add(30 * time.Second).UTC().Format(time.RFC3339)
	liveRS1.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = deadline

	r3 = updateCanaryRolloutStatus(r3, rs2PodHash, 3, 1, 3, false)
	r3.Status.ObservedGeneration = strconv.Itoa(int(r3.Generation))
	r3.Status.Canary.Weights = &v1alpha1.TrafficWeights{
		Canary: v1alpha1.WeightDestination{Weight: 10, ServiceName: "canary", PodTemplateHash: rs3PodHash},
		Stable: v1alpha1.WeightDestination{Weight: 90, ServiceName: "stable", PodTemplateHash: rs2PodHash},
	}
	canarySvc := newService("canary", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs3PodHash}, r3)
	stableSvc := newService("stable", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}, r3)

	f.kubeobjects = append(f.kubeobjects, liveRS1, rs2, rs3, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, cachedRS1, rs2, rs3)
	f.serviceLister = append(f.serviceLister, canarySvc, stableSvc)
	f.rolloutLister = append(f.rolloutLister, r3)
	f.objects = append(f.objects, r3)

	// The informers are not started: they hold what the fixture put in them.
	c, _, k8sI := f.newController(noResyncPeriodFunc)
	c.recordReplicaSetWrite(liveRS1)
	getLiveRS1 := func() *appsv1.ReplicaSet {
		rs, err := f.kubeclient.AppsV1().ReplicaSets(liveRS1.Namespace).Get(context.Background(), liveRS1.Name, metav1.GetOptions{})
		require.NoError(t, err)
		return rs
	}
	writes := func(actions []core.Action) []string {
		var writes []string
		for _, action := range actions {
			if verb := action.GetVerb(); verb != "get" && verb != "list" && verb != "watch" {
				writes = append(writes, verb+" "+action.GetResource().Resource)
			}
		}
		return writes
	}

	// The informer is behind our write: the reconcile is put off, and nothing is written.
	err := c.syncHandler(context.Background(), getKey(r3, t))
	require.ErrorIs(t, err, controllerutil.StaleCacheError)
	assert.Empty(t, writes(f.kubeclient.Actions()))
	assert.Empty(t, writes(f.client.Actions()))
	assert.Equal(t, int32(1), *getLiveRS1().Spec.Replicas)
	assert.Equal(t, deadline, getLiveRS1().Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey])

	// The informer catches up: the reconcile runs, sees the deadline and leaves V1 scaled up.
	require.NoError(t, k8sI.Apps().V1().ReplicaSets().Informer().GetIndexer().Update(liveRS1))
	err = c.syncHandler(context.Background(), getKey(r3, t))
	require.NoError(t, err)
	assert.Equal(t, int32(1), *getLiveRS1().Spec.Replicas)
	assert.Equal(t, deadline, getLiveRS1().Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey])
}
