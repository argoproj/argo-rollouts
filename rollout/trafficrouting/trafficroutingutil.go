package trafficrouting

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// TrafficRoutingReconciler common function across all TrafficRouting implementation
type TrafficRoutingReconciler interface {
	// UpdateHash informs a traffic routing reconciler about new canary, stable, and additionalDestination(s) pod hashes
	UpdateHash(canaryHash, stableHash string, additionalDestinations ...v1alpha1.WeightDestination) error
	// SetWeight sets the canary weight to the desired weight
	SetWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error
	// VerifyWeight returns true if the canary is at the desired weight and additionalDestinations are at the weights specified
	// Returns nil if weight verification is not supported or not applicable
	VerifyWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error)
	// Type returns the type of the traffic routing reconciler
	Type() string
}
