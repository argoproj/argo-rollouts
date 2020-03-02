package ingress

import (
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// GetRolloutIngressKeys returns ingresses keys (namespace/ingressName) which are referenced by specified rollout
func GetRolloutIngressKeys(rollout *v1alpha1.Rollout) []string {
	ingressesSet := make(map[string]bool)
	if rollout.Spec.Strategy.Canary != nil && rollout.Spec.Strategy.Canary.TrafficRouting != nil && rollout.Spec.Strategy.Canary.TrafficRouting.Nginx != nil {
		if rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress != "" {
			//log.Errorf("GetRolloutIngressKeys has nginx routing %s: %s", rollout.Name, rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress)
			ingressesSet[fmt.Sprintf("%s/%s", rollout.Namespace, rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress)] = true
			// Also start watcher for `-canary` ingress which is created by the trafficmanagement controller
			ingressesSet[fmt.Sprintf("%s/%s-canary", rollout.Namespace, rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress)] = true
		}
	}
	var ingresses []string
	for svc := range ingressesSet {
		ingresses = append(ingresses, svc)
	}
	return ingresses
}
