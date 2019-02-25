package conditions

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"k8s.io/utils/pointer"
)

var (
	condInvalidSpec = func() v1alpha1.RolloutCondition {
		return v1alpha1.RolloutCondition{
			Type:   v1alpha1.InvalidSpec,
			Status: v1.ConditionFalse,
			Reason: "ForSomeReason",
		}
	}

	condInvalidSpec2 = func() v1alpha1.RolloutCondition {
		return v1alpha1.RolloutCondition{
			Type:   v1alpha1.InvalidSpec,
			Status: v1.ConditionTrue,
			Reason: "BecauseItIs",
		}
	}

	condAvailable = func() v1alpha1.RolloutCondition {
		return v1alpha1.RolloutCondition{
			Type:   v1alpha1.RolloutAvailable,
			Status: v1.ConditionTrue,
			Reason: "AwesomeController",
		}
	}

	status = func() *v1alpha1.RolloutStatus {
		return &v1alpha1.RolloutStatus{
			Conditions: []v1alpha1.RolloutCondition{condInvalidSpec(), condAvailable()},
		}
	}
)

func TestGetCondition(t *testing.T) {
	exampleStatus := status()

	tests := []struct {
		name     string
		status   v1alpha1.RolloutStatus
		condType v1alpha1.RolloutConditionType

		expected bool
	}{
		{
			name:     "condition exists",
			status:   *exampleStatus,
			condType: v1alpha1.RolloutAvailable,

			expected: true,
		},
		{
			name:     "condition does not exist",
			status:   *exampleStatus,
			condType: FailedRSCreateReason,

			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cond := GetRolloutCondition(test.status, test.condType)
			exists := cond != nil
			assert.Equal(t, exists, test.expected)
		})
	}
}

func TestSetCondition(t *testing.T) {
	now := metav1.Now()
	before := metav1.Time{Time: now.Add(-time.Minute)}
	tests := []struct {
		name string

		status *v1alpha1.RolloutStatus
		cond   v1alpha1.RolloutCondition

		expectedStatus *v1alpha1.RolloutStatus
	}{
		{
			name:   "set for the first time",
			status: &v1alpha1.RolloutStatus{},
			cond:   condAvailable(),

			expectedStatus: &v1alpha1.RolloutStatus{Conditions: []v1alpha1.RolloutCondition{condAvailable()}},
		},
		{
			name:   "simple set",
			status: &v1alpha1.RolloutStatus{Conditions: []v1alpha1.RolloutCondition{condInvalidSpec()}},
			cond:   condAvailable(),

			expectedStatus: status(),
		},
		{
			name:   "No Changes",
			status: &v1alpha1.RolloutStatus{Conditions: []v1alpha1.RolloutCondition{condAvailable()}},
			cond:   condAvailable(),

			expectedStatus: &v1alpha1.RolloutStatus{Conditions: []v1alpha1.RolloutCondition{condAvailable()}},
		},
		{
			name: "Status change",
			status: &v1alpha1.RolloutStatus{Conditions: []v1alpha1.RolloutCondition{
				{
					Type:           v1alpha1.RolloutAvailable,
					Status:         v1.ConditionTrue,
					Reason:         "AwesomeController",
					LastUpdateTime: before,
				},
			}},
			cond: v1alpha1.RolloutCondition{
				Type:           v1alpha1.RolloutAvailable,
				Status:         v1.ConditionFalse,
				Reason:         "AwesomeController",
				LastUpdateTime: now,
			},

			expectedStatus: &v1alpha1.RolloutStatus{Conditions: []v1alpha1.RolloutCondition{
				{
					Type:           v1alpha1.RolloutAvailable,
					Status:         v1.ConditionFalse,
					Reason:         "AwesomeController",
					LastUpdateTime: now,
				},
			}},
		},
		{
			name: "No status change",
			status: &v1alpha1.RolloutStatus{Conditions: []v1alpha1.RolloutCondition{
				{
					Type:           v1alpha1.RolloutAvailable,
					Status:         v1.ConditionTrue,
					Reason:         "AwesomeController",
					LastUpdateTime: before,
				},
			}},
			cond: v1alpha1.RolloutCondition{
				Type:           v1alpha1.RolloutAvailable,
				Status:         v1.ConditionTrue,
				Reason:         "AwesomeController",
				LastUpdateTime: now,
			},

			expectedStatus: &v1alpha1.RolloutStatus{Conditions: []v1alpha1.RolloutCondition{
				{
					Type:           v1alpha1.RolloutAvailable,
					Status:         v1.ConditionTrue,
					Reason:         "AwesomeController",
					LastUpdateTime: before,
				},
			}},
		},
		{
			name:   "overwrite",
			status: &v1alpha1.RolloutStatus{Conditions: []v1alpha1.RolloutCondition{condInvalidSpec()}},
			cond:   condInvalidSpec2(),

			expectedStatus: &v1alpha1.RolloutStatus{Conditions: []v1alpha1.RolloutCondition{condInvalidSpec2()}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			SetRolloutCondition(test.status, test.cond)
			assert.Equal(t, test.status, test.expectedStatus)
		})
	}
}

