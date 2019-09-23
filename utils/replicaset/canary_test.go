package replicaset

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func newRollout(specReplicas, setWeight int32, maxSurge, maxUnavailable intstr.IntOrString, currentPodHash, stablePodHash string) *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Replicas: &specReplicas,
			Strategy: v1alpha1.RolloutStrategy{
				CanaryStrategy: &v1alpha1.CanaryStrategy{
					MaxUnavailable: &maxUnavailable,
					MaxSurge:       &maxSurge,
					Steps: []v1alpha1.CanaryStep{{
						SetWeight: &setWeight,
					}},
				},
			},
		},
		Status: v1alpha1.RolloutStatus{
			Canary: v1alpha1.CanaryStatus{
				StableRS: stablePodHash,
			},
			CurrentPodHash: currentPodHash,
		},
	}
}

func newRS(podHashLabel string, specReplicas, availableReplicas int32) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: podHashLabel},
			Name:   podHashLabel,
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: &specReplicas,
		},
		Status: appsv1.ReplicaSetStatus{
			AvailableReplicas: availableReplicas,
		},
	}
}

func TestCalculateReplicaCountsForCanary(t *testing.T) {
	tests := []struct {
		name string

		rolloutSpecReplicas int32
		setWeight           int32
		maxSurge            intstr.IntOrString
		maxUnavailable      intstr.IntOrString

		stableSpecReplica      int32
		stableAvailableReplica int32

		canarySpecReplica      int32
		canaryAvailableReplica int32

		expectedStableReplicaCount int32
		expectedCanaryReplicaCount int32

		olderRS *appsv1.ReplicaSet
	}{
		{
			name:                "Do not add extra RSs in scaleDownCount when .Spec.Replica < AvailableReplicas",
			rolloutSpecReplicas: 10,
			setWeight:           100,
			maxSurge:            intstr.FromInt(3),
			maxUnavailable:      intstr.FromInt(0),

			stableSpecReplica:      7,
			stableAvailableReplica: 8,

			canarySpecReplica:      6,
			canaryAvailableReplica: 3,

			expectedStableReplicaCount: 7,
			expectedCanaryReplicaCount: 6,
		},
		{
			name:                "Use max surge int to scale up canary",
			rolloutSpecReplicas: 10,
			setWeight:           20,
			maxSurge:            intstr.FromInt(2),
			maxUnavailable:      intstr.FromInt(0),

			stableSpecReplica:      10,
			stableAvailableReplica: 10,

			canarySpecReplica:      0,
			canaryAvailableReplica: 0,

			expectedStableReplicaCount: 10,
			expectedCanaryReplicaCount: 2,
		},
		{
			name:                "Use max surge percentage to scale up canary",
			rolloutSpecReplicas: 10,
			setWeight:           20,
			maxSurge:            intstr.FromString("20%"),
			maxUnavailable:      intstr.FromInt(0),

			stableSpecReplica:      10,
			stableAvailableReplica: 10,

			canarySpecReplica:      0,
			canaryAvailableReplica: 0,

			expectedStableReplicaCount: 10,
			expectedCanaryReplicaCount: 2,
		},
		{
			name:                "Scale down extra stable replicas",
			rolloutSpecReplicas: 10,
			setWeight:           20,
			maxSurge:            intstr.FromString("20%"),
			maxUnavailable:      intstr.FromInt(0),

			stableSpecReplica:      10,
			stableAvailableReplica: 10,

			canarySpecReplica:      2,
			canaryAvailableReplica: 2,

			expectedStableReplicaCount: 8,
			expectedCanaryReplicaCount: 2,
		},
		{
			name:                "Do not go past max surge",
			rolloutSpecReplicas: 10,
			setWeight:           30,
			maxSurge:            intstr.FromInt(2),
			maxUnavailable:      intstr.FromInt(0),

			stableSpecReplica:      10,
			stableAvailableReplica: 10,

			canarySpecReplica:      0,
			canaryAvailableReplica: 0,

			expectedStableReplicaCount: 10,
			expectedCanaryReplicaCount: 2,
		},
		{
			name:                "Use max unavailable int to scale down stableRS",
			rolloutSpecReplicas: 10,
			setWeight:           20,
			maxSurge:            intstr.FromInt(0),
			maxUnavailable:      intstr.FromInt(2),

			stableSpecReplica:      10,
			stableAvailableReplica: 10,

			canarySpecReplica:      0,
			canaryAvailableReplica: 0,

			expectedStableReplicaCount: 8,
			expectedCanaryReplicaCount: 0,
		},
		{
			name:                "Use max surge percentage to scale down stableRS",
			rolloutSpecReplicas: 10,
			setWeight:           20,
			maxSurge:            intstr.FromInt(0),
			maxUnavailable:      intstr.FromString("20%"),

			stableSpecReplica:      10,
			stableAvailableReplica: 10,

			canarySpecReplica:      0,
			canaryAvailableReplica: 0,

			expectedStableReplicaCount: 8,
			expectedCanaryReplicaCount: 0,
		},
		{
			name:                "Do not go past max unavailable",
			rolloutSpecReplicas: 10,
			setWeight:           30,
			maxSurge:            intstr.FromInt(0),
			maxUnavailable:      intstr.FromInt(2),

			stableSpecReplica:      10,
			stableAvailableReplica: 10,

			canarySpecReplica:      0,
			canaryAvailableReplica: 0,

			expectedStableReplicaCount: 8,
			expectedCanaryReplicaCount: 0,
		},
		{
			name:                "Use Max Surge and Max Unavailable",
			rolloutSpecReplicas: 10,
			setWeight:           50,
			maxSurge:            intstr.FromInt(2),
			maxUnavailable:      intstr.FromInt(1),

			stableSpecReplica:      10,
			stableAvailableReplica: 10,

			canarySpecReplica:      0,
			canaryAvailableReplica: 0,

			expectedStableReplicaCount: 9,
			expectedCanaryReplicaCount: 2,
		},
		{
			name:                "Scale canaryRS to zero on setWeight of 0%",
			rolloutSpecReplicas: 10,
			setWeight:           0,
			maxSurge:            intstr.FromInt(1),
			maxUnavailable:      intstr.FromInt(1),

			stableSpecReplica:      10,
			stableAvailableReplica: 10,

			canarySpecReplica:      1,
			canaryAvailableReplica: 1,

			expectedStableReplicaCount: 10,
			expectedCanaryReplicaCount: 0,
		},
		{
			name:                "Scale stable to zero on setWeight of 100%",
			rolloutSpecReplicas: 10,
			setWeight:           100,
			maxSurge:            intstr.FromInt(1),
			maxUnavailable:      intstr.FromInt(1),

			stableSpecReplica:      1,
			stableAvailableReplica: 1,

			canarySpecReplica:      10,
			canaryAvailableReplica: 10,

			expectedStableReplicaCount: 0,
			expectedCanaryReplicaCount: 10,
		},
		{
			name:                "Do not scale newRS down to zero on non-zero weight",
			rolloutSpecReplicas: 1,
			setWeight:           20,
			maxSurge:            intstr.FromInt(1),
			maxUnavailable:      intstr.FromInt(0),

			stableSpecReplica:      1,
			stableAvailableReplica: 1,

			canarySpecReplica:      0,
			canaryAvailableReplica: 0,

			expectedStableReplicaCount: 1,
			expectedCanaryReplicaCount: 1,
		},
		{
			name:                "Do not scale canaryRS down to zero on non-100 weight",
			rolloutSpecReplicas: 1,
			setWeight:           90,
			maxSurge:            intstr.FromInt(1),
			maxUnavailable:      intstr.FromInt(0),

			stableSpecReplica:      1,
			stableAvailableReplica: 1,

			canarySpecReplica:      0,
			canaryAvailableReplica: 0,

			expectedStableReplicaCount: 1,
			expectedCanaryReplicaCount: 1,
		},
		{
			name:                "Scale up Stable before newRS",
			rolloutSpecReplicas: 10,
			setWeight:           30,
			maxSurge:            intstr.FromInt(1),
			maxUnavailable:      intstr.FromInt(0),

			stableSpecReplica:      1,
			stableAvailableReplica: 1,

			canarySpecReplica:      0,
			canaryAvailableReplica: 0,

			expectedStableReplicaCount: 7,
			expectedCanaryReplicaCount: 1,

			olderRS: newRS("older", 3, 3),
		},
		{
			name:                "Scale down newRS and stable",
			rolloutSpecReplicas: 10,
			setWeight:           30,
			maxSurge:            intstr.FromInt(0),
			maxUnavailable:      intstr.FromInt(3),

			stableSpecReplica:      8,
			stableAvailableReplica: 5,

			canarySpecReplica:      4,
			canaryAvailableReplica: 4,

			expectedStableReplicaCount: 7,
			expectedCanaryReplicaCount: 3,
		},
		{
			name:                "Do not scale down newRS or stable when older RS count >= scaleDownCount",
			rolloutSpecReplicas: 10,
			setWeight:           30,
			maxSurge:            intstr.FromInt(0),
			maxUnavailable:      intstr.FromInt(1),

			stableSpecReplica:      8,
			stableAvailableReplica: 6,

			canarySpecReplica:      4,
			canaryAvailableReplica: 2,

			expectedStableReplicaCount: 8,
			expectedCanaryReplicaCount: 4,

			olderRS: newRS("older", 3, 3),
		},
		{
			name:                "Add an extra replica to surge when the setWeight rounding adds another instance",
			rolloutSpecReplicas: 10,
			setWeight:           5,
			maxSurge:            intstr.FromInt(0),
			maxUnavailable:      intstr.FromInt(1),

			stableSpecReplica:      10,
			stableAvailableReplica: 10,

			canarySpecReplica:      0,
			canaryAvailableReplica: 0,

			expectedStableReplicaCount: 10,
			expectedCanaryReplicaCount: 1,
		},
		{
			name:                "Use maxUnavailable of 1 when percentage of maxUnavailable and maxSurge result in 0 replicas",
			rolloutSpecReplicas: 10,
			setWeight:           10,
			maxSurge:            intstr.FromString("0%"),
			maxUnavailable:      intstr.FromString("0%"),

			stableSpecReplica:      10,
			stableAvailableReplica: 10,

			canarySpecReplica:      0,
			canaryAvailableReplica: 0,

			expectedStableReplicaCount: 9,
			expectedCanaryReplicaCount: 0,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			rollout := newRollout(test.rolloutSpecReplicas, test.setWeight, test.maxSurge, test.maxUnavailable, "canary", "stable")
			stableRS := newRS("stable", test.stableSpecReplica, test.stableAvailableReplica)
			canaryRS := newRS("canary", test.canarySpecReplica, test.canaryAvailableReplica)
			newRSReplicaCount, stableRSReplicaCount := CalculateReplicaCountsForCanary(rollout, canaryRS, stableRS, []*appsv1.ReplicaSet{test.olderRS})
			assert.Equal(t, test.expectedCanaryReplicaCount, newRSReplicaCount)
			assert.Equal(t, test.expectedStableReplicaCount, stableRSReplicaCount)
		})
	}
}

