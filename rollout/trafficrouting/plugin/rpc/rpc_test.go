package rpc

import (
	"context"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/utils/plugin/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	goPlugin "github.com/hashicorp/go-plugin"
	"github.com/tj/assert"
)

var testHandshake = goPlugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
	MagicCookieValue: "trafficrouter",
}

func pluginClient(t *testing.T) (TrafficRouterPlugin, goPlugin.ClientProtocol, func(), chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())

	rpcPluginImp := &testRpcPlugin{}

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]goPlugin.Plugin{
		"RpcTrafficRouterPlugin": &RpcTrafficRouterPlugin{Impl: rpcPluginImp},
	}

	ch := make(chan *goPlugin.ReattachConfig, 1)
	closeCh := make(chan struct{})
	go goPlugin.Serve(&goPlugin.ServeConfig{
		HandshakeConfig: testHandshake,
		Plugins:         pluginMap,
		Test: &goPlugin.ServeTestConfig{
			Context:          ctx,
			ReattachConfigCh: ch,
			CloseCh:          closeCh,
		},
	})

	// We should get a config
	var config *goPlugin.ReattachConfig
	select {
	case config = <-ch:
	case <-time.After(2000 * time.Millisecond):
		t.Fatal("should've received reattach")
	}
	if config == nil {
		t.Fatal("config should not be nil")
	}

	// Connect!
	c := goPlugin.NewClient(&goPlugin.ClientConfig{
		Cmd:             nil,
		HandshakeConfig: testHandshake,
		Plugins:         pluginMap,
		Reattach:        config,
	})
	client, err := c.Client()
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	// Request the plugin
	raw, err := client.Dispense("RpcTrafficRouterPlugin")
	if err != nil {
		t.Fail()
	}

	plugin, ok := raw.(TrafficRouterPlugin)
	if !ok {
		t.Fail()
	}

	return plugin, client, cancel, closeCh
}

func TestPlugin(t *testing.T) {
	plugin, _, cancel, closeCh := pluginClient(t)
	defer cancel()

	err := plugin.InitPlugin()
	if err.Error() != "" {
		t.Fail()
	}

	ro := v1alpha1.Rollout{}

	err = plugin.RemoveManagedRoutes(&ro)
	assert.Equal(t, "", err.Error())

	err = plugin.SetMirrorRoute(&ro, &v1alpha1.SetMirrorRoute{})
	assert.Equal(t, "", err.Error())

	err = plugin.SetHeaderRoute(&ro, &v1alpha1.SetHeaderRoute{})
	assert.Equal(t, "", err.Error())

	err = plugin.SetWeight(&ro, 0, []v1alpha1.WeightDestination{})
	assert.Equal(t, "", err.Error())

	b, err := plugin.VerifyWeight(&ro, 0, []v1alpha1.WeightDestination{})
	assert.Equal(t, "", err.Error())
	assert.Equal(t, true, *b.IsVerified())

	err = plugin.UpdateHash(&ro, "canary-hash", "stable-hash", []v1alpha1.WeightDestination{})
	assert.Equal(t, "", err.Error())

	typeString := plugin.Type()
	assert.Equal(t, "TestRPCPlugin", typeString)

	// Canceling should cause an exit
	cancel()
	<-closeCh
}

func TestPluginClosedConnection(t *testing.T) {
	plugin, client, cancel, closeCh := pluginClient(t)
	defer cancel()

	client.Close()
	time.Sleep(100 * time.Millisecond)

	const expectedError = "connection is shut down"

	err := plugin.InitPlugin()
	assert.Contains(t, err.Error(), expectedError)

	err = plugin.RemoveManagedRoutes(&v1alpha1.Rollout{})
	assert.Contains(t, err.Error(), expectedError)

	err = plugin.SetMirrorRoute(&v1alpha1.Rollout{}, &v1alpha1.SetMirrorRoute{})
	assert.Contains(t, err.Error(), expectedError)

	err = plugin.SetHeaderRoute(&v1alpha1.Rollout{}, &v1alpha1.SetHeaderRoute{})
	assert.Contains(t, err.Error(), expectedError)

	err = plugin.SetWeight(&v1alpha1.Rollout{}, 0, []v1alpha1.WeightDestination{})
	assert.Contains(t, err.Error(), expectedError)

	_, err = plugin.VerifyWeight(&v1alpha1.Rollout{}, 0, []v1alpha1.WeightDestination{})
	assert.Contains(t, err.Error(), expectedError)

	cancel()
	<-closeCh
}

func TestInvalidArgs(t *testing.T) {
	server := TrafficRouterRPCServer{}
	badtype := struct {
		Args string
	}{}

	var errRpc types.RpcError
	err := server.SetMirrorRoute(badtype, &errRpc)
	assert.Error(t, err)

	err = server.RemoveManagedRoutes(badtype, &errRpc)
	assert.Error(t, err)

	var vw VerifyWeightResponse
	err = server.VerifyWeight(badtype, &vw)
	assert.Error(t, err)

	err = server.SetMirrorRoute(badtype, &errRpc)
	assert.Error(t, err)

	err = server.SetHeaderRoute(badtype, &errRpc)
	assert.Error(t, err)

	err = server.SetWeight(badtype, &errRpc)
	assert.Error(t, err)

	err = server.UpdateHash(badtype, &errRpc)
	assert.Error(t, err)
}
