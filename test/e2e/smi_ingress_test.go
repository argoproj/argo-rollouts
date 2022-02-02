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

type SMIIngressSuite struct {
	fixtures.E2ESuite
}

func TestSMIIngressSuite(t *testing.T) {
	suite.Run(t, new(SMIIngressSuite))
}

func (s *SMIIngressSuite) SetupSuite() {
	s.E2ESuite.SetupSuite()
	if !s.SMIEnabled {
		s.T().SkipNow()
	}
}

func (s *SMIIngressSuite) TestSMIIngressCanaryStep() {
	const (
		canaryService          = "rollout-smi-ingress-canary-canary"
		stableService          = "rollout-smi-ingress-canary-stable"
		canaryAnnotation       = "nginx.ingress.kubernetes.io/canary"
		canaryWeightAnnotation = "nginx.ingress.kubernetes.io/canary-weight"
	)
	s.Given().
		RolloutObjects("@smi_ingress/rollout-smi-ingress-canary.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ts := t.GetTrafficSplit()

			assert.Len(s.T(), ts.Spec.Backends, 2)
			assert.Equal(s.T(), canaryService, ts.Spec.Backends[0].Service)
			assert.Equal(s.T(), int64(0), ts.Spec.Backends[0].Weight.Value())

			ingressStable := t.GetNginxIngressStable()
			_, ko := ingressStable.Annotations[canaryAnnotation]
			assert.False(s.T(), ko)

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

			assert.Len(s.T(), ts.Spec.Backends, 2)

			assert.Equal(s.T(), canaryService, ts.Spec.Backends[0].Service)
			assert.Equal(s.T(), int64(5), ts.Spec.Backends[0].Weight.Value())

			ingressCanary := t.GetNginxIngressCanary()
			_, ok := ingressCanary.Annotations[canaryAnnotation]
			assert.True(s.T(), ok)
			assert.Equal(s.T(), string("5"), ingressCanary.Annotations[canaryWeightAnnotation])

			ingressStable := t.GetNginxIngressStable()
			_, ko := ingressStable.Annotations[canaryAnnotation]
			assert.False(s.T(), ko)

			assert.Equal(s.T(), stableService, ts.Spec.Backends[1].Service)
			assert.Equal(s.T(), int64(95), ts.Spec.Backends[1].Weight.Value())

			desired, stable := t.GetServices()
			rs1 := t.GetReplicaSetByRevision("1")
			rs2 := t.GetReplicaSetByRevision("2")
			assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], desired.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
			assert.Equal(s.T(), rs1.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], stable.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
		}).
		ExpectExperimentCount(0).
		When().
		WaitForRolloutCanaryStepIndex(2).
		Sleep(3*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ts := t.GetTrafficSplit()

			assert.Len(s.T(), ts.Spec.Backends, 2)

			assert.Equal(s.T(), canaryService, ts.Spec.Backends[0].Service)
			assert.Equal(s.T(), int64(50), ts.Spec.Backends[0].Weight.Value())

			ingressCanary := t.GetNginxIngressCanary()
			_, ok := ingressCanary.Annotations[canaryAnnotation]
			assert.True(s.T(), ok)
			assert.Equal(s.T(), string("50"), ingressCanary.Annotations[canaryWeightAnnotation])

			ingressStable := t.GetNginxIngressStable()
			_, ko := ingressStable.Annotations[canaryAnnotation]
			assert.False(s.T(), ko)

			assert.Equal(s.T(), stableService, ts.Spec.Backends[1].Service)
			assert.Equal(s.T(), int64(50), ts.Spec.Backends[1].Weight.Value())

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

			ingressCanary := t.GetNginxIngressCanary()
			_, ko := ingressCanary.Annotations[canaryAnnotation]
			assert.True(s.T(), ko)
			assert.Equal(s.T(), string("0"), ingressCanary.Annotations[canaryWeightAnnotation])

			assert.Equal(s.T(), canaryService, ts.Spec.Backends[0].Service)
			assert.Equal(s.T(), int64(0), ts.Spec.Backends[0].Weight.Value())

			assert.Equal(s.T(), stableService, ts.Spec.Backends[1].Service)
			assert.Equal(s.T(), int64(100), ts.Spec.Backends[1].Weight.Value())

			desired, stable := t.GetServices()
			rs2 := t.GetReplicaSetByRevision("2")
			assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], desired.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
			assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], stable.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
		}).
		ExpectRevisionPodCount("1", 1) // don't scale down old replicaset since it will be within scaleDownDelay
}
