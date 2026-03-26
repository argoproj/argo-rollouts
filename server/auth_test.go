package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"
)

const (
	testAPIVersionPath = "/api/v1/version"
	testValidToken     = "valid-token"
)

// newFakeAuthenticator creates a tokenAuthenticator with a fake Kubernetes client
// that responds to TokenReview requests based on the validTokens map
func newFakeAuthenticator(validTokens map[string]string) *tokenAuthenticator {
	client := kubefake.NewSimpleClientset()
	client.PrependReactor("create", "tokenreviews", func(action kubetesting.Action) (bool, runtime.Object, error) {
		createAction := action.(kubetesting.CreateAction)
		review := createAction.GetObject().(*authenticationv1.TokenReview)
		token := review.Spec.Token

		username, ok := validTokens[token]
		review.Status = authenticationv1.TokenReviewStatus{
			Authenticated: ok,
		}
		if ok {
			review.Status.User = authenticationv1.UserInfo{
				Username: username,
			}
		}
		return true, review, nil
	})

	return newTokenAuthenticator(client)
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected string
	}{
		{"valid bearer token", "Bearer mytoken123", "mytoken123"},
		{"empty string", "", ""},
		{"no bearer prefix", "mytoken123", ""},
		{"basic auth", "Basic dXNlcjpwYXNz", ""},
		{"bearer lowercase", "bearer mytoken", ""},
		{"just Bearer", "Bearer ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBearerToken(tt.header)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTokenFromHTTPRequest(t *testing.T) {
	t.Run("from Authorization header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, testAPIVersionPath, nil)
		req.Header.Set("Authorization", "Bearer header-token")
		assert.Equal(t, "header-token", tokenFromHTTPRequest(req))
	})

	t.Run("from query parameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rollouts/default/info/watch?token=query-token", nil)
		assert.Equal(t, "query-token", tokenFromHTTPRequest(req))
	})

	t.Run("header takes precedence over query param", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, testAPIVersionPath+"?token=query-token", nil)
		req.Header.Set("Authorization", "Bearer header-token")
		assert.Equal(t, "header-token", tokenFromHTTPRequest(req))
	})

	t.Run("no token present", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, testAPIVersionPath, nil)
		assert.Equal(t, "", tokenFromHTTPRequest(req))
	})
}

func TestIsAPIRoute(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{testAPIVersionPath, true},
		{"/api/v1/rollouts/default/info", true},
		{"/rollouts/api/v1/version", true},
		{"/", false},
		{"/rollouts", false},
		{"/static/index.html", false},
		{"/rollouts/rollout/default/myapp", false},
		{"/auth/login", false},
		{"/auth/callback", false},
		{"/rollouts/auth/login", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.expected, isAPIRoute(tt.path))
		})
	}
}

