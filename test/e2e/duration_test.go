//go:build e2e
// +build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/test/fixtures"
)

type DurationSuite struct {
	fixtures.E2ESuite
}

func TestDurationSuite(t *testing.T) {
	suite.Run(t, new(DurationSuite))
}

// assertDurationFieldsConsistency validates that required duration fields are non-nil and consistent
func assertDurationFieldsConsistency(t *testing.T, ro *v1alpha1.Rollout) {
	t.Helper()

	require.NotNil(t, ro.Status.Duration, "Duration should be initialized")
	assert.NotNil(t, ro.Status.Duration.RolloutStartedAt, "RolloutStartedAt should be set")

	if ro.Status.Duration.FinishedAt != nil {
		assert.NotNil(t, ro.Status.Duration.CompletionStatus, "CompletionStatus should be set when completed")
		assert.Nil(t, ro.Status.Duration.ManualPauseStartedAt, "ManualPauseStartedAt should be nil when completed")
		assert.True(t, ro.Status.Duration.FinishedAt.After(ro.Status.Duration.RolloutStartedAt.Time), "FinishedAt should be after RolloutStartedAt")
	}
}

// Canary Lifecycle Tests

func (s *DurationSuite) TestCanaryDuration_InitialRollout() {
	canarySteps := `
- setWeight: 50
- pause: {duration: 1s}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-start"}).
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Initial rollout should have started and completed
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.Nil(s.T(), ro.Status.Duration.TotalManualPauseDuration)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
		})
}

func (s *DurationSuite) TestCanaryDuration_SpecPaused() {
	canarySteps := `
- setWeight: 50
- pause: {duration: 1s}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-spec-paused"}).
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		// Set spec.paused = true
		UpdateSpec(`{"spec":{"paused":true}}`).
		UpdateSpec(`{"spec":{"template":{"metadata":{"annotations":{"update":"1"}}}}}`).
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be paused via spec.paused, tracking manual pause
			assertDurationFieldsConsistency(s.T(), ro)
			assert.NotNil(s.T(), ro.Status.Duration.ManualPauseStartedAt, "ManualPauseStartedAt should be set")
		}).
		When().
		// Resume by setting spec.paused = false
		Sleep(2*time.Second).
		UpdateSpec(`{"spec":{"paused":false}}`).
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should have completed after resume
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			// TotalManualPauseDuration should be non-zero
			assert.Greater(s.T(), *ro.Status.Duration.TotalManualPauseDuration, int64(0), "TotalManualPauseDuration should be greater than 0")
		})
}

func (s *DurationSuite) TestCanaryDuration_PauseStep() {
	canarySteps := `
- setWeight: 50
- pause: {duration: 1s}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-pause-step"}).
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Progressing").
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.Nil(s.T(), ro.Status.Duration.TotalManualPauseDuration)
		})
}

func (s *DurationSuite) TestCanaryDuration_IndefinitePauseStep() {
	canarySteps := `
- setWeight: 50
- pause: {}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-indefinite-pause-step"}).
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			assertDurationFieldsConsistency(s.T(), ro)
			assert.NotNil(s.T(), ro.Status.Duration.ManualPauseStartedAt, "ManualPauseStartedAt should be nil for step pause")
		}).
		When().
		Sleep(2*time.Second).
		PromoteRollout().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.Greater(s.T(), *ro.Status.Duration.TotalManualPauseDuration, int64(0), "TotalManualPauseDuration should be greater than 0")
		})
}

func (s *DurationSuite) TestCanaryDuration_FullyPromoted() {
	canarySteps := `
- setWeight: 25
- pause: {}
- setWeight: 50
- pause: {}
- setWeight: 75
- pause: {}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-fully-promoted"}).
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		// Full promote (skip all remaining steps)
		PromoteRolloutFull().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with manually-promoted status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusFastPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.GreaterOrEqual(s.T(), *ro.Status.Duration.TotalManualPauseDuration, int64(0))
		})
}

func (s *DurationSuite) TestCanaryDuration_Abort() {
	canarySteps := `
- setWeight: 50
- pause: {}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-abort"}).
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		AbortRollout().
		WaitForRolloutStatus("Degraded", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with abort status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusAborted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.GreaterOrEqual(s.T(), *ro.Status.Duration.TotalManualPauseDuration, int64(0))
		})
}

