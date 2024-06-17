//go:build e2e
// +build e2e

package e2e

import (
	"testing"
	"time"

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
//   - specRef: stable
//   - specRef: canary
//   - valueFrom.podTemplateHashValue: Stable
//   - valueFrom.podTemplateHashValue: Latest
//   - templates.XXXXX.podTemplateHash
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

			// verify if labels and annotations from AnalysisTemplate's .spec.metrics[0].provider.job.metadata
			// are added to the Job.metadata
			assert.Equal(s.T(), "bar", job.GetLabels()["foo"])
			assert.Equal(s.T(), "bar2", job.GetAnnotations()["foo2"])

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

func (s *ExperimentSuite) TestExperimentWithServiceAndScaleDownDelay() {
	g := s.Given()
	g.ApplyManifests("@functional/experiment-with-service.yaml")
	g.When().
		WaitForExperimentPhase("experiment-with-service", "Running").
		WaitForExperimentCondition("experiment-with-service", func(ex *rov1.Experiment) bool {
			return s.GetReplicaSetFromExperiment(ex, "test").Status.Replicas == 1
		}, "number-of-rs-pods-meet", fixtures.E2EWaitTimeout).
		Then().
		ExpectExperimentTemplateReplicaSetNumReplicas("experiment-with-service", "test", 1).
		ExpectExperimentServiceCount("experiment-with-service", 1).
		When().
		WaitForExperimentPhase("experiment-with-service", "Successful").
		WaitForExperimentCondition("experiment-with-service", func(ex *rov1.Experiment) bool {
			return s.GetReplicaSetFromExperiment(ex, "test").Status.Replicas == 0
		}, "number-of-rs-pods-meet", fixtures.E2EWaitTimeout).
		Then().
		ExpectExperimentTemplateReplicaSetNumReplicas("experiment-with-service", "test", 0).
		ExpectExperimentServiceCount("experiment-with-service", 0)
}

func (s *ExperimentSuite) TestExperimentWithServiceNameAndScaleDownDelay() {
	g := s.Given()
	g.ApplyManifests("@functional/experiment-with-service-name.yaml")
	g.When().
		WaitForExperimentPhase("experiment-with-service-name", "Running").
		WaitForExperimentCondition("experiment-with-service-name", func(ex *rov1.Experiment) bool {
			return s.GetReplicaSetFromExperiment(ex, "test").Status.Replicas == 1
		}, "number-of-rs-pods-meet", fixtures.E2EWaitTimeout).
		Then().
		ExpectExperimentTemplateReplicaSetNumReplicas("experiment-with-service-name", "test", 1).
		ExpectExperimentServiceCount("experiment-with-service-name", 1).
		When().
		WaitForExperimentPhase("experiment-with-service-name", "Successful").
		WaitForExperimentCondition("experiment-with-service-name", func(ex *rov1.Experiment) bool {
			return s.GetReplicaSetFromExperiment(ex, "test").Status.Replicas == 0
		}, "number-of-rs-pods-meet", fixtures.E2EWaitTimeout).
		Then().
		ExpectExperimentTemplateReplicaSetNumReplicas("experiment-with-service-name", "test", 0).
		ExpectExperimentServiceCount("experiment-with-service-name", 0)
}

func (s *ExperimentSuite) TestExperimentWithMultiportServiceAndScaleDownDelay() {
	g := s.Given()
	g.ApplyManifests("@functional/experiment-with-multiport-service.yaml")
	g.When().
		WaitForExperimentPhase("experiment-with-multiport-service", "Running").
		WaitForExperimentCondition("experiment-with-multiport-service", func(ex *rov1.Experiment) bool {
			return s.GetReplicaSetFromExperiment(ex, "test").Status.Replicas == 1
		}, "number-of-rs-pods-meet", fixtures.E2EWaitTimeout).
		Then().
		ExpectExperimentTemplateReplicaSetNumReplicas("experiment-with-multiport-service", "test", 1).
		ExpectExperimentServiceCount("experiment-with-multiport-service", 1).
		When().
		WaitForExperimentPhase("experiment-with-multiport-service", "Successful").
		WaitForExperimentCondition("experiment-with-multiport-service", func(ex *rov1.Experiment) bool {
			return s.GetReplicaSetFromExperiment(ex, "test").Status.Replicas == 0
		}, "number-of-rs-pods-meet", fixtures.E2EWaitTimeout).
		Then().
		ExpectExperimentTemplateReplicaSetNumReplicas("experiment-with-multiport-service", "test", 0).
		ExpectExperimentServiceCount("experiment-with-multiport-service", 0)
}

func (s *ExperimentSuite) TestExperimentWithDryRunMetrics() {
	g := s.Given()
	g.ApplyManifests("@functional/experiment-dry-run-analysis.yaml")
	g.When().
		WaitForExperimentPhase("experiment-with-dry-run", "Successful").
		Sleep(time.Second*3).
		Then().
		ExpectExperimentDryRunSummary(1, 0, 1, "experiment-with-dry-run")
}

func (s *ExperimentSuite) TestExperimentWithMeasurementRetentionMetrics() {
	g := s.Given()
	g.ApplyManifests("@functional/experiment-measurement-retention-analysis.yaml")
	g.When().
		WaitForExperimentPhase("experiment-with-mr", "Successful").
		Sleep(time.Second*3).
		Then().
		ExpectExperimentMeasurementsLength(0, 2, "experiment-with-mr")
}

func TestExperimentSuite(t *testing.T) {
	suite.Run(t, new(ExperimentSuite))
}
