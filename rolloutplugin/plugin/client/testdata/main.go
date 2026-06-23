package main

import (
	"os"

	goPlugin "github.com/hashicorp/go-plugin"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutsRpc "github.com/argoproj/argo-rollouts/rolloutplugin/plugin/rpc"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

type testPlugin struct{ failInit bool }

func (p *testPlugin) InitPlugin(_ string) types.RpcError {
	if p.failInit {
		return types.RpcError{ErrorString: "init failed on purpose"}
	}
	return types.RpcError{}
}

func (p *testPlugin) GetResourceStatus(_ v1alpha1.WorkloadRef) (*types.ResourceStatus, types.RpcError) {
	return &types.ResourceStatus{}, types.RpcError{}
}

func (p *testPlugin) SetWeight(_ v1alpha1.WorkloadRef, _ int32) types.RpcError {
	return types.RpcError{}
}

func (p *testPlugin) VerifyWeight(_ v1alpha1.WorkloadRef, _ int32) (bool, types.RpcError) {
	return true, types.RpcError{}
}

func (p *testPlugin) PromoteFull(_ v1alpha1.WorkloadRef) types.RpcError { return types.RpcError{} }
func (p *testPlugin) Abort(_ v1alpha1.WorkloadRef) types.RpcError       { return types.RpcError{} }
func (p *testPlugin) Restart(_ v1alpha1.WorkloadRef) types.RpcError     { return types.RpcError{} }
func (p *testPlugin) Type() string                                      { return "test" }

func main() {
	failInit := len(os.Args) > 1 && os.Args[1] == "--fail-init"
	goPlugin.Serve(&goPlugin.ServeConfig{
		HandshakeConfig: goPlugin.HandshakeConfig{
			ProtocolVersion:  1,
			MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
			MagicCookieValue: "resourceplugin",
		},
		Plugins: map[string]goPlugin.Plugin{
			"RpcResourcePlugin": &rolloutsRpc.ResourcePluginImpl{Impl: &testPlugin{failInit: failInit}},
		},
	})
}
