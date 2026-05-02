//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"

	rov1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/test/fixtures"
	"github.com/argoproj/argo-rollouts/utils/conditions"
)

// rpCanary2Replicas is the base YAML for a 2-replica RolloutPlugin+StatefulSet canary setup.
// Resource names and labels are overridden by RolloutPluginObjects to be unique per test.
// Steps are set dynamically via SetRolloutPluginSteps or the HealthyRolloutPlugin variadic.
const rpCanary2Replicas = `
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: rp-canary
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: rp-canary
  plugin:
    name: statefulset
  strategy:
    canary: {}
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: rp-canary
spec:
  serviceName: rp-canary
  replicas: 2
  selector:
    matchLabels:
      app: rp-canary
  template:
    metadata:
      labels:
        app: rp-canary
    spec:
      containers:
      - name: busybox
        image: quay.io/prometheus/busybox:latest
        command: ["/bin/sh", "-c", "while true; do sleep 30; done"]
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`

// rpCanary4Replicas is the base YAML for a 4-replica RolloutPlugin+StatefulSet canary setup.
const rpCanary4Replicas = `
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: rp-canary
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: rp-canary
  plugin:
    name: statefulset
  strategy:
    canary: {}
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: rp-canary
spec:
  serviceName: rp-canary
  replicas: 4
  selector:
    matchLabels:
      app: rp-canary
  template:
    metadata:
      labels:
        app: rp-canary
    spec:
      containers:
      - name: busybox
        image: quay.io/prometheus/busybox:latest
        command: ["/bin/sh", "-c", "while true; do sleep 30; done"]
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`

type RolloutPluginSuite struct {
	fixtures.E2ESuite
}

func TestRolloutPluginSuite(t *testing.T) {
	suite.Run(t, new(RolloutPluginSuite))
}

func (s *RolloutPluginSuite) SetupSuite() {
	s.E2ESuite.SetupSuite()
	// Shared analysis templates for analysis tests
	s.ApplyManifests(`
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: rp-web-background
spec:
  args:
  - name: url-val
    value: "https://kubernetes.default.svc/version"
  metrics:
  - name: web
    interval: 5s
    successCondition: result.major == '1'
    provider:
      web:
        url: "{{args.url-val}}"
        insecure: true
`)
	s.ApplyManifests(`
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: rp-web-background-fail
spec:
  args:
  - name: url-val
    value: "https://kubernetes.default.svc/version"
  metrics:
  - name: web
    interval: 5s
    failureLimit: 0
    successCondition: result.major == '999'
    provider:
      web:
        url: "{{args.url-val}}"
        insecure: true
`)
	s.ApplyManifests(`
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: rp-sleep-job
spec:
  args:
  - name: duration
    value: "0"
  - name: exit-code
    value: "0"
  metrics:
  - name: sleep-job
    count: 1
    provider:
      job:
        spec:
          template:
            spec:
              containers:
              - name: sleep-job
                image: nginx:1.19-alpine
                command: [sh, -c, -x]
                args:
                - sleep {{args.duration}}; exit {{args.exit-code}}
              restartPolicy: Never
          backoffLimit: 0
`)
	s.ApplyManifests(`
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: rp-web-background-inconclusive
spec:
  args:
  - name: url-val
    value: "https://kubernetes.default.svc/version"
  metrics:
  - name: web
    count: 1
    successCondition: result.major == '999'
    failureCondition: result.major == '998'
    provider:
      web:
        url: "{{args.url-val}}"
        insecure: true
`)
}

// Basic Canary Lifecycle Tests

func (s *RolloutPluginSuite) TestRolloutPluginBasicCanary() {
	s.Given().
		RolloutPluginObjects(rpCanary2Replicas).
		SetRolloutPluginSteps(`
- setWeight: 50
- pause: {}`).
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy).
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		Then().
		ExpectRolloutPluginStatus(rov1.RolloutPluginPhasePaused).
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), rov1.RolloutPluginPhasePaused, rp.Status.Phase)
			assert.NotNil(s.T(), rp.Status.CurrentStepIndex)
			assert.Equal(s.T(), int32(1), *rp.Status.CurrentStepIndex)
		}).
		When().
		PromoteRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), rov1.RolloutPluginPhaseHealthy, rp.Status.Phase)
			assert.Equal(s.T(), rp.Status.CurrentRevision, rp.Status.UpdatedRevision)
		})
}

// TestRolloutPluginCanaryNoSteps tests a canary rollout with no steps (immediate promotion).
func (s *RolloutPluginSuite) TestRolloutPluginCanaryNoSteps() {
	s.Given().
		RolloutPluginObjects(rpCanary2Replicas).
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy).
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), rov1.RolloutPluginPhaseHealthy, rp.Status.Phase)
			assert.Equal(s.T(), rp.Status.CurrentRevision, rp.Status.UpdatedRevision)
		})
}

