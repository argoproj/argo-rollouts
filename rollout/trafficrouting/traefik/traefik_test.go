package traefik

import (
	"context"
	"testing"

	"github.com/pkg/errors"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
)

const traefikService = `
apiVersion: traefik.containo.us/v1alpha1
kind: TraefikService
metadata:
  labels:
    service: argo-traefik
  name: traefik-service
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
apiVersion: traefik.containo.us/v1alpha1
kind: TraefikService
metadata:
  labels:
    service: argo-traefik
  name: traefik-service
`

type fakeClient struct {
	isGetError         bool
	isGetErrorManifest bool
}

var (
	client                 *fakeClient = &fakeClient{}
	traefikServiceObj      *unstructured.Unstructured
	errorTraefikServiceObj *unstructured.Unstructured
)

const (
	stableServiceName  string = "stable-rollout"
	canaryServiceName  string = "canary-rollout"
	traefikServiceName string = "traefik-service"
)

func (f *fakeClient) Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	if f.isGetError == true {
		return traefikServiceObj, errors.New("Traefik get error")
	}
	if f.isGetErrorManifest == true {
		return errorTraefikServiceObj, nil
	}
	return traefikServiceObj, nil
}

func (f *fakeClient) Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return obj, nil
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
	traefikServiceObj = toUnstructured(t, traefikService)
	errorTraefikServiceObj = toUnstructured(t, errorTraefikService)
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
		services, isFound, err := unstructured.NestedSlice(traefikServiceObj.Object, "spec", "weighted", "services")
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
			Client: &fakeClient{
				isGetError: true,
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
			Client: &fakeClient{
				isGetErrorManifest: true,
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
	traefikServiceObj = toUnstructured(t, traefikService)
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
	traefikServiceObj = toUnstructured(t, traefikService)
	t.Run("Type", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, "traefik-service"),
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
