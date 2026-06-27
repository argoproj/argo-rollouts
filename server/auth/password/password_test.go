package password

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashThenVerify(t *testing.T) {
	hash, err := HashPassword("correct horse")
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.NotEqual(t, "correct horse", hash, "hash must not equal plaintext")
	assert.NoError(t, VerifyPassword("correct horse", hash))
}

func TestVerifyWrongPassword(t *testing.T) {
	hash, err := HashPassword("correct horse")
	require.NoError(t, err)
	assert.Error(t, VerifyPassword("battery staple", hash))
}

func TestHashDistinctSalts(t *testing.T) {
	h1, err := HashPassword("same")
	require.NoError(t, err)
	h2, err := HashPassword("same")
	require.NoError(t, err)
	assert.NotEqual(t, h1, h2, "bcrypt salts each hash differently")
}

func TestHashRejectsOverLongPassword(t *testing.T) {
	_, err := HashPassword(strings.Repeat("a", MaxPasswordLength+1))
	require.Error(t, err)
}

func TestVerifyRejectsGarbageHash(t *testing.T) {
	assert.Error(t, VerifyPassword("x", "not-a-bcrypt-hash"))
}

func TestVerifyRejectsOverLongPassword(t *testing.T) {
	hash, err := HashPassword("correct horse")
	require.NoError(t, err)
	// >72-byte password must be rejected before bcrypt sees it.
	err = VerifyPassword(strings.Repeat("a", MaxPasswordLength+1), hash)
	assert.Error(t, err, "VerifyPassword must reject passwords longer than MaxPasswordLength")
}
