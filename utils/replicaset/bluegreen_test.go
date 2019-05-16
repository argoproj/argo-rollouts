package replicaset

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestGetActiveReplicaSet(t *testing.T) {
	activeRS, nonActiveRSs := GetActiveReplicaSet(nil, "")
	assert.Nil(t, activeRS)
	assert.Nil(t, nonActiveRSs)
	rs1 := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "abcd"},
		},
	}
	activeRS, nonActiveRSs = GetActiveReplicaSet([]*appsv1.ReplicaSet{rs1}, "1234")
	assert.Nil(t, activeRS)
	assert.Equal(t, rs1, nonActiveRSs[0])

	rs2 := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "1234"},
		},
	}
	activeRS, nonActiveRSs = GetActiveReplicaSet([]*appsv1.ReplicaSet{nil, rs1, rs2}, "1234")
	assert.Equal(t, rs2, activeRS)
	assert.Len(t, nonActiveRSs, 2)
}

func TestReadyForPreview(t *testing.T) {
	rollout := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreenStrategy: &v1alpha1.BlueGreenStrategy{},
			},
		},
	}

	readyRS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "abcd"},
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: pointer.Int32Ptr(1),
		},
		Status: appsv1.ReplicaSetStatus{
			AvailableReplicas: 1,
		},
	}

	notReadyRS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "abcd"},
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: pointer.Int32Ptr(1),
		},
		Status: appsv1.ReplicaSetStatus{
			AvailableReplicas: 0,
		},
	}
	assert.False(t, ReadyForPreview(&v1alpha1.Rollout{}, nil, nil))
	assert.True(t, ReadyForPreview(rollout, readyRS, []*appsv1.ReplicaSet{readyRS}))
	assert.False(t, ReadyForPreview(rollout, notReadyRS, []*appsv1.ReplicaSet{readyRS}))
}
