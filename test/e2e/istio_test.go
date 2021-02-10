// +build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/argoproj/argo-rollouts/test/fixtures"
)

type IstioSuite struct {
	fixtures.E2ESuite
}

func TestIstioSuite(t *testing.T) {
	suite.Run(t, new(IstioSuite))
}

func (s *IstioSuite) SetupSuite() {
	s.E2ESuite.SetupSuite()
	if !s.IstioEnabled {
		s.T().SkipNow()
	}
}

func (s *IstioSuite) TestIstioRollout() {
	s.Given().
		RolloutObjects("@istio/split-by-services/istio-rollout-analysis.yaml").
		RolloutObjects("@istio/split-by-services/istio-rollout-gateway.yaml").
		RolloutObjects("@istio/split-by-services/istio-rollout-services.yaml").
		RolloutObjects("@istio/split-by-services/istio-rollout-virtualservice.yaml").
		RolloutObjects("@istio/split-by-services/istio-rollout.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		StartLoad().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Wait(time.Minute).
		StopLoad()
}
