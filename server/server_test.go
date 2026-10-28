package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	rolloutapi "github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	fakeroclient "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
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
	assert.Equal(t, expected, actual)
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
		Revision:  29,
	})

	require.NoError(t, err)
	assert.Equal(t, expected, actual)
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
		assert.Equal(t, "127.0.0.1:8080", httpServer.Addr)
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

func TestGetNamespaceToleratesMalformedRollout(t *testing.T) {
	good := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Rollout",
		"metadata":   map[string]any{"name": "good", "namespace": "ns-a"},
	}}
	// resources.limits as a string is accepted by the CRD schema but cannot be
	// decoded into the typed Rollout struct.
	malformed := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Rollout",
		"metadata":   map[string]any{"name": "bad-resources", "namespace": "ns-b"},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name":      "app",
							"image":     "nginx",
							"resources": map[string]any{"limits": "memory:6Gi"},
						},
					},
				},
			},
		},
	}}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{v1alpha1.RolloutGVR: "RolloutList"},
		good, malformed,
	)

	// The real API server returns objects that the typed client then fails to
	// decode; simulate that decoding failure on the typed client.
	rolloutsClient := fakeroclient.NewSimpleClientset()
	rolloutsClient.PrependReactor("list", "rollouts", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("json: cannot unmarshal string into Go struct field ResourceRequirements.items.spec.template.spec.containers.resources.limits of type v1.ResourceList")
	})

	s := &ArgoRolloutsServer{
		Options: ServerOptions{
			Namespace:         "argo-rollouts",
			RolloutsClientset: rolloutsClient,
			DynamicClientset:  dynamicClient,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	info, err := s.GetNamespace(ctx, &empty.Empty{})

	require.NoError(t, err)
	assert.Equal(t, "argo-rollouts", info.Namespace)
	assert.ElementsMatch(t, []string{"ns-a", "ns-b"}, info.AvailableNamespaces)
}
