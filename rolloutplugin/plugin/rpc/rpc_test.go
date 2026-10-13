package rpc

import (
	"testing"

	goPlugin "github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

// recordingPlugin records the RolloutPlugin and weight received by each method.
type recordingPlugin struct {
	received map[string]*v1alpha1.RolloutPlugin
	weights  map[string]int32
}

func (p *recordingPlugin) record(method string, rp *v1alpha1.RolloutPlugin) {
	p.received[method] = rp
}

func (p *recordingPlugin) InitPlugin(string) types.RpcError { return types.RpcError{} }
func (p *recordingPlugin) WatchedGVK() (types.WatchedGVK, types.RpcError) {
	return types.WatchedGVK{Group: "apps", Version: "v1", Kind: "StatefulSet"}, types.RpcError{}
}
func (p *recordingPlugin) GetResourceStatus(rp *v1alpha1.RolloutPlugin) (*types.ResourceStatus, types.RpcError) {
	p.record("GetResourceStatus", rp)
	return &types.ResourceStatus{Replicas: 3, UpdatedRevision: "rev-2", Ready: true, ObservedResourceVersion: "42"}, types.RpcError{}
}
func (p *recordingPlugin) SetWeight(rp *v1alpha1.RolloutPlugin, weight int32) types.RpcError {
	p.record("SetWeight", rp)
	p.weights["SetWeight"] = weight
	return types.RpcError{}
}
func (p *recordingPlugin) VerifyWeight(rp *v1alpha1.RolloutPlugin, weight int32) (bool, types.RpcError) {
	p.record("VerifyWeight", rp)
	p.weights["VerifyWeight"] = weight
	return true, types.RpcError{}
}
func (p *recordingPlugin) PromoteFull(rp *v1alpha1.RolloutPlugin) types.RpcError {
	p.record("PromoteFull", rp)
	return types.RpcError{}
}
func (p *recordingPlugin) Validate(rp *v1alpha1.RolloutPlugin) types.RpcError {
	p.record("Validate", rp)
	return types.RpcError{ErrorString: "config rejected"}
}
func (p *recordingPlugin) Abort(rp *v1alpha1.RolloutPlugin) types.RpcError {
	p.record("Abort", rp)
	return types.RpcError{ErrorString: "abort failed"}
}
func (p *recordingPlugin) Restart(rp *v1alpha1.RolloutPlugin) types.RpcError {
	p.record("Restart", rp)
	return types.RpcError{}
}
func (p *recordingPlugin) Type() string { return "recording" }

// TestRolloutPluginCrossesRPC verifies every workload method delivers the full RolloutPlugin,
// including namespace, workloadRef and the per-RolloutPlugin spec.plugin.config, to the plugin,
// and the plugin's ResourceStatus, including its observed resourceVersion, back.
func TestRolloutPluginCrossesRPC(t *testing.T) {
	impl := &recordingPlugin{
		received: map[string]*v1alpha1.RolloutPlugin{},
		weights:  map[string]int32{},
	}
	client, _ := goPlugin.TestPluginRPCConn(t, map[string]goPlugin.Plugin{
		"RpcResourcePlugin": &ResourcePluginImpl{Impl: impl},
	}, nil)
	defer client.Close()

	raw, err := client.Dispense("RpcResourcePlugin")
	require.NoError(t, err)
	plugin := raw.(ResourcePlugin)

	rp := &v1alpha1.RolloutPlugin{
		ObjectMeta: metav1.ObjectMeta{Name: "my-rp", Namespace: "team-a"},
		Spec: v1alpha1.RolloutPluginSpec{
			WorkloadRef: v1alpha1.WorkloadRef{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "my-sts"},
			Plugin:      v1alpha1.PluginStep{Name: "example/plugin", Config: []byte(`{"maxSurge":2}`)},
		},
	}

	status, rpcErr := plugin.GetResourceStatus(rp)
	require.False(t, rpcErr.HasError())
	assert.Equal(t, int32(3), status.Replicas)
	assert.Equal(t, "rev-2", status.UpdatedRevision)
	assert.Equal(t, "42", status.ObservedResourceVersion)
	assert.False(t, plugin.SetWeight(rp, 25).HasError())
	verified, rpcErr := plugin.VerifyWeight(rp, 25)
	require.False(t, rpcErr.HasError())
	assert.True(t, verified)
	assert.False(t, plugin.PromoteFull(rp).HasError())
	assert.Equal(t, "abort failed", plugin.Abort(rp).ErrorString, "plugin errors must cross RPC")
	assert.False(t, plugin.Restart(rp).HasError())
	assert.Equal(t, "config rejected", plugin.Validate(rp).ErrorString, "validation errors must cross RPC")

	for _, method := range []string{"Validate", "GetResourceStatus", "SetWeight", "VerifyWeight", "PromoteFull", "Abort", "Restart"} {
		got := impl.received[method]
		require.NotNil(t, got, method)
		assert.Equal(t, "team-a", got.Namespace, method)
		assert.Equal(t, "my-rp", got.Name, method)
		assert.Equal(t, rp.Spec.WorkloadRef, got.Spec.WorkloadRef, method)
		assert.JSONEq(t, `{"maxSurge":2}`, string(got.Spec.Plugin.Config), method)
	}
	assert.Equal(t, int32(25), impl.weights["SetWeight"])
	assert.Equal(t, int32(25), impl.weights["VerifyWeight"])
}
