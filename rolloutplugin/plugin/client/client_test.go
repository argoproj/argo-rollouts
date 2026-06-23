package client

import (
	"context"
	"net/rpc"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	goPlugin "github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutsRpc "github.com/argoproj/argo-rollouts/rolloutplugin/plugin/rpc"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

func packageDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filename)
}

type testRpcPlugin struct{}

func (p *testRpcPlugin) InitPlugin(_ string) types.RpcError { return types.RpcError{} }
func (p *testRpcPlugin) GetResourceStatus(_ v1alpha1.WorkloadRef) (*types.ResourceStatus, types.RpcError) {
	return &types.ResourceStatus{}, types.RpcError{}
}
func (p *testRpcPlugin) SetWeight(_ v1alpha1.WorkloadRef, _ int32) types.RpcError {
	return types.RpcError{}
}
func (p *testRpcPlugin) VerifyWeight(_ v1alpha1.WorkloadRef, _ int32) (bool, types.RpcError) {
	return true, types.RpcError{}
}
func (p *testRpcPlugin) PromoteFull(_ v1alpha1.WorkloadRef) types.RpcError { return types.RpcError{} }
func (p *testRpcPlugin) Abort(_ v1alpha1.WorkloadRef) types.RpcError       { return types.RpcError{} }
func (p *testRpcPlugin) Restart(_ v1alpha1.WorkloadRef) types.RpcError     { return types.RpcError{} }
func (p *testRpcPlugin) Type() string                                      { return "TestRPCPlugin" }

func setupTestPlugin(t *testing.T) (*goPlugin.Client, goPlugin.ClientProtocol, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	testPluginMap := map[string]goPlugin.Plugin{
		"RpcResourcePlugin": &rolloutsRpc.ResourcePluginImpl{Impl: &testRpcPlugin{}},
	}
	ch := make(chan *goPlugin.ReattachConfig, 1)
	closeCh := make(chan struct{})
	go goPlugin.Serve(&goPlugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         testPluginMap,
		Test: &goPlugin.ServeTestConfig{
			Context:          ctx,
			ReattachConfigCh: ch,
			CloseCh:          closeCh,
		},
	})
	var config *goPlugin.ReattachConfig
	select {
	case config = <-ch:
	case <-time.After(2000 * time.Millisecond):
		t.Fatal("should've received reattach")
	}
	c := goPlugin.NewClient(&goPlugin.ClientConfig{
		Cmd:             nil,
		HandshakeConfig: handshakeConfig,
		Plugins:         testPluginMap,
		Reattach:        config,
	})
	client, err := c.Client()
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	cleanup := func() {
		cancel()
		select {
		case <-closeCh:
		case <-time.After(200 * time.Millisecond):
		}
	}
	return c, client, cleanup
}

func resetSingleton() {
	registry = nil
	once = sync.Once{}
	mutex = sync.Mutex{}
}

func initSingleton() {
	once.Do(func() {
		registry = &pluginRegistry{
			processClient: make(map[string]*goPlugin.Client),
			rpcConnClient: make(map[string]goPlugin.ClientProtocol),
			instances:     make(map[string]types.RpcResourcePlugin),
		}
	})
}

func buildTestPluginBinary(t *testing.T) string {
	t.Helper()
	pluginBinary := filepath.Join(t.TempDir(), "plugin")
	src := filepath.Join(packageDir(), "testdata")
	cmd := exec.Command("go", "build", "-o", pluginBinary, src)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build test plugin: %v\n%s", err, out)
	}
	return pluginBinary
}

func overrideGetPluginInfo(binary string, args ...string) func() {
	orig := getPluginInfo
	getPluginInfo = func(_ string, _ types.PluginType) (string, []string, error) {
		return binary, args, nil
	}
	return func() { getPluginInfo = orig }
}

func TestGetResourcePlugin_NotConfigured(t *testing.T) {
	resetSingleton()
	result, err := GetResourcePlugin("nonexistent-plugin", "")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unable to start plugin system")
}

