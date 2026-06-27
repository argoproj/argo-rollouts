package auth

import (
	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Enforcer authorizes a subject against a resource/action/object. It is
// satisfied by *rbac.Enforcer.
type Enforcer interface {
	EnforceWithDefault(defaultRole, sub, res, act, obj string) (bool, error)
}

// Subject returns the "sub" claim, or "" if absent.
func Subject(claims jwt.MapClaims) string {
	if claims == nil {
		return ""
	}
	sub, _ := claims["sub"].(string)
	return sub
}

// Groups returns the string values of the "groups" claim, or nil.
func Groups(claims jwt.MapClaims) []string {
	if claims == nil {
		return nil
	}
	raw, ok := claims["groups"].([]interface{})
	if !ok {
		return nil
	}
	groups := make([]string, 0, len(raw))
	for _, g := range raw {
		if s, ok := g.(string); ok {
			groups = append(groups, s)
		}
	}
	return groups
}

// EnforceClaims authorizes the request. It tries the subject and each group as
// a Casbin subject (each with the default-role fallback). It returns nil if any
// is allowed, codes.PermissionDenied if none is, or codes.Internal on enforcer
// error. With empty claims it enforces the empty subject so only the configured
// default role can grant access.
func EnforceClaims(enforcer Enforcer, defaultRole string, claims jwt.MapClaims, resource, action, object string) error {
	subjects := make([]string, 0, 4)
	if sub := Subject(claims); sub != "" {
		subjects = append(subjects, sub)
	}
	subjects = append(subjects, Groups(claims)...)
	if len(subjects) == 0 {
		subjects = append(subjects, "") // anonymous: only default role can apply
	}
	for _, s := range subjects {
		allowed, err := enforcer.EnforceWithDefault(defaultRole, s, resource, action, object)
		if err != nil {
			return status.Error(codes.Internal, "authorization error")
		}
		if allowed {
			return nil
		}
	}
	return status.Error(codes.PermissionDenied, "permission denied")
}
