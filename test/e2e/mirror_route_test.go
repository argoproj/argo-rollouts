//go:build e2e
// +build e2e

package e2e

import (
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/istio"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/tj/assert"

	"github.com/argoproj/argo-rollouts/test/fixtures"
)

type MirrorRouteSuite struct {
	fixtures.E2ESuite
}

func TestMirrorRouteSuite(t *testing.T) {
	suite.Run(t, new(MirrorRouteSuite))
}

func (s *MirrorRouteSuite) SetupSuite() {
	s.E2ESuite.SetupSuite()
	if !s.IstioEnabled {
		s.T().SkipNow()
	}
}

func (s *MirrorRouteSuite) TestIstioHostMirrorRoute() {
	s.Given().
		RolloutObjects("@mirror-route/istio-mirror-host.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), "primary", vsvc.Spec.HTTP[0].Name)
		}).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Sleep(1 * time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), "mirror-route-1", vsvc.Spec.HTTP[0].Name)
			assert.Equal(s.T(), float64(100), vsvc.Spec.HTTP[0].MirrorPercentage.Value)
			assert.Equal(s.T(), "mirror-route-2", vsvc.Spec.HTTP[1].Name)
			assert.Equal(s.T(), float64(80), vsvc.Spec.HTTP[1].MirrorPercentage.Value)
			assertMirrorDestination(s, vsvc.Spec.HTTP[0], "stable-service", int64(80))
			assertMirrorDestination(s, vsvc.Spec.HTTP[0], "canary-service", int64(20))
			assertMirrorDestination(s, vsvc.Spec.HTTP[1], "stable-service", int64(80))
			assertMirrorDestination(s, vsvc.Spec.HTTP[1], "canary-service", int64(20))

			assertMirrorDestination(s, vsvc.Spec.HTTP[2], "stable-service", int64(80))
			assertMirrorDestination(s, vsvc.Spec.HTTP[2], "canary-service", int64(20))
		}).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Paused").
		Sleep(1 * time.Second).
		Then().
		When().
		PromoteRollout().
		WaitForRolloutStatus("Paused").
		Sleep(1 * time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), "mirror-route-2", vsvc.Spec.HTTP[0].Name)
			assertMirrorDestination(s, vsvc.Spec.HTTP[0], "stable-service", int64(60))
			assertMirrorDestination(s, vsvc.Spec.HTTP[0], "canary-service", int64(40))
			assert.Equal(s.T(), "primary", vsvc.Spec.HTTP[1].Name)
			assertMirrorDestination(s, vsvc.Spec.HTTP[1], "stable-service", int64(60))
			assertMirrorDestination(s, vsvc.Spec.HTTP[1], "canary-service", int64(40))
		}).
		When().
		PromoteRolloutFull().
		WaitForRolloutStatus("Healthy").
		Sleep(1 * time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			vsvc := t.GetVirtualService()
			assert.Equal(s.T(), 1, len(vsvc.Spec.HTTP))
			assertMirrorDestination(s, vsvc.Spec.HTTP[0], "stable-service", int64(100))
			assertMirrorDestination(s, vsvc.Spec.HTTP[0], "canary-service", int64(0))
		})
}

func assertMirrorDestination(s *MirrorRouteSuite, route istio.VirtualServiceHTTPRoute, service string, weight int64) {
	for _, destination := range route.Route {
		if destination.Destination.Host == service {
			assert.Equal(s.T(), weight, destination.Weight)
			return
		}
	}
	assert.Fail(s.T(), "Could not find the destination for service: %s", service)
}
