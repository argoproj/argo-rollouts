// +build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/argoproj/argo-rollouts/test/fixtures"
)

type CanarySuite struct {
	fixtures.E2ESuite
}

func (s *CanarySuite) TestCanarySetCanaryScale() {
	canarySteps := `
- pause: {}
- setCanaryScale:
    weight: 25
- pause: {}
- setWeight: 50
- pause: {}
- setCanaryScale:
    replicas: 6
- pause: {}
- setCanaryScale:
    matchTrafficWeight: true
- pause: {}
`
	s.Given().
		RolloutTemplate("@functional/alb-template.yaml", "set-canary-scale").
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateImage("argoproj/rollouts-demo:yellow").
		// at step 0
		WaitForRolloutStatus("Paused").
		Then().
		ExpectCanaryPodCount(0).
		When().
		PromoteRollout().
		WaitForRolloutCanaryStepIndex(2).
		Then().
		// at step 2
		ExpectCanaryPodCount(1).
		When().
		PromoteRollout().
		WaitForRolloutCanaryStepIndex(4).
		Then().
		// at step 4
		ExpectCanaryPodCount(1).
		When().
		PromoteRollout().
		WaitForRolloutCanaryStepIndex(6).
		Then().
		// at step 6
		ExpectCanaryPodCount(6).
		When().
		PromoteRollout().
		WaitForRolloutCanaryStepIndex(8).
		Then().
		// at step 8
		ExpectCanaryPodCount(2).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectCanaryPodCount(4)
}

func TestCanarySuite(t *testing.T) {
	suite.Run(t, new(CanarySuite))
}
