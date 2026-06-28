package server

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/argoproj/argo-rollouts/server/auth/settings"
)

func TestCheckServeErr(t *testing.T) {
	s := NewServer(ServerOptions{})
	// nil error and the graceful-shutdown (stopCh nil) path must not panic/exit.
	s.checkServeErr("noop", nil)
	s.checkServeErr("graceful", errors.New("listener closed"))
}

func TestNewHTTPServerMountsOIDCRoutes(t *testing.T) {
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
	require.NotNil(t, comps.oidc)
	s.auth = comps
	cert, err := generateSelfSignedCert()
	require.NoError(t, err)
	s.tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}}

	httpServer := s.newHTTPServer(context.Background(), 3100)
	require.NotNil(t, httpServer)

	// /auth/login is mounted and the OIDC handler redirects to the IdP.
	rec := httptest.NewRecorder()
	httpServer.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	assert.Equal(t, http.StatusFound, rec.Code, "OIDC login route must redirect to the provider")
}
