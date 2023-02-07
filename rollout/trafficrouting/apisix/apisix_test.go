package apisix

import (
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/apisix/mocks"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
)

const apisixRoute = `
apiVersion: apisix.apache.org/v2
kind: ApisixRoute
metadata:
  name: mocks-apisix-route
spec:
  http:
    - name: mocks-apisix-route
      match:
        paths:
          - /*
        methods:
          - GET
      backends:
        - serviceName: stable-rollout
          servicePort: 80
          weight: 100
        - serviceName: canary-rollout
          servicePort: 80
          weight: 0
`

const errorApisixRoute = `
apiVersion: apisix.apache.org/v2
kind: ApisixRoute
metadata:
  name: mocks-apisix-route
`

var (
	client *mocks.FakeClient = &mocks.FakeClient{}
)

const (
	stableServiceName     string = "stable-rollout"
	fakeStableServiceName string = "fake-stable-rollout"
	canaryServiceName     string = "canary-rollout"
	fakeCanaryServiceName string = "fake-canary-rollout"
	apisixRouteName       string = "mocks-apisix-route"
)

func TestUpdateHash(t *testing.T) {
	t.Run("UpdateHash", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		err := r.UpdateHash("", "")

		// Then
		assert.NoError(t, err)
	})
}

func TestSetWeight(t *testing.T) {
	mocks.ApisixRouteObj = toUnstructured(t, apisixRoute)
	mocks.ErrorApisixRouteObj = toUnstructured(t, errorApisixRoute)
	t.Run("SetWeight", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetWeight(30)

		// Then
		assert.NoError(t, err)
		apisixHttpRoutesObj, isFound, err := unstructured.NestedSlice(mocks.ApisixRouteObj.Object, "spec", "http")
		assert.NoError(t, err)
		assert.Equal(t, isFound, true)
		apisixHttpRouteObj, err := GetHttpRoute(apisixHttpRoutesObj, apisixRouteName)
		assert.NoError(t, err)
		backends, err := GetBackends(apisixHttpRouteObj)
		assert.NoError(t, err)
		for _, backend := range backends {
			typedBackend, ok := backend.(map[string]interface{})
			assert.Equal(t, ok, true)
			nameOfCurrentBackend, isFound, err := unstructured.NestedString(typedBackend, "serviceName")
			assert.NoError(t, err)
			assert.Equal(t, isFound, true)
			if nameOfCurrentBackend == stableServiceName {
				rawWeight, ok := typedBackend["weight"]
				assert.Equal(t, ok, true)
				weight, ok := rawWeight.(int64)
				assert.Equal(t, ok, true)
				assert.Equal(t, weight, int64(70))
			}
			if nameOfCurrentBackend == canaryServiceName {
				rawWeight, ok := typedBackend["weight"]
				assert.Equal(t, ok, true)
				weight, ok := rawWeight.(int64)
				assert.Equal(t, ok, true)
				assert.Equal(t, weight, int64(30))
			}
		}
	})
	t.Run("SetWeightWithError", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client: &mocks.FakeClient{
				IsGetError: true,
			},
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetWeight(30)

		// Then
		assert.Error(t, err)
	})
	t.Run("SetWeightWithErrorManifest", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client: &mocks.FakeClient{
				IsGetErrorManifest: true,
			},
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetWeight(30)

		// Then
		assert.Error(t, err)
	})
	t.Run("SetWeightWithErrorStableName", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(fakeStableServiceName, canaryServiceName, apisixRouteName),
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetWeight(30)

		// Then
		assert.Error(t, err)
	})
	t.Run("SetWeightWithErrorCanaryName", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, fakeCanaryServiceName, apisixRouteName),
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetWeight(30)

		// Then
		assert.Error(t, err)
	})
	t.Run("ApisixUpdateError", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client: &mocks.FakeClient{
				UpdateError: true,
			},
			Recorder: &mocks.FakeRecorder{},
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetWeight(30)

		// Then
		assert.Error(t, err)
	})
}

