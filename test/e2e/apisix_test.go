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

const (
	apisixRouteName     = "rollouts-apisix"
	apisixCanaryService = "rollout-apisix-canary-canary"
	apisixStableService = "rollout-apisix-canary-stable"
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

	s.Given().
		RolloutObjects("@apisix/rollout-apisix-canary.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			s.check(t, 100, 0)
		}).
		ExpectExperimentCount(0).
		When().
		UpdateSpec().
		WaitForRolloutCanaryStepIndex(1).
		Sleep(5*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			s.check(t, 95, 5)
		}).
		ExpectExperimentCount(0).
		When().
		WaitForRolloutCanaryStepIndex(2).
		Sleep(3*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			s.check(t, 50, 50)
		}).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Sleep(1*time.Second). // stable is currently set first, and then changes made to VirtualServices/DestinationRules
		Then().
		Assert(func(t *fixtures.Then) {
			s.check(t, 100, 0)
		}).
		ExpectRevisionPodCount("1", 1) // don't scale down old replicaset since it will be within scaleDownDelay
}

func (s *APISIXSuite) TestAPISIXCanarySetHeaderStep() {

	s.Given().
		RolloutObjects("@apisix/rollout-apisix-canary-set-header.yaml").
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			s.check(t, 100, 0)
		}).
		ExpectExperimentCount(0).
		When().
		UpdateSpec().
		WaitForRolloutCanaryStepIndex(0).
		Sleep(5*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			s.checkSetHeader(t, 0, 0)
		}).
		ExpectExperimentCount(0).
		When().
		WaitForRolloutCanaryStepIndex(3).
		Sleep(5*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			s.check(t, 95, 5)
		}).
		ExpectExperimentCount(0).
		When().
		WaitForRolloutCanaryStepIndex(5).
		Sleep(3*time.Second).
		Then().
		Assert(func(t *fixtures.Then) {
			s.check(t, 50, 50)
		}).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Sleep(1*time.Second). // stable is currently set first, and then changes made to VirtualServices/DestinationRules
		Then().
		Assert(func(t *fixtures.Then) {
			s.check(t, 100, 0)
		}).
		ExpectRevisionPodCount("1", 1) // don't scale down old replicaset since it will be within scaleDownDelay
}

func (s *APISIXSuite) check(t *fixtures.Then, stableWeight int64, canaryWeight int64) {
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
		if nameOfCurrentBackend == apisixStableService {
			rawWeight, ok := typedBackend["weight"]
			assert.Equal(s.T(), ok, true)
			weight, ok := rawWeight.(int64)
			assert.Equal(s.T(), ok, true)
			assert.Equal(s.T(), weight, stableWeight)
		}
		if nameOfCurrentBackend == apisixCanaryService {
			rawWeight, ok := typedBackend["weight"]
			assert.Equal(s.T(), ok, true)
			weight, ok := rawWeight.(int64)
			assert.Equal(s.T(), ok, true)
			assert.Equal(s.T(), weight, canaryWeight)
		}
	}
}

func (s *APISIXSuite) checkSetHeader(t *fixtures.Then, stableWeight int64, canaryWeight int64) {

	ar := t.GetApisixSetHeaderRoute()
	assert.NotEmpty(s.T(), ar)
	apisixHttpRoutesObj, isFound, err := unstructured.NestedSlice(ar.Object, "spec", "http")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), isFound, true)
	assert.Equal(s.T(), "set-header", ar.GetName())
	apisixHttpRouteObj, err := a6.GetHttpRoute(apisixHttpRoutesObj, apisixRouteName)
	assert.NoError(s.T(), err)

	exprs, isFound, err := unstructured.NestedSlice(apisixHttpRouteObj.(map[string]interface{}), "match", "exprs")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), isFound, true)

	assert.Equal(s.T(), 1, len(exprs))
	expr := exprs[0]

	exprObj, ok := expr.(map[string]interface{})
	assert.Equal(s.T(), ok, true)

	op, isFound, err := unstructured.NestedString(exprObj, "op")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), isFound, true)
	assert.Equal(s.T(), "Equal", op)

	name, isFound, err := unstructured.NestedString(exprObj, "subject", "name")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), isFound, true)
	assert.Equal(s.T(), "trace", name)

	scope, isFound, err := unstructured.NestedString(exprObj, "subject", "scope")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), isFound, true)
	assert.Equal(s.T(), "Header", scope)

	value, isFound, err := unstructured.NestedString(exprObj, "value")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), isFound, true)
	assert.Equal(s.T(), "debug", value)
}
