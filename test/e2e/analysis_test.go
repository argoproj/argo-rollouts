//go:build e2e
// +build e2e

package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"

	"github.com/stretchr/testify/suite"
	"github.com/tj/assert"

	"github.com/argoproj/argo-rollouts/test/fixtures"
)

type AnalysisSuite struct {
	fixtures.E2ESuite
}

func TestAnalysisSuite(t *testing.T) {
	suite.Run(t, new(AnalysisSuite))
}

func (s *AnalysisSuite) SetupSuite() {
	s.E2ESuite.SetupSuite()
	// shared analysis templates for suite
	s.ApplyManifests("@functional/analysistemplate-web-background.yaml")
	s.ApplyManifests("@functional/analysistemplate-web-background-inconclusive.yaml")
	s.ApplyManifests("@functional/analysistemplate-sleep-job.yaml")
	s.ApplyManifests("@functional/analysistemplate-multiple-job.yaml")
	s.ApplyManifests("@functional/analysistemplate-fail-multiple-job.yaml")
	s.ApplyManifests("@functional/analysistemplate-long-running-job.yaml")
	s.ApplyManifests("@functional/analysistemplate-invalid-image-job.yaml")
	s.ApplyManifests("@functional/analysistemplate-long-running-job-deadline.yaml")
}

// convenience to generate a new service with a given name
func newService(name, label string) string {
	return fmt.Sprintf(`
kind: Service
apiVersion: v1
metadata:
  name: %s
spec:
  ports:
  - port: 80
    targetPort: 80
  selector:
    app: %s
`, name, label)
}