func TestStartPlugin_ExistingPluginPingSuccess(t *testing.T) {
	resetSingleton()
	pluginClient, rpcClient, cleanup := setupTestPlugin(t)
	defer cleanup()
	defer pluginClient.Kill()

	initSingleton()
	pluginName := "test-plugin"
	registry.processClient[pluginName] = pluginClient
	registry.rpcConnClient[pluginName] = rpcClient

	raw, err := rpcClient.Dispense("RpcResourcePlugin")
	assert.NoError(t, err)
	p, ok := raw.(types.RpcResourcePlugin)
	assert.True(t, ok)
	registry.instances[pluginName] = p

	result, err := registry.startPlugin(pluginName, "")
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestStartPlugin_ExistingPluginPingFailure(t *testing.T) {
	resetSingleton()
	pluginClient, rpcClient, cleanup := setupTestPlugin(t)
	defer cleanup()

	initSingleton()
	pluginName := "test-plugin"
	registry.processClient[pluginName] = pluginClient
	registry.rpcConnClient[pluginName] = rpcClient

	raw, err := rpcClient.Dispense("RpcResourcePlugin")
	assert.NoError(t, err)
	p, ok := raw.(types.RpcResourcePlugin)
	assert.True(t, ok)
	registry.instances[pluginName] = p

	rpcClient.Close()
	pluginClient.Kill()
	time.Sleep(100 * time.Millisecond)

	result, err := registry.startPlugin(pluginName, "")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "could not ping plugin")
	assert.Nil(t, registry.processClient[pluginName])
	assert.Nil(t, registry.rpcConnClient[pluginName])
	assert.Nil(t, registry.instances[pluginName])
}

func TestStartPlugin_ReinitializeWhenRpcClientNil(t *testing.T) {
	resetSingleton()
	pluginClient, _, cleanup := setupTestPlugin(t)
	defer cleanup()
	defer pluginClient.Kill()

	initSingleton()
	pluginName := "test-plugin"
	registry.processClient[pluginName] = pluginClient
	registry.rpcConnClient[pluginName] = nil
	registry.instances[pluginName] = nil

	result, err := registry.startPlugin(pluginName, "")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unable to find plugin")
}

func TestStartPlugin_NewPluginFullFlow(t *testing.T) {
	resetSingleton()
	pluginClient, _, cleanup := setupTestPlugin(t)
	defer cleanup()
	defer pluginClient.Kill()

	initSingleton()
	pluginName := "test-plugin"
	registry.processClient[pluginName] = pluginClient
	pluginClient.Kill()
	time.Sleep(50 * time.Millisecond)

	result, err := registry.startPlugin(pluginName, "")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unable to find plugin")
}

func TestStartPlugin_FullInitFlow(t *testing.T) {
	resetSingleton()
	pluginBinary := buildTestPluginBinary(t)
	defer overrideGetPluginInfo(pluginBinary)()

	initSingleton()
	pluginName := "test-plugin"

	result, err := registry.startPlugin(pluginName, "")
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, registry.processClient[pluginName])
	assert.NotNil(t, registry.rpcConnClient[pluginName])
	assert.NotNil(t, registry.instances[pluginName])

	// Second call should reuse the cached client
	result2, err := registry.startPlugin(pluginName, "")
	assert.NoError(t, err)
	assert.NotNil(t, result2)

	if registry.processClient[pluginName] != nil {
		registry.processClient[pluginName].Kill()
	}
}

func TestStartPlugin_InitPluginHasError(t *testing.T) {
	resetSingleton()
	pluginBinary := buildTestPluginBinary(t)
	defer overrideGetPluginInfo(pluginBinary, "--fail-init")()

	initSingleton()
	result, err := registry.startPlugin("test-plugin", "")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unable to initialize plugin via rpc")

	if registry.processClient["test-plugin"] != nil {
		registry.processClient["test-plugin"].Kill()
	}
}

