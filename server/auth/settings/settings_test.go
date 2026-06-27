package settings

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

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
