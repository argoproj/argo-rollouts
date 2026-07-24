package info

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info/testdata"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

func TestAge(t *testing.T) {
	m := metav1.ObjectMeta{
		CreationTimestamp: metav1.NewTime(time.Now().Add(-7 * time.Hour * time.Duration(24))),
	}
	assert.Equal(t, "7d", Age(m))
}

func TestCanaryRolloutInfo(t *testing.T) {
	rolloutObjs := testdata.NewCanaryRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns, nil)
	assert.Equal(t, roInfo.ObjectMeta.Name, rolloutObjs.Rollouts[0].Name)
	assert.Len(t, Revisions(roInfo), 3)

	assert.Equal(t, Images(roInfo), []ImageInfo{
		{
			Image: "argoproj/rollouts-demo:does-not-exist",
			Tags:  []string{InfoTagCanary},
		},
		{
			Image: "argoproj/rollouts-demo:green",
			Tags:  []string{InfoTagStable},
		},
	})
}

func TestCanaryRolloutInfoWeights(t *testing.T) {
	rolloutObjs := testdata.NewCanaryRollout()

	t.Run("TestActualWeightWithExistingWeight", func(t *testing.T) {
		t.Run("will test that actual weight for info object is set from rollout status", func(t *testing.T) {
			roInfo := NewRolloutInfo(rolloutObjs.Rollouts[4], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns, nil)
			actualWeightString := roInfo.ActualWeight
			actualWeightStringInt32, err := strconv.ParseInt(actualWeightString, 10, 32)
			if err != nil {
				t.Error(err)
			}
			assert.Equal(t, rolloutObjs.Rollouts[4].Status.Canary.Weights.Canary.Weight, int32(actualWeightStringInt32))
		})
	})

	t.Run("TestActualWeightWithoutExistingWeight", func(t *testing.T) {
		t.Run("will test that actual weight is set to SetWeight when status field does not exist", func(t *testing.T) {
			//This test has a no canary weight object in the status field so we fall back to using SetWeight value
			roInfo := NewRolloutInfo(rolloutObjs.Rollouts[5], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns, nil)
			assert.Equal(t, roInfo.SetWeight, roInfo.ActualWeight)
		})
	})
}

func TestPingPongCanaryRolloutInfo(t *testing.T) {
	rolloutObjs := testdata.NewCanaryRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[3], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns, nil)
	assert.Equal(t, roInfo.ObjectMeta.Name, rolloutObjs.Rollouts[3].Name)
	assert.Len(t, Revisions(roInfo), 3)

	assert.Equal(t, Images(roInfo), []ImageInfo{
		{
			Image: "argoproj/rollouts-demo:does-not-exist",
			Tags:  []string{InfoTagCanary, InfoTagPing},
		},
		{
			Image: "argoproj/rollouts-demo:green",
			Tags:  []string{InfoTagStable, InfoTagPong},
		},
	})
}

func TestBlueGreenRolloutInfo(t *testing.T) {
	{
		rolloutObjs := testdata.NewBlueGreenRollout()
		roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns, nil)
		assert.Equal(t, roInfo.ObjectMeta.Name, rolloutObjs.Rollouts[0].Name)
		assert.Len(t, Revisions(roInfo), 3)

		assert.Len(t, ReplicaSetsByRevision(roInfo, 11), 1)
		assert.Len(t, ReplicaSetsByRevision(roInfo, 10), 1)
		assert.Len(t, ReplicaSetsByRevision(roInfo, 8), 1)

		assert.Equal(t, roInfo.ReplicaSets[0].ScaleDownDeadline, "")
		assert.Equal(t, ScaleDownDelay(*roInfo.ReplicaSets[0]), "")

		assert.Equal(t, Images(roInfo), []ImageInfo{
			{
				Image: "argoproj/rollouts-demo:blue",
				Tags:  []string{InfoTagStable, InfoTagActive},
			},
			{
				Image: "argoproj/rollouts-demo:green",
				Tags:  []string{InfoTagPreview},
			},
		})
	}
	{
		rolloutObjs := testdata.NewBlueGreenRollout()
		inFourHours := timeutil.Now().Add(4 * time.Hour).Truncate(time.Second).UTC().Format(time.RFC3339)
		rolloutObjs.ReplicaSets[0].Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inFourHours
		delayedRs := rolloutObjs.ReplicaSets[0].ObjectMeta.UID
		roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns, nil)

		assert.Equal(t, roInfo.ReplicaSets[1].ObjectMeta.UID, delayedRs)
		assert.Equal(t, roInfo.ReplicaSets[1].ScaleDownDeadline, inFourHours)
		assert.Equal(t, ScaleDownDelay(*roInfo.ReplicaSets[1]), "3h59m")
	}
}

