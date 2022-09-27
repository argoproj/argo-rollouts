package traefik

import (
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/traefik/mocks"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
)

const traefikService = `
apiVersion: mocks.containo.us/v1alpha1
kind: TraefikService
metadata:
  labels:
    service: argo-mocks
  name: mocks-service
spec:
  weighted:
    services:
      - name: stable-rollout
        weight: 100
        port: 80
      - name: canary-rollout
        weight: 0
        port: 80
`

const errorTraefikService = `
apiVersion: mocks.containo.us/v1alpha1
kind: TraefikService
metadata:
  labels:
    service: argo-mocks
  name: mocks-service
`

var (
	client *mocks.FakeClient = &mocks.FakeClient{}
)

const (
	stableServiceName     string = "stable-rollout"
	fakeStableServiceName string = "fake-stable-rollout"
	canaryServiceName     string = "canary-rollout"
	fakeCanaryServiceName string = "fake-canary-rollout"
	traefikServiceName    string = "mocks-service"
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
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
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
	mocks.TraefikServiceObj = toUnstructured(t, traefikService)
	mocks.ErrorTraefikServiceObj = toUnstructured(t, errorTraefikService)
	t.Run("SetWeight", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetWeight(30)

		// Then
		assert.NoError(t, err)
		services, isFound, err := unstructured.NestedSlice(mocks.TraefikServiceObj.Object, "spec", "weighted", "services")
		assert.NoError(t, err)
		assert.Equal(t, isFound, true)
		stableService, err := getService(stableServiceName, services)
		assert.NoError(t, err)
		stableServiceWeight, isFound, err := unstructured.NestedInt64(stableService, "weight")
		assert.NoError(t, err)
		assert.Equal(t, isFound, true)
		canaryService, err := getService(canaryServiceName, services)
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
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
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
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
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
			Rollout: newRollout(fakeStableServiceName, canaryServiceName, traefikServiceName),
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
			Rollout: newRollout(stableServiceName, fakeCanaryServiceName, traefikServiceName),
			Client:  client,
		}
		r := NewReconciler(&cfg)

		// When
		err := r.SetWeight(30)

		// Then
		assert.Error(t, err)
	})
	t.Run("TraefikUpdateError", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
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

func TestSetHeaderRoute(t *testing.T) {
	t.Run("SetHeaderRoute", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
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
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
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
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
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
	mocks.TraefikServiceObj = toUnstructured(t, traefikService)
	t.Run("Type", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
			Client:  client,
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
		services := []interface{}{
			mocks.FakeService{Weight: 12},
		}

		// When
		selectedServices, err := getService("default", services)

		// Then
		assert.Nil(t, selectedServices)
		assert.Error(t, err)
	})
	t.Run("ErrorGetServiceFromMap", func(t *testing.T) {
		// Given
		t.Parallel()
		services := map[string]interface{}{
			"weight": 100,
		}

		// When
		selectedServices, err := getService("default", []interface{}{services})

		// Then
		assert.Nil(t, selectedServices)
		assert.Error(t, err)
	})
	t.Run("GetServiceFromMap", func(t *testing.T) {
		// Given
		t.Parallel()
		const serviceName string = "default"
		services := map[string]interface{}{
			"name": serviceName,
		}

		// When
		selectedServices, err := getService(serviceName, []interface{}{services})

		// Then
		assert.NotNil(t, selectedServices)
		assert.NoError(t, err)
	})
	t.Run("ErrorGetServiceFromNil", func(t *testing.T) {
		// Given
		t.Parallel()
		services := map[string]interface{}{
			"name": nil,
		}

		// When
		selectedServices, err := getService("default", []interface{}{services})

		// Then
		assert.Nil(t, selectedServices)
		assert.Error(t, err)
	})
}

func newRollout(stableSvc, canarySvc, traefikServiceName string) *v1alpha1.Rollout {
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
						Traefik: &v1alpha1.TraefikTrafficRouting{
							WeightedTraefikServiceName: traefikServiceName,
						},
					},
				},
			},
		},
	}
}