func (s *DurationSuite) TestCanaryDuration_Retry() {
	initialStartedAt := metav1.Time{}
	canarySteps := `
- setWeight: 50
- pause: {}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-retry"}).
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		AbortRollout().
		WaitForRolloutStatus("Degraded").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			initialStartedAt = *ro.Status.Duration.RolloutStartedAt
		}).
		When().
		// Retry after abort
		Sleep(2*time.Second).
		RetryRollout().
		WaitForRolloutStatus("Paused", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should have restarted, duration reset
			assertDurationFieldsConsistency(s.T(), ro)
			assert.NotEqual(s.T(), initialStartedAt, *ro.Status.Duration.RolloutStartedAt)
			assert.Nil(s.T(), ro.Status.Duration.FinishedAt, "FinishedAt should be nil after retry")
			assert.Nil(s.T(), ro.Status.Duration.CompletionStatus, "CompletionStatus should be nil after retry")
		})
}

func (s *DurationSuite) TestCanaryDuration_SupersededRollout() {
	initialStartedAt := metav1.Time{}
	canarySteps := `
- setWeight: 50
- pause: {}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-superseded-forward"}).
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateVersion("2").
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			initialStartedAt = *ro.Status.Duration.RolloutStartedAt
		}).
		When().
		UpdateVersion("3").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			assertDurationFieldsConsistency(s.T(), ro)
			assert.NotEqual(s.T(), initialStartedAt, *ro.Status.Duration.RolloutStartedAt)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
		})
}

func (s *DurationSuite) TestCanaryDuration_SupersededRollback() {
	initialStartedAt := metav1.Time{}
	canarySteps := `
- setWeight: 50
- pause: {}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-superseded-rollback"}).
		SetSteps(canarySteps).
		SetVersion("1").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		// Start a rollout (revision 2)
		UpdateVersion("2").
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			initialStartedAt = *ro.Status.Duration.RolloutStartedAt
		}).
		When().
		// Supersede with rollback to revision 1
		UpdateVersion("1").
		// should be a fast rollback
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with fast rollback status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), initialStartedAt, *ro.Status.Duration.RolloutStartedAt)
			assert.Equal(s.T(), v1alpha1.CompletionStatusFastRollbacked, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.GreaterOrEqual(s.T(), *ro.Status.Duration.TotalManualPauseDuration, int64(0))
		})
}

func (s *DurationSuite) TestCanaryDuration_RollbackOutsideWindow() {
	canarySteps := `
- setWeight: 50
- pause: {}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-rollback-outside"}).
		SetSteps(canarySteps).
		SetVersion("1").
		RevisionHistoryLimit(5).
		SetRollbackWindow(1).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		// Complete a rollout to revision 2
		UpdateVersion("2").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		// Complete a rollout to revision 3
		UpdateVersion("3").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		// Complete a rollout to revision 4
		UpdateVersion("4").
		WaitForRolloutStatus("Paused").
		// Rollback to revision 1 (outside scaledown window)
		UpdateVersion("1").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with rollbacked status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusRollbacked, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
		})
}

