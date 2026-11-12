package weightutil

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// VerificationPending reports whether weights were applied but not yet verified by the provider.
// A nil weights or nil Verified is not pending.
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