func (s *RolloutPluginSuite) TestRolloutPluginCanaryMultipleSteps() {
	s.Given().
		RolloutPluginObjects(rpCanary4Replicas).
		SetRolloutPluginSteps(`
- setWeight: 25
- pause: {}
- setWeight: 50
- pause: {}
- setWeight: 75
- pause: {}`).
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy).
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 300*time.Second).
		Then().
		ExpectRolloutPluginStatus(rov1.RolloutPluginPhasePaused).
		When().
		PromoteRolloutPlugin().
		WaitForRolloutPluginCanaryStepIndex(3, 300*time.Second).
		Then().
		ExpectRolloutPluginStatus(rov1.RolloutPluginPhasePaused).
		When().
		PromoteRolloutPlugin().
		WaitForRolloutPluginCanaryStepIndex(5, 300*time.Second).
		Then().
		ExpectRolloutPluginStatus(rov1.RolloutPluginPhasePaused).
		When().
		PromoteRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 300*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), rov1.RolloutPluginPhaseHealthy, rp.Status.Phase)
		})
}

// TestRolloutPluginInitialDeploy tests the initial deployment (no prior revision).
func (s *RolloutPluginSuite) TestRolloutPluginInitialDeploy() {
	s.Given().
		RolloutPluginObjects(rpCanary2Replicas).
		SetRolloutPluginSteps(`
- setWeight: 50
- pause: {}`).
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), rov1.RolloutPluginPhaseHealthy, rp.Status.Phase)
			assert.NotEmpty(s.T(), rp.Status.CurrentRevision)
			assert.Equal(s.T(), rp.Status.CurrentRevision, rp.Status.UpdatedRevision)
		})
}

// Pause and Resume Tests

// TestRolloutPluginManualPause tests manual pause via spec.paused.
func (s *RolloutPluginSuite) TestRolloutPluginManualPause() {
	s.Given().
		HealthyRolloutPlugin(rpCanary4Replicas, `
- setWeight: 25
- pause: {duration: 5s}
- setWeight: 50
- setWeight: 75
- setWeight: 100`).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 300*time.Second).
		PauseRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhasePaused, 60*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.True(s.T(), rp.Spec.Paused, "spec.paused should be true")
			assert.Equal(s.T(), rov1.RolloutPluginPhasePaused, rp.Status.Phase)
		}).
		When().
		ResumeRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 300*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), rov1.RolloutPluginPhaseHealthy, rp.Status.Phase)
			assert.False(s.T(), rp.Spec.Paused)
		})
}

// TestRolloutPluginStepPauseAndPromote tests pause via canary step and promote.
func (s *RolloutPluginSuite) TestRolloutPluginStepPauseAndPromote() {
	s.Given().
		HealthyRolloutPlugin(rpCanary4Replicas, `
- setWeight: 25
- pause: {}
- setWeight: 75
- pause: {}`).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 300*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.True(s.T(), rp.Status.ControllerPause)
			assert.True(s.T(), len(rp.Status.PauseConditions) > 0)
		}).
		When().
		PromoteRolloutPlugin().
		WaitForRolloutPluginCanaryStepIndex(3, 300*time.Second).
		Then().
		ExpectRolloutPluginStatus(rov1.RolloutPluginPhasePaused).
		When().
		PromoteRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 300*time.Second)
}

// TestRolloutPluginPauseResumeDuringRollout tests pause/resume mid-rollout.
func (s *RolloutPluginSuite) TestRolloutPluginPauseResumeDuringRollout() {
	s.Given().
		HealthyRolloutPlugin(rpCanary4Replicas, `
- setWeight: 25
- pause: {}
- setWeight: 50
- pause: {duration: 5s}
- setWeight: 100`).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 300*time.Second).
		// Manual pause while at a canary step pause
		PauseRolloutPlugin().
		Then().
		ExpectRolloutPluginStatus(rov1.RolloutPluginPhasePaused).
		When().
		// Resume both manual and step pause
		ResumeRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 300*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), rov1.RolloutPluginPhaseHealthy, rp.Status.Phase)
		})
}

// Abort Tests

// TestRolloutPluginAbort tests aborting a rollout mid-canary.
func (s *RolloutPluginSuite) TestRolloutPluginAbort() {
	s.Given().
		HealthyRolloutPlugin(rpCanary2Replicas, `
- setWeight: 50
- pause: {}`).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		AbortRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseDegraded, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.True(s.T(), rp.Status.Aborted)
			assert.NotEmpty(s.T(), rp.Status.AbortedRevision)
			assert.Equal(s.T(), rov1.RolloutPluginPhaseDegraded, rp.Status.Phase)
			assert.NotNil(s.T(), rp.Status.AbortedAt)
		})
}

// TestRolloutPluginAbortBeforePause tests aborting before reaching a pause step.
func (s *RolloutPluginSuite) TestRolloutPluginAbortBeforePause() {
	s.Given().
		HealthyRolloutPlugin(rpCanary4Replicas, `
- setWeight: 25
- pause: {duration: 300s}
- setWeight: 50
- pause: {}`).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 300*time.Second).
		AbortRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseDegraded, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.True(s.T(), rp.Status.Aborted)
			assert.Equal(s.T(), rov1.RolloutPluginPhaseDegraded, rp.Status.Phase)
		})
}

