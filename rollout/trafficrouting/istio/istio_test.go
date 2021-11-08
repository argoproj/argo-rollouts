package istio

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	unstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/dynamic/dynamiclister"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	testutil "github.com/argoproj/argo-rollouts/test/util"
	evalUtils "github.com/argoproj/argo-rollouts/utils/evaluate"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	"github.com/argoproj/argo-rollouts/utils/record"
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"
)

const NoTlsRouteFoundError = "No matching TLS routes found in the defined Virtual Service."

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

func rollout(stableSvc, canarySvc string, istioVirtualService *v1alpha1.IstioVirtualService) *v1alpha1.Rollout {
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
							VirtualService: istioVirtualService,
						},
					},
				},
			},
		},
	}
}

func rolloutWithHttpRoutes(stableSvc, canarySvc, vsvc string, httpRoutes []string) *v1alpha1.Rollout {
	istioVirtualService := &v1alpha1.IstioVirtualService{
		Name:   vsvc,
		Routes: httpRoutes,
	}
	return rollout(stableSvc, canarySvc, istioVirtualService)
}

func rolloutWithTlsRoutes(stableSvc, canarySvc, vsvc string, tlsRoutes []v1alpha1.TLSRoute) *v1alpha1.Rollout {
	istioVirtualService := &v1alpha1.IstioVirtualService{
		Name:      vsvc,
		TLSRoutes: tlsRoutes,
	}
	return rollout(stableSvc, canarySvc, istioVirtualService)
}

