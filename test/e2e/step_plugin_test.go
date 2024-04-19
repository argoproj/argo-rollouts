//go:build e2e
// +build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/tj/assert"
	"gopkg.in/yaml.v2"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/test/fixtures"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
	corev1 "k8s.io/api/core/v1"
)

const E2EStepPluginName = "step/e2e-test"
const E2EStepPluginNameDisabled = "step/e2e-test-disabled"

type StepPluginSuite struct {
	fixtures.E2ESuite
}

func TestStepPluginSuite(t *testing.T) {
	suite.Run(t, new(StepPluginSuite))
}

func (s *StepPluginSuite) SetupSuite() {
	s.E2ESuite.SetupSuite()
	if !IsStepPluginConfigured(&s.Common, s.GetControllerConfig()) {
		s.T().SkipNow()
	}
}

// getControllerConfiguredPlugin look at the controller default config map to find the list of plugins
// This is a best effort because it does not mean the controller have these plugins configured in-memory
func IsStepPluginConfigured(c *fixtures.Common, config *corev1.ConfigMap) bool {
	var stepPlugins []types.PluginItem
	if err := yaml.Unmarshal([]byte(config.Data["stepPlugins"]), &stepPlugins); err != nil {
		c.CheckError(err)
	}

	hasPlugin := false
	hasPluginDisabled := false
	for _, p := range stepPlugins {
		if p.Name == E2EStepPluginName {
			hasPlugin = true
		}
		if p.Name == E2EStepPluginNameDisabled {
			hasPluginDisabled = true
		}
	}
	return hasPlugin && hasPluginDisabled
}

// test step plugin ignored when disabled
// test rollout error when step plugin not configured

func (s *StepPluginSuite) TestRolloutCompletesWhenStepSuccessful() {
	s.Given().
		RolloutTemplate("@step-plugin/template-rollout.yaml", map[string]string{"$$name$$": "successful", "$$phase$$": string(types.PhaseSuccessful)}).
		When().
		ApplyManifests().WaitForRolloutStatus("Healthy").
		UpdateSpec().WaitForRolloutStatus("Healthy").
		Then().
		ExpectStableRevision("2").
		Assert(func(t *fixtures.Then) {
			rollout := t.GetRollout()
			assert.EqualValues(s.T(), 2, len(rollout.Status.Canary.StepPluginStatuses))

			stepStatus := rollout.Status.Canary.StepPluginStatuses[1]
			assert.EqualValues(s.T(), E2EStepPluginName, stepStatus.Name)
			assert.EqualValues(s.T(), 1, stepStatus.Index)
			assert.EqualValues(s.T(), v1alpha1.StepPluginPhaseSuccessful, stepStatus.Phase)
			assert.EqualValues(s.T(), v1alpha1.StepPluginOperationRun, stepStatus.Operation)
		})
}

func (s *StepPluginSuite) TestRolloutAbortWhenStepFails() {
	s.Given().
		RolloutTemplate("@step-plugin/template-rollout.yaml", map[string]string{"$$name$$": "failed", "$$phase$$": string(types.PhaseFailed)}).
		When().
		ApplyManifests().WaitForRolloutStatus("Healthy").
		UpdateSpec().WaitForRolloutStatus("Degraded").
		Then().
		ExpectStableRevision("1").
		Assert(func(t *fixtures.Then) {
			rollout := t.GetRollout()
			assert.True(s.T(), rollout.Status.Abort)
			assert.EqualValues(s.T(), 3, len(rollout.Status.Canary.StepPluginStatuses))

			stepStatus := rollout.Status.Canary.StepPluginStatuses[1]
			assert.EqualValues(s.T(), E2EStepPluginName, stepStatus.Name)
			assert.EqualValues(s.T(), 1, stepStatus.Index)
			assert.EqualValues(s.T(), v1alpha1.StepPluginPhaseFailed, stepStatus.Phase)
			assert.EqualValues(s.T(), v1alpha1.StepPluginOperationRun, stepStatus.Operation)

			stepStatus = rollout.Status.Canary.StepPluginStatuses[2]
			assert.EqualValues(s.T(), E2EStepPluginName, stepStatus.Name)
			assert.EqualValues(s.T(), 0, stepStatus.Index)
			assert.EqualValues(s.T(), v1alpha1.StepPluginPhaseSuccessful, stepStatus.Phase)
			assert.EqualValues(s.T(), v1alpha1.StepPluginOperationAbort, stepStatus.Operation)
		})
}

