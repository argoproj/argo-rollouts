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
	rolloutNonDefaultValue := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					MaxUnavailable: &maxUnavailable,
				},
			},
		},
	}

	assert.Equal(t, maxUnavailable, *GetMaxUnavailableOrDefault(rolloutNonDefaultValue))
	rolloutDefaultValue := &v1alpha1.Rollout{}
	assert.Equal(t, intstr.FromInt(DefaultMaxUnavailable), *GetMaxUnavailableOrDefault(rolloutDefaultValue))
}

func TestGetStrategyType(t *testing.T) {
	bgRollout := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{},
			},
		},
	}
	assert.Equal(t, "blueGreen", GetStrategyType(bgRollout))

	canaryRollout := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{},
			},
		},
	}
	assert.Equal(t, "canary", GetStrategyType(canaryRollout))

	noStrategyRollout := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{},
		},
	}
	assert.Equal(t, "No Strategy listed", GetStrategyType(noStrategyRollout))
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
	scaleDownDelaySeconds := int32(60)
	rolloutNonDefaultValue := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{
					ScaleDownDelaySeconds: &scaleDownDelaySeconds,
				},
			},
		},
	}

	assert.Equal(t, scaleDownDelaySeconds, GetScaleDownDelaySecondsOrDefault(rolloutNonDefaultValue))
	rolloutNoStrategyDefaultValue := &v1alpha1.Rollout{}
	assert.Equal(t, DefaultScaleDownDelaySeconds, GetScaleDownDelaySecondsOrDefault(rolloutNoStrategyDefaultValue))
	rolloutNoScaleDownDelaySeconds := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{},
			},
		},
	}
	assert.Equal(t, DefaultScaleDownDelaySeconds, GetScaleDownDelaySecondsOrDefault(rolloutNoScaleDownDelaySeconds))
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
	errorLimit := (*int32)(nil)
	metricNonDefaultValue := v1alpha1.Metric{}
	assert.Equal(t, errorLimit, metricNonDefaultValue.ConsecutiveErrorLimit)
	assert.Equal(t, DefaultConsecutiveErrorLimit, GetConsecutiveErrorLimitOrDefault(&metricNonDefaultValue))
}
