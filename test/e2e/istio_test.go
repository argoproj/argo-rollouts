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

type IstioSuite struct {
	fixtures.E2ESuite
}

func TestIstioSuite(t *testing.T) {
	suite.Run(t, new(IstioSuite))
}

func (s *IstioSuite) SetupSuite() {
	s.E2ESuite.SetupSuite()
	if !s.IstioEnabled {
		s.T().SkipNow()
	}
}

func (s *IstioSuite) TestIstioHostSplit() {
	s.Given().
		RolloutObjects("@istio/istio-host-split.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), int64(100), vsvc.Spec.HTTP[0].Route[0].Weight)
			assert.Equal(s.T(), int64(0), vsvc.Spec.HTTP[0].Route[1].Weight)

			desired, stable := t.GetServices()
			rs1 := t.GetReplicaSetByRevision("1")
			assert.Equal(s.T(), rs1.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], desired.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
			assert.Equal(s.T(), rs1.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], stable.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
		}).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), int64(90), vsvc.Spec.HTTP[0].Route[0].Weight)
			assert.Equal(s.T(), int64(10), vsvc.Spec.HTTP[0].Route[1].Weight)

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
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), int64(100), vsvc.Spec.HTTP[0].Route[0].Weight)
			assert.Equal(s.T(), int64(0), vsvc.Spec.HTTP[0].Route[1].Weight)

			desired, stable := t.GetServices()
			rs2 := t.GetReplicaSetByRevision("2")
			assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], desired.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
			assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], stable.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
		}).
		ExpectRevisionPodCount("1", 1) // don't scale down old replicaset since it will be within scaleDownDelay
}

func (s *IstioSuite) TestIstioSubsetSplit() {
	s.Given().
		RolloutObjects("@istio/istio-subset-split.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), int64(100), vsvc.Spec.HTTP[0].Route[0].Weight)
			assert.Equal(s.T(), int64(0), vsvc.Spec.HTTP[0].Route[1].Weight)

			rs1 := t.GetReplicaSetByRevision("1")
			destrule := t.GetDestinationRule()
			assert.Equal(s.T(), rs1.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], destrule.Spec.Subsets[0].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]) // stable
			assert.Equal(s.T(), rs1.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], destrule.Spec.Subsets[1].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]) // canary
		}).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), int64(90), vsvc.Spec.HTTP[0].Route[0].Weight)
			assert.Equal(s.T(), int64(10), vsvc.Spec.HTTP[0].Route[1].Weight)

			rs1 := t.GetReplicaSetByRevision("1")
			rs2 := t.GetReplicaSetByRevision("2")
			destrule := t.GetDestinationRule()
			assert.Equal(s.T(), rs1.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], destrule.Spec.Subsets[0].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]) // stable
			assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], destrule.Spec.Subsets[1].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]) // canary
		}).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Sleep(1*time.Second). // stable is currently set first, and then changes made to VirtualServices/DestinationRules
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), int64(100), vsvc.Spec.HTTP[0].Route[0].Weight)
			assert.Equal(s.T(), int64(0), vsvc.Spec.HTTP[0].Route[1].Weight)

			rs2 := t.GetReplicaSetByRevision("2")
			destrule := t.GetDestinationRule()
			assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], destrule.Spec.Subsets[0].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]) // stable
			assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], destrule.Spec.Subsets[1].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]) // canary
		}).
		ExpectRevisionPodCount("1", 1). // don't scale down old replicaset since it will be within scaleDownDelay
		When().
		// Verify we remove the injections on the DestinationRule when a rollout no longer references it
		UpdateSpec(`
spec:
  strategy:
    canary:
      trafficRouting: null
`).
		Sleep(2*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			destrule := t.GetDestinationRule()
			_, ok := destrule.Annotations[v1alpha1.ManagedByRolloutsKey]
			assert.False(s.T(), ok)
			_, ok = destrule.Spec.Subsets[0].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
			assert.False(s.T(), ok)
			_, ok = destrule.Spec.Subsets[1].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
			assert.False(s.T(), ok)
		}).
		ExpectRevisionPodCount("1", 0) // since we moved back to basic canary, we should scale down older RSs
}

func (s *IstioSuite) TestIstioSubsetSplitSingleRoute() {
	s.Given().
		RolloutObjects("@istio/istio-subset-split-single-route.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), int64(100), vsvc.Spec.HTTP[0].Route[0].Weight)
			assert.Equal(s.T(), int64(0), vsvc.Spec.HTTP[0].Route[1].Weight)

			rs1 := t.GetReplicaSetByRevision("1")
			destrule := t.GetDestinationRule()
			assert.Equal(s.T(), rs1.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], destrule.Spec.Subsets[0].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]) // stable
			assert.Equal(s.T(), rs1.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], destrule.Spec.Subsets[1].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]) // canary
		}).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), int64(90), vsvc.Spec.HTTP[0].Route[0].Weight)
			assert.Equal(s.T(), int64(10), vsvc.Spec.HTTP[0].Route[1].Weight)

			rs1 := t.GetReplicaSetByRevision("1")
			rs2 := t.GetReplicaSetByRevision("2")
			destrule := t.GetDestinationRule()
			assert.Equal(s.T(), rs1.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], destrule.Spec.Subsets[0].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]) // stable
			assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], destrule.Spec.Subsets[1].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]) // canary
		}).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Sleep(1*time.Second). // stable is currently set first, and then changes made to VirtualServices/DestinationRules
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), int64(100), vsvc.Spec.HTTP[0].Route[0].Weight)
			assert.Equal(s.T(), int64(0), vsvc.Spec.HTTP[0].Route[1].Weight)

			rs2 := t.GetReplicaSetByRevision("2")
			destrule := t.GetDestinationRule()
			assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], destrule.Spec.Subsets[0].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]) // stable
			assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], destrule.Spec.Subsets[1].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]) // canary
		}).
		ExpectRevisionPodCount("1", 1) // don't scale down old replicaset since it will be within scaleDownDelay
}

func (s *IstioSuite) TestIstioAbortUpdate() {
	s.Given().
		RolloutObjects("@istio/istio-host-split.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		When().
		AbortRollout().
		WaitForRolloutStatus("Degraded").
		Then().
		ExpectRevisionPodCount("1", 1).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Then().
		When().
		AbortRollout().
		WaitForRolloutStatus("Degraded").
		Then().
		ExpectRevisionPodCount("2", 1)
}