// TestRolloutPluginAbortIdempotent tests that aborting an already-aborted rollout is idempotent.
func (s *RolloutPluginSuite) TestRolloutPluginAbortIdempotent() {
	var firstAbortedAt time.Time
	s.Given().
		HealthyRolloutPlugin(rpCanary2Replicas, `
- setWeight: 50
- pause: {}`).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		AbortRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseDegraded, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.NotNil(s.T(), rp.Status.AbortedAt)
			firstAbortedAt = rp.Status.AbortedAt.Time
		}).
		When().
		// Second abort should be a no-op
		AbortRolloutPlugin().
		Sleep(5 * time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.True(s.T(), rp.Status.Aborted)
			assert.Equal(s.T(), rov1.RolloutPluginPhaseDegraded, rp.Status.Phase)
			assert.NotNil(s.T(), rp.Status.AbortedAt)
			assert.Equal(s.T(), firstAbortedAt, rp.Status.AbortedAt.Time, "AbortedAt timestamp should not change on second abort")
		})
}

// Restart Tests

// TestRolloutPluginRestartAfterAbort tests restarting a rollout after abort.
func (s *RolloutPluginSuite) TestRolloutPluginRestartAfterAbort() {
	s.Given().
		HealthyRolloutPlugin(rpCanary2Replicas, `
- setWeight: 50
- pause: {}`).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		AbortRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseDegraded, 180*time.Second).
		RestartRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseProgressing, 180*time.Second).
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), int32(1), rp.Status.RestartCount)
			assert.NotNil(s.T(), rp.Status.RestartedAt)
			assert.False(s.T(), rp.Status.Aborted)
		}).
		When().
		PromoteRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), rov1.RolloutPluginPhaseHealthy, rp.Status.Phase)
			assert.Equal(s.T(), int32(1), rp.Status.RestartCount)
		})
}

// TestRolloutPluginRestartRejectedWithoutAbort tests that restart is rejected when not aborted.
func (s *RolloutPluginSuite) TestRolloutPluginRestartRejectedWithoutAbort() {
	s.Given().
		HealthyRolloutPlugin(rpCanary2Replicas, `
- setWeight: 50
- pause: {}`).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		// Restart without abort should be rejected
		RestartRolloutPlugin().
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.False(s.T(), rp.Status.Aborted, "Rollout should not be aborted")
			assert.Equal(s.T(), rov1.RolloutPluginPhasePaused, rp.Status.Phase)
			assert.Equal(s.T(), int32(0), rp.Status.RestartCount, "RestartCount should remain 0")
		}).
		When().
		PromoteRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 180*time.Second)
}

// TestRolloutPluginMultipleRestarts tests multiple restart cycles.
func (s *RolloutPluginSuite) TestRolloutPluginMultipleRestarts() {
	s.Given().
		HealthyRolloutPlugin(rpCanary2Replicas, `
- setWeight: 50
- pause: {}`).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		// First abort + restart
		AbortRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseDegraded, 180*time.Second).
		RestartRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseProgressing, 180*time.Second).
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), int32(1), rp.Status.RestartCount)
		}).
		When().
		// Second abort + restart
		AbortRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseDegraded, 180*time.Second).
		RestartRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseProgressing, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), int32(2), rp.Status.RestartCount)
		}).
		When().
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		PromoteRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 180*time.Second)
}

// Promote Full Tests

// TestRolloutPluginPromoteFull tests promoting a rollout to full (skip remaining steps).
func (s *RolloutPluginSuite) TestRolloutPluginPromoteFull() {
	s.Given().
		HealthyRolloutPlugin(rpCanary4Replicas, `
- setWeight: 25
- pause: {}
- setWeight: 50
- pause: {}
- setWeight: 75
- pause: {}`).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 300*time.Second).
		PromoteRolloutPluginFull().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 300*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), rov1.RolloutPluginPhaseHealthy, rp.Status.Phase)
			assert.False(s.T(), rp.Status.PromoteFull)
			assert.Equal(s.T(), rp.Status.CurrentRevision, rp.Status.UpdatedRevision)
		})
}

// TestRolloutPluginPromoteFullFromFirstStep tests PromoteFull from step 0.
func (s *RolloutPluginSuite) TestRolloutPluginPromoteFullFromFirstStep() {
	s.Given().
		RolloutPluginObjects(rpCanary2Replicas).
		SetRolloutPluginSteps(`
- pause: {}
- setWeight: 50
- pause: {}
- setWeight: 100`).
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy).
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(0, 180*time.Second).
		PromoteRolloutPluginFull().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), rov1.RolloutPluginPhaseHealthy, rp.Status.Phase)
		})
}

// New Revision Mid-Rollout Tests

// TestRolloutPluginNewRevisionMidRollout tests updating the image while a canary is in progress.
func (s *RolloutPluginSuite) TestRolloutPluginNewRevisionMidRollout() {
	s.Given().
		HealthyRolloutPlugin(rpCanary2Replicas, `
- setWeight: 50
- pause: {}`).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), rov1.RolloutPluginPhasePaused, rp.Status.Phase)
		}).
		When().
		// Trigger a second revision while paused
		UpdateStatefulSetImage("quay.io/prometheus/busybox:uclibc").
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseProgressing, 60*time.Second).
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		PromoteRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), rov1.RolloutPluginPhaseHealthy, rp.Status.Phase)
			assert.Equal(s.T(), rp.Status.CurrentRevision, rp.Status.UpdatedRevision)
		})
}

