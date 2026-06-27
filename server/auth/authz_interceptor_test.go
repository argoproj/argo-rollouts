package auth

import (
	"context"
	"testing"

	"github.com/argoproj/argo-rollouts/server/auth/rbac"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// allowEnforcer allows a fixed (sub,res,act,obj) tuple.
type allowEnforcer struct{ allowKey string }

func (e allowEnforcer) EnforceWithDefault(_ , sub, res, act, obj string) (bool, error) {
	return sub+"|"+res+"|"+act+"|"+obj == e.allowKey, nil
}

func promoteReq(ns, name string) interface{} { return nsNameReq{ns: ns, name: name} }

func claimsCtx(sub string) context.Context {
	return ContextWithClaims(context.Background(), jwt.MapClaims{"sub": sub})
}

func TestAuthzAllowsPermittedCall(t *testing.T) {
	e := allowEnforcer{allowKey: "alice|rollouts|promote|prod/web"}
	a := NewAuthzInterceptor(e, "")

	called := false
	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		called = true
		return "ok", nil
	}
	resp, err := a.Unary(claimsCtx("alice"), promoteReq("prod", "web"),
		&grpc.UnaryServerInfo{FullMethod: "/rollout.RolloutService/PromoteRollout"}, handler)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.True(t, called)
}

func TestAuthzDeniesUnpermittedCall(t *testing.T) {
	e := allowEnforcer{allowKey: "nobody|x|x|x"}
	a := NewAuthzInterceptor(e, "")

	called := false
	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		called = true
		return "ok", nil
	}
	_, err := a.Unary(claimsCtx("alice"), promoteReq("prod", "web"),
		&grpc.UnaryServerInfo{FullMethod: "/rollout.RolloutService/PromoteRollout"}, handler)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, called, "handler must not run on denial")
}

func TestAuthzUnmappedMethodPassesThrough(t *testing.T) {
	// Version is not in the permission map => no authz, handler runs.
	e := allowEnforcer{allowKey: "no|no|no|no"}
	a := NewAuthzInterceptor(e, "")

	called := false
	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		called = true
		return "v", nil
	}
	resp, err := a.Unary(claimsCtx("alice"), struct{}{},
		&grpc.UnaryServerInfo{FullMethod: "/rollout.RolloutService/Version"}, handler)
	require.NoError(t, err)
	assert.Equal(t, "v", resp)
	assert.True(t, called)
}

func TestAuthzObjectScoping(t *testing.T) {
	// alice may promote in prod/* only; prod/web allowed, dev/web denied.
	e := allowEnforcer{allowKey: "alice|rollouts|promote|prod/web"}
	a := NewAuthzInterceptor(e, "")
	handler := func(_ context.Context, _ interface{}) (interface{}, error) { return "ok", nil }

	_, err := a.Unary(claimsCtx("alice"), promoteReq("dev", "web"),
		&grpc.UnaryServerInfo{FullMethod: "/rollout.RolloutService/PromoteRollout"}, handler)
	assert.Equal(t, codes.PermissionDenied, status.Code(err), "wrong namespace denied")
}

func TestAuthzUsesRbacConstants(t *testing.T) {
	// Sanity: the SetImage method maps to setimage and reads the rollout name.
	perm, ok := PermissionForMethod("/rollout.RolloutService/SetRolloutImage")
	require.True(t, ok)
	assert.Equal(t, rbac.ResourceRollouts, perm.Resource)
	assert.Equal(t, rbac.ActionSetImage, perm.Action)
}