func (s *StepPluginSuite) TestRolloutAbortStepsWhenAborted() {
	s.Given().
		RolloutTemplate("@step-plugin/template-rollout.yaml", map[string]string{"$$name$$": "aborted", "$$phase$$": string(types.PhaseRunning)}).
		When().
		ApplyManifests().WaitForRolloutStatus("Healthy").
		UpdateSpec().WaitForRolloutStatus("Progressing").
		WaitForRolloutCanaryStepIndex(1).
		WaitForRolloutStepPluginRunning().
		AbortRollout().WaitForRolloutStatus("Degraded").
		Then().
		Assert(func(t *fixtures.Then) {
			rollout := t.GetRollout()
			assert.True(s.T(), rollout.Status.Abort)
			assert.EqualValues(s.T(), 4, len(rollout.Status.Canary.StepPluginStatuses))

			stepStatus := rollout.Status.Canary.StepPluginStatuses[1]
			assert.EqualValues(s.T(), E2EStepPluginName, stepStatus.Name)
			assert.EqualValues(s.T(), 1, stepStatus.Index)
			assert.EqualValues(s.T(), v1alpha1.StepPluginPhaseRunning, stepStatus.Phase)
			assert.EqualValues(s.T(), v1alpha1.StepPluginOperationRun, stepStatus.Operation)

			stepStatus = rollout.Status.Canary.StepPluginStatuses[2]
			assert.EqualValues(s.T(), E2EStepPluginName, stepStatus.Name)
			assert.EqualValues(s.T(), 1, stepStatus.Index)
			assert.EqualValues(s.T(), v1alpha1.StepPluginPhaseSuccessful, stepStatus.Phase)
			assert.EqualValues(s.T(), v1alpha1.StepPluginOperationAbort, stepStatus.Operation)

			stepStatus = rollout.Status.Canary.StepPluginStatuses[3]
			assert.EqualValues(s.T(), E2EStepPluginName, stepStatus.Name)
			assert.EqualValues(s.T(), 0, stepStatus.Index)
			assert.EqualValues(s.T(), v1alpha1.StepPluginPhaseSuccessful, stepStatus.Phase)
			assert.EqualValues(s.T(), v1alpha1.StepPluginOperationAbort, stepStatus.Operation)
		})
}

func (s *StepPluginSuite) TestRolloutCompletesWhenPromotedAndStepRunning() {
	s.Given().
		RolloutTemplate("@step-plugin/template-rollout.yaml", map[string]string{"$$name$$": "full-promotion", "$$phase$$": string(types.PhaseRunning)}).
		When().
		ApplyManifests().WaitForRolloutStatus("Healthy").
		UpdateSpec().WaitForRolloutStatus("Progressing").
		WaitForRolloutCanaryStepIndex(1).
		WaitForRolloutStepPluginRunning().
		PromoteRolloutFull().WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			rollout := t.GetRollout()
			assert.EqualValues(s.T(), 3, len(rollout.Status.Canary.StepPluginStatuses))

			stepStatus := rollout.Status.Canary.StepPluginStatuses[1]
			assert.EqualValues(s.T(), E2EStepPluginName, stepStatus.Name)
			assert.EqualValues(s.T(), 1, stepStatus.Index)
			assert.EqualValues(s.T(), v1alpha1.StepPluginPhaseRunning, stepStatus.Phase)
			assert.EqualValues(s.T(), v1alpha1.StepPluginOperationRun, stepStatus.Operation)

			stepStatus = rollout.Status.Canary.StepPluginStatuses[2]
			assert.EqualValues(s.T(), E2EStepPluginName, stepStatus.Name)
			assert.EqualValues(s.T(), 1, stepStatus.Index)
			assert.EqualValues(s.T(), v1alpha1.StepPluginPhaseSuccessful, stepStatus.Phase)
			assert.EqualValues(s.T(), v1alpha1.StepPluginOperationTerminate, stepStatus.Operation)
		})
}

func (s *StepPluginSuite) TestRolloutRetriesUntilDeadlineWhenRunning() {
	s.Given().
		RolloutTemplate("@step-plugin/template-rollout.yaml", map[string]string{"$$name$$": "running", "$$phase$$": string(types.PhaseRunning)}).
		When().ApplyManifests().WaitForRolloutStatus("Healthy").
		UpdateSpec(`
spec:
  progressDeadlineSeconds: 15`).
		UpdateSpec().WaitForRolloutStatus("Progressing").
		WaitForRolloutCanaryStepIndex(1).
		WaitForRolloutStepPluginRunning().
		Wait(20 * time.Second).
		WaitForRolloutStatus("Degraded").
		Then().
		Assert(func(t *fixtures.Then) {
			rollout := t.GetRollout()
			assert.EqualValues(s.T(), 1, *rollout.Status.CurrentStepIndex)
			assert.EqualValues(s.T(), 2, len(rollout.Status.Canary.StepPluginStatuses))

			stepStatus := rollout.Status.Canary.StepPluginStatuses[1]
			assert.EqualValues(s.T(), E2EStepPluginName, stepStatus.Name)
			assert.EqualValues(s.T(), 1, stepStatus.Index)
			assert.EqualValues(s.T(), v1alpha1.StepPluginPhaseRunning, stepStatus.Phase)
			assert.EqualValues(s.T(), v1alpha1.StepPluginOperationRun, stepStatus.Operation)
			assert.NotEmpty(s.T(), stepStatus.Status)

		})
}

