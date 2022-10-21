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
	// SetHeaderRoute sets the header routing step
	SetHeaderRoute(setHeaderRoute *v1alpha1.SetHeaderRoute) error
	// SetMirrorRoute sets up the traffic router to mirror traffic to a service
	SetMirrorRoute(setMirrorRoute *v1alpha1.SetMirrorRoute) error
	// VerifyWeight returns true if the canary is at the desired weight and additionalDestinations are at the weights specified
	// Returns nil if weight verification is not supported or not applicable
	VerifyWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error)
	// RemoveAllRoutes Removes all routes that are managed by rollouts by looking at spec.strategy.canary.trafficRouting.managedRoutes
	RemoveManagedRoutes() error
	// Type returns the type of the traffic routing reconciler
	Type() string
}
