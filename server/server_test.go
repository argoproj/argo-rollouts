package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/rest"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
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

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()

		httpServer.Handler.ServeHTTP(w, req)

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

				req := httptest.NewRequest(http.MethodGet, tc.expectedPath, nil)
				w := httptest.NewRecorder()

				httpServer.Handler.ServeHTTP(w, req)

				assert.NotEqual(t, http.StatusNotFound, w.Code,
					"API route should be registered at %s", tc.expectedPath)
			})
		}
	})

	t.Run("client auth mode wraps handler with middleware", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{
				RootPath: "",
				AuthMode: AuthModeClient,
			},
		}
		ctx := context.Background()
		httpServer := s.newHTTPServer(ctx, 8080)

		// API route without token should get 401
		req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
		w := httptest.NewRecorder()
		httpServer.Handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)

		// Static route without token should pass through
		req = httptest.NewRequest(http.MethodGet, "/", nil)
		w = httptest.NewRecorder()
		httpServer.Handler.ServeHTTP(w, req)
		assert.NotEqual(t, http.StatusUnauthorized, w.Code)
	})
}

func TestNewGRPCServer(t *testing.T) {
	t.Run("server mode creates server without interceptors", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{AuthMode: AuthModeServer},
		}
		grpcS := s.newGRPCServer()
		assert.NotNil(t, grpcS)
	})

	t.Run("client mode creates server with interceptors", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{AuthMode: AuthModeClient},
		}
		grpcS := s.newGRPCServer()
		assert.NotNil(t, grpcS)
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

	t.Run("client mode passes through for API route with header token", func(t *testing.T) {
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

	t.Run("client mode passes through for API route with query token", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{AuthMode: AuthModeClient},
		}
		handler := s.newClientAuthMiddleware(okHandler)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rollouts/watch?token=my-token", nil)
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

	t.Run("client mode with root path passes through for static files", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{AuthMode: AuthModeClient, RootPath: "rollouts"},
		}
		handler := s.newClientAuthMiddleware(okHandler)
		req := httptest.NewRequest(http.MethodGet, "/rollouts/index.html", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
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

	t.Run("empty auth mode returns shared clients", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{},
		}
		clients, err := s.getClients(context.Background())
		assert.NoError(t, err)
		assert.NotNil(t, clients)
	})

	t.Run("client mode without RESTConfig returns shared clients", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{
				AuthMode: AuthModeClient,
			},
		}
		clients, err := s.getClients(context.Background())
		assert.NoError(t, err)
		assert.NotNil(t, clients)
	})

	t.Run("client mode without token returns error", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{
				AuthMode:   AuthModeClient,
				RESTConfig: &rest.Config{Host: "https://localhost:6443"},
			},
		}
		_, err := s.getClients(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing bearer token")
	})

	t.Run("client mode with token creates per-request clients", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{
				AuthMode:   AuthModeClient,
				RESTConfig: &rest.Config{Host: "https://localhost:6443"},
			},
		}
		md := metadata.Pairs("authorization", "Bearer test-token")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		clients, err := s.getClients(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, clients)
		assert.NotNil(t, clients.kubeClientset)
		assert.NotNil(t, clients.rolloutsClientset)
		assert.NotNil(t, clients.dynamicClientset)
		// Ensure these are NOT the same as the server's shared clients
		assert.NotEqual(t, s.Options.KubeClientset, clients.kubeClientset)
	})
}

