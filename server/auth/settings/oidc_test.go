package settings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetOIDCConfigParsed(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: ConfigMapName, Namespace: testNamespace},
		Data: map[string]string{KeyOIDCConfig: `
name: Okta
issuer: https://example.okta.com
clientID: abc123
clientSecret: shh
requestedScopes:
  - openid
  - groups
`},
	}
	m := NewSettingsManager(fake.NewSimpleClientset(cm), testNamespace)

	cfg, ok, err := m.GetOIDCConfig(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "https://example.okta.com", cfg.Issuer)
	assert.Equal(t, "abc123", cfg.ClientID)
	assert.Equal(t, "shh", cfg.ClientSecret)
	assert.Equal(t, []string{"openid", "groups"}, cfg.RequestedScopes)
}

func TestGetOIDCConfigDefaultScopes(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: ConfigMapName, Namespace: testNamespace},
		Data:       map[string]string{KeyOIDCConfig: "issuer: https://i\nclientID: c\n"},
	}
	m := NewSettingsManager(fake.NewSimpleClientset(cm), testNamespace)

	cfg, ok, err := m.GetOIDCConfig(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{"openid", "profile", "email", "groups"}, cfg.RequestedScopes)
}

func TestGetOIDCConfigAbsent(t *testing.T) {
	m := NewSettingsManager(fake.NewSimpleClientset(), testNamespace)
	cfg, ok, err := m.GetOIDCConfig(context.Background())
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, cfg)
}

func TestGetOIDCConfigMalformed(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: ConfigMapName, Namespace: testNamespace},
		Data:       map[string]string{KeyOIDCConfig: "::: not yaml :::"},
	}
	m := NewSettingsManager(fake.NewSimpleClientset(cm), testNamespace)
	_, ok, err := m.GetOIDCConfig(context.Background())
	assert.Error(t, err)
	assert.False(t, ok)
}
