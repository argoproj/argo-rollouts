package trafficrouting

import (
	"time"

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
	// GetWeightUpdateDeadline returns the time at which the weight-update delay completes.
	// Returns (nil, nil) when no delay is pending or when not supported by the implementation.
	// Implementations must read the deadline from the source-of-truth (API server) — not an
	// informer cache — to avoid a staleness race with UpdateHash written in the same reconcile.
	GetWeightUpdateDeadline() (*time.Time, error)
	// ClearWeightUpdateDeadline removes the weight-update-deadline annotation from the routing resource.
	// Returns nil when not supported by the implementation.
	ClearWeightUpdateDeadline() error
	// Type returns the type of the traffic routing reconciler
	Type() string
}
