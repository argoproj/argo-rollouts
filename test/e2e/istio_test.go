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

	tests := []struct {
		filename string
		hasTls   bool
	}{
		{
			"@istio/istio-host-split.yaml",
			false,
		},
		{
			"@istio/istio-host-http-tls-split.yaml",
			true,
		},
	}

	for _, tc := range tests {

		s.Given().
			RolloutObjects(tc.filename).
			When().
			ApplyManifests().
			WaitForRolloutStatus("Healthy").
			Then().
			Assert(func(t *fixtures.Then) {
				vsvc := t.GetVirtualService()
				assert.Equal(s.T(), int64(100), vsvc.Spec.HTTP[0].Route[0].Weight)
				assert.Equal(s.T(), int64(0), vsvc.Spec.HTTP[0].Route[1].Weight)
				if tc.hasTls {
					assert.Equal(s.T(), int64(100), vsvc.Spec.TLS[0].Route[0].Weight)
					assert.Equal(s.T(), int64(0), vsvc.Spec.TLS[0].Route[1].Weight)
				}

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
				if tc.hasTls {
					assert.Equal(s.T(), int64(90), vsvc.Spec.TLS[0].Route[0].Weight)
					assert.Equal(s.T(), int64(10), vsvc.Spec.TLS[0].Route[1].Weight)
				}

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
				if tc.hasTls {
					assert.Equal(s.T(), int64(100), vsvc.Spec.TLS[0].Route[0].Weight)
					assert.Equal(s.T(), int64(0), vsvc.Spec.TLS[0].Route[1].Weight)
				}

				desired, stable := t.GetServices()
				rs2 := t.GetReplicaSetByRevision("2")
				assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], desired.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
				assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], stable.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
			}).
			ExpectRevisionPodCount("1", 1) // don't scale down old replicaset since it will be within scaleDownDelay

		s.TearDownSuite()
	}
}

func (s *IstioSuite) TestIstioHostSplitOnlyTls() {

	tests := []struct {
		filename string
	}{
		{
			"@istio/istio-host-only-tls-split.yaml",
		},
	}

	for _, tc := range tests {

		s.Given().
			RolloutObjects(tc.filename).
			When().
			ApplyManifests().
			WaitForRolloutStatus("Healthy").
			Then().
			Assert(func(t *fixtures.Then) {
				vsvc := t.GetVirtualService()
				assert.Equal(s.T(), int64(100), vsvc.Spec.TLS[0].Route[0].Weight)
				assert.Equal(s.T(), int64(0), vsvc.Spec.TLS[0].Route[1].Weight)
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
				assert.Equal(s.T(), int64(90), vsvc.Spec.TLS[0].Route[0].Weight)
				assert.Equal(s.T(), int64(10), vsvc.Spec.TLS[0].Route[1].Weight)

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
				assert.Equal(s.T(), int64(100), vsvc.Spec.TLS[0].Route[0].Weight)
				assert.Equal(s.T(), int64(0), vsvc.Spec.TLS[0].Route[1].Weight)

				desired, stable := t.GetServices()
				rs2 := t.GetReplicaSetByRevision("2")
				assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], desired.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
				assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], stable.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
			}).
			ExpectRevisionPodCount("1", 1) // don't scale down old replicaset since it will be within scaleDownDelay

		s.TearDownSuite()
	}
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
		WaitForRolloutStatus("Healthy").
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
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectRevisionPodCount("2", 1)
}

