package dashboard

import (
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDashboardAuthModeFlagDefault(t *testing.T) {
	cmd := NewCmdDashboard(&options.ArgoRolloutsOptions{})
	f := cmd.Flags().Lookup("auth-mode")
	require.NotNil(t, f, "auth-mode flag must exist")
	assert.Equal(t, "none", f.DefValue, "auth-mode defaults to none (no behavior change)")
}
