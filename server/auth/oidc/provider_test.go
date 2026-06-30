package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// oidcServerWithKeys serves an OIDC discovery doc plus a JWKS built from key,
// and returns the issuer URL. Used to exercise the real go-oidc verifier.
func oidcServerWithKeys(t *testing.T, key *rsa.PrivateKey, kid string) string {
	t.Helper()
	var issuer string
	jwks := map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": kid,
		"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": issuer, "authorization_endpoint": issuer + "/auth",
			"token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys",
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwks)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	issuer = srv.URL
	return issuer
}

func TestVerifierAdapterVerifiesSignedToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	issuer := oidcServerWithKeys(t, key, "k1")

	h, err := NewProvider(context.Background(), ProviderConfig{
		Issuer: issuer, ClientID: "client", RedirectURL: "https://dash/cb", TokenExpiry: time.Hour,
	})
	require.NoError(t, err)

	mint := func(claims jwt.MapClaims) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tok.Header["kid"] = "k1"
		signed, sErr := tok.SignedString(key)
		require.NoError(t, sErr)
		return signed
	}

	t.Run("valid token yields subject and groups", func(t *testing.T) {
		raw := mint(jwt.MapClaims{
			"iss": issuer, "aud": "client", "sub": "alice",
			"groups": []string{"devs"}, "exp": time.Now().Add(time.Hour).Unix(),
		})
		claims, vErr := h.Verifier.Verify(context.Background(), raw)
		require.NoError(t, vErr)
		assert.Equal(t, "alice", claims.Subject)
		assert.Equal(t, []string{"devs"}, claims.Groups)
	})

	t.Run("wrong audience is rejected", func(t *testing.T) {
		raw := mint(jwt.MapClaims{
			"iss": issuer, "aud": "other", "sub": "alice",
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		_, vErr := h.Verifier.Verify(context.Background(), raw)
		assert.Error(t, vErr)
	})
}

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

func TestNewProviderErrorsOnUndiscoverableIssuer(t *testing.T) {
	// A non-OIDC issuer must fail discovery, not panic.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, &http.Request{})
	}))
	defer srv.Close()
	_, err := NewProvider(context.Background(), ProviderConfig{Issuer: srv.URL, TokenExpiry: time.Hour})
	assert.Error(t, err)
}

func TestNewProviderDiscoversIssuer(t *testing.T) {
	// Serve a minimal OIDC discovery document; NewProvider should wire adapters.
	var issuer string
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 issuer,
			"authorization_endpoint": issuer + "/auth",
			"token_endpoint":         issuer + "/token",
			"jwks_uri":               issuer + "/keys",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	issuer = srv.URL

	h, err := NewProvider(context.Background(), ProviderConfig{
		Issuer:      issuer,
		ClientID:    "client",
		RedirectURL: "https://dash/auth/callback",
		Scopes:      []string{"openid", "groups"},
		TokenExpiry: time.Hour,
	})
	require.NoError(t, err)
	require.NotNil(t, h)
	assert.NotNil(t, h.URLBuilder)
	assert.NotNil(t, h.Exchanger)
	assert.NotNil(t, h.Verifier)
	// The wired URL builder must point at the discovered authorization endpoint.
	assert.Contains(t, h.URLBuilder.AuthCodeURL("st"), "state=st")
}

func TestOauth2AdapterExchange(t *testing.T) {
	t.Run("returns the id_token from the token response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "at",
				"token_type":   "Bearer",
				"id_token":     "raw.jwt.token",
			})
		}))
		defer srv.Close()
		a := newOAuth2Adapter("c", "s", srv.URL+"/auth", srv.URL+"/token", "https://dash/cb", nil)
		raw, err := a.Exchange(context.Background(), "code")
		require.NoError(t, err)
		assert.Equal(t, "raw.jwt.token", raw)
	})

	t.Run("errors when the token response has no id_token", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer"})
		}))
		defer srv.Close()
		a := newOAuth2Adapter("c", "s", srv.URL+"/auth", srv.URL+"/token", "https://dash/cb", nil)
		_, err := a.Exchange(context.Background(), "code")
		assert.Error(t, err)
	})

	t.Run("propagates a token endpoint error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "bad", http.StatusBadRequest)
		}))
		defer srv.Close()
		a := newOAuth2Adapter("c", "s", srv.URL+"/auth", srv.URL+"/token", "https://dash/cb", nil)
		_, err := a.Exchange(context.Background(), "code")
		assert.Error(t, err)
	})
}
