package info

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

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

// TestRolloutInfoPauseDurationSeconds pins the normalization of every duration
// form time.ParseDuration accepts, so clients never reimplement the grammar.
// See RolloutPause.DurationSeconds for the contract.
func TestRolloutInfoPauseDurationSeconds(t *testing.T) {
	tests := []struct {
		name     string
		duration *intstr.IntOrString
		expected int32
	}{
		{"integer duration is seconds", v1alpha1.DurationFromInt(30), 30},
		{"bare string duration is seconds", v1alpha1.DurationFromString("300"), 300},
		{"seconds unit", v1alpha1.DurationFromString("30s"), 30},
		{"minutes unit", v1alpha1.DurationFromString("15m"), 900},
		{"hours unit", v1alpha1.DurationFromString("1h"), 3600},
		{"compound hours and minutes", v1alpha1.DurationFromString("1h30m"), 5400},
		{"compound hours minutes and seconds", v1alpha1.DurationFromString("2h45m30s"), 9930},
		{"fractional hours", v1alpha1.DurationFromString("1.5h"), 5400},
		{"fractional minutes", v1alpha1.DurationFromString("0.5m"), 30},
		{"sub-second duration truncates toward zero", v1alpha1.DurationFromString("500ms"), 0},
		{"mixed sub-second duration truncates toward zero", v1alpha1.DurationFromString("1500ms"), 1},
		{"microseconds truncate toward zero", v1alpha1.DurationFromString("100us"), 0},
		{"negative duration is preserved", v1alpha1.DurationFromString("-5m"), -300},
		{"unparseable duration reports -1", v1alpha1.DurationFromString("1z"), -1},
		{"indefinite pause reports 0", nil, 0},
	}

	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	timeutil.SetNowTimeFunc(func() time.Time { return now })
	defer timeutil.SetNowTimeFunc(time.Now)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ro := pausedCanary(tc.duration, metav1.NewTime(now))
			assert.Equal(t, tc.expected, NewRolloutInfo(ro, nil, nil, nil, nil, nil).PauseDurationSeconds)
		})
	}
}

// TestRolloutInfoPauseRemainingSeconds covers the snapshot field clients render
// from directly. It is clamped to [0, duration] so a client never has to guard
// against a negative or over-long value.
func TestRolloutInfoPauseRemainingSeconds(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	timeutil.SetNowTimeFunc(func() time.Time { return now })
	defer timeutil.SetNowTimeFunc(time.Now)

	dur := intstr.FromString("1h30m")

	tests := []struct {
		name      string
		startedAt time.Time
		expected  int32
	}{
		{"just paused reports the full duration", now, 5400},
		{"a third elapsed", now.Add(-30 * time.Minute), 3600},
		{"nearly done", now.Add(-89 * time.Minute), 60},
		{"elapsed clamps to zero", now.Add(-2 * time.Hour), 0},
		{"start time in the future clamps to the duration", now.Add(10 * time.Minute), 5400},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			roInfo := NewRolloutInfo(pausedCanary(&dur, metav1.NewTime(tc.startedAt)), nil, nil, nil, nil, nil)
			assert.Equal(t, tc.expected, roInfo.PauseRemainingSeconds)
			assert.Equal(t, int32(5400), roInfo.PauseDurationSeconds)
			assert.LessOrEqual(t, roInfo.PauseRemainingSeconds, roInfo.PauseDurationSeconds, "remaining must never exceed duration")
			assert.GreaterOrEqual(t, roInfo.PauseRemainingSeconds, int32(0), "remaining must never be negative")
		})
	}
}

// TestRolloutInfoPauseNotInProgress verifies both fields stay zero unless the
// rollout is actually paused on a timed canary step, so clients can treat
// pauseDurationSeconds > 0 as "a timed pause is running".
func TestRolloutInfoPauseNotInProgress(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	timeutil.SetNowTimeFunc(func() time.Time { return now })
	defer timeutil.SetNowTimeFunc(time.Now)

	dur := intstr.FromString("15m")
	steps := []v1alpha1.CanaryStep{{Pause: &v1alpha1.RolloutPause{Duration: &dur}}}

	notPaused := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec:       v1alpha1.RolloutSpec{Strategy: v1alpha1.RolloutStrategy{Canary: &v1alpha1.CanaryStrategy{Steps: steps}}},
		Status:     v1alpha1.RolloutStatus{CurrentStepIndex: ptr.To[int32](0)},
	}
	roInfo := NewRolloutInfo(notPaused, nil, nil, nil, nil, nil)
	assert.Equal(t, int32(0), roInfo.PauseDurationSeconds)
	assert.Equal(t, int32(0), roInfo.PauseRemainingSeconds)

	blueGreenPause := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{Name: "test2", Namespace: "default"},
		Spec:       v1alpha1.RolloutSpec{Strategy: v1alpha1.RolloutStrategy{Canary: &v1alpha1.CanaryStrategy{Steps: steps}}},
		Status: v1alpha1.RolloutStatus{
			CurrentStepIndex: ptr.To[int32](0),
			PauseConditions: []v1alpha1.PauseCondition{
				{Reason: v1alpha1.PauseReasonBlueGreenPause, StartTime: metav1.NewTime(now)},
			},
		},
	}
	roInfo = NewRolloutInfo(blueGreenPause, nil, nil, nil, nil, nil)
	assert.Equal(t, int32(0), roInfo.PauseDurationSeconds)
	assert.Equal(t, int32(0), roInfo.PauseRemainingSeconds)

	// An indefinite pause is in progress, but there is nothing to count down.
	indefinite := pausedCanary(nil, metav1.NewTime(now))
	roInfo = NewRolloutInfo(indefinite, nil, nil, nil, nil, nil)
	assert.Equal(t, int32(0), roInfo.PauseDurationSeconds)
	assert.Equal(t, int32(0), roInfo.PauseRemainingSeconds)
}

