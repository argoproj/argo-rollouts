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
		WaitForRolloutStatus("Healthy").
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
		Sleep(2 * time.Second).
		UpdateSpec(`{"spec":{"paused":false}}`).
		WaitForRolloutStatus("Healthy").
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
		WaitForRolloutStatus("Healthy").
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
		Sleep(2 * time.Second).
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
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
		WaitForRolloutStatus("Healthy").
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
		WaitForRolloutStatus("Degraded").
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
		Sleep(2 * time.Second).
		RetryRollout().
		WaitForRolloutStatus("Paused").
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
		WaitForRolloutStatus("Healthy").
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
		// Supersede with rollback to revision 1 (within scaledown window)
		UpdateVersion("1").
		// should be a fast rollback since it's within the rollback window
		WaitForRolloutStatus("Healthy").
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
		// Rollback to revision 1 (outside scaledown window)
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
		WaitForRolloutReplicas(2).
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

func (s *DurationSuite) TestBlueGreenDuration_01_Start() {
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-start"}).
		AutoPromotionSeconds(2).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Initial rollout should have started and completed
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
		})
}

func (s *DurationSuite) TestBlueGreenDuration_02_SpecPaused() {
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-spec-paused"}).
		AutoPromotionSeconds(2).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec(`{"spec":{"template":{"metadata":{"annotations":{"update":"1"}}}}}`).
		WaitForRolloutStatus("Paused").
		// Set spec.paused = true
		UpdateSpec(`{"spec":{"paused":true}}`).
		Sleep(2 * time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be paused via spec.paused, tracking manual pause
			assertDurationFieldsConsistency(s.T(), ro)
			assert.NotNil(s.T(), ro.Status.Duration.ManualPauseStartedAt, "ManualPauseStartedAt should be set")
		}).
		When().
		// Resume by setting spec.paused = false
		UpdateSpec(`{"spec":{"paused":false}}`).
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should have completed after resume
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			// TotalManualPauseDuration should be non-zero
			assert.Greater(s.T(), *ro.Status.Duration.TotalManualPauseDuration, int64(0), "TotalManualPauseDuration should be greater than 0")
		})
}

func (s *DurationSuite) TestBlueGreenDuration_03_AutoPromotion() {
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-auto-promotion"}).
		AutoPromotionSeconds(2).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should auto-promote after delay
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
		})
}

func (s *DurationSuite) TestBlueGreenDuration_04_Abort() {
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-abort"}).
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
			// Should be completed with abort status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusAborted, *ro.Status.Duration.CompletionStatus)
		})
}

func (s *DurationSuite) TestBlueGreenDuration_05_Retry() {
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
		// Retry after abort
		RetryRollout().
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should have restarted, duration reset
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Nil(s.T(), ro.Status.Duration.FinishedAt, "FinishedAt should be nil after retry")
			assert.Nil(s.T(), ro.Status.Duration.CompletionStatus, "CompletionStatus should be nil after retry")
		}).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
		})
}

func (s *DurationSuite) TestBlueGreenDuration_06_SupersededRollback() {
	initialObservedGen := ""
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-superseded-rollback"}).
		RevisionHistoryLimit(2).
		AutoPromotionEnabled(false).
		SetVersion("1").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			initialObservedGen = ro.Status.ObservedGeneration
		}).
		When().
		// Start a rollout (revision 2)
		UpdateVersion("2").
		WaitForRolloutStatus("Paused").
		// Supersede with rollback to revision 1 (within scaledown window)
		UpdateVersion("1").
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// ObservedGeneration should have changed for the superseded rollback
			assert.Greater(s.T(), ro.Status.ObservedGeneration, initialObservedGen, "ObservedGeneration should increase for superseded rollback")
			// Should be completed with rollback status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusRollbacked, *ro.Status.Duration.CompletionStatus)
		})
}

func (s *DurationSuite) TestBlueGreenDuration_07_FullyPromoted() {
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-fully-promoted"}).
		AutoPromotionEnabled(false).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		// Full promote (skip autopromote delay)
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
		})
}

func (s *DurationSuite) TestBlueGreenDuration_08_RollbackOutsideWindow() {
	initialObservedGen := ""
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-rollback-outside"}).
		RevisionHistoryLimit(2).
		AutoPromotionSeconds(2).
		ScaleDownDelaySeconds(1).
		SetVersion("1").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			initialObservedGen = ro.Status.ObservedGeneration
		}).
		When().
		// Complete a rollout to revision 2
		UpdateVersion("2").
		WaitForRolloutStatus("Healthy").
		// Wait for scaledown window to pass
		Sleep(3 * time.Second).
		// Rollback to revision 1 (outside scaledown window, will create new RS)
		UpdateVersion("1").
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// ObservedGeneration should have changed
			assert.Greater(s.T(), ro.Status.ObservedGeneration, initialObservedGen, "ObservedGeneration should increase for rollback")
			// Should be completed with rollback status
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusRollbacked, *ro.Status.Duration.CompletionStatus)
		})
}

func (s *DurationSuite) TestBlueGreenDuration_09_SupersededRollforward() {
	initialObservedGen := ""
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-superseded-forward"}).
		RevisionHistoryLimit(2).
		AutoPromotionEnabled(false).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			initialObservedGen = ro.Status.ObservedGeneration
		}).
		When().
		// Start a rollout (revision 2)
		UpdateSpec(`{"spec":{"template":{"metadata":{"annotations":{"update":"2"}}}}}`).
		WaitForRolloutStatus("Paused").
		// Supersede with a new rollout (revision 3)
		UpdateSpec(`{"spec":{"template":{"metadata":{"annotations":{"update":"3"}}}}}`).
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// ObservedGeneration should have changed for the superseded rollforward
			assert.Greater(s.T(), ro.Status.ObservedGeneration, initialObservedGen, "ObservedGeneration should increase for superseded rollforward")
			// Should be completed normally
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
		})
}

func (s *DurationSuite) TestBlueGreenDuration_10_Stable() {
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-stable"}).
		AutoPromotionSeconds(2).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Should be completed and stable
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
			// Verify that the rollout is healthy and stable
			assert.Equal(s.T(), ro.Status.CurrentPodHash, ro.Status.StableRS)
		})
}

func (s *DurationSuite) TestBlueGreenDuration_11_ScalingOnly() {
	s.Given().
		RolloutTemplate("@functional/bluegreen-duration-template.yaml", map[string]string{"ROLLOUT_NAME": "bluegreen-duration-scaling-only"}).
		AutoPromotionSeconds(2).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec(`{"spec":{"replicas":2}}`).
		WaitForRolloutReplicas(2).
		Then().
		Assert(func(t *fixtures.Then) {
			ro := t.GetRollout()
			// Scaling only should not affect duration tracking
			// Duration should still be from the last rollout (completed)
			assertDurationFieldsConsistency(s.T(), ro)
			assert.Equal(s.T(), v1alpha1.CompletionStatusPromoted, *ro.Status.Duration.CompletionStatus)
		})
}
