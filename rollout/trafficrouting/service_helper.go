package trafficrouting

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// GetStableAndCanaryServices return a service names for current stable and canary services.
// If ping-pong feature enabled then the current ping or pong service will be returned. Which is a stable is defined
// based on a rollout status field Status.Canary.StablePingPong

// isPingpongPreferred is needed when Rollout uses both pingpong service and stable/canary service
// for ALB trafficRouting, isPingpongPreferred is true. It uses pingpong service as priority
// for other trafficRouting, isPingpongPrefrered is false. It uses stable/canary service
// This is to ensure it is compatible with previous release.

func GetStableAndCanaryServices(ro *v1alpha1.Rollout, isPingpongPreferred bool) (string, string) {
	pingPongNotPreferredOtherServiceNotDefined := !isPingpongPreferred && ro.Spec.Strategy.Canary.StableService == "" && ro.Spec.Strategy.Canary.CanaryService == ""
	if IsPingPongEnabled(ro) &&
		(isPingpongPreferred || pingPongNotPreferredOtherServiceNotDefined) {
		canary := ro.Spec.Strategy.Canary
		if IsStablePing(ro) {
			return canary.PingPong.PingService, canary.PingPong.PongService
		} else {
			return canary.PingPong.PongService, canary.PingPong.PingService
		}
	}

	return ro.Spec.Strategy.Canary.StableService, ro.Spec.Strategy.Canary.CanaryService

}

// IsStablePing return true if the 'ping' service is pointing to the stable replica set.
// Which of the service currently is using is stored in a status.canary.stablePingPong.
// Return true in a case if status StablePingPong value equal to 'ping'. Return false in
// case when the status value is 'pong' or empty
func IsStablePing(ro *v1alpha1.Rollout) bool {
	return ro.Status.Canary.StablePingPong == v1alpha1.PPPing
}

func IsPingPongEnabled(ro *v1alpha1.Rollout) bool {
	return ro.Spec.Strategy.Canary != nil && ro.Spec.Strategy.Canary.PingPong != nil
}
