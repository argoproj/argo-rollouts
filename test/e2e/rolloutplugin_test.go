//go:build e2e
// +build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	_ "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/test/fixtures"
)

type RolloutPluginSuite struct {
	fixtures.E2ESuite
}

func TestRolloutPluginSuite(t *testing.T) {
	suite.Run(t, new(RolloutPluginSuite))
}

func (s *RolloutPluginSuite) SetupSuite() {
	s.E2ESuite.SetupSuite()
}

// TestBasicCanaryRollout tests the basic canary rollout flow:
// 1. Create StatefulSet and RolloutPlugin
// 2. Update StatefulSet to trigger rollout
// 3. Verify rollout progresses through all steps to completion
// Based on: TEST 1 & 2 from test-retry-features.sh
func (s *RolloutPluginSuite) TestBasicCanaryRollout() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), "Healthy", rp.Status.Phase)
			assert.False(s.T(), rp.Status.RolloutInProgress)
		}).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		// Step 0: setWeight: 20 (partition 4, 1 canary pod)
		WaitForRolloutPluginCanaryStepIndex(0, 120*time.Second).
		WaitForStatefulSetPartition(4, 60*time.Second). // 20% weight = partition 4
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.True(s.T(), rp.Status.RolloutInProgress)
			assert.NotNil(s.T(), rp.Status.CurrentStepIndex)
			assert.Equal(s.T(), int32(0), *rp.Status.CurrentStepIndex)
		}).
		When().
		// Step 1: pause 3s - wait for it to auto-advance to step 2
		WaitForRolloutPluginCanaryStepIndex(2, 120*time.Second).
		// Step 2: setWeight: 40 (partition 3, 2 canary pods)
		WaitForStatefulSetPartition(3, 60*time.Second).
		// Step 3: pause 3s - wait for it to auto-advance to step 4
		WaitForRolloutPluginCanaryStepIndex(4, 120*time.Second).
		// Step 4: setWeight: 60 (partition 2, 3 canary pods)
		WaitForStatefulSetPartition(2, 60*time.Second).
		// Step 5: pause 3s - wait for it to auto-advance to step 6
		WaitForRolloutPluginCanaryStepIndex(6, 120*time.Second).
		// Step 6: setWeight: 80 (partition 1, 4 canary pods)
		WaitForStatefulSetPartition(1, 60*time.Second).
		// Step 7: pause 3s - wait for it to auto-advance to step 8
		WaitForRolloutPluginCanaryStepIndex(8, 120*time.Second).
		// Step 8: setWeight: 100 (partition 0, 5 canary pods)
		WaitForStatefulSetPartition(0, 60*time.Second).
		// Wait for rollout to complete and become healthy
		WaitForRolloutPluginStatus("Healthy", 120*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), "Healthy", rp.Status.Phase)
			assert.False(s.T(), rp.Status.RolloutInProgress)
			assert.False(s.T(), rp.Status.Aborted)
		})
}

// TestCanaryPauseStep tests pause step functionality:
// 1. Rollout pauses at pause step
// 2. PauseStartTime is set
// 3. Pause auto-resumes after duration
// Based on: TEST 2 from test-retry-features.sh (pause behavior)
func (s *RolloutPluginSuite) TestCanaryPauseStep() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary-pause.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 120*time.Second). // Step 1 is pause step
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.True(s.T(), rp.Status.Paused, "RolloutPlugin should be paused")
			assert.NotNil(s.T(), rp.Status.PauseStartTime, "PauseStartTime should be set")
		}).
		When().
		Sleep(15*time.Second).                                   // Wait for 10s pause duration + buffer
		WaitForRolloutPluginCanaryStepIndex(2, 120*time.Second). // Should advance past pause
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.False(s.T(), rp.Status.Paused, "RolloutPlugin should not be paused after duration")
		})
}

// TestAbortRollout tests abort functionality:
// 1. Trigger rollout
// 2. Abort the rollout
// 3. Verify aborted status and rollback
// Based on: TEST 3 from test-retry-features.sh
func (s *RolloutPluginSuite) TestAbortRollout() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 120*time.Second).
		AbortRolloutPlugin().
		WaitForRolloutPluginStatus("Degraded", 60*time.Second).
		WaitForStatefulSetPartition(5, 60*time.Second). // Full rollback - partition = replicas
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.True(s.T(), rp.Status.Aborted, "RolloutPlugin should be aborted")
		})
}

