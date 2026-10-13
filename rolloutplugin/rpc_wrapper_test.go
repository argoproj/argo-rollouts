package rolloutplugin

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

// fakeRpcResourcePlugin is a types.RpcResourcePlugin whose every method returns a fixed
// RpcError, letting us exercise the wrapper's error conversion for both success and failure.
type fakeRpcResourcePlugin struct {
	err      types.RpcError
	verified bool
	status   *types.ResourceStatus
}

func (f *fakeRpcResourcePlugin) InitPlugin(string) types.RpcError { return f.err }
func (f *fakeRpcResourcePlugin) WatchedGVK() (types.WatchedGVK, types.RpcError) {
	return types.WatchedGVK{Group: "apps", Version: "v1", Kind: "StatefulSet"}, f.err
}
func (f *fakeRpcResourcePlugin) GetResourceStatus(_ *v1alpha1.RolloutPlugin) (*types.ResourceStatus, types.RpcError) {
	return f.status, f.err
}
func (f *fakeRpcResourcePlugin) SetWeight(_ *v1alpha1.RolloutPlugin, _ int32) types.RpcError {
	return f.err
}
func (f *fakeRpcResourcePlugin) VerifyWeight(_ *v1alpha1.RolloutPlugin, _ int32) (bool, types.RpcError) {
	return f.verified, f.err
}
func (f *fakeRpcResourcePlugin) PromoteFull(_ *v1alpha1.RolloutPlugin) types.RpcError {
	return f.err
}
func (f *fakeRpcResourcePlugin) Validate(_ *v1alpha1.RolloutPlugin) types.RpcError {
	return f.err
}
func (f *fakeRpcResourcePlugin) Abort(_ *v1alpha1.RolloutPlugin) types.RpcError {
	return f.err
}
func (f *fakeRpcResourcePlugin) Restart(_ *v1alpha1.RolloutPlugin) types.RpcError {
	return f.err
}
func (f *fakeRpcResourcePlugin) Type() string { return "fake" }

func TestAsError(t *testing.T) {
	assert.NoError(t, asError(types.RpcError{}, "abort"), "empty RpcError must convert to nil")

	err := asError(types.RpcError{ErrorString: "boom"}, "abort")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to abort")
	assert.Contains(t, err.Error(), "boom")

	// %w must keep the RpcError inspectable through the chain.
	var rpcErr types.RpcError
	assert.True(t, errors.As(err, &rpcErr), "wrapped RpcError should be recoverable via errors.As")
	assert.Equal(t, "boom", rpcErr.ErrorString)
}

func TestRpcPluginWrapper_PropagatesErrors(t *testing.T) {
	ctx := context.Background()
	rp := &v1alpha1.RolloutPlugin{Spec: v1alpha1.RolloutPluginSpec{WorkloadRef: v1alpha1.WorkloadRef{Name: "sts"}}}
	boom := types.RpcError{ErrorString: "boom"}

	t.Run("errors are wrapped per verb", func(t *testing.T) {
		w := RpcPluginWrapper{RpcResourcePlugin: &fakeRpcResourcePlugin{err: boom}}

		assert.ErrorContains(t, w.Init(ctx, "ns"), "failed to initialize plugin")
		_, err := w.GetResourceStatus(ctx, rp)
		assert.ErrorContains(t, err, "failed to get resource status")
		assert.ErrorContains(t, w.SetWeight(ctx, rp, 50), "failed to set weight")
		_, err = w.VerifyWeight(ctx, rp, 50)
		assert.ErrorContains(t, err, "failed to verify weight")
		assert.ErrorContains(t, w.PromoteFull(ctx, rp), "failed to promote")
		assert.ErrorContains(t, w.Abort(ctx, rp), "failed to abort")
		assert.ErrorContains(t, w.Restart(ctx, rp), "failed to restart")
		assert.EqualError(t, w.Validate(ctx, rp), "boom", "validation message is surfaced unprefixed as the InvalidSpec message")
	})

	t.Run("success returns nil and values pass through", func(t *testing.T) {
		status := &types.ResourceStatus{Ready: true, Replicas: 3, ObservedResourceVersion: "42"}
		fake := &fakeRpcResourcePlugin{verified: true, status: status}
		w := RpcPluginWrapper{RpcResourcePlugin: fake}

		assert.NoError(t, w.Init(ctx, "ns"))
		got, err := w.GetResourceStatus(ctx, rp)
		require.NoError(t, err)
		assert.Equal(t, status, got)
		assert.NoError(t, w.SetWeight(ctx, rp, 50))
		verified, err := w.VerifyWeight(ctx, rp, 50)
		require.NoError(t, err)
		assert.True(t, verified)
		assert.NoError(t, w.PromoteFull(ctx, rp))
		assert.NoError(t, w.Abort(ctx, rp))
		assert.NoError(t, w.Restart(ctx, rp))
		assert.NoError(t, w.Validate(ctx, rp))
	})
}
