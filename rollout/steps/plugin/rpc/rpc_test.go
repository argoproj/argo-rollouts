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
	MagicCookieValue: "step",
}

func pluginClient(t *testing.T) (StepPlugin, goPlugin.ClientProtocol, func(), chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())

	pluginImpl := &testRpcPlugin{}

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]goPlugin.Plugin{
		"RpcStepPlugin": &RpcStepPlugin{Impl: pluginImpl},
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
	raw, err := client.Dispense("RpcStepPlugin")
	if err != nil {
		t.Fail()
	}

	plugin, ok := raw.(StepPlugin)
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

	_, err = plugin.Run(&ro, &types.RpcStepContext{})
	assert.Equal(t, "", err.Error())

	_, err = plugin.Terminate(&ro, &types.RpcStepContext{})
	assert.Equal(t, "", err.Error())

	_, err = plugin.Abort(&ro, &types.RpcStepContext{})
	assert.Equal(t, "", err.Error())

	typeString := plugin.Type()
	assert.Equal(t, "StepPlugin Test", typeString)

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

	_, err = plugin.Run(&v1alpha1.Rollout{}, &types.RpcStepContext{})
	assert.Contains(t, err.Error(), expectedError)

	_, err = plugin.Terminate(&v1alpha1.Rollout{}, &types.RpcStepContext{})
	assert.Contains(t, err.Error(), expectedError)

	_, err = plugin.Abort(&v1alpha1.Rollout{}, &types.RpcStepContext{})
	assert.Contains(t, err.Error(), expectedError)

	cancel()
	<-closeCh
}

func TestInvalidArgs(t *testing.T) {
	server := StepRPCServer{}
	badtype := struct {
		Args string
	}{}

	var resp Response
	err := server.Run(badtype, &resp)
	assert.Error(t, err)

	err = server.Terminate(badtype, &resp)
	assert.Error(t, err)

	err = server.Abort(badtype, &resp)
	assert.Error(t, err)

}