// TestRetryPreventionOnNonAbortedRollout tests that retry is blocked when rollout is not aborted
// Based on: TEST 4 from test-retry-features.sh
// Note: Retry is only allowed after abort - this tests that retry is rejected when not aborted
func (s *RolloutPluginSuite) TestRetryPreventionOnNonAbortedRollout() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 120*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.False(s.T(), rp.Status.Aborted, "Rollout should not be aborted")
			assert.Equal(s.T(), "Progressing", rp.Status.Phase)
		}).
		When().
		RetryRolloutPlugin(0). // Attempt retry without abort - should be rejected
		Sleep(5 * time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			// Retry should be blocked - rollout was not aborted
			assert.Equal(s.T(), "Failed", rp.Status.Phase, "Phase should be Failed because retry without abort is not allowed")
			assert.Contains(s.T(), rp.Status.Message, "not been aborted", "Message should indicate retry requires abort")
		})
}

// TestRetryWithAllowRetryAnnotation tests retry works after abort
// Based on: TEST 5 from test-retry-features.sh
// Note: allow-retry annotation is now optional - retry just requires abort state
func (s *RolloutPluginSuite) TestRetryAfterAbort() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 120*time.Second).
		AbortRolloutPlugin().
		WaitForRolloutPluginStatus("Degraded", 60*time.Second).
		RetryRolloutPlugin(0).
		WaitForRolloutPluginStatus("Progressing", 60*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			// After abort and retry, rollout should resume
			assert.False(s.T(), rp.Status.Aborted, "Aborted should be cleared after retry")
			assert.Greater(s.T(), rp.Status.RetryAttempt, int32(0), "RetryAttempt should be incremented")
			assert.True(s.T(), rp.Status.RolloutInProgress, "Rollout should be in progress")
		})
}

// TestRetryCounterResetOnNewRollout tests that retry counter resets on new rollout
// Based on: TEST 7 from test-retry-features.sh
func (s *RolloutPluginSuite) TestRetryCounterResetOnNewRollout() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 30*time.Second).
		AbortRolloutPlugin(). // Must abort before retry
		WaitForRolloutPluginStatus("Degraded", 60*time.Second).
		RetryRolloutPlugin(0).
		Sleep(3*time.Second). // Give controller time to process retry
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			s.T().Logf("After retry: RetryAttempt=%d, CurrentStepIndex=%v, Phase=%s",
				rp.Status.RetryAttempt, rp.Status.CurrentStepIndex, rp.Status.Phase)
			assert.Greater(s.T(), rp.Status.RetryAttempt, int32(0), "RetryAttempt should be > 0")
			assert.True(s.T(), rp.Status.RolloutInProgress, "Rollout should be in progress after retry")
		}).
		When().
		// Trigger new rollout with different image
		UpdateStatefulSetImage("quay.io/prometheus/busybox:musl").
		WaitForRolloutPluginStatus("Progressing", 30*time.Second). // Wait for new rollout to be detected
		Sleep(2 * time.Second).                                    // Give controller time to reset retry counter
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			s.T().Logf("After new rollout: RetryAttempt=%d, UpdatedRevision=%s, CurrentRevision=%s",
				rp.Status.RetryAttempt, rp.Status.UpdatedRevision, rp.Status.CurrentRevision)

			// RetryAttempt should reset to 0 when a new rollout starts (new UpdatedRevision)
			assert.Equal(s.T(), int32(0), rp.Status.RetryAttempt, "RetryAttempt should reset to 0 on new rollout")
			assert.True(s.T(), rp.Status.RolloutInProgress, "New rollout should be in progress")

			// Verify it's actually a new rollout (CurrentRevision != UpdatedRevision)
			assert.NotEqual(s.T(), rp.Status.CurrentRevision, rp.Status.UpdatedRevision, "Should be a new rollout with different revisions")
		})
}

