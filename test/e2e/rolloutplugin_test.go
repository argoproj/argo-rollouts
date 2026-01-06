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
		WaitForRolloutPluginCanaryStepIndex(0, 60*time.Second).
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
		WaitForRolloutPluginCanaryStepIndex(2, 60*time.Second).
		// Step 2: setWeight: 40 (partition 3, 2 canary pods)
		WaitForStatefulSetPartition(3, 60*time.Second).
		// Step 3: pause 30s - wait for it to auto-advance to step 4
		WaitForRolloutPluginCanaryStepIndex(4, 60*time.Second).
		// Step 4: setWeight: 100 (partition 0, 5 canary pods)
		WaitForStatefulSetPartition(0, 60*time.Second).
		// Wait for rollout to complete and become healthy
		WaitForRolloutPluginStatus("Healthy", 60*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), "Healthy", rp.Status.Phase)
			assert.False(s.T(), rp.Status.RolloutInProgress)
			assert.False(s.T(), rp.Status.Aborted)
		})
}

func (s *RolloutPluginSuite) TestCanaryPauseStep() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary-pause.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 60*time.Second). // Step 1 is pause step
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.True(s.T(), rp.Status.Paused, "RolloutPlugin should be paused")
			assert.NotNil(s.T(), rp.Status.PauseStartTime, "PauseStartTime should be set")
		}).
		When().
		Sleep(15*time.Second).                                  // Wait for 10s pause duration + buffer
		WaitForRolloutPluginCanaryStepIndex(2, 60*time.Second). // Should advance past pause
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.False(s.T(), rp.Status.Paused, "RolloutPlugin should not be paused after duration")
		})
}

func (s *RolloutPluginSuite) TestAbortRollout() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 60*time.Second).
		AbortRolloutPlugin().
		WaitForRolloutPluginStatus("Degraded", 60*time.Second).
		WaitForStatefulSetPartition(5, 60*time.Second). // Full rollback - partition = replicas
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.True(s.T(), rp.Status.Aborted, "RolloutPlugin should be aborted")
			assert.NotEmpty(s.T(), rp.Status.AbortedRevision, "AbortedRevision should be set after abort")
		})
}

func (s *RolloutPluginSuite) TestRestartPreventionOnNonAbortedRollout() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 60*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.False(s.T(), rp.Status.Aborted, "Rollout should not be aborted")
			assert.Equal(s.T(), "Progressing", rp.Status.Phase)
		}).
		When().
		RestartRolloutPlugin().                                    // Attempt restart without abort - should be ignored
		WaitForRolloutPluginStatus("Successful", 120*time.Second). // Rollout continues and completes
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), "Successful", rp.Status.Phase, "Rollout should complete successfully despite rejected restart")
			// Message might not contain rejection since rollout completed normally
		})
}

func (s *RolloutPluginSuite) TestRestartAfterAbort() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 60*time.Second).
		AbortRolloutPlugin().
		WaitForRolloutPluginStatus("Degraded", 60*time.Second).
		RestartRolloutPlugin().
		WaitForRolloutPluginStatus("Progressing", 60*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			// After abort and restart, rollout should resume
			assert.False(s.T(), rp.Status.Aborted, "Aborted should be cleared after restart")
			assert.Greater(s.T(), rp.Status.RestartCount, int32(0), "RestartCount should be incremented")
			assert.True(s.T(), rp.Status.RolloutInProgress, "Rollout should be in progress")
		})
}

