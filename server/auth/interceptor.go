package auth

import (
	"context"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Interceptor authenticates incoming gRPC requests by verifying a session
// token into claims on the context.
type Interceptor struct {
	Verifier         TokenVerifier
	AnonymousEnabled bool
	Whitelist        map[string]bool
}

// NewInterceptor returns an Interceptor. whitelist maps gRPC FullMethod names
// that skip authentication; it may be nil.
func NewInterceptor(verifier TokenVerifier, anonymousEnabled bool, whitelist map[string]bool) *Interceptor {
	if whitelist == nil {
		whitelist = map[string]bool{}
	}
	return &Interceptor{Verifier: verifier, AnonymousEnabled: anonymousEnabled, Whitelist: whitelist}
}

// authenticate returns a context enriched with verified claims, or an error if
// authentication fails. Whitelisted methods return ctx unchanged.
func (i *Interceptor) authenticate(ctx context.Context, fullMethod string) (context.Context, error) {
	if i.Whitelist[fullMethod] {
		return ctx, nil
	}
	token := tokenFromContext(ctx)
	if token == "" {
		if i.AnonymousEnabled {
			return ContextWithClaims(ctx, jwt.MapClaims{}), nil
		}
		return nil, status.Error(codes.Unauthenticated, "no authentication token provided")
	}
	claims, err := i.Verifier.Parse(token)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid authentication token")
	}
	return ContextWithClaims(ctx, claims), nil
}

// Unary is a grpc.UnaryServerInterceptor enforcing authentication.
func (i *Interceptor) Unary(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	newCtx, err := i.authenticate(ctx, info.FullMethod)
	if err != nil {
		return nil, err
	}
	return handler(newCtx, req)
}
