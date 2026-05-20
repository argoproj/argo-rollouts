package v1alpha1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRolloutPauseDuration(t *testing.T) {
	rp := RolloutPause{}
	assert.Equal(t, int32(0), rp.DurationSeconds())
	rp.Duration = DurationFromInt(10)
	assert.Equal(t, int32(10), rp.DurationSeconds())
	rp.Duration = DurationFromString("10")
	assert.Equal(t, int32(10), rp.DurationSeconds())
	rp.Duration = DurationFromString("10s")
	assert.Equal(t, int32(10), rp.DurationSeconds())
	rp.Duration = DurationFromString("1h")
	assert.Equal(t, int32(3600), rp.DurationSeconds())
	rp.Duration = DurationFromString("1ms")
	assert.Equal(t, int32(0), rp.DurationSeconds())
	rp.Duration = DurationFromString("1z")
	assert.Equal(t, int32(-1), rp.DurationSeconds())
	rp.Duration = DurationFromString("20000000000") // out of int32
	assert.Equal(t, int32(-1), rp.DurationSeconds())
}

// TestRolloutDurationStatus_IsAlreadyCompleted tests the IsAlreadyCompleted helper method
func TestRolloutDurationStatus_IsAlreadyCompleted(t *testing.T) {
	tests := []struct {
		name           string
		durationStatus *RolloutDurationStatus
		expected       bool
	}{
		{
			name:           "nil durationStatus",
			durationStatus: nil,
			expected:       false,
		},
		{
			name: "nil finishedAt",
			durationStatus: &RolloutDurationStatus{
				FinishedAt: nil,
			},
			expected: false,
		},
		{
			name: "finishedAt set",
			durationStatus: &RolloutDurationStatus{
				FinishedAt: &metav1.Time{Time: time.Now()},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.durationStatus.IsAlreadyCompleted()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestRolloutDurationStatus_CompleteRollout tests the CompleteRollout helper method
func TestRolloutDurationStatus_CompleteRollout(t *testing.T) {
	t.Run("CompleteRollout sets FinishedAt and CompletionStatus", func(t *testing.T) {
		now := metav1.Now()
		startTime := metav1.NewTime(now.Add(-5 * time.Minute))

		ds := &RolloutDurationStatus{
			RolloutStartedAt: &startTime,
		}

		completeTime := metav1.NewTime(now.Add(1 * time.Minute))
		ds.CompleteRollout(completeTime, "promoted")

		assert.NotNil(t, ds.FinishedAt)
		assert.Equal(t, completeTime, *ds.FinishedAt)
		assert.NotNil(t, ds.CompletionStatus)
		assert.Equal(t, "promoted", *ds.CompletionStatus)
	})

	t.Run("CompleteRollout finalizes active manual pause", func(t *testing.T) {
		now := metav1.Now()
		startTime := metav1.NewTime(now.Add(-10 * time.Minute))
		pauseStartTime := metav1.NewTime(now.Add(-2 * time.Minute))
		previousPause := int64(180) // 3 minutes

		ds := &RolloutDurationStatus{
			RolloutStartedAt:         &startTime,
			ManualPauseStartedAt:     &pauseStartTime,
			TotalManualPauseDuration: &previousPause,
		}

		ds.CompleteRollout(now, "promoted")

		// ManualPauseStartedAt should be cleared
		assert.Nil(t, ds.ManualPauseStartedAt)

		// TotalManualPauseDuration should include the active pause (~2 minutes = ~120 seconds)
		assert.NotNil(t, ds.TotalManualPauseDuration)
		assert.Greater(t, *ds.TotalManualPauseDuration, int64(290)) // 180 + ~120
		assert.Less(t, *ds.TotalManualPauseDuration, int64(310))
	})

	t.Run("CompleteRollout with no previous pause duration", func(t *testing.T) {
		now := metav1.Now()
		startTime := metav1.NewTime(now.Add(-5 * time.Minute))
		pauseStartTime := metav1.NewTime(now.Add(-1 * time.Minute))

		ds := &RolloutDurationStatus{
			RolloutStartedAt:     &startTime,
			ManualPauseStartedAt: &pauseStartTime,
		}

		ds.CompleteRollout(now, "promoted")

		// ManualPauseStartedAt should be cleared
		assert.Nil(t, ds.ManualPauseStartedAt)

		// TotalManualPauseDuration should be set to current pause (~1 minute = ~60 seconds)
		assert.NotNil(t, ds.TotalManualPauseDuration)
		assert.Greater(t, *ds.TotalManualPauseDuration, int64(55))
		assert.Less(t, *ds.TotalManualPauseDuration, int64(65))
	})

	t.Run("CompleteRollout on nil durationStatus is safe", func(t *testing.T) {
		var ds *RolloutDurationStatus
		now := metav1.Now()

		// Should not panic
		ds.CompleteRollout(now, "promoted")
	})
}

// TestRolloutDurationStatus_GetCompletionStatus tests the GetCompletionStatus helper method
func TestRolloutDurationStatus_GetCompletionStatus(t *testing.T) {
	tests := []struct {
		name           string
		durationStatus *RolloutDurationStatus
		expected       string
	}{
		{
			name:           "nil durationStatus",
			durationStatus: nil,
			expected:       "",
		},
		{
			name: "nil completionStatus",
			durationStatus: &RolloutDurationStatus{
				CompletionStatus: nil,
			},
			expected: "",
		},
		{
			name: "promoted",
			durationStatus: &RolloutDurationStatus{
				CompletionStatus: func() *string { s := "promoted"; return &s }(),
			},
			expected: "promoted",
		},
		{
			name: "manually-promoted",
			durationStatus: &RolloutDurationStatus{
				CompletionStatus: func() *string { s := "manually-promoted"; return &s }(),
			},
			expected: "manually-promoted",
		},
		{
			name: "aborted",
			durationStatus: &RolloutDurationStatus{
				CompletionStatus: func() *string { s := "aborted"; return &s }(),
			},
			expected: "aborted",
		},
		{
			name: "superseded",
			durationStatus: &RolloutDurationStatus{
				CompletionStatus: func() *string { s := "superseded"; return &s }(),
			},
			expected: "superseded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.durationStatus.GetCompletionStatus()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRolloutDurationStatus_GetCompletionLogFields(t *testing.T) {
	t.Run("returns correct fields including status", func(t *testing.T) {
		now := metav1.Now()
		startTime := metav1.NewTime(now.Add(-5 * time.Minute))
		totalManualPauseDuration := int64(60) // 1 minute
		completionStatus := "promoted"

		status := &RolloutDurationStatus{
			RolloutStartedAt:         &startTime,
			FinishedAt:               &now,
			CompletionStatus:         &completionStatus,
			TotalManualPauseDuration: &totalManualPauseDuration,
		}

		fields := status.GetCompletionLogFields()

		assert.NotEmpty(t, fields)
		assert.Equal(t, "promoted", fields["status"])
		assert.Equal(t, 300.0, fields["duration_total_seconds"])
		assert.Equal(t, 240.0, fields["duration_progression_seconds"])
		assert.Equal(t, 60.0, fields["duration_manual_pause_seconds"])
	})

	t.Run("returns empty map if FinishedAt is nil", func(t *testing.T) {
		now := metav1.Now()
		startTime := metav1.NewTime(now.Add(-5 * time.Minute))
		completionStatus := "promoted"

		status := &RolloutDurationStatus{
			RolloutStartedAt: &startTime,
			CompletionStatus: &completionStatus,
		}

		fields := status.GetCompletionLogFields()
		assert.Empty(t, fields)
	})

	t.Run("returns empty map if RolloutStartedAt is nil", func(t *testing.T) {
		now := metav1.Now()
		completionStatus := "promoted"

		status := &RolloutDurationStatus{
			FinishedAt:       &now,
			CompletionStatus: &completionStatus,
		}

		fields := status.GetCompletionLogFields()
		assert.Empty(t, fields)
	})

	t.Run("returns empty map if CompletionStatus is empty", func(t *testing.T) {
		now := metav1.Now()
		startTime := metav1.NewTime(now.Add(-5 * time.Minute))

		status := &RolloutDurationStatus{
			RolloutStartedAt: &startTime,
			FinishedAt:       &now,
		}

		fields := status.GetCompletionLogFields()
		assert.Empty(t, fields)
	})

	t.Run("returns empty map if status is nil", func(t *testing.T) {
		var status *RolloutDurationStatus = nil
		fields := status.GetCompletionLogFields()
		assert.Empty(t, fields)
	})

	t.Run("handles zero manual pause duration", func(t *testing.T) {
		now := metav1.Now()
		startTime := metav1.NewTime(now.Add(-2 * time.Minute))
		completionStatus := "promoted"

		status := &RolloutDurationStatus{
			RolloutStartedAt: &startTime,
			FinishedAt:       &now,
			CompletionStatus: &completionStatus,
		}

		fields := status.GetCompletionLogFields()

		assert.NotEmpty(t, fields)
		assert.Equal(t, "promoted", fields["status"])
		assert.Equal(t, 120.0, fields["duration_total_seconds"])
		assert.Equal(t, 120.0, fields["duration_progression_seconds"])
		assert.Equal(t, 0.0, fields["duration_manual_pause_seconds"])
	})
}