func TestExperimentAnalysisRolloutInfo(t *testing.T) {
	rolloutObjs := testdata.NewExperimentAnalysisRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns, nil)
	assert.Equal(t, roInfo.ObjectMeta.Name, rolloutObjs.Rollouts[0].Name)
	assert.Len(t, Revisions(roInfo), 2)

	assert.Len(t, ReplicaSetsByRevision(roInfo, 1), 1)
	assert.Len(t, ReplicaSetsByRevision(roInfo, 2), 1)
	assert.Len(t, ExperimentsByRevision(roInfo, 2), 1)
	assert.Len(t, AnalysisRunsByRevision(roInfo, 2), 1)

	assert.Equal(t, Images(roInfo), []ImageInfo{
		{
			Image: "argoproj/rollouts-demo:blue",
			Tags:  []string{InfoTagStable},
		},
		{
			Image: "argoproj/rollouts-demo:yellow",
			Tags:  []string{InfoTagCanary},
		},
	})
}

func TestExperimentInfo(t *testing.T) {
	rolloutObjs := testdata.NewExperimentAnalysisRollout()
	expInfo := NewExperimentInfo(rolloutObjs.Experiments[0], rolloutObjs.ReplicaSets, rolloutObjs.AnalysisRuns, rolloutObjs.Pods)
	assert.Equal(t, expInfo.ObjectMeta.Name, rolloutObjs.Experiments[0].Name)

	assert.Equal(t, ExperimentImages(expInfo), []ImageInfo{
		{
			Image: "argoproj/rollouts-demo:blue",
		},
		{
			Image: "argoproj/rollouts-demo:yellow",
		},
	})
}

func TestRolloutStatusInvalidSpec(t *testing.T) {
	rolloutObjs := testdata.NewInvalidRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns, nil)
	assert.Equal(t, "Degraded", roInfo.Status)
	assert.Equal(t, "InvalidSpec: The Rollout \"rollout-invalid\" is invalid: spec.template.metadata.labels: Invalid value: map[string]string{\"app\":\"doesnt-match\"}: `selector` does not match template `labels`", roInfo.Message)
}

func TestRolloutAborted(t *testing.T) {
	rolloutObjs := testdata.NewAbortedRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns, nil)
	assert.Equal(t, "Degraded", roInfo.Status)
	assert.Equal(t, `RolloutAborted: metric "web" assessed Failed due to failed (1) > failureLimit (0)`, roInfo.Message)
}

func TestRolloutInfoMetadata(t *testing.T) {
	rolloutObjs := testdata.NewCanaryRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns, nil)
	assert.Equal(t, roInfo.ObjectMeta.Name, rolloutObjs.Rollouts[0].Name)
	assert.Equal(t, roInfo.ObjectMeta.Annotations, rolloutObjs.Rollouts[0].Annotations)
	assert.Equal(t, roInfo.ObjectMeta.Labels, rolloutObjs.Rollouts[0].Labels)
	assert.Equal(t, roInfo.ObjectMeta.Generation, rolloutObjs.Rollouts[0].Generation)
}