// TestRolloutPluginNewRevisionClearsAbort tests that a new revision clears the aborted state.
func (s *RolloutPluginSuite) TestRolloutPluginNewRevisionClearsAbort() {
	s.Given().
		HealthyRolloutPlugin(rpCanary2Replicas, `
- setWeight: 50
- pause: {}`).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		AbortRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseDegraded, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.True(s.T(), rp.Status.Aborted)
		}).
		When().
		// New revision should clear aborted state
		UpdateStatefulSetImage("quay.io/prometheus/busybox:uclibc").
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseProgressing, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.False(s.T(), rp.Status.Aborted, "New revision should clear aborted state")
			assert.Empty(s.T(), rp.Status.AbortedRevision)
		}).
		When().
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		PromoteRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 180*time.Second)
}

// Progress Deadline Tests

// TestRolloutPluginProgressDeadlineNoAbort tests progress deadline exceeded without abort.
func (s *RolloutPluginSuite) TestRolloutPluginProgressDeadlineNoAbort() {
	s.Given().
		RolloutPluginObjects(`
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: rp-deadline-no-abort
spec:
  progressDeadlineSeconds: 5
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: rp-deadline-no-abort
  plugin:
    name: statefulset
  strategy:
    canary:
      steps:
      - setWeight: 50
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: rp-deadline-no-abort
spec:
  serviceName: rp-deadline-no-abort
  replicas: 2
  selector:
    matchLabels:
      app: rp-deadline-no-abort
  template:
    metadata:
      labels:
        app: rp-deadline-no-abort
    spec:
      containers:
      - name: busybox
        image: quay.io/prometheus/busybox:latest
        command: ["/bin/sh", "-c", "while true; do sleep 30; done"]
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy).
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCondition(func(rp *rov1.RolloutPlugin) bool {
			for _, cond := range rp.Status.Conditions {
				if cond.Type == rov1.RolloutPluginConditionProgressing && cond.Reason == conditions.RolloutPluginTimedOutReason {
					return true
				}
			}
			return false
		}, conditions.RolloutPluginTimedOutReason, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			// Without progressDeadlineAbort, rollout should NOT be aborted but phase is Degraded
			assert.False(s.T(), rp.Status.Aborted)
			assert.Equal(s.T(), rov1.RolloutPluginPhaseDegraded, rp.Status.Phase)
		})
}

// TestRolloutPluginProgressDeadlineAbort tests progress deadline exceeded with abort enabled.
func (s *RolloutPluginSuite) TestRolloutPluginProgressDeadlineAbort() {
	s.Given().
		RolloutPluginObjects(`
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: rp-deadline-abort
spec:
  progressDeadlineSeconds: 5
  progressDeadlineAbort: true
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: rp-deadline-abort
  plugin:
    name: statefulset
  strategy:
    canary:
      steps:
      - setWeight: 50
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: rp-deadline-abort
spec:
  serviceName: rp-deadline-abort
  replicas: 2
  selector:
    matchLabels:
      app: rp-deadline-abort
  template:
    metadata:
      labels:
        app: rp-deadline-abort
    spec:
      containers:
      - name: busybox
        image: quay.io/prometheus/busybox:latest
        command: ["/bin/sh", "-c", "while true; do sleep 30; done"]
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy).
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseDegraded, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.True(s.T(), rp.Status.Aborted, "Should be aborted due to progressDeadlineAbort")
			assert.Equal(s.T(), rov1.RolloutPluginPhaseDegraded, rp.Status.Phase)
		})
}

// Validation Tests

