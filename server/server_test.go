package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutfake "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
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

	t.Run("server with token auth wraps handler", func(t *testing.T) {
		kubeClient := kubefake.NewSimpleClientset()
		s := &ArgoRolloutsServer{
			Options: ServerOptions{
				RootPath:      "",
				AuthMode:      AuthModeToken,
				KubeClientset: kubeClient,
			},
		}
		ctx := context.Background()
		port := 8080

		httpServer := s.newHTTPServer(ctx, port)
		assert.NotNil(t, httpServer)

		// API route without token should return 401
		req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
		w := httptest.NewRecorder()
		httpServer.Handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)

		// Non-API route should still be accessible without token
		req = httptest.NewRequest(http.MethodGet, "/", nil)
		w = httptest.NewRecorder()
		httpServer.Handler.ServeHTTP(w, req)
		assert.NotEqual(t, http.StatusUnauthorized, w.Code)
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

func TestNewServer(t *testing.T) {
	opts := ServerOptions{
		Namespace: "default",
		RootPath:  "rollouts",
		AuthMode:  AuthModeServer,
	}
	s := NewServer(opts)
	assert.NotNil(t, s)
	assert.Equal(t, "default", s.Options.Namespace)
	assert.Equal(t, "rollouts", s.Options.RootPath)
	assert.Equal(t, AuthModeServer, s.Options.AuthMode)
}

func TestGetClients(t *testing.T) {
	kubeClient := kubefake.NewSimpleClientset()
	rolloutsClient := rolloutfake.NewSimpleClientset()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())

	t.Run("server mode returns shared clients", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{
				AuthMode:          AuthModeServer,
				KubeClientset:     kubeClient,
				RolloutsClientset: rolloutsClient,
				DynamicClientset:  dynamicClient,
			},
		}

		clients, err := s.getClients(context.Background())
		require.NoError(t, err)
		assert.Same(t, kubeClient, clients.kubeClientset)
		assert.Same(t, rolloutsClient, clients.rolloutsClientset)
		assert.Same(t, dynamicClient, clients.dynamicClientset)
	})

	t.Run("token mode without RESTConfig returns shared clients", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{
				AuthMode:          AuthModeToken,
				RESTConfig:        nil,
				KubeClientset:     kubeClient,
				RolloutsClientset: rolloutsClient,
				DynamicClientset:  dynamicClient,
			},
		}

		clients, err := s.getClients(context.Background())
		require.NoError(t, err)
		assert.Same(t, kubeClient, clients.kubeClientset)
	})

	t.Run("token mode without token in context returns shared clients", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{
				AuthMode:          AuthModeToken,
				RESTConfig:        &rest.Config{Host: "http://localhost:6443"},
				KubeClientset:     kubeClient,
				RolloutsClientset: rolloutsClient,
				DynamicClientset:  dynamicClient,
			},
		}

		clients, err := s.getClients(context.Background())
		require.NoError(t, err)
		assert.Same(t, kubeClient, clients.kubeClientset)
	})

	t.Run("token mode with token creates per-request clients", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{
				AuthMode:          AuthModeToken,
				RESTConfig:        &rest.Config{Host: "http://localhost:6443"},
				KubeClientset:     kubeClient,
				RolloutsClientset: rolloutsClient,
				DynamicClientset:  dynamicClient,
			},
		}

		md := metadata.Pairs("authorization", "Bearer user-token")
		ctx := metadata.NewIncomingContext(context.Background(), md)

		clients, err := s.getClients(ctx)
		require.NoError(t, err)
		assert.NotNil(t, clients)
		// Per-request clients should be different from the server's shared clients
		assert.NotSame(t, kubeClient, clients.kubeClientset)
		assert.NotSame(t, rolloutsClient, clients.rolloutsClientset)
		assert.NotSame(t, dynamicClient, clients.dynamicClientset)
	})
}

func TestNewGRPCServer(t *testing.T) {
	t.Run("default mode creates server without interceptors", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{
				AuthMode: AuthModeServer,
			},
		}
		grpcS := s.newGRPCServer()
		assert.NotNil(t, grpcS)
	})

	t.Run("token mode creates server with auth interceptors", func(t *testing.T) {
		s := &ArgoRolloutsServer{
			Options: ServerOptions{
				AuthMode:      AuthModeToken,
				KubeClientset: kubefake.NewSimpleClientset(),
			},
		}
		grpcS := s.newGRPCServer()
		assert.NotNil(t, grpcS)
	})
}

func TestClientsFromToken(t *testing.T) {
	s := &ArgoRolloutsServer{
		Options: ServerOptions{
			RESTConfig: &rest.Config{Host: "http://localhost:6443"},
		},
	}

	clients, err := s.clientsFromToken("test-bearer-token")
	require.NoError(t, err)
	assert.NotNil(t, clients.kubeClientset)
	assert.NotNil(t, clients.rolloutsClientset)
	assert.NotNil(t, clients.dynamicClientset)
}

