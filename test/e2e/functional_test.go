// +build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/argoproj/argo-rollouts/test/fixtures"
)

type FunctionalSuite struct {
	fixtures.E2ESuite
}

func countReplicaSets(count int) fixtures.ReplicaSetExpectation {
	return func(rsets *appsv1.ReplicaSetList) bool {
		return len(rsets.Items) == count
	}
}

func (s *FunctionalSuite) TestRolloutAbortRetryPromote() {
	s.Given().
		HealthyRollout(`@functional/basic.yaml`).
		When().
		UpdateImage("argoproj/rollouts-demo:yellow").
		WaitForRolloutStatus("Paused").
		Then().
		ExpectReplicaSets("two replicasets", countReplicaSets(2)).
		When().
		AbortRollout().
		WaitForRolloutStatus("Degraded").
		RetryRollout().
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy")
}

func (s *FunctionalSuite) TestRolloutRestart() {
	s.Given().
		HealthyRollout(`@functional/basic.yaml`).
		When().
		RestartRollout().
		WaitForRolloutStatus("Progressing").
		WaitForRolloutStatus("Healthy")
}

func TestFunctionalSuite(t *testing.T) {
	suite.Run(t, new(FunctionalSuite))
}
