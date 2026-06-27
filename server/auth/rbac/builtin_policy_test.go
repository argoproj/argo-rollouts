package rbac

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuiltinRoles(t *testing.T) {
	assert.Equal(t, []string{"role:admin", "role:readonly", "role:operator"}, BuiltinRoles)
}

func TestBuiltinPolicyMentionsRoles(t *testing.T) {
	for _, role := range BuiltinRoles {
		assert.Contains(t, BuiltinPolicyCSV, role, "policy should reference %s", role)
	}
}

func TestOperatorHasPromoteNotDelete(t *testing.T) {
	lines := strings.Split(BuiltinPolicyCSV, "\n")
	var hasPromote, hasDelete bool
	for _, l := range lines {
		if strings.Contains(l, "role:operator") && strings.Contains(l, "promote") {
			hasPromote = true
		}
		if strings.Contains(l, "role:operator") && strings.Contains(l, "delete") {
			hasDelete = true
		}
	}
	assert.True(t, hasPromote, "operator must have promote")
	assert.False(t, hasDelete, "operator must NOT have delete")
}
