package session

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateReturnsThreePartToken(t *testing.T) {
	mgr := NewSessionManager([]byte("test-signing-key"))
	tok, err := mgr.Create("alice", time.Hour, "jti-1")
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(tok, "."), "JWT has three dot-separated parts")
}

func TestCreateDistinctSubjectsDistinctTokens(t *testing.T) {
	mgr := NewSessionManager([]byte("test-signing-key"))
	a, err := mgr.Create("alice", time.Hour, "jti-1")
	require.NoError(t, err)
	b, err := mgr.Create("bob", time.Hour, "jti-2")
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
}