// TestRolloutInfoPauseOnNonPauseStep covers status that disagrees with itself:
// a canary pause condition is recorded, but the current step index does not
// point at a pause step. There is nothing to count down, so both fields stay 0
// rather than reporting a duration from an unrelated step.
func TestRolloutInfoPauseOnNonPauseStep(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	timeutil.SetNowTimeFunc(func() time.Time { return now })
	defer timeutil.SetNowTimeFunc(time.Now)

	pausedOn := func(steps []v1alpha1.CanaryStep, idx int32) *rollout.RolloutInfo {
		ro := &v1alpha1.Rollout{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec:       v1alpha1.RolloutSpec{Strategy: v1alpha1.RolloutStrategy{Canary: &v1alpha1.CanaryStrategy{Steps: steps}}},
			Status: v1alpha1.RolloutStatus{
				CurrentStepIndex: ptr.To[int32](idx),
				PauseConditions: []v1alpha1.PauseCondition{
					{Reason: v1alpha1.PauseReasonBlueGreenPause, StartTime: metav1.NewTime(now)},
					{Reason: v1alpha1.PauseReasonCanaryPauseStep, StartTime: metav1.NewTime(now)},
				},
			},
		}
		return NewRolloutInfo(ro, nil, nil, nil, nil, nil)
	}

	// Current step is a setWeight, not a pause.
	roInfo := pausedOn([]v1alpha1.CanaryStep{{SetWeight: ptr.To[int32](20)}}, 0)
	assert.Equal(t, int32(0), roInfo.PauseDurationSeconds)
	assert.Equal(t, int32(0), roInfo.PauseRemainingSeconds)

	// Step index past the end of the configured steps.
	dur := intstr.FromString("15m")
	roInfo = pausedOn([]v1alpha1.CanaryStep{{Pause: &v1alpha1.RolloutPause{Duration: &dur}}}, 5)
	assert.Equal(t, int32(0), roInfo.PauseDurationSeconds)
	assert.Equal(t, int32(0), roInfo.PauseRemainingSeconds)

	// No canary steps configured at all.
	roInfo = pausedOn(nil, 0)
	assert.Equal(t, int32(0), roInfo.PauseDurationSeconds)
	assert.Equal(t, int32(0), roInfo.PauseRemainingSeconds)
}

// pausedCanary builds a canary rollout paused on a single pause step.
func pausedCanary(duration *intstr.IntOrString, startTime metav1.Time) *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: v1alpha1.RolloutSpec{Strategy: v1alpha1.RolloutStrategy{Canary: &v1alpha1.CanaryStrategy{
			Steps: []v1alpha1.CanaryStep{{Pause: &v1alpha1.RolloutPause{Duration: duration}}},
		}}},
		Status: v1alpha1.RolloutStatus{
			CurrentStepIndex: ptr.To[int32](0),
			PauseConditions: []v1alpha1.PauseCondition{
				{Reason: v1alpha1.PauseReasonCanaryPauseStep, StartTime: startTime},
			},
		},
	}
}

func TestPauseStepRemaining(t *testing.T) {
	// The controller already did the arithmetic; this only formats.
	remaining, ok := PauseStepRemaining(&rollout.RolloutInfo{PauseDurationSeconds: 5400, PauseRemainingSeconds: 3600})
	assert.True(t, ok)
	assert.Equal(t, "60m", remaining)

	remaining, ok = PauseStepRemaining(&rollout.RolloutInfo{PauseDurationSeconds: 40, PauseRemainingSeconds: 15})
	assert.True(t, ok)
	assert.Equal(t, "15s", remaining)

	// Elapsed, but the controller has not advanced the step yet.
	remaining, ok = PauseStepRemaining(&rollout.RolloutInfo{PauseDurationSeconds: 40, PauseRemainingSeconds: 0})
	assert.True(t, ok)
	assert.Equal(t, "0s", remaining)

	// Indefinite pause, or not paused on a step.
	_, ok = PauseStepRemaining(&rollout.RolloutInfo{PauseDurationSeconds: 0})
	assert.False(t, ok)

	// Duration the controller could not parse.
	_, ok = PauseStepRemaining(&rollout.RolloutInfo{PauseDurationSeconds: -1})
	assert.False(t, ok)
}
