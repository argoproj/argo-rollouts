package validation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
)

const (
	errTrafficRoutingWithExperimentSupport = "Experiment template weight is only available for TrafficRouting with SMI, ALB, and Istio at this time"
)

func TestValidateRollout(t *testing.T) {
	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"key": "value"},
	}
	ro := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Selector: selector,
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{},
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
	t.Run("missing selector", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Selector = nil
		allErrs := ValidateRollout(invalidRo)
		message := fmt.Sprintf(MissingFieldMessage, ".spec.selector")
		assert.Equal(t, message, allErrs[0].Detail)
	})

	t.Run("empty selector", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Selector = &metav1.LabelSelector{}
		allErrs := ValidateRollout(invalidRo)
		assert.Equal(t, "empty selector is invalid for deployment", allErrs[0].Detail)
	})

	t.Run("invalid progressDeadlineSeconds", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.MinReadySeconds = defaults.GetProgressDeadlineSecondsOrDefault(invalidRo) + 1
		allErrs := ValidateRollout(invalidRo)
		assert.Equal(t, "spec.progressDeadlineSeconds", allErrs[0].Field)
		assert.Equal(t, "must be greater than minReadySeconds", allErrs[0].Detail)

	})

	t.Run("successful run", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary = nil
		invalidRo.Spec.Strategy.BlueGreen = &v1alpha1.BlueGreenStrategy{
			ActiveService:  "active",
			PreviewService: "preview",
		}
		allErrs := ValidateRollout(invalidRo)
		assert.Empty(t, allErrs)
	})

	t.Run("privileged container", func(t *testing.T) {
		ro := ro.DeepCopy()
		ro.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		}
		allErrs := ValidateRollout(ro)
		assert.Empty(t, allErrs)
	})

}

func TestValidateRolloutStrategy(t *testing.T) {
	rollout := v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{},
		},
	}

	allErrs := ValidateRolloutStrategy(&rollout, field.NewPath(""))
	message := fmt.Sprintf(MissingFieldMessage, ".spec.strategy.canary or .spec.strategy.blueGreen")
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
					AutoPromotionSeconds:        autoPromotionSeconds,
				},
			},
		},
	}

	allErrs := ValidateRolloutStrategyBlueGreen(&rollout, field.NewPath("spec", "strategy", "blueGreen"))
	assert.Len(t, allErrs, 2)
	assert.Equal(t, DuplicatedServicesBlueGreenMessage, allErrs[0].Detail)
	assert.Equal(t, ScaleDownLimitLargerThanRevisionLimit, allErrs[1].Detail)
}

