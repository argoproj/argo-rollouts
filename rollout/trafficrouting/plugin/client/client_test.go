package client

import (
	"context"
	"fmt"
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
	rolloutsRpc "github.com/argoproj/argo-rollouts/rollout/trafficrouting/plugin/rpc"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

// packageDir returns the absolute path to this package's directory,
// so that testdata paths work regardless of the working directory.
func packageDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filename)
}

type testRpcPlugin struct{}

func (p *testRpcPlugin) InitPlugin() types.RpcError {
	return types.RpcError{}
}
func (r *testRpcPlugin) SetWeight(ro *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) types.RpcError {
	return types.RpcError{}
}
func (r *testRpcPlugin) SetHeaderRoute(ro *v1alpha1.Rollout, headerRouting *v1alpha1.SetHeaderRoute) types.RpcError {
	return types.RpcError{}
}
func (r *testRpcPlugin) VerifyWeight(ro *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) (types.RpcVerified, types.RpcError) {
	return types.Verified, types.RpcError{}
}
func (r *testRpcPlugin) UpdateHash(ro *v1alpha1.Rollout, canaryHash, stableHash string, additionalDestinations []v1alpha1.WeightDestination) types.RpcError {
	return types.RpcError{}
}
func (r *testRpcPlugin) SetMirrorRoute(ro *v1alpha1.Rollout, setMirrorRoute *v1alpha1.SetMirrorRoute) types.RpcError {
	return types.RpcError{}
}
func (r *testRpcPlugin) RemoveManagedRoutes(ro *v1alpha1.Rollout) types.RpcError {
	return types.RpcError{}
}
func (r *testRpcPlugin) Type() string {
	return "TestRPCPlugin"
}

