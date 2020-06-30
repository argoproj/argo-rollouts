package validation

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	corev1defaults "k8s.io/kubernetes/pkg/apis/core/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
)

func TestValidateRollout(t *testing.T) {
	numReplicas := int32(0)
	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"key": "value"},
	}
	ro := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Replicas: &numReplicas,
			Selector: selector,
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{
					PreviewService: "preview",
					ActiveService:  "active",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: selector.MatchLabels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Resources: corev1.ResourceRequirements{},
						Image:     "foo",
						Name:      "image-name",
					}},
				},
			},
		},
	}
	podTemplate := corev1.PodTemplate{
		Template: ro.Spec.Template,
	}
	corev1defaults.SetObjectDefaults_PodTemplate(&podTemplate)
	ro.Spec.Template = podTemplate.Template

	allErrs := ValidateRollout(ro)
	assert.Empty(t, allErrs)
}

func TestValidateRolloutStrategy(t *testing.T) {
	rollout := v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{},
		},
	}

	allErrs := ValidateRolloutStrategy(&rollout, field.NewPath(""))
	message := fmt.Sprintf(MissingFieldMessage, ".Spec.Strategy.Canary or .Spec.Strategy.BlueGreen")
	assert.Equal(t, message, allErrs[0].Detail)

	rollout.Spec.Strategy.BlueGreen = &v1alpha1.BlueGreenStrategy{}
	rollout.Spec.Strategy.Canary = &v1alpha1.CanaryStrategy{}
	allErrs = ValidateRolloutStrategy(&rollout, field.NewPath(""))
	assert.Equal(t, InvalidStrategyMessage, allErrs[0].Detail)
}

func TestValidateRolloutStrategyBlueGreen(t *testing.T) {
	scaleDownDelayRevisionLimit := defaults.DefaultRevisionHistoryLimit + 1
	autoPromotionSeconds := int32(30)
	rollout := v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{
					PreviewService:              "service-name",
					ActiveService:               "service-name",
					ScaleDownDelayRevisionLimit: &scaleDownDelayRevisionLimit,
					AutoPromotionSeconds:        &autoPromotionSeconds,
				},
			},
		},
	}

	allErrs := ValidateRolloutStrategyBlueGreen(&rollout, field.NewPath("spec", "strategy", "blueGreen"))
	assert.Len(t, allErrs, 3)
	assert.Equal(t, DuplicatedServicesBlueGreenMessage, allErrs[0].BadValue)
	assert.Equal(t, ScaleDownLimitLargerThanRevisionLimit, allErrs[1].Detail)
	assert.Equal(t, InvalidAutoPromotionSecondsMessage, allErrs[2].Detail)
}

func TestValidateRolloutStrategyCanary(t *testing.T) {
	setWeight := int32(101)
	pauseDuration := intstr.FromInt(-1)
	canaryStrategy := &v1alpha1.CanaryStrategy{
		CanaryService: "stable-service",
		StableService: "stable-service",
		TrafficRouting: &v1alpha1.RolloutTrafficRouting{
			SMI: &v1alpha1.SMITrafficRouting{},
		},
		Steps: []v1alpha1.CanaryStep{{}},
	}
	ro := &v1alpha1.Rollout{}
	ro.Spec.Strategy.Canary = canaryStrategy

	allErrs := ValidateRolloutStrategyCanary(ro, field.NewPath(""))
	assert.Equal(t, DuplicatedServicesCanaryMessage, allErrs[0].Detail)

	ro.Spec.Strategy.Canary.CanaryService = ""
	allErrs = ValidateRolloutStrategyCanary(ro, field.NewPath(""))
	assert.Equal(t, InvalidTrafficRoutingMessage, allErrs[0].Detail)

	ro.Spec.Strategy.Canary.CanaryService = "canary-service"
	allErrs = ValidateRolloutStrategyCanary(ro, field.NewPath(""))
	assert.Equal(t, InvalidStepMessage, allErrs[0].Detail)

	ro.Spec.Strategy.Canary.Steps[0].SetWeight = &setWeight
	allErrs = ValidateRolloutStrategyCanary(ro, field.NewPath(""))
	assert.Equal(t, InvalidSetWeightMessage, allErrs[0].Detail)

	ro.Spec.Strategy.Canary.Steps[0].Pause = &v1alpha1.RolloutPause{
		Duration: &pauseDuration,
	}
	ro.Spec.Strategy.Canary.Steps[0].SetWeight = nil
	allErrs = ValidateRolloutStrategyCanary(ro, field.NewPath(""))
	assert.Equal(t, InvalidDurationMessage, allErrs[0].Detail)
}