func rolloutWithHttpAndTlsRoutes(stableSvc, canarySvc, vsvc string, httpRoutes []string, tlsRoutes []v1alpha1.TLSRoute) *v1alpha1.Rollout {
	istioVirtualService := &v1alpha1.IstioVirtualService{
		Name:      vsvc,
		Routes:    httpRoutes,
		TLSRoutes: tlsRoutes,
	}
	return rollout(stableSvc, canarySvc, istioVirtualService)
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

const regularMixedVsvc = `apiVersion: networking.istio.io/v1alpha3
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
      weight: 0
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

const regularMixedVsvcTwoHttpRoutes = `apiVersion: networking.istio.io/v1alpha3
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
      weight: 0
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

const regularMixedVsvcTwoTlsRoutes = `apiVersion: networking.istio.io/v1alpha3
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

const regularTlsSniVsvc = `apiVersion: networking.istio.io/v1alpha3
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
    - sniHosts:
      - foo.bar.com
      - bar.foo.com
    - sniHosts:
      - localhost
    route:
    - destination:
        host: 'stable'
      weight: 100
    - destination:
        host: canary
      weight: 0
  - match:
    - port: 3001
      sniHosts:
      - localhost
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

const singleRouteMixedVsvc = `apiVersion: networking.istio.io/v1alpha3
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
      weight: 0
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

func extractHttpRoutes(t *testing.T, modifiedObj *unstructured.Unstructured) []VirtualServiceHTTPRoute {
	routes, ok, err := unstructured.NestedSlice(modifiedObj.Object, "spec", "http")
	assert.Nil(t, err)
	assert.True(t, ok)
	routeBytes, _ := json.Marshal(routes)
	var httpRoutes []VirtualServiceHTTPRoute
	err = json.Unmarshal(routeBytes, &httpRoutes)
	assert.Nil(t, err)
	return httpRoutes
}

func assertHttpRouteWeightChanges(t *testing.T, httpRoute VirtualServiceHTTPRoute, routeName string, canaryWeight, stableWeight int) {
	assert.Equal(t, httpRoute.Name, routeName)
	checkDestination(t, httpRoute.Route, "stable", stableWeight)
	checkDestination(t, httpRoute.Route, "canary", canaryWeight)
}

func extractTlsRoutes(t *testing.T, modifiedObj *unstructured.Unstructured) []VirtualServiceTLSRoute {
	routes, ok, err := unstructured.NestedSlice(modifiedObj.Object, "spec", "tls")
	assert.Nil(t, err)
	assert.True(t, ok)
	routeBytes, _ := json.Marshal(routes)
	var tlsRoutes []VirtualServiceTLSRoute
	err = json.Unmarshal(routeBytes, &tlsRoutes)
	assert.Nil(t, err)
	return tlsRoutes
}

func assertTlsRouteWeightChanges(t *testing.T, tlsRoute VirtualServiceTLSRoute, snis []string, portNum, canaryWeight, stableWeight int) {
	portsMap := make(map[int64]bool)
	sniHostsMap := make(map[string]bool)
	for _, routeMatch := range tlsRoute.Match {
		if routeMatch.Port != 0 {
			portsMap[routeMatch.Port] = true
		}
		for _, sniHost := range routeMatch.SNI {
			sniHostsMap[sniHost] = true
		}
	}
	port := 0
	for portNumber := range portsMap {
		port = int(portNumber)
	}
	sniHosts := []string{}
	for sniHostName := range sniHostsMap {
		sniHosts = append(sniHosts, sniHostName)
	}
	if portNum != 0 {
		assert.Equal(t, portNum, port)
	}
	if len(snis) != 0 {
		assert.Equal(t, evalUtils.Equal(snis, sniHosts), true)
	}
	checkDestination(t, tlsRoute.Route, "stable", stableWeight)
	checkDestination(t, tlsRoute.Route, "canary", canaryWeight)
}

func TestHttpReconcileWeightsBaseCase(t *testing.T) {
	r := &Reconciler{
		rollout: rolloutWithHttpRoutes("stable", "canary", "vsvc", []string{"primary"}),
	}

	// Test for both the HTTP VS & Mixed VS
	for _, vsvc := range []string{regularVsvc, regularMixedVsvcTwoHttpRoutes} {
		obj := unstructuredutil.StrToUnstructuredUnsafe(vsvc)
		vsvcRoutes := r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Routes
		vsvcTLSRoutes := r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.TLSRoutes
		modifiedObj, _, err := r.reconcileVirtualService(obj, vsvcRoutes, vsvcTLSRoutes, 10)
		assert.Nil(t, err)
		assert.NotNil(t, modifiedObj)

		// HTTP Routes
		httpRoutes := extractHttpRoutes(t, modifiedObj)

		// Assertions
		assertHttpRouteWeightChanges(t, httpRoutes[0], "primary", 10, 90)
		assertHttpRouteWeightChanges(t, httpRoutes[1], "secondary", 0, 100)
	}
}

func TestTlsReconcileWeightsBaseCase(t *testing.T) {
	r := &Reconciler{
		rollout: rolloutWithTlsRoutes("stable", "canary", "vsvc",
			[]v1alpha1.TLSRoute{
				{
					Port: 3000,
				},
			},
		),
	}

	// Test for both the TLS VS & Mixed VS
	for _, vsvc := range []string{regularTlsVsvc, regularMixedVsvcTwoTlsRoutes} {
		obj := unstructuredutil.StrToUnstructuredUnsafe(vsvc)
		vsvcRoutes := r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Routes
		vsvcTLSRoutes := r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.TLSRoutes
		modifiedObj, _, err := r.reconcileVirtualService(obj, vsvcRoutes, vsvcTLSRoutes, 30)
		assert.Nil(t, err)
		assert.NotNil(t, modifiedObj)

		// TLS Routes
		tlsRoutes := extractTlsRoutes(t, modifiedObj)

		// Assestions
		assertTlsRouteWeightChanges(t, tlsRoutes[0], nil, 3000, 30, 70)
		assertTlsRouteWeightChanges(t, tlsRoutes[1], nil, 3001, 0, 100)
	}
}

func TestTlsSniReconcileWeightsBaseCase(t *testing.T) {
	snis := []string{"foo.bar.com", "bar.foo.com", "localhost"}
	r := &Reconciler{
		rollout: rolloutWithTlsRoutes("stable", "canary", "vsvc",
			[]v1alpha1.TLSRoute{
				{
					SNIHosts: snis,
				},
			},
		),
	}

	obj := unstructuredutil.StrToUnstructuredUnsafe(regularTlsSniVsvc)
	vsvcRoutes := r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Routes
	vsvcTLSRoutes := r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.TLSRoutes
	modifiedObj, _, err := r.reconcileVirtualService(obj, vsvcRoutes, vsvcTLSRoutes, 30)
	assert.Nil(t, err)
	assert.NotNil(t, modifiedObj)

	// TLS Routes
	tlsRoutes := extractTlsRoutes(t, modifiedObj)

	// Assestions
	assertTlsRouteWeightChanges(t, tlsRoutes[0], snis, 0, 30, 70)
	assertTlsRouteWeightChanges(t, tlsRoutes[1], []string{"localhost"}, 3001, 0, 100)
}

func TestTlsPortAndSniReconcileWeightsBaseCase(t *testing.T) {
	snis := []string{"localhost"}
	r := &Reconciler{
		rollout: rolloutWithTlsRoutes("stable", "canary", "vsvc",
			[]v1alpha1.TLSRoute{
				{
					Port:     3001,
					SNIHosts: snis,
				},
			},
		),
	}

	obj := unstructuredutil.StrToUnstructuredUnsafe(regularTlsSniVsvc)
	vsvcRoutes := r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Routes
	vsvcTLSRoutes := r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.TLSRoutes
	modifiedObj, _, err := r.reconcileVirtualService(obj, vsvcRoutes, vsvcTLSRoutes, 30)
	assert.Nil(t, err)
	assert.NotNil(t, modifiedObj)

	// TLS Routes
	tlsRoutes := extractTlsRoutes(t, modifiedObj)

	// Assestions
	assertTlsRouteWeightChanges(t, tlsRoutes[1], []string{"localhost"}, 3001, 30, 70)
	assertTlsRouteWeightChanges(t, tlsRoutes[0], []string{"foo.bar.com", "bar.foo.com", "localhost"}, 0, 0, 100)
}

func TestReconcileWeightsBaseCase(t *testing.T) {
	r := &Reconciler{
		rollout: rolloutWithHttpAndTlsRoutes("stable", "canary", "vsvc", []string{"primary"},
			[]v1alpha1.TLSRoute{
				{
					Port: 3000,
				},
			},
		),
	}
	obj := unstructuredutil.StrToUnstructuredUnsafe(regularMixedVsvc)
	vsvcRoutes := r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Routes
	vsvcTLSRoutes := r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.TLSRoutes
	modifiedObj, _, err := r.reconcileVirtualService(obj, vsvcRoutes, vsvcTLSRoutes, 20)
	assert.Nil(t, err)
	assert.NotNil(t, modifiedObj)

	// HTTP Routes
	httpRoutes := extractHttpRoutes(t, modifiedObj)

	// Assertions
	assertHttpRouteWeightChanges(t, httpRoutes[0], "primary", 20, 80)
	assertHttpRouteWeightChanges(t, httpRoutes[1], "secondary", 0, 100)

	// TLS Routes
	tlsRoutes := extractTlsRoutes(t, modifiedObj)

	// Assestions
	assertTlsRouteWeightChanges(t, tlsRoutes[0], nil, 3000, 20, 80)
	assertTlsRouteWeightChanges(t, tlsRoutes[1], nil, 3001, 0, 100)
}

func TestReconcileUpdateVirtualService(t *testing.T) {
	ro := rolloutWithHttpRoutes("stable", "canary", "vsvc", []string{"primary"})
	AssertReconcileUpdateVirtualService(t, regularVsvc, ro)
}

func TestTlsReconcileUpdateVirtualService(t *testing.T) {
	ro := rolloutWithTlsRoutes("stable", "canary", "vsvc",
		[]v1alpha1.TLSRoute{
			{
				Port: 3000,
			},
		},
	)
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
	ro := rolloutWithHttpRoutes("stable", "canary", "vsvc", []string{"primary"})
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), nil, nil)
	err := r.SetWeight(0)
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), 1)
	assert.Equal(t, "get", client.Actions()[0].GetVerb())
}

func TestReconcileVirtualServiceExperimentStep(t *testing.T) {
	obj := unstructuredutil.StrToUnstructuredUnsafe(regularVsvc)
	client := testutil.NewFakeDynamicClient(obj)
	ro := rolloutWithHttpRoutes("stable", "canary", "vsvc", []string{"primary"})
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), nil, nil)
	additionalDestinations := []v1alpha1.WeightDestination{
		{
			ServiceName:     "exp-svc",
			PodTemplateHash: "",
			Weight:          20,
		},
	}
	// Set weight with additionalDestination
	err := r.SetWeight(10, additionalDestinations...)
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), 2)
	assert.Equal(t, "get", client.Actions()[0].GetVerb())
	assert.Equal(t, "update", client.Actions()[1].GetVerb())
	// Change weight of additionalDestination
	additionalDestinations[0].Weight = 10
	err = r.SetWeight(10, additionalDestinations...)
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), 4)
	assert.Equal(t, "get", client.Actions()[2].GetVerb())
	assert.Equal(t, "update", client.Actions()[3].GetVerb())
	// Delete additionalDestination
	err = r.SetWeight(10)
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), 6)
	assert.Equal(t, "get", client.Actions()[4].GetVerb())
	assert.Equal(t, "update", client.Actions()[5].GetVerb())
}

func TestHostSplitExperimentStep(t *testing.T) {
	obj := unstructuredutil.StrToUnstructuredUnsafe(regularVsvc)
	client := testutil.NewFakeDynamicClient(obj)
	ro := rolloutWithHttpRoutes("stable", "canary", "vsvc", []string{"primary"})
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), nil, nil)
	additionalDestinations := []v1alpha1.WeightDestination{
		{
			ServiceName:     "exp-svc",
			PodTemplateHash: "",
			Weight:          20,
		},
	}
	vsvcRoutes := r.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Routes
	modifiedObj, _, err := r.reconcileVirtualService(obj, vsvcRoutes, nil, 10, additionalDestinations...)
	assert.Nil(t, err)
	// Create route for experiment service
	httpRoutes := extractHttpRoutes(t, modifiedObj)
	checkDestination(t, httpRoutes[0].Route, "stable", 70)
	checkDestination(t, httpRoutes[0].Route, "canary", 10)
	checkDestination(t, httpRoutes[0].Route, "exp-svc", 20)
	// Delete route for experiment service if no additionalDestinations specified
	modifiedObj, _, err = r.reconcileVirtualService(obj, vsvcRoutes, nil, 10)
	assert.Nil(t, err)
	httpRoutes = extractHttpRoutes(t, modifiedObj)
	checkDestination(t, httpRoutes[0].Route, "stable", 90)
	checkDestination(t, httpRoutes[0].Route, "canary", 10)
}

func TestTlsReconcileNoChanges(t *testing.T) {
	obj := unstructuredutil.StrToUnstructuredUnsafe(regularTlsVsvc)
	client := testutil.NewFakeDynamicClient(obj)
	ro := rolloutWithTlsRoutes("stable", "canary", "vsvc",
		[]v1alpha1.TLSRoute{
			{
				Port: 3001,
			},
		},
	)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), nil, nil)
	err := r.SetWeight(0)
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), 1)
	assert.Equal(t, "get", client.Actions()[0].GetVerb())
}

func TestReconcileInvalidValidation(t *testing.T) {
	obj := unstructuredutil.StrToUnstructuredUnsafe(regularVsvc)
	client := testutil.NewFakeDynamicClient(obj)
	ro := rolloutWithHttpRoutes("stable", "canary", "vsvc", []string{"route-not-found"})
	vsvcLister, druleLister := getIstioListers(client)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()
	err := r.SetWeight(0)
	assert.Equal(t, "HTTP Route 'route-not-found' is not found in the defined Virtual Service.", err.Error())
}

func TestTlsReconcileInvalidValidation(t *testing.T) {
	obj := unstructuredutil.StrToUnstructuredUnsafe(regularTlsVsvc)
	client := testutil.NewFakeDynamicClient(obj)
	ro := rolloutWithTlsRoutes("stable", "canary", "vsvc",
		[]v1alpha1.TLSRoute{
			{
				Port: 1001,
			},
		},
	)
	vsvcLister, druleLister := getIstioListers(client)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()
	err := r.SetWeight(0)
	assert.Equal(t, NoTlsRouteFoundError, err.Error())
}

func TestReconcileVirtualServiceNotFound(t *testing.T) {
	client := testutil.NewFakeDynamicClient()
	ro := rolloutWithHttpRoutes("stable", "canary", "vsvc", []string{"primary"})
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
	ro := rolloutWithHttpRoutes("stable", "canary", "vsvc", nil)
	vsvcLister, druleLister := getIstioListers(client)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()
	err := r.SetWeight(0)
	assert.Equal(t, "spec.http[] should be set in VirtualService and it must have exactly one route when omitting spec.strategy.canary.trafficRouting.istio.virtualService.routes", err.Error())
}

func TestTlsReconcileAmbiguousRoutes(t *testing.T) {
	obj := unstructuredutil.StrToUnstructuredUnsafe(regularTlsVsvc)
	client := testutil.NewFakeDynamicClient(obj)
	ro := rolloutWithHttpRoutes("stable", "canary", "vsvc", nil)
	vsvcLister, druleLister := getIstioListers(client)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()
	err := r.SetWeight(0)
	assert.Equal(t, "spec.tls[] should be set in VirtualService and it must have exactly one route when omitting spec.strategy.canary.trafficRouting.istio.virtualService.tlsRoutes", err.Error())
}

// TestReconcileInferredSingleRoute we can support case where we infer the only route in the VirtualService
func TestHttpReconcileInferredSingleRoute(t *testing.T) {
	ro := rolloutWithHttpRoutes("stable", "canary", "vsvc", nil)
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
	ro := rolloutWithTlsRoutes("stable", "canary", "vsvc", nil)
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

func TestReconcileInferredSingleRoute(t *testing.T) {
	ro := rolloutWithHttpAndTlsRoutes("stable", "canary", "vsvc", nil, nil)
	client := AssertReconcileUpdateVirtualService(t, singleRouteMixedVsvc, ro)

	// Verify we actually made the correct change
	vsvcUn, err := client.Resource(istioutil.GetIstioVirtualServiceGVR()).Namespace(ro.Namespace).Get(context.TODO(), "vsvc", metav1.GetOptions{})
	assert.NoError(t, err)

	// HTTP Routes
	vsHttpRoutes, _, _ := unstructured.NestedSlice(vsvcUn.Object, "spec", "http")
	routeBytes, _ := json.Marshal(vsHttpRoutes)
	var httpRoutes []VirtualServiceHTTPRoute
	err = json.Unmarshal(routeBytes, &httpRoutes)
	assert.Nil(t, err)
	httpRoute := httpRoutes[0]
	checkDestination(t, httpRoute.Route, "stable", 90)
	checkDestination(t, httpRoute.Route, "canary", 10)

	// TLS Routes
	vsTlsRoutes, _, _ := unstructured.NestedSlice(vsvcUn.Object, "spec", "tls")
	routeBytes, _ = json.Marshal(vsTlsRoutes)
	var tlsRoutes []VirtualServiceTLSRoute
	err = json.Unmarshal(routeBytes, &tlsRoutes)
	assert.Nil(t, err)
	tlsRoute := tlsRoutes[0]
	checkDestination(t, tlsRoute.Route, "stable", 90)
	checkDestination(t, tlsRoute.Route, "canary", 10)
}

func TestType(t *testing.T) {
	client := testutil.NewFakeDynamicClient()
	ro := rolloutWithHttpRoutes("stable", "canary", "vsvc", []string{"primary"})
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
								VirtualService: &v1alpha1.IstioVirtualService{
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
		}, {
			Destination: VirtualServiceDestination{
				Host: "canary",
			},
		}},
	}}
	rollout := newRollout([]string{"test"})
	vsvcRoutes := rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Routes

	err := ValidateHTTPRoutes(rollout, vsvcRoutes, httpRoutes)
	assert.Nil(t, err)

	rolloutWithNotFoundRoute := newRollout([]string{"not-found-route"})
	vsvcRoutes = rolloutWithNotFoundRoute.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Routes
	err = ValidateHTTPRoutes(rolloutWithNotFoundRoute, vsvcRoutes, httpRoutes)
	assert.Equal(t, "HTTP Route 'not-found-route' is not found in the defined Virtual Service.", err.Error())
}

func TestValidateTLSRoutes(t *testing.T) {
	newRollout := func(routes []string, tlsRoutes []v1alpha1.TLSRoute) *v1alpha1.Rollout {
		return &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					Canary: &v1alpha1.CanaryStrategy{
						StableService: "stable",
						CanaryService: "canary",
						TrafficRouting: &v1alpha1.RolloutTrafficRouting{
							Istio: &v1alpha1.IstioTrafficRouting{
								VirtualService: &v1alpha1.IstioVirtualService{
									Routes:    routes,
									TLSRoutes: tlsRoutes,
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

	rollout := newRollout([]string{},
		[]v1alpha1.TLSRoute{
			{
				Port: 3000,
			},
		},
	)

	tlsRoutes[0].Route = []VirtualServiceRouteDestination{{
		Destination: VirtualServiceDestination{
			Host: "stable",
		},
	}, {
		Destination: VirtualServiceDestination{
			Host: "canary",
		},
	}}
	vsvcTLSRoutes := rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.TLSRoutes
	err := ValidateTlsRoutes(rollout, vsvcTLSRoutes, tlsRoutes)
	assert.Nil(t, err)

	rolloutWithNotFoundRoute := newRollout([]string{},
		[]v1alpha1.TLSRoute{
			{
				Port: 2002,
			},
		},
	)
	vsvcTLSRoutes = rolloutWithNotFoundRoute.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.TLSRoutes
	err = ValidateTlsRoutes(rolloutWithNotFoundRoute, vsvcTLSRoutes, tlsRoutes)
	assert.Equal(t, NoTlsRouteFoundError, err.Error())
}

func TestValidateHosts(t *testing.T) {
	hr := VirtualServiceHTTPRoute{
		Name: "test",
		Route: []VirtualServiceRouteDestination{{
			Destination: VirtualServiceDestination{
				Host: "stable",
			},
		}, {
			Destination: VirtualServiceDestination{
				Host: "canary",
			},
		}},
	}

	err := validateVirtualServiceRouteDestinations(hr.Route, "stable", "canary", nil)
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
							VirtualService: &v1alpha1.IstioVirtualService{
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

	vsvcRoutes := rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Routes

	{
		// the good case
		err := ValidateHTTPRoutes(rollout, vsvcRoutes, httpRoutes)
		assert.NoError(t, err)
	}
	{
		// the stable subset doesnt exist
		rollout = rollout.DeepCopy()
		rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule.StableSubsetName = "doesntexist"
		err := ValidateHTTPRoutes(rollout, vsvcRoutes, httpRoutes)
		assert.EqualError(t, err, "Stable DestinationRule subset 'doesntexist' not found in route")
	}
	{
		// the canary subset doesnt exist
		rollout = rollout.DeepCopy()
		rollout.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule.CanarySubsetName = "doesntexist"
		err := ValidateHTTPRoutes(rollout, vsvcRoutes, httpRoutes)
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
							VirtualService: &v1alpha1.IstioVirtualService{
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

func TestUpdateHashWithAdditionalDestinations(t *testing.T) {
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

	// UpdateHash for 1 additional destination
	additionalDestinations := []v1alpha1.WeightDestination{
		{
			ServiceName:     "exp-svc",
			PodTemplateHash: "exp-hash",
			Weight:          20,
		},
	}
	err := r.UpdateHash("abc123", "def456", additionalDestinations...)
	assert.NoError(t, err)
	actions := client.Actions()
	assert.Len(t, actions, 1)
	assert.Equal(t, "update", actions[0].GetVerb())
	dRuleUn, err := client.Resource(istioutil.GetIstioDestinationRuleGVR()).Namespace(r.rollout.Namespace).Get(context.TODO(), "istio-destrule", metav1.GetOptions{})
	assert.NoError(t, err)
	_, dRule, _, err := unstructuredToDestinationRules(dRuleUn)
	assert.NoError(t, err)
	assert.Equal(t, dRule.Annotations[v1alpha1.ManagedByRolloutsKey], "rollout")
	assert.Len(t, dRule.Spec.Subsets, 3)
	assert.Equal(t, "def456", dRule.Spec.Subsets[0].Labels[v1alpha1.DefaultRolloutUniqueLabelKey])
	assert.Equal(t, "abc123", dRule.Spec.Subsets[1].Labels[v1alpha1.DefaultRolloutUniqueLabelKey])
	assert.Equal(t, "exp-svc", dRule.Spec.Subsets[2].Name)
	assert.Equal(t, "exp-hash", dRule.Spec.Subsets[2].Labels[v1alpha1.DefaultRolloutUniqueLabelKey])

	// Add another additionalDestination
	client = testutil.NewFakeDynamicClient(dRuleUn)
	vsvcLister, druleLister = getIstioListers(client)
	r = NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()
	additionalDestinations = append(additionalDestinations, v1alpha1.WeightDestination{
		ServiceName:     "exp-svc2",
		PodTemplateHash: "exp-hash2",
		Weight:          40,
	},
	)
	err = r.UpdateHash("abc123", "def456", additionalDestinations...)
	assert.NoError(t, err)
	actions = client.Actions()
	assert.Len(t, actions, 1)
	assert.Equal(t, "update", actions[0].GetVerb())
	dRuleUn, err = client.Resource(istioutil.GetIstioDestinationRuleGVR()).Namespace(r.rollout.Namespace).Get(context.TODO(), "istio-destrule", metav1.GetOptions{})
	assert.NoError(t, err)
	_, dRule, _, err = unstructuredToDestinationRules(dRuleUn)
	assert.NoError(t, err)
	assert.Len(t, dRule.Spec.Subsets, 4)
	assert.Equal(t, "exp-svc2", dRule.Spec.Subsets[3].Name)
	assert.Equal(t, "exp-hash2", dRule.Spec.Subsets[3].Labels[v1alpha1.DefaultRolloutUniqueLabelKey])

	// Remove 1 of additionalDestinations
	client = testutil.NewFakeDynamicClient(dRuleUn)
	vsvcLister, druleLister = getIstioListers(client)
	r = NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()
	err = r.UpdateHash("abc123", "def456", additionalDestinations[1])
	assert.NoError(t, err)
	actions = client.Actions()
	assert.Len(t, actions, 1)
	assert.Equal(t, "update", actions[0].GetVerb())
	dRuleUn, err = client.Resource(istioutil.GetIstioDestinationRuleGVR()).Namespace(r.rollout.Namespace).Get(context.TODO(), "istio-destrule", metav1.GetOptions{})
	assert.NoError(t, err)
	_, dRule, _, err = unstructuredToDestinationRules(dRuleUn)
	assert.NoError(t, err)
	assert.Len(t, dRule.Spec.Subsets, 3)
	assert.Equal(t, "exp-svc2", dRule.Spec.Subsets[2].Name)
	assert.Equal(t, "exp-hash2", dRule.Spec.Subsets[2].Labels[v1alpha1.DefaultRolloutUniqueLabelKey])
}

//Multiple Virtual Service Support Unit Tests

func multiVsRollout(stableSvc string, canarySvc string, multipleVirtualService []v1alpha1.IstioVirtualService) *v1alpha1.Rollout {
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
							VirtualServices: multipleVirtualService,
						},
					},
				},
			},
		},
	}
}

const sampleRouteVirtualService1 = `apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: vsvc1
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

const sampleRouteVirtualService2 = `apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: vsvc2
  namespace: default
spec:
  gateways:
  - istio-rollout-gateway
  hosts:
  - istio-rollout.dev.argoproj.io
  http:
  - name: blue-green
    route:
    - destination:
        host: 'stable'
      weight: 100
    - destination:
        host: canary
      weight: 0`

const singleRouteMultipleVirtualService1 = `apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: vsvc1
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

const singleRouteMultipleVirtualService2 = `apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: vsvc2
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

func TestMultipleVirtualServiceConfigured(t *testing.T) {
	multipleVirtualService := []v1alpha1.IstioVirtualService{{Name: "vsvc1", Routes: []string{"primary", "secondary"}}, {Name: "vsvc2", Routes: []string{"blue-green"}}}
	ro := multiVsRollout("stable", "canary", multipleVirtualService)
	mvsvc := istioutil.MultipleVirtualServiceConfigured(ro)
	assert.Equal(t, true, mvsvc)
	istioVirtualService := &v1alpha1.IstioVirtualService{
		Name:   "vsvc",
		Routes: []string{"primary"},
	}
	ro = rollout("stable", "canary", istioVirtualService)
	mvsvc = istioutil.MultipleVirtualServiceConfigured(ro)
	assert.Equal(t, false, mvsvc)
}

//This Testcase validates the reconcileVirtualService using VirtualServices configuration
func TestMultipleVirtualServiceReconcileWeightsBaseCase(t *testing.T) {

	multipleVirtualService := []v1alpha1.IstioVirtualService{{Name: "vsvc", Routes: []string{"secondary"}, TLSRoutes: []v1alpha1.TLSRoute{{Port: 3000}}}}
	mr := &Reconciler{
		rollout: multiVsRollout("stable", "canary", multipleVirtualService),
	}

	obj := unstructuredutil.StrToUnstructuredUnsafe(regularMixedVsvc)

	// Choosing the second virtual service i.e., secondary
	vsvc := mr.rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualServices[0]
	modifiedObj, _, err := mr.reconcileVirtualService(obj, vsvc.Routes, vsvc.TLSRoutes, 20)
	assert.Nil(t, err)
	assert.NotNil(t, modifiedObj)

	// HTTP Routes
	httpRoutes := extractHttpRoutes(t, modifiedObj)

	// Assertions
	assertHttpRouteWeightChanges(t, httpRoutes[0], "primary", 0, 100)
	assertHttpRouteWeightChanges(t, httpRoutes[1], "secondary", 20, 80)

	// TLS Routes
	tlsRoutes := extractTlsRoutes(t, modifiedObj)

	// Assestions
	assertTlsRouteWeightChanges(t, tlsRoutes[0], nil, 3000, 20, 80)
	assertTlsRouteWeightChanges(t, tlsRoutes[1], nil, 3001, 0, 100)
}

func TestMultipleVirtualServiceReconcileNoChanges(t *testing.T) {
	obj1 := unstructuredutil.StrToUnstructuredUnsafe(sampleRouteVirtualService1)
	obj2 := unstructuredutil.StrToUnstructuredUnsafe(sampleRouteVirtualService2)
	client := testutil.NewFakeDynamicClient(obj1, obj2)
	multipleVirtualService := []v1alpha1.IstioVirtualService{{Name: "vsvc1", Routes: []string{"primary", "secondary"}}, {Name: "vsvc2", Routes: []string{"blue-green"}}}
	ro := multiVsRollout("stable", "canary", multipleVirtualService)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), nil, nil)
	err := r.SetWeight(0)
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), 2)
	assert.Equal(t, "get", client.Actions()[0].GetVerb())
	assert.Equal(t, "get", client.Actions()[1].GetVerb())
}