func (s *IstioSuite) TestIstioUpdateInMiddleZeroCanaryReplicas() {
	s.Given().
		RolloutObjects("@istio/istio-host-split-update-in-middle.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		ExpectRevisionPodCount("2", 1).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		ExpectRevisionPodCount("3", 1)
}

func (s *IstioSuite) TestIstioAbortUpdateDeleteAllCanaryPods() {
	s.Given().
		RolloutObjects("@istio/istio-rollout-abort-delete-all-canary-pods.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		ExpectRevisionPodCount("2", 2).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Paused").
		Then().
		ExpectRevisionPodCount("2", 4).
		When().
		AbortRollout().
		WaitForRolloutStatus("Degraded").
		Then().
		ExpectRevisionPodCount("1", 5).
		ExpectRevisionPodCount("2", 4).    // canary pods remained scaled
		ExpectRevisionScaleDown("2", true) // but have a scale down delay
}

func (s *IstioSuite) TestIstioHostSplitExperimentStep() {
	s.Given().
		RolloutObjects("@istio/istio-host-split-experiment-step.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), int64(100), vsvc.Spec.HTTP[0].Route[0].Weight)
			assert.Equal(s.T(), "istio-host-split-stable", vsvc.Spec.HTTP[0].Route[0].Destination.Host)
			assert.Equal(s.T(), int64(0), vsvc.Spec.HTTP[0].Route[1].Weight)
			assert.Equal(s.T(), "istio-host-split-canary", vsvc.Spec.HTTP[0].Route[1].Destination.Host)

			desired, stable := t.GetServices()
			rs1 := t.GetReplicaSetByRevision("1")
			assert.Equal(s.T(), rs1.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], desired.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
			assert.Equal(s.T(), rs1.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], stable.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
		}).
		When().
		UpdateSpec().
		WaitForRolloutCanaryStepIndex(1).
		Sleep(10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), int64(70), vsvc.Spec.HTTP[0].Route[0].Weight)
			assert.Equal(s.T(), "istio-host-split-stable", vsvc.Spec.HTTP[0].Route[0].Destination.Host)

			assert.Equal(s.T(), int64(10), vsvc.Spec.HTTP[0].Route[1].Weight)
			assert.Equal(s.T(), "istio-host-split-canary", vsvc.Spec.HTTP[0].Route[1].Destination.Host)

			assert.Equal(s.T(), int64(20), vsvc.Spec.HTTP[0].Route[2].Weight)
			ex := t.GetRolloutExperiments().Items[0]
			exServiceName := ex.Status.TemplateStatuses[0].ServiceName
			assert.Equal(s.T(), exServiceName, vsvc.Spec.HTTP[0].Route[2].Destination.Host)

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
			assert.Equal(s.T(), "istio-host-split-stable", vsvc.Spec.HTTP[0].Route[0].Destination.Host)

			assert.Equal(s.T(), int64(0), vsvc.Spec.HTTP[0].Route[1].Weight)
			assert.Equal(s.T(), "istio-host-split-canary", vsvc.Spec.HTTP[0].Route[1].Destination.Host)

			desired, stable := t.GetServices()
			rs2 := t.GetReplicaSetByRevision("2")
			assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], desired.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
			assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], stable.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey])
		}).
		ExpectRevisionPodCount("1", 1) // don't scale down old replicaset since it will be within scaleDownDelay

	s.TearDownSuite()
}

func (s *IstioSuite) TestIstioSubsetSplitExperimentStep() {
	s.Given().
		RolloutObjects("@istio/istio-subset-split-experiment-step.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), int64(100), vsvc.Spec.HTTP[0].Route[0].Weight) // stable
			assert.Equal(s.T(), int64(0), vsvc.Spec.HTTP[0].Route[1].Weight)   // canary

			rs1 := t.GetReplicaSetByRevision("1")
			destrule := t.GetDestinationRule()
			assert.Len(s.T(), destrule.Spec.Subsets, 2)
			assert.Equal(s.T(), rs1.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], destrule.Spec.Subsets[0].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]) // stable
			assert.Equal(s.T(), rs1.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], destrule.Spec.Subsets[1].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]) // canary

		}).
		When().
		UpdateSpec().
		WaitForRolloutCanaryStepIndex(1).
		Sleep(10*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), int64(70), vsvc.Spec.HTTP[0].Route[0].Weight) // stable
			assert.Equal(s.T(), int64(10), vsvc.Spec.HTTP[0].Route[1].Weight) // canary
			assert.Equal(s.T(), int64(20), vsvc.Spec.HTTP[0].Route[2].Weight)
			ex := t.GetRolloutExperiments().Items[0]
			assert.Equal(s.T(), ex.Status.TemplateStatuses[0].ServiceName, vsvc.Spec.HTTP[0].Route[2].Destination.Host)

			rs1 := t.GetReplicaSetByRevision("1")
			rs2 := t.GetReplicaSetByRevision("2")
			destrule := t.GetDestinationRule()
			assert.Len(s.T(), destrule.Spec.Subsets, 3)
			assert.Equal(s.T(), rs1.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], destrule.Spec.Subsets[0].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]) // stable
			assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], destrule.Spec.Subsets[1].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]) // canary
			assert.Equal(s.T(), ex.Status.TemplateStatuses[0].PodTemplateHash, destrule.Spec.Subsets[2].Labels[v1alpha1.DefaultRolloutUniqueLabelKey])
		}).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Sleep(1*time.Second). // stable is currently set first, and then changes made to VirtualServices/DestinationRules
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), int64(100), vsvc.Spec.HTTP[0].Route[0].Weight) // stable
			assert.Equal(s.T(), int64(0), vsvc.Spec.HTTP[0].Route[1].Weight)   // canary

			destrule := t.GetDestinationRule()
			rs2 := t.GetReplicaSetByRevision("2")
			assert.Len(s.T(), destrule.Spec.Subsets, 2)
			assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], destrule.Spec.Subsets[0].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]) // stable
			assert.Equal(s.T(), rs2.Spec.Template.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], destrule.Spec.Subsets[1].Labels[v1alpha1.DefaultRolloutUniqueLabelKey]) // canary
		}).
		ExpectRevisionPodCount("1", 1) // don't scale down old replicaset since it will be within scaleDownDelay

	s.TearDownSuite()
}