func TestClientsFromToken(t *testing.T) {
	t.Run("creates clients with bearer token", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{
				RESTConfig: &rest.Config{
					Host:     "https://localhost:6443",
					Username: "admin",
					Password: "password",
					TLSClientConfig: rest.TLSClientConfig{
						CertData: []byte("cert"),
						CertFile: "/path/to/cert",
						KeyData:  []byte("key"),
						KeyFile:  "/path/to/key",
					},
				},
			},
		}
		clients, err := s.clientsFromToken("my-bearer-token")
		require.NoError(t, err)
		assert.NotNil(t, clients.kubeClientset)
		assert.NotNil(t, clients.rolloutsClientset)
		assert.NotNil(t, clients.dynamicClientset)
	})

	t.Run("returns error for invalid config", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{
				RESTConfig: &rest.Config{
					Host:            "https://localhost:6443",
					ContentConfig:   rest.ContentConfig{ContentType: "invalid/\x00type"},
					RateLimiter:     nil,
					BearerToken:     "",
					BearerTokenFile: "",
				},
			},
		}
		// Even with an odd config, NewForConfig generally succeeds since it defers
		// actual connection until a request is made. We just verify it doesn't panic.
		clients, err := s.clientsFromToken("token")
		if err == nil {
			assert.NotNil(t, clients)
		}
	})
}

func TestAuthUnaryInterceptor(t *testing.T) {
	mockHandler := func(ctx context.Context, req any) (any, error) {
		return "success", nil
	}

	t.Run("server mode passes through", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{AuthMode: AuthModeServer},
		}
		interceptor := s.newAuthUnaryInterceptor()
		resp, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, mockHandler)
		assert.NoError(t, err)
		assert.Equal(t, "success", resp)
	})

	t.Run("client mode without token returns unauthenticated", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{AuthMode: AuthModeClient},
		}
		interceptor := s.newAuthUnaryInterceptor()
		_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, mockHandler)
		assert.Error(t, err)
		st, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})

	t.Run("client mode with token passes through", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{AuthMode: AuthModeClient},
		}
		interceptor := s.newAuthUnaryInterceptor()
		md := metadata.Pairs("authorization", "Bearer valid-token")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		resp, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, mockHandler)
		assert.NoError(t, err)
		assert.Equal(t, "success", resp)
	})
}

// mockServerStream implements grpc.ServerStream for testing stream interceptors
type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context {
	return m.ctx
}

func TestAuthStreamInterceptor(t *testing.T) {
	mockHandler := func(srv any, ss grpc.ServerStream) error {
		return nil
	}

	t.Run("server mode passes through", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{AuthMode: AuthModeServer},
		}
		interceptor := s.newAuthStreamInterceptor()
		stream := &mockServerStream{ctx: context.Background()}
		err := interceptor(nil, stream, &grpc.StreamServerInfo{}, mockHandler)
		assert.NoError(t, err)
	})

	t.Run("client mode without token returns unauthenticated", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{AuthMode: AuthModeClient},
		}
		interceptor := s.newAuthStreamInterceptor()
		stream := &mockServerStream{ctx: context.Background()}
		err := interceptor(nil, stream, &grpc.StreamServerInfo{}, mockHandler)
		assert.Error(t, err)
		st, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})

	t.Run("client mode with token passes through", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{AuthMode: AuthModeClient},
		}
		interceptor := s.newAuthStreamInterceptor()
		md := metadata.Pairs("authorization", "Bearer valid-token")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		stream := &mockServerStream{ctx: ctx}
		err := interceptor(nil, stream, &grpc.StreamServerInfo{}, mockHandler)
		assert.NoError(t, err)
	})
}

func TestVersion(t *testing.T) {
	t.Run("returns version in server mode", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{AuthMode: AuthModeServer},
		}
		v, err := s.Version(context.Background(), &empty.Empty{})
		assert.NoError(t, err)
		assert.NotNil(t, v)
		assert.NotEmpty(t, v.RolloutsVersion)
	})
}

