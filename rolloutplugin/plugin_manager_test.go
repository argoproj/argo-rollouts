package rolloutplugin

import (
	"context"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

// fakeResourcePlugin is a no-op ResourcePlugin used to verify registration wiring.
type fakeResourcePlugin struct {
	initialized bool
	namespace   string
	initErr     error
}

func (f *fakeResourcePlugin) Init(ctx context.Context, namespace string) error {
	f.initialized, f.namespace = true, namespace
	return f.initErr
}
func (f *fakeResourcePlugin) WatchedGVK() (schema.GroupVersionKind, error) {
	return appsv1.SchemeGroupVersion.WithKind("StatefulSet"), nil
}
func (f *fakeResourcePlugin) GetResourceStatus(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) (*ResourceStatus, error) {
	return nil, nil
}
func (f *fakeResourcePlugin) SetWeight(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, weight int32) error {
	return nil
}
func (f *fakeResourcePlugin) VerifyWeight(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, weight int32) (bool, error) {
	return true, nil
}
func (f *fakeResourcePlugin) PromoteFull(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error {
	return nil
}
func (f *fakeResourcePlugin) Validate(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error {
	return nil
}
func (f *fakeResourcePlugin) Abort(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error {
	return nil
}
func (f *fakeResourcePlugin) Restart(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error {
	return nil
}

func (f *fakeResourcePlugin) DeletionCleanup(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error {
	return nil
}

func newTestManager() *DefaultPluginManager {
	return &DefaultPluginManager{
		plugins:         make(map[string]ResourcePlugin),
		builtinEnabled:  make(map[string]bool),
		externalEnabled: make(map[string]bool),
	}
}

func fakeFactories() map[string]BuiltinPluginFactory {
	return map[string]BuiltinPluginFactory{
		"statefulset": func(logCtx *log.Entry, args []string) (ResourcePlugin, error) { return &fakeResourcePlugin{}, nil },
	}
}

func TestRegisterBuiltinPlugins_RegistersBuiltinUnderConfigMapName(t *testing.T) {
	pm := newTestManager()
	items := []types.PluginItem{
		{Name: "argoproj/statefulset", Location: "builtin://statefulset", Type: types.PluginTypeResourcePlugin},
	}

	require.NoError(t, pm.RegisterBuiltinPlugins(items, fakeFactories()))

	// Registered under the ConfigMap name, which is what spec.plugin.name references.
	plugin, err := pm.GetPlugin("argoproj/statefulset")
	require.NoError(t, err)
	require.NotNil(t, plugin)
	assert.False(t, plugin.(*fakeResourcePlugin).initialized, "plugins are initialized by Start, not at registration")
}

func TestRegisterBuiltinPlugins_ErrorsOnUnknownBuiltinID(t *testing.T) {
	pm := newTestManager()
	items := []types.PluginItem{
		{Name: "argoproj/daemonset", Location: "builtin://daemonset", Type: types.PluginTypeResourcePlugin},
	}

	err := pm.RegisterBuiltinPlugins(items, fakeFactories())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "daemonset")
}

// rpcPluginStub records the calls to the stubbed external-plugin constructor.
type rpcPluginStub struct {
	calls      int
	namespace  string
	initialize []bool
}

// withStubbedRpcPlugin swaps the external-plugin constructor for a fake that records its calls,
// restoring the original afterwards.
func withStubbedRpcPlugin(t *testing.T, plugin ResourcePlugin) *rpcPluginStub {
	t.Helper()
	orig := newRpcPlugin
	stub := &rpcPluginStub{}
	newRpcPlugin = func(name, namespace string, initialize bool) (ResourcePlugin, error) {
		stub.calls++
		stub.namespace = namespace
		stub.initialize = append(stub.initialize, initialize)
		return plugin, nil
	}
	t.Cleanup(func() { newRpcPlugin = orig })
	return stub
}

// Plugins are only initialized by Start, which runs once this replica is the leader; before
// that, external plugins are only connected so SetupWithManager can register their watches.
func TestStart_InitializesPlugins(t *testing.T) {
	pm := newTestManager()
	pm.namespace = "my-namespace"
	items := []types.PluginItem{
		{Name: "argoproj/statefulset", Location: "builtin://statefulset", Type: types.PluginTypeResourcePlugin},
		{Name: "resource/e2e-test", Location: "file://plugin-bin/e2e-resource-plugin", Type: types.PluginTypeResourcePlugin},
	}
	require.NoError(t, pm.RegisterBuiltinPlugins(items, fakeFactories()))
	stub := withStubbedRpcPlugin(t, &fakeResourcePlugin{})

	_, err := pm.EnabledPlugins()
	require.NoError(t, err)
	builtin, err := pm.GetPlugin("argoproj/statefulset")
	require.NoError(t, err)
	assert.False(t, builtin.(*fakeResourcePlugin).initialized)
	assert.Equal(t, []bool{false}, stub.initialize)

	require.NoError(t, pm.Start(context.Background()))
	assert.True(t, builtin.(*fakeResourcePlugin).initialized)
	assert.Equal(t, "my-namespace", builtin.(*fakeResourcePlugin).namespace)
	assert.Equal(t, []bool{false, true}, stub.initialize, "Start initializes external plugins")

	_, err = pm.GetPlugin("resource/e2e-test")
	require.NoError(t, err)
	assert.Equal(t, []bool{false, true, true}, stub.initialize, "external plugins connected after Start are initialized")
}

// TestGetPlugin_ExternalUsesManagerNamespace verifies Bug 1's fix: a lazily loaded external
// plugin is started with the manager's namespace even when no built-in was ever registered.
func TestGetPlugin_ExternalUsesManagerNamespace(t *testing.T) {
	pm := &DefaultPluginManager{
		plugins:         make(map[string]ResourcePlugin),
		builtinEnabled:  make(map[string]bool),
		externalEnabled: map[string]bool{"resource/e2e-test": true},
		namespace:       "my-namespace",
	}
	stub := withStubbedRpcPlugin(t, &fakeResourcePlugin{})

	_, err := pm.GetPlugin("resource/e2e-test")
	require.NoError(t, err)
	assert.Equal(t, "my-namespace", stub.namespace, "external plugin must get the manager's namespace")
}
