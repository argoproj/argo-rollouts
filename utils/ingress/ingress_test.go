package ingress

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestGetRolloutIngressKeysForCanary(t *testing.T) {
	keys := GetRolloutIngressKeys(&v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myrollout",
			Namespace: "default",
		},
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
			Name:      "myrollout",
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
	assert.ElementsMatch(t, keys, []string{"default/stable-ingress", "default/myrollout-stable-ingress-canary"})
}

func TestGetCanaryIngressName(t *testing.T) {
	rollout := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myrollout",
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
	}

	t.Run("NoTrim", func(t *testing.T) {
		rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress = "stable-ingress"
		canaryIngress := GetCanaryIngressName(rollout)
		assert.Equal(t, "myrollout-stable-ingress-canary", canaryIngress)
	})
	t.Run("Trim", func(t *testing.T) {
		rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress = fmt.Sprintf("stable-ingress%s", strings.Repeat("a", 260))
		canaryIngress := GetCanaryIngressName(rollout)
		assert.Equal(t, 253, len(canaryIngress), "canary ingress truncated to 253")
		assert.Equal(t, true, strings.HasSuffix(canaryIngress, "-canary"), "canary ingress has -canary suffix")
	})
	t.Run("NoStableIngress", func(t *testing.T) {
		rollout.Spec.Strategy.Canary.TrafficRouting.Nginx = nil
		canaryIngress := GetCanaryIngressName(rollout)
		assert.Equal(t, "", canaryIngress, "canary ingress is empty")
	})
}