func setupTestPlugin(t *testing.T) (*goPlugin.Client, goPlugin.ClientProtocol, func()) {
	ctx, cancel := context.WithCancel(context.Background())

	testPluginMap := map[string]goPlugin.Plugin{
		"RpcTrafficRouterPlugin": &rolloutsRpc.RpcTrafficRouterPlugin{Impl: &testRpcPlugin{}},
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
	pluginClients = nil
	once = sync.Once{}
	mutex = sync.Mutex{}
}

func initSingleton() {
	once.Do(func() {
		pluginClients = &trafficPlugin{
			pluginClient: make(map[string]*goPlugin.Client),
			rpcClient:    make(map[string]goPlugin.ClientProtocol),
			plugin:       make(map[string]rolloutsRpc.TrafficRouterPlugin),
		}
	})
}

// buildTestPluginBinary builds the test plugin binary from testdata/ and returns its path.
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

// overrideGetPluginInfo overrides getPluginInfo to return the given binary path
// and optional args, and returns a cleanup function to restore the original.
func overrideGetPluginInfo(binary string, args ...string) func() {
	orig := getPluginInfo
	getPluginInfo = func(pluginName string, pluginType types.PluginType) (string, []string, error) {
		return binary, args, nil
	}
	return func() { getPluginInfo = orig }
}

// TestStartPlugin_ExistingPluginPingSuccess calls startPlugin() on an already-initialized plugin.
// Covers: else branch, cached rpcClient ping success, final return.
func TestStartPlugin_ExistingPluginPingSuccess(t *testing.T) {
	resetSingleton()

	pluginClient, rpcClient, cleanup := setupTestPlugin(t)
	defer cleanup()
	defer pluginClient.Kill()

	initSingleton()

	pluginName := "test-plugin"
	pluginClients.pluginClient[pluginName] = pluginClient
	pluginClients.rpcClient[pluginName] = rpcClient

	raw, err := rpcClient.Dispense("RpcTrafficRouterPlugin")
	assert.NoError(t, err)
	p, ok := raw.(rolloutsRpc.TrafficRouterPlugin)
	assert.True(t, ok)
	pluginClients.plugin[pluginName] = p

	result, err := pluginClients.startPlugin(pluginName)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestStartPlugin_ExistingPluginPingFailure calls startPlugin() when ping fails.
// Covers: else branch, ping failure, cleanup of all references.
func TestStartPlugin_ExistingPluginPingFailure(t *testing.T) {
	resetSingleton()

	pluginClient, rpcClient, cleanup := setupTestPlugin(t)
	defer cleanup()

	initSingleton()

	pluginName := "test-plugin"
	pluginClients.pluginClient[pluginName] = pluginClient
	pluginClients.rpcClient[pluginName] = rpcClient

	raw, err := rpcClient.Dispense("RpcTrafficRouterPlugin")
	assert.NoError(t, err)
	p, ok := raw.(rolloutsRpc.TrafficRouterPlugin)
	assert.True(t, ok)
	pluginClients.plugin[pluginName] = p

	rpcClient.Close()
	pluginClient.Kill()
	time.Sleep(100 * time.Millisecond)

	result, err := pluginClients.startPlugin(pluginName)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "could not ping plugin")
	assert.Nil(t, pluginClients.pluginClient[pluginName])
	assert.Nil(t, pluginClients.rpcClient[pluginName])
	assert.Nil(t, pluginClients.plugin[pluginName])
}

// TestStartPlugin_ReinitializeWhenRpcClientNil calls startPlugin() when rpcClient is nil.
// Covers: else branch, nil rpcClient check, kill, recursive call to startPluginLocked.
func TestStartPlugin_ReinitializeWhenRpcClientNil(t *testing.T) {
	resetSingleton()

	pluginClient, _, cleanup := setupTestPlugin(t)
	defer cleanup()
	defer pluginClient.Kill()

	initSingleton()

	pluginName := "test-plugin"
	pluginClients.pluginClient[pluginName] = pluginClient
	pluginClients.rpcClient[pluginName] = nil
	pluginClients.plugin[pluginName] = nil

	result, err := pluginClients.startPlugin(pluginName)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unable to find plugin")
}

// TestGetTrafficPlugin_NotConfigured calls GetTrafficPlugin with a non-existent plugin.
// Covers: GetTrafficPlugin, once.Do initialization, startPlugin if branch, getPluginInfo error.
func TestGetTrafficPlugin_NotConfigured(t *testing.T) {
	resetSingleton()

	result, err := GetTrafficPlugin("nonexistent-plugin")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unable to start plugin system")
}

// TestStartPlugin_NewPluginFullFlow uses a mock getPluginInfo to test the full
// initialization path in the if branch of startPluginLocked.
func TestStartPlugin_NewPluginFullFlow(t *testing.T) {
	resetSingleton()

	pluginClient, _, cleanup := setupTestPlugin(t)
	defer cleanup()
	defer pluginClient.Kill()

	initSingleton()

	pluginName := "test-plugin"
	pluginClients.pluginClient[pluginName] = pluginClient
	pluginClient.Kill()
	time.Sleep(50 * time.Millisecond)

	result, err := pluginClients.startPlugin(pluginName)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unable to find plugin")
}

// TestStartPlugin_NewPluginInitSuccess tests the full else-branch with caching
// by pre-configuring a reattach-based plugin client.
func TestStartPlugin_NewPluginInitSuccess(t *testing.T) {
	resetSingleton()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testPluginMap := map[string]goPlugin.Plugin{
		"RpcTrafficRouterPlugin": &rolloutsRpc.RpcTrafficRouterPlugin{Impl: &testRpcPlugin{}},
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

	var reattachConfig *goPlugin.ReattachConfig
	select {
	case reattachConfig = <-ch:
	case <-time.After(2000 * time.Millisecond):
		t.Fatal("should've received reattach")
	}

	defer func() {
		cancel()
		select {
		case <-closeCh:
		case <-time.After(200 * time.Millisecond):
		}
	}()

	initSingleton()

	pluginName := "test-plugin"

	origPluginMap := pluginMap
	pluginMap = testPluginMap
	defer func() { pluginMap = origPluginMap }()

	c := goPlugin.NewClient(&goPlugin.ClientConfig{
		Cmd:             nil,
		HandshakeConfig: handshakeConfig,
		Plugins:         testPluginMap,
		Reattach:        reattachConfig,
	})
	rpcClient, err := c.Client()
	assert.NoError(t, err)

	pluginClients.pluginClient[pluginName] = c
	pluginClients.rpcClient[pluginName] = rpcClient

	raw, err := rpcClient.Dispense("RpcTrafficRouterPlugin")
	assert.NoError(t, err)
	p, ok := raw.(rolloutsRpc.TrafficRouterPlugin)
	assert.True(t, ok)
	pluginClients.plugin[pluginName] = p

	result1, err := pluginClients.startPlugin(pluginName)
	assert.NoError(t, err)
	assert.NotNil(t, result1)

	result2, err := pluginClients.startPlugin(pluginName)
	assert.NoError(t, err)
	assert.NotNil(t, result2)
	assert.Equal(t, result1, result2)
}

// TestGetTrafficPlugin_WithExistingPlugin tests GetTrafficPlugin returns cached plugin.
func TestGetTrafficPlugin_WithExistingPlugin(t *testing.T) {
	resetSingleton()

	pluginClient, rpcClient, cleanup := setupTestPlugin(t)
	defer cleanup()
	defer pluginClient.Kill()

	pluginName := "test-plugin"

	initSingleton()

	pluginClients.pluginClient[pluginName] = pluginClient
	pluginClients.rpcClient[pluginName] = rpcClient

	raw, err := rpcClient.Dispense("RpcTrafficRouterPlugin")
	assert.NoError(t, err)
	p, ok := raw.(rolloutsRpc.TrafficRouterPlugin)
	assert.True(t, ok)
	pluginClients.plugin[pluginName] = p

	result, err := GetTrafficPlugin(pluginName)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestStartPlugin_FullInitFlow builds the sample traffic router plugin binary
// and tests the complete if-branch initialization path in startPluginLocked.
func TestStartPlugin_FullInitFlow(t *testing.T) {
	resetSingleton()

	pluginBinary := buildTestPluginBinary(t)
	defer overrideGetPluginInfo(pluginBinary)()

	initSingleton()

	pluginName := "test-plugin"

	result, err := pluginClients.startPlugin(pluginName)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	assert.NotNil(t, pluginClients.pluginClient[pluginName])
	assert.NotNil(t, pluginClients.rpcClient[pluginName])
	assert.NotNil(t, pluginClients.plugin[pluginName])

	// Call again - should hit else branch and reuse cached client
	result2, err := pluginClients.startPlugin(pluginName)
	assert.NoError(t, err)
	assert.NotNil(t, result2)

	if pluginClients.pluginClient[pluginName] != nil {
		pluginClients.pluginClient[pluginName].Kill()
	}
}

// TestStartPlugin_InitPluginHasError tests the if-branch where InitPlugin returns an error.
// Covers: resp.HasError() branch.
func TestStartPlugin_InitPluginHasError(t *testing.T) {
	resetSingleton()

	pluginBinary := buildTestPluginBinary(t)
	defer overrideGetPluginInfo(pluginBinary, "--fail-init")()

	initSingleton()

	result, err := pluginClients.startPlugin("test-plugin")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unable to initialize plugin via rpc")

	if pluginClients.pluginClient["test-plugin"] != nil {
		pluginClients.pluginClient["test-plugin"].Kill()
	}
}

// TestStartPlugin_GetPluginClientError tests the if-branch where Client() returns an error.
// Covers: Client() error branch.
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

	result, err := pluginClients.startPlugin("test-plugin")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unable to get plugin client")
}

// TestStartPlugin_DispenseError tests the if-branch where Dispense returns an error.
// Covers: Dispense error branch.
func TestStartPlugin_DispenseError(t *testing.T) {
	resetSingleton()

	pluginBinary := buildTestPluginBinary(t)
	defer overrideGetPluginInfo(pluginBinary)()

	origPluginMap := pluginMap
	pluginMap = map[string]goPlugin.Plugin{
		"WrongPluginName": &rolloutsRpc.RpcTrafficRouterPlugin{},
	}
	defer func() { pluginMap = origPluginMap }()

	initSingleton()

	result, err := pluginClients.startPlugin("test-plugin")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unable to dispense plugin")

	if pluginClients.pluginClient["test-plugin"] != nil {
		pluginClients.pluginClient["test-plugin"].Kill()
	}
}

// TestStartPlugin_PingFailureInIfBranch tests the if-branch where ping fails after
// successful init by killing the plugin process right before ping.
// Covers: lines 104-109 - ping failure cleanup in if-branch.
func TestStartPlugin_PingFailureInIfBranch(t *testing.T) {
	resetSingleton()

	pluginBinary := buildTestPluginBinary(t)
	defer overrideGetPluginInfo(pluginBinary)()

	// Set up hook to kill the plugin right before Ping()
	testBeforePing = func(pluginName string) {
		if pluginClients.rpcClient[pluginName] != nil {
			pluginClients.rpcClient[pluginName].Close()
		}
		if pluginClients.pluginClient[pluginName] != nil {
			pluginClients.pluginClient[pluginName].Kill()
		}
		time.Sleep(100 * time.Millisecond)
	}
	defer func() { testBeforePing = nil }()

	initSingleton()

	pluginName := "test-plugin"

	result, err := pluginClients.startPlugin(pluginName)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "could not ping plugin")
	assert.Nil(t, pluginClients.pluginClient[pluginName])
	assert.Nil(t, pluginClients.rpcClient[pluginName])
	assert.Nil(t, pluginClients.plugin[pluginName])
}

// wrongTypePlugin implements goPlugin.Plugin but its Client method returns
// a type that does not implement rolloutsRpc.TrafficRouterPlugin.
type wrongTypePlugin struct{}

func (w *wrongTypePlugin) Server(*goPlugin.MuxBroker) (interface{}, error) {
	return "not-a-traffic-router-plugin", nil
}

func (w *wrongTypePlugin) Client(_ *goPlugin.MuxBroker, _ *rpc.Client) (interface{}, error) {
	return "not-a-traffic-router-plugin", nil
}

// TestStartPlugin_TypeAssertionFailure tests the !ok branch when Dispense returns
// a type that does not implement rolloutsRpc.TrafficRouterPlugin.
// Covers: line 94 - "unexpected type from plugin"
func TestStartPlugin_TypeAssertionFailure(t *testing.T) {
	resetSingleton()

	pluginBinary := buildTestPluginBinary(t)
	defer overrideGetPluginInfo(pluginBinary)()

	origPluginMap := pluginMap
	pluginMap = map[string]goPlugin.Plugin{
		"RpcTrafficRouterPlugin": &wrongTypePlugin{},
	}
	defer func() { pluginMap = origPluginMap }()

	initSingleton()

	result, err := pluginClients.startPlugin("test-plugin")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unexpected type from plugin")

	if pluginClients.pluginClient["test-plugin"] != nil {
		pluginClients.pluginClient["test-plugin"].Kill()
	}
}

// TestStartPlugin_PingFailureAfterInit tests the else-branch where ping fails.
// Covers: else branch ping failure, cleanup of all references including rpcClient.
func TestStartPlugin_PingFailureAfterInit(t *testing.T) {
	resetSingleton()

	pluginClient, rpcClient, cleanup := setupTestPlugin(t)
	defer cleanup()

	initSingleton()

	pluginName := "test-plugin"
	pluginClients.pluginClient[pluginName] = pluginClient
	pluginClients.rpcClient[pluginName] = rpcClient

	raw, err := rpcClient.Dispense("RpcTrafficRouterPlugin")
	assert.NoError(t, err)
	p, ok := raw.(rolloutsRpc.TrafficRouterPlugin)
	assert.True(t, ok)
	pluginClients.plugin[pluginName] = p

	err = rpcClient.Ping()
	assert.NoError(t, err)

	rpcClient.Close()
	time.Sleep(100 * time.Millisecond)

	result, err := pluginClients.startPlugin(pluginName)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "could not ping plugin")
	assert.Nil(t, pluginClients.pluginClient[pluginName])
	assert.Nil(t, pluginClients.rpcClient[pluginName])
	assert.Nil(t, pluginClients.plugin[pluginName])
}

// Ensure fmt is used (for wrongTypePlugin error messages if needed).
var _ = fmt.Sprintf
