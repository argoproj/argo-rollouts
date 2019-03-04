package conditions

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
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
				BlueGreenStrategy: &v1alpha1.BlueGreenStrategy{
					PreviewService: "preview",
					ActiveService:  "active",
				},
			},
		},
	}
	assert.Nil(t, VerifyRolloutSpec(validRollout, nil))

	noActiveSvc := validRollout.DeepCopy()
	noActiveSvc.Spec.Strategy.BlueGreenStrategy.ActiveService = ""
	noActiveSvcCond := VerifyRolloutSpec(noActiveSvc, nil)
	assert.NotNil(t, noActiveSvcCond)
	assert.Equal(t, fmt.Sprintf(MissingFieldMessage, ".Spec.Strategy.BlueGreenStrategy.ActiveService"), noActiveSvcCond.Message)
	assert.Equal(t, MissingFieldReason, noActiveSvcCond.Reason)

	sameSvcs := validRollout.DeepCopy()
	sameSvcs.Spec.Strategy.BlueGreenStrategy.ActiveService = "preview"
	sameSvcsCond := VerifyRolloutSpec(sameSvcs, nil)
	assert.NotNil(t, sameSvcsCond)
	assert.Equal(t, DuplicatedServicesMessage, sameSvcsCond.Message)
	assert.Equal(t, DuplicatedServicesReason, sameSvcsCond.Reason)
}

func TestVerifyRolloutSpecBaseCases(t *testing.T) {
	ro := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"key": "value"},
			},
			Strategy: v1alpha1.RolloutStrategy{
				CanaryStrategy: &v1alpha1.CanaryStrategy{},
				BlueGreenStrategy: &v1alpha1.BlueGreenStrategy{
					ActiveService: "active",
				},
			},
		},
	}
	cond := VerifyRolloutSpec(ro, nil)
	assert.Equal(t, v1alpha1.InvalidSpec, cond.Type)
	assert.Equal(t, InvalidFieldReason, cond.Reason)
	assert.Equal(t, InvalidStrategyMessage, cond.Message)

	validRollout := ro.DeepCopy()
	validRollout.Spec.Strategy.CanaryStrategy = nil
	validRolloutCond := VerifyRolloutSpec(validRollout, nil)
	assert.Nil(t, validRolloutCond)

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
	assert.Equal(t, MissingFieldReason, noSelectorCond.Reason)
}

func TestVerifyRolloutSpecCanary(t *testing.T) {
	zero := intstr.FromInt(0)
	tests := []struct {
		name           string
		maxUnavailable *intstr.IntOrString
		maxSurge       *intstr.IntOrString
		steps          []v1alpha1.CanaryStep

		notValid bool
		reason   string
		message  string
	}{
		{
			name:           "Max Surge and Max Unavailable set to zero",
			maxUnavailable: &zero,
			maxSurge:       &zero,

			notValid: true,
			reason:   InvalidFieldReason,
			message:  InvalidMaxSurgeMaxUnavailable,
		},
		{
			name: "setWeight and pause both set",
			steps: []v1alpha1.CanaryStep{{
				Pause:     &v1alpha1.RolloutPause{},
				SetWeight: pointer.Int32Ptr(10),
			}},

			notValid: true,
			reason:   InvalidFieldReason,
			message:  InvalidStepMessage,
		},
		{
			name:  "Nether setWeight and pause are set",
			steps: []v1alpha1.CanaryStep{{}},

			notValid: true,
			reason:   InvalidFieldReason,
			message:  InvalidStepMessage,
		},
		{
			name: "setWeight over 0",
			steps: []v1alpha1.CanaryStep{{
				SetWeight: pointer.Int32Ptr(-1),
			}},

			notValid: true,
			reason:   InvalidFieldReason,
			message:  InvalidSetWeightMessage,
		},
		{
			name: "setWeight less than 100",
			steps: []v1alpha1.CanaryStep{{
				SetWeight: pointer.Int32Ptr(110),
			}},

			notValid: true,
			reason:   InvalidFieldReason,
			message:  InvalidSetWeightMessage,
		},
		{
			name: "Pause duration is not less than 0",
			steps: []v1alpha1.CanaryStep{{
				Pause: &v1alpha1.RolloutPause{
					Duration: pointer.Int32Ptr(-1),
				},
			}},

			notValid: true,
			reason:   InvalidFieldReason,
			message:  InvalidDurationMessage,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			ro := &v1alpha1.Rollout{
				Spec: v1alpha1.RolloutSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"key": "value"},
					},
					Strategy: v1alpha1.RolloutStrategy{
						CanaryStrategy: &v1alpha1.CanaryStrategy{
							MaxUnavailable: test.maxUnavailable,
							MaxSurge:       test.maxSurge,
							Steps:          test.steps,
						},
					},
				},
			}
			cond := VerifyRolloutSpec(ro, nil)
			if test.notValid {
				assert.Equal(t, v1alpha1.InvalidSpec, cond.Type)
				assert.Equal(t, test.reason, cond.Reason)
				assert.Equal(t, test.message, cond.Message)
			} else {
				assert.Nil(t, cond)
			}
		})
	}
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
			r.Status.BlueGreenStatus.ActiveSelector = podHash
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
	roPaused.Spec.Paused = true
	roPausedHash := ComputeGenerationHash(roPaused.Spec)

	assert.NotEqual(t, baseline, roPausedHash)
}

func TestComputeStepHash(t *testing.T) {
	ro := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
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
