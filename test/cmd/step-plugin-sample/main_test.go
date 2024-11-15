package main

import (
	"context"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/rollout/steps/plugin/rpc"
	rolloutsPlugin "github.com/argoproj/argo-rollouts/rollout/steps/plugin/rpc"
	"github.com/argoproj/argo-rollouts/test/cmd/step-plugin-sample/internal/plugin"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
	goPlugin "github.com/hashicorp/go-plugin"
	log "github.com/sirupsen/logrus"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/tj/assert"
)

var testHandshake = goPlugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
	MagicCookieValue: "step",
}

func pluginClient(t *testing.T) (rpc.StepPlugin, goPlugin.ClientProtocol, func(), chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())

	pluginImpl := plugin.New(log.WithFields(log.Fields{}), 0)

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]goPlugin.Plugin{
		"RpcStepPlugin": &rolloutsPlugin.RpcStepPlugin{Impl: pluginImpl},
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

	plugin, ok := raw.(rpc.StepPlugin)
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
	rpcCtx := &types.RpcStepContext{
		PluginName: "test-1",
		Config:     nil,
		Status:     nil,
	}

	result, err := plugin.Run(&ro, rpcCtx)
	assert.Equal(t, "", err.Error())
	assert.NotNil(t, result)

	// Canceling should cause an exit
	cancel()
	<-closeCh
}
