package replicaset

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func newRollout(
	specReplicas,
	setWeight int32,
	maxSurge,
	maxUnavailable intstr.IntOrString,
	currentPodHash,
	stablePodHash string,
	scs *v1alpha1.SetCanaryScale,
	rtr *v1alpha1.RolloutTrafficRouting,
) *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Replicas: &specReplicas,
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					MaxUnavailable: &maxUnavailable,
					MaxSurge:       &maxSurge,
					Steps: []v1alpha1.CanaryStep{{
						SetWeight:      &setWeight,
						SetCanaryScale: scs,
					}},
					TrafficRouting: rtr,
				},
			},
		},
		Status: v1alpha1.RolloutStatus{
			StableRS:       stablePodHash,
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

func newSetCanaryScale(replicas, weight *int32, matchTrafficWeight bool) *v1alpha1.SetCanaryScale {
	scs := v1alpha1.SetCanaryScale{}
	scs.Replicas = replicas
	scs.Weight = weight
	scs.MatchTrafficWeight = matchTrafficWeight
	return &scs
}

func TestCalculateReplicaCountsForCanary(t *testing.T) {
	intPnt := func(i int32) *int32 {
		return &i
	}
	tests := []struct {
		name string

		rolloutSpecReplicas int32
		setWeight           int32
		maxSurge            intstr.IntOrString
		maxUnavailable      intstr.IntOrString
		promoteFull         bool

		stableSpecReplica      int32
		stableAvailableReplica int32

		canarySpecReplica      int32
		canaryAvailableReplica int32

		expectedStableReplicaCount int32
		expectedCanaryReplicaCount int32

		olderRS        *appsv1.ReplicaSet
		setCanaryScale *v1alpha1.SetCanaryScale
		trafficRouting *v1alpha1.RolloutTrafficRouting
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
		{
			name: "For canary replicas, use setCanaryScale.replicas when specified along with trafficRouting (and ignore setWeight)",

			rolloutSpecReplicas:    1,
			stableSpecReplica:      1,
			stableAvailableReplica: 1,

			expectedStableReplicaCount: 1,
			expectedCanaryReplicaCount: 9,
			setWeight:                  100,
			setCanaryScale:             newSetCanaryScale(intPnt(9), nil, false),
			trafficRouting:             &v1alpha1.RolloutTrafficRouting{},
		},
		{
			name: "Use setCanaryScale.weight for canary replicas when specified with trafficRouting (and ignore setWeight)",

			rolloutSpecReplicas:    4,
			stableSpecReplica:      4,
			stableAvailableReplica: 4,

			expectedStableReplicaCount: 4,
			expectedCanaryReplicaCount: 8,
			setWeight:                  100,
			setCanaryScale:             newSetCanaryScale(nil, intPnt(200), false),
			trafficRouting:             &v1alpha1.RolloutTrafficRouting{},
		},
		{
			name:                "Ignore setCanaryScale replicas/weight when matchTrafficWeight is true",
			rolloutSpecReplicas: 10,
			setWeight:           20,
			maxSurge:            intstr.FromString("20%"),
			maxUnavailable:      intstr.FromInt(0),

			stableSpecReplica:      10,
			stableAvailableReplica: 10,

			canarySpecReplica:      0,
			canaryAvailableReplica: 0,

			setCanaryScale: newSetCanaryScale(intPnt(9), intPnt(100), true),
			trafficRouting: &v1alpha1.RolloutTrafficRouting{},

			expectedStableReplicaCount: 10,
			expectedCanaryReplicaCount: 2,
		},
		{
			name:                "Ignore setCanaryScale when trafficRouting is missing and use setWeight for replicas",
			rolloutSpecReplicas: 10,
			setWeight:           20,
			maxSurge:            intstr.FromString("20%"),
			maxUnavailable:      intstr.FromInt(0),

			stableSpecReplica:      10,
			stableAvailableReplica: 10,

			canarySpecReplica:      0,
			canaryAvailableReplica: 0,

			setCanaryScale: newSetCanaryScale(intPnt(9), intPnt(100), false),
			trafficRouting: nil,

			expectedStableReplicaCount: 10,
			expectedCanaryReplicaCount: 2,
		},
		{
			// verify we surge canary up correctly when stable RS is not available
			name:                "honor maxSurge during scale up when stableRS unavailable",
			rolloutSpecReplicas: 4,
			setWeight:           100,
			maxSurge:            intstr.FromInt(1),
			maxUnavailable:      intstr.FromInt(0),

			stableSpecReplica:      4,
			stableAvailableReplica: 1,

			canarySpecReplica:      0,
			canaryAvailableReplica: 0,

			expectedStableReplicaCount: 4,
			expectedCanaryReplicaCount: 1, // should only surge by 1 to honor maxSurge: 1
		},
		{
			// verify we scale down stableRS while honoring maxUnavailable even when stableRS unavailable
			name:                "honor maxUnavailable during scale down stableRS unavailable",
			rolloutSpecReplicas: 4,
			setWeight:           100,
			maxSurge:            intstr.FromInt(1),
			maxUnavailable:      intstr.FromInt(0),

			stableSpecReplica:      4,
			stableAvailableReplica: 1,

			canarySpecReplica:      1,
			canaryAvailableReplica: 1,

			expectedStableReplicaCount: 3, // should only scale down by 1 to honor maxUnavailable: 0
			expectedCanaryReplicaCount: 1,
		},
		{
			// verify we honor maxUnavailable when aborting or reducing weight
			name:                "honor maxUnavailable when aborting or reducing weight part 1",
			rolloutSpecReplicas: 4,
			setWeight:           0,
			maxSurge:            intstr.FromInt(1),
			maxUnavailable:      intstr.FromInt(0),

			stableSpecReplica:      0,
			stableAvailableReplica: 0,

			canarySpecReplica:      4,
			canaryAvailableReplica: 4,

			expectedStableReplicaCount: 1,
			expectedCanaryReplicaCount: 4, // should not bring down canary until we surge stable
		},
		{
			// verify we honor maxUnavailable when aborting or reducing weight (after surging stable)
			name:                "honor maxUnavailable when aborting or reducing weight part 2",
			rolloutSpecReplicas: 4,
			setWeight:           0,
			maxSurge:            intstr.FromInt(1),
			maxUnavailable:      intstr.FromInt(0),

			stableSpecReplica:      1,
			stableAvailableReplica: 0,

			canarySpecReplica:      4,
			canaryAvailableReplica: 4,

			expectedStableReplicaCount: 1, // should not adjust counts at all since stable not available
			expectedCanaryReplicaCount: 4,
		},
		{
			// verify we honor maxUnavailable when aborting or reducing weight (after stable surge availability)
			name:                "honor maxUnavailable when aborting or reducing weight part 3",
			rolloutSpecReplicas: 4,
			setWeight:           0,
			maxSurge:            intstr.FromInt(1),
			maxUnavailable:      intstr.FromInt(0),

			stableSpecReplica:      1,
			stableAvailableReplica: 1,

			canarySpecReplica:      4,
			canaryAvailableReplica: 4,

			expectedStableReplicaCount: 1,
			expectedCanaryReplicaCount: 3, // should only reduce by 1 to honor maxUnavailable
		},
		{
			// verify when promoting a rollout fully, we don't consider canary weight
			name:                "promote full does not consider weight",
			promoteFull:         true,
			rolloutSpecReplicas: 4,
			setWeight:           1,
			maxSurge:            intstr.FromInt(4),
			maxUnavailable:      intstr.FromInt(4),

			stableSpecReplica:      4,
			stableAvailableReplica: 4,

			canarySpecReplica:      0,
			canaryAvailableReplica: 0,

			expectedStableReplicaCount: 0,
			expectedCanaryReplicaCount: 4,
		},
		{
			// verify when promoting a rollout fully, we still honor maxSurge and maxUnavailable
			name:                "promote full still honors maxSurge/maxUnavailable",
			promoteFull:         true,
			rolloutSpecReplicas: 4,
			setWeight:           1,
			maxSurge:            intstr.FromInt(3),
			maxUnavailable:      intstr.FromInt(0),

			stableSpecReplica:      4,
			stableAvailableReplica: 4,

			canarySpecReplica:      0,
			canaryAvailableReplica: 0,

			expectedStableReplicaCount: 4,
			expectedCanaryReplicaCount: 3,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			rollout := newRollout(test.rolloutSpecReplicas, test.setWeight, test.maxSurge, test.maxUnavailable, "canary", "stable", test.setCanaryScale, test.trafficRouting)
			rollout.Status.PromoteFull = test.promoteFull
			stableRS := newRS("stable", test.stableSpecReplica, test.stableAvailableReplica)
			canaryRS := newRS("canary", test.canarySpecReplica, test.canaryAvailableReplica)
			newRSReplicaCount, stableRSReplicaCount := CalculateReplicaCountsForCanary(rollout, canaryRS, stableRS, []*appsv1.ReplicaSet{test.olderRS})
			assert.Equal(t, test.expectedCanaryReplicaCount, newRSReplicaCount, "check canary replica count")
			assert.Equal(t, test.expectedStableReplicaCount, stableRSReplicaCount, "check stable replica count")
		})
	}
}

