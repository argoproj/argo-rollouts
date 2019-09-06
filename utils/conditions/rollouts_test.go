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
	assert.Equal(t, InvalidSpecReason, noActiveSvcCond.Reason)

	scaleDownDelayLongerThanProgressDeadline := validRollout.DeepCopy()
	scaleDownDelayLongerThanProgressDeadline.Spec.Strategy.BlueGreenStrategy.ScaleDownDelaySeconds = pointer.Int32Ptr(1000)
	scaleDownDelayLongerThanProgressDeadlineCond := VerifyRolloutSpec(scaleDownDelayLongerThanProgressDeadline, nil)
	assert.NotNil(t, scaleDownDelayLongerThanProgressDeadlineCond)
	assert.Equal(t, InvalidSpecReason, scaleDownDelayLongerThanProgressDeadlineCond.Reason)
	assert.Equal(t, ScaleDownDelayLongerThanDeadlineMessage, scaleDownDelayLongerThanProgressDeadlineCond.Message)

	sameSvcs := validRollout.DeepCopy()
	sameSvcs.Spec.Strategy.BlueGreenStrategy.ActiveService = "preview"
	sameSvcsCond := VerifyRolloutSpec(sameSvcs, nil)
	assert.NotNil(t, sameSvcsCond)
	assert.Equal(t, DuplicatedServicesMessage, sameSvcsCond.Message)
	assert.Equal(t, InvalidSpecReason, sameSvcsCond.Reason)

	scaleLimitLargerThanRevision := validRollout.DeepCopy()
	scaleLimitLargerThanRevision.Spec.Strategy.BlueGreenStrategy.ScaleDownDelayRevisionLimit = pointer.Int32Ptr(100)
	scaleLimitLargerThanRevisionCond := VerifyRolloutSpec(scaleLimitLargerThanRevision, nil)
	assert.NotNil(t, scaleLimitLargerThanRevisionCond)
	assert.Equal(t, ScaleDownLimitLargerThanRevisionLimit, scaleLimitLargerThanRevisionCond.Message)
	assert.Equal(t, InvalidSpecReason, sameSvcsCond.Reason)
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
	assert.Equal(t, InvalidSpecReason, cond.Reason)
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
	assert.Equal(t, InvalidSpecReason, selectorEverythingConf.Reason)

	noSelector := validRollout.DeepCopy()
	noSelector.Spec.Selector = nil
	noSelectorCond := VerifyRolloutSpec(noSelector, nil)
	assert.NotNil(t, noSelectorCond)
	assert.Equal(t, fmt.Sprintf(MissingFieldMessage, ".Spec.Selector"), noSelectorCond.Message)
	assert.Equal(t, InvalidSpecReason, noSelectorCond.Reason)

	minReadyLongerThanProgessDeadline := validRollout.DeepCopy()
	minReadyLongerThanProgessDeadline.Spec.MinReadySeconds = 1000
	minReadyLongerThanProgessDeadlineCond := VerifyRolloutSpec(minReadyLongerThanProgessDeadline, nil)
	assert.NotNil(t, minReadyLongerThanProgessDeadlineCond)
	assert.Equal(t, InvalidSpecReason, minReadyLongerThanProgessDeadlineCond.Reason)
	assert.Equal(t, MinReadyLongerThanDeadlineMessage, minReadyLongerThanProgessDeadlineCond.Message)
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
			reason:   InvalidSpecReason,
			message:  InvalidMaxSurgeMaxUnavailable,
		},
		{
			name: "setWeight and pause both set",
			steps: []v1alpha1.CanaryStep{{
				Pause:     &v1alpha1.RolloutPause{},
				SetWeight: pointer.Int32Ptr(10),
			}},

			notValid: true,
			reason:   InvalidSpecReason,
			message:  InvalidStepMessage,
		},
		{
			name:  "Nether setWeight and pause are set",
			steps: []v1alpha1.CanaryStep{{}},

			notValid: true,
			reason:   InvalidSpecReason,
			message:  InvalidStepMessage,
		},
		{
			name: "setWeight over 0",
			steps: []v1alpha1.CanaryStep{{
				SetWeight: pointer.Int32Ptr(-1),
			}},

			notValid: true,
			reason:   InvalidSpecReason,
			message:  InvalidSetWeightMessage,
		},
		{
			name: "setWeight less than 100",
			steps: []v1alpha1.CanaryStep{{
				SetWeight: pointer.Int32Ptr(110),
			}},

			notValid: true,
			reason:   InvalidSpecReason,
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
			reason:   InvalidSpecReason,
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

func TestInvalidMaxSurgeMaxUnavailable(t *testing.T) {
	r := func(maxSurge, maxUnavailable intstr.IntOrString) *v1alpha1.Rollout {
		return &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					CanaryStrategy: &v1alpha1.CanaryStrategy{
						MaxSurge:       &maxSurge,
						MaxUnavailable: &maxUnavailable,
					},
				},
			},
		}
	}
	assert.True(t, invalidMaxSurgeMaxUnavailable(r(intstr.FromInt(0), intstr.FromInt(0))))
	assert.True(t, invalidMaxSurgeMaxUnavailable(r(intstr.FromString("0"), intstr.FromInt(0))))
	assert.True(t, invalidMaxSurgeMaxUnavailable(r(intstr.FromString("0%"), intstr.FromInt(0))))
	assert.True(t, invalidMaxSurgeMaxUnavailable(r(intstr.FromInt(0), intstr.FromString("0"))))
	assert.True(t, invalidMaxSurgeMaxUnavailable(r(intstr.FromInt(0), intstr.FromString("0%"))))
	assert.True(t, invalidMaxSurgeMaxUnavailable(r(intstr.FromString("0"), intstr.FromString("0"))))
	assert.True(t, invalidMaxSurgeMaxUnavailable(r(intstr.FromString("0%"), intstr.FromString("0%"))))

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

