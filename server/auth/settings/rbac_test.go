package settings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetRBACConfig(t *testing.T) {
	client := fake.NewSimpleClientset(
		cmWith(RBACConfigMapName, map[string]string{
			KeyPolicyCSV:     "g, alice, role:operator",
			KeyPolicyDefault: "role:readonly",
		}),
	)
	m := NewSettingsManager(client, testNamespace)

	cfg, err := m.GetRBACConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "g, alice, role:operator", cfg.PolicyCSV)
	assert.Equal(t, "role:readonly", cfg.DefaultRole)
	assert.Equal(t, "glob", cfg.MatchMode, "match mode defaults to glob when unset")
}

func TestGetRBACConfigEmpty(t *testing.T) {
	client := fake.NewSimpleClientset() // no rbac cm
	m := NewSettingsManager(client, testNamespace)

	cfg, err := m.GetRBACConfig(context.Background())
	require.NoError(t, err)
	assert.Empty(t, cfg.PolicyCSV)
	assert.Empty(t, cfg.DefaultRole)
	assert.Equal(t, "glob", cfg.MatchMode)
}

func TestAnonymousEnabledDefaultFalse(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewSettingsManager(client, testNamespace)

	enabled, err := m.AnonymousEnabled(context.Background())
	require.NoError(t, err)
	assert.False(t, enabled, "anonymous access is off by default")
}

func TestAnonymousEnabledTrue(t *testing.T) {
	client := fake.NewSimpleClientset(
		cmWith(ConfigMapName, map[string]string{KeyAnonymousEnabled: "true"}),
	)
	m := NewSettingsManager(client, testNamespace)

	enabled, err := m.AnonymousEnabled(context.Background())
	require.NoError(t, err)
	assert.True(t, enabled)
}

func TestGetURL(t *testing.T) {
	client := fake.NewSimpleClientset(
		cmWith(ConfigMapName, map[string]string{KeyURL: "https://rollouts.example.com"}),
	)
	m := NewSettingsManager(client, testNamespace)

	url, err := m.GetURL(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "https://rollouts.example.com", url)
}
