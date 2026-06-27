package server

import (
	"context"
	"fmt"
	"time"

	"github.com/argoproj/argo-rollouts/server/auth"
	"github.com/argoproj/argo-rollouts/server/auth/rbac"
	"github.com/argoproj/argo-rollouts/server/auth/session"
	"github.com/argoproj/argo-rollouts/server/auth/settings"
)

// tokenExpiry is the lifetime of a dashboard session token.
const tokenExpiry = 24 * time.Hour

// authNWhitelist lists gRPC methods that skip authentication entirely.
var authNWhitelist = map[string]bool{
	"/rollout.RolloutService/Version": true,
}

// authComponents holds the assembled authentication/authorization machinery.
type authComponents struct {
	authn       *auth.Interceptor
	authz       *auth.AuthzInterceptor
	login       *auth.LoginHandler
	enforcer    *rbac.Enforcer
	defaultRole string
}

// setupAuth builds the auth components from the dashboard's Kubernetes settings.
// It returns an error (never a silently-disabled result) if required config —
// notably the signing key — is missing or invalid.
func (s *ArgoRolloutsServer) setupAuth(ctx context.Context) (*authComponents, error) {
	sm := settings.NewSettingsManager(s.Options.KubeClientset, s.Options.Namespace)

	key, err := sm.GetSigningKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth setup: %w", err)
	}
	sessionMgr := session.NewSessionManager(key)

	rbacCfg, err := sm.GetRBACConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth setup: load rbac config: %w", err)
	}
	enforcer, err := rbac.NewEnforcer()
	if err != nil {
		return nil, fmt.Errorf("auth setup: new enforcer: %w", err)
	}
	if err := enforcer.SetUserPolicy(rbacCfg.PolicyCSV); err != nil {
		return nil, fmt.Errorf("auth setup: load policy: %w", err)
	}

	anonymous, err := sm.AnonymousEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth setup: anonymous flag: %w", err)
	}

	return &authComponents{
		authn:       auth.NewInterceptor(sessionMgr, anonymous, authNWhitelist),
		authz:       auth.NewAuthzInterceptor(enforcer, rbacCfg.DefaultRole),
		login:       &auth.LoginHandler{Verifier: sm, Issuer: sessionMgr, TokenExpiry: tokenExpiry, Secure: false},
		enforcer:    enforcer,
		defaultRole: rbacCfg.DefaultRole,
	}, nil
}

// authorizeStream enforces RBAC for a streaming RPC using the claims on ctx.
// It is a no-op when auth is disabled (s.auth == nil).
func (s *ArgoRolloutsServer) authorizeStream(ctx context.Context, action, object string) error {
	if s.auth == nil {
		return nil
	}
	claims, _ := auth.ClaimsFromContext(ctx)
	return auth.EnforceClaims(s.auth.enforcer, s.auth.defaultRole, claims, rbac.ResourceRollouts, action, object)
}