func TestRolloutProgressing(t *testing.T) {
	rolloutStatus := func(current, updated, ready, available int32) v1alpha1.RolloutStatus {
		return v1alpha1.RolloutStatus{
			Replicas:          current,
			UpdatedReplicas:   updated,
			ReadyReplicas:     ready,
			AvailableReplicas: available,
		}
	}
	blueGreenStatus := func(current, updated, ready, available int32, activeSelector, previewSelector string) v1alpha1.RolloutStatus {
		status := rolloutStatus(current, updated, ready, available)
		status.BlueGreen.ActiveSelector = activeSelector
		status.BlueGreen.PreviewSelector = previewSelector
		return status
	}
	canaryStatus := func(current, updated, ready, available int32, stableRS string, index int32, stepHash string) v1alpha1.RolloutStatus {
		status := rolloutStatus(current, updated, ready, available)
		status.Canary.StableRS = stableRS
		status.CurrentStepIndex = &index
		status.CurrentStepHash = stepHash
		return status
	}
	blueGreenRollout := func(current, updated, ready, available int32, activeSelector, previewSelector string) *v1alpha1.Rollout {
		return &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					BlueGreenStrategy: &v1alpha1.BlueGreenStrategy{},
				},
			},
			Status: blueGreenStatus(current, updated, ready, available, activeSelector, previewSelector),
		}
	}
	canaryRollout := func(current, updated, ready, available int32, stableRS string, index int32, stepHash string) *v1alpha1.Rollout {
		return &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					CanaryStrategy: &v1alpha1.CanaryStrategy{},
				},
			},
			Status: canaryStatus(current, updated, ready, available, stableRS, index, stepHash),
		}
	}

	tests := []struct {
		name           string
		updatedRollout *v1alpha1.Rollout
		oldStatus      v1alpha1.RolloutStatus
		expected       bool
	}{
		{
			name:           "BlueGreen: Active Selector change",
			updatedRollout: blueGreenRollout(1, 1, 1, 1, "active", "preview"),
			oldStatus:      blueGreenStatus(1, 1, 1, 1, "", "preview"),
			expected:       true,
		},
		{
			name:           "BlueGreen: Preview Selector change",
			updatedRollout: blueGreenRollout(1, 1, 1, 1, "active", "preview"),
			oldStatus:      blueGreenStatus(1, 1, 1, 1, "active", ""),
			expected:       true,
		},
		{
			name:           "BlueGreen: No change",
			updatedRollout: blueGreenRollout(1, 1, 1, 1, "active", "preview"),
			oldStatus:      blueGreenStatus(1, 1, 1, 1, "active", "preview"),
			expected:       false,
		},
		{
			name:           "Canary: Stable Selector change",
			updatedRollout: canaryRollout(1, 1, 1, 1, "active", 1, "abcdef"),
			oldStatus:      canaryStatus(1, 1, 1, 1, "", 1, "abcdef"),
			expected:       true,
		},
		{
			name:           "Canary: StepIndex change",
			updatedRollout: canaryRollout(1, 1, 1, 1, "active", 1, "abcdef"),
			oldStatus:      canaryStatus(1, 1, 1, 1, "active", 2, "abcdef"),
			expected:       true,
		},
		{
			name:           "Canary: StepHash change",
			updatedRollout: canaryRollout(1, 1, 1, 1, "active", 1, "abcdef"),
			oldStatus:      canaryStatus(1, 1, 1, 1, "active", 1, "12345"),
			expected:       true,
		},
		{
			name:           "Canary: No change",
			updatedRollout: canaryRollout(1, 1, 1, 1, "active", 1, "abcdef"),
			oldStatus:      canaryStatus(1, 1, 1, 1, "active", 1, "abcdef"),
			expected:       false,
		},
		{
			name:           "Updated Replica change",
			updatedRollout: canaryRollout(1, 1, 1, 1, "active", 1, "abcdef"),
			oldStatus:      canaryStatus(1, 2, 1, 1, "active", 1, "abcdef"),
			expected:       true,
		},
		{
			name:           "Ready Replica change",
			updatedRollout: canaryRollout(1, 1, 1, 1, "active", 1, "abcdef"),
			oldStatus:      canaryStatus(1, 1, 2, 1, "active", 1, "abcdef"),
			expected:       true,
		},
		{
			name:           "Available Replica change",
			updatedRollout: canaryRollout(1, 1, 1, 1, "active", 1, "abcdef"),
			oldStatus:      canaryStatus(1, 1, 1, 2, "active", 1, "abcdef"),
			expected:       true,
		},
		{
			name:           "Old Replica Replica change",
			updatedRollout: canaryRollout(2, 1, 1, 1, "active", 1, "abcdef"),
			oldStatus:      canaryStatus(1, 1, 1, 2, "active", 1, "abcdef"),
			expected:       true,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, RolloutProgressing(test.updatedRollout, &test.oldStatus))
		})
	}

}