func TestGetHttpRouteError(t *testing.T) {
	type testcase struct {
		routes []interface{}
		ref    string
	}
	testcases := []testcase{
		{
			routes: nil,
			ref:    "nil",
		},
		{
			routes: []interface{}{""},
			ref:    "Failed type",
		},
		{
			routes: []interface{}{
				map[string]interface{}{
					"x": nil,
				},
			},
			ref: "noname",
		},
		{
			routes: []interface{}{
				map[string]interface{}{
					"name": 123,
				},
			},
			ref: "name type error",
		},
		{
			routes: []interface{}{
				map[string]interface{}{
					"name": "123",
				},
			},
			ref: "name not found",
		},
	}

	for _, tc := range testcases {
		_, err := GetHttpRoute(tc.routes, tc.ref)
		assert.Error(t, err)
	}
}

func TestGetBackendsError(t *testing.T) {
	testcases := []interface{}{
		nil,
		123,
		map[string]interface{}{},
		map[string]interface{}{
			"backends": "123",
		},
	}

	for _, tc := range testcases {
		_, err := GetBackends(tc)
		assert.Error(t, err)
	}
}

func TestSetBackendWeightError(t *testing.T) {
	type testcase struct {
		backendName string
		backends    []interface{}
		weight      int64
	}
	testcases := []testcase{
		{},
		{
			backends: []interface{}{
				"",
			},
		},
		{
			backends: []interface{}{
				map[string]interface{}{
					"abc": 123,
				},
			},
		},
		{
			backends: []interface{}{
				map[string]interface{}{
					"serviceName": 123,
				},
			},
		},
	}

	for _, tc := range testcases {
		err := setBackendWeight(tc.backendName, tc.backends, tc.weight)
		assert.Error(t, err)
	}
}

func TestSetHeaderRoute(t *testing.T) {
	t.Run("SetHeaderRoute", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetHeaderRoute(&v1alpha1.SetHeaderRoute{
			Name: "set-header",
			Match: []v1alpha1.HeaderRoutingMatch{{
				HeaderName: "header-name",
				HeaderValue: &v1alpha1.StringMatch{
					Exact: "value",
				},
			}},
		})

		// Then
		assert.NoError(t, err)

		err = r.RemoveManagedRoutes()
		assert.Nil(t, err)
	})
}

func TestSetMirrorRoute(t *testing.T) {
	t.Run("SetMirrorRoute", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetMirrorRoute(&v1alpha1.SetMirrorRoute{
			Name: "mirror-route",
			Match: []v1alpha1.RouteMatch{{
				Method: &v1alpha1.StringMatch{Exact: "GET"},
			}},
		})

		// Then
		assert.NoError(t, err)

		err = r.RemoveManagedRoutes()
		assert.Nil(t, err)
	})
}

func toUnstructured(t *testing.T, manifest string) *unstructured.Unstructured {
	t.Helper()
	obj := &unstructured.Unstructured{}

	dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	_, _, err := dec.Decode([]byte(manifest), nil, obj)
	if err != nil {
		t.Fatal(err)
	}
	return obj
}

func TestVerifyWeight(t *testing.T) {
	t.Run("VerifyWeight", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		isSynced, err := r.VerifyWeight(32)

		// Then
		assert.Nil(t, isSynced)
		assert.Nil(t, err)
	})
}

func TestType(t *testing.T) {
	mocks.ApisixRouteObj = toUnstructured(t, apisixRoute)
	t.Run("Type", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		reconcilerType := r.Type()

		// Then
		assert.Equal(t, Type, reconcilerType)
	})
}

func newRollout(stableSvc, canarySvc, apisixRouteRef string) *v1alpha1.Rollout {
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
						Apisix: &v1alpha1.ApisixTrafficRouting{
							Route: &v1alpha1.ApisixRoute{
								Name: apisixRouteRef,
							},
						},
					},
				},
			},
		},
	}
}