func (s *StepPluginSuite) TestRolloutRetriesUntilDeadlineWhenError() {
	s.Given().
		RolloutTemplate("@step-plugin/template-rollout.yaml", map[string]string{"$$name$$": "error", "$$phase$$": "invalidPhaseCausingPluginError"}).
		When().ApplyManifests().WaitForRolloutStatus("Healthy").
		UpdateSpec(`
spec:
  progressDeadlineSeconds: 15`).
		UpdateSpec().WaitForRolloutStatus("Progressing").
		Wait(20 * time.Second).
		WaitForRolloutStatus("Degraded").
		Then().
		Assert(func(t *fixtures.Then) {
			rollout := t.GetRollout()
			assert.EqualValues(s.T(), 1, *rollout.Status.CurrentStepIndex)
			assert.EqualValues(s.T(), 2, len(rollout.Status.Canary.StepPluginStatuses))

			stepStatus := rollout.Status.Canary.StepPluginStatuses[1]
			assert.EqualValues(s.T(), E2EStepPluginName, stepStatus.Name)
			assert.EqualValues(s.T(), 1, stepStatus.Index)
			assert.EqualValues(s.T(), v1alpha1.StepPluginPhaseError, stepStatus.Phase)
			assert.EqualValues(s.T(), v1alpha1.StepPluginOperationRun, stepStatus.Operation)
			assert.Contains(s.T(), stepStatus.Message, "phase 'invalidPhaseCausingPluginError' is not valid")
			assert.Empty(s.T(), stepStatus.Status)

		})
}

func (s *StepPluginSuite) TestRolloutStatusIsNotUsedOnNewRollout() {
	s.Given().
		RolloutTemplate("@step-plugin/template-rollout.yaml", map[string]string{"$$name$$": "run-again", "$$phase$$": string(types.PhaseSuccessful)}).
		When().
		ApplyManifests().WaitForRolloutStatus("Healthy").
		UpdateSpec().WaitForRolloutStatus("Healthy").Then().ExpectStableRevision("2").When().
		UpdateSpec(`
spec:
  strategy:
    canary:
      steps:
      - plugin:
          name: step/e2e-test
          config:
            return: Successful`).
		UpdateSpec().WaitForRolloutStatus("Healthy").
		Then().
		ExpectStableRevision("3").
		Assert(func(t *fixtures.Then) {
			rollout := t.GetRollout()
			assert.EqualValues(s.T(), 1, len(rollout.Status.Canary.StepPluginStatuses))

			stepStatus := rollout.Status.Canary.StepPluginStatuses[0]
			assert.EqualValues(s.T(), E2EStepPluginName, stepStatus.Name)
			assert.EqualValues(s.T(), 0, stepStatus.Index)
			assert.EqualValues(s.T(), v1alpha1.StepPluginPhaseSuccessful, stepStatus.Phase)
			assert.EqualValues(s.T(), v1alpha1.StepPluginOperationRun, stepStatus.Operation)
		})
}

func (s *StepPluginSuite) TestRolloutErrorWhenStepPluginNotConfigured() {
	s.Given().
		RolloutObjects("@step-plugin/invalid-rollout.yaml").
		When().
		ApplyManifests().WaitForRolloutStatus("Healthy").
		UpdateSpec().WaitForRolloutStatus("Degraded").
		Then().
		ExpectStableRevision("1").
		Assert(func(t *fixtures.Then) {
			rollout := t.GetRollout()
			assert.EqualValues(s.T(), 0, *rollout.Status.CurrentStepIndex)
			assert.EqualValues(s.T(), 0, len(rollout.Status.Canary.StepPluginStatuses))
			// Should really have some error returned to the user...
		})
}

func (s *StepPluginSuite) TestRolloutSkipPluginWhenDisabled() {
	s.Given().
		RolloutTemplate("@step-plugin/disabled-rollout.yaml", map[string]string{"$$disabled_plugin$$": E2EStepPluginNameDisabled}).
		When().
		ApplyManifests().WaitForRolloutStatus("Healthy").
		UpdateSpec().WaitForRolloutStatus("Healthy").
		Then().
		ExpectStableRevision("2").
		Assert(func(t *fixtures.Then) {
			rollout := t.GetRollout()
			assert.EqualValues(s.T(), 1, len(rollout.Status.Canary.StepPluginStatuses))

			stepStatus := rollout.Status.Canary.StepPluginStatuses[0]
			assert.EqualValues(s.T(), E2EStepPluginName, stepStatus.Name)
			assert.EqualValues(s.T(), 1, stepStatus.Index)
			assert.EqualValues(s.T(), v1alpha1.StepPluginPhaseSuccessful, stepStatus.Phase)
			assert.EqualValues(s.T(), v1alpha1.StepPluginOperationRun, stepStatus.Operation)
		})
}