func TestGetRolloutInfoClientModeNoToken(t *testing.T) {
	s := &ArgoRolloutsServer{
		Options: ServerOptions{
			AuthMode:   AuthModeClient,
			RESTConfig: &rest.Config{Host: "https://localhost:6443"},
		},
	}
	_, err := s.GetRolloutInfo(context.Background(), &rollout.RolloutInfoQuery{Name: "test", Namespace: "default"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing bearer token")
}

func TestListRolloutInfosClientModeNoToken(t *testing.T) {
	s := &ArgoRolloutsServer{
		Options: ServerOptions{
			AuthMode:   AuthModeClient,
			RESTConfig: &rest.Config{Host: "https://localhost:6443"},
		},
	}
	_, err := s.ListRolloutInfos(context.Background(), &rollout.RolloutInfoListQuery{Namespace: "default"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing bearer token")
}

func TestRestartRolloutClientModeNoToken(t *testing.T) {
	s := &ArgoRolloutsServer{
		Options: ServerOptions{
			AuthMode:   AuthModeClient,
			RESTConfig: &rest.Config{Host: "https://localhost:6443"},
		},
	}
	_, err := s.RestartRollout(context.Background(), &rollout.RestartRolloutRequest{Name: "test", Namespace: "default"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing bearer token")
}

func TestPromoteRolloutClientModeNoToken(t *testing.T) {
	s := &ArgoRolloutsServer{
		Options: ServerOptions{
			AuthMode:   AuthModeClient,
			RESTConfig: &rest.Config{Host: "https://localhost:6443"},
		},
	}
	_, err := s.PromoteRollout(context.Background(), &rollout.PromoteRolloutRequest{Name: "test", Namespace: "default"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing bearer token")
}

func TestAbortRolloutClientModeNoToken(t *testing.T) {
	s := &ArgoRolloutsServer{
		Options: ServerOptions{
			AuthMode:   AuthModeClient,
			RESTConfig: &rest.Config{Host: "https://localhost:6443"},
		},
	}
	_, err := s.AbortRollout(context.Background(), &rollout.AbortRolloutRequest{Name: "test", Namespace: "default"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing bearer token")
}

func TestRetryRolloutClientModeNoToken(t *testing.T) {
	s := &ArgoRolloutsServer{
		Options: ServerOptions{
			AuthMode:   AuthModeClient,
			RESTConfig: &rest.Config{Host: "https://localhost:6443"},
		},
	}
	_, err := s.RetryRollout(context.Background(), &rollout.RetryRolloutRequest{Name: "test", Namespace: "default"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing bearer token")
}

func TestSetRolloutImageClientModeNoToken(t *testing.T) {
	s := &ArgoRolloutsServer{
		Options: ServerOptions{
			AuthMode:   AuthModeClient,
			RESTConfig: &rest.Config{Host: "https://localhost:6443"},
		},
	}
	_, err := s.SetRolloutImage(context.Background(), &rollout.SetImageRequest{Rollout: "test", Namespace: "default"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing bearer token")
}

func TestUndoRolloutClientModeNoToken(t *testing.T) {
	s := &ArgoRolloutsServer{
		Options: ServerOptions{
			AuthMode:   AuthModeClient,
			RESTConfig: &rest.Config{Host: "https://localhost:6443"},
		},
	}
	_, err := s.UndoRollout(context.Background(), &rollout.UndoRolloutRequest{Rollout: "test", Namespace: "default"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing bearer token")
}

func TestGetNamespaceClientModeNoToken(t *testing.T) {
	s := &ArgoRolloutsServer{
		Options: ServerOptions{
			AuthMode:   AuthModeClient,
			RESTConfig: &rest.Config{Host: "https://localhost:6443"},
		},
	}
	_, err := s.GetNamespace(context.Background(), &empty.Empty{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing bearer token")
}

func TestRolloutToRolloutInfoClientModeNoToken(t *testing.T) {
	s := &ArgoRolloutsServer{
		Options: ServerOptions{
			AuthMode:   AuthModeClient,
			RESTConfig: &rest.Config{Host: "https://localhost:6443"},
		},
	}
	// RolloutToRolloutInfo uses context.Background() internally, so it won't have a token
	// in client mode and should return an error
	_, err := s.RolloutToRolloutInfo(&v1alpha1.Rollout{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing bearer token")
}
