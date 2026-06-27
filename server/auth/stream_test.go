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

// fakeServerStream is a minimal grpc.ServerStream carrying a context.
type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (f *fakeServerStream) Context() context.Context { return f.ctx }

func TestStreamValidTokenInjectsClaims(t *testing.T) {
	v := &fakeVerifier{claims: jwt.MapClaims{"sub": "alice"}}
	i := NewInterceptor(v, false, nil)

	md := metadata.Pairs("authorization", "Bearer good")
	base := metadata.NewIncomingContext(context.Background(), md)
	ss := &fakeServerStream{ctx: base}

	var seenSub interface{}
	handler := func(_ interface{}, stream grpc.ServerStream) error {
		c, _ := ClaimsFromContext(stream.Context())
		seenSub = c["sub"]
		return nil
	}
	err := i.Stream(nil, ss, &grpc.StreamServerInfo{FullMethod: "/rollout.RolloutService/WatchRolloutInfos"}, handler)
	require.NoError(t, err)
	assert.Equal(t, "alice", seenSub)
}

func TestStreamInvalidTokenRejected(t *testing.T) {
	v := &fakeVerifier{err: errors.New("bad")}
	i := NewInterceptor(v, false, nil)

	md := metadata.Pairs("authorization", "Bearer forged")
	ss := &fakeServerStream{ctx: metadata.NewIncomingContext(context.Background(), md)}

	err := i.Stream(nil, ss, &grpc.StreamServerInfo{FullMethod: "/rollout.RolloutService/WatchRolloutInfos"}, func(_ interface{}, _ grpc.ServerStream) error {
		return nil
	})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}
