package server

import (
	"context"
	"strings"
	"testing"

	"github.com/argoproj/argo-rollouts/server/auth/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestSetupAuthBuildsComponents(t *testing.T) {
	ns := "argo-rollouts"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: settings.SecretName, Namespace: ns},
		Data:       map[string][]byte{settings.KeyServerSignature: []byte(strings.Repeat("k", 32))},
	}
	rbacCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: settings.RBACConfigMapName, Namespace: ns},
		Data:       map[string]string{settings.KeyPolicyDefault: "role:readonly"},
	}
	client := k8sfake.NewSimpleClientset(secret, rbacCM)
	s := NewServer(ServerOptions{KubeClientset: client, Namespace: ns, AuthMode: AuthModeServer})

	comps, err := s.setupAuth(context.Background())
	require.NoError(t, err)
	require.NotNil(t, comps)
	assert.NotNil(t, comps.authn)
	assert.NotNil(t, comps.authz)
	assert.NotNil(t, comps.login)
	assert.NotNil(t, comps.enforcer)
	assert.Equal(t, "role:readonly", comps.defaultRole)
}

func TestSetupAuthErrorsWithoutSigningKey(t *testing.T) {
	ns := "argo-rollouts"
	client := k8sfake.NewSimpleClientset() // no secret => no signing key
	s := NewServer(ServerOptions{KubeClientset: client, Namespace: ns, AuthMode: AuthModeServer})

	_, err := s.setupAuth(context.Background())
	assert.Error(t, err, "missing/short signing key must fail loudly, not silently disable auth")
}
