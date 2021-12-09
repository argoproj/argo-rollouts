package trafficrouting

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// GetCurrentPingPong return the names of the services.
// Which one "ping-service" or "pong-service" currently is a stable one which is a canary
func GetCurrentPingPong(ro *v1alpha1.Rollout) (string, string) {
	canary := ro.Spec.Strategy.Canary
	if IsStablePing(ro) {
		return canary.PingPong.PingService, canary.PingPong.PongService
	} else {
		return canary.PingPong.PongService, canary.PingPong.PingService
	}
}

// GetStableAndCanaryServices return a service names for current stable and canary services.
// If ping-pong feature enabled then the current ping or pong service will be returned. Which is a stable is defined
// based on a rollout status field Status.Canary.StablePingPong
func GetStableAndCanaryServices(ro *v1alpha1.Rollout) (string, string) {
	if IsPingPongEnabled(ro) {
		return GetCurrentPingPong(ro)
	} else {
		return ro.Spec.Strategy.Canary.StableService, ro.Spec.Strategy.Canary.CanaryService
	}
}

func isStablePing(current v1alpha1.PingPongType) bool {
	return current == v1alpha1.PPPing
}

func IsStablePing(ro *v1alpha1.Rollout) bool {
	return ro.Status.Canary.StablePingPong == v1alpha1.PPPing
}

func PingPongOpposite(current v1alpha1.PingPongType) v1alpha1.PingPongType {
	if isStablePing(current) {
		return v1alpha1.PPPong
	} else {
		return v1alpha1.PPPing
	}
}

func IsPingPongEnabled(ro *v1alpha1.Rollout) bool {
	return ro.Spec.Strategy.Canary != nil && ro.Spec.Strategy.Canary.PingPong != nil
}
