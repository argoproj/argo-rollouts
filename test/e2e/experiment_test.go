// +build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/argoproj/argo-rollouts/test/fixtures"
)

type ExperimentSuite struct {
	fixtures.E2ESuite
}

func (s *ExperimentSuite) TestRolloutWithExperiment() {
	s.Given().
		RolloutObjects("@functional/rollout-experiment.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		// TODO: verify there are no experiments
		Then().
		When().
		UpdateSpec().
		// TODO: wait for experiment to start and complete successful
		// TODO: verify pods
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy")
}

func TestExperimentSuite(t *testing.T) {
	suite.Run(t, new(ExperimentSuite))
}
