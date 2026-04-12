package dashboard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	options "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
)

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