func TestRolloutComplete(t *testing.T) {
	rollout := func(desired, current, updated, available int32, correctObservedGeneration bool) *v1alpha1.Rollout {
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
		return r
	}

	blueGreenRollout := func(desired, current, updated, available int32, correctObservedGeneration bool, activeSelector, previewSelector string) *v1alpha1.Rollout {
		r := rollout(desired, current, updated, available, correctObservedGeneration)
		r.Spec.Strategy = v1alpha1.RolloutStrategy{
			BlueGreenStrategy: &v1alpha1.BlueGreenStrategy{
				PreviewService: "preview",
				ActiveService:  "active",
			},
		}
		r.Status.BlueGreen.ActiveSelector = activeSelector
		r.Status.BlueGreen.PreviewSelector = previewSelector
		if correctObservedGeneration {
			r.Status.ObservedGeneration = ComputeGenerationHash(r.Spec)
		}
		return r
	}

	canaryRollout := func(desired, current, updated, available int32, correctObservedGeneration bool, stableRS string, hasSteps bool, stepIndex *int32) *v1alpha1.Rollout {
		r := rollout(desired, current, updated, available, correctObservedGeneration)
		steps := []v1alpha1.CanaryStep{}
		if hasSteps {
			steps = append(steps, v1alpha1.CanaryStep{SetWeight: pointer.Int32Ptr(30)})
		}
		r.Spec.Strategy = v1alpha1.RolloutStrategy{
			CanaryStrategy: &v1alpha1.CanaryStrategy{
				Steps: steps,
			},
		}
		r.Status.Canary.StableRS = stableRS
		r.Status.CurrentStepIndex = stepIndex
		if correctObservedGeneration {
			r.Status.ObservedGeneration = ComputeGenerationHash(r.Spec)
		}
		return r
	}

	tests := []struct {
		name     string
		r        *v1alpha1.Rollout
		expected bool
	}{
		{
			name:     "BlueGreen complete",
			r:        blueGreenRollout(5, 5, 5, 5, true, "685bdb47d8", ""),
			expected: true,
		},
		{
			name:     "BlueGreen not completed: active service does not point at updated rs",
			r:        blueGreenRollout(1, 1, 1, 1, true, "not-active", ""),
			expected: false,
		},
		{
			name:     "BlueGreen not completed:: preview service points at something",
			r:        blueGreenRollout(1, 1, 1, 1, true, "active", "preview"),
			expected: false,
		},
		{
			name:     "CanaryWithSteps Completed",
			r:        canaryRollout(1, 1, 1, 1, true, "active", true, pointer.Int32Ptr(1)),
			expected: false,
		},
		{
			name:     "CanaryWithSteps Not Completed: Steps left",
			r:        canaryRollout(1, 1, 1, 1, true, "active", true, pointer.Int32Ptr(0)),
			expected: false,
		},
		{
			name:     "CanaryNoSteps Completed",
			r:        canaryRollout(1, 1, 1, 1, true, "active", false, nil),
			expected: false,
		},
		{
			name:     "Canary Not Completed: Diff stableRs",
			r:        canaryRollout(1, 1, 1, 1, true, "not-active", false, nil),
			expected: false,
		},
		{
			name:     "not complete: min but not all pods become available",
			r:        rollout(5, 5, 5, 4, true),
			expected: false,
		},
		{
			name:     "not complete: all pods are available but not all active",
			r:        rollout(5, 5, 4, 5, true),
			expected: false,
		},
		{
			name:     "not complete: still running old pods",
			r:        rollout(1, 2, 1, 1, true),
			expected: false,
		},
		{
			name:     "not complete: Mismatching ObservedGeneration",
			r:        rollout(1, 2, 1, 1, false),
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, RolloutComplete(test.r, &test.r.Status))
		})
	}

}

