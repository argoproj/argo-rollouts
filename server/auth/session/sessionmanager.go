// Package session issues and validates HS256 JWT session tokens for the
// Argo Rollouts dashboard.
package session

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SessionManagerClaimsIssuer is the iss claim value for dashboard-issued tokens.
const SessionManagerClaimsIssuer = "argo-rollouts"

// SessionManager signs and verifies session tokens with a shared HS256 key.
type SessionManager struct {
	signingKey []byte
}

// NewSessionManager returns a SessionManager that signs with signingKey.
func NewSessionManager(signingKey []byte) *SessionManager {
	return &SessionManager{signingKey: signingKey}
}

// Create signs a new HS256 JWT for subject, valid for the given duration. id is
// the token's unique jti.
func (mgr *SessionManager) Create(subject string, expiry time.Duration, id string) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    SessionManagerClaimsIssuer,
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
		ID:        id,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(mgr.signingKey)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}
