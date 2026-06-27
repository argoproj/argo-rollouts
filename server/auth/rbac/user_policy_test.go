package rbac

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserPolicyGroupBinding(t *testing.T) {
	e, err := NewEnforcer()
	require.NoError(t, err)

	// Bind user alice to role:operator, and grant a custom narrow rule to bob.
	userCSV := `
g, alice, role:operator
p, bob, rollouts, get, dev/*, allow
`
	require.NoError(t, e.SetUserPolicy(userCSV))

	ok, err := e.Enforce("alice", "rollouts", "promote", "prod/web")
	require.NoError(t, err)
	assert.True(t, ok, "alice inherits operator promote")

	ok, err = e.Enforce("bob", "rollouts", "get", "dev/web")
	require.NoError(t, err)
	assert.True(t, ok, "bob get dev")

	ok, err = e.Enforce("bob", "rollouts", "get", "prod/web")
	require.NoError(t, err)
	assert.False(t, ok, "bob denied outside dev/*")
}

func TestEnforceWithDefaultRole(t *testing.T) {
	e, err := NewEnforcer()
	require.NoError(t, err)

	// carol has no binding; default role readonly grants get.
	ok, err := e.EnforceWithDefault("role:readonly", "carol", "rollouts", "get", "prod/web")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = e.EnforceWithDefault("role:readonly", "carol", "rollouts", "promote", "prod/web")
	require.NoError(t, err)
	assert.False(t, ok, "default readonly cannot promote")

	// empty default = locked down
	ok, err = e.EnforceWithDefault("", "carol", "rollouts", "get", "prod/web")
	require.NoError(t, err)
	assert.False(t, ok)
}