func TestRemoveCondition(t *testing.T) {
	tests := []struct {
		name string

		status   *v1alpha1.RolloutStatus
		condType v1alpha1.RolloutConditionType

		expectedStatus *v1alpha1.RolloutStatus
	}{
		{
			name: "remove from empty status",

			status:   &v1alpha1.RolloutStatus{},
			condType: v1alpha1.InvalidSpec,

			expectedStatus: &v1alpha1.RolloutStatus{},
		},
		{
			name: "simple remove",

			status:   &v1alpha1.RolloutStatus{Conditions: []v1alpha1.RolloutCondition{condInvalidSpec()}},
			condType: v1alpha1.InvalidSpec,

			expectedStatus: &v1alpha1.RolloutStatus{},
		},
		{
			name: "doesn't remove anything",

			status:   status(),
			condType: FailedRSCreateReason,

			expectedStatus: status(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			RemoveRolloutCondition(test.status, test.condType)
			assert.Equal(t, test.status, test.expectedStatus)
		})
	}
}

func TestVerifyRolloutSpecBlueGreen(t *testing.T) {
	validRollout := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"key": "value"},
			},
			Strategy: v1alpha1.RolloutStrategy{
				Type: v1alpha1.BlueGreenRolloutStrategyType,
				BlueGreenStrategy: &v1alpha1.BlueGreenStrategy{
					PreviewService: "preview",
					ActiveService:  "active",
				},
			},
		},
	}
	assert.Nil(t, VerifyRolloutSpec(validRollout, nil))

	selectorEverything := validRollout.DeepCopy()
	selectorEverything.Spec.Selector = &metav1.LabelSelector{}
	selectorEverythingConf := VerifyRolloutSpec(selectorEverything, nil)
	assert.NotNil(t, selectorEverythingConf)
	assert.Equal(t, SelectAllMessage, selectorEverythingConf.Message)
	assert.Equal(t, InvalidSelectorReason, selectorEverythingConf.Reason)

	noSelector := validRollout.DeepCopy()
	noSelector.Spec.Selector = nil
	noSelectorCond := VerifyRolloutSpec(noSelector, nil)
	assert.NotNil(t, noSelectorCond)
	assert.Equal(t, fmt.Sprintf(MissingFieldMessage, ".Spec.Selector"), noSelectorCond.Message)
	assert.Equal(t, MissingSelectorReason, noSelectorCond.Reason)

	noBlueGreenStrategy := validRollout.DeepCopy()
	noBlueGreenStrategy.Spec.Strategy.BlueGreenStrategy = nil
	noBlueGreenStrategyCond := VerifyRolloutSpec(noBlueGreenStrategy, nil)
	assert.NotNil(t, noBlueGreenStrategyCond)
	assert.Equal(t, fmt.Sprintf(MissingFieldMessage, ".Spec.Strategy.BlueGreenStrategy"), noBlueGreenStrategyCond.Message)
	assert.Equal(t, MissingBlueGreenStrategyReason, noBlueGreenStrategyCond.Reason)

	noActiveSvc := validRollout.DeepCopy()
	noActiveSvc.Spec.Strategy.BlueGreenStrategy.ActiveService = ""
	noActiveSvcCond := VerifyRolloutSpec(noActiveSvc, nil)
	assert.NotNil(t, noActiveSvcCond)
	assert.Equal(t, fmt.Sprintf(MissingFieldMessage, ".Spec.Strategy.BlueGreenStrategy.ActiveService"), noActiveSvcCond.Message)
	assert.Equal(t, MissingActiveServiceReason, noActiveSvcCond.Reason)

	sameSvcs := validRollout.DeepCopy()
	sameSvcs.Spec.Strategy.BlueGreenStrategy.ActiveService = "preview"
	sameSvcsCond := VerifyRolloutSpec(sameSvcs, nil)
	assert.NotNil(t, sameSvcsCond)
	assert.Equal(t, SameServicesMessage, sameSvcsCond.Message)
	assert.Equal(t, SameServicesReason, sameSvcsCond.Reason)

	noStrategy := validRollout.DeepCopy()
	noStrategy.Spec.Strategy = v1alpha1.RolloutStrategy{}
	noStrategyCond := VerifyRolloutSpec(noStrategy, nil)

	assert.NotNil(t, noStrategyCond)
	assert.Equal(t, fmt.Sprintf(MissingFieldMessage, ".Spec.Strategy.Type"), noStrategyCond.Message)
	assert.Equal(t, MissingStrategyTypeReason, noStrategyCond.Reason)
}