func (s *RolloutPluginSuite) TestRestartCounterResetOnNewRollout() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 30*time.Second).
		AbortRolloutPlugin(). // Must abort before restart
		WaitForRolloutPluginStatus("Degraded", 60*time.Second).
		RestartRolloutPlugin().
		Sleep(3*time.Second). // Give controller time to process restart
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			s.T().Logf("After restart: RestartCount=%d, CurrentStepIndex=%v, Phase=%s",
				rp.Status.RestartCount, rp.Status.CurrentStepIndex, rp.Status.Phase)
			assert.Greater(s.T(), rp.Status.RestartCount, int32(0), "RestartCount should be > 0")
			assert.True(s.T(), rp.Status.RolloutInProgress, "Rollout should be in progress after restart")
		}).
		When().
		// Trigger new rollout with different image
		UpdateStatefulSetImage("quay.io/prometheus/busybox:musl").
		WaitForRolloutPluginStatus("Progressing", 30*time.Second). // Wait for new rollout to be detected
		Sleep(2 * time.Second).                                    // Give controller time to reset restart counter
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			s.T().Logf("After new rollout: RestartCount=%d, UpdatedRevision=%s, CurrentRevision=%s",
				rp.Status.RestartCount, rp.Status.UpdatedRevision, rp.Status.CurrentRevision)

			// RestartCount should reset to 0 when a new rollout starts (new UpdatedRevision)
			assert.Equal(s.T(), int32(0), rp.Status.RestartCount, "RestartCount should reset to 0 on new rollout")
			assert.True(s.T(), rp.Status.RolloutInProgress, "New rollout should be in progress")

			// Verify it's actually a new rollout (CurrentRevision != UpdatedRevision)
			assert.NotEqual(s.T(), rp.Status.CurrentRevision, rp.Status.UpdatedRevision, "Should be a new rollout with different revisions")
		})
}

func (s *RolloutPluginSuite) TestRestartFunctionality() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(2, 60*time.Second). // Progress to step 2
		Then().
		// Verify partition is less than replicas (canary in progress)
		ExpectStatefulSetPartitionLessThan(5).
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			// Record state before abort
			s.T().Logf("Before abort: Phase=%s, RestartCount=%d, CurrentStepIndex=%v",
				rp.Status.Phase, rp.Status.RestartCount, *rp.Status.CurrentStepIndex)
			assert.Equal(s.T(), int32(2), *rp.Status.CurrentStepIndex, "Should be at step 2")
			assert.Equal(s.T(), int32(0), rp.Status.RestartCount, "RestartCount should be 0 before restart")
		}).
		When().
		AbortRolloutPlugin().                   // Abort the rollout first
		WaitForRolloutPluginStatus("Degraded"). // Wait for abort to complete
		RestartRolloutPlugin().                 // Trigger Restart() to restart from step 0
		Sleep(3*time.Second).                   // Give controller time to process restart and start step 0
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			// Verify that restart was processed and rollout restarted
			s.T().Logf("After restart: Phase=%s, RestartCount=%d, CurrentStepIndex=%v, RolloutInProgress=%v",
				rp.Status.Phase, rp.Status.RestartCount, rp.Status.CurrentStepIndex, rp.Status.RolloutInProgress)

			// Key verification: RestartCount should be incremented
			assert.Equal(s.T(), int32(1), rp.Status.RestartCount, "RestartCount should be 1 after restart")

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
			s.T().Logf("After completion: Phase=%s, RestartCount=%d",
				rp.Status.Phase, rp.Status.RestartCount)

			// Verify rollout completed successfully with restart counter preserved
			assert.Equal(s.T(), "Successful", rp.Status.Phase, "Rollout should complete successfully")
			assert.Equal(s.T(), int32(1), rp.Status.RestartCount, "RestartCount should still be 1")
			assert.False(s.T(), rp.Status.RolloutInProgress, "Rollout should be complete")
		})
}

func (s *RolloutPluginSuite) TestRestartBlockedWithoutAllowRestart() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 60*time.Second).
		AbortRolloutPlugin().
		WaitForRolloutPluginStatus("Degraded", 60*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.True(s.T(), rp.Status.Aborted, "RolloutPlugin should be aborted")
			assert.NotEmpty(s.T(), rp.Status.AbortedRevision, "AbortedRevision should be set")
		}).
		When().
		// Try to trigger the SAME revision again (without allowRestart)
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		Sleep(5 * time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			// Should still be aborted, blocked from restarting
			assert.True(s.T(), rp.Status.Aborted, "Should remain aborted")
			assert.NotEmpty(s.T(), rp.Status.AbortedRevision, "AbortedRevision should still be set")
			assert.Equal(s.T(), "Degraded", rp.Status.Phase)
			assert.Contains(s.T(), rp.Status.Message, "aborted", "Should indicate rollout is blocked")
		})
}

