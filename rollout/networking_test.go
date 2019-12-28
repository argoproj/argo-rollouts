package rollout

import (
	"fmt"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
)

type FakeNetworkingReconciler struct {
	errMessage                 string
	controllerSetDesiredWeight int32
}

func (r *FakeNetworkingReconciler) SetDesiredWeight(desiredWeight int32) {
	r.controllerSetDesiredWeight = desiredWeight
}

func (r *FakeNetworkingReconciler) Reconcile() error {
	if r.errMessage != "" {
		return fmt.Errorf(r.errMessage)
	}
	return nil
}

func (r *FakeNetworkingReconciler) Type() string {
	return "fake"
}

func TestRolloutUseDesiredWeight(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(10),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.Networking = &v1alpha1.RolloutNetworking{}
	f.fakeNetworking = &FakeNetworkingReconciler{}

	progressingCondition, _ := newProgressingCondition(conditions.PausedRolloutReason, r2)
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, true)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	assert.Equal(t, int32(10), f.fakeNetworking.controllerSetDesiredWeight)
}

func TestRolloutUsePreviousSetWeight(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(10),
		},
		{
			SetWeight: pointer.Int32Ptr(20),
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.Networking = &v1alpha1.RolloutNetworking{}
	f.fakeNetworking = &FakeNetworkingReconciler{}

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, false)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectUpdateReplicaSetAction(rs2)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	assert.Equal(t, int32(10), f.fakeNetworking.controllerSetDesiredWeight)
}

func TestRolloutSetWeightToZeroWhenFullyRolledOut(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(10),
		},
	}
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(1), intstr.FromInt(0))
	r1.Spec.Strategy.Canary.Networking = &v1alpha1.RolloutNetworking{}
	f.fakeNetworking = &FakeNetworkingReconciler{
		controllerSetDesiredWeight: 10,
	}

	rs1 := newReplicaSetWithStatus(r1, 10, 10)

	f.kubeobjects = append(f.kubeobjects, rs1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	r1 = updateCanaryRolloutStatus(r1, rs1PodHash, 10, 0, 10, false)
	f.rolloutLister = append(f.rolloutLister, r1)
	f.objects = append(f.objects, r1)

	f.expectPatchRolloutAction(r1)
	f.run(getKey(r1, t))

	assert.Equal(t, int32(0), f.fakeNetworking.controllerSetDesiredWeight)
}

/*
Test calculate the desiredWeight (wait until ready)

*/
