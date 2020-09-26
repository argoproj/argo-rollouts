// +build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/argoproj/argo-rollouts/test/fixtures"
)

type AnalysisSuite struct {
	fixtures.E2ESuite
}

func (s *AnalysisSuite) TestRolloutWithBackgroundAnalysis() {
	s.Given().
		RolloutObjects("@functional/analysistemplate-web-background.yaml").
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

func (s *AnalysisSuite) TestRolloutWithInlineAnalysis() {
	s.Given().
		RolloutObjects("@functional/analysistemplate-echo-job.yaml").
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

func TestAnalysisSuite(t *testing.T) {
	suite.Run(t, new(AnalysisSuite))
}
