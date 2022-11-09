//go:build e2e
// +build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/argoproj/argo-rollouts/test/fixtures"
)

type RollbackSuite struct {
	fixtures.E2ESuite
}

func TestRollbackSuite(t *testing.T) {
	suite.Run(t, new(RollbackSuite))
}

func (s *RollbackSuite) SetupSuite() {
	s.E2ESuite.SetupSuite()
	// shared analysis templates for suite
	s.ApplyManifests("@functional/analysistemplate-sleep-job.yaml")
	s.ApplyManifests("@functional/rollout-rollback-window.yaml")
}

func (s *RollbackSuite) TestRollbackAnalysis() {
	s.Given().
		HealthyRollout("@functional/rollout-rollback-window.yaml").
		When().
		UpdateSpec(`
spec:
  template:
    metadata:
      annotations:
        rev: two`). // update to revision 2
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(1).
		When().
		UpdateSpec(`
spec:
  template:
    metadata:
      annotations:
        rev: three`). // update to revision 3
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(2).
		When().
		UpdateSpec(`
spec:
  template:
    metadata:
      annotations:
        rev: four`). // update to revision 4
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(3).
		When().
		UpdateSpec(`
spec:
  template:
    metadata:
      annotations:
        rev: three`). // rollback to revision 3 (update to revision 5)
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(3). // fast rollback, no analysis run
		When().
		UpdateSpec(`
spec:
  template:
    metadata:
      annotations:
        rev: six`). // update to revision 6
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(4).
		When().
		UpdateSpec(`
spec:
  template:
    metadata:
      annotations:
        rev: two`). // rollback to revision 2 (update to revision 7)
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(5) // regular rollback, no fast track (outside window)
}