func TestValidateRolloutStrategyCanary(t *testing.T) {
	canaryStrategy := &v1alpha1.CanaryStrategy{
		CanaryService: "canary",
		StableService: "stable",
		TrafficRouting: &v1alpha1.RolloutTrafficRouting{
			ALB: &v1alpha1.ALBTrafficRouting{RootService: "root-service"},
		},
		Steps: []v1alpha1.CanaryStep{{}},
	}
	ro := &v1alpha1.Rollout{}
	ro.Spec.Strategy.Canary = canaryStrategy

	invalidArgs := []v1alpha1.AnalysisRunArgument{
		{
			Name: "metadata.labels['app']",
			ValueFrom: &v1alpha1.ArgumentValueFrom{
				FieldRef: &v1alpha1.FieldRef{FieldPath: "metadata.label['app']"},
			},
		},
		{
			Name:  "value-key",
			Value: "hardcoded-value",
		},
	}
	rolloutAnalysisStep := &v1alpha1.RolloutAnalysis{
		Args: invalidArgs,
	}

	rolloutExperimentStep := &v1alpha1.RolloutExperimentStep{
		Analyses: []v1alpha1.RolloutExperimentStepAnalysisTemplateRef{
			{
				Args: invalidArgs,
			},
		},
	}

	t.Run("duplicate services", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.CanaryService = "stable"
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, DuplicatedServicesCanaryMessage, allErrs[0].Detail)
	})

	t.Run("duplicate ping pong services", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.PingPong = &v1alpha1.PingPongSpec{PingService: "ping", PongService: "ping"}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, DuplicatedPingPongServicesMessage, allErrs[0].Detail)
	})

	t.Run("ping services using only", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.PingPong = &v1alpha1.PingPongSpec{PingService: "ping", PongService: ""}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalidPingPongProvidedMessage, allErrs[0].Detail)
	})

	t.Run("pong service using only", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.PingPong = &v1alpha1.PingPongSpec{PingService: "", PongService: "pong"}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalidPingPongProvidedMessage, allErrs[0].Detail)
	})

	t.Run("missed ALB root service for the ping-pong feature", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.PingPong = &v1alpha1.PingPongSpec{PingService: "ping", PongService: "pong"}
		invalidRo.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			ALB: &v1alpha1.ALBTrafficRouting{RootService: ""},
		}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, MissedAlbRootServiceMessage, allErrs[0].Detail)
	})

	t.Run("ping-pong feature without the ALB traffic routing", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.PingPong = &v1alpha1.PingPongSpec{PingService: "ping", PongService: "pong"}
		invalidRo.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			Nginx: &v1alpha1.NginxTrafficRouting{StableIngress: "stable-ingress"},
		}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, PingPongWithAlbOnlyMessage, allErrs[0].Detail)
	})

	t.Run("invalid traffic routing", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.CanaryService = ""
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalidTrafficRoutingMessage, allErrs[0].Detail)
	})

	t.Run("invalid setCanaryScale without trafficRouting", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.Steps[0].SetCanaryScale = &v1alpha1.SetCanaryScale{}
		invalidRo.Spec.Strategy.Canary.TrafficRouting = nil
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalidSetCanaryScaleTrafficPolicy, allErrs[0].Detail)
	})

	t.Run("invalid canary step", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalidStepMessage, allErrs[0].Detail)
	})

	t.Run("invalid set weight value", func(t *testing.T) {
		setWeight := int32(101)
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.Steps[0].SetWeight = &setWeight
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalidSetWeightMessage, allErrs[0].Detail)
	})

	t.Run("invalid duration set in paused step", func(t *testing.T) {
		pauseDuration := intstr.FromInt(-1)
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.Steps[0].Pause = &v1alpha1.RolloutPause{
			Duration: &pauseDuration,
		}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalidDurationMessage, allErrs[0].Detail)
	})
	t.Run("invalid metadata references in analysis step", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.Steps[0].Analysis = rolloutAnalysisStep
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalidAnalysisArgsMessage, allErrs[0].Detail)
	})
	t.Run("invalid metadata references in experiment step", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.Steps[0].Experiment = rolloutExperimentStep
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalidAnalysisArgsMessage, allErrs[0].Detail)
	})
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

func TestValidateRolloutStrategyCanarySetHeaderRoute(t *testing.T) {
	ro := &v1alpha1.Rollout{}
	ro.Spec.Strategy.Canary = &v1alpha1.CanaryStrategy{
		CanaryService: "canary",
		StableService: "stable",
	}

	t.Run("using SetHeaderRoute step without the traffic routing", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetHeaderRoute: &v1alpha1.SetHeaderRoute{
				Match: []v1alpha1.HeaderRoutingMatch{
					{
						HeaderName:  "agent",
						HeaderValue: &v1alpha1.StringMatch{Exact: "chrome"},
					},
				},
			},
		}}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalidSetHeaderRouteTrafficPolicy, allErrs[0].Detail)
	})
}