func TestMultipleVirtualServiceReconcileUpdateVirtualServices(t *testing.T) {
	obj1 := unstructuredutil.StrToUnstructuredUnsafe(sampleRouteVirtualService1)
	obj2 := unstructuredutil.StrToUnstructuredUnsafe(sampleRouteVirtualService2)
	client := testutil.NewFakeDynamicClient(obj1, obj2)
	multipleVirtualService := []v1alpha1.IstioVirtualService{{Name: "vsvc1", Routes: []string{"primary", "secondary"}}, {Name: "vsvc2", Routes: []string{"blue-green"}}}
	ro := multiVsRollout("stable", "canary", multipleVirtualService)
	vsvcLister, druleLister := getIstioListers(client)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()
	err := r.SetWeight(10)
	assert.Nil(t, err)
	actions := client.Actions()
	assert.Len(t, actions, 2)
	assert.Equal(t, "update", actions[0].GetVerb())
	assert.Equal(t, "update", actions[1].GetVerb())
}

func TestMultipleVirtualServiceReconcileInvalidValidation(t *testing.T) {
	obj1 := unstructuredutil.StrToUnstructuredUnsafe(sampleRouteVirtualService1)
	obj2 := unstructuredutil.StrToUnstructuredUnsafe(sampleRouteVirtualService2)
	client := testutil.NewFakeDynamicClient(obj1, obj2)
	multipleVirtualService := []v1alpha1.IstioVirtualService{{Name: "vsvc1", Routes: []string{"route-not-found"}}, {Name: "vsvc2", Routes: []string{"route-not-found"}}}
	ro := multiVsRollout("stable", "canary", multipleVirtualService)
	vsvcLister, druleLister := getIstioListers(client)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()
	err := r.SetWeight(0)
	assert.Equal(t, "HTTP Route 'route-not-found' is not found in the defined Virtual Service.", err.Error())
}

