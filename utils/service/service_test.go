package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestGetRolloutSelectorLabel(t *testing.T) {
	selector := GetRolloutSelectorLabel(nil)
	assert.Empty(t, selector)

	svc := &corev1.Service{}
	selector = GetRolloutSelectorLabel(svc)
	assert.Empty(t, selector)

	testSelectorValue := "abcdef"
	svc = &corev1.Service{
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				v1alpha1.DefaultRolloutUniqueLabelKey: testSelectorValue,
			},
		},
	}
	selector = GetRolloutSelectorLabel(svc)
	assert.Equal(t, selector, testSelectorValue)
}

func TestGetRolloutServiceKeysForNilRollout(t *testing.T) {
	keys := GetRolloutServiceKeys(nil)
	assert.Nil(t, keys)
}

func TestGetRolloutServiceKeysForCanary(t *testing.T) {
	keys := GetRolloutServiceKeys(&v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{},
			},
		},
	})
	assert.Empty(t, keys)
}

func TestGetRolloutServiceKeysForCanaryWithCanaryService(t *testing.T) {
	keys := GetRolloutServiceKeys(&v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					CanaryService: "canary-service",
					StableService: "stable-service",
				},
			},
		},
	})
	assert.Equal(t, keys, []string{"default/canary-service", "default/stable-service"})
}

func TestGetRolloutServiceKeysForPingPongCanaryService(t *testing.T) {
	keys := GetRolloutServiceKeys(&v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					PingPong: &v1alpha1.PingPongSpec{
						PingService: "ping-service",
						PongService: "pong-service",
					},
				},
			},
		},
	})
	assert.Equal(t, keys, []string{"default/ping-service", "default/pong-service"})
}

func TestGetRolloutServiceKeysForBlueGreen(t *testing.T) {
	keys := GetRolloutServiceKeys(&v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{
					PreviewService: "preview-service",
					ActiveService:  "active-service",
				},
			},
		},
	})
	assert.Equal(t, keys, []string{"default/active-service", "default/preview-service"})
}

func TestHasManagedByAnnotation(t *testing.T) {
	service := &corev1.Service{}
	managedBy, exists := HasManagedByAnnotation(service)
	assert.False(t, exists)
	assert.Equal(t, "", managedBy)

	service.Annotations = map[string]string{
		v1alpha1.ManagedByRolloutsKey: "test",
	}
	managedBy, exists = HasManagedByAnnotation(service)
	assert.True(t, exists)
	assert.Equal(t, "test", managedBy)

}

func TestCheckRolloutForService(t *testing.T) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{
					PreviewService: "preview-service",
					ActiveService:  "active-service",
				},
			},
		},
	}

	t.Run("Rollout does not reference service", func(t *testing.T) {
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: metav1.NamespaceDefault,
				Name:      "no-existing-service",
			},
		}
		assert.False(t, CheckRolloutForService(ro, service))
	})
	t.Run("Rollout references Service", func(t *testing.T) {
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: metav1.NamespaceDefault,
				Name:      "active-service",
			},
		}
		assert.True(t, CheckRolloutForService(ro, service))
	})
}