func TestCalculateReplicaCountsForCanaryTrafficRouting(t *testing.T) {
	rollout := newRollout(10, 10, intstr.FromInt(0), intstr.FromInt(1), "canary", "stable", nil, nil)
	rollout.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
	stableRS := newRS("stable", 10, 10)
	newRS := newRS("canary", 0, 0)
	newRSReplicaCount, stableRSReplicaCount := CalculateReplicaCountsForCanary(rollout, newRS, stableRS, nil)
	assert.Equal(t, int32(1), newRSReplicaCount)
	assert.Equal(t, int32(10), stableRSReplicaCount)
}

func TestCalculateReplicaCountsForCanaryStableRSdEdgeCases(t *testing.T) {
	rollout := newRollout(10, 10, intstr.FromInt(0), intstr.FromInt(1), "", "", nil, nil)
	newRS := newRS("stable", 9, 9)
	newRSReplicaCount, stableRSReplicaCount := CalculateReplicaCountsForCanary(rollout, newRS, nil, []*appsv1.ReplicaSet{})
	assert.Equal(t, int32(10), newRSReplicaCount)
	assert.Equal(t, int32(0), stableRSReplicaCount)

	newRSReplicaCount, stableRSReplicaCount = CalculateReplicaCountsForCanary(rollout, newRS, newRS, []*appsv1.ReplicaSet{})
	assert.Equal(t, int32(10), newRSReplicaCount)
	assert.Equal(t, int32(0), stableRSReplicaCount)
}

