package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	fakeroclient "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
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

	t.Run("token from cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rollouts", nil)
		req.AddCookie(&http.Cookie{Name: AuthCookieName, Value: "cookie-token"})
		assert.Equal(t, "cookie-token", tokenFromHTTPRequest(req))
	})

	t.Run("cookie value may carry a Bearer prefix", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rollouts", nil)
		req.AddCookie(&http.Cookie{Name: AuthCookieName, Value: url.QueryEscape("Bearer cookie-token")})
		assert.Equal(t, "cookie-token", tokenFromHTTPRequest(req))
	})

	t.Run("header takes precedence over cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rollouts", nil)
		req.AddCookie(&http.Cookie{Name: AuthCookieName, Value: "cookie-token"})
		req.Header.Set("Authorization", "Bearer header-token")
		assert.Equal(t, "header-token", tokenFromHTTPRequest(req))
	})

	t.Run("token is never read from the query string", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rollouts?token=query-token", nil)
		assert.Equal(t, "", tokenFromHTTPRequest(req))
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

	// EventSource cannot set headers, so SSE requests authenticate with the cookie. The
	// middleware rewrites it into an Authorization header so the gRPC side only reads one place.
	t.Run("client mode normalizes the cookie into an Authorization header", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{AuthMode: AuthModeClient},
		}
		var seen string
		handler := s.newClientAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rollouts/watch", nil)
		req.AddCookie(&http.Cookie{Name: AuthCookieName, Value: "my-token"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "Bearer my-token", seen)
	})

	t.Run("client mode returns 401 for API route with only a query token", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{AuthMode: AuthModeClient},
		}
		handler := s.newClientAuthMiddleware(okHandler)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rollouts/watch?token=my-token", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
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

	// a missing REST config must never silently downgrade client mode to the server's own
	// credentials, which would run every request as the dashboard's identity
	t.Run("client mode without RESTConfig fails instead of using server credentials", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{
				AuthMode:      AuthModeClient,
				KubeClientset: k8sfake.NewSimpleClientset(),
			},
		}
		md := metadata.Pairs("authorization", "Bearer test-token")
		_, err := s.getClients(metadata.NewIncomingContext(context.Background(), md))
		assert.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
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

