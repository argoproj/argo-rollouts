package main

import (
	goPlugin "github.com/hashicorp/go-plugin"

	rolloutsPlugin "github.com/argoproj/argo-rollouts/rollout/trafficrouting/plugin/rpc"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

type testPlugin struct{}

func (p *testPlugin) InitPlugin() types.RpcError                    { return types.RpcError{} }
func (p *testPlugin) SetWeight(_ *v1alpha1.Rollout, _ int32, _ []v1alpha1.WeightDestination) types.RpcError {
	return types.RpcError{}
}
func (p *testPlugin) SetHeaderRoute(_ *v1alpha1.Rollout, _ *v1alpha1.SetHeaderRoute) types.RpcError {
	return types.RpcError{}
}
func (p *testPlugin) VerifyWeight(_ *v1alpha1.Rollout, _ int32, _ []v1alpha1.WeightDestination) (types.RpcVerified, types.RpcError) {
	return types.Verified, types.RpcError{}
}
func (p *testPlugin) UpdateHash(_ *v1alpha1.Rollout, _, _ string, _ []v1alpha1.WeightDestination) types.RpcError {
	return types.RpcError{}
}
func (p *testPlugin) SetMirrorRoute(_ *v1alpha1.Rollout, _ *v1alpha1.SetMirrorRoute) types.RpcError {
	return types.RpcError{}
}
func (p *testPlugin) RemoveManagedRoutes(_ *v1alpha1.Rollout) types.RpcError {
	return types.RpcError{}
}
func (p *testPlugin) Type() string { return "test" }

func main() {
	goPlugin.Serve(&goPlugin.ServeConfig{
		HandshakeConfig: goPlugin.HandshakeConfig{
			ProtocolVersion:  1,
			MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
			MagicCookieValue: "trafficrouter",
		},
		Plugins: map[string]goPlugin.Plugin{
			"RpcTrafficRouterPlugin": &rolloutsPlugin.RpcTrafficRouterPlugin{Impl: &testPlugin{}},
		},
	})
}