func TestGetOtherRSs(t *testing.T) {
	rs := func(podHash string) appsv1.ReplicaSet {
		return appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: podHash,
			},
		}
	}
	rollout := &v1alpha1.Rollout{}
	rs1 := rs("1")
	rs2 := rs("2")
	handleNil := GetOtherRSs(rollout, nil, nil, []*appsv1.ReplicaSet{&rs1})
	assert.Len(t, handleNil, 1)
	assert.Equal(t, *handleNil[0], rs1)

	handleExistingNewRS := GetOtherRSs(rollout, &rs1, nil, []*appsv1.ReplicaSet{&rs1, &rs2})
	assert.Len(t, handleExistingNewRS, 1)
	assert.Equal(t, *handleExistingNewRS[0], rs2)

	handleExistingStableRS := GetOtherRSs(rollout, nil, &rs1, []*appsv1.ReplicaSet{&rs1, &rs2})
	assert.Len(t, handleExistingStableRS, 1)
	assert.Equal(t, *handleExistingStableRS[0], rs2)

}

func TestGetStableRS(t *testing.T) {
	rs := func(podHash string) appsv1.ReplicaSet {
		return appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: podHash,
				Labels: map[string]string{
					v1alpha1.DefaultRolloutUniqueLabelKey: podHash,
				},
			},
		}
	}

	rollout := &v1alpha1.Rollout{}
	rs1 := rs("1")
	rs2 := rs("2")
	rs3 := rs("3")
	noStable := GetStableRS(rollout, &rs1, []*appsv1.ReplicaSet{&rs2, &rs3})
	assert.Nil(t, noStable)

	rollout.Status.StableRS = "1"
	stableNotFound := GetStableRS(rollout, &rs2, []*appsv1.ReplicaSet{&rs3})
	assert.Nil(t, stableNotFound)

	sameAsNewRS := GetStableRS(rollout, &rs1, []*appsv1.ReplicaSet{&rs2, &rs3})
	assert.Equal(t, *sameAsNewRS, rs1)

	stableInOtherRSs := GetStableRS(rollout, &rs2, []*appsv1.ReplicaSet{&rs1, &rs2, &rs3})
	assert.Equal(t, *stableInOtherRSs, rs1)
}