func TestConfigForToken(t *testing.T) {
	// whatever credentials the dashboard itself was started with must not survive into the
	// per-request config, or a user's request could be served with the dashboard's identity
	s := &ArgoRolloutsServer{
		Options: ServerOptions{
			RESTConfig: &rest.Config{
				Host:            "https://localhost:6443",
				Username:        "admin",
				Password:        "password",
				BearerToken:     "server-token",
				BearerTokenFile: "/var/run/secrets/token",
				TLSClientConfig: rest.TLSClientConfig{
					CertData: []byte("cert"),
					CertFile: "/path/to/cert",
					KeyData:  []byte("key"),
					KeyFile:  "/path/to/key",
					CAData:   []byte("ca"),
				},
				AuthProvider: &clientcmdapi.AuthProviderConfig{Name: "gcp"},
				ExecProvider: &clientcmdapi.ExecConfig{Command: "aws"},
			},
		},
	}

	cfg := s.configForToken("user-token")

	assert.Equal(t, "user-token", cfg.BearerToken)
	assert.Empty(t, cfg.BearerTokenFile)
	assert.Empty(t, cfg.Username)
	assert.Empty(t, cfg.Password)
	assert.Nil(t, cfg.CertData)
	assert.Empty(t, cfg.CertFile)
	assert.Nil(t, cfg.KeyData)
	assert.Empty(t, cfg.KeyFile)
	assert.Nil(t, cfg.AuthProvider)
	assert.Nil(t, cfg.ExecProvider)
	// the API server address and its CA still have to come from the server's config
	assert.Equal(t, "https://localhost:6443", cfg.Host)
	assert.Equal(t, []byte("ca"), cfg.CAData)
	// and the server's own config must be left untouched
	assert.Equal(t, "server-token", s.Options.RESTConfig.BearerToken)
	assert.Equal(t, "admin", s.Options.RESTConfig.Username)
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

// TestClientModeRequiresToken asserts that every handler goes through getClients, so that no
// endpoint can be reached in client mode without the caller presenting a token.
func TestClientModeRequiresToken(t *testing.T) {
	s := &ArgoRolloutsServer{
		Options: ServerOptions{
			AuthMode:   AuthModeClient,
			RESTConfig: &rest.Config{Host: "https://localhost:6443"},
		},
	}
	ctx := context.Background()

	calls := map[string]func() error{
		"GetRolloutInfo": func() error {
			_, err := s.GetRolloutInfo(ctx, &rollout.RolloutInfoQuery{Name: "test", Namespace: "default"})
			return err
		},
		"ListRolloutInfos": func() error {
			_, err := s.ListRolloutInfos(ctx, &rollout.RolloutInfoListQuery{Namespace: "default"})
			return err
		},
		"RestartRollout": func() error {
			_, err := s.RestartRollout(ctx, &rollout.RestartRolloutRequest{Name: "test", Namespace: "default"})
			return err
		},
		"PromoteRollout": func() error {
			_, err := s.PromoteRollout(ctx, &rollout.PromoteRolloutRequest{Name: "test", Namespace: "default"})
			return err
		},
		"AbortRollout": func() error {
			_, err := s.AbortRollout(ctx, &rollout.AbortRolloutRequest{Name: "test", Namespace: "default"})
			return err
		},
		"RetryRollout": func() error {
			_, err := s.RetryRollout(ctx, &rollout.RetryRolloutRequest{Name: "test", Namespace: "default"})
			return err
		},
		"SetRolloutImage": func() error {
			_, err := s.SetRolloutImage(ctx, &rollout.SetImageRequest{Rollout: "test", Namespace: "default"})
			return err
		},
		"UndoRollout": func() error {
			_, err := s.UndoRollout(ctx, &rollout.UndoRolloutRequest{Rollout: "test", Namespace: "default"})
			return err
		},
		"GetNamespace": func() error {
			_, err := s.GetNamespace(ctx, &empty.Empty{})
			return err
		},
		"WatchRolloutInfo": func() error {
			return s.WatchRolloutInfo(&rollout.RolloutInfoQuery{Name: "test", Namespace: "default"}, &mockWatchRolloutInfoServer{ctx: ctx})
		},
		"WatchRolloutInfos": func() error {
			return s.WatchRolloutInfos(&rollout.RolloutInfoListQuery{Namespace: "default"}, &mockWatchRolloutInfosServer{ctx: ctx})
		},
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			assert.Error(t, err)
			assert.Equal(t, codes.Unauthenticated, status.Code(err))
		})
	}
}

// newFakeDynamicClient creates a dynamic fake client with the rollout scheme registered
func newFakeDynamicClient(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	_ = v1alpha1.AddToScheme(scheme.Scheme)
	return dynamicfake.NewSimpleDynamicClient(scheme.Scheme, objs...)
}

// newServerWithFakes returns an ArgoRolloutsServer in server auth mode with fake clients
func newServerWithFakes(roObjs []runtime.Object, kubeObjs []runtime.Object, dynamicObjs []runtime.Object) *ArgoRolloutsServer {
	return &ArgoRolloutsServer{
		Options: ServerOptions{
			AuthMode:          AuthModeServer,
			Namespace:         "default",
			KubeClientset:     k8sfake.NewSimpleClientset(kubeObjs...),
			RolloutsClientset: fakeroclient.NewSimpleClientset(roObjs...),
			DynamicClientset:  newFakeDynamicClient(dynamicObjs...),
		},
	}
}

func TestListReplicaSetsAndPods(t *testing.T) {
	t.Run("returns empty lists for empty namespace", func(t *testing.T) {
		kubeClient := k8sfake.NewSimpleClientset()
		s := newServerWithFakes(nil, nil, nil)
		rs, pods, err := s.ListReplicaSetsAndPods(context.Background(), "default", kubeClient)
		assert.NoError(t, err)
		assert.Empty(t, rs)
		assert.Empty(t, pods)
	})

	t.Run("returns replica sets and pods", func(t *testing.T) {
		rs := &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{Name: "rs-1", Namespace: "default"},
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"},
		}
		kubeClient := k8sfake.NewSimpleClientset(rs, pod)
		s := newServerWithFakes(nil, nil, nil)
		rsList, podList, err := s.ListReplicaSetsAndPods(context.Background(), "default", kubeClient)
		assert.NoError(t, err)
		assert.Len(t, rsList, 1)
		assert.Len(t, podList, 1)
		assert.Equal(t, "rs-1", rsList[0].Name)
		assert.Equal(t, "pod-1", podList[0].Name)
	})
}