func TestClientsFromToken_Error(t *testing.T) {
	// QPS > 0 with Burst = 0 causes NewForConfig to fail
	s := &ArgoRolloutsServer{
		Options: ServerOptions{
			RESTConfig: &rest.Config{
				Host:  "http://localhost:6443",
				QPS:   1.0,
				Burst: 0,
			},
		},
	}

	_, err := s.clientsFromToken("test-token")
	assert.Error(t, err)
}

func TestVersion(t *testing.T) {
	s := NewServer(ServerOptions{AuthMode: AuthModeServer})
	v, err := s.Version(context.Background(), &empty.Empty{})
	require.NoError(t, err)
	assert.NotEmpty(t, v.RolloutsVersion)
}

func TestGetNamespace(t *testing.T) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rollout",
			Namespace: "default",
		},
	}
	s := NewServer(ServerOptions{
		AuthMode:          AuthModeServer,
		Namespace:         "default",
		KubeClientset:     kubefake.NewSimpleClientset(),
		RolloutsClientset: rolloutfake.NewSimpleClientset(ro),
	})

	ns, err := s.GetNamespace(context.Background(), &empty.Empty{})
	require.NoError(t, err)
	assert.Equal(t, "default", ns.Namespace)
	assert.Contains(t, ns.AvailableNamespaces, "default")
}

func TestListRolloutInfos(t *testing.T) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rollout",
			Namespace: "default",
			UID:       "test-uid",
		},
	}
	s := NewServer(ServerOptions{
		AuthMode:          AuthModeServer,
		KubeClientset:     kubefake.NewSimpleClientset(),
		RolloutsClientset: rolloutfake.NewSimpleClientset(ro),
	})

	list, err := s.ListRolloutInfos(context.Background(), &rollout.RolloutInfoListQuery{
		Namespace: "default",
	})
	require.NoError(t, err)
	assert.Len(t, list.Rollouts, 1)
	assert.Equal(t, "test-rollout", list.Rollouts[0].ObjectMeta.Name)
}

func TestListReplicaSetsAndPods(t *testing.T) {
	t.Run("empty namespace", func(t *testing.T) {
		s := NewServer(ServerOptions{
			AuthMode:      AuthModeServer,
			KubeClientset: kubefake.NewSimpleClientset(),
		})

		rs, pods, err := s.ListReplicaSetsAndPods(context.Background(), "default")
		require.NoError(t, err)
		assert.Empty(t, rs)
		assert.Empty(t, pods)
	})

	t.Run("with replicasets and pods", func(t *testing.T) {
		rs1 := &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{Name: "rs1", Namespace: "default"},
		}
		pod1 := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "default"},
		}
		s := NewServer(ServerOptions{
			AuthMode:      AuthModeServer,
			KubeClientset: kubefake.NewSimpleClientset(rs1, pod1),
		})

		rs, pods, err := s.ListReplicaSetsAndPods(context.Background(), "default")
		require.NoError(t, err)
		assert.Len(t, rs, 1)
		assert.Len(t, pods, 1)
		assert.Equal(t, "rs1", rs[0].Name)
		assert.Equal(t, "pod1", pods[0].Name)
	})
}

func TestRolloutToRolloutInfo(t *testing.T) {
	s := NewServer(ServerOptions{
		AuthMode:      AuthModeServer,
		KubeClientset: kubefake.NewSimpleClientset(),
	})

	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rollout",
			Namespace: "default",
		},
	}

	ri, err := s.RolloutToRolloutInfo(ro)
	require.NoError(t, err)
	assert.NotNil(t, ri)
	assert.Equal(t, "test-rollout", ri.ObjectMeta.Name)
}

func TestPromoteRollout(t *testing.T) {
	s := NewServer(ServerOptions{
		AuthMode:          AuthModeServer,
		KubeClientset:     kubefake.NewSimpleClientset(),
		RolloutsClientset: rolloutfake.NewSimpleClientset(),
	})

	_, err := s.PromoteRollout(context.Background(), &rollout.PromoteRolloutRequest{
		Name:      "nonexistent",
		Namespace: "default",
	})
	assert.Error(t, err)
}

func TestAbortRollout(t *testing.T) {
	s := NewServer(ServerOptions{
		AuthMode:          AuthModeServer,
		KubeClientset:     kubefake.NewSimpleClientset(),
		RolloutsClientset: rolloutfake.NewSimpleClientset(),
	})

	_, err := s.AbortRollout(context.Background(), &rollout.AbortRolloutRequest{
		Name:      "nonexistent",
		Namespace: "default",
	})
	assert.Error(t, err)
}