func TestCalculateReplicaCountsForCanaryStableRSdEdgeCases(t *testing.T) {
	rollout := newRollout(10, 10, intstr.FromInt(0), intstr.FromInt(1), "", "")
	newRS := newRS("stable", 9, 9)
	newRSReplicaCount, stableRSReplicaCount := CalculateReplicaCountsForCanary(rollout, newRS, nil, []*appsv1.ReplicaSet{})
	assert.Equal(t, int32(10), newRSReplicaCount)
	assert.Equal(t, int32(0), stableRSReplicaCount)

	newRSReplicaCount, stableRSReplicaCount = CalculateReplicaCountsForCanary(rollout, newRS, newRS, []*appsv1.ReplicaSet{})
	assert.Equal(t, int32(10), newRSReplicaCount)
	assert.Equal(t, int32(0), stableRSReplicaCount)
}

func TestGetCurrentCanaryStep(t *testing.T) {
	rollout := newRollout(10, 10, intstr.FromInt(0), intstr.FromInt(1), "", "")
	rollout.Spec.Strategy.CanaryStrategy.Steps = nil
	noCurrentSteps, _ := GetCurrentCanaryStep(rollout)
	assert.Nil(t, noCurrentSteps)

	rollout.Spec.Strategy.CanaryStrategy.Steps = []v1alpha1.CanaryStep{{
		Pause: &v1alpha1.RolloutPause{},
	}}
	rollout.Status.CurrentStepIndex = func(i int32) *int32 { return &i }(0)

	currentStep, index := GetCurrentCanaryStep(rollout)
	assert.NotNil(t, currentStep)
	assert.Equal(t, int32(0), *index)

	rollout.Status.CurrentStepIndex = func(i int32) *int32 { return &i }(1)
	noMoreStep, _ := GetCurrentCanaryStep(rollout)
	assert.Nil(t, noMoreStep)
}

