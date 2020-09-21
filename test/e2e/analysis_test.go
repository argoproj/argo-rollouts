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
		// TODO: verify there are no analysisruns
		Then().
		When().
		UpdateImage("argoproj/rollouts-demo:yellow").
		WaitForRolloutStatus("Paused").
		// TODO: verify there is one analysis running
		PromoteRollout().
		WaitForRolloutStatus("Healthy")
	// TODO: verify analysisrun is terminated
}

func TestAnalysisSuite(t *testing.T) {
	suite.Run(t, new(AnalysisSuite))
}