func TestRolloutInfoPauseStartTime(t *testing.T) {
	start := metav1.NewTime(time.Date(2026, 7, 10, 16, 0, 0, 0, time.UTC))
	canary := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec:       v1alpha1.RolloutSpec{Strategy: v1alpha1.RolloutStrategy{Canary: &v1alpha1.CanaryStrategy{}}},
		Status: v1alpha1.RolloutStatus{
			PauseConditions: []v1alpha1.PauseCondition{
				{Reason: v1alpha1.PauseReasonCanaryPauseStep, StartTime: start},
			},
		},
	}
	assert.Equal(t, "2026-07-10T16:00:00Z", NewRolloutInfo(canary, nil, nil, nil, nil, nil).PauseStartTime)

	noPause := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{Name: "test2", Namespace: "default"},
		Spec:       v1alpha1.RolloutSpec{Strategy: v1alpha1.RolloutStrategy{Canary: &v1alpha1.CanaryStrategy{}}},
	}
	assert.Equal(t, "", NewRolloutInfo(noPause, nil, nil, nil, nil, nil).PauseStartTime)

	nonCanaryPause := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{Name: "test3", Namespace: "default"},
		Spec:       v1alpha1.RolloutSpec{Strategy: v1alpha1.RolloutStrategy{Canary: &v1alpha1.CanaryStrategy{}}},
		Status: v1alpha1.RolloutStatus{
			PauseConditions: []v1alpha1.PauseCondition{
				{Reason: v1alpha1.PauseReasonBlueGreenPause, StartTime: start},
			},
		},
	}
	assert.Equal(t, "", NewRolloutInfo(nonCanaryPause, nil, nil, nil, nil, nil).PauseStartTime)
}

func TestPauseStepRemaining(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	timeutil.SetNowTimeFunc(func() time.Time { return now })
	defer timeutil.SetNowTimeFunc(time.Now)

	dur := intstr.FromString("40s")
	steps := []*v1alpha1.CanaryStep{
		{Pause: &v1alpha1.RolloutPause{}},               // index 0: indefinite pause
		{Pause: &v1alpha1.RolloutPause{Duration: &dur}}, // index 1: timed pause (40s)
	}

	// Paused 25s ago on the timed step (index 1) -> 15s remaining.
	roInfo := &rollout.RolloutInfo{
		Step:           "1/2",
		Steps:          steps,
		PauseStartTime: now.Add(-25 * time.Second).UTC().Format(time.RFC3339),
	}
	remaining, ok := PauseStepRemaining(roInfo)
	assert.True(t, ok)
	assert.Equal(t, "15s", remaining)

	// Elapsed beyond the duration -> clamps to 0s (controller not yet advanced).
	roInfo.PauseStartTime = now.Add(-60 * time.Second).UTC().Format(time.RFC3339)
	remaining, ok = PauseStepRemaining(roInfo)
	assert.True(t, ok)
	assert.Equal(t, "0s", remaining)

	// No pause condition recorded -> not applicable.
	roInfo.PauseStartTime = ""
	_, ok = PauseStepRemaining(roInfo)
	assert.False(t, ok)

	// Paused on an indefinite pause (no duration) -> not applicable.
	roInfo.Step = "0/2"
	roInfo.PauseStartTime = now.UTC().Format(time.RFC3339)
	_, ok = PauseStepRemaining(roInfo)
	assert.False(t, ok)
}

func TestPauseStepRemainingInvalidInputs(t *testing.T) {
	dur := intstr.FromString("40s")
	steps := []*v1alpha1.CanaryStep{{Pause: &v1alpha1.RolloutPause{Duration: &dur}}}

	// Malformed pause start time -> not applicable.
	_, ok := PauseStepRemaining(&rollout.RolloutInfo{Step: "0/1", Steps: steps, PauseStartTime: "not-a-timestamp"})
	assert.False(t, ok)

	// Step index past the end of the steps slice -> not applicable.
	_, ok = PauseStepRemaining(&rollout.RolloutInfo{Step: "5/1", Steps: steps, PauseStartTime: "2026-07-13T12:00:00Z"})
	assert.False(t, ok)

	// Non-numeric step index -> not applicable.
	_, ok = PauseStepRemaining(&rollout.RolloutInfo{Step: "", Steps: steps, PauseStartTime: "2026-07-13T12:00:00Z"})
	assert.False(t, ok)
}
