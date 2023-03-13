package defaults

import (
	"testing"
	"time"

	"k8s.io/utils/pointer"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestGetStringOrDefault(t *testing.T) {
	assert.Equal(t, "some value", GetStringOrDefault("some value", "default value"))
	assert.Equal(t, "default value", GetStringOrDefault("", "default value"))
}

func TestGetReplicasOrDefault(t *testing.T) {
	replicas := int32(2)
	assert.Equal(t, replicas, GetReplicasOrDefault(&replicas))
	assert.Equal(t, DefaultReplicas, GetReplicasOrDefault(nil))
}

func TestGetExperimentScaleDownDelaySecondsOrDefault(t *testing.T) {
	exp := v1alpha1.Experiment{
		Spec: v1alpha1.ExperimentSpec{
			ScaleDownDelaySeconds: pointer.Int32Ptr(0),
		},
	}
	// Custom value
	assert.Equal(t, *exp.Spec.ScaleDownDelaySeconds, GetExperimentScaleDownDelaySecondsOrDefault(&exp))

	// Default value
	exp.Spec.ScaleDownDelaySeconds = nil
	assert.Equal(t, DefaultScaleDownDelaySeconds, GetExperimentScaleDownDelaySecondsOrDefault(&exp))
}

func TestGetRevisionHistoryOrDefault(t *testing.T) {
	revisionHistoryLimit := int32(2)
	rolloutNonDefaultValue := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			RevisionHistoryLimit: &revisionHistoryLimit,
		},
	}

	assert.Equal(t, revisionHistoryLimit, GetRevisionHistoryLimitOrDefault(rolloutNonDefaultValue))
	rolloutDefaultValue := &v1alpha1.Rollout{}
	assert.Equal(t, DefaultRevisionHistoryLimit, GetRevisionHistoryLimitOrDefault(rolloutDefaultValue))
}

func TestGetAnalysisRunSuccessfulHistoryLimitOrDefault(t *testing.T) {
	succeedHistoryLimit := int32(2)
	rolloutNonDefaultValue := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Analysis: &v1alpha1.AnalysisRunStrategy{SuccessfulRunHistoryLimit: &succeedHistoryLimit},
		},
	}

	assert.Equal(t, succeedHistoryLimit, GetAnalysisRunSuccessfulHistoryLimitOrDefault(rolloutNonDefaultValue))
	assert.Equal(t, DefaultAnalysisRunSuccessfulHistoryLimit, GetAnalysisRunSuccessfulHistoryLimitOrDefault(&v1alpha1.Rollout{}))
	assert.Equal(t, DefaultAnalysisRunSuccessfulHistoryLimit, GetAnalysisRunSuccessfulHistoryLimitOrDefault(&v1alpha1.Rollout{Spec: v1alpha1.RolloutSpec{}}))
	assert.Equal(t, DefaultAnalysisRunSuccessfulHistoryLimit, GetAnalysisRunSuccessfulHistoryLimitOrDefault(&v1alpha1.Rollout{Spec: v1alpha1.RolloutSpec{Analysis: &v1alpha1.AnalysisRunStrategy{}}}))
}

func TestGetAnalysisRunUnsuccessfulHistoryLimitOrDefault(t *testing.T) {
	failedHistoryLimit := int32(3)
	rolloutNonDefaultValue := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Analysis: &v1alpha1.AnalysisRunStrategy{UnsuccessfulRunHistoryLimit: &failedHistoryLimit},
		},
	}

	assert.Equal(t, failedHistoryLimit, GetAnalysisRunUnsuccessfulHistoryLimitOrDefault(rolloutNonDefaultValue))
	assert.Equal(t, DefaultAnalysisRunUnsuccessfulHistoryLimit, GetAnalysisRunUnsuccessfulHistoryLimitOrDefault(&v1alpha1.Rollout{}))
	assert.Equal(t, DefaultAnalysisRunUnsuccessfulHistoryLimit, GetAnalysisRunUnsuccessfulHistoryLimitOrDefault(&v1alpha1.Rollout{Spec: v1alpha1.RolloutSpec{}}))
	assert.Equal(t, DefaultAnalysisRunUnsuccessfulHistoryLimit, GetAnalysisRunUnsuccessfulHistoryLimitOrDefault(&v1alpha1.Rollout{Spec: v1alpha1.RolloutSpec{Analysis: &v1alpha1.AnalysisRunStrategy{}}}))
}