func TestValidateRolloutStrategyCanarySetHeaderRouteIstio(t *testing.T) {
	ro := &v1alpha1.Rollout{}
	ro.Spec.Strategy.Canary = &v1alpha1.CanaryStrategy{
		CanaryService: "canary",
		StableService: "stable",
		TrafficRouting: &v1alpha1.RolloutTrafficRouting{
			Istio: &v1alpha1.IstioTrafficRouting{
				VirtualService: &v1alpha1.IstioVirtualService{Name: "virtual-service"},
			},
		},
	}

	t.Run("using SetHeaderRoute step with multiple values", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetHeaderRoute: &v1alpha1.SetHeaderRoute{
				Match: []v1alpha1.HeaderRoutingMatch{
					{
						HeaderName: "agent",
						HeaderValue: &v1alpha1.StringMatch{
							Exact: "chrome",
							Regex: "chrome(.*)",
						},
					},
				},
			},
		}}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalidStringMatchMultipleValuePolicy, allErrs[0].Detail)
	})

	t.Run("using SetHeaderRoute step with missed values", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetHeaderRoute: &v1alpha1.SetHeaderRoute{
				Match: []v1alpha1.HeaderRoutingMatch{
					{
						HeaderName: "agent",
					},
				},
			},
		}}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalidStringMatchMissedValuePolicy, allErrs[0].Detail)
	})

	t.Run("using SetHeaderRoute step without managedRoutes defined but missing route", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetHeaderRoute: &v1alpha1.SetHeaderRoute{
				Match: []v1alpha1.HeaderRoutingMatch{
					{
						HeaderName:  "agent",
						HeaderValue: &v1alpha1.StringMatch{Exact: "exact"},
					},
				},
			},
		}}
		invalidRo.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes = append(invalidRo.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes, v1alpha1.MangedRoutes{
			Name: "not-in-steps",
		})
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalideStepRouteNameNotFoundInManagedRoutes, allErrs[0].Detail)
	})
}

func TestValidateRolloutStrategyCanarySetHeaderRoutingALB(t *testing.T) {
	ro := &v1alpha1.Rollout{}
	ro.Spec.Strategy.Canary = &v1alpha1.CanaryStrategy{
		CanaryService: "canary",
		StableService: "stable",
		TrafficRouting: &v1alpha1.RolloutTrafficRouting{
			ALB: &v1alpha1.ALBTrafficRouting{
				RootService: "action_name",
			},
		},
	}

	t.Run("using SetHeaderRouting step with multiple values", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetHeaderRoute: &v1alpha1.SetHeaderRoute{
				Match: []v1alpha1.HeaderRoutingMatch{
					{
						HeaderName: "agent",
						HeaderValue: &v1alpha1.StringMatch{
							Exact: "chrome",
							Regex: "chrome(.*)",
						},
					},
				},
			},
		}}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalidSetHeaderRouteALBValuePolicy, allErrs[0].Detail)
	})

	t.Run("using SetHeaderRouting step with missed values", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetHeaderRoute: &v1alpha1.SetHeaderRoute{
				Match: []v1alpha1.HeaderRoutingMatch{
					{
						HeaderName: "agent",
					},
				},
			},
		}}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalidStringMatchMissedValuePolicy, allErrs[0].Detail)
	})

	t.Run("using SetHeaderRouting step with invalid ALB match value", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetHeaderRoute: &v1alpha1.SetHeaderRoute{
				Match: []v1alpha1.HeaderRoutingMatch{
					{
						HeaderName: "agent",
						HeaderValue: &v1alpha1.StringMatch{
							Prefix: "chrome",
						},
					},
				},
			},
		}}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalidSetHeaderRouteALBValuePolicy, allErrs[0].Detail)
	})
}

