package auth

import (
	"context"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"
)

func TestClaimsRoundTrip(t *testing.T) {
	claims := jwt.MapClaims{"sub": "alice"}
	ctx := ContextWithClaims(context.Background(), claims)

	got, ok := ClaimsFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, "alice", got["sub"])
}

func TestClaimsFromContextAbsent(t *testing.T) {
	_, ok := ClaimsFromContext(context.Background())
	assert.False(t, ok)
}

func TestTokenFromAuthorizationHeader(t *testing.T) {
	md := metadata.Pairs("authorization", "Bearer abc.def.ghi")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	assert.Equal(t, "abc.def.ghi", tokenFromContext(ctx))
}

func TestTokenFromAuthorizationNoBearerPrefix(t *testing.T) {
	md := metadata.Pairs("authorization", "abc.def.ghi")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	assert.Equal(t, "abc.def.ghi", tokenFromContext(ctx))
}

func TestTokenFromCookie(t *testing.T) {
	md := metadata.Pairs("cookie", AuthCookieName+"=cookie.token.val; other=x")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	assert.Equal(t, "cookie.token.val", tokenFromContext(ctx))
}

func TestTokenAuthorizationBeatsCookie(t *testing.T) {
	md := metadata.Pairs(
		"authorization", "Bearer header.token",
		"cookie", AuthCookieName+"=cookie.token",
	)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	assert.Equal(t, "header.token", tokenFromContext(ctx))
}

func TestTokenAbsent(t *testing.T) {
	assert.Equal(t, "", tokenFromContext(context.Background()))
}