func (s *DurationSuite) TestCanaryDuration_RollbackInsideWindow() {
	canarySteps := `
- setWeight: 50
- pause: {}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-rollback-outside"}).
		SetSteps(canarySteps).
		SetVersion("1").
		RevisionHistoryLimit(5).
		SetRollbackWindow(1).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		// Complete a rollout to revision 2
		UpdateVersion("2").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		// Complete a rollout to revision 3
		UpdateVersion("3").
		WaitForRolloutStatus("Paused").
		// Rollback to revision 1 (inside scaledown window)
		UpdateVersion("1").
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with rollbacked status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusFastRollbacked, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
		})
}

func (s *DurationSuite) TestCanaryDuration_ScalingOnly() {
	initialStartedAt := metav1.Time{}
	canarySteps := `
- setWeight: 50
- pause: {duration: 1s}`

	s.Given().
		RolloutTemplate("@functional/canary-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "canary-duration-scaling-only"}).
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			initialStartedAt = *ro.Status.Duration.RolloutStartedAt
		}).
		When().
		UpdateSpec(`{"spec":{"replicas":2}}`).
		WaitForRolloutReplicas(2, 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Scaling only should not affect duration tracking
			// Duration should still be from the last rollout (completed)
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), initialStartedAt, *ro.Status.Duration.RolloutStartedAt)
		})
}

// BlueGreen Lifecycle Tests

func (s *DurationSuite) TestBlueGreenDuration_InitialRollout() {
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-start"}).
		AutoPromotionSeconds(2).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Initial rollout should have started and completed
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.Nil(s.T(), ro.Status.Duration.TotalManualPauseDuration)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
		})
}

func (s *DurationSuite) TestBlueGreenDuration_SpecPaused() {
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-spec-paused"}).
		AutoPromotionSeconds(2).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec(`{"spec":{"paused":true}}`).
		UpdateVersion("1").
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be paused via spec.paused, tracking manual pause
			assertDurationFieldsConsistency(s.T(), ro)
			assert.NotNil(s.T(), ro.Status.Duration.ManualPauseStartedAt, "ManualPauseStartedAt should be set")
		}).
		When().
		// Resume by setting spec.paused = false
		Sleep(2*time.Second).
		UpdateSpec(`{"spec":{"paused":false}}`).
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should have completed after resume
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			// TotalManualPauseDuration should be non-zero
			assert.Greater(s.T(), *ro.Status.Duration.TotalManualPauseDuration, int64(0), "TotalManualPauseDuration should be greater than 0")
		})
}

func (s *DurationSuite) TestBlueGreenDuration_AutoPromotion() {
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-auto-promotion"}).
		AutoPromotionSeconds(2).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should auto-promote after delay
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.Nil(s.T(), ro.Status.Duration.TotalManualPauseDuration)
		})
}

func (s *DurationSuite) TestBlueGreenDuration_ManualPromotion() {
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-manual-promotion"}).
		AutoPromotionEnabled(false).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.GreaterOrEqual(s.T(), *ro.Status.Duration.TotalManualPauseDuration, int64(0), "TotalManualPauseDuration should be greater than 0")
		})
}

func (s *DurationSuite) TestBlueGreenDuration_FullyPromoted() {
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-fully-promoted"}).
		AutoPromotionEnabled(false).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		PromoteRolloutFull().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusFastPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.GreaterOrEqual(s.T(), *ro.Status.Duration.TotalManualPauseDuration, int64(0), "TotalManualPauseDuration should be greater than 0")
		})
}

func (s *DurationSuite) TestBlueGreenDuration_Abort() {
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-abort"}).
		AutoPromotionEnabled(false).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		AbortRollout().
		WaitForRolloutStatus("Degraded", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with abort status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusAborted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.GreaterOrEqual(s.T(), *ro.Status.Duration.TotalManualPauseDuration, int64(0))
		})
}

func (s *DurationSuite) TestBlueGreenDuration_Retry() {
	initialStartedAt := metav1.Time{}
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-retry"}).
		AutoPromotionEnabled(false).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		AbortRollout().
		WaitForRolloutStatus("Degraded").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			initialStartedAt = *ro.Status.Duration.RolloutStartedAt
		}).
		When().
		// Retry after abort
		Sleep(2*time.Second).
		RetryRollout().
		WaitForRolloutStatus("Paused", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should have restarted, duration reset
			assertDurationFieldsConsistency(s.T(), ro)
			assert.NotEqual(s.T(), initialStartedAt, *ro.Status.Duration.RolloutStartedAt)
			assert.Nil(s.T(), ro.Status.Duration.FinishedAt, "FinishedAt should be nil after retry")
			assert.Nil(s.T(), ro.Status.Duration.CompletionStatus, "CompletionStatus should be nil after retry")
		})
}

// TODO
func (s *DurationSuite) TestBlueGreenDuration_SupersededRollout() {
	initialStartedAt := metav1.Time{}
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-superseded-rollout"}).
		AutoPromotionEnabled(false).
		SetVersion("1").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateVersion("2").
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			initialStartedAt = *ro.Status.Duration.RolloutStartedAt
		}).
		When().
		UpdateVersion("3").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			assertDurationFieldsConsistency(s.T(), ro)
			assert.NotEqual(s.T(), initialStartedAt, *ro.Status.Duration.RolloutStartedAt)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
		})
}

func (s *DurationSuite) TestBlueGreenDuration_SupersededRollbackToStable() {
	initialStartedAt := metav1.Time{}
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-superseded-rollback-stable"}).
		AutoPromotionEnabled(false).
		SetVersion("1").
		ScaleDownDelaySeconds(0). // No scale-down delay so we know we ecaluate a rollback to stable
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		// Start a rollout (revision 2)
		UpdateVersion("2").
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			initialStartedAt = *ro.Status.Duration.RolloutStartedAt
		}).
		When().
		// Supersede with rollback to revision 1
		UpdateVersion("1").
		// should be a fast rollback
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with fast rollback status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), initialStartedAt, *ro.Status.Duration.RolloutStartedAt)
			assert.Equal(s.T(), v1alpha1.CompletionStatusFastRollbacked, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
			assert.GreaterOrEqual(s.T(), *ro.Status.Duration.TotalManualPauseDuration, int64(0))
		})
}

func (s *DurationSuite) TestBlueGreenDuration_SupersededRollback() {

	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-superseded-rollback"}).
		AutoPromotionEnabled(false).
		SetVersion("1").
		ScaleDownDelaySeconds(30). // Explicitly set scale down delay since we are testing it
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		// Complete a rollout to revision 2
		UpdateVersion("2").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		// Complete a rollout to revision 3
		UpdateVersion("3").
		WaitForRolloutStatus("Paused").
		// Rollback to revision 1 (inside scaledown delay)
		UpdateVersion("1").
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with rollbacked status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusFastRollbacked, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
		})
}

func (s *DurationSuite) TestBlueGreenDuration_RollbackOutsideWindow() {

	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-rollback-outside-window"}).
		AutoPromotionEnabled(false).
		SetVersion("1").
		RevisionHistoryLimit(5).
		SetRollbackWindow(1).
		ScaleDownDelaySeconds(0). // No scale-down delay so we know that windows are used for rollback
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		// Complete a rollout to revision 2
		UpdateVersion("2").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		// Complete a rollout to revision 3
		UpdateVersion("3").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		// Complete a rollout to revision 4
		UpdateVersion("4").
		WaitForRolloutStatus("Paused").
		// Rollback to revision 1 (outside scaledown window)
		UpdateVersion("1").
		WaitForRolloutStatus("Paused", 10*time.Second).
		PromoteRollout().
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with rollbacked status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusRollbacked, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
		})
}

func (s *DurationSuite) TestBlueGreenDuration_RollbackInsideWindow() {

	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-rollback-inside-window"}).
		AutoPromotionEnabled(false).
		SetVersion("1").
		RevisionHistoryLimit(5).
		SetRollbackWindow(1).
		ScaleDownDelaySeconds(0). // No scale-down delay so we know that windows are used for rollback
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		// Complete a rollout to revision 2
		UpdateVersion("2").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		// Complete a rollout to revision 3
		UpdateVersion("3").
		WaitForRolloutStatus("Paused").
		// Rollback to revision 1 (inside scaledown window)
		UpdateVersion("1").
		WaitForRolloutStatus("Healthy", 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed with rollbacked status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusFastRollbacked, *ro.Status.Duration.CompletionStatus)
			assert.NotNil(s.T(), ro.Status.Duration.FinishedAt)
		})
}

func (s *DurationSuite) TestBlueGreenDuration_ScalingOnly() {
	initialStartedAt := metav1.Time{}
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-scaling-only"}).
		AutoPromotionSeconds(2).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			initialStartedAt = *ro.Status.Duration.RolloutStartedAt
		}).
		When().
		UpdateSpec(`{"spec":{"replicas":2}}`).
		WaitForRolloutReplicas(2, 10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Scaling only should not affect duration tracking
			// Duration should still be from the last rollout (completed)
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), initialStartedAt, *ro.Status.Duration.RolloutStartedAt)
		})
}
