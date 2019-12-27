package istio

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/stretchr/testify/assert"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func strToUnstructured(yamlStr string) *unstructured.Unstructured {
	obj := make(map[string]interface{})
	yamlStr = strings.ReplaceAll(yamlStr, "\t", "    ")
	err := yaml.Unmarshal([]byte(yamlStr), &obj)
	if err != nil {
		panic(err)
	}
	return &unstructured.Unstructured{Object: obj}
}

func rollout(stableSvc, canarySvc, vsvc string, routes []string) *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rollout",
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService: stableSvc,
					CanaryService: canarySvc,
					Networking: &v1alpha1.RolloutNetworking{
						Istio: &v1alpha1.IstioNetworking{
							VirtualService: v1alpha1.IstioVirtualService{
								Name:   vsvc,
								Routes: routes,
							},
						},
					},
				},
			},
		},
	}
}

func checkDestination(t *testing.T, route map[string]interface{}, svc string, expectWeight int) {
	destinations := route["route"].([]interface{})
	routeName := route["name"].(string)
	for _, elem := range destinations {
		destination := elem.(map[string]interface{})
		if destination["destination"].(map[string]interface{})["host"] == svc {
			assert.Equal(t, expectWeight, int(destination["weight"].(float64)))
			return
		}
	}
	msg := fmt.Sprintf("Service '%s' not found within hosts of route '%s'", svc, routeName)
	assert.Fail(t, msg)
}

const regularVsvc = `apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: vsvc
  namespace: default
spec:
  gateways:
  - istio-rollout-gateway
  hosts:
  - istio-rollout.dev.argoproj.io
  http:
  - name: primary
    route:
    - destination:
        host: 'stable'
      weight: 100
    - destination:
        host: canary
	  weight: 0
  - name: secondary
	route:
	- destination:
		host: 'stable'
	  weight: 100
	- destination:
	    host: canary
	  weight: 0`

func TestReconcileWeightsBaseCase(t *testing.T) {
	r := &Reconciler{
		rollout:       rollout("stable", "canary", "vsvc", []string{"primary"}),
		desiredWeight: 10,
	}
	obj := strToUnstructured(regularVsvc)
	modifedObj, _, err := r.reconcileVirtualService(obj)
	assert.Nil(t, err)
	assert.NotNil(t, modifedObj)
	routes, ok, err := unstructured.NestedSlice(modifedObj.Object, "spec", "http")
	assert.Nil(t, err)
	assert.True(t, ok)
	route := routes[0].(map[string]interface{})
	assert.Equal(t, route["name"].(string), "primary")
	checkDestination(t, route, "stable", 90)
	checkDestination(t, route, "canary", 10)
	unmodifiedRoute := routes[1].(map[string]interface{})
	assert.Equal(t, unmodifiedRoute["name"].(string), "secondary")
	checkDestination(t, unmodifiedRoute, "stable", 100)
	checkDestination(t, unmodifiedRoute, "canary", 0)
}

func TestReconcileUpdateVirtualService(t *testing.T) {
	obj := strToUnstructured(regularVsvc)
	schema := runtime.NewScheme()
	client := fake.NewSimpleDynamicClient(schema, obj)
	ro := rollout("stable", "canary", "vsvc", []string{"primary"})
	r := NewReconciler(ro, 10, client, &record.FakeRecorder{})
	err := r.Reconcile()
	assert.Nil(t, err)
	actions := client.Actions()
	assert.Len(t, actions, 2)
	assert.Equal(t, "get", actions[0].GetVerb())
	assert.Equal(t, "update", actions[1].GetVerb())
}

func TestReconcileNoChanges(t *testing.T) {
	obj := strToUnstructured(regularVsvc)
	schema := runtime.NewScheme()
	client := fake.NewSimpleDynamicClient(schema, obj)
	ro := rollout("stable", "canary", "vsvc", []string{"primary"})
	r := NewReconciler(ro, 0, client, &record.FakeRecorder{})
	err := r.Reconcile()
	assert.Nil(t, err)
	actions := client.Actions()
	assert.Len(t, actions, 1)
	assert.Equal(t, "get", actions[0].GetVerb())
}

func TestReconcileVirtualServiceNotFound(t *testing.T) {
	schema := runtime.NewScheme()
	client := fake.NewSimpleDynamicClient(schema)
	ro := rollout("stable", "canary", "vsvc", []string{"primary"})
	r := NewReconciler(ro, 10, client, &record.FakeRecorder{})
	err := r.Reconcile()
	assert.NotNil(t, err)
	assert.True(t, k8serrors.IsNotFound(err))
	actions := client.Actions()
	assert.Len(t, actions, 1)
	assert.Equal(t, "get", actions[0].GetVerb())
}

func TestType(t *testing.T) {
	schema := runtime.NewScheme()
	client := fake.NewSimpleDynamicClient(schema)
	ro := rollout("stable", "canary", "vsvc", []string{"primary"})
	r := NewReconciler(ro, 10, client, &record.FakeRecorder{})
	assert.Equal(t, networkingType, r.Type())
}

func TestInvalidPatches(t *testing.T) {
	patches := virtualServicePatches{{
		routeIndex:       0,
		destinationIndex: 0,
		weight:           10,
	}}
	{
		invalidHTTPRoute := make([]interface{}, 1)
		invalidHTTPRoute[0] = "not a map"
		err := patches.patchVirtualService(invalidHTTPRoute)
		assert.Error(t, err, invalidCasting, "http[]", "map[string]interface")
	}
	{
		invalidHTTPRoute := []interface{}{
			map[string]interface{}{
				"route": "not a []interface",
			},
		}
		err := patches.patchVirtualService(invalidHTTPRoute)
		assert.Error(t, err, invalidCasting, "http[].route", "[]interface")
	}
	{
		invalidHTTPRoute := []interface{}{
			map[string]interface{}{
				"route": []interface{}{
					"destination",
				},
			},
		}
		err := patches.patchVirtualService(invalidHTTPRoute)
		assert.Error(t, err, invalidCasting, "http[].route[].destination", "map[string]interface")
	}
}