// TestRolloutPluginInvalidSpecMissingStrategy tests InvalidSpec condition for missing strategy.
func (s *RolloutPluginSuite) TestRolloutPluginInvalidSpecMissingStrategy() {
	s.Given().
		RolloutPluginObjects(`
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: rp-invalid-strategy
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: rp-invalid-strategy
  plugin:
    name: statefulset
  strategy: {}
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: rp-invalid-strategy
spec:
  serviceName: rp-invalid-strategy
  replicas: 1
  selector:
    matchLabels:
      app: rp-invalid-strategy
  template:
    metadata:
      labels:
        app: rp-invalid-strategy
    spec:
      containers:
      - name: busybox
        image: quay.io/prometheus/busybox:latest
        command: ["/bin/sh", "-c", "while true; do sleep 30; done"]
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		ApplyManifests().
		WaitForRolloutPluginCondition(func(rp *rov1.RolloutPlugin) bool {
			for _, cond := range rp.Status.Conditions {
				if cond.Type == rov1.RolloutPluginConditionInvalidSpec {
					return true
				}
			}
			return false
		}, "InvalidSpec", 60*time.Second).
		Then().
		ExpectRolloutPluginCondition(rov1.RolloutPluginConditionInvalidSpec)
}

// TestRolloutPluginInvalidSpecPluginNotFound tests InvalidSpec for a nonexistent plugin.
func (s *RolloutPluginSuite) TestRolloutPluginInvalidSpecPluginNotFound() {
	s.Given().
		RolloutPluginObjects(`
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: rp-invalid-plugin
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: rp-invalid-plugin
  plugin:
    name: nonexistent-plugin
  strategy:
    canary:
      steps:
      - setWeight: 50
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: rp-invalid-plugin
spec:
  serviceName: rp-invalid-plugin
  replicas: 1
  selector:
    matchLabels:
      app: rp-invalid-plugin
  template:
    metadata:
      labels:
        app: rp-invalid-plugin
    spec:
      containers:
      - name: busybox
        image: quay.io/prometheus/busybox:latest
        command: ["/bin/sh", "-c", "while true; do sleep 30; done"]
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		ApplyManifests().
		WaitForRolloutPluginCondition(func(rp *rov1.RolloutPlugin) bool {
			for _, cond := range rp.Status.Conditions {
				if cond.Type == rov1.RolloutPluginConditionInvalidSpec {
					return true
				}
			}
			return false
		}, "InvalidSpec", 60*time.Second).
		Then().
		ExpectRolloutPluginCondition(rov1.RolloutPluginConditionInvalidSpec)
}

// TestRolloutPluginInvalidSpecFix tests that fixing an invalid spec removes the InvalidSpec condition.
func (s *RolloutPluginSuite) TestRolloutPluginInvalidSpecFix() {
	s.Given().
		RolloutPluginObjects(`
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: rp-invalid-fix
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: rp-invalid-fix
  plugin:
    name: statefulset
  strategy: {}
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: rp-invalid-fix
spec:
  serviceName: rp-invalid-fix
  replicas: 1
  selector:
    matchLabels:
      app: rp-invalid-fix
  template:
    metadata:
      labels:
        app: rp-invalid-fix
    spec:
      containers:
      - name: busybox
        image: quay.io/prometheus/busybox:latest
        command: ["/bin/sh", "-c", "while true; do sleep 30; done"]
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		ApplyManifests().
		WaitForRolloutPluginCondition(func(rp *rov1.RolloutPlugin) bool {
			for _, cond := range rp.Status.Conditions {
				if cond.Type == rov1.RolloutPluginConditionInvalidSpec {
					return true
				}
			}
			return false
		}, "InvalidSpec", 60*time.Second).
		Then().
		ExpectRolloutPluginCondition(rov1.RolloutPluginConditionInvalidSpec).
		When().
		// Fix the spec by adding a proper canary strategy
		PatchRolloutPluginSpec(`
spec:
  strategy:
    canary:
      steps:
      - setWeight: 50
`).
		WaitForRolloutPluginCondition(func(rp *rov1.RolloutPlugin) bool {
			for _, cond := range rp.Status.Conditions {
				if cond.Type == rov1.RolloutPluginConditionInvalidSpec {
					return false
				}
			}
			return true
		}, "InvalidSpecRemoved", 60*time.Second).
		Then().
		ExpectNoRolloutPluginCondition(rov1.RolloutPluginConditionInvalidSpec)
}

// Analysis Tests

// TestRolloutPluginBackgroundAnalysisSuccess tests successful background analysis.
func (s *RolloutPluginSuite) TestRolloutPluginBackgroundAnalysisSuccess() {
	s.Given().
		RolloutPluginObjects(`
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: rp-bg-analysis-ok
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: rp-bg-analysis-ok
  plugin:
    name: statefulset
  strategy:
    canary:
      analysis:
        templates:
        - templateName: rp-web-background
        startingStep: 0
        args:
        - name: url-val
          value: "https://kubernetes.default.svc/version"
      steps:
      - setWeight: 50
      - pause: {}
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: rp-bg-analysis-ok
spec:
  serviceName: rp-bg-analysis-ok
  replicas: 2
  selector:
    matchLabels:
      app: rp-bg-analysis-ok
  template:
    metadata:
      labels:
        app: rp-bg-analysis-ok
    spec:
      containers:
      - name: busybox
        image: quay.io/prometheus/busybox:latest
        command: ["/bin/sh", "-c", "while true; do sleep 30; done"]
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy).
		Then().
		ExpectRolloutPluginAnalysisRunCount(0).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		Then().
		ExpectRolloutPluginAnalysisRunCount(1).
		When().
		PromoteRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 180*time.Second).
		WaitForRolloutPluginBackgroundAnalysisRunPhase("Successful").
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.False(s.T(), rp.Status.Aborted)
			assert.Equal(s.T(), rov1.RolloutPluginPhaseHealthy, rp.Status.Phase)
		})
}

