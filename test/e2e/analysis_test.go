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

func (s *AnalysisSuite) TestBlueGreenAnalysis() {
	s.Given().
		RolloutObjects(newService("bluegreen-analysis-active")).
		RolloutObjects(newService("bluegreen-analysis-preview")).
		RolloutObjects(`
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
		ExpectActiveRevision("1").
		ExpectPreviewRevision("2").
		When().
		Sleep(2 * time.Second). // promoting too fast causes test to flake
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(2).
		ExpectActiveRevision("2").
		ExpectPreviewRevision("2")
}

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
  strategy:
    blueGreen:
      activeService: pre-promotion-fail-active
      previewService: pre-promotion-fail-preview
      prePromotionAnalysis:
        templates:
        - templateName: sleep-job
        args:
        - name: exit-code
          value: "1"
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
		ExpectActiveRevision("1").
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
		ExpectActiveRevision("1").
		ExpectPreviewRevision("2").
		When().
		PromoteRollout().
		Sleep(2 * time.Second). // checking service selectors too fast causes test to flake
		Then().
		ExpectActiveRevision("2").
		ExpectPreviewRevision("2").
		When().
		WaitForRolloutStatus("Degraded").
		Then().
		ExpectAnalysisRunCount(1).
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
		ExpectRevisionPodCount("1", 0). // this may change after https://github.com/argoproj/argo-rollouts/issues/756
		ExpectRevisionPodCount("2", 0).
		ExpectRevisionPodCount("3", 1).
		ExpectActiveRevision("3").
		ExpectPreviewRevision("3").
		ExpectAnalysisRunCount(2)
}