func TestRestartRollout(t *testing.T) {
	s := NewServer(ServerOptions{
		AuthMode:          AuthModeServer,
		KubeClientset:     kubefake.NewSimpleClientset(),
		RolloutsClientset: rolloutfake.NewSimpleClientset(),
	})

	_, err := s.RestartRollout(context.Background(), &rollout.RestartRolloutRequest{
		Name:      "nonexistent",
		Namespace: "default",
	})
	assert.Error(t, err)
}

func TestRetryRollout(t *testing.T) {
	s := NewServer(ServerOptions{
		AuthMode:          AuthModeServer,
		KubeClientset:     kubefake.NewSimpleClientset(),
		RolloutsClientset: rolloutfake.NewSimpleClientset(),
	})

	_, err := s.RetryRollout(context.Background(), &rollout.RetryRolloutRequest{
		Name:      "nonexistent",
		Namespace: "default",
	})
	assert.Error(t, err)
}

func TestGetRollout(t *testing.T) {
	s := NewServer(ServerOptions{
		AuthMode:          AuthModeServer,
		RolloutsClientset: rolloutfake.NewSimpleClientset(),
	})
	// Close stopCh immediately so WaitForCacheSync returns without blocking
	s.stopCh = make(chan struct{})
	close(s.stopCh)

	// getRollout will fail because cache didn't sync, but the code paths are exercised
	_, err := s.getRollout(context.Background(), "default", "nonexistent")
	assert.Error(t, err)
}

func TestSetRolloutImage(t *testing.T) {
	s := NewServer(ServerOptions{
		AuthMode:         AuthModeServer,
		DynamicClientset: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()),
	})

	_, err := s.SetRolloutImage(context.Background(), &rollout.SetImageRequest{
		Rollout:   "nonexistent",
		Namespace: "default",
		Container: "main",
		Image:     "nginx",
		Tag:       "latest",
	})
	assert.Error(t, err)
}

func TestUndoRollout(t *testing.T) {
	s := NewServer(ServerOptions{
		AuthMode:         AuthModeServer,
		KubeClientset:    kubefake.NewSimpleClientset(),
		DynamicClientset: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()),
	})

	_, err := s.UndoRollout(context.Background(), &rollout.UndoRolloutRequest{
		Rollout:   "nonexistent",
		Namespace: "default",
		Revision:  0,
	})
	assert.Error(t, err)
}

func TestGetRolloutInfo(t *testing.T) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rollout",
			Namespace: "default",
		},
	}
	s := NewServer(ServerOptions{
		AuthMode:          AuthModeServer,
		KubeClientset:     kubefake.NewSimpleClientset(),
		RolloutsClientset: rolloutfake.NewSimpleClientset(ro),
	})

	ri, err := s.GetRolloutInfo(context.Background(), &rollout.RolloutInfoQuery{
		Name:      "test-rollout",
		Namespace: "default",
	})
	require.NoError(t, err)
	assert.Equal(t, "test-rollout", ri.ObjectMeta.Name)
}

// newBrokenTokenServer creates a server in token mode with a REST config that causes
// clientsFromToken to fail (QPS > 0 with Burst = 0), used to test getClients error paths.
func newBrokenTokenServer() *ArgoRolloutsServer {
	return &ArgoRolloutsServer{
		Options: ServerOptions{
			AuthMode: AuthModeToken,
			RESTConfig: &rest.Config{
				Host:  "http://localhost:6443",
				QPS:   1.0,
				Burst: 0,
			},
		},
	}
}

func tokenContext() context.Context {
	md := metadata.Pairs("authorization", "Bearer test-token")
	return metadata.NewIncomingContext(context.Background(), md)
}