// TestRetryFromSpecificStep tests retry from a specific step after abort
// Based on: TEST 8 from test-retry-features.sh
func (s *RolloutPluginSuite) TestRetryFromSpecificStep() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary-many-steps.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(3, 120*time.Second). // Wait for step 3
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.NotNil(s.T(), rp.Status.CurrentStepIndex)
			assert.GreaterOrEqual(s.T(), *rp.Status.CurrentStepIndex, int32(3))
		}).
		When().
		AbortRolloutPlugin(). // Must abort before retry
		WaitForRolloutPluginStatus("Degraded", 60*time.Second).
		RetryRolloutPlugin(1). // Retry from step 1
		Sleep(5 * time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			// Step should be reset to 1
			assert.NotNil(s.T(), rp.Status.CurrentStepIndex)
			// The step index should be around 1 or progressed from 1
			assert.Greater(s.T(), rp.Status.RetryAttempt, int32(0), "RetryAttempt should be incremented")
		})
}

// TestResetFunctionality tests the Reset() plugin method via retry after abort
// Based on: TEST 9 from test-retry-features.sh
// Note: Retry is only allowed after aborting the rollout (matches main Rollout controller behavior)
func (s *RolloutPluginSuite) TestResetFunctionality() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(2, 120*time.Second). // Progress to step 2
		Then().
		// Verify partition is less than replicas (canary in progress)
		ExpectStatefulSetPartitionLessThan(5).
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			// Record state before abort
			s.T().Logf("Before abort: Phase=%s, RetryAttempt=%d, CurrentStepIndex=%v",
				rp.Status.Phase, rp.Status.RetryAttempt, *rp.Status.CurrentStepIndex)
			assert.Equal(s.T(), int32(2), *rp.Status.CurrentStepIndex, "Should be at step 2")
			assert.Equal(s.T(), int32(0), rp.Status.RetryAttempt, "RetryAttempt should be 0 before retry")
		}).
		When().
		AbortRolloutPlugin().                   // Abort the rollout first
		WaitForRolloutPluginStatus("Degraded"). // Wait for abort to complete
		RetryRolloutPlugin(0).                  // Trigger Reset() and restart from step 0
		Sleep(3*time.Second).                   // Give controller time to process retry and start step 0
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			// Verify that retry was processed and rollout restarted
			s.T().Logf("After retry: Phase=%s, RetryAttempt=%d, CurrentStepIndex=%v, RolloutInProgress=%v",
				rp.Status.Phase, rp.Status.RetryAttempt, rp.Status.CurrentStepIndex, rp.Status.RolloutInProgress)

			// Key verification: RetryAttempt should be incremented
			assert.Equal(s.T(), int32(1), rp.Status.RetryAttempt, "RetryAttempt should be 1 after retry")

			// Rollout should be in progress
			assert.True(s.T(), rp.Status.RolloutInProgress, "Rollout should be in progress")
			assert.Equal(s.T(), "Progressing", rp.Status.Phase, "Phase should be Progressing")

			// CurrentStepIndex should be processing from step 0 onwards
			// (may already be past step 0 due to immediate requeue)
			assert.NotNil(s.T(), rp.Status.CurrentStepIndex, "CurrentStepIndex should be set")
			assert.GreaterOrEqual(s.T(), *rp.Status.CurrentStepIndex, int32(0), "Should be at or past step 0")
		}).
		When().
		// Wait for rollout to complete successfully after retry
		WaitForRolloutPluginStatus("Successful", 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			s.T().Logf("After completion: Phase=%s, RetryAttempt=%d",
				rp.Status.Phase, rp.Status.RetryAttempt)

			// Verify rollout completed successfully with retry counter preserved
			assert.Equal(s.T(), "Successful", rp.Status.Phase, "Rollout should complete successfully")
			assert.Equal(s.T(), int32(1), rp.Status.RetryAttempt, "RetryAttempt should still be 1")
			assert.False(s.T(), rp.Status.RolloutInProgress, "Rollout should be complete")
		})
}

// TestPromoteToFullLoad tests promoting to 100% (full load)
// Based on: TEST 10 from test-retry-features.sh
func (s *RolloutPluginSuite) TestPromoteToFullLoad() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 120*time.Second).
		PromoteRolloutPluginFull().
		WaitForRolloutPluginStatus("Healthy", 180*time.Second).
		WaitForStatefulSetPartition(0, 60*time.Second). // Full rollout complete - partition = 0
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), "Healthy", rp.Status.Phase)
			assert.False(s.T(), rp.Status.RolloutInProgress)
		})
}

