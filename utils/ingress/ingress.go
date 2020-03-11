package ingress

import (
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const (
	// CanaryIngressSuffix is the name suffix all canary ingresses created by the rollouts controller will have
	CanaryIngressSuffix = "-canary"
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
			fmt.Sprintf("%s/%s", rollout.Namespace, GetCanaryIngressName(rollout)),
		}
	}

	return nil
}

// GetCanaryIngressName constructs the name to use for the canary ingress resource from a given Rollout
func GetCanaryIngressName(rollout *v1alpha1.Rollout) string {
	// names limited to 253 characters
	if rollout.Spec.Strategy.Canary != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting.Nginx != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress != "" {

		prefix := fmt.Sprintf("%s-%s", rollout.GetName(), rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress)
		if len(prefix) > 253-len(CanaryIngressSuffix) {
			// trim prefix
			prefix = prefix[0 : 253-len(CanaryIngressSuffix)]
		}
		return fmt.Sprintf("%s%s", prefix, CanaryIngressSuffix)
	}
	return ""
}
