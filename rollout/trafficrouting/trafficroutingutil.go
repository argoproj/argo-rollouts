package trafficrouting

// TODO: Modify tests to accomodate moved location of TrafficRoutingReconciler
// TrafficRoutingReconciler common function across all TrafficRouting implementation
type TrafficRoutingReconciler interface {
	// UpdateHash informs a traffic routing reconciler about new canary/stable pod hashes
	UpdateHash(canaryHash, stableHash string) error
	// SetWeight sets the canary weight to the desired weight
	SetWeight(desiredWeight int32, additionalDestinations ...WeightDestination) error
	// VerifyWeight returns true if the canary is at the desired weight
	VerifyWeight(desiredWeight int32) (bool, error)
	// Type returns the type of the traffic routing reconciler
	Type() string
}

// WeightDestination common struct
type WeightDestination struct {
	ServiceName     string
	PodTemplateHash string
	Weight          int32
}
