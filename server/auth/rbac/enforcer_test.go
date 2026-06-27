package rbac

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func enforce(t *testing.T, e *Enforcer, sub, res, act, obj string) bool {
	t.Helper()
	ok, err := e.Enforce(sub, res, act, obj)
	require.NoError(t, err)
	return ok
}

func TestBuiltinRoleMatrix(t *testing.T) {
	e, err := NewEnforcer()
	require.NoError(t, err)

	cases := []struct {
		name             string
		sub, res, act, obj string
		want             bool
	}{
		{"readonly get rollout", "role:readonly", "rollouts", "get", "prod/web", true},
		{"readonly cannot promote", "role:readonly", "rollouts", "promote", "prod/web", false},
		{"operator promote", "role:operator", "rollouts", "promote", "prod/web", true},
		{"operator get experiment", "role:operator", "experiments", "get", "prod/e1", true},
		{"operator cannot delete", "role:operator", "rollouts", "delete", "prod/web", false},
		{"operator cannot setimage", "role:operator", "rollouts", "setimage", "prod/web", false},
		{"admin delete", "role:admin", "rollouts", "delete", "prod/web", true},
		{"admin anything", "role:admin", "analysisruns", "abort", "any/thing", true},
		{"unknown subject denied", "role:nobody", "rollouts", "get", "prod/web", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, enforce(t, e, c.sub, c.res, c.act, c.obj))
		})
	}
}