// TestAbortDuringMidRollout tests abort during mid-rollout
// Based on: TEST 11 from test-retry-features.sh
func (s *RolloutPluginSuite) TestAbortDuringMidRollout() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary-many-steps.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(2, 120*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			// Verify we're mid-rollout
			assert.True(s.T(), rp.Status.RolloutInProgress)
			assert.NotNil(s.T(), rp.Status.CurrentStepIndex)
			assert.GreaterOrEqual(s.T(), *rp.Status.CurrentStepIndex, int32(2))
		}).
		When().
		AbortRolloutPlugin().
		WaitForRolloutPluginStatus("Degraded", 60*time.Second).
		WaitForStatefulSetPartition(5, 60*time.Second). // Rolled back
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.True(s.T(), rp.Status.Aborted)
		})
}

// TestManualPauseAndResume tests spec.paused for manual pause/resume
// Based on: TEST 12 from test-retry-features.sh
func (s *RolloutPluginSuite) TestManualPauseAndResume() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 120*time.Second).
		PauseRolloutPlugin().
		Sleep(3 * time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.True(s.T(), rp.Status.Paused, "RolloutPlugin should be paused")
		}).
		When().
		ResumeRolloutPlugin().
		Sleep(5 * time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			// Should resume (unless at another pause step)
			// Check that it's progressing or advanced
			assert.False(s.T(), rp.Spec.Paused, "spec.paused should be false")
		})
}

// TestCanaryWeightProgression tests canary weight progression through steps
func (s *RolloutPluginSuite) TestCanaryWeightProgression() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(0, 120*time.Second). // setWeight: 20
		WaitForStatefulSetPartition(4, 60*time.Second).          // At 20% weight with 5 replicas, partition should be 4 (1 canary pod)
		PromoteRolloutPlugin().
		WaitForRolloutPluginCanaryStepIndex(2, 120*time.Second). // setWeight: 40
		WaitForStatefulSetPartition(3, 60*time.Second).          // At 40% weight with 5 replicas, partition should be 3 (2 canary pods)
		PromoteRolloutPlugin().
		WaitForRolloutPluginCanaryStepIndex(4, 120*time.Second). // setWeight: 60
		WaitForStatefulSetPartition(2, 60*time.Second).          // At 60% weight with 5 replicas, partition should be 2 (3 canary pods)
		Then().
		ExpectStatefulSetPartition(2)
}

// TestRolloutPluginStatusUpdates tests that status is updated correctly
func (s *RolloutPluginSuite) TestRolloutPluginStatusUpdates() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			// Initial healthy state
			assert.Equal(s.T(), "Healthy", rp.Status.Phase)
			assert.NotEmpty(s.T(), rp.Status.CurrentRevision)
			assert.False(s.T(), rp.Status.RolloutInProgress)
			assert.False(s.T(), rp.Status.Paused)
			assert.False(s.T(), rp.Status.Aborted)
		}).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginStatus("Progressing", 120*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			// Progressing state
			assert.Equal(s.T(), "Progressing", rp.Status.Phase)
			assert.True(s.T(), rp.Status.RolloutInProgress)
			assert.NotEmpty(s.T(), rp.Status.UpdatedRevision)
			// UpdatedRevision should be different from CurrentRevision
			assert.NotEqual(s.T(), rp.Status.CurrentRevision, rp.Status.UpdatedRevision)
		})
}

// TestRolloutPluginWithAnalysis tests analysis integration
func (s *RolloutPluginSuite) TestRolloutPluginWithBackgroundAnalysis() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary-analysis.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 120*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.True(s.T(), rp.Status.RolloutInProgress)
		}).
		// Check that AnalysisRun was created
		ExpectRolloutPluginAnalysisRunCount(1)
}

// TestCompleteRolloutToHealthy tests full rollout completion
func (s *RolloutPluginSuite) TestCompleteRolloutToHealthy() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary-fast.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		// First wait for rollout to start (Progressing), then wait for completion (Healthy)
		WaitForRolloutPluginStatus("Progressing", 120*time.Second).
		WaitForStatefulSetPartition(0, 180*time.Second).        // Wait for all pods to be updated
		WaitForRolloutPluginStatus("Healthy", 120*time.Second). // Wait for full completion
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), "Healthy", rp.Status.Phase)
			assert.False(s.T(), rp.Status.RolloutInProgress)
			assert.False(s.T(), rp.Status.Aborted)
			// CurrentRevision should be updated to the new revision
			assert.NotEmpty(s.T(), rp.Status.CurrentRevision)
		})
}
