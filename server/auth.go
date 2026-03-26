package server

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// AuthModeServer uses the server's kubeconfig credentials (default, no auth required from users)
	AuthModeServer = "server"
	// AuthModeToken requires a valid Kubernetes bearer token from users
	AuthModeToken = "token"
)

// tokenCacheEntry caches token validation results to avoid excessive TokenReview calls
type tokenCacheEntry struct {
	authenticated bool
	expiresAt     time.Time
}

// tokenAuthenticator validates bearer tokens against the Kubernetes API
type tokenAuthenticator struct {
	kubeClient kubernetes.Interface
	cache      sync.Map
	cacheTTL   time.Duration
}

func newTokenAuthenticator(kubeClient kubernetes.Interface) *tokenAuthenticator {
	return &tokenAuthenticator{
		kubeClient: kubeClient,
		cacheTTL:   1 * time.Minute,
	}
}

// authenticate validates a bearer token using the Kubernetes TokenReview API
func (a *tokenAuthenticator) authenticate(ctx context.Context, token string) (bool, error) {
	if token == "" {
		return false, nil
	}

	// Check cache
	if entry, ok := a.cache.Load(token); ok {
		cached := entry.(tokenCacheEntry)
		if time.Now().Before(cached.expiresAt) {
			return cached.authenticated, nil
		}
		a.cache.Delete(token)
	}

	// Validate via TokenReview
	review := &authenticationv1.TokenReview{
		Spec: authenticationv1.TokenReviewSpec{
			Token: token,
		},
	}

	result, err := a.kubeClient.AuthenticationV1().TokenReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		log.Errorf("TokenReview failed: %v", err)
		return false, err
	}

	authenticated := result.Status.Authenticated

	// Cache result
	a.cache.Store(token, tokenCacheEntry{
		authenticated: authenticated,
		expiresAt:     time.Now().Add(a.cacheTTL),
	})

	if authenticated {
		log.Infof("Authenticated user: %s", result.Status.User.Username)
	}

	return authenticated, nil
}

// extractBearerToken extracts the bearer token from an Authorization header value
func extractBearerToken(authHeader string) string {
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(authHeader, "Bearer ")
}

// tokenFromHTTPRequest extracts a bearer token from an HTTP request.
// It checks the Authorization header first, then falls back to the "token" query parameter
// (needed for EventSource/SSE connections which don't support custom headers).
func tokenFromHTTPRequest(r *http.Request) string {
	token := extractBearerToken(r.Header.Get("Authorization"))
	if token != "" {
		return token
	}
	return r.URL.Query().Get("token")
}

// newTokenAuthMiddleware creates an HTTP middleware that validates bearer tokens.
// It skips authentication for non-API routes (static files, login page).
func newTokenAuthMiddleware(authenticator *tokenAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for non-API routes (static files, login page)
			if !isAPIRoute(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			token := tokenFromHTTPRequest(r)
			if token == "" {
				http.Error(w, "Authorization required", http.StatusUnauthorized)
				return
			}

			authenticated, err := authenticator.authenticate(r.Context(), token)
			if err != nil {
				http.Error(w, "Authentication error", http.StatusInternalServerError)
				return
			}
			if !authenticated {
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isAPIRoute returns true if the path is an API endpoint that requires authentication.
// OIDC auth routes (/auth/login, /auth/callback) are excluded since they handle their own auth flow.
func isAPIRoute(urlPath string) bool {
	if strings.Contains(urlPath, "/auth/") {
		return false
	}
	return strings.Contains(urlPath, "/api/")
}

// tokenFromGRPCContext extracts a bearer token from gRPC metadata
func tokenFromGRPCContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return ""
	}
	return extractBearerToken(values[0])
}

// newAuthUnaryInterceptor creates a gRPC unary interceptor that validates bearer tokens
func newAuthUnaryInterceptor(authenticator *tokenAuthenticator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		token := tokenFromGRPCContext(ctx)
		if token == "" {
			return nil, status.Error(codes.Unauthenticated, "authorization required")
		}

		authenticated, err := authenticator.authenticate(ctx, token)
		if err != nil {
			return nil, status.Error(codes.Internal, "authentication error")
		}
		if !authenticated {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}

		return handler(ctx, req)
	}
}

// newAuthStreamInterceptor creates a gRPC stream interceptor that validates bearer tokens
func newAuthStreamInterceptor(authenticator *tokenAuthenticator) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		token := tokenFromGRPCContext(ss.Context())
		if token == "" {
			return status.Error(codes.Unauthenticated, "authorization required")
		}

		authenticated, err := authenticator.authenticate(ss.Context(), token)
		if err != nil {
			return status.Error(codes.Internal, "authentication error")
		}
		if !authenticated {
			return status.Error(codes.Unauthenticated, "invalid token")
		}

		return handler(srv, ss)
	}
}
