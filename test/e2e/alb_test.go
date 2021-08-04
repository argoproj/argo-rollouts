// +build e2e

package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/tj/assert"

	"github.com/argoproj/argo-rollouts/test/fixtures"
)

type ALBSuite struct {
	fixtures.E2ESuite
}

func TestALBSuite(t *testing.T) {
	suite.Run(t, new(ALBSuite))
}

func (s *ALBSuite) SetupSuite() {
	s.E2ESuite.SetupSuite()
}

const actionTemplate = `{"Type":"forward","ForwardConfig":{"TargetGroups":[{"ServiceName":"%s","ServicePort":"%d","Weight":%d},{"ServiceName":"%s","ServicePort":"%d","Weight":%d}]}}`

const actionTemplateWithExperiment = `{"Type":"forward","ForwardConfig":{"TargetGroups":[{"ServiceName":"%s","ServicePort":"%d","Weight":%d},{"ServiceName":"%s","ServicePort":"%d","Weight":%d},{"ServiceName":"%s","ServicePort":"%d","Weight":%d}]}}`

func (s *ALBSuite) TestALBExperimentStep() {
	s.Given().
		RolloutObjects("@alb/rollout-alb-experiment.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ingress := t.GetALBIngress()
			action, ok :=  ingress.Annotations["alb.ingress.kubernetes.io/actions.alb-rollout-root"]
			assert.True(s.T(), ok)

			port := 80
			expectedAction := fmt.Sprintf(actionTemplate, "alb-rollout-canary", port, 0, "alb-rollout-stable", port, 100)
			assert.Equal(s.T(), expectedAction, action)
		}).
		ExpectExperimentCount(0).
		When().
		UpdateSpec().
		WaitForRolloutCanaryStepIndex(1).
		Sleep(10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ingress := t.GetALBIngress()
			action, ok :=  ingress.Annotations["alb.ingress.kubernetes.io/actions.alb-rollout-root"]
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
		Sleep(1*time.Second). // stable is currently set first, and then changes made to VirtualServices/DestinationRules
		Then().
		Assert(func(t *fixtures.Then) {
			ingress := t.GetALBIngress()
			action, ok :=  ingress.Annotations["alb.ingress.kubernetes.io/actions.alb-rollout-root"]
			assert.True(s.T(), ok)

			port := 80
			expectedAction := fmt.Sprintf(actionTemplate, "alb-rollout-canary", port, 0, "alb-rollout-stable", port, 100)
			assert.Equal(s.T(), expectedAction, action)
		})
}
