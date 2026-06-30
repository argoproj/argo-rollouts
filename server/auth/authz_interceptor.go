package auth

import (
	"context"

	"google.golang.org/grpc"
)

// AuthzInterceptor enforces RBAC on RolloutService calls using the claims placed
// on the context by the authentication interceptor.
type AuthzInterceptor struct {
	Enforcer    Enforcer
	DefaultRole string
}

// NewAuthzInterceptor returns an AuthzInterceptor backed by enforcer. defaultRole
// is applied for callers without an explicit grant (may be "").
func NewAuthzInterceptor(enforcer Enforcer, defaultRole string) *AuthzInterceptor {
	return &AuthzInterceptor{Enforcer: enforcer, DefaultRole: defaultRole}
}

// Unary is a grpc.UnaryServerInterceptor enforcing authorization. Methods absent
// from the permission map require no authorization and pass through.
func (a *AuthzInterceptor) Unary(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	perm, ok := PermissionForMethod(info.FullMethod)
	if !ok {
		return handler(ctx, req)
	}
	claims, _ := ClaimsFromContext(ctx)
	object := objectFromRequest(req)
	if err := EnforceClaims(a.Enforcer, a.DefaultRole, claims, perm.Resource, perm.Action, object); err != nil {
		return nil, err
	}
	return handler(ctx, req)
}
