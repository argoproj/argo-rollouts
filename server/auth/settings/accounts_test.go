package settings

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/server/auth/password"
)

func cmWith(name string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Data:       data,
	}
}

func TestGetAccountPropagatesAPIErrors(t *testing.T) {
	t.Run("secret get error", func(t *testing.T) {
		c := fake.NewSimpleClientset()
		c.PrependReactor("get", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("api down")
		})
		_, err := NewSettingsManager(c, testNamespace).GetAccount(context.Background(), AdminUsername)
		assert.Error(t, err)
	})
	t.Run("configmap get error", func(t *testing.T) {
		secret := secretWith(map[string][]byte{KeyAdminPassword: []byte("hash")})
		c := fake.NewSimpleClientset(secret)
		c.PrependReactor("get", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("api down")
		})
		_, err := NewSettingsManager(c, testNamespace).GetAccount(context.Background(), AdminUsername)
		assert.Error(t, err)
	})
}

func TestParseBoolDefaultFallsBack(t *testing.T) {
	assert.True(t, parseBoolDefault("", true))
	assert.False(t, parseBoolDefault("", false))
	assert.True(t, parseBoolDefault("not-a-bool", true), "unparseable value falls back to default")
}

func TestVerifyAdminPassword(t *testing.T) {
	hash, err := password.HashPassword("s3cret")
	require.NoError(t, err)
	client := fake.NewSimpleClientset(
		secretWith(map[string][]byte{KeyAdminPassword: []byte(hash)}),
	)
	m := NewSettingsManager(client, testNamespace)

	assert.NoError(t, m.VerifyUsernamePassword(context.Background(), AdminUsername, "s3cret"))
	assert.Error(t, m.VerifyUsernamePassword(context.Background(), AdminUsername, "wrong"))
}

func TestVerifyAdminDisabled(t *testing.T) {
	hash, err := password.HashPassword("s3cret")
	require.NoError(t, err)
	client := fake.NewSimpleClientset(
		secretWith(map[string][]byte{KeyAdminPassword: []byte(hash)}),
		cmWith(ConfigMapName, map[string]string{KeyAdminEnabled: "false"}),
	)
	m := NewSettingsManager(client, testNamespace)

	assert.Error(t, m.VerifyUsernamePassword(context.Background(), AdminUsername, "s3cret"),
		"disabled admin must not authenticate even with the right password")
}

func TestVerifyNamedAccount(t *testing.T) {
	hash, err := password.HashPassword("devpass")
	require.NoError(t, err)
	client := fake.NewSimpleClientset(
		secretWith(map[string][]byte{"accounts.dev.password": []byte(hash)}),
	)
	m := NewSettingsManager(client, testNamespace)

	assert.NoError(t, m.VerifyUsernamePassword(context.Background(), "dev", "devpass"))
	assert.Error(t, m.VerifyUsernamePassword(context.Background(), "dev", "nope"))
}

func TestVerifyUnknownAccount(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewSettingsManager(client, testNamespace)

	assert.Error(t, m.VerifyUsernamePassword(context.Background(), "ghost", "whatever"),
		"unknown account must fail closed")
}