func TestRolloutTimedOut(t *testing.T) {

	before := metav1.Time{
		Time: metav1.Now().Add(-10 * time.Second),
	}

	conditons := func(reason string, lastUpdate metav1.Time) []v1alpha1.RolloutCondition {
		return []v1alpha1.RolloutCondition{{
			Type:           v1alpha1.RolloutProgressing,
			Reason:         reason,
			LastUpdateTime: lastUpdate,
		}}
	}

	tests := []struct {
		name                    string
		progressDeadlineSeconds int32
		newStatus               v1alpha1.RolloutStatus
		expected                bool
	}{
		{
			name: "New RS is Available",
			newStatus: v1alpha1.RolloutStatus{
				Conditions: conditons(NewRSAvailableReason, metav1.Now()),
			},
			expected: false,
		},
		{
			name: "Has no progressing condition",
			newStatus: v1alpha1.RolloutStatus{
				Conditions: []v1alpha1.RolloutCondition{},
			},
			expected: false,
		},
		{
			name: "Rollout is already has timed out condition",
			newStatus: v1alpha1.RolloutStatus{
				Conditions: conditons(TimedOutReason, metav1.Now()),
			},
			expected: true,
		},
		{
			name:                    "Rollout has not timed out",
			progressDeadlineSeconds: 30,
			newStatus: v1alpha1.RolloutStatus{
				Conditions: conditons(ReplicaSetUpdatedReason, before),
			},
			expected: false,
		},
		{
			name:                    "Rollout has timed out",
			progressDeadlineSeconds: 5,
			newStatus: v1alpha1.RolloutStatus{
				Conditions: conditons(ReplicaSetUpdatedReason, before),
			},
			expected: true,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			rollout := &v1alpha1.Rollout{
				Spec: v1alpha1.RolloutSpec{
					ProgressDeadlineSeconds: &test.progressDeadlineSeconds,
				},
			}
			assert.Equal(t, test.expected, RolloutTimedOut(rollout, &test.newStatus))
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

// TestComputeStableStepHash verifies we generate different hashes for various step definitions.
// Also verifies we do not unintentionally break our ComputeStepHash function somehow (e.g. by
// modifying types or change libraries)
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
	assert.Equal(t, "79c9b9f6bf", roWithDiffStepsHash)

	roWithSameSteps := ro.DeepCopy()
	roWithSameSteps.Status.CurrentPodHash = "Test"
	roWithSameSteps.Spec.Replicas = pointer.Int32Ptr(1)
	roWithSameStepsHash := ComputeStepHash(roWithSameSteps)
	assert.Equal(t, "6b9b86fbd5", roWithSameStepsHash)

	roNoSteps := ro.DeepCopy()
	roNoSteps.Spec.Strategy.CanaryStrategy.Steps = nil
	roNoStepsHash := ComputeStepHash(roNoSteps)
	assert.Equal(t, "5ffbfbbd64", roNoStepsHash)

	roBlueGreen := ro.DeepCopy()
	roBlueGreen.Spec.Strategy.CanaryStrategy = nil
	roBlueGreen.Spec.Strategy.BlueGreenStrategy = &v1alpha1.BlueGreenStrategy{}
	roBlueGreenHash := ComputeStepHash(roBlueGreen)
	assert.Equal(t, "", roBlueGreenHash)

	assert.NotEqual(t, baseline, roWithDiffStepsHash)
	assert.Equal(t, baseline, roWithSameStepsHash)
	assert.NotEqual(t, baseline, roNoStepsHash)
}
