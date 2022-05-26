package gatewayapi

import (
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/gatewayapi/mocks"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
)

var (
	client *mocks.FakeClient = &mocks.FakeClient{}
)

const httpRoute = `
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: HTTPRoute
metadata:
  name: argo-rollouts-http-route
spec:
  parentRefs:
  - name: argo-rollouts-gateway
  rules:
  - backendRefs:
    - name: argo-rollouts-stable-service
      port: 80
      weight: 0
    - name: argo-rollouts-canary-service
      port: 80
      weight: 0

`

const errorHTTPRoute = `
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: HTTPRoute
metadata:
  name: argo-rollouts-http-route
`

const (
	stableServiceName     = "argo-rollouts-stable-service"
	fakeStableServiceName = "fake-argo-rollouts-stable-service"
	canaryServiceName     = "argo-rollouts-canary-service"
	fakeCanaryServiceName = "fake-argo-rollouts-canary-service"
	httpRouteName         = "argo-rollouts-http-route"
)

func TestNewDynamicClient(t *testing.T) {
	t.Run("NewDynamicClient", func(t *testing.T) {
		// Given
		t.Parallel()
		fakeDynamicClient := &mocks.FakeDynamicClient{}

		// When
		NewDynamicClient(fakeDynamicClient, "default")
	})
}

func TestUpdateHash(t *testing.T) {
	t.Run("UpdateHash", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, httpRouteName),
		}
		r := NewReconciler(&cfg)

		// When
		err := r.UpdateHash("", "")

		// Then
		assert.NoError(t, err)
	})
}

func TestSetWeight(t *testing.T) {
	mocks.HTTPRouteObj = toUnstructured(t, httpRoute)
	mocks.ErrorHTTPRouteObj = toUnstructured(t, errorHTTPRoute)
	t.Run("SetWeight", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, httpRouteName),
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetWeight(30)

		// Then
		assert.NoError(t, err)
		rules, isFound, err := unstructured.NestedSlice(mocks.HTTPRouteObj.Object, "spec", "rules")
		assert.NoError(t, err)
		assert.Equal(t, isFound, true)
		backendRefs, err := getBackendRefs(rules)
		assert.NoError(t, err)
		stableService, err := getService(stableServiceName, backendRefs)
		assert.NoError(t, err)
		stableServiceWeight, isFound, err := unstructured.NestedInt64(stableService, "weight")
		assert.NoError(t, err)
		assert.Equal(t, isFound, true)
		canaryService, err := getService(canaryServiceName, backendRefs)
		assert.NoError(t, err)
		canaryServiceWeight, isFound, err := unstructured.NestedInt64(canaryService, "weight")
		assert.Equal(t, isFound, true)
		assert.NoError(t, err)
		assert.Equal(t, int64(70), stableServiceWeight)
		assert.Equal(t, int64(30), canaryServiceWeight)
	})
	t.Run("SetWeightWithError", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, httpRouteName),
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
			Rollout: newRollout(stableServiceName, canaryServiceName, httpRouteName),
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
			Rollout: newRollout(fakeStableServiceName, canaryServiceName, httpRouteName),
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
			Rollout: newRollout(stableServiceName, fakeCanaryServiceName, httpRouteName),
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetWeight(30)

		// Then
		assert.Error(t, err)
	})
	t.Run("GatewayAPIUpdateError", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, httpRouteName),
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

func TestVerifyWeight(t *testing.T) {
	t.Run("VerifyWeight", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, httpRouteName),
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
	t.Run("Type", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, httpRouteName),
		}
		r := NewReconciler(&cfg)

		// When
		reconcilerType := r.Type()

		// Then
		assert.Equal(t, Type, reconcilerType)
	})
}

func TestGetService(t *testing.T) {
	t.Run("ErrorGetServiceFromStruct ", func(t *testing.T) {
		// Given
		t.Parallel()
		backendRefs := []interface{}{
			mocks.FakeService{Weight: 12},
		}

		// When
		selectedServices, err := getService("default", backendRefs)

		// Then
		assert.Nil(t, selectedServices)
		assert.Error(t, err)
	})
	t.Run("ErrorGetServiceFromMap", func(t *testing.T) {
		// Given
		t.Parallel()
		backendRefs := map[string]interface{}{
			"weight": 100,
		}

		// When
		selectedServices, err := getService("default", []interface{}{backendRefs})

		// Then
		assert.Nil(t, selectedServices)
		assert.Error(t, err)
	})
	t.Run("GetServiceFromMap", func(t *testing.T) {
		// Given
		t.Parallel()
		const serviceName string = "default"
		backendRefs := map[string]interface{}{
			"name": serviceName,
		}

		// When
		selectedServices, err := getService(serviceName, []interface{}{backendRefs})

		// Then
		assert.NotNil(t, selectedServices)
		assert.NoError(t, err)
	})
	t.Run("ErrorGetServiceFromNil", func(t *testing.T) {
		// Given
		t.Parallel()
		backendRefs := map[string]interface{}{
			"name": nil,
		}

		// When
		selectedServices, err := getService("default", []interface{}{backendRefs})

		// Then
		assert.Nil(t, selectedServices)
		assert.Error(t, err)
	})
}

func TestGetBackendRefs(t *testing.T) {}

func TestMergeBackendRefs(t *testing.T) {}

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

func newRollout(stableSvc, canarySvc, httpRouteName string) *v1alpha1.Rollout {
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
						GatewayAPI: &v1alpha1.GatewayAPITrafficRouting{
							HTTPRoute: httpRouteName,
						},
					},
				},
			},
		},
	}
}