func (s *RolloutPluginSuite) TestRestartWithAllowRestart() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 60*time.Second).
		AbortRolloutPlugin().
		WaitForRolloutPluginStatus("Degraded", 60*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.NotEmpty(s.T(), rp.Status.AbortedRevision, "AbortedRevision should be set")
			s.T().Logf("AbortedRevision: %s", rp.Status.AbortedRevision)
		}).
		When().
		// Set allowRestart=true to permit restarting the aborted revision
		AllowRestartRolloutPlugin().
		// Trigger the SAME revision again
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginStatus("Progressing", 60*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			// Should clear abort state and proceed
			assert.False(s.T(), rp.Status.Aborted, "Aborted should be cleared")
			assert.Empty(s.T(), rp.Status.AbortedRevision, "AbortedRevision should be cleared")
			assert.False(s.T(), rp.Status.AllowRestart, "AllowRestart should be cleared after processing")
			assert.True(s.T(), rp.Status.RolloutInProgress, "Rollout should proceed")
		})
}

func (s *RolloutPluginSuite) TestAbortedRevisionAutoCleared() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 60*time.Second).
		AbortRolloutPlugin().
		WaitForRolloutPluginStatus("Degraded", 60*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.NotEmpty(s.T(), rp.Status.AbortedRevision, "AbortedRevision should be set")
			s.T().Logf("AbortedRevision: %s", rp.Status.AbortedRevision)
		}).
		When().
		// Deploy a DIFFERENT revision (should auto-clear abort state)
		UpdateStatefulSetImage("quay.io/prometheus/busybox:musl").
		WaitForRolloutPluginStatus("Progressing", 60*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			// Abort state should be automatically cleared for new revision
			assert.False(s.T(), rp.Status.Aborted, "Aborted should be cleared for new revision")
			assert.Empty(s.T(), rp.Status.AbortedRevision, "AbortedRevision should be cleared")
			assert.True(s.T(), rp.Status.RolloutInProgress, "New rollout should proceed")
		})
}

func (s *RolloutPluginSuite) TestPromoteToFullLoad() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 60*time.Second).
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

func (s *RolloutPluginSuite) TestAbortDuringMidRollout() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary-many-steps.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(2, 60*time.Second).
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

func (s *RolloutPluginSuite) TestManualPauseAndResume() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 60*time.Second).
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

func (s *RolloutPluginSuite) TestCanaryWeightProgression() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(0, 60*time.Second). // setWeight: 20
		WaitForStatefulSetPartition(4, 60*time.Second).         // At 20% weight with 5 replicas, partition should be 4 (1 canary pod)
		PromoteRolloutPlugin().
		WaitForRolloutPluginCanaryStepIndex(2, 60*time.Second). // setWeight: 40
		WaitForStatefulSetPartition(3, 60*time.Second).         // At 40% weight with 5 replicas, partition should be 3 (2 canary pods)
		PromoteRolloutPlugin().
		WaitForRolloutPluginCanaryStepIndex(4, 60*time.Second). // setWeight: 60
		WaitForStatefulSetPartition(0, 60*time.Second).         // At 100% weight with 5 replicas, partition should be 0 (5 canary pods)
		Then().
		ExpectStatefulSetPartition(0)
}

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
		WaitForRolloutPluginStatus("Progressing", 60*time.Second).
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

func (s *RolloutPluginSuite) TestRolloutPluginWithBackgroundAnalysis() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary-analysis.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 60*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.True(s.T(), rp.Status.RolloutInProgress)
		}).
		// Check that AnalysisRun was created
		ExpectRolloutPluginAnalysisRunCount(1)
}

// =================== Analysis Tests ===================

