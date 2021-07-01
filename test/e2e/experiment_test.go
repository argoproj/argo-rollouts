// +build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	rov1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/test/fixtures"
)

type ExperimentSuite struct {
	fixtures.E2ESuite
}

// TestRolloutWithExperimentAndAnalysis this tests the ability for a rollout to launch an experiment,
// and use self-referencing features/pass metadata arguments to the experiment and analysis, such as:
//  * specRef: stable
//  * specRef: canary
//  * valueFrom.podTemplateHashValue: Stable
//  * valueFrom.podTemplateHashValue: Latest
//  * templates.XXXXX.podTemplateHash
func (s *ExperimentSuite) TestRolloutWithExperimentAndAnalysis() {
	s.T().Parallel()
	s.Given().
		RolloutObjects("@functional/rollout-experiment-analysis.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectExperimentCount(0).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		ExpectExperimentCount(1).
		ExpectExperimentByRevisionPhase("2", "Successful").
		Assert(func(t *fixtures.Then) {
			rs1 := t.GetReplicaSetByRevision("1")
			rs1Hash := rs1.Labels[rov1.DefaultRolloutUniqueLabelKey]
			rs2 := t.GetReplicaSetByRevision("2")
			rs2Hash := rs2.Labels[rov1.DefaultRolloutUniqueLabelKey]
			assert.NotEmpty(s.T(), rs1Hash)
			assert.NotEmpty(s.T(), rs2Hash)
			assert.NotEqual(s.T(), rs1Hash, rs2Hash, "rs %s and %s share same pod hash", rs1.Name, rs2.Name)

			exp := t.GetExperimentByRevision("2")
			expRSCanary := t.GetReplicaSetFromExperiment(exp, "canary")
			expRSBaseline := t.GetReplicaSetFromExperiment(exp, "baseline")

			// verify the experiment pod specs are identical when using `specRef: stable` and `specRef: canary`
			assert.Equal(s.T(), rs1.Spec.Template.Spec, expRSBaseline.Spec.Template.Spec, "rs %s and %s pod specs differ", rs1.Name, expRSBaseline.Name)
			assert.Equal(s.T(), rs2.Spec.Template.Spec, expRSCanary.Spec.Template.Spec, "rs %s and %s pod specs differ", rs2.Name, expRSCanary.Name)

			// verify the `valueFrom.podTemplateHashValue: Stable` and `valueFrom.podTemplateHashValue: Latest` are working
			ar := t.GetExperimentAnalysisRun(exp)
			job := t.GetJobFromAnalysisRun(ar)
			envVars := job.Spec.Template.Spec.Containers[0].Env
			assert.Equal(s.T(), rs1Hash, envVars[1].Value)
			assert.Equal(s.T(), rs2Hash, envVars[0].Value)

			// verify the `templates.XXXXX.podTemplateHash` variables are working
			expRSCanaryHash := expRSCanary.Spec.Template.Labels[rov1.DefaultRolloutUniqueLabelKey]
			expRSBaselineHash := expRSBaseline.Spec.Template.Labels[rov1.DefaultRolloutUniqueLabelKey]
			assert.Equal(s.T(), envVars[2].Value, expRSBaselineHash, "rs %s pod-template-hash does not match baseline replicaset %s", expRSBaseline.Name, rs1.Name)
			assert.Equal(s.T(), envVars[3].Value, expRSCanaryHash, "rs %s pod-template-hash does not match canary replicaset %s", expRSCanary.Name, rs2.Name)
			assert.NotEqual(s.T(), expRSBaselineHash, expRSCanaryHash)

			// verify we use different pod hashes for experiment replicasets vs. rollout replicasets
			// See: https://github.com/argoproj/argo-rollouts/issues/380
			assert.NotEqual(s.T(), rs1Hash, expRSBaseline, "rs %s and %s share same pod hash", rs1.Name, expRSBaseline.Name)
			assert.NotEqual(s.T(), rs2Hash, expRSCanaryHash, "rs %s and %s share same pod hash", rs2.Name, expRSCanary.Name)

		}).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy")
}

func TestExperimentSuite(t *testing.T) {
	suite.Run(t, new(ExperimentSuite))
}
