// Package password provides bcrypt-based password hashing and verification
// for Argo Rollouts dashboard local accounts.
package password

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// MaxPasswordLength is bcrypt's maximum input length in bytes. Inputs longer
// than this are silently truncated by bcrypt, so we reject them explicitly.
const MaxPasswordLength = 72

// HashPassword returns a bcrypt hash of password at the default cost.
func HashPassword(password string) (string, error) {
	if len(password) > MaxPasswordLength {
		return "", fmt.Errorf("password exceeds maximum length of %d bytes", MaxPasswordLength)
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hashed), nil
}

// VerifyPassword returns nil if password matches hashedPassword, else an error.
func VerifyPassword(password, hashedPassword string) error {
	if len(password) > MaxPasswordLength {
		return fmt.Errorf("password exceeds maximum length of %d bytes", MaxPasswordLength)
	}
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}