func TestValidateRolloutStrategyCanarySetMirrorRouteIstio(t *testing.T) {
	ro := &v1alpha1.Rollout{}
	ro.Spec.Strategy.Canary = &v1alpha1.CanaryStrategy{
		CanaryService: "canary",
		StableService: "stable",
		TrafficRouting: &v1alpha1.RolloutTrafficRouting{
			Istio: &v1alpha1.IstioTrafficRouting{
				VirtualService: &v1alpha1.IstioVirtualService{Name: "virtual-service"},
			},
		},
	}

	t.Run("using SetMirrorRoute step without the traffic routing", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.TrafficRouting = nil
		invalidRo.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetMirrorRoute: &v1alpha1.SetMirrorRoute{
				Name:       "test-mirror-1",
				Match:      nil,
				Percentage: nil,
			},
		}}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalidSetMirrorRouteTrafficPolicy, allErrs[0].Detail)
	})

	t.Run("using SetMirrorRoute step with multiple values", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetMirrorRoute: &v1alpha1.SetMirrorRoute{
				Name: "test-mirror-1",
				Match: []v1alpha1.RouteMatch{{
					Method: &v1alpha1.StringMatch{
						Exact:  "test",
						Prefix: "test",
					},
					Path:    nil,
					Headers: nil,
				}},
				Percentage: nil,
			},
		}}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalidStringMatchMultipleValuePolicy, allErrs[0].Detail)
	})

	t.Run("using SetMirrorRoute step with missed match and no kind", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetMirrorRoute: &v1alpha1.SetMirrorRoute{
				Name: "test-mirror-1",
				Match: []v1alpha1.RouteMatch{{
					Method:  &v1alpha1.StringMatch{},
					Path:    nil,
					Headers: nil,
				}},
				Percentage: nil,
			},
		}}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalidStringMatchMissedValuePolicy, allErrs[0].Detail)
	})

	t.Run("using SetMirrorRoute step without managedRoutes not defined", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetMirrorRoute: &v1alpha1.SetMirrorRoute{
				Name: "test-mirror-1",
				Match: []v1alpha1.RouteMatch{{
					Method: &v1alpha1.StringMatch{
						Exact: "exact",
					},
				}},
				Percentage: nil,
			},
		}}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, fmt.Sprintf(MissingFieldMessage, "spec.strategy.canary.trafficRouting.managedRoutes"), allErrs[0].Detail)
	})

	t.Run("using SetMirrorRoute step without managedRoutes defined but missing route", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetMirrorRoute: &v1alpha1.SetMirrorRoute{
				Name: "test-mirror-1",
				Match: []v1alpha1.RouteMatch{{
					Method: &v1alpha1.StringMatch{
						Exact: "GET",
					},
					Path: &v1alpha1.StringMatch{
						Prefix: "/",
					},
				}},
				Percentage: nil,
			},
		}}
		invalidRo.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes = append(invalidRo.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes, v1alpha1.MangedRoutes{
			Name: "not-in-steps",
		})
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, InvalideStepRouteNameNotFoundInManagedRoutes, allErrs[0].Detail)
	})

	t.Run("using SetMirrorRoute step with managedRoutes defined", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetMirrorRoute: &v1alpha1.SetMirrorRoute{
				Name: "test-mirror-1",
				Match: []v1alpha1.RouteMatch{{
					Method: &v1alpha1.StringMatch{
						Exact: "GET",
					},
					Path: &v1alpha1.StringMatch{
						Prefix: "/",
					},
				}},
				Percentage: nil,
			},
		}}
		invalidRo.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes = append(invalidRo.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes, v1alpha1.MangedRoutes{
			Name: "test-mirror-1",
		})
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Len(t, allErrs, 0)
	})
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

func TestCanaryScaleDownDelaySeconds(t *testing.T) {
	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"key": "value"},
	}
	ro := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Selector: selector,
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService:         "stable",
					CanaryService:         "canary",
					ScaleDownDelaySeconds: pointer.Int32Ptr(60),
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
	t.Run("scaleDownDelaySeconds with basic canary", func(t *testing.T) {
		ro := ro.DeepCopy()
		allErrs := ValidateRollout(ro)
		assert.EqualError(t, allErrs[0], fmt.Sprintf("spec.strategy.scaleDownDelaySeconds: Invalid value: 60: %s", InvalidCanaryScaleDownDelay))
	})
	t.Run("scaleDownDelaySeconds with traffic weight canary", func(t *testing.T) {
		ro := ro.DeepCopy()
		ro.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			SMI: &v1alpha1.SMITrafficRouting{},
		}
		allErrs := ValidateRollout(ro)
		assert.Empty(t, allErrs)
	})
}

func TestCanaryDynamicStableScale(t *testing.T) {
	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"key": "value"},
	}
	ro := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Selector: selector,
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService:      "stable",
					CanaryService:      "canary",
					DynamicStableScale: true,
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
	t.Run("dynamicStableScale with basic canary", func(t *testing.T) {
		ro := ro.DeepCopy()
		allErrs := ValidateRollout(ro)
		assert.EqualError(t, allErrs[0], fmt.Sprintf("spec.strategy.dynamicStableScale: Invalid value: true: %s", InvalidCanaryDynamicStableScale))
	})
	t.Run("dynamicStableScale with scaleDownDelaySeconds", func(t *testing.T) {
		ro := ro.DeepCopy()
		ro.Spec.Strategy.Canary.ScaleDownDelaySeconds = pointer.Int32Ptr(60)
		ro.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			SMI: &v1alpha1.SMITrafficRouting{},
		}
		allErrs := ValidateRollout(ro)
		assert.EqualError(t, allErrs[0], fmt.Sprintf("spec.strategy.dynamicStableScale: Invalid value: true: %s", InvalidCanaryDynamicStableScaleWithScaleDownDelay))
	})

}