func TestMultipleVirtualServiceReconcileVirtualServiceNotFound(t *testing.T) {
	obj := unstructuredutil.StrToUnstructuredUnsafe(sampleRouteVirtualService1)
	client := testutil.NewFakeDynamicClient(obj)
	multipleVirtualService := []v1alpha1.IstioVirtualService{{Name: "vsvc1", Routes: []string{"primary", "secondary"}}, {Name: "vsvc2", Routes: []string{"blue-green"}}}
	ro := multiVsRollout("stable", "canary", multipleVirtualService)
	vsvcLister, druleLister := getIstioListers(client)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()
	err := r.SetWeight(10)
	assert.NotNil(t, err)
	assert.True(t, k8serrors.IsNotFound(err))
}

// TestReconcileAmbiguousRoutes tests when we omit route names and there are multiple routes in the VirtualService
func TestMultipleVirtualServiceReconcileAmbiguousRoutes(t *testing.T) {
	obj1 := unstructuredutil.StrToUnstructuredUnsafe(sampleRouteVirtualService1)
	obj2 := unstructuredutil.StrToUnstructuredUnsafe(sampleRouteVirtualService2)
	client := testutil.NewFakeDynamicClient(obj1, obj2)
	multipleVirtualService := []v1alpha1.IstioVirtualService{{Name: "vsvc1", Routes: nil}, {Name: "vsvc2", Routes: nil}}
	ro := multiVsRollout("stable", "canary", multipleVirtualService)
	vsvcLister, druleLister := getIstioListers(client)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()
	err := r.SetWeight(0)
	assert.Equal(t, "spec.http[] should be set in VirtualService and it must have exactly one route when omitting spec.strategy.canary.trafficRouting.istio.virtualService.routes", err.Error())
}