func TestGetMaxSurgeOrDefault(t *testing.T) {
	maxSurge := intstr.FromInt(2)
	rolloutNonDefaultValue := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					MaxSurge: &maxSurge,
				},
			},
		},
	}

	assert.Equal(t, maxSurge, *GetMaxSurgeOrDefault(rolloutNonDefaultValue))
	rolloutDefaultValue := &v1alpha1.Rollout{}
	assert.Equal(t, intstr.FromString(DefaultMaxSurge), *GetMaxSurgeOrDefault(rolloutDefaultValue))
}

func TestGetMaxUnavailableOrDefault(t *testing.T) {
	maxUnavailable := intstr.FromInt(2)
	rolloutCanaryNonDefaultValue := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					MaxUnavailable: &maxUnavailable,
				},
			},
		},
	}

	assert.Equal(t, maxUnavailable, *GetMaxUnavailableOrDefault(rolloutCanaryNonDefaultValue))

	rolloutBlueGreenNonDefaultValue := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{
					MaxUnavailable: &maxUnavailable,
				},
			},
		},
	}
	assert.Equal(t, maxUnavailable, *GetMaxUnavailableOrDefault(rolloutBlueGreenNonDefaultValue))

	rolloutDefaultValue := &v1alpha1.Rollout{}
	assert.Equal(t, intstr.FromString(DefaultMaxUnavailable), *GetMaxUnavailableOrDefault(rolloutDefaultValue))
}

func TestGetCanaryIngressAnnotationPrefixOrDefault(t *testing.T) {
	customPrefix := "custom.nginx.ingress.kubernetes.io"
	rolloutNonDefaultValue := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Nginx: &v1alpha1.NginxTrafficRouting{
							AnnotationPrefix: customPrefix,
						},
					},
				},
			},
		},
	}

	assert.Equal(t, customPrefix, GetCanaryIngressAnnotationPrefixOrDefault(rolloutNonDefaultValue))
	rolloutDefaultValue := &v1alpha1.Rollout{}
	assert.Equal(t, "nginx.ingress.kubernetes.io", GetCanaryIngressAnnotationPrefixOrDefault(rolloutDefaultValue))
}

func TestGetProgressDeadlineSecondsOrDefault(t *testing.T) {
	seconds := int32(2)
	rolloutNonDefaultValue := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			ProgressDeadlineSeconds: &seconds,
		},
	}

	assert.Equal(t, seconds, GetProgressDeadlineSecondsOrDefault(rolloutNonDefaultValue))
	rolloutDefaultValue := &v1alpha1.Rollout{}
	assert.Equal(t, DefaultProgressDeadlineSeconds, GetProgressDeadlineSecondsOrDefault(rolloutDefaultValue))
}

func TestGetScaleDownDelaySecondsOrDefault(t *testing.T) {
	{
		scaleDownDelaySeconds := int32(60)
		blueGreenNonDefaultValue := &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					BlueGreen: &v1alpha1.BlueGreenStrategy{
						ScaleDownDelaySeconds: &scaleDownDelaySeconds,
					},
				},
			},
		}
		assert.Equal(t, time.Duration(scaleDownDelaySeconds)*time.Second, GetScaleDownDelaySecondsOrDefault(blueGreenNonDefaultValue))
	}
	{
		rolloutNoStrategyDefaultValue := &v1alpha1.Rollout{}
		assert.Equal(t, time.Duration(0), GetScaleDownDelaySecondsOrDefault(rolloutNoStrategyDefaultValue))
	}
	{
		rolloutNoScaleDownDelaySeconds := &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					BlueGreen: &v1alpha1.BlueGreenStrategy{},
				},
			},
		}
		assert.Equal(t, time.Duration(DefaultScaleDownDelaySeconds)*time.Second, GetScaleDownDelaySecondsOrDefault(rolloutNoScaleDownDelaySeconds))
	}
	{
		scaleDownDelaySeconds := int32(60)
		canaryNoTrafficRouting := &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					Canary: &v1alpha1.CanaryStrategy{
						ScaleDownDelaySeconds: &scaleDownDelaySeconds,
					},
				},
			},
		}
		assert.Equal(t, time.Duration(0), GetScaleDownDelaySecondsOrDefault(canaryNoTrafficRouting))
	}
	{
		scaleDownDelaySeconds := int32(60)
		canaryWithTrafficRouting := &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					Canary: &v1alpha1.CanaryStrategy{
						ScaleDownDelaySeconds: &scaleDownDelaySeconds,
						TrafficRouting:        &v1alpha1.RolloutTrafficRouting{},
					},
				},
			},
		}
		assert.Equal(t, time.Duration(scaleDownDelaySeconds)*time.Second, GetScaleDownDelaySecondsOrDefault(canaryWithTrafficRouting))
	}
}