func TestValidateRolloutStrategyAntiAffinity(t *testing.T) {
	antiAffinity := v1alpha1.AntiAffinity{
		PreferredDuringSchedulingIgnoredDuringExecution: nil,
		RequiredDuringSchedulingIgnoredDuringExecution:  nil,
	}
	allErrs := ValidateRolloutStrategyAntiAffinity(&antiAffinity, field.NewPath("antiAffinity"))
	assert.Equal(t, InvalidAntiAffinityStrategyMessage, allErrs[0].Detail)

	antiAffinity = v1alpha1.AntiAffinity{
		PreferredDuringSchedulingIgnoredDuringExecution: &v1alpha1.PreferredDuringSchedulingIgnoredDuringExecution{
			Weight: 101,
		},
	}
	allErrs = ValidateRolloutStrategyAntiAffinity(&antiAffinity, field.NewPath("antiAffinity"))
	assert.Equal(t, InvalidAntiAffinityWeightMessage, allErrs[0].Detail)
}

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
	path := &field.Path{}
	assert.Equal(t, InvalidMaxSurgeMaxUnavailable, invalidMaxSurgeMaxUnavailable(r(intstr.FromInt(0), intstr.FromInt(0)), path)[0].Detail)
	assert.Equal(t, InvalidMaxSurgeMaxUnavailable, invalidMaxSurgeMaxUnavailable(r(intstr.FromString("0"), intstr.FromInt(0)), path)[0].Detail)
	assert.Equal(t, InvalidMaxSurgeMaxUnavailable, invalidMaxSurgeMaxUnavailable(r(intstr.FromString("0%"), intstr.FromInt(0)), path)[0].Detail)
	assert.Equal(t, InvalidMaxSurgeMaxUnavailable, invalidMaxSurgeMaxUnavailable(r(intstr.FromInt(0), intstr.FromString("0")), path)[0].Detail)
	assert.Equal(t, InvalidMaxSurgeMaxUnavailable, invalidMaxSurgeMaxUnavailable(r(intstr.FromInt(0), intstr.FromString("0%")), path)[0].Detail)
	assert.Equal(t, InvalidMaxSurgeMaxUnavailable, invalidMaxSurgeMaxUnavailable(r(intstr.FromString("0"), intstr.FromString("0")), path)[0].Detail)
	assert.Equal(t, InvalidMaxSurgeMaxUnavailable, invalidMaxSurgeMaxUnavailable(r(intstr.FromString("0%"), intstr.FromString("0%")), path)[0].Detail)
}

func TestHasMultipleStepsType(t *testing.T) {
	setWeight := int32(1)
	pauseDuration := intstr.FromInt(1)
	step := v1alpha1.CanaryStep{
		SetWeight: &setWeight,
	}

	allErrs := hasMultipleStepsType(step, field.NewPath(""))
	assert.Empty(t, allErrs)

	step.Pause = &v1alpha1.RolloutPause{
		Duration: &pauseDuration,
	}
	allErrs = hasMultipleStepsType(step, field.NewPath(""))
	assert.Equal(t, InvalidStepMessage, allErrs[0].Detail)
}
