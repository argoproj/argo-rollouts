package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateWithGroupsRoundTrip(t *testing.T) {
	mgr := NewSessionManager([]byte("test-signing-key-at-least-32bytes!!"))
	tok, err := mgr.CreateWithGroups("alice", []string{"dev", "ops"}, time.Hour, "jti-1")
	require.NoError(t, err)

	claims, err := mgr.Parse(tok)
	require.NoError(t, err)
	assert.Equal(t, "alice", claims["sub"])

	raw, ok := claims["groups"].([]interface{})
	require.True(t, ok, "groups claim present as array")
	assert.ElementsMatch(t, []interface{}{"dev", "ops"}, raw)
}

func TestCreateWithGroupsEmptyOmitsClaim(t *testing.T) {
	mgr := NewSessionManager([]byte("test-signing-key-at-least-32bytes!!"))
	tok, err := mgr.CreateWithGroups("bob", nil, time.Hour, "jti-2")
	require.NoError(t, err)

	claims, err := mgr.Parse(tok)
	require.NoError(t, err)
	_, hasGroups := claims["groups"]
	assert.False(t, hasGroups, "no groups claim when none supplied")
}
