package istio

import (
	"fmt"
	"strings"
	"testing"

	istioutil "github.com/argoproj/argo-rollouts/utils/istio"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/dynamic/dynamiclister"

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

func getVirtualServiceLister(client dynamic.Interface) dynamiclister.Lister {
	istioGVR := istioutil.GetIstioGVR("v1alpha3")
	dynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(client, 0)
	istioVirtualServiceInformer := dynamicInformerFactory.ForResource(istioGVR).Informer()
	stopCh := make(chan struct{})
	dynamicInformerFactory.Start(stopCh)
	dynamicInformerFactory.WaitForCacheSync(stopCh)
	close(stopCh)
	return dynamiclister.New(istioVirtualServiceInformer.GetIndexer(), istioGVR)
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
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Istio: &v1alpha1.IstioTrafficRouting{
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
		rollout: rollout("stable", "canary", "vsvc", []string{"primary"}),
	}
	obj := strToUnstructured(regularVsvc)
	modifiedObj, _, err := r.reconcileVirtualService(obj, 10)
	assert.Nil(t, err)
	assert.NotNil(t, modifiedObj)
	routes, ok, err := unstructured.NestedSlice(modifiedObj.Object, "spec", "http")
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
	vsvcLister := getVirtualServiceLister(client)
	r := NewReconciler(ro, client, &record.FakeRecorder{}, "v1alpha3", vsvcLister)
	client.ClearActions()
	err := r.Reconcile(10)
	assert.Nil(t, err)
	actions := client.Actions()
	assert.Len(t, actions, 1)
	assert.Equal(t, "update", actions[0].GetVerb())
}

func TestReconcileNoChanges(t *testing.T) {
	obj := strToUnstructured(regularVsvc)
	schema := runtime.NewScheme()
	client := fake.NewSimpleDynamicClient(schema, obj)
	ro := rollout("stable", "canary", "vsvc", []string{"primary"})
	r := NewReconciler(ro, client, &record.FakeRecorder{}, "v1alpha3", nil)
	err := r.Reconcile(0)
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), 1)
	assert.Equal(t, "get", client.Actions()[0].GetVerb())
}

func TestReconcileInvalidValidation(t *testing.T) {
	obj := strToUnstructured(regularVsvc)
	schema := runtime.NewScheme()
	client := fake.NewSimpleDynamicClient(schema, obj)
	ro := rollout("stable", "canary", "vsvc", []string{"route-not-found"})
	vsvcLister := getVirtualServiceLister(client)
	r := NewReconciler(ro, client, &record.FakeRecorder{}, "v1alpha3", vsvcLister)
	client.ClearActions()
	err := r.Reconcile(0)
	assert.Equal(t, "Route 'route-not-found' is not found", err.Error())
}

func TestReconcileVirtualServiceNotFound(t *testing.T) {
	schema := runtime.NewScheme()
	client := fake.NewSimpleDynamicClient(schema)
	ro := rollout("stable", "canary", "vsvc", []string{"primary"})
	vsvcLister := getVirtualServiceLister(client)
	r := NewReconciler(ro, client, &record.FakeRecorder{}, "v1alpha3", vsvcLister)
	client.ClearActions()
	err := r.Reconcile(10)
	assert.NotNil(t, err)
	assert.True(t, k8serrors.IsNotFound(err))
}

func TestType(t *testing.T) {
	schema := runtime.NewScheme()
	client := fake.NewSimpleDynamicClient(schema)
	ro := rollout("stable", "canary", "vsvc", []string{"primary"})
	r := NewReconciler(ro, client, &record.FakeRecorder{}, "v1alpha3", nil)
	assert.Equal(t, Type, r.Type())
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

func TestValidateHTTPRoutes(t *testing.T) {
	newRollout := func(routes []string) *v1alpha1.Rollout {
		return &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					Canary: &v1alpha1.CanaryStrategy{
						StableService: "stable",
						CanaryService: "canary",
						TrafficRouting: &v1alpha1.RolloutTrafficRouting{
							Istio: &v1alpha1.IstioTrafficRouting{
								VirtualService: v1alpha1.IstioVirtualService{
									Routes: routes,
								},
							},
						},
					},
				},
			},
		}
	}
	httpRoutes := []HttpRoute{{
		Name: "test",
		Route: []route{{
			Destination: destination{
				Host: "stable",
			},
		}},
	}}
	rollout := newRollout([]string{"test"})
	err := ValidateHTTPRoutes(rollout, httpRoutes)
	assert.Equal(t, fmt.Errorf("Route 'test' does not have exactly two routes"), err)

	httpRoutes[0].Route = []route{{
		Destination: destination{
			Host: "stable",
		},
	}, {
		Destination: destination{
			Host: "canary",
		},
	}}
	err = ValidateHTTPRoutes(rollout, httpRoutes)
	assert.Nil(t, err)

	rolloutWithNotFoundRoute := newRollout([]string{"not-found-route"})
	err = ValidateHTTPRoutes(rolloutWithNotFoundRoute, httpRoutes)
	assert.Equal(t, "Route 'not-found-route' is not found", err.Error())

}

func TestValidateHosts(t *testing.T) {
	hr := HttpRoute{
		Name: "test",
		Route: []route{{
			Destination: destination{
				Host: "stable",
			},
		}},
	}
	err := validateHosts(hr, "stable", "canary")
	assert.Equal(t, fmt.Errorf("Route 'test' does not have exactly two routes"), err)

	hr.Route = []route{{
		Destination: destination{
			Host: "stable",
		},
	}, {
		Destination: destination{
			Host: "canary",
		},
	}}
	err = validateHosts(hr, "stable", "canary")
	assert.Nil(t, err)

	err = validateHosts(hr, "not-found-stable", "canary")
	assert.Equal(t, fmt.Errorf("Stable Service 'not-found-stable' not found in route"), err)

	err = validateHosts(hr, "stable", "not-found-canary")
	assert.Equal(t, fmt.Errorf("Canary Service 'not-found-canary' not found in route"), err)
}

func TestGetRolloutVirtualServiceKeys(t *testing.T) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{},
		},
	}
	assert.Len(t, GetRolloutVirtualServiceKeys(ro), 0)
	ro.Spec.Strategy.Canary = &v1alpha1.CanaryStrategy{}
	assert.Len(t, GetRolloutVirtualServiceKeys(ro), 0)
	ro.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
	assert.Len(t, GetRolloutVirtualServiceKeys(ro), 0)
	ro.Spec.Strategy.Canary.TrafficRouting.Istio = &v1alpha1.IstioTrafficRouting{
		VirtualService: v1alpha1.IstioVirtualService{},
	}
	assert.Len(t, GetRolloutVirtualServiceKeys(ro), 0)
	ro.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Name = "test"
	keys := GetRolloutVirtualServiceKeys(ro)
	assert.Len(t, keys, 1)
	assert.Equal(t, keys[0], "default/test")
}