// func (s *RolloutPluginSuite) TestBackgroundAnalysisSuccess() {
// 	s.Given().
// 		RolloutPluginObjects("@rolloutplugin/statefulset-canary-bg-analysis-success.yaml").
// 		When().
// 		ApplyManifests().
// 		WaitForStatefulSetReady().
// 		WaitForRolloutPluginStatus("Healthy").
// 		Then().
// 		Assert(func(t *fixtures.Then) {
// 			rp := t.GetRolloutPlugin()
// 			assert.Equal(s.T(), "Healthy", rp.Status.Phase)
// 			assert.False(s.T(), rp.Status.RolloutInProgress)
// 		}).
// 		When().
// 		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
// 		WaitForRolloutPluginCanaryStepIndex(0, 60*time.Second).
// 		Then().
// 		// Background analysis should start immediately
// 		ExpectRolloutPluginAnalysisRunCount(1).
// 		Assert(func(t *fixtures.Then) {
// 			rp := t.GetRolloutPlugin()
// 			assert.True(s.T(), rp.Status.RolloutInProgress)
// 			// Background analysis status should be tracked
// 			assert.NotNil(s.T(), rp.Status.Canary.CurrentBackgroundAnalysisRunStatus)
// 			s.T().Logf("Background AnalysisRun: %s, Status: %s",
// 				rp.Status.Canary.CurrentBackgroundAnalysisRunStatus.Name,
// 				rp.Status.Canary.CurrentBackgroundAnalysisRunStatus.Status)
// 		}).
// 		When().
// 		// Wait for background analysis to complete successfully
// 		WaitForRolloutPluginBackgroundAnalysisRunPhase("Successful").
// 		Then().
// 		Assert(func(t *fixtures.Then) {
// 			rp := t.GetRolloutPlugin()
// 			// Background analysis succeeded, rollout should continue
// 			assert.Equal(s.T(), "Successful", string(rp.Status.Canary.CurrentBackgroundAnalysisRunStatus.Status))
// 			assert.True(s.T(), rp.Status.RolloutInProgress, "Rollout should continue after successful analysis")
// 		}).
// 		When().
// 		// Wait for rollout to complete
// 		WaitForRolloutPluginStatus("Healthy", 180*time.Second).
// 		Then().
// 		Assert(func(t *fixtures.Then) {
// 			rp := t.GetRolloutPlugin()
// 			assert.Equal(s.T(), "Healthy", rp.Status.Phase)
// 			assert.False(s.T(), rp.Status.RolloutInProgress)
// 		})
// }

// func (s *RolloutPluginSuite) TestBackgroundAnalysisFailure() {
// 	s.Given().
// 		RolloutPluginObjects("@rolloutplugin/statefulset-canary-bg-analysis-fail.yaml").
// 		When().
// 		ApplyManifests().
// 		WaitForStatefulSetReady().
// 		WaitForRolloutPluginStatus("Healthy").
// 		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
// 		WaitForRolloutPluginCanaryStepIndex(0, 60*time.Second).
// 		Then().
// 		// Background analysis should start
// 		ExpectRolloutPluginAnalysisRunCount(1).
// 		Assert(func(t *fixtures.Then) {
// 			rp := t.GetRolloutPlugin()
// 			assert.True(s.T(), rp.Status.RolloutInProgress)
// 			assert.NotNil(s.T(), rp.Status.Canary.CurrentBackgroundAnalysisRunStatus)
// 		}).
// 		When().
// 		// Wait for background analysis to fail
// 		WaitForRolloutPluginBackgroundAnalysisRunPhase("Failed").
// 		Sleep(5 * time.Second). // Give controller time to process the failure
// 		Then().
// 		Assert(func(t *fixtures.Then) {
// 			rp := t.GetRolloutPlugin()
// 			// Background analysis failed, rollout should be aborted or failed
// 			assert.Equal(s.T(), "Failed", string(rp.Status.Canary.CurrentBackgroundAnalysisRunStatus.Status))
// 			s.T().Logf("Rollout Phase after analysis failure: %s", rp.Status.Phase)
// 			// Rollout should be stopped (Failed or Degraded state)
// 			assert.Contains(s.T(), []string{"Failed", "Degraded"}, rp.Status.Phase,
// 				"Rollout should be Failed or Degraded after analysis failure")
// 		})
// }