// TestReconcileInferredSingleRoute we can support case where we infer the only route in the VirtualService
func TestMultipleVirtualServiceReconcileInferredSingleRoute(t *testing.T) {
	obj1 := unstructuredutil.StrToUnstructuredUnsafe(singleRouteMultipleVirtualService1)
	obj2 := unstructuredutil.StrToUnstructuredUnsafe(singleRouteMultipleVirtualService2)
	client := testutil.NewFakeDynamicClient(obj1, obj2)
	multipleVirtualService := []v1alpha1.IstioVirtualService{{Name: "vsvc1", Routes: nil}, {Name: "vsvc2", Routes: nil}}
	ro := multiVsRollout("stable", "canary", multipleVirtualService)
	vsvcLister, druleLister := getIstioListers(client)
	r := NewReconciler(ro, client, record.NewFakeEventRecorder(), vsvcLister, druleLister)
	client.ClearActions()
	err := r.SetWeight(10)
	assert.NoError(t, err)
	actions := client.Actions()
	assert.Len(t, actions, 2)
	assert.Equal(t, "update", actions[0].GetVerb())
	assert.Equal(t, "update", actions[1].GetVerb())

	// Verify we actually made the correct change
	for _, vsvName := range []string{"vsvc1", "vsvc2"} {
		vsvcUn, err := client.Resource(istioutil.GetIstioVirtualServiceGVR()).Namespace(ro.Namespace).Get(context.TODO(), vsvName, metav1.GetOptions{})
		assert.NoError(t, err)
		// HTTP Routes
		httpRoutes := extractHttpRoutes(t, vsvcUn)
		// Assertions
		assertHttpRouteWeightChanges(t, httpRoutes[0], "", 10, 90)
	}
}
