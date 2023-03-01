package rpc

import (
	"github.com/argoproj/argo-rollouts/utils/plugin/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

type testRpcPlugin struct{}

func (p *testRpcPlugin) InitPlugin() types.RpcError {
	return types.RpcError{}
}

func (r *testRpcPlugin) SetWeight(ro *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) types.RpcError {
	return types.RpcError{}
}

func (r *testRpcPlugin) SetHeaderRoute(ro *v1alpha1.Rollout, headerRouting *v1alpha1.SetHeaderRoute) types.RpcError {
	return types.RpcError{}
}

func (r *testRpcPlugin) VerifyWeight(ro *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) (types.RpcVerified, types.RpcError) {
	return types.Verified, types.RpcError{}
}

// UpdateHash informs a traffic routing reconciler about new canary/stable pod hashes
func (r *testRpcPlugin) UpdateHash(ro *v1alpha1.Rollout, canaryHash, stableHash string, additionalDestinations []v1alpha1.WeightDestination) types.RpcError {
	return types.RpcError{}
}

func (r *testRpcPlugin) SetMirrorRoute(ro *v1alpha1.Rollout, setMirrorRoute *v1alpha1.SetMirrorRoute) types.RpcError {
	return types.RpcError{}
}

func (r *testRpcPlugin) RemoveManagedRoutes(ro *v1alpha1.Rollout) types.RpcError {
	return types.RpcError{}
}

func (r *testRpcPlugin) Type() string {
	return "TestRPCPlugin"
}
