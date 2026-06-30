package session

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRoundTrip(t *testing.T) {
	mgr := NewSessionManager([]byte("test-signing-key"))
	tok, err := mgr.Create("alice", time.Hour, "jti-1")
	require.NoError(t, err)

	claims, err := mgr.Parse(tok)
	require.NoError(t, err)
	assert.Equal(t, "alice", claims["sub"])
	assert.Equal(t, SessionManagerClaimsIssuer, claims["iss"])
}

func TestParseRejectsExpired(t *testing.T) {
	mgr := NewSessionManager([]byte("test-signing-key"))
	tok, err := mgr.Create("alice", -time.Hour, "jti-1") // already expired
	require.NoError(t, err)

	_, err = mgr.Parse(tok)
	assert.Error(t, err)
}

func TestParseRejectsWrongKey(t *testing.T) {
	signer := NewSessionManager([]byte("key-A"))
	verifier := NewSessionManager([]byte("key-B"))
	tok, err := signer.Create("alice", time.Hour, "jti-1")
	require.NoError(t, err)

	_, err = verifier.Parse(tok)
	assert.Error(t, err, "signature forged under a different key must be rejected")
}

func TestParseRejectsWrongIssuer(t *testing.T) {
	mgr := NewSessionManager([]byte("test-signing-key"))
	// Hand-craft a token with a different issuer but the correct key.
	claims := jwt.RegisteredClaims{
		Issuer:    "someone-else",
		Subject:   "alice",
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-signing-key"))
	require.NoError(t, err)

	_, err = mgr.Parse(tok)
	assert.Error(t, err, "issuer mismatch must be rejected")
}

func TestParseRejectsAlgNone(t *testing.T) {
	mgr := NewSessionManager([]byte("test-signing-key"))
	// alg=none token (unsigned) must never be accepted.
	claims := jwt.RegisteredClaims{
		Issuer:    SessionManagerClaimsIssuer,
		Subject:   "attacker",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = mgr.Parse(tok)
	assert.Error(t, err, "alg=none must be rejected (algorithm-confusion guard)")
}

// TestParseRejectsRS256 verifies that an RS256-signed token cannot be accepted
// by a manager that only trusts HS256 (algorithm-confusion attack guard).
func TestParseRejectsRS256(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	claims := jwt.RegisteredClaims{
		Issuer:    SessionManagerClaimsIssuer,
		Subject:   "attacker",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
	require.NoError(t, err)

	mgr := NewSessionManager([]byte("test-signing-key"))
	_, err = mgr.Parse(tok)
	assert.Error(t, err, "RS256-signed token must be rejected by an HS256-only verifier")
}

// TestParseRejectsMissingExpiry verifies that a token without an exp claim is
// rejected even when the signature and issuer are valid.
func TestParseRejectsMissingExpiry(t *testing.T) {
	mgr := NewSessionManager([]byte("test-signing-key"))
	// RegisteredClaims with no ExpiresAt → no exp claim in the token.
	claims := jwt.RegisteredClaims{
		Issuer:  SessionManagerClaimsIssuer,
		Subject: "alice",
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-signing-key"))
	require.NoError(t, err)

	_, err = mgr.Parse(tok)
	assert.Error(t, err, "token without exp claim must be rejected")
}