func TestGetAbortScaleDownDelaySecondsOrDefault(t *testing.T) {
	{
		abortScaleDownDelaySeconds := int32(60)
		blueGreenNonDefaultValue := &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					BlueGreen: &v1alpha1.BlueGreenStrategy{
						AbortScaleDownDelaySeconds: &abortScaleDownDelaySeconds,
					},
				},
			},
		}
		abortDelay, wasSet := GetAbortScaleDownDelaySecondsOrDefault(blueGreenNonDefaultValue)
		assert.Equal(t, time.Duration(abortScaleDownDelaySeconds)*time.Second, *abortDelay)
		assert.True(t, wasSet)
	}
	{
		// dont scale down preview
		abortScaleDownDelaySeconds := int32(0)
		blueGreenZeroValue := &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					BlueGreen: &v1alpha1.BlueGreenStrategy{
						AbortScaleDownDelaySeconds: &abortScaleDownDelaySeconds,
					},
				},
			},
		}
		abortDelay, wasSet := GetAbortScaleDownDelaySecondsOrDefault(blueGreenZeroValue)
		assert.Nil(t, abortDelay)
		assert.True(t, wasSet)
	}
	{
		blueGreenDefaultValue := &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					BlueGreen: &v1alpha1.BlueGreenStrategy{},
				},
			},
		}
		abortDelay, wasSet := GetAbortScaleDownDelaySecondsOrDefault(blueGreenDefaultValue)
		assert.Equal(t, time.Duration(DefaultAbortScaleDownDelaySeconds)*time.Second, *abortDelay)
		assert.False(t, wasSet)
	}
	{
		abortScaleDownDelaySeconds := int32(60)
		canaryNonDefaultValue := &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					Canary: &v1alpha1.CanaryStrategy{
						AbortScaleDownDelaySeconds: &abortScaleDownDelaySeconds,
						TrafficRouting:             &v1alpha1.RolloutTrafficRouting{},
					},
				},
			},
		}
		abortDelay, wasSet := GetAbortScaleDownDelaySecondsOrDefault(canaryNonDefaultValue)
		assert.Equal(t, time.Duration(abortScaleDownDelaySeconds)*time.Second, *abortDelay)
		assert.True(t, wasSet)
	}
	{
		// dont scale down canary
		abortScaleDownDelaySeconds := int32(0)
		canaryZeroValue := &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					Canary: &v1alpha1.CanaryStrategy{
						AbortScaleDownDelaySeconds: &abortScaleDownDelaySeconds,
						TrafficRouting:             &v1alpha1.RolloutTrafficRouting{},
					},
				},
			},
		}
		abortDelay, wasSet := GetAbortScaleDownDelaySecondsOrDefault(canaryZeroValue)
		assert.Nil(t, abortDelay)
		assert.True(t, wasSet)
	}
	{
		rolloutNoStrategyDefaultValue := &v1alpha1.Rollout{}
		abortDelay, wasSet := GetAbortScaleDownDelaySecondsOrDefault(rolloutNoStrategyDefaultValue)
		assert.Equal(t, time.Duration(0), *abortDelay)
		assert.False(t, wasSet)
	}
	{
		canaryDefaultValue := &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					Canary: &v1alpha1.CanaryStrategy{
						TrafficRouting: &v1alpha1.RolloutTrafficRouting{},
					},
				},
			},
		}
		abortDelay, wasSet := GetAbortScaleDownDelaySecondsOrDefault(canaryDefaultValue)
		assert.Equal(t, time.Duration(DefaultAbortScaleDownDelaySeconds)*time.Second, *abortDelay)
		assert.False(t, wasSet)
	}
	{
		// basic canary should not have scaledown delay seconds
		canaryDefaultValue := &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					Canary: &v1alpha1.CanaryStrategy{},
				},
			},
		}
		abortDelay, wasSet := GetAbortScaleDownDelaySecondsOrDefault(canaryDefaultValue)
		assert.Equal(t, time.Duration(0), *abortDelay)
		assert.False(t, wasSet)
	}

}