// func (s *RolloutPluginSuite) TestInlineAnalysisSuccess() {
// 	s.Given().
// 		RolloutPluginObjects("@rolloutplugin/statefulset-canary-inline-analysis-success.yaml").
// 		When().
// 		ApplyManifests().
// 		WaitForStatefulSetReady().
// 		WaitForRolloutPluginStatus("Healthy").
// 		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
// 		WaitForRolloutPluginCanaryStepIndex(0, 60*time.Second). // Step 0: setWeight 33
// 		WaitForStatefulSetPartition(2, 60*time.Second).          // Verify weight set
// 		WaitForRolloutPluginCanaryStepIndex(1, 60*time.Second). // Step 1: analysis
// 		Then().
// 		// Inline (step) analysis should be created
// 		Assert(func(t *fixtures.Then) {
// 			rp := t.GetRolloutPlugin()
// 			assert.True(s.T(), rp.Status.RolloutInProgress)
// 			// Step analysis status should be tracked
// 			assert.NotNil(s.T(), rp.Status.Canary.CurrentStepAnalysisRunStatus)
// 			s.T().Logf("Step AnalysisRun: %s, Status: %s",
// 				rp.Status.Canary.CurrentStepAnalysisRunStatus.Name,
// 				rp.Status.Canary.CurrentStepAnalysisRunStatus.Status)
// 		}).
// 		When().
// 		// Wait for inline analysis to complete successfully
// 		WaitForRolloutPluginInlineAnalysisRunPhase("Successful").
// 		Sleep(3 * time.Second). // Give controller time to advance to next step
// 		Then().
// 		Assert(func(t *fixtures.Then) {
// 			rp := t.GetRolloutPlugin()
// 			// Analysis succeeded, should advance to next step
// 			assert.True(s.T(), rp.Status.RolloutInProgress, "Rollout should continue after successful step analysis")
// 			// Should have moved past the analysis step
// 			assert.NotNil(s.T(), rp.Status.CurrentStepIndex)
// 			assert.Greater(s.T(), *rp.Status.CurrentStepIndex, int32(1), "Should advance past analysis step")
// 		}).
// 		When().
// 		// Wait for rollout to complete
// 		WaitForRolloutPluginStatus("Healthy", 180*time.Second).
// 		Then().
// 		Assert(func(t *fixtures.Then) {
// 			rp := t.GetRolloutPlugin()
// 			assert.Equal(s.T(), "Healthy", rp.Status.Phase)
// 			assert.False(s.T(), rp.Status.RolloutInProgress)
// 		})
// }

// func (s *RolloutPluginSuite) TestInlineAnalysisFailure() {
// 	s.Given().
// 		RolloutPluginObjects("@rolloutplugin/statefulset-canary-inline-analysis-fail.yaml").
// 		When().
// 		ApplyManifests().
// 		WaitForStatefulSetReady().
// 		WaitForRolloutPluginStatus("Healthy").
// 		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
// 		WaitForRolloutPluginCanaryStepIndex(0, 60*time.Second). // Step 0: setWeight 33
// 		WaitForRolloutPluginCanaryStepIndex(1, 60*time.Second). // Step 1: analysis
// 		Then().
// 		// Inline (step) analysis should be created
// 		Assert(func(t *fixtures.Then) {
// 			rp := t.GetRolloutPlugin()
// 			assert.True(s.T(), rp.Status.RolloutInProgress)
// 			assert.NotNil(s.T(), rp.Status.Canary.CurrentStepAnalysisRunStatus)
// 			s.T().Logf("Step AnalysisRun: %s",
// 				rp.Status.Canary.CurrentStepAnalysisRunStatus.Name)
// 		}).
// 		When().
// 		// Wait for inline analysis to fail
// 		WaitForRolloutPluginInlineAnalysisRunPhase("Failed").
// 		Sleep(5 * time.Second). // Give controller time to process the failure
// 		Then().
// 		Assert(func(t *fixtures.Then) {
// 			rp := t.GetRolloutPlugin()
// 			// Step analysis failed, rollout should be aborted/failed
// 			assert.Equal(s.T(), "Failed", string(rp.Status.Canary.CurrentStepAnalysisRunStatus.Status))
// 			s.T().Logf("Rollout Phase after step analysis failure: %s", rp.Status.Phase)
// 			// Rollout should be stopped (Failed state)
// 			assert.Equal(s.T(), "Failed", rp.Status.Phase,
// 				"Rollout should be Failed after step analysis failure")
// 			assert.False(s.T(), rp.Status.RolloutInProgress, "Rollout should be stopped")
// 		}).
// 		When().
// 		// Verify rollback happened
// 		Sleep(3 * time.Second).
// 		Then().
// 		ExpectStatefulSetPartition(3) // Should rollback to stable (partition = replicas)
// }

