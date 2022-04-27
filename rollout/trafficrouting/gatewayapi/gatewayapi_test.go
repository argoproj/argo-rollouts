package gatewayapi

import (
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/gatewayapi/mocks"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	stableServiceName string = "stable-rollout"
	canaryServiceName string = "canary-rollout"
	httpRouteName     string = "argo-rollouts-http-route"
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

func TestSetWeight(t *testing.T) {}

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

func TestGetService(t *testing.T) {}

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
