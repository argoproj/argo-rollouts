package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/server/auth"
	"github.com/argoproj/argo-rollouts/server/auth/rbac"
	"github.com/argoproj/argo-rollouts/server/auth/settings"
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

func oidcDiscoveryServer(t *testing.T) *httptest.Server {
	t.Helper()
	var issuer string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 issuer,
			"authorization_endpoint": issuer + "/auth",
			"token_endpoint":         issuer + "/token",
			"jwks_uri":               issuer + "/keys",
		})
	})
	srv := httptest.NewServer(mux)
	issuer = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

func authSecretAndNS() (*corev1.Secret, string) {
	ns := "argo-rollouts"
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: settings.SecretName, Namespace: ns},
		Data:       map[string][]byte{settings.KeyServerSignature: []byte(strings.Repeat("k", 32))},
	}, ns
}

func TestSetupAuthBuildsOIDCHandler(t *testing.T) {
	srv := oidcDiscoveryServer(t)
	secret, ns := authSecretAndNS()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: settings.ConfigMapName, Namespace: ns},
		Data: map[string]string{
			settings.KeyOIDCConfig: "issuer: " + srv.URL + "\nclientID: client\n",
			settings.KeyURL:        "https://dash.example.com",
		},
	}
	client := k8sfake.NewSimpleClientset(secret, cm)
	s := NewServer(ServerOptions{KubeClientset: client, Namespace: ns, AuthMode: AuthModeServer})

	comps, err := s.setupAuth(context.Background())
	require.NoError(t, err)
	require.NotNil(t, comps.oidc, "OIDC handler must be built when oidc.config and url are present")
}

func TestSetupAuthOIDCRequiresURL(t *testing.T) {
	secret, ns := authSecretAndNS()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: settings.ConfigMapName, Namespace: ns},
		Data:       map[string]string{settings.KeyOIDCConfig: "issuer: https://idp.example.com\nclientID: c\n"},
	}
	client := k8sfake.NewSimpleClientset(secret, cm)
	s := NewServer(ServerOptions{KubeClientset: client, Namespace: ns, AuthMode: AuthModeServer})

	_, err := s.setupAuth(context.Background())
	require.Error(t, err, "OIDC configured without a dashboard url must fail")
}

func TestSetupAuthOIDCDiscoveryError(t *testing.T) {
	secret, ns := authSecretAndNS()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: settings.ConfigMapName, Namespace: ns},
		Data: map[string]string{
			// Unreachable issuer => provider discovery fails.
			settings.KeyOIDCConfig: "issuer: http://127.0.0.1:1/oidc\nclientID: c\n",
			settings.KeyURL:        "https://dash.example.com",
		},
	}
	client := k8sfake.NewSimpleClientset(secret, cm)
	s := NewServer(ServerOptions{KubeClientset: client, Namespace: ns, AuthMode: AuthModeServer})

	_, err := s.setupAuth(context.Background())
	require.Error(t, err)
}

func TestSetupAuthAnonymousEnabled(t *testing.T) {
	secret, ns := authSecretAndNS()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: settings.ConfigMapName, Namespace: ns},
		Data:       map[string]string{settings.KeyAnonymousEnabled: "true"},
	}
	client := k8sfake.NewSimpleClientset(secret, cm)
	s := NewServer(ServerOptions{KubeClientset: client, Namespace: ns, AuthMode: AuthModeServer})

	comps, err := s.setupAuth(context.Background())
	require.NoError(t, err)
	require.NotNil(t, comps.authn)
}

// fakeWatchStream is a minimal RolloutService_WatchRolloutInfoServer carrying a context.
type fakeWatchStream struct {
	rollout.RolloutService_WatchRolloutInfoServer
	ctx context.Context
}

func (f *fakeWatchStream) Context() context.Context { return f.ctx }

// authedServer builds an ArgoRolloutsServer with auth enabled, using policyCSV as the RBAC policy.
func authedServer(t *testing.T, policyCSV string) *ArgoRolloutsServer {
	t.Helper()
	ns := "argo-rollouts"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: settings.SecretName, Namespace: ns},
		Data:       map[string][]byte{settings.KeyServerSignature: []byte(strings.Repeat("k", 32))},
	}
	rbacCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: settings.RBACConfigMapName, Namespace: ns},
		Data:       map[string]string{settings.KeyPolicyCSV: policyCSV},
	}
	client := k8sfake.NewSimpleClientset(secret, rbacCM)
	s := NewServer(ServerOptions{KubeClientset: client, Namespace: ns, AuthMode: AuthModeServer})
	comps, err := s.setupAuth(context.Background())
	require.NoError(t, err)
	s.auth = comps
	return s
}

func TestAuthorizeStreamDeniesUnpermitted(t *testing.T) {
	s := authedServer(t, "") // empty policy: nobody allowed
	ctx := auth.ContextWithClaims(context.Background(), jwt.MapClaims{"sub": "alice"})
	err := s.authorizeStream(ctx, rbac.ActionGet, "prod/web")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestAuthorizeStreamAllowsPermitted(t *testing.T) {
	s := authedServer(t, "g, alice, role:readonly") // readonly grants get on everything
	ctx := auth.ContextWithClaims(context.Background(), jwt.MapClaims{"sub": "alice"})
	err := s.authorizeStream(ctx, rbac.ActionGet, "prod/web")
	assert.NoError(t, err)
}

func TestAuthorizeStreamNilAuthIsNoop(t *testing.T) {
	s := NewServer(ServerOptions{}) // auth disabled (s.auth nil)
	err := s.authorizeStream(context.Background(), rbac.ActionGet, "prod/web")
	assert.NoError(t, err, "auth disabled => no authorization enforced")
}

// TestWatchRolloutInfoDeniedBeforeWork exercises the REAL WatchRolloutInfo handler:
// a denied caller must be rejected before the handler touches any controller/client.
func TestWatchRolloutInfoDeniedBeforeWork(t *testing.T) {
	s := authedServer(t, "") // empty policy: alice denied
	ctx := auth.ContextWithClaims(context.Background(), jwt.MapClaims{"sub": "alice"})
	stream := &fakeWatchStream{ctx: ctx}
	err := s.WatchRolloutInfo(&rollout.RolloutInfoQuery{Namespace: "prod", Name: "web"}, stream)
	assert.Equal(t, codes.PermissionDenied, status.Code(err),
		"unauthorized watch must be denied at the handler, not served")
}
