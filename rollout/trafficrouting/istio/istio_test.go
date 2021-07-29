package istio

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/dynamic/dynamiclister"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	testutil "github.com/argoproj/argo-rollouts/test/util"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	"github.com/argoproj/argo-rollouts/utils/record"
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"
)

const RouteMissingBothDestinationsError = "Route does not have exactly two route destinations."

func getIstioListers(client dynamic.Interface) (dynamiclister.Lister, dynamiclister.Lister) {
	vsvcGVR := istioutil.GetIstioVirtualServiceGVR()
	druleGVR := istioutil.GetIstioDestinationRuleGVR()
	dynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(client, 0)
	istioVirtualServiceInformer := dynamicInformerFactory.ForResource(vsvcGVR).Informer()
	destinationRuleInformer := dynamicInformerFactory.ForResource(druleGVR).Informer()
	stopCh := make(chan struct{})
	dynamicInformerFactory.Start(stopCh)
	dynamicInformerFactory.WaitForCacheSync(stopCh)
	close(stopCh)
	vsvcLister := dynamiclister.New(istioVirtualServiceInformer.GetIndexer(), vsvcGVR)
	druleLister := dynamiclister.New(destinationRuleInformer.GetIndexer(), druleGVR)
	return vsvcLister, druleLister
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

func checkDestination(t *testing.T, destinations []VirtualServiceRouteDestination, svc string, expectWeight int) {
	for _, destination := range destinations {
		if destination.Destination.Host == svc {
			assert.Equal(t, expectWeight, int(destination.Weight))
			return
		}
	}
	msg := fmt.Sprintf("Service '%s' not found within hosts of routes.", svc)
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

const regularTlsVsvc = `apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: vsvc
  namespace: default
spec:
  gateways:
  - istio-rollout-gateway
  hosts:
  - istio-rollout.dev.argoproj.io
  tls:
  - match:
    - port: 3000
    route:
    - destination:
        host: 'stable'
      weight: 100
    - destination:
        host: canary
      weight: 0
  - match:
    - port: 3001
    route:
    - destination:
        host: 'stable'
      weight: 100
    - destination:
        host: canary
      weight: 0`

const singleRouteVsvc = `apiVersion: networking.istio.io/v1alpha3
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
  - route:
    - destination:
        host: 'stable'
      weight: 100
    - destination:
        host: canary
      weight: 0`

const singleRouteTlsVsvc = `apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: vsvc
  namespace: default
spec:
  gateways:
  - istio-rollout-gateway
  hosts:
  - istio-rollout.dev.argoproj.io
  tls:
  - match:
    - port: 3000
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
	obj := unstructuredutil.StrToUnstructuredUnsafe(regularVsvc)
	modifiedObj, _, err := r.reconcileVirtualService(obj, 10)
	assert.Nil(t, err)
	assert.NotNil(t, modifiedObj)
	routes, ok, err := unstructured.NestedSlice(modifiedObj.Object, "spec", "http")
	assert.Nil(t, err)
	assert.True(t, ok)
	routeBytes, _ := json.Marshal(routes)
	var httpRoutes []VirtualServiceHTTPRoute
	err = json.Unmarshal(routeBytes, &httpRoutes)
	assert.Nil(t, err)
	route := httpRoutes[0]
	assert.Equal(t, route.Name, "primary")
	checkDestination(t, route.Route, "stable", 90)
	checkDestination(t, route.Route, "canary", 10)
	unmodifiedRoute := httpRoutes[1]
	assert.Equal(t, unmodifiedRoute.Name, "secondary")
	checkDestination(t, unmodifiedRoute.Route, "stable", 100)
	checkDestination(t, unmodifiedRoute.Route, "canary", 0)
}

func TestTlsReconcileWeightsBaseCase(t *testing.T) {
	r := &Reconciler{
		rollout: rollout("stable", "canary", "vsvc", []string{"tls-3000"}),
	}
	obj := unstructuredutil.StrToUnstructuredUnsafe(regularTlsVsvc)
	modifiedObj, _, err := r.reconcileVirtualService(obj, 20)
	assert.Nil(t, err)
	assert.NotNil(t, modifiedObj)
	routes, ok, err := unstructured.NestedSlice(modifiedObj.Object, "spec", "tls")
	assert.Nil(t, err)
	assert.True(t, ok)
	routeBytes, _ := json.Marshal(routes)
	var tlsRoutes []VirtualServiceTLSRoute
	err = json.Unmarshal(routeBytes, &tlsRoutes)
	assert.Nil(t, err)
	route := tlsRoutes[0]
	routeMatch := route.Match
	assert.Equal(t, int(routeMatch[0].Port), 3000)
	checkDestination(t, route.Route, "stable", 80)
	checkDestination(t, route.Route, "canary", 20)
	unmodifiedRoute := tlsRoutes[1]
	unmodifiedRouteMatch := unmodifiedRoute.Match
	assert.Equal(t, int(unmodifiedRouteMatch[0].Port), 3001)
	checkDestination(t, unmodifiedRoute.Route, "stable", 100)
	checkDestination(t, unmodifiedRoute.Route, "canary", 0)
}

func TestReconcileUpdateVirtualService(t *testing.T) {
	ro := rollout("stable", "canary", "vsvc", []string{"primary"})
	AssertReconcileUpdateVirtualService(t, regularVsvc, ro)
}

func TestTlsReconcileUpdateVirtualService(t *testing.T) {
	ro := rollout("stable", "canary", "vsvc", []string{"https-3000"})
	AssertReconcileUpdateVirtualService(t, regularTlsVsvc, ro)
}

func AssertReconcileUpdateVirtualService(t *testing.T, vsvc string, ro *v1alpha1.Rollout) *dynamicfake.FakeDynamicClient {
	obj := unstructuredutil.StrToUnstructuredUnsafe(vsvc)
	client := testutil.NewFakeDynamicClient(obj)
	vsvcLister, druleLister := getIstioListers(client)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()
	err := r.SetWeight(10)
	assert.Nil(t, err)
	actions := client.Actions()
	assert.Len(t, actions, 1)
	assert.Equal(t, "update", actions[0].GetVerb())
	return client
}

func TestReconcileNoChanges(t *testing.T) {
	obj := unstructuredutil.StrToUnstructuredUnsafe(regularVsvc)
	client := testutil.NewFakeDynamicClient(obj)
	ro := rollout("stable", "canary", "vsvc", []string{"primary"})
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), nil, nil)
	err := r.SetWeight(0)
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), 1)
	assert.Equal(t, "get", client.Actions()[0].GetVerb())
}

func TestTlsReconcileNoChanges(t *testing.T) {
	obj := unstructuredutil.StrToUnstructuredUnsafe(regularTlsVsvc)
	client := testutil.NewFakeDynamicClient(obj)
	ro := rollout("stable", "canary", "vsvc", []string{"https-3001"})
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), nil, nil)
	err := r.SetWeight(0)
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), 1)
	assert.Equal(t, "get", client.Actions()[0].GetVerb())
}

func TestReconcileInvalidValidation(t *testing.T) {
	obj := unstructuredutil.StrToUnstructuredUnsafe(regularVsvc)
	client := testutil.NewFakeDynamicClient(obj)
	ro := rollout("stable", "canary", "vsvc", []string{"route-not-found"})
	vsvcLister, druleLister := getIstioListers(client)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()
	err := r.SetWeight(0)
	assert.Equal(t, "HTTP Route 'route-not-found' is not found in the defined Virtual Service.", err.Error())
}

func TestTlsReconcileInvalidValidation(t *testing.T) {
	obj := unstructuredutil.StrToUnstructuredUnsafe(regularTlsVsvc)
	client := testutil.NewFakeDynamicClient(obj)
	ro := rollout("stable", "canary", "vsvc", []string{"tls-route-not-found"})
	vsvcLister, druleLister := getIstioListers(client)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()
	err := r.SetWeight(0)
	assert.Equal(t, "TLS Route 'tls-route-not-found' is not found in the defined Virtual Service.", err.Error())
}

func TestReconcileVirtualServiceNotFound(t *testing.T) {
	client := testutil.NewFakeDynamicClient()
	ro := rollout("stable", "canary", "vsvc", []string{"primary"})
	vsvcLister, druleLister := getIstioListers(client)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()
	err := r.SetWeight(10)
	assert.NotNil(t, err)
	assert.True(t, k8serrors.IsNotFound(err))
}

// TestReconcileAmbiguousRoutes tests when we omit route names and there are multiple routes in the VirtualService
func TestReconcileAmbiguousRoutes(t *testing.T) {
	obj := unstructuredutil.StrToUnstructuredUnsafe(regularVsvc)
	client := testutil.NewFakeDynamicClient(obj)
	ro := rollout("stable", "canary", "vsvc", nil)
	vsvcLister, druleLister := getIstioListers(client)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()
	err := r.SetWeight(0)
	assert.Equal(t, "Either of spec.http[] or spec.tls[] should be set in VirtualService and it must have exactly one route when omitting spec.strategy.canary.trafficRouting.istio.virtualService.routes", err.Error())
}

func TestTlsReconcileAmbiguousRoutes(t *testing.T) {
	obj := unstructuredutil.StrToUnstructuredUnsafe(regularTlsVsvc)
	client := testutil.NewFakeDynamicClient(obj)
	ro := rollout("stable", "canary", "vsvc", nil)
	vsvcLister, druleLister := getIstioListers(client)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()
	err := r.SetWeight(0)
	assert.Equal(t, "Either of spec.http[] or spec.tls[] should be set in VirtualService and it must have exactly one route when omitting spec.strategy.canary.trafficRouting.istio.virtualService.routes", err.Error())
}

// TestReconcileInferredSingleRoute we can support case where we infer the only route in the VirtualService
func TestReconcileInferredSingleRoute(t *testing.T) {
	ro := rollout("stable", "canary", "vsvc", nil)
	client := AssertReconcileUpdateVirtualService(t, singleRouteVsvc, ro)

	// Verify we actually made the correct change
	vsvcUn, err := client.Resource(istioutil.GetIstioVirtualServiceGVR()).Namespace(ro.Namespace).Get(context.TODO(), "vsvc", metav1.GetOptions{})
	assert.NoError(t, err)
	vsHttpRoutes, _, _ := unstructured.NestedSlice(vsvcUn.Object, "spec", "http")
	routeBytes, _ := json.Marshal(vsHttpRoutes)
	var httpRoutes []VirtualServiceHTTPRoute
	err = json.Unmarshal(routeBytes, &httpRoutes)
	assert.Nil(t, err)
	route := httpRoutes[0]
	checkDestination(t, route.Route, "stable", 90)
	checkDestination(t, route.Route, "canary", 10)
}

func TestTlsReconcileInferredSingleRoute(t *testing.T) {
	ro := rollout("stable", "canary", "vsvc", nil)
	client := AssertReconcileUpdateVirtualService(t, singleRouteTlsVsvc, ro)

	// Verify we actually made the correct change
	vsvcUn, err := client.Resource(istioutil.GetIstioVirtualServiceGVR()).Namespace(ro.Namespace).Get(context.TODO(), "vsvc", metav1.GetOptions{})
	assert.NoError(t, err)
	vsTlsRoutes, _, _ := unstructured.NestedSlice(vsvcUn.Object, "spec", "tls")
	routeBytes, _ := json.Marshal(vsTlsRoutes)
	var tlsRoutes []VirtualServiceTLSRoute
	err = json.Unmarshal(routeBytes, &tlsRoutes)
	assert.Nil(t, err)
	route := tlsRoutes[0]
	checkDestination(t, route.Route, "stable", 90)
	checkDestination(t, route.Route, "canary", 10)
}

func TestType(t *testing.T) {
	client := testutil.NewFakeDynamicClient()
	ro := rollout("stable", "canary", "vsvc", []string{"primary"})
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), nil, nil)
	assert.Equal(t, Type, r.Type())
}

func TestInvalidPatches(t *testing.T) {
	patches := virtualServicePatches{{
		routeIndex:       0,
		routeType:        "http",
		destinationIndex: 0,
		weight:           10,
	}}
	{
		invalidHTTPRoute := make([]interface{}, 1)
		invalidTlsRoute := make([]interface{}, 1)
		invalidHTTPRoute[0] = "not a map"
		err := patches.patchVirtualService(invalidHTTPRoute, invalidTlsRoute)
		assert.Error(t, err, invalidCasting, "http[]", "map[string]interface")
	}
	{
		invalidHTTPRoute := []interface{}{
			map[string]interface{}{
				"route": "not a []interface",
			},
		}
		invalidTlsRoute := make([]interface{}, 1)
		err := patches.patchVirtualService(invalidHTTPRoute, invalidTlsRoute)
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
		invalidTlsRoute := make([]interface{}, 1)
		err := patches.patchVirtualService(invalidHTTPRoute, invalidTlsRoute)
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
	httpRoutes := []VirtualServiceHTTPRoute{{
		Name: "test",
		Route: []VirtualServiceRouteDestination{{
			Destination: VirtualServiceDestination{
				Host: "stable",
			},
		}},
	}}
	rollout := newRollout([]string{"test"})
	err := ValidateHTTPRoutes(rollout, httpRoutes)
	assert.Equal(t, fmt.Errorf(RouteMissingBothDestinationsError), err)

	httpRoutes[0].Route = []VirtualServiceRouteDestination{{
		Destination: VirtualServiceDestination{
			Host: "stable",
		},
	}, {
		Destination: VirtualServiceDestination{
			Host: "canary",
		},
	}}
	err = ValidateHTTPRoutes(rollout, httpRoutes)
	assert.Nil(t, err)

	rolloutWithNotFoundRoute := newRollout([]string{"not-found-route"})
	err = ValidateHTTPRoutes(rolloutWithNotFoundRoute, httpRoutes)
	assert.Equal(t, "HTTP Route 'not-found-route' is not found in the defined Virtual Service.", err.Error())
}

func TestValidateTLSRoutes(t *testing.T) {
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
	tlsRoutes := []VirtualServiceTLSRoute{{
		Match: []TLSMatchAttributes{{
			Port: 3000,
		}},
		Route: []VirtualServiceRouteDestination{{
			Destination: VirtualServiceDestination{
				Host: "stable",
			},
		}},
	}}
	rollout := newRollout([]string{"https-3001"})
	err := ValidateTlsRoutes(rollout, tlsRoutes)
	assert.Equal(t, fmt.Errorf("TLS Route 'https-3001' is not found in the defined Virtual Service."), err)

	rollout = newRollout([]string{"https-3000"})
	err = ValidateTlsRoutes(rollout, tlsRoutes)
	assert.Equal(t, fmt.Errorf(RouteMissingBothDestinationsError), err)

	tlsRoutes[0].Route = []VirtualServiceRouteDestination{{
		Destination: VirtualServiceDestination{
			Host: "stable",
		},
	}, {
		Destination: VirtualServiceDestination{
			Host: "canary",
		},
	}}
	err = ValidateTlsRoutes(rollout, tlsRoutes)
	assert.Nil(t, err)

	rolloutWithNotFoundRoute := newRollout([]string{"tls-not-found-route"})
	err = ValidateTlsRoutes(rolloutWithNotFoundRoute, tlsRoutes)
	assert.Equal(t, "TLS Route 'tls-not-found-route' is not found in the defined Virtual Service.", err.Error())
}

func TestValidateHosts(t *testing.T) {
	hr := VirtualServiceHTTPRoute{
		Name: "test",
		Route: []VirtualServiceRouteDestination{{
			Destination: VirtualServiceDestination{
				Host: "stable",
			},
		}},
	}
	err := validateVirtualServiceRouteDestinations(hr.Route, "stable", "canary", nil)
	assert.Equal(t, fmt.Errorf(RouteMissingBothDestinationsError), err)

	hr.Route = []VirtualServiceRouteDestination{{
		Destination: VirtualServiceDestination{
			Host: "stable",
		},
	}, {
		Destination: VirtualServiceDestination{
			Host: "canary",
		},
	}}
	err = validateVirtualServiceRouteDestinations(hr.Route, "stable", "canary", nil)
	assert.Nil(t, err)

	err = validateVirtualServiceRouteDestinations(hr.Route, "not-found-stable", "canary", nil)
	assert.Equal(t, fmt.Errorf("Stable Service 'not-found-stable' not found in route"), err)

	err = validateVirtualServiceRouteDestinations(hr.Route, "stable", "not-found-canary", nil)
	assert.Equal(t, fmt.Errorf("Canary Service 'not-found-canary' not found in route"), err)

	hr.Route = []VirtualServiceRouteDestination{{
		Destination: VirtualServiceDestination{
			Host: "stable.namespace",
		},
	}, {
		Destination: VirtualServiceDestination{
			Host: "canary.namespace",
		},
	}}
	err = validateVirtualServiceRouteDestinations(hr.Route, "stable", "canary", nil)
	assert.Nil(t, err)
}

func TestValidateHTTPRoutesSubsets(t *testing.T) {
	rollout := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Istio: &v1alpha1.IstioTrafficRouting{
							VirtualService: v1alpha1.IstioVirtualService{
								Routes: []string{"primary"},
							},
							DestinationRule: &v1alpha1.IstioDestinationRule{
								Name:             "subset",
								CanarySubsetName: "canary",
								StableSubsetName: "stable",
							},
						},
					},
				},
			},
		},
	}
	httpRoutes := []VirtualServiceHTTPRoute{{
		Name: "primary",
		Route: []VirtualServiceRouteDestination{
			{
				Destination: VirtualServiceDestination{
					Host:   "rollout",
					Subset: "stable",
				},
			},
			{
				Destination: VirtualServiceDestination{
					Host:   "rollout",
					Subset: "canary",
				},
			},
		},
	}}

	{
		// the good case
		err := ValidateHTTPRoutes(rollout, httpRoutes)
		assert.NoError(t, err)
	}
	{
		// the stable subset doesnt exist
		rollout = rollout.DeepCopy()
		rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule.StableSubsetName = "doesntexist"
		err := ValidateHTTPRoutes(rollout, httpRoutes)
		assert.EqualError(t, err, "Stable DestinationRule subset 'doesntexist' not found in route")
	}
	{
		// the canary subset doesnt exist
		rollout = rollout.DeepCopy()
		rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule.CanarySubsetName = "doesntexist"
		err := ValidateHTTPRoutes(rollout, httpRoutes)
		assert.EqualError(t, err, "Canary DestinationRule subset 'doesntexist' not found in route")
	}
}

func rolloutWithDestinationRule() *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rollout",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Istio: &v1alpha1.IstioTrafficRouting{
							VirtualService: v1alpha1.IstioVirtualService{
								Routes: []string{"primary"},
							},
							DestinationRule: &v1alpha1.IstioDestinationRule{
								Name:             "istio-destrule",
								CanarySubsetName: "canary",
								StableSubsetName: "stable",
							},
						},
					},
				},
			},
		},
	}
}

// TestUpdateHashWithListers verifies behavior of UpdateHash when using informers/listers
func TestUpdateHashAdditionalFieldsWithListers(t *testing.T) {
	ro := rolloutWithDestinationRule()
	obj := unstructuredutil.StrToUnstructuredUnsafe(`
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: istio-destrule
  namespace: default
spec:
  host: ratings.prod.svc.cluster.local
  trafficPolicy:
    loadBalancer:
      simple: LEAST_CONN
  subsets:
  - name: stable
    labels:
      version: v3
  - name: canary
    trafficPolicy:
      loadBalancer:
        simple: ROUND_ROBIN
`)
	client := testutil.NewFakeDynamicClient(obj)
	vsvcLister, druleLister := getIstioListers(client)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()

	err := r.UpdateHash("abc123", "def456")
	assert.NoError(t, err)
	actions := client.Actions()
	assert.Len(t, actions, 1)
	assert.Equal(t, "update", actions[0].GetVerb())

	dRuleUn, err := client.Resource(istioutil.GetIstioDestinationRuleGVR()).Namespace(r.rollout.Namespace).Get(context.TODO(), "istio-destrule", metav1.GetOptions{})
	assert.NoError(t, err)

	dRuleUnBytes, err := json.Marshal(dRuleUn)
	assert.NoError(t, err)
	assert.Equal(t, `{"apiVersion":"networking.istio.io/v1alpha3","kind":"DestinationRule","metadata":{"annotations":{"argo-rollouts.argoproj.io/managed-by-rollouts":"rollout"},"name":"istio-destrule","namespace":"default"},"spec":{"host":"ratings.prod.svc.cluster.local","subsets":[{"labels":{"rollouts-pod-template-hash":"def456","version":"v3"},"name":"stable"},{"labels":{"rollouts-pod-template-hash":"abc123"},"name":"canary","trafficPolicy":{"loadBalancer":{"simple":"ROUND_ROBIN"}}}],"trafficPolicy":{"loadBalancer":{"simple":"LEAST_CONN"}}}}`,
		string(dRuleUnBytes))

	_, dRule, _, err := unstructuredToDestinationRules(dRuleUn)
	assert.NoError(t, err)
	assert.Equal(t, dRule.Annotations[v1alpha1.ManagedByRolloutsKey], "rollout")
	assert.Equal(t, dRule.Spec.Subsets[0].Labels[v1alpha1.DefaultRolloutUniqueLabelKey], "def456")
	assert.Equal(t, dRule.Spec.Subsets[1].Labels[v1alpha1.DefaultRolloutUniqueLabelKey], "abc123")
	assert.Nil(t, dRule.Spec.Subsets[0].Extra)
	assert.NotNil(t, dRule.Spec.Subsets[1].Extra)

	jsonBytes, err := json.Marshal(dRule)
	assert.NoError(t, err)
	assert.Equal(t, `{"metadata":{"name":"istio-destrule","namespace":"default","creationTimestamp":null,"annotations":{"argo-rollouts.argoproj.io/managed-by-rollouts":"rollout"}},"spec":{"subsets":[{"name":"stable","labels":{"rollouts-pod-template-hash":"def456","version":"v3"}},{"name":"canary","labels":{"rollouts-pod-template-hash":"abc123"},"Extra":{"trafficPolicy":{"loadBalancer":{"simple":"ROUND_ROBIN"}}}}]}}`,
		string(jsonBytes))
}

// TestUpdateHashWithListers verifies behavior of UpdateHash when using informers/listers
func TestUpdateHashWithListers(t *testing.T) {
	ro := rolloutWithDestinationRule()
	obj := unstructuredutil.StrToUnstructuredUnsafe(`
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: istio-destrule
  namespace: default
spec:
  subsets:
  - name: stable
  - name: canary
`)
	client := testutil.NewFakeDynamicClient(obj)
	vsvcLister, druleLister := getIstioListers(client)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()

	err := r.UpdateHash("abc123", "def456")
	assert.NoError(t, err)
	actions := client.Actions()
	assert.Len(t, actions, 1)
	assert.Equal(t, "update", actions[0].GetVerb())

	dRuleUn, err := client.Resource(istioutil.GetIstioDestinationRuleGVR()).Namespace(r.rollout.Namespace).Get(context.TODO(), "istio-destrule", metav1.GetOptions{})
	assert.NoError(t, err)
	_, dRule, _, err := unstructuredToDestinationRules(dRuleUn)
	assert.NoError(t, err)
	assert.Equal(t, dRule.Annotations[v1alpha1.ManagedByRolloutsKey], "rollout")
	assert.Equal(t, dRule.Spec.Subsets[0].Labels[v1alpha1.DefaultRolloutUniqueLabelKey], "def456")
	assert.Equal(t, dRule.Spec.Subsets[1].Labels[v1alpha1.DefaultRolloutUniqueLabelKey], "abc123")
}

// TestUpdateHashNoChange verifies we don't make any API calls when there are no changes necessary to the destinationRule
func TestUpdateHashNoChange(t *testing.T) {
	ro := rolloutWithDestinationRule()
	obj := unstructuredutil.StrToUnstructuredUnsafe(`
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: istio-destrule
  namespace: default
  annotations:
    argo-rollouts.argoproj.io/managed-by-rollouts: rollout
spec:
  subsets:
  - name: stable
    labels:
      rollouts-pod-template-hash: def456
  - name: canary
    labels:
      rollouts-pod-template-hash: abc123
`)
	client := testutil.NewFakeDynamicClient(obj)
	vsvcLister, druleLister := getIstioListers(client)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()

	err := r.UpdateHash("abc123", "def456")
	assert.NoError(t, err)
	actions := client.Actions()
	assert.Len(t, actions, 0)
}

// TestUpdateHashWithListers verifies behavior of UpdateHash when we do not yet have a lister/informer
func TestUpdateHashWithoutListers(t *testing.T) {
	ro := rolloutWithDestinationRule()
	obj := unstructuredutil.StrToUnstructuredUnsafe(`
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: istio-destrule
  namespace: default
spec:
  subsets:
  - name: stable
  - name: canary
`)
	client := testutil.NewFakeDynamicClient(obj)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), nil, nil)
	client.ClearActions()

	err := r.UpdateHash("abc123", "def456")
	assert.NoError(t, err)
	actions := client.Actions()
	assert.Len(t, actions, 2)
	assert.Equal(t, "get", actions[0].GetVerb())
	assert.Equal(t, "update", actions[1].GetVerb())

	dRuleUn, err := client.Resource(istioutil.GetIstioDestinationRuleGVR()).Namespace(r.rollout.Namespace).Get(context.TODO(), "istio-destrule", metav1.GetOptions{})
	assert.NoError(t, err)
	_, dRule, _, err := unstructuredToDestinationRules(dRuleUn)
	assert.NoError(t, err)
	assert.Equal(t, dRule.Annotations[v1alpha1.ManagedByRolloutsKey], "rollout")
	assert.Equal(t, dRule.Spec.Subsets[0].Labels[v1alpha1.DefaultRolloutUniqueLabelKey], "def456")
	assert.Equal(t, dRule.Spec.Subsets[1].Labels[v1alpha1.DefaultRolloutUniqueLabelKey], "abc123")
}

func TestUpdateHashDestinationRuleNotFound(t *testing.T) {
	ro := rolloutWithDestinationRule()
	client := testutil.NewFakeDynamicClient()
	vsvcLister, druleLister := getIstioListers(client)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()

	err := r.UpdateHash("abc123", "def456")
	actions := client.Actions()
	assert.Len(t, actions, 0)
	assert.EqualError(t, err, "destinationrules.networking.istio.io \"istio-destrule\" not found")
}
