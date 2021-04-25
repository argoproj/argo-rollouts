package defaults

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestGetReplicasOrDefault(t *testing.T) {
	replicas := int32(2)
	assert.Equal(t, replicas, GetReplicasOrDefault(&replicas))
	assert.Equal(t, DefaultReplicas, GetReplicasOrDefault(nil))
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
		assert.Equal(t, scaleDownDelaySeconds, GetScaleDownDelaySecondsOrDefault(blueGreenNonDefaultValue))
	}
	{
		rolloutNoStrategyDefaultValue := &v1alpha1.Rollout{}
		assert.Equal(t, int32(0), GetScaleDownDelaySecondsOrDefault(rolloutNoStrategyDefaultValue))
	}
	{
		rolloutNoScaleDownDelaySeconds := &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					BlueGreen: &v1alpha1.BlueGreenStrategy{},
				},
			},
		}
		assert.Equal(t, DefaultScaleDownDelaySeconds, GetScaleDownDelaySecondsOrDefault(rolloutNoScaleDownDelaySeconds))
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
		assert.Equal(t, int32(0), GetScaleDownDelaySecondsOrDefault(canaryNoTrafficRouting))
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
		assert.Equal(t, scaleDownDelaySeconds, GetScaleDownDelaySecondsOrDefault(canaryWithTrafficRouting))
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