// TestRolloutPluginBackgroundAnalysisFail tests that a failed background analysis aborts the rollout.
func (s *RolloutPluginSuite) TestRolloutPluginBackgroundAnalysisFail() {
	s.Given().
		RolloutPluginObjects(`
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: rp-bg-analysis-fail
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: rp-bg-analysis-fail
  plugin:
    name: statefulset
  strategy:
    canary:
      analysis:
        templates:
        - templateName: rp-web-background-fail
        startingStep: 0
        args:
        - name: url-val
          value: "https://kubernetes.default.svc/version"
      steps:
      - setWeight: 50
      - pause: {duration: 300s}
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: rp-bg-analysis-fail
spec:
  serviceName: rp-bg-analysis-fail
  replicas: 2
  selector:
    matchLabels:
      app: rp-bg-analysis-fail
  template:
    metadata:
      labels:
        app: rp-bg-analysis-fail
    spec:
      containers:
      - name: busybox
        image: quay.io/prometheus/busybox:latest
        command: ["/bin/sh", "-c", "while true; do sleep 30; done"]
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy).
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginBackgroundAnalysisRunPhase("Failed").
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseDegraded, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.True(s.T(), rp.Status.Aborted)
			assert.Equal(s.T(), rov1.RolloutPluginPhaseDegraded, rp.Status.Phase)
		})
}

// TestRolloutPluginInlineAnalysisSuccess tests successful inline (step) analysis.
func (s *RolloutPluginSuite) TestRolloutPluginInlineAnalysisSuccess() {
	s.Given().
		RolloutPluginObjects(`
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: rp-inline-ok
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: rp-inline-ok
  plugin:
    name: statefulset
  strategy:
    canary:
      steps:
      - setWeight: 50
      - analysis:
          templates:
          - templateName: rp-sleep-job
          args:
          - name: exit-code
            value: "0"
      - pause: {}
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: rp-inline-ok
spec:
  serviceName: rp-inline-ok
  replicas: 2
  selector:
    matchLabels:
      app: rp-inline-ok
  template:
    metadata:
      labels:
        app: rp-inline-ok
    spec:
      containers:
      - name: busybox
        image: quay.io/prometheus/busybox:latest
        command: ["/bin/sh", "-c", "while true; do sleep 30; done"]
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy).
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		WaitForRolloutPluginInlineAnalysisRunPhase("Successful").
		WaitForRolloutPluginCanaryStepIndex(2, 180*time.Second).
		Then().
		ExpectRolloutPluginAnalysisRunCount(1).
		ExpectRolloutPluginStatus(rov1.RolloutPluginPhasePaused).
		When().
		PromoteRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 180*time.Second)
}

// TestRolloutPluginInlineAnalysisFail tests that a failed inline analysis aborts the rollout.
func (s *RolloutPluginSuite) TestRolloutPluginInlineAnalysisFail() {
	s.Given().
		RolloutPluginObjects(`
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: rp-inline-fail
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: rp-inline-fail
  plugin:
    name: statefulset
  strategy:
    canary:
      steps:
      - setWeight: 50
      - analysis:
          templates:
          - templateName: rp-sleep-job
          args:
          - name: exit-code
            value: "1"
      - pause: {}
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: rp-inline-fail
spec:
  serviceName: rp-inline-fail
  replicas: 2
  selector:
    matchLabels:
      app: rp-inline-fail
  template:
    metadata:
      labels:
        app: rp-inline-fail
    spec:
      containers:
      - name: busybox
        image: quay.io/prometheus/busybox:latest
        command: ["/bin/sh", "-c", "while true; do sleep 30; done"]
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy).
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		WaitForRolloutPluginInlineAnalysisRunPhase("Failed").
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseDegraded, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.True(s.T(), rp.Status.Aborted)
			assert.Equal(s.T(), rov1.RolloutPluginPhaseDegraded, rp.Status.Phase)
		})
}

// TestRolloutPluginAnalysisRunOwnership tests that AnalysisRuns are owned by the RolloutPlugin.
func (s *RolloutPluginSuite) TestRolloutPluginAnalysisRunOwnership() {
	s.Given().
		RolloutPluginObjects(`
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: rp-ar-ownership
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: rp-ar-ownership
  plugin:
    name: statefulset
  strategy:
    canary:
      analysis:
        templates:
        - templateName: rp-web-background
        startingStep: 0
        args:
        - name: url-val
          value: "https://kubernetes.default.svc/version"
      steps:
      - setWeight: 50
      - pause: {}
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: rp-ar-ownership
spec:
  serviceName: rp-ar-ownership
  replicas: 2
  selector:
    matchLabels:
      app: rp-ar-ownership
  template:
    metadata:
      labels:
        app: rp-ar-ownership
    spec:
      containers:
      - name: busybox
        image: quay.io/prometheus/busybox:latest
        command: ["/bin/sh", "-c", "while true; do sleep 30; done"]
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy).
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		Then().
		ExpectRolloutPluginAnalysisRunCount(1).
		Assert(func(t *fixtures.Then) {
			aruns := t.GetRolloutPluginAnalysisRuns()
			rp := t.GetRolloutPlugin()
			for _, ar := range aruns.Items {
				ownerRef := ar.OwnerReferences
				assert.Len(s.T(), ownerRef, 1)
				assert.Equal(s.T(), rp.Name, ownerRef[0].Name)
				assert.Equal(s.T(), "RolloutPlugin", ownerRef[0].Kind)
			}
		})
}

