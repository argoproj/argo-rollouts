package rollout

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/record"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

type stableScaleDownTest struct {
	name             string
	noPolicy         bool
	abort            bool
	hold             string
	stableReplicas   int32
	stableWeight     int32
	expectedReplicas int32
	// expectedHold is the replica count of the hold left on the stable RS, nil for no hold
	expectedHold *int32
	// expectRestamp means the hold deadline was set to now+delay during this reconcile
	expectRestamp bool
}

const stableScaleDownTestDelay = 60 * time.Second

func TestReconcileCanaryStableReplicaSetWithStableScaleDownPolicy(t *testing.T) {
	hold := func(in time.Duration, replicas int32) string {
		b, err := json.Marshal(stableScaleDownHold{Deadline: metav1.NewTime(timeutil.Now().Add(in)), Replicas: replicas})
		require.NoError(t, err)
		return string(b)
	}
	tests := []stableScaleDownTest{
		{name: "no policy scales down immediately", noPolicy: true, stableWeight: 0, expectedReplicas: 0},
		{name: "first scale-down is held", stableWeight: 50, expectedReplicas: 10, expectedHold: ptr.To[int32](5), expectRestamp: true},
		{name: "held until the deadline", hold: hold(30*time.Second, 5), stableWeight: 50, expectedReplicas: 10, expectedHold: ptr.To[int32](5)},
		{name: "lower desired count restarts the hold", hold: hold(5*time.Second, 5), stableWeight: 0, expectedReplicas: 10, expectedHold: ptr.To[int32](0), expectRestamp: true},
		{name: "higher desired count keeps the deadline", hold: hold(30*time.Second, 5), stableWeight: 80, expectedReplicas: 10, expectedHold: ptr.To[int32](8)},
		{name: "drop after a rise restarts the hold", hold: hold(30*time.Second, 8), stableWeight: 70, expectedReplicas: 10, expectedHold: ptr.To[int32](7), expectRestamp: true},
		{name: "abort shifting back to stable drops an expired hold", abort: true, hold: hold(-time.Second, 5), stableWeight: 80, expectedReplicas: 10},
		{name: "abort with traffic back on stable holds a scale-in", abort: true, stableReplicas: 12, stableWeight: 100, expectedReplicas: 12, expectedHold: ptr.To[int32](10), expectRestamp: true},
		{name: "scales down after the deadline and drops the hold", hold: hold(-time.Second, 0), stableWeight: 0, expectedReplicas: 0},
		{name: "partial scale-down after the deadline drops the hold", hold: hold(-time.Second, 5), stableWeight: 50, expectedReplicas: 5},
		{name: "withdrawn scale-down drops the hold", hold: hold(30*time.Second, 5), stableWeight: 100, expectedReplicas: 10},
		{name: "unreadable hold is replaced", hold: "garbage", stableWeight: 50, expectedReplicas: 10, expectedHold: ptr.To[int32](5), expectRestamp: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			roCtx, client, enqueuedAfter := newStableScaleDownTestContext(t, test)

			_, err := roCtx.reconcileCanaryStableReplicaSet()
			require.NoError(t, err)

			live, err := client.AppsV1().ReplicaSets(roCtx.stableRS.Namespace).Get(context.TODO(), roCtx.stableRS.Name, metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, test.expectedReplicas, *live.Spec.Replicas)
			liveHold := getStableScaleDownHold(live)
			if test.expectedHold == nil {
				assert.NotContains(t, live.Annotations, v1alpha1.DefaultStableScaleDownHoldAnnotationKey)
				return
			}
			require.NotNil(t, liveHold)
			assert.Equal(t, *test.expectedHold, liveHold.Replicas)
			assert.Positive(t, *enqueuedAfter)
			if test.expectRestamp {
				assert.WithinDuration(t, timeutil.Now().Add(stableScaleDownTestDelay), liveHold.Deadline.Time, 2*time.Second)
			} else {
				var initial stableScaleDownHold
				require.NoError(t, json.Unmarshal([]byte(test.hold), &initial))
				assert.True(t, initial.Deadline.Equal(&liveHold.Deadline))
			}
		})
	}
}

func TestStableScaleDownHoldPatchFailure(t *testing.T) {
	failPatches := func(client *k8sfake.Clientset) {
		client.PrependReactor("patch", "replicasets", func(action core.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("patch failed")
		})
	}
	t.Run("hold on the stable", func(t *testing.T) {
		roCtx, client, _ := newStableScaleDownTestContext(t, stableScaleDownTest{stableWeight: 50})
		failPatches(client)
		_, err := roCtx.reconcileCanaryStableReplicaSet()
		require.ErrorContains(t, err, "patch failed")
		assert.Equal(t, int32(10), *roCtx.stableRS.Spec.Replicas)
	})
	t.Run("leftover hold on the new RS", func(t *testing.T) {
		roCtx, client, _ := newStableScaleDownTestContext(t, stableScaleDownTest{stableWeight: 100})
		roCtx.newRS.Annotations[v1alpha1.DefaultStableScaleDownHoldAnnotationKey] = `{"deadline":"2000-01-01T00:00:00Z","replicas":0}`
		failPatches(client)
		_, err := roCtx.reconcileCanaryReplicaSets()
		require.ErrorContains(t, err, "patch failed")
	})
}

