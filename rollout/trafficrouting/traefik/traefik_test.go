package traefik

import (
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/traefik/mocks"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	stableServiceName     string = "stable-rollout"
	fakeStableServiceName string = "fake-stable-rollout"
	canaryServiceName     string = "canary-rollout"
	fakeCanaryServiceName string = "fake-canary-rollout"
	traefikServiceName    string = "mocks-service"
)

const (
	mirrorTraefikService = `
apiVersion: mocks.containo.us/v1alpha1
kind: TraefikService
metadata:
  labels:
    service: argo-mocks
  name: argo-mirror-mocks-service
spec:
  mirroring:
    name: mocks-service
    kind: TraefikService
    mirrors:
      - name: canary-rollout
        kind: TraefikService
      - name: test-rollout
        kind: TraefikService
`

	errorMirrorTraefikService = `
apiVersion: mocks.containo.us/v1alpha1
kind: TraefikService
metadata:
  labels:
    service: argo-mocks
  name: argo-mirror-mocks-service
spec:
  mirroring:
    name: mocks-service
    kind: TraefikService
    mirrors:
      name: canary-rollout
      kind: TraefikService
`

	mirrorTraefikServiceWithNotFoundField = `
apiVersion: mocks.containo.us/v1alpha1
kind: TraefikService
metadata:
  labels:
    service: argo-mocks
  name: argo-mirror-mocks-service
spec:
  mirroring:
    name: mocks-service
    kind: TraefikService
`

	traefikService = `
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

	errorTraefikService = `
apiVersion: mocks.containo.us/v1alpha1
kind: TraefikService
metadata:
  labels:
    service: argo-mocks
  name: mocks-service
`
)

var (
	clientSet = ClientSet{
		TraefikServiceClient: &mocks.FakeTraefikServiceClient{},
		IngressRouteClient:   &mocks.FakeIngressRouteClient{},
		ServiceClient:        &mocks.FakeServiceClient{},
	}

	managedRouteList []v1alpha1.ManagedRoutes = []v1alpha1.ManagedRoutes{
		{
			Name: "test-rollout",
		},
	}
	mirrorList []interface{} = []interface{}{
		map[string]interface{}{
			"name": canaryServiceName,
			"kind": "TraefikService",
		},
	}
	mirrorListWithFailedMirrorTypeAssertion []interface{} = []interface{}{
		"error-mirror",
	}
	mirrorListWithFailedMirrorNameTypeAssertion []interface{} = []interface{}{
		map[string]interface{}{
			"name": 12,
			"kind": "TraefikService",
		},
	}
	mirrorListWithFailedMirrorKindTypeAssertion []interface{} = []interface{}{
		map[string]interface{}{
			"name": canaryServiceName,
			"kind": 12,
		},
	}
)

func TestNewDynamicClient(t *testing.T) {
	t.Run("NewDynamicClient", func(t *testing.T) {
		// Given
		t.Parallel()
		fakeDynamicClient := &mocks.FakeDynamicClient{}

		// When
		NewDynamicClient(fakeDynamicClient, "test", "default")
	})
}

func TestUpdateHash(t *testing.T) {
	t.Run("UpdateHash", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout:   newRollout(stableServiceName, canaryServiceName, traefikServiceName),
			ClientSet: clientSet,
		}
		r := NewReconciler(&cfg)

		// When
		err := r.UpdateHash("", "")

		// Then
		assert.NoError(t, err)
	})
}

func TestSetWeight(t *testing.T) {
	var err error
	mocks.TraefikServiceObj, err = toUnstructured(traefikService)
	assert.NoError(t, err)
	mocks.ErrorTraefikServiceObj, err = toUnstructured(errorTraefikService)
	assert.NoError(t, err)
	t.Run("SetWeight", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout:   newRollout(stableServiceName, canaryServiceName, traefikServiceName),
			ClientSet: clientSet,
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
			ClientSet: ClientSet{
				TraefikServiceClient: &mocks.FakeTraefikServiceClient{
					IsClientGetTraefikServiceError: true,
				},
				IngressRouteClient: &mocks.FakeIngressRouteClient{},
				ServiceClient:      &mocks.FakeServiceClient{},
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
			ClientSet: ClientSet{
				TraefikServiceClient: &mocks.FakeTraefikServiceClient{
					IsGetErrorTraefikServiceManifest: true,
				},
				IngressRouteClient: &mocks.FakeIngressRouteClient{},
				ServiceClient:      &mocks.FakeServiceClient{},
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
			Rollout:   newRollout(fakeStableServiceName, canaryServiceName, traefikServiceName),
			ClientSet: clientSet,
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
			Rollout:   newRollout(stableServiceName, fakeCanaryServiceName, traefikServiceName),
			ClientSet: clientSet,
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
			ClientSet: ClientSet{
				TraefikServiceClient: &mocks.FakeTraefikServiceClient{
					IsClientUpdateError: true,
				},
				IngressRouteClient: &mocks.FakeIngressRouteClient{},
				ServiceClient:      &mocks.FakeServiceClient{},
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
			Rollout:   newRollout(stableServiceName, canaryServiceName, traefikServiceName),
			ClientSet: clientSet,
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
	})
}

func TestSetMirrorRoute(t *testing.T) {
	var err error
	mocks.MirrorTraefikServiceObj, err = toUnstructured(mirrorTraefikService)
	assert.NoError(t, err)
	mocks.ErrorMirrorTraefikServiceObj, err = toUnstructured(errorMirrorTraefikService)
	assert.NoError(t, err)
	mocks.MirrorTraefikServiceWithNotFoundField, err = toUnstructured(mirrorTraefikServiceWithNotFoundField)
	assert.NoError(t, err)
	t.Run("SetMirrorRoute", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
			ClientSet: ClientSet{
				TraefikServiceClient: &mocks.FakeTraefikServiceClient{
					IsGetMirrorTraefikService: true,
				},
				IngressRouteClient: &mocks.FakeIngressRouteClient{},
				ServiceClient:      &mocks.FakeServiceClient{},
			},
		}
		r := NewReconciler(&cfg)

		// When
		var percentage int32 = 50
		err := r.SetMirrorRoute(&v1alpha1.SetMirrorRoute{
			Name:       canaryServiceName,
			Percentage: &percentage,
		})

		// Then
		assert.NoError(t, err)
	})
	t.Run("SetMirrorRouteWithClientGetError", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
			ClientSet: ClientSet{
				TraefikServiceClient: &mocks.FakeTraefikServiceClient{
					IsClientGetErrorMirrorTraefikService: true,
				},
				IngressRouteClient: &mocks.FakeIngressRouteClient{},
				ServiceClient:      &mocks.FakeServiceClient{},
			},
		}
		r := NewReconciler(&cfg)

		// When
		var percentage int32 = 50
		err := r.SetMirrorRoute(&v1alpha1.SetMirrorRoute{
			Name:       canaryServiceName,
			Percentage: &percentage,
		})

		// Then
		assert.Error(t, err)
	})
	t.Run("SetMirrorRouteWithManifestError", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
			ClientSet: ClientSet{
				TraefikServiceClient: &mocks.FakeTraefikServiceClient{
					IsGetErrorMirrorTraefikServiceManifest: true,
				},
				IngressRouteClient: &mocks.FakeIngressRouteClient{},
				ServiceClient:      &mocks.FakeServiceClient{},
			},
		}
		r := NewReconciler(&cfg)

		// When
		var percentage int32 = 50
		err := r.SetMirrorRoute(&v1alpha1.SetMirrorRoute{
			Name:       canaryServiceName,
			Percentage: &percentage,
		})

		// Then
		assert.Error(t, err)
	})
	t.Run("SetMirrorRouteWithNotFoundField", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
			ClientSet: ClientSet{
				TraefikServiceClient: &mocks.FakeTraefikServiceClient{
					IsGetMirrorTraefikServiceWithNotFoundField: true,
				},
				IngressRouteClient: &mocks.FakeIngressRouteClient{},
				ServiceClient:      &mocks.FakeServiceClient{},
			},
		}
		r := NewReconciler(&cfg)

		// When
		var percentage int32 = 50
		err := r.SetMirrorRoute(&v1alpha1.SetMirrorRoute{
			Name:       canaryServiceName,
			Percentage: &percentage,
		})

		// Then
		assert.NoError(t, err)
	})
	t.Run("SetMirrorRouteUpdateError", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
			ClientSet: ClientSet{
				TraefikServiceClient: &mocks.FakeTraefikServiceClient{
					IsClientUpdateError:       true,
					IsGetMirrorTraefikService: true,
				},
				IngressRouteClient: &mocks.FakeIngressRouteClient{},
				ServiceClient:      &mocks.FakeServiceClient{},
			},
			Recorder: &mocks.FakeRecorder{},
		}
		r := NewReconciler(&cfg)

		// When
		var percentage int32 = 50
		err := r.SetMirrorRoute(&v1alpha1.SetMirrorRoute{
			Name:       canaryServiceName,
			Percentage: &percentage,
		})

		// Then
		assert.Error(t, err)
	})
}

func TestRemoveManagedRoutes(t *testing.T) {
	var err error
	mocks.MirrorTraefikServiceObj, err = toUnstructured(mirrorTraefikService)
	assert.NoError(t, err)
	mocks.ErrorMirrorTraefikServiceObj, err = toUnstructured(errorMirrorTraefikService)
	assert.NoError(t, err)
	mocks.MirrorTraefikServiceWithNotFoundField, err = toUnstructured(mirrorTraefikServiceWithNotFoundField)
	assert.NoError(t, err)

	t.Run("RemoveManagedRoutes", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
			ClientSet: ClientSet{
				TraefikServiceClient: &mocks.FakeTraefikServiceClient{
					IsGetMirrorTraefikService: true,
				},
				IngressRouteClient: &mocks.FakeIngressRouteClient{},
				ServiceClient:      &mocks.FakeServiceClient{},
			},
		}
		r := NewReconciler(&cfg)

		// When
		err := r.RemoveManagedRoutes()

		// Then
		assert.NoError(t, err)
	})
	t.Run("RemoveManagedRoutesWithClientError", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
			ClientSet: ClientSet{
				TraefikServiceClient: &mocks.FakeTraefikServiceClient{
					IsClientGetErrorMirrorTraefikService: true,
				},
				IngressRouteClient: &mocks.FakeIngressRouteClient{},
				ServiceClient:      &mocks.FakeServiceClient{},
			},
		}
		r := NewReconciler(&cfg)

		// When
		err := r.RemoveManagedRoutes()

		// Then
		assert.Error(t, err)
	})
	t.Run("RemoveManagedRoutesWithManifestError", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
			ClientSet: ClientSet{
				TraefikServiceClient: &mocks.FakeTraefikServiceClient{
					IsGetErrorMirrorTraefikServiceManifest: true,
				},
				IngressRouteClient: &mocks.FakeIngressRouteClient{},
				ServiceClient:      &mocks.FakeServiceClient{},
			},
		}
		r := NewReconciler(&cfg)

		// When
		err := r.RemoveManagedRoutes()

		// Then
		assert.Error(t, err)
	})
	t.Run("RemoveManagedRoutesWithNotFoundField", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
			ClientSet: ClientSet{
				TraefikServiceClient: &mocks.FakeTraefikServiceClient{
					IsGetMirrorTraefikServiceWithNotFoundField: true,
				},
				IngressRouteClient: &mocks.FakeIngressRouteClient{},
				ServiceClient:      &mocks.FakeServiceClient{},
			},
		}
		r := NewReconciler(&cfg)

		// When
		err := r.RemoveManagedRoutes()

		// Then
		assert.Error(t, err)
	})
	t.Run("SetMirrorRouteUpdateError", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout: newRollout(stableServiceName, canaryServiceName, traefikServiceName),
			ClientSet: ClientSet{
				TraefikServiceClient: &mocks.FakeTraefikServiceClient{
					IsClientUpdateError:       true,
					IsGetMirrorTraefikService: true,
				},
				IngressRouteClient: &mocks.FakeIngressRouteClient{},
				ServiceClient:      &mocks.FakeServiceClient{},
			},
			Recorder: &mocks.FakeRecorder{},
		}
		r := NewReconciler(&cfg)

		// When
		err := r.RemoveManagedRoutes()

		// Then
		assert.Error(t, err)
	})
}

func TestVerifyWeight(t *testing.T) {
	t.Run("VerifyWeight", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout:   newRollout(stableServiceName, canaryServiceName, traefikServiceName),
			ClientSet: clientSet,
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
	var err error
	mocks.TraefikServiceObj, err = toUnstructured(traefikService)
	assert.NoError(t, err)
	t.Run("Type", func(t *testing.T) {
		// Given
		t.Parallel()
		cfg := ReconcilerConfig{
			Rollout:   newRollout(stableServiceName, canaryServiceName, traefikServiceName),
			ClientSet: clientSet,
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
						ManagedRoutes: []v1alpha1.ManagedRoutes{
							{
								Name: canaryServiceName,
							},
						},
						Traefik: &v1alpha1.TraefikTrafficRouting{
							WeightedTraefikServiceName: traefikServiceName,
						},
					},
				},
			},
		},
	}
}
