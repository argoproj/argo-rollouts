package controller

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
)

func newCanaryRollout(name string, replicas int, revisionHistoryLimit *int32, selector map[string]string, steps []v1alpha1.CanaryStep, stepIndex *int32) *v1alpha1.Rollout {
	rollout := newRollout(name, replicas, revisionHistoryLimit, selector)
	rollout.Spec.Strategy.Type = v1alpha1.CanaryRolloutStrategyType
	rollout.Spec.Strategy.CanaryStrategy = &v1alpha1.CanaryStrategy{
		Steps: steps,
	}
	rollout.Status.CurrentStepIndex = stepIndex
	return rollout
}

func TestReconcileCanaryStepsHandleBaseCases(t *testing.T) {
	fake := fake.Clientset{}
	k8sfake := k8sfake.Clientset{}
	controller := &Controller{
		rolloutsclientset: &fake,
		kubeclientset:     &k8sfake,
		recorder:          &record.FakeRecorder{},
	}

	// Handle case with no steps
	r := newCanaryRollout("test", 1, nil, nil, []v1alpha1.CanaryStep{}, nil)
	stepResult, err := controller.reconcileCanarySteps(r, nil)
	assert.Nil(t, err)
	assert.False(t, stepResult)
	assert.Len(t, fake.Actions(), 0)

	//Handle case where currentStepIndex is greater than the list of steps
	r2 := newCanaryRollout("test", 1, nil, nil, []v1alpha1.CanaryStep{{SetWeight: int32Ptr(10)}}, nil)
	r2.Status.CurrentStepIndex = int32Ptr(1)
	stepResult, err = controller.reconcileCanarySteps(r2, nil)
	assert.Nil(t, err)
	assert.False(t, stepResult)
	assert.Len(t, fake.Actions(), 0)

}

func TestReconcileCanaryStepsHandlePause(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }
	tests := []struct {
		name             string
		setPauseValue    *bool
		steps            []v1alpha1.CanaryStep
		currentStepIndex int32

		expectPatch           bool
		expectedSetPauseValue *bool
	}{
		{
			name:          "Put Canary into pause",
			setPauseValue: nil,
			steps: []v1alpha1.CanaryStep{
				{
					Pause: &v1alpha1.RolloutPause{},
				},
			},

			expectPatch:           true,
			expectedSetPauseValue: boolPtr(true),
		},
		{
			name:          "Do nothing if the canary is paused",
			setPauseValue: boolPtr(true),
			steps: []v1alpha1.CanaryStep{
				{
					Pause: &v1alpha1.RolloutPause{},
				},
			},

			expectPatch: false,
		},
		{
			name:          "Progress Canary after unpausing",
			setPauseValue: boolPtr(false),
			steps: []v1alpha1.CanaryStep{
				{
					Pause: &v1alpha1.RolloutPause{},
				},
			},

			expectPatch:           true,
			expectedSetPauseValue: nil,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			r := newCanaryRollout("test", 1, nil, nil, test.steps, nil)
			r.Status.CurrentStepIndex = &test.currentStepIndex
			r.Status.SetPause = test.setPauseValue

			fake := fake.Clientset{}
			k8sfake := k8sfake.Clientset{}
			controller := &Controller{
				rolloutsclientset: &fake,
				kubeclientset:     &k8sfake,
				recorder:          &record.FakeRecorder{},
			}
			stepResult, err := controller.reconcileCanarySteps(r, nil)
			assert.Nil(t, err)
			assert.True(t, stepResult)
			if test.expectPatch {
				patchRollout := fake.Actions()[0].(core.PatchAction).GetPatch()
				if test.expectedSetPauseValue == nil {
					assert.Equal(t, fmt.Sprintf(setPausePatch, "null"), string(patchRollout))
				} else {
					assert.Equal(t, fmt.Sprintf(setPausePatch, "true"), string(patchRollout))
				}
			} else {
				assert.Len(t, fake.Actions(), 0)
			}

		})
	}
}

const expectedResetCurrentStepIndexPatch = `{
  "status": {
    "currentPodHash":"57b9899597",
    "currentStepIndex":0,
    "observedGeneration":"5f5d596d45"
  }
}
`

func TestResetCurrentStepIndexOnSpecChange(t *testing.T) {
	// Remove spaces and newlines
	expectedPatch := strings.Replace(strings.Replace(expectedResetCurrentStepIndexPatch, " ", "", -1), "\n", "", -1)
	fake := fake.Clientset{}
	k8sfake := k8sfake.Clientset{}
	controller := &Controller{
		rolloutsclientset: &fake,
		kubeclientset:     &k8sfake,
		recorder:          &record.FakeRecorder{},
	}
	stepIndex := int32(1)
	steps := []v1alpha1.CanaryStep{
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	r := newCanaryRollout("test", 1, nil, nil, steps, &stepIndex)
	r.Status.CurrentPodHash = "old"
	err := controller.rolloutCanary(r, nil)
	assert.Nil(t, err)
	assert.Len(t, fake.Actions(), 1)
	resetIndexPatch := fake.Actions()[0].(core.PatchAction).GetPatch()
	assert.Equal(t, []byte(expectedPatch), resetIndexPatch)
}
