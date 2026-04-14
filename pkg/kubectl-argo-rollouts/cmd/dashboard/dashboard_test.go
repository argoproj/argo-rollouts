package dashboard

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	options "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	fakeoptions "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
)

// failingRESTConfigGetter wraps a RESTClientGetter and overrides ToRESTConfig to return an error
type failingRESTConfigGetter struct {
	delegate genericclioptions.RESTClientGetter
}

func (f *failingRESTConfigGetter) ToRESTConfig() (*rest.Config, error) {
	return nil, fmt.Errorf("mock REST config error")
}

func (f *failingRESTConfigGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return f.delegate.ToRawKubeConfigLoader()
}

func (f *failingRESTConfigGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	return f.delegate.ToDiscoveryClient()
}

func (f *failingRESTConfigGetter) ToRESTMapper() (meta.RESTMapper, error) {
	return f.delegate.ToRESTMapper()
}

func TestNewCmdDashboard(t *testing.T) {
	streams := genericclioptions.IOStreams{}
	o := options.NewArgoRolloutsOptions(streams)

	t.Run("default auth mode is server", func(t *testing.T) {
		cmd := NewCmdDashboard(o)
		f := cmd.Flags().Lookup("auth-mode")
		assert.NotNil(t, f)
		assert.Equal(t, "server", f.DefValue)
	})

	t.Run("has port flag", func(t *testing.T) {
		cmd := NewCmdDashboard(o)
		f := cmd.Flags().Lookup("port")
		assert.NotNil(t, f)
		assert.Equal(t, "3100", f.DefValue)
	})

	t.Run("has root-path flag", func(t *testing.T) {
		cmd := NewCmdDashboard(o)
		f := cmd.Flags().Lookup("root-path")
		assert.NotNil(t, f)
		assert.Equal(t, "rollouts", f.DefValue)
	})

	t.Run("rejects invalid auth mode", func(t *testing.T) {
		cmd := NewCmdDashboard(o)
		cmd.Flags().Set("auth-mode", "invalid")
		err := cmd.RunE(cmd, []string{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid auth mode")
	})
}

func TestDashboardClientAuthModeRESTConfigFailure(t *testing.T) {
	tf, o := fakeoptions.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	// Wrap the RESTClientGetter so ToRESTConfig fails but other methods work
	o.RESTClientGetter = &failingRESTConfigGetter{delegate: tf}
	cmd := NewCmdDashboard(o)
	cmd.Flags().Set("auth-mode", "client")
	err := cmd.RunE(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get REST config")
}
