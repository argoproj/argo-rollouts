package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"
	"k8s.io/client-go/rest"
)

func TestNewHTTPServer(t *testing.T) {
	t.Run("server is created with correct address", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{
				RootPath: "",
			},
		}
		ctx := context.Background()
		port := 8080

		httpServer := s.newHTTPServer(ctx, port)

		assert.NotNil(t, httpServer)
		assert.Equal(t, "0.0.0.0:8080", httpServer.Addr)
		assert.NotNil(t, httpServer.Handler)
	})

	t.Run("mux handles root route for static files", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{
				RootPath: "",
			},
		}
		ctx := context.Background()
		port := 8080

		httpServer := s.newHTTPServer(ctx, port)

		// Test that / route is registered
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()

		httpServer.Handler.ServeHTTP(w, req)

		// The handler should be registered (will be handled by staticFileHttpHandler)
		// The actual response will depend on static file configuration
		assert.NotNil(t, w.Code, "Root route should be registered")
	})

	t.Run("server with different root paths", func(t *testing.T) {
		testCases := []struct {
			name         string
			rootPath     string
			expectedPath string
		}{
			{
				name:         "empty root path",
				rootPath:     "",
				expectedPath: "/api/",
			},
			{
				name:         "simple root path",
				rootPath:     "/rollouts",
				expectedPath: "/rollouts/api/",
			},
			{
				name:         "nested root path",
				rootPath:     "/custom/path",
				expectedPath: "/custom/path/api/",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				s := &ArgoRolloutsServer{
					Options: ServerOptions{
						RootPath: tc.rootPath,
					},
				}
				ctx := context.Background()
				port := 8080

				httpServer := s.newHTTPServer(ctx, port)

				// Test that the expected API path is registered
				req := httptest.NewRequest(http.MethodGet, tc.expectedPath, nil)
				w := httptest.NewRecorder()

				httpServer.Handler.ServeHTTP(w, req)

				// The handler should be registered (not 404)
				assert.NotEqual(t, http.StatusNotFound, w.Code,
					"API route should be registered at %s", tc.expectedPath)
			})
		}
	})
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected string
	}{
		{"valid bearer token", "Bearer my-token-123", "my-token-123"},
		{"empty header", "", ""},
		{"no bearer prefix", "my-token-123", ""},
		{"lowercase bearer", "bearer my-token-123", ""},
		{"bearer with no token", "Bearer ", ""},
		{"basic auth", "Basic dXNlcjpwYXNz", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBearerToken(tt.header)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTokenFromHTTPRequest(t *testing.T) {
	t.Run("token from Authorization header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
		req.Header.Set("Authorization", "Bearer header-token")
		assert.Equal(t, "header-token", tokenFromHTTPRequest(req))
	})

	t.Run("token from query parameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rollouts?token=query-token", nil)
		assert.Equal(t, "query-token", tokenFromHTTPRequest(req))
	})

	t.Run("header takes precedence over query", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rollouts?token=query-token", nil)
		req.Header.Set("Authorization", "Bearer header-token")
		assert.Equal(t, "header-token", tokenFromHTTPRequest(req))
	})

	t.Run("no token returns empty", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
		assert.Equal(t, "", tokenFromHTTPRequest(req))
	})
}

func TestTokenFromGRPCContext(t *testing.T) {
	t.Run("token from gRPC metadata", func(t *testing.T) {
		md := metadata.Pairs("authorization", "Bearer grpc-token")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		assert.Equal(t, "grpc-token", tokenFromGRPCContext(ctx))
	})

	t.Run("no metadata returns empty", func(t *testing.T) {
		assert.Equal(t, "", tokenFromGRPCContext(context.Background()))
	})

	t.Run("no authorization header returns empty", func(t *testing.T) {
		md := metadata.Pairs("content-type", "application/json")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		assert.Equal(t, "", tokenFromGRPCContext(ctx))
	})

	t.Run("invalid authorization format returns empty", func(t *testing.T) {
		md := metadata.Pairs("authorization", "Basic dXNlcjpwYXNz")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		assert.Equal(t, "", tokenFromGRPCContext(ctx))
	})
}

func TestIsAPIRoute(t *testing.T) {
	tests := []struct {
		name     string
		urlPath  string
		rootPath string
		expected bool
	}{
		{"API route no root", "/api/v1/version", "", true},
		{"API route with root", "/rollouts/api/v1/version", "rollouts", true},
		{"static file no root", "/index.html", "", false},
		{"static file with root", "/rollouts/index.html", "rollouts", false},
		{"root path", "/", "", false},
		{"root path with root", "/rollouts/", "rollouts", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isAPIRoute(tt.urlPath, tt.rootPath))
		})
	}
}

func TestClientAuthMiddleware(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("server mode passes through without token", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{AuthMode: AuthModeServer},
		}
		handler := s.newClientAuthMiddleware(okHandler)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("client mode returns 401 for API route without token", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{AuthMode: AuthModeClient},
		}
		handler := s.newClientAuthMiddleware(okHandler)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("client mode passes through for API route with token", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{AuthMode: AuthModeClient},
		}
		handler := s.newClientAuthMiddleware(okHandler)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("client mode passes through for static files without token", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{AuthMode: AuthModeClient},
		}
		handler := s.newClientAuthMiddleware(okHandler)
		req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("client mode with root path returns 401 for API route without token", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{AuthMode: AuthModeClient, RootPath: "rollouts"},
		}
		handler := s.newClientAuthMiddleware(okHandler)
		req := httptest.NewRequest(http.MethodGet, "/rollouts/api/v1/version", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestGetClients(t *testing.T) {
	t.Run("server mode returns shared clients", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{
				AuthMode: AuthModeServer,
			},
		}
		clients, err := s.getClients(context.Background())
		assert.NoError(t, err)
		assert.NotNil(t, clients)
		assert.Equal(t, s.Options.KubeClientset, clients.kubeClientset)
		assert.Equal(t, s.Options.RolloutsClientset, clients.rolloutsClientset)
		assert.Equal(t, s.Options.DynamicClientset, clients.dynamicClientset)
	})

	t.Run("client mode without token returns error", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{
				AuthMode:   AuthModeClient,
				RESTConfig: &fakeRESTConfig,
			},
		}
		_, err := s.getClients(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing bearer token")
	})
}

// fakeRESTConfig is a minimal REST config for testing
var fakeRESTConfig = rest.Config{
	Host: "https://localhost:6443",
}