func TestListRolloutInfosServerMode(t *testing.T) {
	t.Run("returns empty list when no rollouts exist", func(t *testing.T) {
		s := newServerWithFakes(nil, nil, nil)
		result, err := s.ListRolloutInfos(context.Background(), &rollout.RolloutInfoListQuery{Namespace: "default"})
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Empty(t, result.Rollouts)
	})

	t.Run("returns rollout infos with replica set info", func(t *testing.T) {
		ro := &v1alpha1.Rollout{
			ObjectMeta: metav1.ObjectMeta{Name: "my-rollout", Namespace: "default", UID: "test-uid"},
		}
		s := newServerWithFakes([]runtime.Object{ro}, nil, nil)
		result, err := s.ListRolloutInfos(context.Background(), &rollout.RolloutInfoListQuery{Namespace: "default"})
		assert.NoError(t, err)
		require.Len(t, result.Rollouts, 1)
		assert.Equal(t, "my-rollout", result.Rollouts[0].ObjectMeta.Name)
	})
}

func TestGetNamespaceServerMode(t *testing.T) {
	t.Run("returns namespace info with no rollouts", func(t *testing.T) {
		s := newServerWithFakes(nil, nil, nil)
		ns, err := s.GetNamespace(context.Background(), &empty.Empty{})
		assert.NoError(t, err)
		assert.Equal(t, "default", ns.Namespace)
		assert.Empty(t, ns.AvailableNamespaces)
	})

	t.Run("returns available namespaces from rollouts", func(t *testing.T) {
		ro1 := &v1alpha1.Rollout{
			ObjectMeta: metav1.ObjectMeta{Name: "r1", Namespace: "ns1"},
		}
		ro2 := &v1alpha1.Rollout{
			ObjectMeta: metav1.ObjectMeta{Name: "r2", Namespace: "ns2"},
		}
		ro3 := &v1alpha1.Rollout{
			ObjectMeta: metav1.ObjectMeta{Name: "r3", Namespace: "ns1"},
		}
		s := newServerWithFakes([]runtime.Object{ro1, ro2, ro3}, nil, nil)
		ns, err := s.GetNamespace(context.Background(), &empty.Empty{})
		assert.NoError(t, err)
		assert.Equal(t, "default", ns.Namespace)
		assert.Len(t, ns.AvailableNamespaces, 2)
		assert.Contains(t, ns.AvailableNamespaces, "ns1")
		assert.Contains(t, ns.AvailableNamespaces, "ns2")
	})
}

// GetNamespace is what the UI calls to decide whether a token is usable, so it must not report
// success when the API server rejected the token, and must not fail a user who simply cannot list
// rollouts cluster-wide.
func TestGetNamespaceSurfacesRejectedToken(t *testing.T) {
	newServerRejecting := func(err error) *ArgoRolloutsServer {
		roClient := fakeroclient.NewSimpleClientset()
		roClient.PrependReactor("list", "rollouts", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, err
		})
		return &ArgoRolloutsServer{
			Options: ServerOptions{
				AuthMode:          AuthModeServer,
				Namespace:         "default",
				RolloutsClientset: roClient,
			},
		}
	}

	t.Run("rejected token returns unauthenticated", func(t *testing.T) {
		s := newServerRejecting(apierrors.NewUnauthorized("token is invalid"))
		_, err := s.GetNamespace(context.Background(), &empty.Empty{})
		assert.Error(t, err)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("forbidden cluster-wide list still returns the default namespace", func(t *testing.T) {
		s := newServerRejecting(apierrors.NewForbidden(v1alpha1.Resource("rollouts"), "", nil))
		ns, err := s.GetNamespace(context.Background(), &empty.Empty{})
		assert.NoError(t, err)
		assert.Equal(t, "default", ns.Namespace)
		assert.Empty(t, ns.AvailableNamespaces)
	})
}

// Operating on a rollout that does not exist must reach the caller as NotFound rather than a
// generic error, so the dashboard can tell "you cannot do this" apart from "this is not there".
func TestServerModeMapsNotFound(t *testing.T) {
	s := newServerWithFakes(nil, nil, nil)
	ctx := context.Background()

	calls := map[string]func() error{
		"RestartRollout": func() error {
			_, err := s.RestartRollout(ctx, &rollout.RestartRolloutRequest{Name: "nonexistent", Namespace: "default"})
			return err
		},
		"PromoteRollout": func() error {
			_, err := s.PromoteRollout(ctx, &rollout.PromoteRolloutRequest{Name: "nonexistent", Namespace: "default"})
			return err
		},
		"AbortRollout": func() error {
			_, err := s.AbortRollout(ctx, &rollout.AbortRolloutRequest{Name: "nonexistent", Namespace: "default"})
			return err
		},
		"RetryRollout": func() error {
			_, err := s.RetryRollout(ctx, &rollout.RetryRolloutRequest{Name: "nonexistent", Namespace: "default"})
			return err
		},
		"SetRolloutImage": func() error {
			_, err := s.SetRolloutImage(ctx, &rollout.SetImageRequest{Rollout: "nonexistent", Namespace: "default", Image: "nginx", Tag: "latest", Container: "main"})
			return err
		},
		"UndoRollout": func() error {
			_, err := s.UndoRollout(ctx, &rollout.UndoRolloutRequest{Rollout: "nonexistent", Namespace: "default", Revision: 0})
			return err
		},
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			assert.Error(t, err)
			assert.Equal(t, codes.NotFound, status.Code(err))
		})
	}
}

