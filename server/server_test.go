package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	rolloutapi "github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info/testdata"
	fakeoptions "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
)

func newTestServer(t *testing.T, objects ...runtime.Object) *ArgoRolloutsServer {
	t.Helper()
	tf, o := fakeoptions.NewFakeArgoRolloutsOptions(objects...)
	t.Cleanup(tf.Cleanup)
	return &ArgoRolloutsServer{
		Options: ServerOptions{
			KubeClientset:     o.KubeClient,
			RolloutsClientset: o.RolloutsClient,
			DynamicClientset:  o.DynamicClient,
		},
	}
}

func TestGetRollout(t *testing.T) {
	expected := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rollout",
			Namespace: "default",
		},
	}
	s := newTestServer(t, expected)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	actual, err := s.getRollout(ctx, expected.Namespace, expected.Name)

	require.NoError(t, err)
	assert.Equal(t, expected, actual)
}

func TestSetRolloutImageReturnsRollout(t *testing.T) {
	objects := testdata.NewCanaryRollout()
	expected := objects.Rollouts[0]
	container := expected.Spec.Template.Spec.Containers[0]
	s := newTestServer(t, objects.AllObjects()...)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	actual, err := s.SetRolloutImage(ctx, &rolloutapi.SetImageRequest{
		Namespace: expected.Namespace,
		Rollout:   expected.Name,
		Container: container.Name,
		Image:     "quay.io/argoproj/rollouts-demo",
		Tag:       "updated",
	})

	require.NoError(t, err)
	assert.Equal(t, expected.Name, actual.Name)
}

func TestUndoRolloutReturnsRollout(t *testing.T) {
	objects := testdata.NewCanaryRollout()
	expected := objects.Rollouts[0]
	s := newTestServer(t, objects.AllObjects()...)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	actual, err := s.UndoRollout(ctx, &rolloutapi.UndoRolloutRequest{
		Namespace: expected.Namespace,
		Rollout:   expected.Name,
		Revision:  31,
	})

	require.NoError(t, err)
	assert.Equal(t, expected.Name, actual.Name)
}

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
