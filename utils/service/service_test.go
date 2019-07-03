package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestGetRolloutSelectorLabel(t *testing.T) {
	selector, ok := GetRolloutSelectorLabel(nil)
	assert.Empty(t, selector)
	assert.False(t, ok)

	svc := &corev1.Service{}
	selector, ok = GetRolloutSelectorLabel(svc)
	assert.Empty(t, selector)
	assert.False(t, ok)

	testSelectorValue := "abcdef"
	svc = &corev1.Service{
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				v1alpha1.DefaultRolloutUniqueLabelKey: testSelectorValue,
			},
		},
	}
	selector, ok = GetRolloutSelectorLabel(svc)
	assert.Equal(t, selector, testSelectorValue)
	assert.True(t, ok)

}

func TestGetRolloutServiceKeysForCanary(t *testing.T) {
	keys := GetRolloutServiceKeys(&v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				CanaryStrategy: &v1alpha1.CanaryStrategy{},
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
				CanaryStrategy: &v1alpha1.CanaryStrategy{
					CanaryService: "canary-service",
				},
			},
		},
	})
	assert.ElementsMatch(t, keys, []string{"default/canary-service"})
}

func TestGetRolloutServiceKeysForBlueGreen(t *testing.T) {
	keys := GetRolloutServiceKeys(&v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreenStrategy: &v1alpha1.BlueGreenStrategy{
					PreviewService: "preview-service",
					ActiveService:  "active-service",
				},
			},
		},
	})
	assert.ElementsMatch(t, keys, []string{"default/preview-service", "default/active-service"})
}
