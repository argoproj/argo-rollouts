package rpc

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

type testRpcPlugin struct{}

func (p *testRpcPlugin) InitPlugin() types.RpcError {
	return types.RpcError{}
}

func (p *testRpcPlugin) Run(_ *v1alpha1.Rollout, _ *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	return types.RpcStepResult{}, types.RpcError{}
}

func (p *testRpcPlugin) Terminate(_ *v1alpha1.Rollout, _ *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	return types.RpcStepResult{}, types.RpcError{}
}

func (p *testRpcPlugin) Abort(_ *v1alpha1.Rollout, _ *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	return types.RpcStepResult{}, types.RpcError{}
}

// Type returns the type of the step plugin
func (p *testRpcPlugin) Type() string {
	return "StepPlugin Test"
}
