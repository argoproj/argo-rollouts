package trafficrouting

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// GetCurrentPingPong return the names of the services.
// Which one "ping-service" or "pong-service" currently is a stable one which is a canary
func GetCurrentPingPong(ro *v1alpha1.Rollout) (string, string) {
	canary := ro.Spec.Strategy.Canary
	if isPing(ro.Status.Canary.StablePingPong) {
		return canary.PingPong.PingService, canary.PingPong.PongService
	} else {
		return canary.PingPong.PongService, canary.PingPong.PingService
	}
}

func isPing(current v1alpha1.PingPongType) bool {
	return current == v1alpha1.PPPing
}

func PingPongOpposite(current v1alpha1.PingPongType) v1alpha1.PingPongType {
	if isPing(current) {
		return v1alpha1.PPPong
	} else {
		return v1alpha1.PPPing
	}
}

func IsPingPongEnabled(ro *v1alpha1.Rollout) bool {
	return ro.Spec.Strategy.Canary != nil && ro.Spec.Strategy.Canary.PingPong != nil
}
