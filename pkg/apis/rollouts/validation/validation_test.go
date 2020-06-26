package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestValidateRollout(t *testing.T) {

}

func TestValidateRolloutSpec(t *testing.T) {
	// TODO: 1 test -> fail validation
	// TestVerifyRolloutSpecBaseCases
	//ro := &v1alpha1.Rollout{
	//	Spec: v1alpha1.RolloutSpec{
	//		Selector: &metav1.LabelSelector{
	//			MatchLabels: map[string]string{"key": "value"},
	//		},
	//		Strategy: v1alpha1.RolloutStrategy{
	//			Canary: &v1alpha1.CanaryStrategy{},
	//			BlueGreen: &v1alpha1.BlueGreenStrategy{
	//				ActiveService: "active",
	//			},
	//		},
	//	},
	//}
	//cond := VerifyRolloutSpec(ro, nil)
	//assert.Equal(t, v1alpha1.InvalidSpec, cond.Type)
	//assert.Equal(t, InvalidSpecReason, cond.Reason)
	//assert.Equal(t, InvalidStrategyMessage, cond.Message)
	//
	//validRollout := ro.DeepCopy()
	//validRollout.Spec.Strategy.Canary = nil
	//validRolloutCond := VerifyRolloutSpec(validRollout, nil)
	//assert.Nil(t, validRolloutCond)
	//
	//minReadyLongerThanProgessDeadline := validRollout.DeepCopy()
	//minReadyLongerThanProgessDeadline.Spec.MinReadySeconds = 1000
	//minReadyLongerThanProgessDeadlineCond := VerifyRolloutSpec(minReadyLongerThanProgessDeadline, nil)
	//assert.NotNil(t, minReadyLongerThanProgessDeadlineCond)
	//assert.Equal(t, InvalidSpecReason, minReadyLongerThanProgessDeadlineCond.Reason)
	//assert.Equal(t, RolloutMinReadyLongerThanDeadlineMessage, minReadyLongerThanProgessDeadlineCond.Message)
}

func TestValidateRolloutStrategy(t *testing.T) {

}

func TestValidateRolloutStrategyBlueGreen(t *testing.T) {
	//validRollout := &v1alpha1.Rollout{
	//	Spec: v1alpha1.RolloutSpec{
	//		Selector: &metav1.LabelSelector{
	//			MatchLabels: map[string]string{"key": "value"},
	//		},
	//		Strategy: v1alpha1.RolloutStrategy{
	//			BlueGreen: &v1alpha1.BlueGreenStrategy{
	//				PreviewService: "preview",
	//				ActiveService:  "active",
	//			},
	//		},
	//	},
	//}
	//assert.Nil(t, VerifyRolloutSpec(validRollout, nil))
	//
	//sameSvcs := validRollout.DeepCopy()
	//sameSvcs.Spec.Strategy.BlueGreen.ActiveService = "preview"
	//sameSvcsCond := VerifyRolloutSpec(sameSvcs, nil)
	//assert.NotNil(t, sameSvcsCond)
	//assert.Equal(t, DuplicatedServicesMessage, sameSvcsCond.Message)
	//assert.Equal(t, InvalidSpecReason, sameSvcsCond.Reason)
	//
	//scaleLimitLargerThanRevision := validRollout.DeepCopy()
	//scaleLimitLargerThanRevision.Spec.Strategy.BlueGreen.ScaleDownDelayRevisionLimit = pointer.Int32Ptr(100)
	//scaleLimitLargerThanRevisionCond := VerifyRolloutSpec(scaleLimitLargerThanRevision, nil)
	//assert.NotNil(t, scaleLimitLargerThanRevisionCond)
	//assert.Equal(t, ScaleDownLimitLargerThanRevisionLimit, scaleLimitLargerThanRevisionCond.Message)
	//assert.Equal(t, InvalidSpecReason, sameSvcsCond.Reason)
}

