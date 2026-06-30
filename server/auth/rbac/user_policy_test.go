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

// TestEnforceWithDefaultDirectlyAllowed exercises the ok==true early-return
// path: sub is directly permitted (via user policy), so the defaultRole branch
// is never consulted even though defaultRole itself cannot perform the action.
func TestEnforceWithDefaultDirectlyAllowed(t *testing.T) {
	e, err := NewEnforcer()
	require.NoError(t, err)

	// Bind dave directly to role:operator (which can promote).
	require.NoError(t, e.SetUserPolicy("g, dave, role:operator"))

	// defaultRole readonly cannot promote — but dave is directly allowed, so the
	// early-return (ok == true) must fire and the result must be true.
	ok, err := e.EnforceWithDefault("role:readonly", "dave", "rollouts", "promote", "prod/web")
	require.NoError(t, err)
	assert.True(t, ok, "dave directly allowed via operator; early-return must fire")
}
