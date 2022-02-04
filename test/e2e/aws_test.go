//go:build e2e
// +build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/tj/assert"

	"github.com/argoproj/argo-rollouts/test/fixtures"
	ingress2 "github.com/argoproj/argo-rollouts/utils/ingress"
)

type AWSSuite struct {
	fixtures.E2ESuite
}

func TestAWSSuite(t *testing.T) {
	suite.Run(t, new(AWSSuite))
}

const actionTemplateWithExperiment = `{"Type":"forward","ForwardConfig":{"TargetGroups":[{"ServiceName":"%s","ServicePort":"%d","Weight":%d},{"ServiceName":"%s","ServicePort":"%d","Weight":%d},{"ServiceName":"%s","ServicePort":"%d","Weight":%d}]}}`
const actionTemplateWithExperiments = `{"Type":"forward","ForwardConfig":{"TargetGroups":[{"ServiceName":"%s","ServicePort":"%d","Weight":%d},{"ServiceName":"%s","ServicePort":"%d","Weight":%d},{"ServiceName":"%s","ServicePort":"%d","Weight":%d},{"ServiceName":"%s","ServicePort":"%d","Weight":%d}]}}`

// TestALBUpdate is a simple integration test which verifies the controller can work in a real AWS
// environment. It is intended to be run with the `--aws-verify-target-group` controller flag. Success of
// this test against a controller using that flag, indicates that the controller was able to perform
// weight verification using AWS APIs.
// This test will be skipped unless E2E_ALB_INGESS_ANNOTATIONS is set (can be an empty struct). e.g.:
// make test-e2e E2E_TEST_OPTIONS="-testify.m TestALBCanaryUpdate$" E2E_IMAGE_PREFIX="docker.intuit.com/docker-rmt/" E2E_INSTANCE_ID= E2E_ALB_INGESS_ANNOTATIONS='{"kubernetes.io/ingress.class": "aws-alb", "alb.ingress.kubernetes.io/security-groups": "iks-intuit-cidr-ingress-tcp-443"}'
func (s *AWSSuite) TestALBCanaryUpdate() {
	if val, _ := os.LookupEnv(fixtures.EnvVarE2EALBIngressAnnotations); val == "" {
		s.T().SkipNow()
	}
	s.Given().
		HealthyRollout(`@functional/alb-canary-rollout.yaml`).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Healthy")
}

func (s *AWSSuite) TestALBBlueGreenUpdate() {
	if val, _ := os.LookupEnv(fixtures.EnvVarE2EALBIngressAnnotations); val == "" {
		s.T().SkipNow()
	}
	s.Given().
		HealthyRollout(`@functional/alb-bluegreen-rollout.yaml`).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Healthy")
}

func (s *AWSSuite) TestALBPingPongUpdate() {
	s.Given().
		RolloutObjects("@functional/alb-pingpong-rollout.yaml").
		When().ApplyManifests().WaitForRolloutStatus("Healthy").
		Then().
		Assert(assertWeights(s, "ping-service", "pong-service", 100, 0)).
		// Update 1. Test the weight switch from ping => pong
		When().UpdateSpec().
		WaitForRolloutCanaryStepIndex(1).Sleep(1 * time.Second).Then().
		Assert(assertWeights(s, "ping-service", "pong-service", 75, 25)).
		When().PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Sleep(1 * time.Second).
		Then().
		Assert(assertWeights(s, "ping-service", "pong-service", 0, 100)).
		// Update 2. Test the weight switch from pong => ping
		When().UpdateSpec().
		WaitForRolloutCanaryStepIndex(1).Sleep(1 * time.Second).Then().
		Assert(assertWeights(s, "ping-service", "pong-service", 25, 75)).
		When().PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Sleep(1 * time.Second).
		Then().
		Assert(assertWeights(s, "ping-service", "pong-service", 100, 0))
}

func assertWeights(s *AWSSuite, groupA, groupB string, weightA, weightB int64) func(t *fixtures.Then) {
	return func(t *fixtures.Then) {
		ingress := t.GetALBIngress()
		action, ok := ingress.Annotations["alb.ingress.kubernetes.io/actions.alb-rollout-root"]
		assert.True(s.T(), ok)

		var albAction ingress2.ALBAction
		if err := json.Unmarshal([]byte(action), &albAction); err != nil {
			panic(err)
		}
		for _, targetGroup := range albAction.ForwardConfig.TargetGroups {
			switch targetGroup.ServiceName {
			case groupA:
				assert.True(s.T(), *targetGroup.Weight == weightA, fmt.Sprintf("Weight doesn't match: %d and %d", *targetGroup.Weight, weightA))
			case groupB:
				assert.True(s.T(), *targetGroup.Weight == weightB, fmt.Sprintf("Weight doesn't match: %d and %d", *targetGroup.Weight, weightB))
			default:
				assert.True(s.T(), false, "Service is not expected in the target group: "+targetGroup.ServiceName)
			}
		}
	}
}

func (s *AWSSuite) TestALBExperimentStep() {
	s.Given().
		RolloutObjects("@alb/rollout-alb-experiment.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(assertWeights(s, "alb-rollout-canary", "alb-rollout-stable", 0, 100)).
		ExpectExperimentCount(0).
		When().
		UpdateSpec().
		WaitForRolloutCanaryStepIndex(1).
		Sleep(10 * time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ingress := t.GetALBIngress()
			action, ok := ingress.Annotations["alb.ingress.kubernetes.io/actions.alb-rollout-root"]
			assert.True(s.T(), ok)

			ex := t.GetRolloutExperiments().Items[0]
			exServiceName := ex.Status.TemplateStatuses[0].ServiceName

			port := 80
			expectedAction := fmt.Sprintf(actionTemplateWithExperiment, "alb-rollout-canary", port, 10, exServiceName, port, 20, "alb-rollout-stable", port, 70)
			assert.Equal(s.T(), expectedAction, action)
		}).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Sleep(1 * time.Second). // stable is currently set first, and then changes made to VirtualServices/DestinationRules
		Then().
		Assert(assertWeights(s, "alb-rollout-canary", "alb-rollout-stable", 0, 100))
}

func (s *AWSSuite) TestALBExperimentStepNoSetWeight() {
	s.Given().
		RolloutObjects("@alb/rollout-alb-experiment-no-setweight.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(assertWeights(s, "alb-rollout-canary", "alb-rollout-stable", 0, 100)).
		ExpectExperimentCount(0).
		When().
		UpdateSpec().
		Sleep(10 * time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ingress := t.GetALBIngress()
			action, ok := ingress.Annotations["alb.ingress.kubernetes.io/actions.alb-rollout-root"]
			assert.True(s.T(), ok)

			experiment := t.GetRolloutExperiments().Items[0]
			exService1, exService2 := experiment.Status.TemplateStatuses[0].ServiceName, experiment.Status.TemplateStatuses[1].ServiceName

			port := 80
			expectedAction := fmt.Sprintf(actionTemplateWithExperiments, "alb-rollout-canary", port, 0, exService1, port, 20, exService2, port, 20, "alb-rollout-stable", port, 60)
			assert.Equal(s.T(), expectedAction, action)
		}).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Sleep(1 * time.Second). // stable is currently set first, and then changes made to VirtualServices/DestinationRules
		Then().
		Assert(assertWeights(s, "alb-rollout-canary", "alb-rollout-stable", 0, 100))
}