func newStableScaleDownTestContext(t *testing.T, test stableScaleDownTest) (*rolloutContext, *k8sfake.Clientset, *time.Duration) {
	ro := newCanaryRollout("foo", 10, nil, nil, nil, intstr.FromInt(1), intstr.FromInt(0))
	ro.Spec.Strategy.Canary.DynamicStableScale = true
	ro.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{SMI: &v1alpha1.SMITrafficRouting{}}
	if !test.noPolicy {
		ro.Spec.Strategy.Canary.StableScaleDownPolicy = &v1alpha1.StableScaleDownPolicy{DelaySeconds: ptr.To(int32(stableScaleDownTestDelay.Seconds()))}
	}
	if test.abort {
		ro.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{SetWeight: ptr.To[int32](20)}, {SetWeight: ptr.To[int32](50)}}
		ro.Status.Abort = true
	}
	stableRS := newStableScaleDownTestRS("foo-stable", max(test.stableReplicas, 10))
	newRS := newStableScaleDownTestRS("foo-canary", 10)
	if test.hold != "" {
		stableRS.Annotations[v1alpha1.DefaultStableScaleDownHoldAnnotationKey] = test.hold
	}
	ro.Status.StableRS = "stable"
	ro.Status.CurrentPodHash = "canary"
	ro.Status.Canary.Weights = &v1alpha1.TrafficWeights{
		Stable: v1alpha1.WeightDestination{Weight: test.stableWeight},
		Canary: v1alpha1.WeightDestination{Weight: 100 - test.stableWeight},
	}

	client := k8sfake.NewSimpleClientset(stableRS, newRS)
	var enqueuedAfter time.Duration
	roCtx := &rolloutContext{
		rollout:      ro,
		stableRS:     stableRS,
		newRS:        newRS,
		log:          log.WithField("test", t.Name()),
		pauseContext: &pauseContext{rollout: ro},
		reconcilerBase: reconcilerBase{
			kubeclientset: client,
			recorder:      record.NewFakeEventRecorder(),
			resyncPeriod:  10 * time.Minute,
			enqueueRolloutAfter: func(obj any, d time.Duration) {
				enqueuedAfter = d
			},
		},
	}
	return roCtx, client, &enqueuedAfter
}

// After promotion, dynamicStableScale scales the previous stable to zero at once. With
// stableScaleDownPolicy it is kept for delaySeconds instead, the way scaleDownDelaySeconds keeps it
// without dynamicStableScale. A hold left on the new stable is removed.
func TestCanaryDynamicStableScaleKeepsPreviousStableAfterPromotion(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newCanaryRollout("foo", 1, nil, []v1alpha1.CanaryStep{{
		SetWeight: ptr.To[int32](100),
	}}, ptr.To[int32](0), intstr.FromInt(1), intstr.FromInt(1))
	r1.Spec.Strategy.Canary.CanaryService = "canary"
	r1.Spec.Strategy.Canary.StableService = "stable"
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		SMI: &v1alpha1.SMITrafficRouting{},
	}
	r1.Spec.Strategy.Canary.DynamicStableScale = true
	r1.Spec.Strategy.Canary.StableScaleDownPolicy = &v1alpha1.StableScaleDownPolicy{DelaySeconds: ptr.To[int32](60)}
	r2 := bumpVersion(r1)
	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs2.Annotations[v1alpha1.DefaultStableScaleDownHoldAnnotationKey] = `{"deadline":"2000-01-01T00:00:00Z","replicas":0}`
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	r2 = updateCanaryRolloutStatus(r2, rs2PodHash, 2, 1, 2, false)
	r2.Status.ObservedGeneration = strconv.Itoa(int(r2.Generation))
	r2.Status.CurrentStepIndex = ptr.To[int32](1)
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)
	completedCondition, _ := newCompletedCondition(true)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)
	_, r2.Status.Canary.Weights = calculateWeightStatus(r2, rs2PodHash, rs2PodHash, 0)
	finishedAt := timeutil.MetaNow()
	promoted := v1alpha1.CompletionStatusPromoted
	r2.Status.Duration.FinishedAt = &finishedAt
	r2.Status.Duration.CompletionStatus = &promoted

	selector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	canarySvc := newService("canary", 80, selector, r2)
	stableSvc := newService("stable", 80, selector, r2)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, canarySvc, stableSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs2Patch := f.expectPatchReplicaSetAction(rs2) // remove the leftover hold
	rs1Patch := f.expectPatchReplicaSetAction(rs1) // set scale-down-deadline annotation
	f.run(getKey(r2, t))

	assert.JSONEq(t, `{"metadata":{"annotations":{"`+v1alpha1.DefaultStableScaleDownHoldAnnotationKey+`":null}}}`, string(f.kubeActionAt(rs2Patch).(core.PatchAction).GetPatch()))
	f.verifyPatchedReplicaSet(rs1Patch, 60)
}

func newStableScaleDownTestRS(name string, replicas int32) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   metav1.NamespaceDefault,
			Annotations: map[string]string{},
		},
		Spec: appsv1.ReplicaSetSpec{Replicas: ptr.To(replicas)},
		Status: appsv1.ReplicaSetStatus{
			Replicas:          replicas,
			AvailableReplicas: replicas,
		},
	}
}