func TestGetCurrentSetWeight(t *testing.T) {
	stepIndex := int32(1)
	rollout := newRollout(10, 10, intstr.FromInt(0), intstr.FromInt(1), "", "")
	rollout.Status.CurrentStepIndex = &stepIndex

	setWeight := GetCurrentSetWeight(rollout)
	assert.Equal(t, setWeight, int32(100))

	stepIndex = 0
	setWeight = GetCurrentSetWeight(rollout)
	assert.Equal(t, setWeight, int32(10))

}

func TestGetCurrentExperiment(t *testing.T) {
	rollout := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				CanaryStrategy: &v1alpha1.CanaryStrategy{
					Steps: []v1alpha1.CanaryStep{
						{
							Experiment: &v1alpha1.RolloutCanaryExperimentStep{
								Duration: int32(1),
							},
						}, {
							Pause: &v1alpha1.RolloutPause{},
						},
					},
				},
			},
		},
	}
	rollout.Status.CurrentStepIndex = pointer.Int32Ptr(0)

	e := GetCurrentExperimentStep(rollout)
	assert.Equal(t, int32(1), e.Duration)

	rollout.Status.CurrentStepIndex = pointer.Int32Ptr(1)

	e = GetCurrentExperimentStep(rollout)
	assert.Equal(t, int32(1), e.Duration)

	rollout.Status.CurrentStepIndex = pointer.Int32Ptr(2)

	assert.Nil(t, GetCurrentExperimentStep(rollout))

	rollout.Spec.Strategy.CanaryStrategy.Steps[0] = v1alpha1.CanaryStep{SetWeight: pointer.Int32Ptr(10)}
	rollout.Status.CurrentStepIndex = pointer.Int32Ptr(1)

	assert.Nil(t, GetCurrentExperimentStep(rollout))

}
