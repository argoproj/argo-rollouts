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

func TestCanarySuite(t *testing.T) {
	suite.Run(t, new(CanarySuite))
}

func (s *CanarySuite) TestCanarySetCanaryScale() {
	s.T().Skip("skipping v0.10 feature")
	s.T().Parallel()
	canarySteps := `
- pause: {}
- setCanaryScale:
    weight: 25
- pause: {}
- setWeight: 50
- pause: {}
- setCanaryScale:
    replicas: 3
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
		UpdateSpec().
		// at step 0
		WaitForRolloutStatus("Paused").
		Then().
		ExpectCanaryStablePodCount(0, 4).
		When().
		PromoteRollout().
		WaitForRolloutCanaryStepIndex(2).
		Then().
		// at step 2
		ExpectCanaryStablePodCount(1, 4).
		When().
		PromoteRollout().
		WaitForRolloutCanaryStepIndex(4).
		Then().
		// at step 4
		ExpectCanaryStablePodCount(1, 4).
		When().
		PromoteRollout().
		WaitForRolloutCanaryStepIndex(6).
		Then().
		// at step 6
		ExpectCanaryStablePodCount(3, 4).
		When().
		PromoteRollout().
		WaitForRolloutCanaryStepIndex(8).
		Then().
		// at step 8
		ExpectCanaryStablePodCount(2, 4).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectCanaryStablePodCount(4, 4)
}

// TestRolloutScalingWhenPaused verifies behavior when scaling a rollout up/down when paused
func (s *FunctionalSuite) TestRolloutScalingWhenPaused() {
	s.Given().
		RolloutObjects(`@functional/rollout-basic.yaml`).
		SetSteps(`
- setWeight: 25
- pause: {}`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		ExpectCanaryStablePodCount(1, 1).
		When().
		ScaleRollout(8).
		WaitForRolloutAvailableReplicas(8).
		Then().
		ExpectCanaryStablePodCount(2, 6).
		When().
		ScaleRollout(4).
		WaitForRolloutAvailableReplicas(4).
		Then().
		ExpectCanaryStablePodCount(1, 3)
}

// TestRolloutScalingDuringUpdate verifies behavior when scaling a rollout up/down in middle of update
func (s *FunctionalSuite) TestRolloutScalingDuringUpdate() {
	s.Given().
		HealthyRollout(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: updatescaling
spec:
  replicas: 4
  strategy:
    canary:
      maxSurge: 2
      maxUnavailable: 0
  selector:
    matchLabels:
      app: updatescaling
  template:
    metadata:
      labels:
        app: updatescaling
    spec:
      containers:
      - name: updatescaling
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 1m`).
		When().
		PatchSpec(`
spec:
  template:
    spec:
      containers:
      - name: updatescaling
        command: [/bad-command]`).
		WaitForRolloutReplicas(6).
		Then().
		ExpectCanaryStablePodCount(2, 4).
		When().
		ScaleRollout(8).
		WaitForRolloutReplicas(10).
		Then().
		// NOTE: the numbers below may change in the future.
		// See: https://github.com/argoproj/argo-rollouts/issues/738
		ExpectCanaryStablePodCount(6, 4).
		When().
		ScaleRollout(4)
	// WaitForRolloutReplicas(4) // this doesn't work yet (bug)
}
