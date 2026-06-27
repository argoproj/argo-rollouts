// Package auth provides authentication for the Argo Rollouts dashboard server:
// extracting a request token, verifying it into claims, and enforcing that
// verification via gRPC interceptors.
package auth

import (
	"context"

	"github.com/golang-jwt/jwt/v5"
)

// TokenVerifier verifies a token string and returns its claims. It is
// satisfied by session.SessionManager.
//
// Contract: Parse must return a non-nil error on failure. On success it
// returns the token's claims; a nil success result is defensively normalized
// to an empty jwt.MapClaims{} by the interceptor so callers always see a
// non-nil map for authenticated requests.
type TokenVerifier interface {
	Parse(token string) (jwt.MapClaims, error)
}

// contextKey is an unexported type for context keys defined in this package,
// to prevent collisions with keys from other packages.
type contextKey string

const claimsContextKey contextKey = "auth.claims"

// ContextWithClaims returns a copy of ctx carrying claims.
func ContextWithClaims(ctx context.Context, claims jwt.MapClaims) context.Context {
	return context.WithValue(ctx, claimsContextKey, claims)
}

// ClaimsFromContext returns the claims carried by ctx, if any.
func ClaimsFromContext(ctx context.Context) (jwt.MapClaims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(jwt.MapClaims)
	return claims, ok
}
