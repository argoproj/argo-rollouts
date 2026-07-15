package rollout

import (
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

func TestApplyStableScaleDownPolicyInactiveWithoutPolicy(t *testing.T) {
	ro := newStableScaleDownTestRollout(false, nil)
	stableRS := newStableScaleDownTestRS(2, nil)
	roCtx := newStableScaleDownTestContext(t, ro, stableRS)

	desired, held, err := roCtx.applyStableScaleDownPolicy(0)
	require.NoError(t, err)
	assert.False(t, held)
	assert.Equal(t, int32(0), desired)
}

func TestApplyStableScaleDownPolicyHoldsOnScaleDown(t *testing.T) {
	ro := newStableScaleDownTestRollout(true, ptr.To[int32](30))
	stableRS := newStableScaleDownTestRS(2, nil)
	roCtx := newStableScaleDownTestContext(t, ro, stableRS)

	desired, held, err := roCtx.applyStableScaleDownPolicy(0)
	require.NoError(t, err)
	assert.True(t, held)
	assert.Equal(t, int32(2), desired)
}

func TestApplyStableScaleDownPolicyAllowsScaleDownAfterDeadline(t *testing.T) {
	ro := newStableScaleDownTestRollout(true, ptr.To[int32](30))
	past := timeutil.MetaNow().Add(-time.Second).UTC().Format(time.RFC3339)
	stableRS := newStableScaleDownTestRS(2, map[string]string{
		v1alpha1.DefaultStableScaleDownDeadlineAnnotationKey: past,
	})
	roCtx := newStableScaleDownTestContext(t, ro, stableRS)

	desired, held, err := roCtx.applyStableScaleDownPolicy(0)
	require.NoError(t, err)
	assert.False(t, held)
	assert.Equal(t, int32(0), desired)
}

func TestApplyStableScaleDownPolicyClearsDeadlineOnScaleUp(t *testing.T) {
	ro := newStableScaleDownTestRollout(true, ptr.To[int32](30))
	future := timeutil.MetaNow().Add(time.Minute).UTC().Format(time.RFC3339)
	stableRS := newStableScaleDownTestRS(2, map[string]string{
		v1alpha1.DefaultStableScaleDownDeadlineAnnotationKey: future,
	})
	roCtx := newStableScaleDownTestContext(t, ro, stableRS)

	desired, held, err := roCtx.applyStableScaleDownPolicy(4)
	require.NoError(t, err)
	assert.False(t, held)
	assert.Equal(t, int32(4), desired)
	assert.NotContains(t, stableRS.Annotations, v1alpha1.DefaultStableScaleDownDeadlineAnnotationKey)
}

func newStableScaleDownTestRollout(withPolicy bool, delay *int32) *v1alpha1.Rollout {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{Name: "ro", Namespace: "default"},
		Spec: v1alpha1.RolloutSpec{
			Replicas: ptr.To[int32](5),
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					DynamicStableScale: true,
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Istio: &v1alpha1.IstioTrafficRouting{},
					},
				},
			},
		},
	}
	if withPolicy {
		ro.Spec.Strategy.Canary.StableScaleDownPolicy = &v1alpha1.StableScaleDownPolicy{
			DelaySeconds: delay,
		}
	}
	return ro
}

func newStableScaleDownTestRS(replicas int32, annotations map[string]string) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "stable-rs",
			Namespace:   "default",
			Annotations: annotations,
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: ptr.To(replicas),
		},
	}
}

func newStableScaleDownTestContext(t *testing.T, ro *v1alpha1.Rollout, stableRS *appsv1.ReplicaSet) *rolloutContext {
	t.Helper()
	k8sfake := k8sfake.NewSimpleClientset(stableRS)
	roCtx := &rolloutContext{
		rollout:  ro,
		stableRS: stableRS,
		log:      log.WithField("test", t.Name()),
		reconcilerBase: reconcilerBase{
			kubeclientset: k8sfake,
			resyncPeriod:  time.Minute,
		},
	}
	roCtx.enqueueRolloutAfter = func(obj any, duration time.Duration) {}
	return roCtx
}
