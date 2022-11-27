//go:build e2e
// +build e2e

package e2e

import (
	a6 "github.com/argoproj/argo-rollouts/rollout/trafficrouting/apisix"
	"github.com/argoproj/argo-rollouts/test/fixtures"
	"github.com/stretchr/testify/suite"
	"github.com/tj/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"testing"
	"time"
)

type APISIXSuite struct {
	fixtures.E2ESuite
}

func TestAPISIXSuite(t *testing.T) {
	suite.Run(t, new(APISIXSuite))
}

func (a *APISIXSuite) SetupSuite() {
	a.E2ESuite.SetupSuite()
	if !a.ApisixEnabled {
		a.T().SkipNow()
	}
}

func (s *APISIXSuite) TestAPISIXCanaryStep() {
	const (
		apisixRouteName = "rollouts-apisix"
		canaryService   = "rollout-apisix-canary-canary"
		stableService   = "rollout-apisix-canary-stable"
	)
	s.Given().
		RolloutObjects("@apisix/rollout-apisix-canary.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			ar := t.GetApisixRoute()
			assert.NotEmpty(s.T(), ar)
			apisixHttpRoutesObj, isFound, err := unstructured.NestedSlice(ar.Object, "spec", "http")
			assert.NoError(s.T(), err)
			assert.Equal(s.T(), isFound, true)
			apisixHttpRouteObj, err := a6.GetHttpRoute(apisixHttpRoutesObj, apisixRouteName)
			assert.NoError(s.T(), err)
			backends, err := a6.GetBackends(apisixHttpRouteObj)
			assert.NoError(s.T(), err)

			for _, backend := range backends {
				typedBackend, ok := backend.(map[string]interface{})
				assert.Equal(s.T(), ok, true)
				nameOfCurrentBackend, isFound, err := unstructured.NestedString(typedBackend, "serviceName")
				assert.NoError(s.T(), err)
				assert.Equal(s.T(), isFound, true)
				if nameOfCurrentBackend == stableService {
					rawWeight, ok := typedBackend["weight"]
					assert.Equal(s.T(), ok, true)
					weight, ok := rawWeight.(int64)
					assert.Equal(s.T(), ok, true)
					assert.Equal(s.T(), weight, int64(100))
				}
				if nameOfCurrentBackend == canaryService {
					rawWeight, ok := typedBackend["weight"]
					assert.Equal(s.T(), ok, true)
					weight, ok := rawWeight.(int64)
					assert.Equal(s.T(), ok, true)
					assert.Equal(s.T(), weight, int64(0))
				}
			}
		}).
		ExpectExperimentCount(0).
		When().
		UpdateSpec().
		WaitForRolloutCanaryStepIndex(1).
		Sleep(5*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ar := t.GetApisixRoute()
			assert.NotEmpty(s.T(), ar)
			apisixHttpRoutesObj, isFound, err := unstructured.NestedSlice(ar.Object, "spec", "http")
			assert.NoError(s.T(), err)
			assert.Equal(s.T(), isFound, true)
			apisixHttpRouteObj, err := a6.GetHttpRoute(apisixHttpRoutesObj, apisixRouteName)
			assert.NoError(s.T(), err)
			backends, err := a6.GetBackends(apisixHttpRouteObj)
			assert.NoError(s.T(), err)

			for _, backend := range backends {
				typedBackend, ok := backend.(map[string]interface{})
				assert.Equal(s.T(), ok, true)
				nameOfCurrentBackend, isFound, err := unstructured.NestedString(typedBackend, "serviceName")
				assert.NoError(s.T(), err)
				assert.Equal(s.T(), isFound, true)
				if nameOfCurrentBackend == stableService {
					rawWeight, ok := typedBackend["weight"]
					assert.Equal(s.T(), ok, true)
					weight, ok := rawWeight.(int64)
					assert.Equal(s.T(), ok, true)
					assert.Equal(s.T(), weight, int64(95))
				}
				if nameOfCurrentBackend == canaryService {
					rawWeight, ok := typedBackend["weight"]
					assert.Equal(s.T(), ok, true)
					weight, ok := rawWeight.(int64)
					assert.Equal(s.T(), ok, true)
					assert.Equal(s.T(), weight, int64(5))
				}
			}
		}).
		ExpectExperimentCount(0).
		When().
		WaitForRolloutCanaryStepIndex(2).
		Sleep(3*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			ar := t.GetApisixRoute()
			assert.NotEmpty(s.T(), ar)
			apisixHttpRoutesObj, isFound, err := unstructured.NestedSlice(ar.Object, "spec", "http")
			assert.NoError(s.T(), err)
			assert.Equal(s.T(), isFound, true)
			apisixHttpRouteObj, err := a6.GetHttpRoute(apisixHttpRoutesObj, apisixRouteName)
			assert.NoError(s.T(), err)
			backends, err := a6.GetBackends(apisixHttpRouteObj)
			assert.NoError(s.T(), err)

			for _, backend := range backends {
				typedBackend, ok := backend.(map[string]interface{})
				assert.Equal(s.T(), ok, true)
				nameOfCurrentBackend, isFound, err := unstructured.NestedString(typedBackend, "serviceName")
				assert.NoError(s.T(), err)
				assert.Equal(s.T(), isFound, true)
				if nameOfCurrentBackend == stableService {
					rawWeight, ok := typedBackend["weight"]
					assert.Equal(s.T(), ok, true)
					weight, ok := rawWeight.(int64)
					assert.Equal(s.T(), ok, true)
					assert.Equal(s.T(), weight, int64(50))
				}
				if nameOfCurrentBackend == canaryService {
					rawWeight, ok := typedBackend["weight"]
					assert.Equal(s.T(), ok, true)
					weight, ok := rawWeight.(int64)
					assert.Equal(s.T(), ok, true)
					assert.Equal(s.T(), weight, int64(50))
				}
			}
		}).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Sleep(1*time.Second). // stable is currently set first, and then changes made to VirtualServices/DestinationRules
		Then().
		Assert(func(t *fixtures.Then) {
			ar := t.GetApisixRoute()
			assert.NotEmpty(s.T(), ar)
			apisixHttpRoutesObj, isFound, err := unstructured.NestedSlice(ar.Object, "spec", "http")
			assert.NoError(s.T(), err)
			assert.Equal(s.T(), isFound, true)
			apisixHttpRouteObj, err := a6.GetHttpRoute(apisixHttpRoutesObj, apisixRouteName)
			assert.NoError(s.T(), err)
			backends, err := a6.GetBackends(apisixHttpRouteObj)
			assert.NoError(s.T(), err)

			for _, backend := range backends {
				typedBackend, ok := backend.(map[string]interface{})
				assert.Equal(s.T(), ok, true)
				nameOfCurrentBackend, isFound, err := unstructured.NestedString(typedBackend, "serviceName")
				assert.NoError(s.T(), err)
				assert.Equal(s.T(), isFound, true)
				if nameOfCurrentBackend == stableService {
					rawWeight, ok := typedBackend["weight"]
					assert.Equal(s.T(), ok, true)
					weight, ok := rawWeight.(int64)
					assert.Equal(s.T(), ok, true)
					assert.Equal(s.T(), weight, int64(100))
				}
				if nameOfCurrentBackend == canaryService {
					rawWeight, ok := typedBackend["weight"]
					assert.Equal(s.T(), ok, true)
					weight, ok := rawWeight.(int64)
					assert.Equal(s.T(), ok, true)
					assert.Equal(s.T(), weight, int64(0))
				}
			}
		}).
		ExpectRevisionPodCount("1", 1) // don't scale down old replicaset since it will be within scaleDownDelay
}