func TestGetAutoPromotionEnabledOrDefault(t *testing.T) {
	autoPromote := false
	rolloutNonDefaultValue := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{
					AutoPromotionEnabled: &autoPromote,
				},
			},
		},
	}

	assert.Equal(t, autoPromote, GetAutoPromotionEnabledOrDefault(rolloutNonDefaultValue))
	rolloutNoStrategyDefaultValue := &v1alpha1.Rollout{}
	assert.Equal(t, DefaultAutoPromotionEnabled, GetAutoPromotionEnabledOrDefault(rolloutNoStrategyDefaultValue))
	rolloutNoAutoPromotionEnabled := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{},
			},
		},
	}
	assert.Equal(t, DefaultAutoPromotionEnabled, GetAutoPromotionEnabledOrDefault(rolloutNoAutoPromotionEnabled))
}

func TestGetExperimentProgressDeadlineSecondsOrDefault(t *testing.T) {
	seconds := int32(2)
	nonDefaultValue := &v1alpha1.Experiment{
		Spec: v1alpha1.ExperimentSpec{
			ProgressDeadlineSeconds: &seconds,
		},
	}

	assert.Equal(t, seconds, GetExperimentProgressDeadlineSecondsOrDefault(nonDefaultValue))
	defaultValue := &v1alpha1.Experiment{}
	assert.Equal(t, DefaultProgressDeadlineSeconds, GetExperimentProgressDeadlineSecondsOrDefault(defaultValue))
}

func TestGetConsecutiveErrorLimitOrDefault(t *testing.T) {
	errorLimit := intstr.FromInt(2)
	metricNonDefaultValue := &v1alpha1.Metric{
		ConsecutiveErrorLimit: &errorLimit,
	}
	assert.Equal(t, int32(errorLimit.IntValue()), GetConsecutiveErrorLimitOrDefault(metricNonDefaultValue))

	metricDefaultValue := &v1alpha1.Metric{}
	assert.Equal(t, DefaultConsecutiveErrorLimit, GetConsecutiveErrorLimitOrDefault(metricDefaultValue))
}

func TestSetDefaults(t *testing.T) {
	SetVerifyTargetGroup(true)
	assert.True(t, VerifyTargetGroup())
	SetVerifyTargetGroup(false)
	assert.False(t, VerifyTargetGroup())

	SetIstioAPIVersion("v1alpha9")
	assert.Equal(t, "v1alpha9", GetIstioAPIVersion())
	SetIstioAPIVersion(DefaultIstioVersion)
	assert.Equal(t, DefaultIstioVersion, GetIstioAPIVersion())

	SetAmbassadorAPIVersion("v1alpha9")
	assert.Equal(t, "v1alpha9", GetAmbassadorAPIVersion())
	SetAmbassadorAPIVersion(DefaultAmbassadorVersion)
	assert.Equal(t, DefaultAmbassadorVersion, GetAmbassadorAPIVersion())

	SetSMIAPIVersion("v1alpha9")
	assert.Equal(t, "v1alpha9", GetSMIAPIVersion())
	SetSMIAPIVersion(DefaultSMITrafficSplitVersion)
	assert.Equal(t, DefaultSMITrafficSplitVersion, GetSMIAPIVersion())

	SetTargetGroupBindingAPIVersion("v1alpha9")
	assert.Equal(t, "v1alpha9", GetTargetGroupBindingAPIVersion())
	SetTargetGroupBindingAPIVersion(DefaultTargetGroupBindingAPIVersion)
	assert.Equal(t, DefaultTargetGroupBindingAPIVersion, GetTargetGroupBindingAPIVersion())

	assert.Equal(t, DefaultAppMeshCRDVersion, GetAppMeshCRDVersion())
	SetAppMeshCRDVersion("v1beta3")
	assert.Equal(t, "v1beta3", GetAppMeshCRDVersion())
	SetAppMeshCRDVersion(DefaultAmbassadorVersion)

	assert.Equal(t, DefaultMetricCleanupDelay, int32(GetMetricCleanupDelaySeconds().Seconds()))
	SetMetricCleanupDelaySeconds(24)
	assert.Equal(t, time.Duration(24)*time.Second, GetMetricCleanupDelaySeconds())

	assert.Equal(t, DefaultDescribeTagsLimit, GetDescribeTagsLimit())
	SetDescribeTagsLimit(2)
	assert.Equal(t, 2, GetDescribeTagsLimit())
}
