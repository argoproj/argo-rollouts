package ingress

import (
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// GetRolloutIngressKeys returns ingresses keys (namespace/ingressName) which are referenced by specified rollout
func GetRolloutIngressKeys(rollout *v1alpha1.Rollout) []string {
	if rollout.Spec.Strategy.Canary != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting.Nginx != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress != "" {

		// Also start watcher for `-canary` ingress which is created by the trafficmanagement controller
		return []string{
			fmt.Sprintf("%s/%s", rollout.Namespace, rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress),
			fmt.Sprintf("%s/%s-canary", rollout.Namespace, rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress),
		}
	}

	return nil
}
