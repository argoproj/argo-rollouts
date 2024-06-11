package trafficrouting

import (
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const PING_SVC = "ping-service"
const PONG_SVC = "pong-service"

func fakeRollout(stableSvc, canarySvc string, pingPong *v1alpha1.PingPongSpec, stableIng string, port int32) *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rollout",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService: stableSvc,
					CanaryService: canarySvc,
					PingPong:      pingPong,
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						ALB: &v1alpha1.ALBTrafficRouting{
							Ingress:     stableIng,
							ServicePort: port,
						},
						Istio: &v1alpha1.IstioTrafficRouting{
							VirtualService: &v1alpha1.IstioVirtualService{
								Name: "istio-vsvc",
							},
							DestinationRule: &v1alpha1.IstioDestinationRule{
								Name:             "istio-destrule",
								CanarySubsetName: "canary",
								StableSubsetName: "stable",
							},
						},
					},
				},
			},
		},
	}
}

func TestGetStableAndCanaryServices(t *testing.T) {
	// Rollout has no pingPong
	rollout := fakeRollout("stable-service", "canary-service", nil, "stable-ingress", 443)

	stableService, canaryService := GetStableAndCanaryServices(rollout, true)
	assert.Equal(t, "stable-service", stableService)
	assert.Equal(t, "canary-service", canaryService)

	stableService, canaryService = GetStableAndCanaryServices(rollout, false)
	assert.Equal(t, "stable-service", stableService)
	assert.Equal(t, "canary-service", canaryService)

	// Rollout has pingPong and stable/canary
	pp := &v1alpha1.PingPongSpec{PingService: PING_SVC, PongService: PONG_SVC}
	rollout = fakeRollout("stable-service", "canary-service", pp, "stable-ingress", 443)

	stableService, canaryService = GetStableAndCanaryServices(rollout, true)
	assert.Equal(t, PONG_SVC, stableService)
	assert.Equal(t, PING_SVC, canaryService)

	stableService, canaryService = GetStableAndCanaryServices(rollout, false)
	assert.Equal(t, "stable-service", stableService)
	assert.Equal(t, "canary-service", canaryService)

	// Rollout has pingPong, no stable/canary
	rollout = fakeRollout("", "", pp, "stable-ingress", 443)

	stableService, canaryService = GetStableAndCanaryServices(rollout, true)
	assert.Equal(t, PONG_SVC, stableService)
	assert.Equal(t, PING_SVC, canaryService)

	stableService, canaryService = GetStableAndCanaryServices(rollout, false)
	assert.Equal(t, PONG_SVC, stableService)
	assert.Equal(t, PING_SVC, canaryService)
}
