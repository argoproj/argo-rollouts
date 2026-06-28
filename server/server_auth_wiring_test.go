package server

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGRPCServerWithAuth(t *testing.T) {
	s := authedServer(t, "")
	grpcServer := s.newGRPCServer()
	require.NotNil(t, grpcServer, "gRPC server must be built with auth interceptors chained")
}

func TestNewHTTPServerMountsAuthRoutesUnderTLS(t *testing.T) {
	s := authedServer(t, "g, admin, role:admin")
	// A TLS config triggers the verified loopback dial path for the gateway.
	cert, err := generateSelfSignedCert()
	require.NoError(t, err)
	s.tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}}

	httpServer := s.newHTTPServer(context.Background(), 3100)
	require.NotNil(t, httpServer)

	// Login is mounted (GET is rejected by the handler, proving the route exists
	// rather than falling through to the static 404).
	rec := httptest.NewRecorder()
	httpServer.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/login", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)

	// Logout is mounted and clears the session cookie.
	rec = httptest.NewRecorder()
	httpServer.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/logout", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}