func TestTokenAuthenticator(t *testing.T) {
	validTokens := map[string]string{
		testValidToken: "test-user",
	}
	authenticator := newFakeAuthenticator(validTokens)

	t.Run("valid token authenticates", func(t *testing.T) {
		ok, err := authenticator.authenticate(context.Background(), testValidToken)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("invalid token is rejected", func(t *testing.T) {
		ok, err := authenticator.authenticate(context.Background(), "invalid-token")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("empty token is rejected", func(t *testing.T) {
		ok, err := authenticator.authenticate(context.Background(), "")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("caches results", func(t *testing.T) {
		authenticator := newFakeAuthenticator(validTokens)

		ok, err := authenticator.authenticate(context.Background(), testValidToken)
		require.NoError(t, err)
		assert.True(t, ok)

		ok, err = authenticator.authenticate(context.Background(), testValidToken)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("cache expires", func(t *testing.T) {
		authenticator := newFakeAuthenticator(validTokens)
		authenticator.cacheTTL = 1 * time.Millisecond

		ok, err := authenticator.authenticate(context.Background(), testValidToken)
		require.NoError(t, err)
		assert.True(t, ok)

		time.Sleep(5 * time.Millisecond)

		ok, err = authenticator.authenticate(context.Background(), testValidToken)
		require.NoError(t, err)
		assert.True(t, ok)
	})
}

func TestTokenAuthMiddleware(t *testing.T) {
	validTokens := map[string]string{
		testValidToken: "test-user",
	}
	authenticator := newFakeAuthenticator(validTokens)
	middleware := newTokenAuthMiddleware(authenticator)

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	handler := middleware(okHandler)

	t.Run("valid token allows access", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, testAPIVersionPath, nil)
		req.Header.Set("Authorization", "Bearer "+testValidToken)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "ok", w.Body.String())
	})

	t.Run("missing token returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, testAPIVersionPath, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid token returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, testAPIVersionPath, nil)
		req.Header.Set("Authorization", "Bearer bad-token")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("static file routes skip auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/rollouts", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("root route skips auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("token from query param works", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rollouts/default/info/watch?token="+testValidToken, nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestTokenFromGRPCContext(t *testing.T) {
	t.Run("extracts token from metadata", func(t *testing.T) {
		md := metadata.Pairs("authorization", "Bearer grpc-token")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		assert.Equal(t, "grpc-token", tokenFromGRPCContext(ctx))
	})

	t.Run("returns empty for missing metadata", func(t *testing.T) {
		assert.Equal(t, "", tokenFromGRPCContext(context.Background()))
	})

	t.Run("returns empty for missing authorization", func(t *testing.T) {
		md := metadata.Pairs("other-header", "value")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		assert.Equal(t, "", tokenFromGRPCContext(ctx))
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

func newFakeAuthenticatorWithError() *tokenAuthenticator {
	client := kubefake.NewSimpleClientset()
	client.PrependReactor("create", "tokenreviews", func(action kubetesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("API server unavailable")
	})
	return newTokenAuthenticator(client)
}

func TestNewAuthUnaryInterceptor(t *testing.T) {
	validTokens := map[string]string{
		testValidToken: "test-user",
	}
	authenticator := newFakeAuthenticator(validTokens)
	interceptor := newAuthUnaryInterceptor(authenticator)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	t.Run("valid token passes through", func(t *testing.T) {
		md := metadata.Pairs("authorization", "Bearer "+testValidToken)
		ctx := metadata.NewIncomingContext(context.Background(), md)

		resp, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, handler)
		require.NoError(t, err)
		assert.Equal(t, "response", resp)
	})

	t.Run("missing token returns unauthenticated", func(t *testing.T) {
		ctx := context.Background()

		_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, handler)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})

	t.Run("invalid token returns unauthenticated", func(t *testing.T) {
		md := metadata.Pairs("authorization", "Bearer invalid-token")
		ctx := metadata.NewIncomingContext(context.Background(), md)

		_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, handler)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})

	t.Run("auth error returns internal", func(t *testing.T) {
		errInterceptor := newAuthUnaryInterceptor(newFakeAuthenticatorWithError())

		md := metadata.Pairs("authorization", "Bearer some-token")
		ctx := metadata.NewIncomingContext(context.Background(), md)

		_, err := errInterceptor(ctx, nil, &grpc.UnaryServerInfo{}, handler)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Internal, st.Code())
	})
}

func TestNewAuthStreamInterceptor(t *testing.T) {
	validTokens := map[string]string{
		testValidToken: "test-user",
	}
	authenticator := newFakeAuthenticator(validTokens)
	interceptor := newAuthStreamInterceptor(authenticator)

	handler := func(srv interface{}, stream grpc.ServerStream) error {
		return nil
	}

	t.Run("valid token passes through", func(t *testing.T) {
		md := metadata.Pairs("authorization", "Bearer "+testValidToken)
		ctx := metadata.NewIncomingContext(context.Background(), md)
		stream := &mockServerStream{ctx: ctx}

		err := interceptor(nil, stream, &grpc.StreamServerInfo{}, handler)
		require.NoError(t, err)
	})

	t.Run("missing token returns unauthenticated", func(t *testing.T) {
		stream := &mockServerStream{ctx: context.Background()}

		err := interceptor(nil, stream, &grpc.StreamServerInfo{}, handler)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})

	t.Run("invalid token returns unauthenticated", func(t *testing.T) {
		md := metadata.Pairs("authorization", "Bearer invalid-token")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		stream := &mockServerStream{ctx: ctx}

		err := interceptor(nil, stream, &grpc.StreamServerInfo{}, handler)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})

	t.Run("auth error returns internal", func(t *testing.T) {
		errInterceptor := newAuthStreamInterceptor(newFakeAuthenticatorWithError())

		md := metadata.Pairs("authorization", "Bearer some-token")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		stream := &mockServerStream{ctx: ctx}

		err := errInterceptor(nil, stream, &grpc.StreamServerInfo{}, handler)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Internal, st.Code())
	})
}

func TestTokenAuthenticator_APIError(t *testing.T) {
	authenticator := newFakeAuthenticatorWithError()

	ok, err := authenticator.authenticate(context.Background(), "some-token")
	assert.Error(t, err)
	assert.False(t, ok)
	assert.Contains(t, err.Error(), "API server unavailable")
}

func TestTokenAuthMiddleware_AuthError(t *testing.T) {
	authenticator := newFakeAuthenticatorWithError()
	middleware := newTokenAuthMiddleware(authenticator)

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := middleware(okHandler)

	req := httptest.NewRequest(http.MethodGet, testAPIVersionPath, nil)
	req.Header.Set("Authorization", "Bearer some-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