// TestRolloutPluginInlineAnalysisInconclusive tests that an inconclusive inline analysis pauses
// the rollout with PauseReasonInconclusiveAnalysis instead of aborting it.
func (s *RolloutPluginSuite) TestRolloutPluginInlineAnalysisInconclusive() {
	s.Given().
		RolloutPluginObjects(`
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: rp-inline-inconclusive
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: rp-inline-inconclusive
  plugin:
    name: statefulset
  strategy:
    canary:
      steps:
      - setWeight: 50
      - analysis:
          templates:
          - templateName: rp-web-background-inconclusive
          args:
          - name: url-val
            value: "https://kubernetes.default.svc/version"
      - pause: {}
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: rp-inline-inconclusive
spec:
  serviceName: rp-inline-inconclusive
  replicas: 2
  selector:
    matchLabels:
      app: rp-inline-inconclusive
  template:
    metadata:
      labels:
        app: rp-inline-inconclusive
    spec:
      containers:
      - name: busybox
        image: quay.io/prometheus/busybox:latest
        command: ["/bin/sh", "-c", "while true; do sleep 30; done"]
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy).
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		WaitForRolloutPluginInlineAnalysisRunPhase("Inconclusive").
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhasePaused, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.False(s.T(), rp.Status.Aborted, "rollout should not be aborted on inconclusive")
			assert.True(s.T(), rp.Status.ControllerPause, "rollout should be controller-paused")
			foundInconclusivePause := false
			for _, pc := range rp.Status.PauseConditions {
				if pc.Reason == rov1.PauseReasonInconclusiveAnalysis {
					foundInconclusivePause = true
					break
				}
			}
			assert.True(s.T(), foundInconclusivePause, "expected PauseReasonInconclusiveAnalysis pause condition")
		})
}

// TestRolloutPluginStepAnalysisRunLabels tests that step AnalysisRuns get the correct
// RolloutCanaryStepIndexLabel and RolloutTypeLabel labels.
func (s *RolloutPluginSuite) TestRolloutPluginStepAnalysisRunLabels() {
	s.Given().
		RolloutPluginObjects(`
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: rp-step-labels
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: rp-step-labels
  plugin:
    name: statefulset
  strategy:
    canary:
      steps:
      - setWeight: 50
      - analysis:
          templates:
          - templateName: rp-sleep-job
          args:
          - name: exit-code
            value: "0"
      - pause: {}
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: rp-step-labels
spec:
  serviceName: rp-step-labels
  replicas: 2
  selector:
    matchLabels:
      app: rp-step-labels
  template:
    metadata:
      labels:
        app: rp-step-labels
    spec:
      containers:
      - name: busybox
        image: quay.io/prometheus/busybox:latest
        command: ["/bin/sh", "-c", "while true; do sleep 30; done"]
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy).
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		WaitForRolloutPluginInlineAnalysisRunPhase("Successful").
		WaitForRolloutPluginCanaryStepIndex(2, 180*time.Second).
		Then().
		ExpectRolloutPluginAnalysisRunCount(1).
		Assert(func(t *fixtures.Then) {
			aruns := t.GetRolloutPluginAnalysisRuns()
			for _, ar := range aruns.Items {
				assert.Equal(s.T(), rov1.RolloutTypeStepLabel, ar.Labels[rov1.RolloutTypeLabel],
					"AnalysisRun should have RolloutTypeLabel=Step")
				stepIdx, ok := ar.Labels[rov1.RolloutCanaryStepIndexLabel]
				assert.True(s.T(), ok, "AnalysisRun should have RolloutCanaryStepIndexLabel")
				assert.Equal(s.T(), strconv.Itoa(1), stepIdx,
					"step-index label should match the analysis step index")
			}
		})
}

// Conditions Tests

// TestRolloutPluginConditions tests that proper conditions are set during lifecycle.
func (s *RolloutPluginSuite) TestRolloutPluginConditions() {
	s.Given().
		HealthyRolloutPlugin(rpCanary2Replicas, `
- setWeight: 50
- pause: {}`).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		Sleep(5*time.Second). // Wait for Paused condition to be set on next reconcile
		Then().
		// During pause, should have Paused condition with Status=True
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			var hasPausedTrue bool
			for _, cond := range rp.Status.Conditions {
				if cond.Type == rov1.RolloutPluginConditionPaused && cond.Status == corev1.ConditionTrue {
					hasPausedTrue = true
				}
			}
			assert.True(s.T(), hasPausedTrue, "Should have Paused condition with Status=True")
		}).
		When().
		PromoteRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 180*time.Second).
		Sleep(5 * time.Second). // Wait for conditions to update after promotion
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			for _, cond := range rp.Status.Conditions {
				if cond.Type == rov1.RolloutPluginConditionPaused {
					assert.Equal(s.T(), corev1.ConditionFalse, cond.Status,
						"Paused condition should be False after completion")
				}
			}
		})
}

// Event Tests

// TestRolloutPluginEvents tests that events are recorded during rollout lifecycle.
func (s *RolloutPluginSuite) TestRolloutPluginEvents() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Given().
		StartEventWatch(ctx).
		RolloutPluginObjects(rpCanary2Replicas).
		SetRolloutPluginSteps(`
- setWeight: 50
- pause: {}`).
		When().
		ApplyManifests().
		WaitForStatefulSetReady().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy).
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		PromoteRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			reasons := t.GetRolloutPluginEventReasons()
			assert.NotEmpty(s.T(), reasons, "Should have recorded events")
			assert.Contains(s.T(), reasons, conditions.RolloutPluginProgressingReason, "Should have RolloutPluginProgressing event")
			assert.Contains(s.T(), reasons, conditions.RolloutPluginPausedReason, "Should have RolloutPluginPaused event")
			assert.Contains(s.T(), reasons, conditions.RolloutPluginCompletedReason, "Should have RolloutPluginCompleted event")
			// Verify event ordering: Progressing → Paused → Completed should appear as a subsequence
			expectedSeq := []string{conditions.RolloutPluginProgressingReason, conditions.RolloutPluginPausedReason, conditions.RolloutPluginCompletedReason}
			i := 0
			for _, r := range reasons {
				if i < len(expectedSeq) && r == expectedSeq[i] {
					i++
				}
			}
			assert.Equal(s.T(), len(expectedSeq), i,
				"Events should appear in order: Progressing → Paused → Completed, got: %v", reasons)
		})
}

