package diff

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestCreateTwoWayMergeInvalidOrig(t *testing.T) {
	_, _, err := CreateTwoWayMergePatch(make(chan int), nil, nil)
	assert.NotNil(t, err)
}

func TestCreateTwoWayMergeInvalidNewObj(t *testing.T) {
	rollout := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}
	_, _, err := CreateTwoWayMergePatch(rollout, make(chan int), nil)
	assert.NotNil(t, err)
}

func TestCreateTwoWayMergeInvalidDataStruct(t *testing.T) {
	rollout := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}
	rollout2 := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: v1alpha1.RolloutSpec{
			Replicas: pointer.Int32Ptr(1),
		},
	}
	_, _, err := CreateTwoWayMergePatch(rollout, rollout2, nil)
	assert.Equal(t, err, fmt.Errorf("expected a struct, but received a nil"))
}

func TestCreateTwoWayMergeCreatePatch(t *testing.T) {
	rollout := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}
	rollout2 := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: v1alpha1.RolloutSpec{
			Replicas: pointer.Int32Ptr(1),
		},
	}
	patch, isPatched, err := CreateTwoWayMergePatch(rollout, rollout2, v1alpha1.Rollout{})
	assert.Nil(t, err)
	assert.True(t, isPatched)
	assert.Equal(t, `{"spec":{"replicas":1}}`, string(patch))
}

func TestCreateTwoWayMergeNoPatch(t *testing.T) {
	rollout := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}
	rollout2 := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}
	patch, isPatched, err := CreateTwoWayMergePatch(rollout, rollout2, v1alpha1.Rollout{})
	assert.Nil(t, err)
	assert.False(t, isPatched)
	assert.Equal(t, "{}", string(patch))
}
