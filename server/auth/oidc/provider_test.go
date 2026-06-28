package oidc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// oauth2Adapter and verifierAdapter are the real adapters; these tests assert
// they satisfy the Task 3 interfaces and build AuthCodeURLs, without any network.
func TestOauth2AdapterBuildsURL(t *testing.T) {
	a := newOAuth2Adapter("client", "secret", "https://idp/auth", "https://idp/token", "https://dash/auth/callback", []string{"openid", "groups"})
	url := a.AuthCodeURL("state123")
	assert.Contains(t, url, "client_id=client")
	assert.Contains(t, url, "state=state123")
	assert.Contains(t, url, "redirect_uri=")
}

func TestNewProviderValidatesConfig(t *testing.T) {
	// Empty issuer must error rather than attempting discovery.
	_, err := NewProvider(nil, ProviderConfig{Issuer: "", TokenExpiry: time.Hour})
	assert.Error(t, err)
}
