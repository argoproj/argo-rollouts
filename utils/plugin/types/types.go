package types

import (
	"encoding/gob"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func init() {
	gob.RegisterName("RpcError", new(RpcError))
}

type RpcError struct {
	ErrorString string
}

func (e RpcError) Error() string {
	return e.ErrorString
}

type RpcMetricProvider interface {
	// Run start a new external system call for a measurement
	// Should be idempotent and do nothing if a call has already been started
	Run(*v1alpha1.AnalysisRun, v1alpha1.Metric) v1alpha1.Measurement
	// Checks if the external system call is finished and returns the current measurement
	Resume(*v1alpha1.AnalysisRun, v1alpha1.Metric, v1alpha1.Measurement) v1alpha1.Measurement
	// Terminate will terminate an in-progress measurement
	Terminate(*v1alpha1.AnalysisRun, v1alpha1.Metric, v1alpha1.Measurement) v1alpha1.Measurement
	// GarbageCollect is used to garbage collect completed measurements to the specified limit
	GarbageCollect(*v1alpha1.AnalysisRun, v1alpha1.Metric, int) RpcError
	// Type gets the provider type
	Type() string
	// GetMetadata returns any additional metadata which providers need to store/display as part
	// of the metric result. For example, Prometheus uses is to store the final resolved queries.
	GetMetadata(metric v1alpha1.Metric) map[string]string
}

type RpcTrafficRoutingReconciler interface {
	// UpdateHash informs a traffic routing reconciler about new canary, stable, and additionalDestination(s) pod hashes
	UpdateHash(rollout *v1alpha1.Rollout, canaryHash, stableHash string, additionalDestinations []v1alpha1.WeightDestination) RpcError
	// SetWeight sets the canary weight to the desired weight
	SetWeight(rollout *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) RpcError
	// SetHeaderRoute sets the header routing step
	SetHeaderRoute(rollout *v1alpha1.Rollout, setHeaderRoute *v1alpha1.SetHeaderRoute) RpcError
	// SetMirrorRoute sets up the traffic router to mirror traffic to a service
	SetMirrorRoute(rollout *v1alpha1.Rollout, setMirrorRoute *v1alpha1.SetMirrorRoute) RpcError
	// VerifyWeight returns true if the canary is at the desired weight and additionalDestinations are at the weights specified
	// Returns nil if weight verification is not supported or not applicable
	VerifyWeight(rollout *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) (*bool, RpcError)
	// RemoveAllRoutes Removes all routes that are managed by rollouts by looking at spec.strategy.canary.trafficRouting.managedRoutes
	RemoveManagedRoutes(ro *v1alpha1.Rollout) RpcError
	// Type returns the type of the traffic routing reconciler
	Type() string
}
