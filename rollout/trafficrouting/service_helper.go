package trafficrouting

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// GetStableAndCanaryServices return a service names for current stable and canary services.
// If ping-pong feature enabled then the current ping or pong service will be returned. Which is a stable is defined
// based on a rollout status field Status.Canary.StablePingPong
func GetStableAndCanaryServices(ro *v1alpha1.Rollout) (string, string) {
	if IsPingPongEnabled(ro) {
		canary := ro.Spec.Strategy.Canary
		if IsStablePing(ro) {
			return canary.PingPong.PingService, canary.PingPong.PongService
		} else {
			return canary.PingPong.PongService, canary.PingPong.PingService
		}
	} else {
		return ro.Spec.Strategy.Canary.StableService, ro.Spec.Strategy.Canary.CanaryService
	}
}

func IsStablePing(ro *v1alpha1.Rollout) bool {
	return ro.Status.Canary.StablePingPong == "" || ro.Status.Canary.StablePingPong == v1alpha1.PPPing
}

func PingPongOpposite(ro *v1alpha1.Rollout) v1alpha1.PingPongType {
	if ro.Status.Canary.StablePingPong != "" && IsStablePing(ro) {
		return v1alpha1.PPPong
	} else {
		return v1alpha1.PPPing
	}
}

func IsPingPongEnabled(ro *v1alpha1.Rollout) bool {
	return ro.Spec.Strategy.Canary != nil && ro.Spec.Strategy.Canary.PingPong != nil
}
