package apisix

import (
	"errors"
	"fmt"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/apisix/mocks"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
)

const SetHeaderRouteName = "set-header"
const HeaderName = "header-name"

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

const apisixSetHeaderRoute = `
apiVersion: apisix.apache.org/v2
kind: ApisixRoute
metadata:
  name: set-header
  ownerReferences:
    - apiVersion: argoproj.io/v1alpha1
      blockOwnerDeletion: true
      controller: true
      kind: Rollout
      name: rollout
      uid: 1a2b2d82-50a4-4d83-9ff4-cdc6f5197d30
spec:
  http:
    - backends:
        - serviceName: canary-rollout
          servicePort: 80
          weight: 100
      match:
        exprs:
          - op: Equal
            subject:
              name: trace
              scope: Header
            value: debug
        hosts:
          - rollouts-demo.apisix.local
        methods:
          - GET
          - POST
          - PUT
          - DELETE
          - PATCH
        paths:
          - /*
      name: mocks-apisix-route
      priority: 2
`

const apisixSetHeaderDuplicateRoute = `
apiVersion: apisix.apache.org/v2
kind: ApisixRoute
metadata:
  name: set-header
spec:
  http:
    - backends:
        - serviceName: canary-rollout
          servicePort: 80
          weight: 100
      match:
        exprs:
          - op: Equal
            subject:
              name: trace
              scope: Header
            value: debug
        hosts:
          - rollouts-demo.apisix.local
        methods:
          - GET
          - POST
          - PUT
          - DELETE
          - PATCH
        paths:
          - /*
      name: mocks-apisix-route
      priority: 2
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
	setHeaderName         string = "mocks-set-header"
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
	mocks.ApisixRouteObj = toUnstructured(t, apisixRoute)
	mocks.SetHeaderApisixRouteObj = toUnstructured(t, apisixSetHeaderRoute)
	mocks.DuplicateSetHeaderApisixRouteObj = toUnstructured(t, apisixSetHeaderDuplicateRoute)
	mocks.ErrorApisixRouteObj = toUnstructured(t, errorApisixRoute)
	t.Run("SetHeaderGetRouteError", func(t *testing.T) {
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client: &mocks.FakeClient{
				IsGetError: true,
			},
		}
		r := NewReconciler(&cfg)
		// When
		err := r.SetHeaderRoute(&v1alpha1.SetHeaderRoute{
			Name: SetHeaderRouteName,
		})

		// Then
		assert.Error(t, err)
	})
	t.Run("SetHeaderGetManagedRouteError", func(t *testing.T) {
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client: &mocks.FakeClient{
				IsGetManagedRouteError: true,
			},
		}
		r := NewReconciler(&cfg)
		// When
		err := r.SetHeaderRoute(&v1alpha1.SetHeaderRoute{
			Name: SetHeaderRouteName,
			Match: []v1alpha1.HeaderRoutingMatch{{
				HeaderName: HeaderName,
				HeaderValue: &v1alpha1.StringMatch{
					Exact: "value",
				},
			}},
		})

		// Then
		assert.Error(t, err)
	})
	t.Run("SetHeaderDuplicateManagedRouteError", func(t *testing.T) {
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client: &mocks.FakeClient{
				IsDuplicateSetHeaderRouteError: true,
			},
		}
		r := NewReconciler(&cfg)
		// When
		err := r.SetHeaderRoute(&v1alpha1.SetHeaderRoute{
			Name: SetHeaderRouteName,
			Match: []v1alpha1.HeaderRoutingMatch{{
				HeaderName: HeaderName,
				HeaderValue: &v1alpha1.StringMatch{
					Exact: "value",
				},
			}},
		})

		// Then
		assert.ErrorContains(t, err, "duplicate ApisixRoute")

	})
	t.Run("SetHeaderRouteNilMatchWithNew", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client: &mocks.FakeClient{
				IsGetNotFoundError: true,
			},
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetHeaderRoute(&v1alpha1.SetHeaderRoute{
			Name:  SetHeaderRouteName,
			Match: nil,
		})

		// Then
		assert.NoError(t, err)
	})
	t.Run("SetHeaderRouteNilMatch", func(t *testing.T) {
		client := &mocks.FakeClient{}
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetHeaderRoute(&v1alpha1.SetHeaderRoute{
			Name:  SetHeaderRouteName,
			Match: nil,
		})

		// Then
		assert.NoError(t, err)
		assert.Equal(t, SetHeaderRouteName, client.DeleteName)
	})
	t.Run("SetHeaderRoutePriorityWithNew", func(t *testing.T) {
		// Given
		t.Parallel()
		client := &mocks.FakeClient{
			IsGetNotFoundError: true,
		}
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetHeaderRoute(&v1alpha1.SetHeaderRoute{
			Name: SetHeaderRouteName,
			Match: []v1alpha1.HeaderRoutingMatch{{
				HeaderName: HeaderName,
				HeaderValue: &v1alpha1.StringMatch{
					Exact: "value",
				},
			}},
		})

		// Then
		assert.NoError(t, err)
		rules, ok, err := unstructured.NestedSlice(client.CreatedObj.Object, "spec", "http")
		assert.NoError(t, err)
		assert.Equal(t, true, ok)

		rule, ok := rules[0].(map[string]interface{})
		assert.Equal(t, true, ok)
		priority, ok, err := unstructured.NestedInt64(rule, "priority")
		assert.NoError(t, err)
		assert.Equal(t, true, ok)
		assert.Equal(t, int64(1), priority)
	})
	t.Run("SetHeaderRoutePriorityWithNew", func(t *testing.T) {
		// Given
		t.Parallel()
		client := &mocks.FakeClient{
			IsGetNotFoundError: false,
		}
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetHeaderRoute(&v1alpha1.SetHeaderRoute{
			Name: SetHeaderRouteName,
			Match: []v1alpha1.HeaderRoutingMatch{{
				HeaderName: HeaderName,
				HeaderValue: &v1alpha1.StringMatch{
					Exact: "value",
				},
			}},
		})

		// Then
		assert.NoError(t, err)
		rules, ok, err := unstructured.NestedSlice(client.UpdatedObj.Object, "spec", "http")
		assert.NoError(t, err)
		assert.Equal(t, true, ok)

		rule, ok := rules[0].(map[string]interface{})
		assert.Equal(t, true, ok)
		priority, ok, err := unstructured.NestedInt64(rule, "priority")
		assert.NoError(t, err)
		assert.Equal(t, true, ok)
		assert.Equal(t, int64(2), priority)
	})

	t.Run("SetHeaderRouteExprsWithNew", func(t *testing.T) {
		// Given
		t.Parallel()
		client := &mocks.FakeClient{
			IsGetNotFoundError: true,
		}
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetHeaderRoute(&v1alpha1.SetHeaderRoute{
			Name: SetHeaderRouteName,
			Match: []v1alpha1.HeaderRoutingMatch{
				{
					HeaderName: HeaderName,
					HeaderValue: &v1alpha1.StringMatch{
						Exact: "value",
					},
				},
				{
					HeaderName: HeaderName,
					HeaderValue: &v1alpha1.StringMatch{
						Regex: "value",
					},
				}, {
					HeaderName: HeaderName,
					HeaderValue: &v1alpha1.StringMatch{
						Prefix: "value",
					},
				}},
		})

		// Then
		assert.NoError(t, err)
		rules, ok, err := unstructured.NestedSlice(client.CreatedObj.Object, "spec", "http")
		assert.NoError(t, err)
		assert.Equal(t, true, ok)

		rule, ok := rules[0].(map[string]interface{})
		assert.Equal(t, true, ok)
		exprs, ok, err := unstructured.NestedSlice(rule, "match", "exprs")
		assert.NoError(t, err)
		assert.Equal(t, true, ok)
		values := [][]string{
			{"Equal", HeaderName, "Header", "value"},
			{"RegexMatch", HeaderName, "Header", "value"},
			{"RegexMatch", HeaderName, "Header", fmt.Sprintf("^%s.*", "value")},
		}
		for i, expr := range exprs {
			assertExpr(t, expr, values[i][0], values[i][1], values[i][2], values[i][3])
		}
	})
	t.Run("SetHeaderRouteExprs", func(t *testing.T) {
		// Given
		t.Parallel()
		client := &mocks.FakeClient{
			IsGetNotFoundError: false,
		}
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetHeaderRoute(&v1alpha1.SetHeaderRoute{
			Name: SetHeaderRouteName,
			Match: []v1alpha1.HeaderRoutingMatch{
				{
					HeaderName: HeaderName,
					HeaderValue: &v1alpha1.StringMatch{
						Exact: "value",
					},
				},
				{
					HeaderName: HeaderName,
					HeaderValue: &v1alpha1.StringMatch{
						Regex: "value",
					},
				}, {
					HeaderName: HeaderName,
					HeaderValue: &v1alpha1.StringMatch{
						Prefix: "value",
					},
				}},
		})

		// Then
		assert.NoError(t, err)
		rules, ok, err := unstructured.NestedSlice(client.UpdatedObj.Object, "spec", "http")
		assert.NoError(t, err)
		assert.Equal(t, true, ok)

		rule, ok := rules[0].(map[string]interface{})
		assert.Equal(t, true, ok)
		exprs, ok, err := unstructured.NestedSlice(rule, "match", "exprs")
		assert.NoError(t, err)
		assert.Equal(t, true, ok)
		values := [][]string{
			{"Equal", HeaderName, "Header", "value"},
			{"RegexMatch", HeaderName, "Header", "value"},
			{"RegexMatch", HeaderName, "Header", fmt.Sprintf("^%s.*", "value")},
		}
		for i, expr := range exprs {
			assertExpr(t, expr, values[i][0], values[i][1], values[i][2], values[i][3])
		}
	})
	t.Run("SetHeaderDeleteError", func(t *testing.T) {
		// Given
		t.Parallel()
		client := &mocks.FakeClient{
			IsDeleteError: true,
		}
		cfg := ReconcilerConfig{
			Rollout:  newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client:   client,
			Recorder: &mocks.FakeRecorder{},
		}
		r := NewReconciler(&cfg)
		err := r.SetHeaderRoute(&v1alpha1.SetHeaderRoute{
			Name:  SetHeaderRouteName,
			Match: nil,
		})
		assert.Error(t, err)
	})
	t.Run("SetHeaderCreateError", func(t *testing.T) {
		// Given
		t.Parallel()
		client := &mocks.FakeClient{
			IsCreateError:      true,
			IsGetNotFoundError: true,
		}
		cfg := ReconcilerConfig{
			Rollout:  newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client:   client,
			Recorder: &mocks.FakeRecorder{},
		}
		r := NewReconciler(&cfg)
		err := r.SetHeaderRoute(&v1alpha1.SetHeaderRoute{
			Name: SetHeaderRouteName,
			Match: []v1alpha1.HeaderRoutingMatch{{
				HeaderName: HeaderName,
				HeaderValue: &v1alpha1.StringMatch{
					Exact: "value",
				},
			}},
		})
		assert.Error(t, err)
	})
	t.Run("SetHeaderUpdateError", func(t *testing.T) {
		// Given
		t.Parallel()
		client := &mocks.FakeClient{
			UpdateError:        true,
			IsGetNotFoundError: false,
		}
		cfg := ReconcilerConfig{
			Rollout:  newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client:   client,
			Recorder: &mocks.FakeRecorder{},
		}
		r := NewReconciler(&cfg)
		err := r.SetHeaderRoute(&v1alpha1.SetHeaderRoute{
			Name: SetHeaderRouteName,
			Match: []v1alpha1.HeaderRoutingMatch{{
				HeaderName: HeaderName,
				HeaderValue: &v1alpha1.StringMatch{
					Exact: "value",
				},
			}},
		})
		assert.Error(t, err)
	})
	t.Run("RemoveManagedRoutesDeleteError", func(t *testing.T) {
		// Given
		t.Parallel()
		client := &mocks.FakeClient{
			IsDeleteError: true,
		}
		cfg := ReconcilerConfig{
			Rollout:  newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client:   client,
			Recorder: &mocks.FakeRecorder{},
		}
		r := NewReconciler(&cfg)
		err := r.RemoveManagedRoutes()
		assert.Error(t, err)
	})
	t.Run("RemoveManagedRoutesNilManagedRoutes", func(t *testing.T) {
		// Given
		t.Parallel()
		client := &mocks.FakeClient{
			IsDeleteError: true,
		}
		cfg := ReconcilerConfig{
			Rollout:  newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client:   client,
			Recorder: &mocks.FakeRecorder{},
		}
		cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes = nil
		r := NewReconciler(&cfg)
		err := r.RemoveManagedRoutes()
		assert.NoError(t, err)
	})
}

func assertExpr(t *testing.T, expr interface{}, op, name, scope, value string) {
	if expr == nil {
		assert.Error(t, errors.New("expr is nil"))
	}
	typedExpr, ok := expr.(map[string]interface{})
	assert.Equal(t, true, ok)

	opAct, ok, err := unstructured.NestedString(typedExpr, "op")
	assert.NoError(t, err)
	assert.Equal(t, true, ok)
	assert.Equal(t, op, opAct)

	nameAct, ok, err := unstructured.NestedString(typedExpr, "subject", "name")
	assert.NoError(t, err)
	assert.Equal(t, true, ok)
	assert.Equal(t, name, nameAct)

	scopeAct, ok, err := unstructured.NestedString(typedExpr, "subject", "scope")
	assert.NoError(t, err)
	assert.Equal(t, true, ok)
	assert.Equal(t, scope, scopeAct)

	valueAct, ok, err := unstructured.NestedString(typedExpr, "value")
	assert.NoError(t, err)
	assert.Equal(t, true, ok)
	assert.Equal(t, value, valueAct)
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

func TestRemoveManagedRoutes(t *testing.T) {
	mocks.SetHeaderApisixRouteObj = toUnstructured(t, apisixSetHeaderRoute)
	mocks.ApisixRouteObj = toUnstructured(t, apisixRoute)
	mocks.DuplicateSetHeaderApisixRouteObj = toUnstructured(t, apisixSetHeaderDuplicateRoute)
	t.Run("RemoveManagedRoutes", func(t *testing.T) {
		client := &mocks.FakeClient{}
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client:  client,
		}
		r := NewReconciler(&cfg)
		err := r.RemoveManagedRoutes()
		// Then
		assert.NoError(t, err)
		assert.Equal(t, SetHeaderRouteName, client.DeleteName)
	})
	t.Run("RemoveManagedRoutesError", func(t *testing.T) {
		client := &mocks.FakeClient{
			IsGetManagedRouteError: true,
		}
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client:  client,
		}
		r := NewReconciler(&cfg)
		err := r.RemoveManagedRoutes()
		// Then
		assert.Error(t, err)
		assert.Equal(t, "", client.DeleteName)
	})
	t.Run("RemoveManagedRoutesNotFound", func(t *testing.T) {
		client := &mocks.FakeClient{
			IsGetNotFoundError: true,
		}
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, apisixRouteName),
			Client:  client,
		}
		r := NewReconciler(&cfg)
		err := r.RemoveManagedRoutes()
		// Then
		assert.NoError(t, err)
		assert.Equal(t, "", client.DeleteName)
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
			UID:       "1a2b2d82-50a4-4d83-9ff4-cdc6f5197d30",
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
						ManagedRoutes: []v1alpha1.MangedRoutes{
							{
								Name: SetHeaderRouteName,
							},
						},
					},
				},
			},
		},
	}
}
