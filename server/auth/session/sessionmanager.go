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
// The caller is responsible for supplying a strong signingKey — at least 32
// bytes of high-entropy data — since an empty or short HS256 key is trivially
// brute-forced.
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

// Parse verifies tokenString's HS256 signature and issuer and returns its
// claims. It rejects any non-HS256 algorithm (including "none"), a bad
// signature, a wrong issuer, an expired token, or a token missing the exp claim.
func (mgr *SessionManager) Parse(tokenString string) (jwt.MapClaims, error) {
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return mgr.signingKey, nil
	},
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(SessionManagerClaimsIssuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	return claims, nil
}

// CreateWithGroups signs an HS256 session token for subject with an optional
// groups claim. Used by the OIDC callback to carry identity from the IdP into a
// normal dashboard session token.
func (mgr *SessionManager) CreateWithGroups(subject string, groups []string, expiry time.Duration, id string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": SessionManagerClaimsIssuer,
		"sub": subject,
		"iat": now.Unix(),
		"exp": now.Add(expiry).Unix(),
		"jti": id,
	}
	if len(groups) > 0 {
		arr := make([]interface{}, len(groups))
		for i, g := range groups {
			arr[i] = g
		}
		claims["groups"] = arr
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(mgr.signingKey)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}
