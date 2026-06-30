package auth

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// recordingEnforcer allows (sub,res,act,obj) tuples present in its allow set.
type recordingEnforcer struct {
	allow map[string]bool // key: sub|res|act|obj
	err   error
	calls []string
}

func (e *recordingEnforcer) EnforceWithDefault(defaultRole, sub, res, act, obj string) (bool, error) {
	if e.err != nil {
		return false, e.err
	}
	key := sub + "|" + res + "|" + act + "|" + obj
	e.calls = append(e.calls, key)
	// defaultRole fallback: if sub not allowed, try the default role's key.
	if e.allow[key] {
		return true, nil
	}
	if defaultRole != "" {
		return e.allow[defaultRole+"|"+res+"|"+act+"|"+obj], nil
	}
	return false, nil
}

func TestSubjectAndGroups(t *testing.T) {
	claims := jwt.MapClaims{"sub": "alice", "groups": []interface{}{"dev", "ops"}}
	assert.Equal(t, "alice", Subject(claims))
	assert.Equal(t, []string{"dev", "ops"}, Groups(claims))

	assert.Equal(t, "", Subject(jwt.MapClaims{}))
	assert.Nil(t, Groups(jwt.MapClaims{}))
	assert.Equal(t, "", Subject(nil))
}

func TestEnforceClaimsAllowsSubject(t *testing.T) {
	e := &recordingEnforcer{allow: map[string]bool{"alice|rollouts|promote|prod/web": true}}
	claims := jwt.MapClaims{"sub": "alice"}
	assert.NoError(t, EnforceClaims(e, "", claims, "rollouts", "promote", "prod/web"))
}

func TestEnforceClaimsAllowsViaGroup(t *testing.T) {
	e := &recordingEnforcer{allow: map[string]bool{"ops|rollouts|abort|prod/web": true}}
	claims := jwt.MapClaims{"sub": "alice", "groups": []interface{}{"dev", "ops"}}
	assert.NoError(t, EnforceClaims(e, "", claims, "rollouts", "abort", "prod/web"))
}

func TestEnforceClaimsDeniedIsPermissionDenied(t *testing.T) {
	e := &recordingEnforcer{allow: map[string]bool{}}
	claims := jwt.MapClaims{"sub": "alice"}
	err := EnforceClaims(e, "", claims, "rollouts", "delete", "prod/web")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestEnforceClaimsEnforcerErrorIsInternal(t *testing.T) {
	e := &recordingEnforcer{err: assertAnErr{}}
	claims := jwt.MapClaims{"sub": "alice"}
	err := EnforceClaims(e, "", claims, "rollouts", "get", "prod/web")
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestEnforceClaimsAnonymousUsesDefaultRole(t *testing.T) {
	e := &recordingEnforcer{allow: map[string]bool{"role:readonly|rollouts|get|prod/web": true}}
	// empty claims => empty subject => default role applies
	assert.NoError(t, EnforceClaims(e, "role:readonly", jwt.MapClaims{}, "rollouts", "get", "prod/web"))
	// no default role => denied
	err := EnforceClaims(e, "", jwt.MapClaims{}, "rollouts", "get", "prod/web")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

type assertAnErr struct{}

func (assertAnErr) Error() string { return "boom" }
