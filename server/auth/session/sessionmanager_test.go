package session

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRejectsNonHMACSigningMethod(t *testing.T) {
	// A token using a non-HMAC algorithm must be rejected (alg-confusion guard).
	mgr := NewSessionManager([]byte("test-signing-key"))
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"iss": "argo-rollouts", "sub": "alice", "exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	_, err = mgr.Parse(signed)
	assert.Error(t, err, "non-HMAC signing method must be rejected")
}

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
