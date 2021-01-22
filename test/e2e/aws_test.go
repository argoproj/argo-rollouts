// +build e2e

package e2e

import (
	"os"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/argoproj/argo-rollouts/test/fixtures"
)

type AWSSuite struct {
	fixtures.E2ESuite
}

func TestAWSSuite(t *testing.T) {
	suite.Run(t, new(AWSSuite))
}

// TestALBUpdate is a simple integration test which verifies the controller can work in a real AWS
// environment. It is intended to be run with the `--alb-verify-weight` controller flag. Success of
// this test against a controller using that flag, indicates that the controller was able to perform
// weight verification using AWS APIs.
// This test will be skipped unless E2E_ALB_INGESS_ANNOTATIONS is set (can be an empty struct). e.g.:
// make test-e2e E2E_INSTANCE_ID= E2E_TEST_OPTIONS="-testify.m TestALBUpdate$" E2E_ALB_INGESS_ANNOTATIONS='{"kubernetes.io/ingress.class": "aws-alb", "alb.ingress.kubernetes.io/security-groups": "iks-intuit-cidr-ingress-tcp-443"}'
func (s *AWSSuite) TestALBUpdate() {
	if val, _ := os.LookupEnv(fixtures.EnvVarE2EALBIngressAnnotations); val == "" {
		s.T().SkipNow()
	}
	s.Given().
		HealthyRollout(`@functional/alb-rollout.yaml`).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Healthy")
}
