// +build e2e

package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

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
	s.ApplyManifests("@functional/analysistemplate-sleep-job.yaml")
}

// convenience to generate a new service with a given name
func newService(name string) string {
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
`, name, name)
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
		RolloutObjects(newService("bluegreen-analysis-active")).
		RolloutObjects(newService("bluegreen-analysis-preview")).
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

// TestBlueGreenPrePromotionFail test rollout behavior when pre promotion analysis fails
func (s *AnalysisSuite) TestBlueGreenPrePromotionFail() {
	s.Given().
		RolloutObjects(newService("pre-promotion-fail-active")).
		RolloutObjects(newService("pre-promotion-fail-preview")).
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: pre-promotion-fail
spec:
  replicas: 2
  strategy:
    blueGreen:
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
		RolloutObjects(newService("post-promotion-fail-active")).
		RolloutObjects(newService("post-promotion-fail-preview")).
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
		RolloutObjects(newService("bluegreen-abort-and-update-active")).
		RolloutObjects(newService("bluegreen-abort-and-update-preview")).
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
		Then().
		ExpectRevisionPodCount("1", 1).
		ExpectRevisionPodCount("2", 0).
		ExpectRevisionPodCount("3", 1).
		ExpectActiveRevision("1").
		ExpectPreviewRevision("3").
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectRevisionPodCount("1", 1).
		ExpectRevisionPodCount("2", 0).
		ExpectRevisionPodCount("3", 1).
		ExpectActiveRevision("3").
		ExpectPreviewRevision("3").
		ExpectAnalysisRunCount(2)
}

// TestBlueGreenKitchenSink various features of blue-green strategy
func (s *AnalysisSuite) TestBlueGreenKitchenSink() {
	s.Given().
		RolloutObjects(newService("bluegreen-kitchensink-active")).
		RolloutObjects(newService("bluegreen-kitchensink-preview")).
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
      autoPromotionSeconds: 5
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