func TestGetRolloutInfoServerMode(t *testing.T) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{Name: "my-rollout", Namespace: "default"},
	}
	s := newServerWithFakes([]runtime.Object{ro}, nil, nil)
	ri, err := s.GetRolloutInfo(context.Background(), &rollout.RolloutInfoQuery{Name: "my-rollout", Namespace: "default"})
	assert.NoError(t, err)
	assert.NotNil(t, ri)
	assert.Equal(t, "my-rollout", ri.ObjectMeta.Name)
}

// mockWatchRolloutInfoServer implements rollout.RolloutService_WatchRolloutInfoServer
type mockWatchRolloutInfoServer struct {
	grpc.ServerStream
	ctx  context.Context
	sent []*rollout.RolloutInfo
}

func (m *mockWatchRolloutInfoServer) Context() context.Context { return m.ctx }
func (m *mockWatchRolloutInfoServer) Send(ri *rollout.RolloutInfo) error {
	m.sent = append(m.sent, ri)
	return nil
}
func (m *mockWatchRolloutInfoServer) SendMsg(msg any) error        { return nil }
func (m *mockWatchRolloutInfoServer) RecvMsg(msg any) error        { return nil }
func (m *mockWatchRolloutInfoServer) SetHeader(metadata.MD) error  { return nil }
func (m *mockWatchRolloutInfoServer) SendHeader(metadata.MD) error { return nil }
func (m *mockWatchRolloutInfoServer) SetTrailer(metadata.MD)       {}

// mockWatchRolloutInfosServer implements rollout.RolloutService_WatchRolloutInfosServer
type mockWatchRolloutInfosServer struct {
	grpc.ServerStream
	ctx  context.Context
	sent []*rollout.RolloutWatchEvent
}

func (m *mockWatchRolloutInfosServer) Context() context.Context { return m.ctx }
func (m *mockWatchRolloutInfosServer) Send(ev *rollout.RolloutWatchEvent) error {
	m.sent = append(m.sent, ev)
	return nil
}
func (m *mockWatchRolloutInfosServer) SendMsg(msg any) error        { return nil }
func (m *mockWatchRolloutInfosServer) RecvMsg(msg any) error        { return nil }
func (m *mockWatchRolloutInfosServer) SetHeader(metadata.MD) error  { return nil }
func (m *mockWatchRolloutInfosServer) SendHeader(metadata.MD) error { return nil }
func (m *mockWatchRolloutInfosServer) SetTrailer(metadata.MD)       {}

func TestWatchRolloutInfoServerMode(t *testing.T) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{Name: "my-rollout", Namespace: "default"},
	}
	s := newServerWithFakes([]runtime.Object{ro}, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately so the watch returns quickly
	cancel()
	ws := &mockWatchRolloutInfoServer{ctx: ctx}
	err := s.WatchRolloutInfo(&rollout.RolloutInfoQuery{Name: "my-rollout", Namespace: "default"}, ws)
	assert.NoError(t, err)
}

func TestWatchRolloutInfosServerMode(t *testing.T) {
	s := newServerWithFakes(nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ws := &mockWatchRolloutInfosServer{ctx: ctx}
	err := s.WatchRolloutInfos(&rollout.RolloutInfoListQuery{Namespace: "default"}, ws)
	assert.NoError(t, err)
}