func TestBeforeStartingStep(t *testing.T) {
	ro := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					Analysis: &v1alpha1.RolloutAnalysisBackground{},
				},
			},
		},
	}
	assert.False(t, BeforeStartingStep(ro))

	ro.Spec.Strategy.Canary.Analysis.StartingStep = pointer.Int32Ptr(1)
	assert.False(t, BeforeStartingStep(ro))
	ro.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{
		{
			SetWeight: pointer.Int32Ptr(1),
		},
		{
			Pause: &v1alpha1.RolloutPause{},
		},
	}
	ro.Status.CurrentStepIndex = pointer.Int32Ptr(0)
	assert.True(t, BeforeStartingStep(ro))
	ro.Status.CurrentStepIndex = pointer.Int32Ptr(1)
	assert.False(t, BeforeStartingStep(ro))

}

func TestGetCurrentCanaryStep(t *testing.T) {
	rollout := newRollout(10, 10, intstr.FromInt(0), intstr.FromInt(1), "", "", nil, nil)
	rollout.Spec.Strategy.Canary.Steps = nil
	noCurrentSteps, _ := GetCurrentCanaryStep(rollout)
	assert.Nil(t, noCurrentSteps)

	rollout.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
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
	rollout := newRollout(10, 10, intstr.FromInt(0), intstr.FromInt(1), "", "", nil, nil)
	rollout.Status.CurrentStepIndex = &stepIndex

	setWeight := GetCurrentSetWeight(rollout)
	assert.Equal(t, setWeight, int32(100))

	stepIndex = 0
	setWeight = GetCurrentSetWeight(rollout)
	assert.Equal(t, setWeight, int32(10))

	rollout.Status.Abort = true
	setWeight = GetCurrentSetWeight(rollout)
	assert.Equal(t, setWeight, int32(0))

}

