package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// fakeVerifier returns fixed claims/err regardless of token, recording the
// token it was given.
type fakeVerifier struct {
	claims jwt.MapClaims
	err    error
	seen   string
}

func (f *fakeVerifier) Parse(token string) (jwt.MapClaims, error) {
	f.seen = token
	return f.claims, f.err
}

func ctxWithToken(token string) context.Context {
	md := metadata.Pairs("authorization", "Bearer "+token)
	return metadata.NewIncomingContext(context.Background(), md)
}

func okHandler(_ context.Context, _ interface{}) (interface{}, error) {
	return "ok", nil
}

func TestUnaryValidToken(t *testing.T) {
	v := &fakeVerifier{claims: jwt.MapClaims{"sub": "alice"}}
	i := NewInterceptor(v, false, nil)

	var seenClaims jwt.MapClaims
	handler := func(ctx context.Context, _ interface{}) (interface{}, error) {
		seenClaims, _ = ClaimsFromContext(ctx)
		return "ok", nil
	}
	resp, err := i.Unary(ctxWithToken("good"), nil, &grpc.UnaryServerInfo{FullMethod: "/rollout.RolloutService/PromoteRollout"}, handler)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.Equal(t, "alice", seenClaims["sub"])
	assert.Equal(t, "good", v.seen)
}

func TestUnaryInvalidTokenRejected(t *testing.T) {
	v := &fakeVerifier{err: errors.New("bad signature")}
	i := NewInterceptor(v, true, nil) // even with anonymous enabled, a BAD token is rejected

	_, err := i.Unary(ctxWithToken("forged"), nil, &grpc.UnaryServerInfo{FullMethod: "/rollout.RolloutService/PromoteRollout"}, okHandler)
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestUnaryMissingTokenNoAnonymous(t *testing.T) {
	v := &fakeVerifier{}
	i := NewInterceptor(v, false, nil)

	_, err := i.Unary(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/rollout.RolloutService/ListRolloutInfos"}, okHandler)
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestUnaryMissingTokenAnonymousAllowed(t *testing.T) {
	v := &fakeVerifier{}
	i := NewInterceptor(v, true, nil)

	var hadClaims bool
	handler := func(ctx context.Context, _ interface{}) (interface{}, error) {
		_, hadClaims = ClaimsFromContext(ctx)
		return "ok", nil
	}
	resp, err := i.Unary(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/rollout.RolloutService/ListRolloutInfos"}, handler)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.True(t, hadClaims, "anonymous request still gets (empty) claims injected")
	assert.Empty(t, v.seen, "verifier not called when no token present")
}

func TestUnaryWhitelistSkipsAuth(t *testing.T) {
	v := &fakeVerifier{err: errors.New("should not be called")}
	wl := map[string]bool{"/session.SessionService/Create": true}
	i := NewInterceptor(v, false, wl)

	resp, err := i.Unary(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/session.SessionService/Create"}, okHandler)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.Empty(t, v.seen, "whitelisted method must not invoke the verifier")
}

func TestUnaryNilClaimsNormalized(t *testing.T) {
	// Verifier returns (nil, nil) — a degenerate but spec-legal success.
	// Fix A requires the interceptor to normalize nil to an empty map so that
	// authenticated requests always carry a non-nil claims map.
	v := &fakeVerifier{claims: nil, err: nil}
	i := NewInterceptor(v, false, nil)

	var (
		handlerOk  bool
		claimsNonNil bool
	)
	handler := func(ctx context.Context, _ interface{}) (interface{}, error) {
		handlerOk = true
		c, ok := ClaimsFromContext(ctx)
		claimsNonNil = ok && c != nil
		return "ok", nil
	}
	resp, err := i.Unary(ctxWithToken("valid"), nil, &grpc.UnaryServerInfo{FullMethod: "/rollout.RolloutService/PromoteRollout"}, handler)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.True(t, handlerOk, "handler must be called on valid token")
	assert.True(t, claimsNonNil, "nil claims from verifier must be normalized to empty non-nil map")
}