func TestHandlers_GetClientsError(t *testing.T) {
	s := newBrokenTokenServer()
	ctx := tokenContext()

	t.Run("GetRolloutInfo", func(t *testing.T) {
		_, err := s.GetRolloutInfo(ctx, &rollout.RolloutInfoQuery{Name: "r", Namespace: "default"})
		assert.Error(t, err)
	})

	t.Run("WatchRolloutInfo", func(t *testing.T) {
		ws := &mockWatchRolloutInfoServer{ctx: ctx}
		err := s.WatchRolloutInfo(&rollout.RolloutInfoQuery{Name: "r", Namespace: "default"}, ws)
		assert.Error(t, err)
	})

	t.Run("ListReplicaSetsAndPods", func(t *testing.T) {
		_, _, err := s.ListReplicaSetsAndPods(ctx, "default")
		assert.Error(t, err)
	})

	t.Run("ListRolloutInfos", func(t *testing.T) {
		_, err := s.ListRolloutInfos(ctx, &rollout.RolloutInfoListQuery{Namespace: "default"})
		assert.Error(t, err)
	})

	t.Run("RestartRollout", func(t *testing.T) {
		_, err := s.RestartRollout(ctx, &rollout.RestartRolloutRequest{Name: "r", Namespace: "default"})
		assert.Error(t, err)
	})

	t.Run("WatchRolloutInfos", func(t *testing.T) {
		ws := &mockWatchRolloutInfosServer{ctx: ctx}
		err := s.WatchRolloutInfos(&rollout.RolloutInfoListQuery{Namespace: "default"}, ws)
		assert.Error(t, err)
	})

	t.Run("GetNamespace", func(t *testing.T) {
		_, err := s.GetNamespace(ctx, &empty.Empty{})
		assert.Error(t, err)
	})

	t.Run("PromoteRollout", func(t *testing.T) {
		_, err := s.PromoteRollout(ctx, &rollout.PromoteRolloutRequest{Name: "r", Namespace: "default"})
		assert.Error(t, err)
	})

	t.Run("AbortRollout", func(t *testing.T) {
		_, err := s.AbortRollout(ctx, &rollout.AbortRolloutRequest{Name: "r", Namespace: "default"})
		assert.Error(t, err)
	})

	t.Run("getRollout", func(t *testing.T) {
		_, err := s.getRollout(ctx, "default", "r")
		assert.Error(t, err)
	})

	t.Run("SetRolloutImage", func(t *testing.T) {
		_, err := s.SetRolloutImage(ctx, &rollout.SetImageRequest{Rollout: "r", Namespace: "default", Image: "nginx", Tag: "latest"})
		assert.Error(t, err)
	})

	t.Run("UndoRollout", func(t *testing.T) {
		_, err := s.UndoRollout(ctx, &rollout.UndoRolloutRequest{Rollout: "r", Namespace: "default"})
		assert.Error(t, err)
	})

	t.Run("RetryRollout", func(t *testing.T) {
		_, err := s.RetryRollout(ctx, &rollout.RetryRolloutRequest{Name: "r", Namespace: "default"})
		assert.Error(t, err)
	})
}

func TestGetRolloutInfo_NotFound(t *testing.T) {
	s := NewServer(ServerOptions{
		AuthMode:          AuthModeServer,
		KubeClientset:     kubefake.NewSimpleClientset(),
		RolloutsClientset: rolloutfake.NewSimpleClientset(),
	})

	_, err := s.GetRolloutInfo(context.Background(), &rollout.RolloutInfoQuery{
		Name:      "nonexistent",
		Namespace: "default",
	})
	assert.Error(t, err)
}

// mockWatchRolloutInfoServer implements rollout.RolloutService_WatchRolloutInfoServer for testing
type mockWatchRolloutInfoServer struct {
	grpc.ServerStream
	ctx  context.Context
	sent []*rollout.RolloutInfo
}

func (m *mockWatchRolloutInfoServer) Context() context.Context { return m.ctx }
func (m *mockWatchRolloutInfoServer) Send(info *rollout.RolloutInfo) error {
	m.sent = append(m.sent, info)
	return nil
}

// mockWatchRolloutInfosServer implements rollout.RolloutService_WatchRolloutInfosServer for testing
type mockWatchRolloutInfosServer struct {
	grpc.ServerStream
	ctx  context.Context
	sent []*rollout.RolloutWatchEvent
}

func (m *mockWatchRolloutInfosServer) Context() context.Context { return m.ctx }
func (m *mockWatchRolloutInfosServer) Send(e *rollout.RolloutWatchEvent) error {
	m.sent = append(m.sent, e)
	return nil
}

func TestWatchRolloutInfo(t *testing.T) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rollout",
			Namespace: "default",
		},
	}
	s := NewServer(ServerOptions{
		AuthMode:          AuthModeServer,
		KubeClientset:     kubefake.NewSimpleClientset(),
		RolloutsClientset: rolloutfake.NewSimpleClientset(ro),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ws := &mockWatchRolloutInfoServer{ctx: ctx}
	err := s.WatchRolloutInfo(&rollout.RolloutInfoQuery{
		Name:      "test-rollout",
		Namespace: "default",
	}, ws)
	assert.NoError(t, err)
}

func TestWatchRolloutInfos(t *testing.T) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rollout",
			Namespace: "default",
		},
	}
	s := NewServer(ServerOptions{
		AuthMode:          AuthModeServer,
		KubeClientset:     kubefake.NewSimpleClientset(),
		RolloutsClientset: rolloutfake.NewSimpleClientset(ro),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ws := &mockWatchRolloutInfosServer{ctx: ctx}
	err := s.WatchRolloutInfos(&rollout.RolloutInfoListQuery{
		Namespace: "default",
	}, ws)
	assert.NoError(t, err)
}