func TestGetCurrentExperiment(t *testing.T) {
	rollout := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					Steps: []v1alpha1.CanaryStep{
						{
							Experiment: &v1alpha1.RolloutExperimentStep{
								Duration: "1s",
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
	assert.Equal(t, v1alpha1.DurationString("1s"), e.Duration)

	rollout.Status.CurrentStepIndex = pointer.Int32Ptr(1)

	e = GetCurrentExperimentStep(rollout)
	assert.Equal(t, v1alpha1.DurationString("1s"), e.Duration)

	rollout.Status.CurrentStepIndex = pointer.Int32Ptr(2)

	assert.Nil(t, GetCurrentExperimentStep(rollout))

	rollout.Spec.Strategy.Canary.Steps[0] = v1alpha1.CanaryStep{SetWeight: pointer.Int32Ptr(10)}
	rollout.Status.CurrentStepIndex = pointer.Int32Ptr(1)

	assert.Nil(t, GetCurrentExperimentStep(rollout))

}

func TestSyncReplicaSetEphemeralPodMetadata(t *testing.T) {
	rs := appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: corev1.NamespaceDefault,
			Annotations: map[string]string{
				EphemeralMetadataAnnotation: `{"labels":{"aaa":"111","bbb":"222"},"annotations":{"ccc":"333","ddd":"444"}}`,
			},
		},
		Spec: appsv1.ReplicaSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"aaa": "111",
						"bbb": "222",
					},
					Annotations: map[string]string{
						"ccc": "333",
						"ddd": "444",
					},
				},
			},
		},
	}
	{
		// verify applying the same pod-metadata is a noop
		oldPodMetadata := v1alpha1.PodTemplateMetadata{
			Labels: map[string]string{
				"aaa": "111",
				"bbb": "222",
			},
			Annotations: map[string]string{
				"ccc": "333",
				"ddd": "444",
			},
		}
		newRS, modified := SyncReplicaSetEphemeralPodMetadata(&rs, &oldPodMetadata)
		assert.False(t, modified)
		assert.Equal(t, rs.Annotations, newRS.Annotations)
		assert.Equal(t, rs.Spec.Template.Labels, newRS.Spec.Template.Labels)
		assert.Equal(t, rs.Spec.Template.Annotations, newRS.Spec.Template.Annotations)
	}
	{
		// verify applying new new update works
		newPodMetadata := v1alpha1.PodTemplateMetadata{
			Labels: map[string]string{
				"aaa": "111",
			},
			Annotations: map[string]string{
				"ccc": "333",
			},
		}
		newRS, modified := SyncReplicaSetEphemeralPodMetadata(&rs, &newPodMetadata)
		assert.True(t, modified)
		assert.Equal(t, newPodMetadata.Labels, newRS.Spec.Template.Labels)
		assert.Equal(t, newPodMetadata.Annotations, newRS.Spec.Template.Annotations)
		assert.Equal(t, newRS.Annotations[EphemeralMetadataAnnotation], `{"labels":{"aaa":"111"},"annotations":{"ccc":"333"}}`)
	}
	{
		// verify we can remove metadata
		newRS, modified := SyncReplicaSetEphemeralPodMetadata(&rs, nil)
		assert.True(t, modified)
		assert.Empty(t, newRS.Spec.Template.Labels)
		assert.Empty(t, newRS.Spec.Template.Annotations)
		assert.Empty(t, newRS.Annotations[EphemeralMetadataAnnotation])
	}
}

func TestSyncEphemeralPodMetadata(t *testing.T) {
	meta := metav1.ObjectMeta{
		Labels: map[string]string{
			"aaa":    "111",
			"do-not": "touch",
			"bbb":    "222",
		},
		Annotations: map[string]string{
			"ccc":    "333",
			"do-not": "touch",
			"ddd":    "444",
		},
	}
	existing := v1alpha1.PodTemplateMetadata{
		Labels: map[string]string{
			"aaa": "111",
			"bbb": "222",
		},
		Annotations: map[string]string{
			"ccc": "333",
			"ddd": "444",
		},
	}
	{
		// verify modified is false if there are no changes
		newMetadata, modified := SyncEphemeralPodMetadata(&meta, &existing, &existing)
		assert.False(t, modified)
		assert.Equal(t, meta, *newMetadata)
	}
	{
		// verify we don't touch metadata that we did not inject ourselves
		desired := v1alpha1.PodTemplateMetadata{
			Labels: map[string]string{
				"aaa": "222",
			},
			Annotations: map[string]string{
				"ccc": "444",
			},
		}
		newMetadata, modified := SyncEphemeralPodMetadata(&meta, &existing, &desired)
		assert.True(t, modified)
		expected := metav1.ObjectMeta{
			Labels: map[string]string{
				"aaa":    "222",
				"do-not": "touch",
			},
			Annotations: map[string]string{
				"ccc":    "444",
				"do-not": "touch",
			},
		}
		assert.True(t, modified)
		assert.Equal(t, expected, *newMetadata)
	}

}
