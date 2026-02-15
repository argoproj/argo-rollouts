package main

import (
	"fmt"

	goPlugin "github.com/hashicorp/go-plugin"

	rolloutsPlugin "github.com/argoproj/argo-rollouts/rollout/trafficrouting/plugin/rpc"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

type failingPlugin struct{}

func (p *failingPlugin) InitPlugin() types.RpcError {
	return types.RpcError{ErrorString: fmt.Sprintf("init failed on purpose")}
}
func (p *failingPlugin) SetWeight(_ *v1alpha1.Rollout, _ int32, _ []v1alpha1.WeightDestination) types.RpcError {
	return types.RpcError{}
}
func (p *failingPlugin) SetHeaderRoute(_ *v1alpha1.Rollout, _ *v1alpha1.SetHeaderRoute) types.RpcError {
	return types.RpcError{}
}
func (p *failingPlugin) VerifyWeight(_ *v1alpha1.Rollout, _ int32, _ []v1alpha1.WeightDestination) (types.RpcVerified, types.RpcError) {
	return types.Verified, types.RpcError{}
}
func (p *failingPlugin) UpdateHash(_ *v1alpha1.Rollout, _, _ string, _ []v1alpha1.WeightDestination) types.RpcError {
	return types.RpcError{}
}
func (p *failingPlugin) SetMirrorRoute(_ *v1alpha1.Rollout, _ *v1alpha1.SetMirrorRoute) types.RpcError {
	return types.RpcError{}
}
func (p *failingPlugin) RemoveManagedRoutes(_ *v1alpha1.Rollout) types.RpcError {
	return types.RpcError{}
}
func (p *failingPlugin) Type() string { return "failing" }

func main() {
	goPlugin.Serve(&goPlugin.ServeConfig{
		HandshakeConfig: goPlugin.HandshakeConfig{
			ProtocolVersion:  1,
			MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
			MagicCookieValue: "trafficrouter",
		},
		Plugins: map[string]goPlugin.Plugin{
			"RpcTrafficRouterPlugin": &rolloutsPlugin.RpcTrafficRouterPlugin{Impl: &failingPlugin{}},
		},
	})
}
