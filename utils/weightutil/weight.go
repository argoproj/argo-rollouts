package weightutil

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// VerificationPending reports whether recorded traffic weights were applied but the traffic
// provider has not yet verified them (Verified explicitly false, e.g. ALB load balancer weights
// still propagating). While pending, serving capacity must not be reduced and promotion must not
// proceed: the provider may still be routing traffic according to the previous weights. A nil
// weights or nil Verified (verification not implemented or not applicable) is not pending.
func VerificationPending(weights *v1alpha1.TrafficWeights) bool {
	return weights != nil && weights.Verified != nil && !*weights.Verified
}

func MaxTrafficWeight(ro *v1alpha1.Rollout) int32 {
	maxWeight := int32(100)
	if ro.Spec.Strategy.Canary != nil && ro.Spec.Strategy.Canary.TrafficRouting != nil && ro.Spec.Strategy.Canary.TrafficRouting.MaxTrafficWeight != nil {
		maxWeight = *ro.Spec.Strategy.Canary.TrafficRouting.MaxTrafficWeight
	}
	return maxWeight
}
