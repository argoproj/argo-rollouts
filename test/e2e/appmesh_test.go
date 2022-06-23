// +build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/test/fixtures"
	"github.com/stretchr/testify/suite"
	"github.com/tj/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type AppMeshSuite struct {
	fixtures.E2ESuite
}

func TestAppMeshSuite(t *testing.T) {
	suite.Run(t, new(AppMeshSuite))
}

func (s *AppMeshSuite) SetupSuite() {
	s.E2ESuite.SetupSuite()
	if !s.AppMeshEnabled {
		s.T().SkipNow()
	}
}

func (s *AppMeshSuite) TestAppMeshCanaryRollout() {
	s.Given().
		RolloutObjects(`@appmesh/appmesh-canary-rollout.yaml`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		Sleep(1 * time.Second).
		Then().
		//Before rollout canary should be 0 weight
		Assert(func(t *fixtures.Then) {
			uVr := t.GetAppMeshVirtualRouter()
			canaryWeight := int64(0)
			s.assertWeightedTargets(uVr, canaryWeight)
		}).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Sleep(1 * time.Second).
		Then().
		//During deployment canary should increment to stepWeight
		Assert(func(t *fixtures.Then) {
			uVr := t.GetAppMeshVirtualRouter()
			canaryWeight := int64(*(t.Rollout().Spec.Strategy.Canary.Steps[0].SetWeight))
			s.assertWeightedTargets(uVr, canaryWeight)
		}).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy")
}

func (s *AppMeshSuite) assertWeightedTargets(uVr *unstructured.Unstructured, canaryWeight int64) {
	wtMap := s.getWeightedTargets(uVr)
	for routeName, wt := range wtMap {
		assert.Equal(s.T(), canaryWeight, wt.canaryWeight, fmt.Sprintf("Route %s has wrong weight for canary", routeName))
		assert.Equal(s.T(), 100-canaryWeight, wt.stableWeight, fmt.Sprintf("Route %s has wrong weight for stable", routeName))
	}
}

func (s *AppMeshSuite) getWeightedTargets(uVr *unstructured.Unstructured) map[string]weightedTargets {
	result := make(map[string]weightedTargets)
	routesI, _, _ := unstructured.NestedSlice(uVr.Object, "spec", "routes")
	for _, rI := range routesI {
		route, _ := rI.(map[string]interface{})
		routeName, _ := route["name"].(string)
		wtsI, _, _ := unstructured.NestedSlice(route, "httpRoute", "action", "weightedTargets")
		wtStruct := weightedTargets{}
		for _, wtI := range wtsI {
			wt, _ := wtI.(map[string]interface{})
			vnodeName, _, _ := unstructured.NestedString(wt, "virtualNodeRef", "name")
			weight, _, _ := unstructured.NestedInt64(wt, "weight")
			fmt.Printf("Found wt %+v with vnodeName (%s), weight (%d)", wt, vnodeName, weight)
			if strings.Contains(vnodeName, "canary") {
				wtStruct.canaryWeight = weight
			} else {
				wtStruct.stableWeight = weight
			}
		}
		result[routeName] = wtStruct
	}
	return result
}

type weightedTargets struct {
	canaryWeight int64
	stableWeight int64
}
