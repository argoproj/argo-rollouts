//go:build e2e
// +build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/tj/assert"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/test/fixtures"
)

type SMISuite struct {
	fixtures.E2ESuite
}

func TestSMISuite(t *testing.T) {
	suite.Run(t, new(SMISuite))
}

func (s *SMISuite) SetupSuite() {
	s.E2ESuite.SetupSuite()
	if !s.SMIEnabled {
		s.T().SkipNow()
	}
}

func (s *SMISuite) TestSMIExperimentStep() {
	s.Given().
		RolloutObjects("@smi/rollout-smi-experiment.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ts := t.GetTrafficSplit()

			assert.Len(s.T(), ts.Spec.Backends, 2)

			assert.Equal(s.T(), "rollout-smi-experiment-canary", ts.Spec.Backends[0].Service)
			assert.Equal(s.T(), int64(0), ts.Spec.Backends[0].Weight.Value())

			assert.Equal(s.T(), "rollout-smi-experiment-stable", ts.Spec.Backends[1].Service)
			assert.Equal(s.T(), int64(100), ts.Spec.Backends[1].Weight.Value())

			desired, stable := t.GetServices()
			rs1 := t.GetReplicaSetByRevision("1")
			assert.Equal(s.T(), rs1.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], desired.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
			assert.Equal(s.T(), rs1.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], stable.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
		}).
		ExpectExperimentCount(0).
		When().
		UpdateSpec().
		WaitForRolloutCanaryStepIndex(1).
		Sleep(5*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ts := t.GetTrafficSplit()

			assert.Len(s.T(), ts.Spec.Backends, 3)

			assert.Equal(s.T(), "rollout-smi-experiment-canary", ts.Spec.Backends[0].Service)
			assert.Equal(s.T(), int64(5), ts.Spec.Backends[0].Weight.Value())

			ex := t.GetRolloutExperiments().Items[0]
			exServiceName := ex.Status.TemplateStatuses[0].ServiceName
			assert.Equal(s.T(), int64(5), ts.Spec.Backends[1].Weight.Value())
			assert.Equal(s.T(), exServiceName, ts.Spec.Backends[1].Service)

			assert.Equal(s.T(), "rollout-smi-experiment-stable", ts.Spec.Backends[2].Service)
			assert.Equal(s.T(), int64(90), ts.Spec.Backends[2].Weight.Value())

			desired, stable := t.GetServices()
			rs1 := t.GetReplicaSetByRevision("1")
			rs2 := t.GetReplicaSetByRevision("2")
			assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], desired.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
			assert.Equal(s.T(), rs1.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], stable.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
		}).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Sleep(1*time.Second). // stable is currently set first, and then changes made to VirtualServices/DestinationRules
		Then().
		Assert(func(t *fixtures.Then) {
			ts := t.GetTrafficSplit()

			assert.Len(s.T(), ts.Spec.Backends, 2)

			assert.Equal(s.T(), "rollout-smi-experiment-canary", ts.Spec.Backends[0].Service)
			assert.Equal(s.T(), int64(0), ts.Spec.Backends[0].Weight.Value())

			assert.Equal(s.T(), "rollout-smi-experiment-stable", ts.Spec.Backends[1].Service)
			assert.Equal(s.T(), int64(100), ts.Spec.Backends[1].Weight.Value())

			desired, stable := t.GetServices()
			rs2 := t.GetReplicaSetByRevision("2")
			assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], desired.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
			assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], stable.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
		}).
		ExpectRevisionPodCount("1", 1) // don't scale down old replicaset since it will be within scaleDownDelay
}
