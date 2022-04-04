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
	stableServiceName  string = "stable-rollout"
	canaryServiceName  string = "canary-rollout"
	traefikServiceName string = "mocks-service"
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
		r := NewReconciler(cfg)

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
		r := NewReconciler(cfg)

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
		r := NewReconciler(cfg)

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
		r := NewReconciler(cfg)

		// When
		err := r.SetWeight(30)

		// Then
		assert.NoError(t, err)
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
	mocks.TraefikServiceObj = toUnstructured(t, traefikService)
	t.Run("VerifyWeight", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
			Client:  client,
		}
		r := NewReconciler(cfg)

		// When
		isSynced, err := r.VerifyWeight(0)

		// Then
		assert.NoError(t, err)
		assert.Equal(t, true, *isSynced)
	})
}

func TestType(t *testing.T) {
	mocks.TraefikServiceObj = toUnstructured(t, traefikService)
	t.Run("Type", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, "mocks-service"),
			Client:  client,
		}
		r := NewReconciler(cfg)

		// When
		reconcilerType := r.Type()

		// Then
		assert.Equal(t, Type, reconcilerType)
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
							Service: traefikServiceName,
						},
					},
				},
			},
		},
	}
}
