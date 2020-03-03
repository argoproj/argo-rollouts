package ingress

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestGetRolloutIngressKeysForCanary(t *testing.T) {
	keys := GetRolloutIngressKeys(&v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{},
			},
		},
	})
	assert.Empty(t, keys)
}

func TestGetRolloutIngressKeysForCanaryWithTrafficRouting(t *testing.T) {
	keys := GetRolloutIngressKeys(&v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					CanaryService: "canary-service",
					StableService: "stable-service",
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Nginx: &v1alpha1.NginxTrafficRouting{
							StableIngress: "stable-ingress",
						},
					},
				},
			},
		},
	})
	assert.ElementsMatch(t, keys, []string{"default/stable-ingress", "default/stable-ingress-canary"})
}