func (s *AnalysisSuite) TestCanaryBackgroundAnalysis() {
	s.Given().
		RolloutObjects("@functional/rollout-background-analysis.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		ExpectAnalysisRunCount(1).
		ExpectBackgroundAnalysisRunPhase("Running").
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		WaitForBackgroundAnalysisRunPhase("Successful")
}

func (s *AnalysisSuite) TestCanaryInconclusiveBackgroundAnalysis() {
	s.Given().
		RolloutObjects("@functional/rollout-background-analysis-inconclusive.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		WaitForBackgroundAnalysisRunPhase("Running").
		Then().
		ExpectAnalysisRunCount(1).
		When().
		WaitForBackgroundAnalysisRunPhase("Inconclusive").
		WaitForRolloutMessage("InconclusiveAnalysisRun").
		Then().
		ExpectRolloutStatus("Paused").
		ExpectRolloutMessage("InconclusiveAnalysisRun")
}

func (s *AnalysisSuite) TestCanaryInlineAnalysis() {
	s.Given().
		RolloutObjects("@functional/rollout-inline-analysis.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		ExpectAnalysisRunCount(1).
		When().
		WaitForInlineAnalysisRunPhase("Successful").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(3)
}

func (s *AnalysisSuite) TestCanaryInlineMultipleAnalysis() {
	s.Given().
		RolloutObjects("@functional/rollout-inline-multiple-analysis.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		Sleep(5 * time.Second).
		Then().
		ExpectAnalysisRunCount(1).
		ExpectInlineAnalysisRunPhase("Running").
		When().
		WaitForInlineAnalysisRunPhase("Successful").
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(1)
}

func (s *AnalysisSuite) TestCanaryFailInlineMultipleAnalysis() {
	s.Given().
		RolloutObjects("@functional/rollout-degraded-inline-multiple-analysis.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		Sleep(1 * time.Second). // promoting too fast causes test to flake
		Then().
		ExpectRolloutStatus("Progressing").
		When().
		WaitForInlineAnalysisRunPhase("Failed").
		WaitForRolloutStatus("Degraded").
		Then().
		ExpectRolloutStatus("Degraded")
}

// TestBlueGreenAnalysis tests blue-green with pre/post analysis and then fast-tracked rollback
func (s *AnalysisSuite) TestBlueGreenAnalysis() {
	original := `
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: bluegreen-analysis
spec:
  strategy:
    blueGreen:
      activeService: bluegreen-analysis-active
      previewService: bluegreen-analysis-preview
      prePromotionAnalysis:
        templates:
        - templateName: sleep-job
        args:
        - name: duration
          value: "5"
      postPromotionAnalysis:
        templates:
        - templateName: sleep-job
        args:
        - name: duration
          value: "5"
      autoPromotionEnabled: false
  selector:
    matchLabels:
      app: bluegreen-analysis
  template:
    metadata:
      labels:
        app: bluegreen-analysis
    spec:
      containers:
      - name: bluegreen-analysis
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 5m
`
	s.Given().
		RolloutObjects(newService("bluegreen-analysis-active", "bluegreen-analysis")).
		RolloutObjects(newService("bluegreen-analysis-preview", "bluegreen-analysis")).
		RolloutObjects(original).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		When().
		// make sure we're changing a field in a manner that the kubectl apply of the original
		// manifests will be detected in the 3-way merge patch, so that we can re-apply the original
		// yaml and the rollout controller will detect a fast-tracked rollback.
		UpdateSpec(`
spec:
  template:
    spec:
      containers:
      - name: bluegreen-analysis
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 4m`).
		WaitForRolloutStatus("Progressing").
		WaitForRolloutStatus("Paused").
		Then().
		ExpectAnalysisRunCount(1).
		ExpectActiveRevision("1").
		ExpectPreviewRevision("2").
		When().
		Sleep(2*time.Second). // promoting too fast causes test to flake
		PromoteRollout().
		WaitForActiveRevision("2").
		Sleep(2*time.Second). // analysis is created on later reconciliations after service cutover
		Then().
		ExpectAnalysisRunCount(2).
		ExpectReplicaCounts(1, 2, 1, 1, 1).
		ExpectRolloutStatus("Progressing").
		When().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(2).
		ExpectActiveRevision("2").
		ExpectPreviewRevision("2").
		When().
		ApplyManifests(original). // perform a rollback and make sure we skip pause/analysis
		Sleep(2 * time.Second).   // checking too early may not catch the bug where we create analysis unnecessarily
		Then().
		ExpectRolloutStatus("Healthy"). // rollout is healthy immediately
		ExpectAnalysisRunCount(2).      // no new analysis runs created
		ExpectStableRevision("3").
		ExpectActiveRevision("3").
		ExpectPreviewRevision("3")
}

// blueGreenWorkloadRefObjects returns a Deployment and a blue-green Rollout that resolves its pod
// template from that Deployment via workloadRef, with pre/post promotion analysis configured.
func blueGreenWorkloadRefObjects(name string, autoPromotionEnabled bool) string {
	return fmt.Sprintf(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %[1]s
spec:
  replicas: 0
  selector:
    matchLabels:
      app: %[1]s
  template:
    metadata:
      labels:
        app: %[1]s
    spec:
      containers:
      - name: %[1]s
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 5m
---
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: %[1]s
spec:
  replicas: 1
  workloadRef:
    apiVersion: apps/v1
    kind: Deployment
    name: %[1]s
  selector:
    matchLabels:
      app: %[1]s
  strategy:
    blueGreen:
      activeService: %[1]s-active
      previewService: %[1]s-preview
      autoPromotionEnabled: %[2]t
      prePromotionAnalysis:
        templates:
        - templateName: sleep-job
        args:
        - name: duration
          value: "5"
      postPromotionAnalysis:
        templates:
        - templateName: sleep-job
        args:
        - name: duration
          value: "5"
`, name, autoPromotionEnabled)
}

// blueGreenWorkloadRefUpdate is a merge patch for the referenced Deployment that changes the pod
// template (cpu request 5m -> 4m) and therefore triggers a new Rollout revision.
func blueGreenWorkloadRefUpdate(name string) string {
	return fmt.Sprintf(`
spec:
  template:
    spec:
      containers:
      - name: %[1]s
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 4m`, name)
}

// TestBlueGreenWorkloadRefManualPromotionAnalysis verifies that a blue-green rollout resolving
// its pod template through workloadRef honors autoPromotionEnabled: false and runs pre/post
// promotion analysis when the referenced Deployment's pod template changes (#4503).
func (s *AnalysisSuite) TestBlueGreenWorkloadRefManualPromotionAnalysis() {
	name := "bluegreen-workloadref-manual"
	s.Given().
		RolloutObjects(newService(name+"-active", name)).
		RolloutObjects(newService(name+"-preview", name)).
		RolloutObjects(blueGreenWorkloadRefObjects(name, false)).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		ExpectActiveRevision("1").
		ExpectStableRevision("1").
		When().
		UpdateWorkloadRef(name, blueGreenWorkloadRefUpdate(name)).
		WaitForRolloutStatus("Progressing").
		WaitForRolloutStatus("Paused").
		Then().
		ExpectAnalysisRunCount(1). // pre-promotion analysis ran before pausing for manual promotion
		ExpectActiveRevision("1").
		ExpectStableRevision("1").
		ExpectPreviewRevision("2").
		When().
		Sleep(2 * time.Second). // promoting too fast causes test to flake
		PromoteRollout().
		WaitForActiveRevision("2").
		Sleep(2 * time.Second). // analysis is created on later reconciliations after service cutover
		Then().
		ExpectAnalysisRunCount(2). // post-promotion analysis started after the manual promotion
		ExpectRolloutStatus("Progressing").
		When().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(2).
		ExpectStableRevision("2").
		ExpectActiveRevision("2").
		ExpectPreviewRevision("2")
}

// TestBlueGreenWorkloadRefAutoPromotionAnalysis verifies that a blue-green rollout resolving its
// pod template through workloadRef runs pre-promotion analysis before the automatic promotion and
// post-promotion analysis after it when the referenced Deployment's pod template changes.
func (s *AnalysisSuite) TestBlueGreenWorkloadRefAutoPromotionAnalysis() {
	name := "bluegreen-workloadref-auto"
	s.Given().
		RolloutObjects(newService(name+"-active", name)).
		RolloutObjects(newService(name+"-preview", name)).
		RolloutObjects(blueGreenWorkloadRefObjects(name, true)).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		ExpectActiveRevision("1").
		ExpectStableRevision("1").
		When().
		UpdateWorkloadRef(name, blueGreenWorkloadRefUpdate(name)).
		WaitForRolloutStatus("Progressing").
		WaitForRolloutCondition(func(ro *v1alpha1.Rollout) bool {
			return ro.Status.BlueGreen.PrePromotionAnalysisRunStatus != nil
		}, "pre-promotion analysis started").
		Then().
		ExpectAnalysisRunCount(1).
		ExpectActiveRevision("1"). // pre-promotion analysis gates the automatic promotion
		When().
		WaitForPrePromotionAnalysisRunPhase("Successful").
		WaitForActiveRevision("2"). // promoted automatically once pre-promotion analysis succeeded
		Sleep(2 * time.Second).     // analysis is created on later reconciliations after service cutover
		Then().
		ExpectAnalysisRunCount(2).
		When().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(2).
		ExpectStableRevision("2").
		ExpectActiveRevision("2").
		ExpectPreviewRevision("2")
}

// TestBlueGreenPrePromotionFail test rollout behavior when pre promotion analysis fails
func (s *AnalysisSuite) TestBlueGreenPrePromotionFail() {
	s.Given().
		RolloutObjects(newService("pre-promotion-fail-active", "pre-promotion-fail")).
		RolloutObjects(newService("pre-promotion-fail-preview", "pre-promotion-fail")).
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: pre-promotion-fail
spec:
  replicas: 2
  strategy:
    blueGreen:
      abortScaleDownDelaySeconds: 0
      activeService: pre-promotion-fail-active
      previewService: pre-promotion-fail-preview
      previewReplicaCount: 1
      prePromotionAnalysis:
        templates:
        - templateName: sleep-job
        args:
        - name: exit-code
          value: "1"
        - name: duration
          value: "5"
  selector:
    matchLabels:
      app: pre-promotion-fail
  template:
    metadata:
      labels:
        app: pre-promotion-fail
    spec:
      containers:
      - name: pre-promotion-fail
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 5m
`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Degraded").
		Then().
		ExpectAnalysisRunCount(1).
		ExpectStableRevision("1").
		ExpectActiveRevision("1").
		ExpectPreviewRevision("2").
		When().
		PatchSpec(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: pre-promotion-fail
spec:
  strategy:
    blueGreen:
      prePromotionAnalysis:
        templates:
        - templateName: sleep-job
        args:
        - name: exit-code
          value: "0"
        - name: duration
          value: "5"
`).
		RetryRollout().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(2).
		ExpectStableRevision("2").
		ExpectActiveRevision("2").
		ExpectPreviewRevision("2")
}

func (s *AnalysisSuite) TestBlueGreenPostPromotionFail() {
	s.Given().
		RolloutObjects(newService("post-promotion-fail-active", "post-promotion-fail")).
		RolloutObjects(newService("post-promotion-fail-preview", "post-promotion-fail")).
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: post-promotion-fail
spec:
  strategy:
    blueGreen:
      activeService: post-promotion-fail-active
      previewService: post-promotion-fail-preview
      autoPromotionEnabled: false
      postPromotionAnalysis:
        templates:
        - templateName: sleep-job
        args:
        - name: exit-code
          value: "1"
        - name: duration
          value: "5"
  selector:
    matchLabels:
      app: post-promotion-fail
  template:
    metadata:
      labels:
        app: post-promotion-fail
    spec:
      containers:
      - name: post-promotion-fail
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 5m
`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Progressing").
		WaitForRolloutStatus("Paused").
		Then().
		ExpectStableRevision("1").
		ExpectActiveRevision("1").
		ExpectPreviewRevision("2").
		When().
		PromoteRollout().
		Sleep(2 * time.Second). // checking service selectors too fast causes test to flake
		Then().
		ExpectStableRevision("1").
		ExpectActiveRevision("2").
		ExpectPreviewRevision("2").
		When().
		WaitForRolloutStatus("Degraded").
		Then().
		ExpectAnalysisRunCount(1).
		ExpectStableRevision("1").
		ExpectActiveRevision("1").
		ExpectPreviewRevision("2")
}

// TestBlueGreenAbortThenUpdate tests the scenario:
// 1. deploy rev 1
// 2. deploy rev 2 which fails analysis
// 3. deploy rev 3 which passes analysis
// verifies
func (s *AnalysisSuite) TestBlueGreenAbortAndUpdate() {
	s.Given().
		RolloutObjects(newService("bluegreen-abort-and-update-active", "bluegreen-abort-and-update")).
		RolloutObjects(newService("bluegreen-abort-and-update-preview", "bluegreen-abort-and-update")).
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: bluegreen-abort-and-update
spec:
  strategy:
    blueGreen:
      activeService: bluegreen-abort-and-update-active
      previewService: bluegreen-abort-and-update-preview
      postPromotionAnalysis:
        templates:
        - templateName: sleep-job
        args:
        - name: exit-code
          value: "1"
  selector:
    matchLabels:
      app: bluegreen-abort-and-update
  template:
    metadata:
      labels:
        app: bluegreen-abort-and-update
    spec:
      containers:
      - name: bluegreen-abort-and-update
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 5m
`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Degraded").
		Then().
		ExpectAnalysisRunCount(1).
		ExpectActiveRevision("1").
		ExpectPreviewRevision("2").
		When().
		// Update spec to a successful version
		UpdateSpec(`
spec:
  template:
    metadata:
      annotations:
        foo: bar
  strategy:
    blueGreen:
      autoPromotionEnabled: false
      postPromotionAnalysis:
        templates:
        - templateName: sleep-job
        args: null
`).
		WaitForRolloutStatus("Paused").
		Sleep(2*time.Second). // Give some time before validating the scaling down event
		Then().
		ExpectRevisionPodCount("1", 1).
		ExpectRevisionScaleDown("2", true).
		ExpectRevisionPodCount("3", 1).
		ExpectActiveRevision("1").
		ExpectPreviewRevision("3").
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Sleep(2*time.Second). // Give some time before validating the scaling down event
		Then().
		ExpectRevisionPodCount("1", 1).
		ExpectRevisionScaleDown("1", true).
		ExpectRevisionScaleDown("2", true).
		ExpectRevisionPodCount("3", 1).
		ExpectActiveRevision("3").
		ExpectPreviewRevision("3").
		ExpectAnalysisRunCount(2)
}

// TestBlueGreenKitchenSink various features of blue-green strategy
func (s *AnalysisSuite) TestBlueGreenKitchenSink() {
	s.Given().
		RolloutObjects(newService("bluegreen-kitchensink-active", "bluegreen-kitchensink")).
		RolloutObjects(newService("bluegreen-kitchensink-preview", "bluegreen-kitchensink")).
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: bluegreen-kitchensink
spec:
  replicas: 2
  strategy:
    blueGreen:
      activeService: bluegreen-kitchensink-active
      previewService: bluegreen-kitchensink-preview
      previewReplicaCount: 1
      autoPromotionSeconds: 10
      scaleDownDelaySeconds: 5
      prePromotionAnalysis:
        templates:
        - templateName: sleep-job
        args:
        - name: duration
          value: "10"
      postPromotionAnalysis:
        templates:
        - templateName: sleep-job
        args:
        - name: exit-code
          value: "1"
        - name: duration
          value: "10"
  selector:
    matchLabels:
      app: bluegreen-kitchensink
  template:
    metadata:
      labels:
        app: bluegreen-kitchensink
    spec:
      containers:
      - name: bluegreen-kitchensink
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		ExpectActiveRevision("1").
		ExpectPreviewRevision("2").
		ExpectStableRevision("1").
		ExpectRevisionPodCount("1", 2).
		ExpectRevisionPodCount("2", 1).
		ExpectReplicaCounts(2, 3, 1, 2, 2). // desired, current, updated, ready, available
		ExpectAnalysisRunCount(1).
		When().
		Sleep(5*time.Second). // sleep 5 seconds, verify we did not autopromote too early
		Then().
		ExpectActiveRevision("1").
		ExpectStableRevision("1").
		ExpectRevisionPodCount("1", 2).
		ExpectRevisionPodCount("2", 1).
		ExpectReplicaCounts(2, 3, 1, 2, 2). // desired, current, updated, ready, available
		When().
		WaitForActiveRevision("2"). // no need to manually promote since autoPromotionSeconds will do it
		Then().
		ExpectRevisionPodCount("2", 2).
		ExpectRevisionPodCount("1", 2).
		ExpectReplicaCounts(2, 4, 2, 2, 2). // desired, current, updated, ready, available
		When().
		Sleep(time.Second).
		Then().
		ExpectAnalysisRunCount(2).
		When().
		WaitForRolloutStatus("Degraded").
		Then().
		ExpectActiveRevision("1").
		ExpectPreviewRevision("2").
		ExpectStableRevision("1").
		ExpectRevisionPodCount("2", 2).
		ExpectRevisionPodCount("1", 2).
		ExpectReplicaCounts(2, 4, 2, 2, 2)
}

// TestMultipleAnalysis verifies we merge analysis templates properly when multiple are specified
func (s *AnalysisSuite) TestMultipleAnalysis() {
	s.Given().
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: template-a
spec:
  args:
  - name: host
    value: SHOULD-NOT-USE
  - name: repo
  metrics:
  - name: metric-1
    count: 1
    provider:
      web:
        url: https://{{args.host}}/repos/{{args.repo}}
  - name: metric-2
    count: 1
    provider:
      web:
        url: https://{{args.host}}/repos/{{args.repo}}`).
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: template-b
spec:
  args:
  - name: host
  - name: repo
    value: SHOULD-NOT-USE
  metrics:
  - name: metric-3
    count: 1
    provider:
      web:
        url: https://{{args.host}}/repos/{{args.repo}}
  - name: metric-4
    count: 1
    provider:
      web:
        url: https://{{args.host}}/repos/{{args.repo}}`).
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: multi-analysis
spec:
  selector:
    matchLabels:
      app: multi-analysis
  strategy:
    canary:
      steps:
      - analysis:
          templates:
          - templateName: template-a
          - templateName: template-b
          args:
          - name: host
            value: api.github.com
          - name: repo
            value: argoproj/argo-rollouts
  template:
    metadata:
      labels:
        app: multi-analysis
    spec:
      containers:
      - name: multi-analysis
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 5m`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(1)
}

func (s *AnalysisSuite) TestAnalysisWithSecret() {
	s.Given().
		RolloutObjects("@functional/rollout-secret.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			ar := t.GetRolloutAnalysisRuns().Items[0]
			assert.Equal(s.T(), v1alpha1.AnalysisPhaseSuccessful, ar.Status.Phase)
			metricResult := ar.Status.MetricResults[0]
			assert.Equal(s.T(), int32(2), metricResult.Count)
		}).
		When().
		WaitForInlineAnalysisRunPhase("Successful").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectStableRevision("2")
}

func (s *AnalysisSuite) TestAnalysisWithArgs() {
	s.Given().
		RolloutObjects("@functional/rollout-secret-withArgs.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			ar := t.GetRolloutAnalysisRuns().Items[0]
			assert.Equal(s.T(), v1alpha1.AnalysisPhaseSuccessful, ar.Status.Phase)
			metricResult := ar.Status.MetricResults[0]
			assert.Equal(s.T(), int32(3), metricResult.Count)
		}).
		When().
		WaitForInlineAnalysisRunPhase("Successful").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectStableRevision("2")
}

func (s *AnalysisSuite) TestBackgroundAnalysisWithArgs() {
	s.Given().
		RolloutObjects("@functional/rollout-bg-analysis-withArgs.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Sleep(3 * time.Second). // Give some time before validating that AnalysisRun got kicked off
		Then().
		ExpectAnalysisRunCount(1).
		ExpectBackgroundAnalysisRunPhase("Running").
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		WaitForBackgroundAnalysisRunPhase("Successful")
}

func (s *AnalysisSuite) TestCanaryInlineAnalysisLongRunningJobWithDeadline() {
	s.Given().
		RolloutObjects("@functional/rollout-inline-long-running-job-deadline.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Degraded").
		Then().
		ExpectAnalysisRunCount(1).
		When().
		WaitForInlineAnalysisRunPhase("Failed").
		Then().
		Assert(func(t *fixtures.Then) {
			ar := t.GetRolloutAnalysisRuns().Items[0]
			assert.Equal(s.T(), v1alpha1.AnalysisPhaseFailed, ar.Status.Phase)
			if len(ar.Status.MetricResults) > 0 {
				metricResult := ar.Status.MetricResults[0]
				assert.Equal(s.T(), v1alpha1.AnalysisPhaseFailed, metricResult.Phase)
				if len(metricResult.Measurements) > 0 {
					measurement := metricResult.Measurements[len(metricResult.Measurements)-1]
					// The job that was terminated due to deadline should be Failed
					assert.Equal(s.T(), v1alpha1.AnalysisPhaseFailed, measurement.Phase)
				}
			}
		})
}

func (s *AnalysisSuite) TestCanaryBackgroundAnalysisLongRunningJobWithDeadline() {
	s.Given().
		RolloutObjects("@functional/rollout-background-long-running-job-deadline.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Degraded").
		Then().
		ExpectAnalysisRunCount(1).
		When().
		WaitForBackgroundAnalysisRunPhase("Failed").
		Then().
		Assert(func(t *fixtures.Then) {
			ar := t.GetBackgroundAnalysisRun()
			assert.Equal(s.T(), v1alpha1.AnalysisPhaseFailed, ar.Status.Phase)
			if len(ar.Status.MetricResults) > 0 {
				metricResult := ar.Status.MetricResults[0]
				assert.Equal(s.T(), v1alpha1.AnalysisPhaseFailed, metricResult.Phase)
				if len(metricResult.Measurements) > 0 {
					measurement := metricResult.Measurements[len(metricResult.Measurements)-1]
					// The job that was terminated due to deadline should be Failed
					assert.Equal(s.T(), v1alpha1.AnalysisPhaseFailed, measurement.Phase)
				}
			}
		})
}

func (s *AnalysisSuite) TestCanaryBackgroundAnalysisLongRunningJob() {
	s.Given().
		RolloutObjects("@functional/rollout-background-long-running-job.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		ExpectAnalysisRunCount(1).
		ExpectBackgroundAnalysisRunPhase("Running").
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		WaitForBackgroundAnalysisRunPhase("Inconclusive").
		Then().
		Assert(func(t *fixtures.Then) {
			ar := t.GetBackgroundAnalysisRun()
			assert.Equal(s.T(), v1alpha1.AnalysisPhaseInconclusive, ar.Status.Phase)
			if len(ar.Status.MetricResults) > 0 {
				metricResult := ar.Status.MetricResults[0]
				// The job that never started should be marked as Inconclusive, not Successful as it never finished
				assert.Equal(s.T(), v1alpha1.AnalysisPhaseInconclusive, metricResult.Phase)
				if len(metricResult.Measurements) > 0 {
					measurement := metricResult.Measurements[len(metricResult.Measurements)-1]
					// The measurement should be Inconclusive, not Successful as the job never finished
					assert.Equal(s.T(), v1alpha1.AnalysisPhaseInconclusive, measurement.Phase, "Job that never finished should be Inconclusive, not Successful")
				}
			}
		})
}

func (s *AnalysisSuite) TestCanaryInlineAnalysisInvalidImageJob() {
	s.Given().
		RolloutObjects("@functional/rollout-inline-invalid-image-job.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused"). // paused because of inconclusive rollout/analysis status, not inline pause
		Then().
		ExpectAnalysisRunCount(1).
		ExpectInlineAnalysisRunPhase("Inconclusive").
		Assert(func(t *fixtures.Then) {
			ar := t.GetRolloutAnalysisRuns().Items[0]
			assert.Equal(s.T(), v1alpha1.AnalysisPhaseInconclusive, ar.Status.Phase)
			if len(ar.Status.MetricResults) > 0 {
				metricResult := ar.Status.MetricResults[0]
				assert.Equal(s.T(), v1alpha1.AnalysisPhaseInconclusive, metricResult.Phase)
				if len(metricResult.Measurements) > 0 {
					measurement := metricResult.Measurements[len(metricResult.Measurements)-1]
					// A job pod stuck in ErrImagePull/ImagePullBackOff/InvalidImageName
					// (without a deadline) should produce an Inconclusive measurement, which
					// pauses the rollout with reason InconclusiveAnalysisRun instead of failing it.
					assert.Equal(s.T(), v1alpha1.AnalysisPhaseInconclusive, measurement.Phase)
				}
			}
		})
}

func (s *AnalysisSuite) TestCanaryBackgroundAnalysisInvalidImageJob() {
	s.Given().
		RolloutObjects("@functional/rollout-background-invalid-job.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		ExpectAnalysisRunCount(2).
		ExpectInlineAnalysisRunPhase("Running").
		ExpectBackgroundAnalysisRunPhase("Inconclusive").
		Assert(func(t *fixtures.Then) {
			ar := t.GetBackgroundAnalysisRun()
			assert.Equal(s.T(), v1alpha1.AnalysisPhaseInconclusive, ar.Status.Phase)
			if len(ar.Status.MetricResults) > 0 {
				metricResult := ar.Status.MetricResults[0]
				assert.Equal(s.T(), v1alpha1.AnalysisPhaseInconclusive, metricResult.Phase)
				if len(metricResult.Measurements) > 0 {
					measurement := metricResult.Measurements[len(metricResult.Measurements)-1]
					assert.Equal(s.T(), v1alpha1.AnalysisPhaseInconclusive, measurement.Phase)
				}
			}
		})
}