func TestWorkloadRefWithTemplate(t *testing.T) {
	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"key": "value"},
	}
	ro := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			WorkloadRef: &v1alpha1.ObjectRef{
				Name:       "my-deployment",
				Kind:       "Deployment",
				APIVersion: "apps/v1",
			},
			Selector: selector,
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService: "stable",
					CanaryService: "canary",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: selector.MatchLabels,
				},
			},
		},
	}
	t.Run("workload reference with template", func(t *testing.T) {
		ro := ro.DeepCopy()
		allErrs := ValidateRollout(ro)
		assert.Equal(t, 1, len(allErrs))
		assert.EqualError(t, allErrs[0], "spec.template: Internal error: template must be empty for workload reference rollout")
	})
	t.Run("valid workload reference with selector", func(t *testing.T) {
		ro := ro.DeepCopy()
		ro.Spec.Template = corev1.PodTemplateSpec{}
		allErrs := ValidateRollout(ro)
		assert.Equal(t, 0, len(allErrs))
	})
	t.Run("valid workload reference without selector", func(t *testing.T) {
		ro := ro.DeepCopy()
		ro.Spec.Selector = nil
		ro.Spec.Template = corev1.PodTemplateSpec{}
		allErrs := ValidateRollout(ro)
		assert.Equal(t, 0, len(allErrs))
	})
}

func TestCanaryExperimentStepWithWeight(t *testing.T) {
	canaryStrategy := &v1alpha1.CanaryStrategy{
		CanaryService: "canary",
		StableService: "stable",
		Steps: []v1alpha1.CanaryStep{{
			Experiment: &v1alpha1.RolloutExperimentStep{
				Templates: []v1alpha1.RolloutExperimentTemplate{{
					Name:   "template",
					Weight: pointer.Int32Ptr(20),
				}},
			},
		}},
	}
	ro := &v1alpha1.Rollout{}
	ro.Spec.Strategy.Canary = canaryStrategy

	t.Run("invalid - no TrafficRouting set", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, 1, len(allErrs))
		assert.Equal(t, "Experiment template weight cannot be set unless TrafficRouting is enabled", allErrs[0].Detail)
	})

	t.Run("invalid - empty TrafficRouting", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, 1, len(allErrs))
		assert.Equal(t, errTrafficRoutingWithExperimentSupport, allErrs[0].Detail)
	})

	t.Run("unsupported - Nginx TrafficRouting", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			Nginx: &v1alpha1.NginxTrafficRouting{
				StableIngress: "nginx-ingress",
			},
		}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, 1, len(allErrs))
		assert.Equal(t, errTrafficRoutingWithExperimentSupport, allErrs[0].Detail)
	})

	t.Run("unsupported - Ambassador TrafficRouting", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			Ambassador: &v1alpha1.AmbassadorTrafficRouting{
				Mappings: []string{"stable-mapping"},
			},
		}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, 1, len(allErrs))
		assert.Equal(t, errTrafficRoutingWithExperimentSupport, allErrs[0].Detail)
	})

	t.Run("unsupported - Istio TrafficRouting", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			Istio: &v1alpha1.IstioTrafficRouting{
				VirtualService: &v1alpha1.IstioVirtualService{
					Name: "virtualSvc",
				},
			},
		}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, 0, len(allErrs))
	})

	t.Run("success - SMI TrafficRouting", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			SMI: &v1alpha1.SMITrafficRouting{},
		}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, 0, len(allErrs))
	})

	t.Run("success - ALB TrafficRouting", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			ALB: &v1alpha1.ALBTrafficRouting{},
		}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, 0, len(allErrs))
	})

	t.Run("unsupported - AppMesh TrafficRouting", func(t *testing.T) {
		invalidRo := ro.DeepCopy()
		invalidRo.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			AppMesh: &v1alpha1.AppMeshTrafficRouting{},
		}
		allErrs := ValidateRolloutStrategyCanary(invalidRo, field.NewPath(""))
		assert.Equal(t, 1, len(allErrs))
		assert.Equal(t, errTrafficRoutingWithExperimentSupport, allErrs[0].Detail)
	})

}