func TestStartPlugin_GetPluginClientError(t *testing.T) {
	resetSingleton()

	pluginBinary := filepath.Join(t.TempDir(), "bad-plugin")
	badPluginSrc := filepath.Join(t.TempDir(), "main.go")
	err := os.WriteFile(badPluginSrc, []byte("package main\nfunc main() {}\n"), 0644)
	assert.NoError(t, err)

	cmd := exec.Command("go", "build", "-o", pluginBinary, badPluginSrc)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build bad plugin: %v\n%s", err, out)
	}

	defer overrideGetPluginInfo(pluginBinary)()
	initSingleton()

	result, err := registry.startPlugin("test-plugin", "")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unable to get plugin client")
}

func TestStartPlugin_DispenseError(t *testing.T) {
	resetSingleton()
	pluginBinary := buildTestPluginBinary(t)
	defer overrideGetPluginInfo(pluginBinary)()

	origPluginMap := pluginMap
	pluginMap = map[string]goPlugin.Plugin{"WrongPluginName": &rolloutsRpc.ResourcePluginImpl{}}
	defer func() { pluginMap = origPluginMap }()

	initSingleton()
	result, err := registry.startPlugin("test-plugin", "")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unable to dispense plugin")

	if registry.processClient["test-plugin"] != nil {
		registry.processClient["test-plugin"].Kill()
	}
}

func TestStartPlugin_PingFailure(t *testing.T) {
	resetSingleton()
	pluginBinary := buildTestPluginBinary(t)
	defer overrideGetPluginInfo(pluginBinary)()

	testBeforePing = func(pluginName string) {
		if registry.rpcConnClient[pluginName] != nil {
			registry.rpcConnClient[pluginName].Close()
		}
		if registry.processClient[pluginName] != nil {
			registry.processClient[pluginName].Kill()
		}
		time.Sleep(100 * time.Millisecond)
	}
	defer func() { testBeforePing = nil }()

	initSingleton()
	pluginName := "test-plugin"

	result, err := registry.startPlugin(pluginName, "")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "could not ping plugin")
	assert.Nil(t, registry.processClient[pluginName])
	assert.Nil(t, registry.rpcConnClient[pluginName])
	assert.Nil(t, registry.instances[pluginName])
}

// wrongTypePlugin returns a type that does not implement types.RpcResourcePlugin from Client().
type wrongTypePlugin struct{}

func (w *wrongTypePlugin) Server(*goPlugin.MuxBroker) (interface{}, error) {
	return "not-a-resource-plugin", nil
}
func (w *wrongTypePlugin) Client(_ *goPlugin.MuxBroker, _ *rpc.Client) (interface{}, error) {
	return "not-a-resource-plugin", nil
}

func TestStartPlugin_TypeAssertionFailure(t *testing.T) {
	resetSingleton()
	pluginBinary := buildTestPluginBinary(t)
	defer overrideGetPluginInfo(pluginBinary)()

	origPluginMap := pluginMap
	pluginMap = map[string]goPlugin.Plugin{"RpcResourcePlugin": &wrongTypePlugin{}}
	defer func() { pluginMap = origPluginMap }()

	initSingleton()
	result, err := registry.startPlugin("test-plugin", "")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unexpected type from plugin")

	if registry.processClient["test-plugin"] != nil {
		registry.processClient["test-plugin"].Kill()
	}
}

func TestGetResourcePlugin_WithExistingPlugin(t *testing.T) {
	resetSingleton()
	pluginClient, rpcClient, cleanup := setupTestPlugin(t)
	defer cleanup()
	defer pluginClient.Kill()

	initSingleton()
	pluginName := "test-plugin"
	registry.processClient[pluginName] = pluginClient
	registry.rpcConnClient[pluginName] = rpcClient

	raw, err := rpcClient.Dispense("RpcResourcePlugin")
	assert.NoError(t, err)
	p, ok := raw.(types.RpcResourcePlugin)
	assert.True(t, ok)
	registry.instances[pluginName] = p

	result, err := GetResourcePlugin(pluginName, "")
	assert.NoError(t, err)
	assert.NotNil(t, result)
}
