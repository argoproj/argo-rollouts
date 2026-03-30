package dashboard

import (
	"testing"

	"github.com/stretchr/testify/assert"

	options "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
)

func TestNewCmdDashboard_InvalidAuthMode(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdDashboard(o)
	cmd.SetArgs([]string{"--auth-mode", "invalid"})
	err := cmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid auth mode")
}

func TestNewCmdDashboard_OIDCRequiresTokenMode(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdDashboard(o)
	cmd.SetArgs([]string{"--oidc-issuer-url", "https://example.com"})
	err := cmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--oidc-issuer-url requires --auth-mode=token")
}

func TestNewCmdDashboard_DefaultFlags(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdDashboard(o)

	rootPath, _ := cmd.Flags().GetString("root-path")
	assert.Equal(t, "rollouts", rootPath)

	port, _ := cmd.Flags().GetInt("port")
	assert.Equal(t, 3100, port)

	authMode, _ := cmd.Flags().GetString("auth-mode")
	assert.Equal(t, "server", authMode)

	oidcIssuerURL, _ := cmd.Flags().GetString("oidc-issuer-url")
	assert.Equal(t, "", oidcIssuerURL)

	oidcClientID, _ := cmd.Flags().GetString("oidc-client-id")
	assert.Equal(t, "", oidcClientID)

	oidcClientSecret, _ := cmd.Flags().GetString("oidc-client-secret")
	assert.Equal(t, "", oidcClientSecret)

	oidcRedirectURL, _ := cmd.Flags().GetString("oidc-redirect-url")
	assert.Equal(t, "", oidcRedirectURL)

	oidcScopes, _ := cmd.Flags().GetString("oidc-scopes")
	assert.Equal(t, "", oidcScopes)
}
