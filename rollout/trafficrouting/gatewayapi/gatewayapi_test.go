package gatewayapi

import (
	"encoding/json"
	"log"
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
		rollout := newRollout(stableServiceName, canaryServiceName, httpRouteName)
		cfg := ReconcilerConfig{
			Rollout: rollout,
		}
		r := NewReconciler(&cfg)

		// When
		err := r.UpdateHash(rollout, "", "", []v1alpha1.WeightDestination{})

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
		rollout := newRollout(stableServiceName, canaryServiceName, httpRouteName)
		cfg := ReconcilerConfig{
			Rollout: rollout,
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		rpcErr := r.SetWeight(rollout, 30, []v1alpha1.WeightDestination{})

		// Then
		assert.Empty(t, rpcErr.Error())
		rules, isFound, err := unstructured.NestedSlice(mocks.HTTPRouteObj.Object, "spec", "rules")
		assert.NoError(t, err)
		assert.Equal(t, isFound, true)
		backendRefs, err := getBackendRefList(rules)
		assert.NoError(t, err)
		stableService, err := getBackendRef(stableServiceName, backendRefs)
		assert.NoError(t, err)
		stableServiceWeight, isFound, err := unstructured.NestedInt64(stableService, "weight")
		assert.NoError(t, err)
		assert.Equal(t, isFound, true)
		canaryService, err := getBackendRef(canaryServiceName, backendRefs)
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
		rollout := newRollout(stableServiceName, canaryServiceName, httpRouteName)
		cfg := ReconcilerConfig{
			Rollout: rollout,
			Client: &mocks.FakeClient{
				IsGetError: true,
			},
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetWeight(rollout, 30, []v1alpha1.WeightDestination{})

		// Then
		assert.Error(t, err)
	})
	t.Run("SetWeightWithErrorManifest", func(t *testing.T) {
		// Given
		t.Parallel()
		rollout := newRollout(stableServiceName, canaryServiceName, httpRouteName)
		cfg := ReconcilerConfig{
			Rollout: rollout,
			Client: &mocks.FakeClient{
				IsGetErrorManifest: true,
			},
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetWeight(rollout, 30, []v1alpha1.WeightDestination{})

		// Then
		assert.Error(t, err)
	})
	t.Run("SetWeightWithErrorStableName", func(t *testing.T) {
		// Given
		t.Parallel()
		rollout := newRollout(stableServiceName, canaryServiceName, httpRouteName)
		cfg := ReconcilerConfig{
			Rollout: rollout,
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetWeight(rollout, 30, []v1alpha1.WeightDestination{})

		// Then
		assert.Error(t, err)
	})
	t.Run("SetWeightWithErrorCanaryName", func(t *testing.T) {
		// Given
		t.Parallel()
		rollout := newRollout(stableServiceName, canaryServiceName, httpRouteName)
		cfg := ReconcilerConfig{
			Rollout: rollout,
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetWeight(rollout, 30, []v1alpha1.WeightDestination{})

		// Then
		assert.Error(t, err)
	})
	t.Run("GatewayAPIUpdateError", func(t *testing.T) {
		// Given
		t.Parallel()
		rollout := newRollout(stableServiceName, canaryServiceName, httpRouteName)
		cfg := ReconcilerConfig{
			Rollout: rollout,
			Client: &mocks.FakeClient{
				UpdateError: true,
			},
			Recorder: &mocks.FakeRecorder{},
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetWeight(rollout, 30, []v1alpha1.WeightDestination{})

		// Then
		assert.Error(t, err)
	})
}

func TestSetHeaderRouting(t *testing.T) {
	t.Run("SetHeaderRouting", func(t *testing.T) {
		// Given
		t.Parallel()
		rollout := newRollout(stableServiceName, canaryServiceName, httpRouteName)
		cfg := ReconcilerConfig{}
		r := NewReconciler(&cfg)
		headerRouting := &v1alpha1.SetHeaderRoute{}

		// When
		err := r.SetHeaderRoute(rollout, headerRouting)

		// Then
		assert.NoError(t, err)
	})
}

func TestVerifyWeight(t *testing.T) {
	t.Run("VerifyWeight", func(t *testing.T) {
		// Given
		t.Parallel()
		rollout := newRollout(stableServiceName, canaryServiceName, httpRouteName)
		cfg := ReconcilerConfig{
			Rollout: rollout,
		}
		r := NewReconciler(&cfg)

		// When
		isSynced, err := r.VerifyWeight(rollout, 32, []v1alpha1.WeightDestination{})

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

func TestGetBackendRef(t *testing.T) {
	t.Run("ErrorGetBackendRefFromStruct ", func(t *testing.T) {
		// Given
		t.Parallel()
		backendRefs := []interface{}{
			mocks.FakeBackendRef{Weight: 12},
		}

		// When
		selectedBackendRef, err := getBackendRef("default", backendRefs)

		// Then
		assert.Nil(t, selectedBackendRef)
		assert.Error(t, err)
	})
	t.Run("ErrorGetBackendRefFromMap", func(t *testing.T) {
		// Given
		t.Parallel()
		backendRefs := map[string]interface{}{
			"weight": 100,
		}

		// When
		selectedBackendRef, err := getBackendRef("default", []interface{}{backendRefs})

		// Then
		assert.Nil(t, selectedBackendRef)
		assert.Error(t, err)
	})
	t.Run("GetBackendRefFromMap", func(t *testing.T) {
		// Given
		t.Parallel()
		const serviceName string = "default"
		backendRefs := map[string]interface{}{
			"name": serviceName,
		}

		// When
		selectedBackendRef, err := getBackendRef(serviceName, []interface{}{backendRefs})

		// Then
		assert.NotNil(t, selectedBackendRef)
		assert.NoError(t, err)
	})
	t.Run("ErrorGetBackendRefFromNil", func(t *testing.T) {
		// Given
		t.Parallel()
		backendRefs := map[string]interface{}{
			"name": nil,
		}

		// When
		selectedBackendRef, err := getBackendRef("default", []interface{}{backendRefs})

		// Then
		assert.Nil(t, selectedBackendRef)
		assert.Error(t, err)
	})
}

func TestGetBackendRefList(t *testing.T) {
	t.Run("ErrorGetBackendRefListFromStruct ", func(t *testing.T) {
		// Given
		t.Parallel()
		rules := []interface{}{
			mocks.FakeBackendRefList{},
		}

		// When
		backendRefs, err := getBackendRefList(rules)

		// Then
		assert.Nil(t, backendRefs)
		assert.Error(t, err)
	})
	t.Run("ErrorGetBackendRefListFromMap", func(t *testing.T) {
		// Given
		t.Parallel()
		rules := map[string]interface{}{
			"mock": nil,
		}

		// When
		backendRefs, err := getBackendRefList([]interface{}{rules})

		// Then
		assert.Nil(t, backendRefs)
		assert.Error(t, err)
	})
	t.Run("GetBackendRefListFromMap", func(t *testing.T) {
		// Given
		t.Parallel()
		rules := map[string]interface{}{
			"backendRefs": []interface{}{"mock value"},
		}

		// When
		backendRefs, err := getBackendRefList([]interface{}{rules})

		// Then
		assert.NotNil(t, backendRefs)
		assert.NoError(t, err)
	})
	t.Run("ErrorGetBackendRefListFromNil", func(t *testing.T) {
		// Given
		t.Parallel()
		rules := map[string]interface{}{
			"backendRefs": nil,
		}

		// When
		backendRefs, err := getBackendRefList([]interface{}{rules})

		// Then
		assert.Nil(t, backendRefs)
		assert.Error(t, err)
	})
}

func TestMergeBackendRefs(t *testing.T) {
	t.Run("ErrorMergeBackendRefsStruct ", func(t *testing.T) {
		// Given
		t.Parallel()
		rules := []interface{}{
			mocks.FakeBackendRefList{},
		}
		backendRefs := []interface{}{}

		// When
		updatedRules, err := mergeBackendRefs(rules, backendRefs)

		// Then
		assert.Nil(t, updatedRules)
		assert.Error(t, err)
	})
	t.Run("ErrorMergeBackendRefsMap", func(t *testing.T) {
		// Given
		t.Parallel()
		rules := map[string]interface{}{
			"mock": nil,
		}
		backendRefs := []interface{}{}

		// When
		updatedRules, err := mergeBackendRefs([]interface{}{rules}, backendRefs)

		// Then
		typedRule, ok := updatedRules[0].(map[string]interface{})
		assert.True(t, ok)
		assert.Nil(t, typedRule["backendRef"])
		assert.Error(t, err)
	})
	t.Run("GetMergeBackendRefsMap", func(t *testing.T) {
		// Given
		t.Parallel()
		rules := map[string]interface{}{
			"backendRefs": []interface{}{
				map[string]interface{}{
					"name": "test",
				},
			},
		}
		backendRefs := []interface{}{
			map[string]interface{}{
				"name":   "test",
				"weight": "100",
			},
		}

		// When
		updatedRules, err := mergeBackendRefs([]interface{}{rules}, backendRefs)

		// Then
		assert.NoError(t, err)
		typedRule, ok := updatedRules[0].(map[string]interface{})
		assert.True(t, ok)
		updatedBackendRefs, isFound, err := unstructured.NestedSlice(typedRule, "backendRefs")
		assert.True(t, isFound)
		assert.NoError(t, err)
		typedBackendRefs, ok := updatedBackendRefs[0].(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, "100", typedBackendRefs["weight"])
		assert.NoError(t, err)
	})
	t.Run("ErrorMergeBackendRefsNil", func(t *testing.T) {
		// Given
		t.Parallel()
		rules := map[string]interface{}{
			"backendRefs": 1,
		}
		backendRefs := []interface{}{
			map[string]interface{}{
				"name":   "test",
				"weight": "100",
			},
		}

		// When
		updatedRules, err := mergeBackendRefs([]interface{}{rules}, backendRefs)

		// Then
		assert.Nil(t, updatedRules)
		assert.Error(t, err)
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

func newRollout(stableSvc, canarySvc, httpRouteName string) *v1alpha1.Rollout {
	gatewayAPIConfig := GatewayAPITrafficRouting{
		HTTPRoute: httpRouteName,
	}
	encodedGatewayAPIConfig, err := json.Marshal(gatewayAPIConfig)
	if err != nil {
		log.Fatal(err)
	}
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
						Plugin: map[string]json.RawMessage{
							"gatewayAPI": encodedGatewayAPIConfig,
						},
					},
				},
			},
		},
	}
}