// TestRolloutPluginAbortEvent tests that an abort event is recorded.
func (s *RolloutPluginSuite) TestRolloutPluginAbortEvent() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Given().
		StartEventWatch(ctx).
		HealthyRolloutPlugin(rpCanary2Replicas, `
- setWeight: 50
- pause: {}`).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		AbortRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseDegraded, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			reasons := t.GetRolloutPluginEventReasons()
			assert.Contains(s.T(), reasons, conditions.RolloutPluginAbortedReason, "Should have RolloutPluginAborted event")
		})
}

// Status Field Tests

// TestRolloutPluginStatusFields tests that all key status fields are properly set.
func (s *RolloutPluginSuite) TestRolloutPluginStatusFields() {
	s.Given().
		HealthyRolloutPlugin(rpCanary2Replicas, `
- setWeight: 50
- pause: {}`).
		When().
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			// Verify all key fields are populated after initial deploy
			assert.NotEmpty(s.T(), rp.Status.CurrentRevision, "CurrentRevision should be set")
			assert.Equal(s.T(), rp.Status.CurrentRevision, rp.Status.UpdatedRevision, "Revisions should match when healthy")
			assert.Greater(s.T(), rp.Status.ObservedGeneration, int64(0), "ObservedGeneration should be > 0")
			assert.Equal(s.T(), rov1.RolloutPluginPhaseHealthy, rp.Status.Phase)
			assert.False(s.T(), rp.Status.Aborted)
			assert.False(s.T(), rp.Status.ControllerPause)
			assert.Equal(s.T(), int32(0), rp.Status.RestartCount)
		}).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			// During canary, verify mid-rollout fields
			assert.Equal(s.T(), rov1.RolloutPluginPhasePaused, rp.Status.Phase)
			assert.NotEqual(s.T(), rp.Status.CurrentRevision, rp.Status.UpdatedRevision, "Revisions should differ during rollout")
			assert.True(s.T(), rp.Status.ControllerPause)
			assert.True(s.T(), len(rp.Status.PauseConditions) > 0)
		}).
		When().
		PromoteRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 180*time.Second)
}

// TestRolloutPluginObservedGeneration tests that ObservedGeneration is updated on spec changes.
func (s *RolloutPluginSuite) TestRolloutPluginObservedGeneration() {
	var initialGen int64
	s.Given().
		HealthyRolloutPlugin(rpCanary2Replicas, `
- setWeight: 50
- pause: {}`).
		When().
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			initialGen = rp.Generation
			assert.Equal(s.T(), rp.Generation, rp.Status.ObservedGeneration, "ObservedGeneration should match Generation after initial deploy")
		}).
		When().
		// Spec change: pause triggers a new generation
		PauseRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhasePaused, 60*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Greater(s.T(), rp.Generation, initialGen, "Generation should increment after spec change")
			assert.Equal(s.T(), rp.Generation, rp.Status.ObservedGeneration, "ObservedGeneration should catch up to new Generation")
		}).
		When().
		ResumeRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 180*time.Second)
}

// Timed Pause Step Tests

// TestRolloutPluginTimedPause tests that timed pause steps auto-advance.
func (s *RolloutPluginSuite) TestRolloutPluginTimedPause() {
	s.Given().
		HealthyRolloutPlugin(rpCanary2Replicas, `
- setWeight: 50
- pause: {duration: 5s}
- setWeight: 100`).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		// Should auto-advance past the 5s timed pause and complete
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 180*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			rp := t.GetRolloutPlugin()
			assert.Equal(s.T(), rov1.RolloutPluginPhaseHealthy, rp.Status.Phase)
		})
}

// StatefulSet Partition Tests

// TestRolloutPluginStatefulSetPartition tests that partition is set correctly during canary.
func (s *RolloutPluginSuite) TestRolloutPluginStatefulSetPartition() {
	s.Given().
		HealthyRolloutPlugin(rpCanary4Replicas, `
- setWeight: 50
- pause: {}`).
		When().
		UpdateStatefulSetImage("quay.io/prometheus/busybox:glibc").
		WaitForRolloutPluginCanaryStepIndex(1, 300*time.Second).
		Then().
		// With 4 replicas and 50% weight, partition should allow 2 pods to update
		Assert(func(t *fixtures.Then) {
			sts := t.GetStatefulSet()
			var partition int32
			if sts.Spec.UpdateStrategy.RollingUpdate != nil && sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
				partition = *sts.Spec.UpdateStrategy.RollingUpdate.Partition
			}
			// Partition should be set to limit canary to 50%
			assert.True(s.T(), partition == 2,
				"Partition should be 2 for 50%% weight with 4 replicas, got %d", partition)
		}).
		When().
		PromoteRolloutPlugin().
		WaitForRolloutPluginStatus(rov1.RolloutPluginPhaseHealthy, 300*time.Second).
		Then().
		// After completion, partition should be 0
		ExpectStatefulSetPartition(0)
}
