package defaults

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestGetRolloutReplicasOrDefault(t *testing.T) {
	replicas := int32(2)
	rolloutNonDefaultValue := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Replicas: &replicas,
		},
	}

	assert.Equal(t, replicas, GetRolloutReplicasOrDefault(rolloutNonDefaultValue))
	rolloutDefaultValue := &v1alpha1.Rollout{}
	assert.Equal(t, DefaultReplicas, GetRolloutReplicasOrDefault(rolloutDefaultValue))
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
				CanaryStrategy: &v1alpha1.CanaryStrategy{
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
				CanaryStrategy: &v1alpha1.CanaryStrategy{
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
				BlueGreenStrategy: &v1alpha1.BlueGreenStrategy{},
			},
		},
	}
	assert.Equal(t, "blueGreen", GetStrategyType(bgRollout))

	canaryRollout := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				CanaryStrategy: &v1alpha1.CanaryStrategy{},
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
				BlueGreenStrategy: &v1alpha1.BlueGreenStrategy{
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
				BlueGreenStrategy: &v1alpha1.BlueGreenStrategy{},
			},
		},
	}
	assert.Equal(t, DefaultScaleDownDelaySeconds, GetScaleDownDelaySecondsOrDefault(rolloutNoScaleDownDelaySeconds))
}