func TestValidateRolloutStrategyCanary(t *testing.T) {
	//zero := intstr.FromInt(0)
	//tests := []struct {
	//	name           string
	//	maxUnavailable *intstr.IntOrString
	//	maxSurge       *intstr.IntOrString
	//	steps          []v1alpha1.CanaryStep
	//
	//	notValid bool
	//	reason   string
	//	message  string
	//}{
	//	{
	//		name:           "Max Surge and Max Unavailable set to zero",
	//		maxUnavailable: &zero,
	//		maxSurge:       &zero,
	//
	//		notValid: true,
	//		reason:   InvalidSpecReason,
	//		message:  InvalidMaxSurgeMaxUnavailable,
	//	},
	//	{
	//		name: "setWeight and pause both set",
	//		steps: []v1alpha1.CanaryStep{{
	//			Pause:     &v1alpha1.RolloutPause{},
	//			SetWeight: pointer.Int32Ptr(10),
	//		}},
	//
	//		notValid: true,
	//		reason:   InvalidSpecReason,
	//		message:  InvalidStepMessage,
	//	},
	//	{
	//		name:  "experiment, setWeight, and pause are not set",
	//		steps: []v1alpha1.CanaryStep{{}},
	//
	//		notValid: true,
	//		reason:   InvalidSpecReason,
	//		message:  InvalidStepMessage,
	//	},
	//	{
	//		name: "setWeight over 0",
	//		steps: []v1alpha1.CanaryStep{{
	//			SetWeight: pointer.Int32Ptr(-1),
	//		}},
	//
	//		notValid: true,
	//		reason:   InvalidSpecReason,
	//		message:  InvalidSetWeightMessage,
	//	},
	//	{
	//		name: "setWeight less than 100",
	//		steps: []v1alpha1.CanaryStep{{
	//			SetWeight: pointer.Int32Ptr(110),
	//		}},
	//
	//		notValid: true,
	//		reason:   InvalidSpecReason,
	//		message:  InvalidSetWeightMessage,
	//	},
	//	{
	//		name: "Pause duration is not less than 0",
	//		steps: []v1alpha1.CanaryStep{{
	//			Pause: &v1alpha1.RolloutPause{
	//				Duration: v1alpha1.DurationFromInt(-1),
	//			},
	//		}},
	//
	//		notValid: true,
	//		reason:   InvalidSpecReason,
	//		message:  InvalidDurationMessage,
	//	},
	//	{
	//		name: "Pause duration invalid unit",
	//		steps: []v1alpha1.CanaryStep{{
	//			Pause: &v1alpha1.RolloutPause{
	//				Duration: v1alpha1.DurationFromString("10z"),
	//			},
	//		}},
	//
	//		notValid: true,
	//		reason:   InvalidSpecReason,
	//		message:  InvalidDurationMessage,
	//	},
	//}
	//for i := range tests {
	//	test := tests[i]
	//	t.Run(test.name, func(t *testing.T) {
	//		ro := &v1alpha1.Rollout{
	//			Spec: v1alpha1.RolloutSpec{
	//				Selector: &metav1.LabelSelector{
	//					MatchLabels: map[string]string{"key": "value"},
	//				},
	//				Strategy: v1alpha1.RolloutStrategy{
	//					Canary: &v1alpha1.CanaryStrategy{
	//						MaxUnavailable: test.maxUnavailable,
	//						MaxSurge:       test.maxSurge,
	//						Steps:          test.steps,
	//					},
	//				},
	//			},
	//		}
	//		cond := VerifyRolloutSpec(ro, nil)
	//		if test.notValid {
	//			assert.Equal(t, v1alpha1.InvalidSpec, cond.Type)
	//			assert.Equal(t, test.reason, cond.Reason)
	//			assert.Equal(t, test.message, cond.Message)
	//		} else {
	//			assert.Nil(t, cond)
	//		}
	//	})
	//}
}

//func TestInvalidAntiAffinity(t *testing.T) {
//	affinity := v1alpha1.AntiAffinity{}
//	reason, message := invalidAntiAffinity(affinity, "BlueGreen")
//	expectedMsg := fmt.Sprintf(MissingFieldMessage, ".Spec.Strategy.BlueGreen.AntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution or .Spec.Strategy.BlueGreen.AntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution")
//	assert.Equal(t, InvalidSpecReason, reason)
//	assert.Equal(t, expectedMsg, message)
//
//	affinity = v1alpha1.AntiAffinity{
//		RequiredDuringSchedulingIgnoredDuringExecution:  &v1alpha1.RequiredDuringSchedulingIgnoredDuringExecution{},
//		PreferredDuringSchedulingIgnoredDuringExecution: &v1alpha1.PreferredDuringSchedulingIgnoredDuringExecution{Weight: 1},
//	}
//	reason, message = invalidAntiAffinity(affinity, "Canary")
//	assert.Equal(t, InvalidSpecReason, reason)
//	assert.Equal(t, "Multiple Anti-Affinity Strategies can not be listed", message)
//}

//func TestValidateRolloutSpecAntiAffinity(t *testing.T) {
//	affinity := &v1alpha1.AntiAffinity{}
//	invalidRollout := &v1alpha1.Rollout{
//		Spec: v1alpha1.RolloutSpec{
//			Selector: &metav1.LabelSelector{
//				MatchLabels: map[string]string{"key": "value"},
//			},
//			Strategy: v1alpha1.RolloutStrategy{
//				BlueGreen: &v1alpha1.BlueGreenStrategy{
//					PreviewService: "preview",
//					ActiveService:  "active",
//					AntiAffinity:   affinity,
//				},
//			},
//		},
//	}
//	cond := VerifyRolloutSpec(invalidRollout, nil)
//	message := fmt.Sprintf(MissingFieldMessage, ".Spec.Strategy.BlueGreen.AntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution or .Spec.Strategy.BlueGreen.AntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution")
//	assert.Equal(t, InvalidSpecReason, cond.Reason)
//	assert.Equal(t, message, cond.Message)
//
//	invalidRollout.Spec.Strategy.BlueGreen = nil
//	invalidRollout.Spec.Strategy.Canary = &v1alpha1.CanaryStrategy{AntiAffinity: &v1alpha1.AntiAffinity{
//		PreferredDuringSchedulingIgnoredDuringExecution: &v1alpha1.PreferredDuringSchedulingIgnoredDuringExecution{
//			Weight: 1,
//		},
//		RequiredDuringSchedulingIgnoredDuringExecution: &v1alpha1.RequiredDuringSchedulingIgnoredDuringExecution{},
//	}}
//	cond = VerifyRolloutSpec(invalidRollout, nil)
//	assert.Equal(t, InvalidSpecReason, cond.Reason)
//	assert.Equal(t, "Multiple Anti-Affinity Strategies can not be listed", cond.Message)
//}

func TestInvalidMaxSurgeMaxUnavailable(t *testing.T) {
	r := func(maxSurge, maxUnavailable intstr.IntOrString) *v1alpha1.Rollout {
		return &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					Canary: &v1alpha1.CanaryStrategy{
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
