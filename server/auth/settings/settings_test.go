package settings

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestAPIErrorsPropagate(t *testing.T) {
	// Non-NotFound API errors must surface rather than be swallowed as empty data.
	client := fake.NewSimpleClientset()
	client.PrependReactor("get", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("api down")
	})
	client.PrependReactor("get", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("api down")
	})
	m := NewSettingsManager(client, testNamespace)

	_, err := m.GetSigningKey(context.Background())
	assert.Error(t, err, "secret get error must propagate")

	_, err = m.GetRBACConfig(context.Background())
	assert.Error(t, err, "configmap get error must propagate")

	_, err = m.AnonymousEnabled(context.Background())
	assert.Error(t, err, "anonymous flag get error must propagate")

	_, err = m.GetURL(context.Background())
	assert.Error(t, err, "url get error must propagate")

	_, _, err = m.GetOIDCConfig(context.Background())
	assert.Error(t, err, "oidc config get error must propagate")
}

func TestGetRBACConfigHonorsMatchMode(t *testing.T) {
	rbacCM := cmWith(RBACConfigMapName, map[string]string{KeyPolicyMatchMode: "regex"})
	m := NewSettingsManager(fake.NewSimpleClientset(rbacCM), testNamespace)
	cfg, err := m.GetRBACConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "regex", cfg.MatchMode, "explicit matchMode must be honored over the glob default")
}

func TestGetTLSCertificateRejectsInvalidKeypair(t *testing.T) {
	secret := secretWith(map[string][]byte{
		KeyTLSCert: []byte("not-a-cert"),
		KeyTLSKey:  []byte("not-a-key"),
	})
	m := NewSettingsManager(fake.NewSimpleClientset(secret), testNamespace)
	_, ok, err := m.GetTLSCertificate(context.Background())
	assert.Error(t, err, "invalid keypair must error")
	assert.False(t, ok)
}

const testNamespace = "argo-rollouts"

func secretWith(data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: testNamespace},
		Data:       data,
	}
}

func TestGetSigningKeyReturnsKey(t *testing.T) {
	key := []byte(strings.Repeat("k", MinSigningKeyLength))
	client := fake.NewSimpleClientset(secretWith(map[string][]byte{KeyServerSignature: key}))
	m := NewSettingsManager(client, testNamespace)

	got, err := m.GetSigningKey(context.Background())
	require.NoError(t, err)
	assert.Equal(t, key, got)
}

func TestGetSigningKeyRejectsShortKey(t *testing.T) {
	client := fake.NewSimpleClientset(secretWith(map[string][]byte{KeyServerSignature: []byte("too-short")}))
	m := NewSettingsManager(client, testNamespace)

	_, err := m.GetSigningKey(context.Background())
	assert.Error(t, err)
}

func TestGetSigningKeyMissingSecret(t *testing.T) {
	client := fake.NewSimpleClientset() // no secret at all
	m := NewSettingsManager(client, testNamespace)

	_, err := m.GetSigningKey(context.Background())
	assert.Error(t, err, "missing signing key must be an error, not empty success")
}