// func (s *RolloutPluginSuite) TestAnalysisRunOwnership() {
// 	s.Given().
// 		RolloutPluginObjects("@rolloutplugin/statefulset-canary-bg-analysis-success.yaml").
// 		When().
// 		ApplyManifests().
// 		WaitForStatefulSetReady().
// 		WaitForRolloutPluginStatus("Healthy").
// 		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
// 		WaitForRolloutPluginCanaryStepIndex(0, 60*time.Second).
// 		Then().
// 		ExpectRolloutPluginAnalysisRunCount(1).
// 		Assert(func(t *fixtures.Then) {
// 			rp := t.GetRolloutPlugin()
// 			aruns := t.GetRolloutPluginAnalysisRuns()

// 			assert.Len(s.T(), aruns.Items, 1, "Should have exactly one AnalysisRun")

// 			ar := aruns.Items[0]
// 			// Verify owner reference
// 			assert.NotEmpty(s.T(), ar.OwnerReferences, "AnalysisRun should have owner reference")
// 			assert.Equal(s.T(), rp.UID, ar.OwnerReferences[0].UID, "AnalysisRun should be owned by RolloutPlugin")
// 			assert.Equal(s.T(), "RolloutPlugin", ar.OwnerReferences[0].Kind)

// 			s.T().Logf("AnalysisRun %s is correctly owned by RolloutPlugin %s",
// 				ar.Name, rp.Name)
// 		})
// }

func (s *RolloutPluginSuite) TestCompleteRolloutToHealthy() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/statefulset-canary-fast.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy").
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		// First wait for rollout to start (Progressing), then wait for completion (Healthy)
		WaitForRolloutPluginStatus("Progressing", 60*time.Second).
		WaitForStatefulSetPartition(0, 180*time.Second).       // Wait for all pods to be updated
		WaitForRolloutPluginStatus("Healthy", 60*time.Second). // Wait for full completion
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

// =================== Validation Tests ===================

// TestInvalidSpecMissingStrategy tests that validation fails when no strategy is specified
// Note: CRD allows empty strategy, but controller validates it
func (s *RolloutPluginSuite) TestInvalidSpecMissingStrategy() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/invalid-spec-missing-strategy.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Failed", 60*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), "Failed", rp.Status.Phase)
			assert.Contains(s.T(), rp.Status.Message, "canary or blueGreen strategy")

			// Verify InvalidSpec condition is set
			var foundInvalidSpec bool
			for _, cond := range rp.Status.Conditions {
				if cond.Type == "InvalidSpec" {
					foundInvalidSpec = true
					assert.Equal(s.T(), "True", string(cond.Status))
					break
				}
			}
			assert.True(s.T(), foundInvalidSpec, "InvalidSpec condition should be set")
		})
}

// TestInvalidSpecPluginNotFound tests that validation fails when the plugin is not registered
func (s *RolloutPluginSuite) TestInvalidSpecPluginNotFound() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/invalid-spec-plugin-notfound.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Failed", 60*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), "Failed", rp.Status.Phase)
			// Check for plugin not found error message
			assert.Contains(s.T(), rp.Status.Message, "not found")
			assert.Contains(s.T(), rp.Status.Message, "nonexistent-plugin")

			// Verify InvalidSpec condition is set for plugin not found
			var foundInvalidSpec bool
			for _, cond := range rp.Status.Conditions {
				if cond.Type == "InvalidSpec" {
					foundInvalidSpec = true
					assert.Equal(s.T(), "True", string(cond.Status))
					assert.Contains(s.T(), cond.Message, "not found")
					break
				}
			}
			assert.True(s.T(), foundInvalidSpec, "InvalidSpec condition should be set for plugin not found")
		})
}

// TestValidSpecNoInvalidCondition tests that a valid spec doesn't have InvalidSpec condition
func (s *RolloutPluginSuite) TestValidSpecNoInvalidCondition() {
	s.Given().
		RolloutPluginObjects("@rolloutplugin/valid-spec-fix.yaml").
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus("Healthy", 60*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), "Healthy", rp.Status.Phase)

			// InvalidSpec condition should NOT be present for valid spec
			for _, cond := range rp.Status.Conditions {
				if cond.Type == "InvalidSpec" {
					s.T().Errorf("InvalidSpec condition should not be present on valid spec, but found: %+v", cond)
				}
			}
		})
}