func TestHasRevisionHistoryLimit(t *testing.T) {
	r := &v1alpha1.Rollout{}
	assert.False(t, HasRevisionHistoryLimit(r))
	int32Value := int32(math.MaxInt32)
	r.Spec.RevisionHistoryLimit = &int32Value
	assert.False(t, HasRevisionHistoryLimit(r))
	int32Value = int32(1)
	r.Spec.RevisionHistoryLimit = &int32Value
	assert.True(t, HasRevisionHistoryLimit(r))
}

func TestRolloutComplete(t *testing.T) {
	rollout := func(desired, current, updated, available int32, pointActiveAtPodHash bool, correctObservedGeneration bool) *v1alpha1.Rollout {
		r := &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Replicas: &desired,
			},
			Status: v1alpha1.RolloutStatus{
				Replicas:          current,
				UpdatedReplicas:   updated,
				AvailableReplicas: available,
			},
		}
		podHash := controller.ComputeHash(&r.Spec.Template, r.Status.CollisionCount)
		r.Status.CurrentPodHash = podHash
		if correctObservedGeneration {
			r.Status.ObservedGeneration = ComputeGenerationHash(r.Spec)
		}
		if pointActiveAtPodHash {
			r.Status.ActiveSelector = podHash
		}
		return r
	}

	tests := []struct {
		name     string
		r        *v1alpha1.Rollout
		expected bool
	}{
		{
			name: "complete",

			r:        rollout(5, 5, 5, 5, true, true),
			expected: true,
		},
		{
			name: "not complete: min but not all pods become available",

			r:        rollout(5, 5, 5, 4, true, true),
			expected: false,
		},
		{
			name:     "not complete: all pods are available but not all active",
			r:        rollout(5, 5, 4, 5, true, true),
			expected: false,
		},
		{
			name:     "not complete: still running old pods",
			r:        rollout(1, 2, 1, 1, true, true),
			expected: false,
		},
		{
			name:     "not complete: Mismatching ObservedGeneration",
			r:        rollout(1, 2, 1, 1, true, false),
			expected: false,
		},
		{
			name:     "not complete: active service does not point at updated rs",
			r:        rollout(1, 1, 1, 1, false, true),
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, RolloutComplete(test.r, &test.r.Status))
		})
	}

}

func TestComputeGenerationHash(t *testing.T) {
	ro := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Replicas: pointer.Int32Ptr(10),
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{{
						Image: "name",
					}},
				},
			},
		},
	}
	baseline := ComputeGenerationHash(ro.Spec)
	roPaused := ro.DeepCopy()
	roPaused.Spec.Pause = pointer.BoolPtr(true)
	roPausedHash := ComputeGenerationHash(roPaused.Spec)

	assert.NotEqual(t, baseline, roPausedHash)
}

func TestComputeStepHash(t *testing.T) {
	ro := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Type: v1alpha1.CanaryRolloutStrategyType,
				CanaryStrategy: &v1alpha1.CanaryStrategy{
					Steps: []v1alpha1.CanaryStep{
						{
							Pause: &v1alpha1.RolloutPause{},
						},
					},
				},
			},
		},
	}
	baseline := ComputeStepHash(ro)
	roWithDiffSteps := ro.DeepCopy()
	roWithDiffSteps.Spec.Strategy.CanaryStrategy.Steps = []v1alpha1.CanaryStep{
		{
			Pause: &v1alpha1.RolloutPause{},
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	roWithDiffStepsHash := ComputeStepHash(roWithDiffSteps)

	roWithSameSteps := ro.DeepCopy()
	roWithSameSteps.Status.CurrentPodHash = "Test"
	roWithSameSteps.Spec.Replicas = pointer.Int32Ptr(1)
	roWithSameStepsHash := ComputeStepHash(roWithSameSteps)

	roNoSteps := ro.DeepCopy()
	roNoSteps.Spec.Strategy.CanaryStrategy.Steps = nil
	roNoStepsHash := ComputeStepHash(roNoSteps)

	roBlueGreen := ro.DeepCopy()
	roBlueGreen.Spec.Strategy.BlueGreenStrategy = &v1alpha1.BlueGreenStrategy{}
	roBlueGreen.Spec.Strategy.CanaryStrategy = nil
	roBlueGreenHash := ComputeStepHash(roBlueGreen)

	assert.NotEqual(t, baseline, roWithDiffStepsHash)
	assert.Equal(t, baseline, roWithSameStepsHash)
	assert.NotEqual(t, baseline, roNoStepsHash)
	assert.Equal(t, "", roBlueGreenHash)
}
