package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

// OIDCConfig holds the OIDC provider configuration for SSO login
type OIDCConfig struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

// IsConfigured returns true if OIDC is configured with the required fields
func (c *OIDCConfig) IsConfigured() bool {
	return c.IssuerURL != "" && c.ClientID != ""
}

// oidcDiscoveryDoc represents the OpenID Connect discovery document
type oidcDiscoveryDoc struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
}

// oidcHandler manages the OIDC login flow
type oidcHandler struct {
	config    OIDCConfig
	oauth2Cfg *oauth2.Config
	states    sync.Map // CSRF state tokens
	rootPath  string
}

func newOIDCHandler(config OIDCConfig, rootPath string) (*oidcHandler, error) {
	// Discover OIDC endpoints
	discovery, err := discoverOIDC(config.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery failed: %w", err)
	}

	scopes := config.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}

	oauth2Cfg := &oauth2.Config{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		RedirectURL:  config.RedirectURL,
		Endpoint: oauth2.Endpoint{
			AuthURL:  discovery.AuthorizationEndpoint,
			TokenURL: discovery.TokenEndpoint,
		},
		Scopes: scopes,
	}

	return &oidcHandler{
		config:    config,
		oauth2Cfg: oauth2Cfg,
		rootPath:  rootPath,
	}, nil
}

// discoverOIDC fetches the OIDC discovery document from the issuer
func discoverOIDC(issuerURL string) (*oidcDiscoveryDoc, error) {
	wellKnown := strings.TrimSuffix(issuerURL, "/") + "/.well-known/openid-configuration"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnown, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OIDC discovery document from %s: %w", wellKnown, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC discovery returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read OIDC discovery response: %w", err)
	}

	var doc oidcDiscoveryDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse OIDC discovery document: %w", err)
	}

	if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" {
		return nil, fmt.Errorf("OIDC discovery document missing required endpoints")
	}

	return &doc, nil
}

// generateState creates a random state string for CSRF protection
func generateState() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// handleLogin redirects the user to the OIDC provider's authorization endpoint
func (h *oidcHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := generateState()
	if err != nil {
		log.Errorf("Failed to generate OIDC state: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Store state with expiry for CSRF validation
	h.states.Store(state, time.Now().Add(5*time.Minute))

	url := h.oauth2Cfg.AuthCodeURL(state)
	http.Redirect(w, r, url, http.StatusFound)
}

// handleCallback processes the OIDC provider's callback, exchanges the authorization
// code for tokens, and redirects the user back to the frontend with the token
func (h *oidcHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	// Validate state for CSRF protection
	state := r.URL.Query().Get("state")
	if state == "" {
		http.Error(w, "Missing state parameter", http.StatusBadRequest)
		return
	}

	expiry, ok := h.states.LoadAndDelete(state)
	if !ok {
		http.Error(w, "Invalid state parameter", http.StatusBadRequest)
		return
	}
	if time.Now().After(expiry.(time.Time)) {
		http.Error(w, "State parameter expired", http.StatusBadRequest)
		return
	}

	// Check for errors from the OIDC provider
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		errDesc := r.URL.Query().Get("error_description")
		log.Errorf("OIDC callback error: %s - %s", errMsg, errDesc)
		http.Error(w, fmt.Sprintf("Authentication error: %s", errDesc), http.StatusUnauthorized)
		return
	}

	// Exchange authorization code for tokens
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	token, err := h.oauth2Cfg.Exchange(r.Context(), code)
	if err != nil {
		log.Errorf("Failed to exchange OIDC code: %v", err)
		http.Error(w, "Failed to exchange authorization code", http.StatusInternalServerError)
		return
	}

	// Extract ID token - this is what Kubernetes uses for OIDC authentication
	idToken, ok := token.Extra("id_token").(string)
	if !ok || idToken == "" {
		log.Error("No id_token in OIDC token response")
		http.Error(w, "No ID token received from provider", http.StatusInternalServerError)
		return
	}

	// Redirect to frontend with token in URL fragment
	// Fragment (#) is not sent to the server, making it more secure than query params
	redirectPath := path.Clean("/"+h.rootPath) + "/"
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Authenticating...</title></head>
<body>
<script>
localStorage.setItem('auth_token', %s);
window.location.href = %s;
</script>
<noscript>Authentication requires JavaScript.</noscript>
</body>
</html>`, jsonString(idToken), jsonString(redirectPath))
}

// jsonString returns a JSON-encoded string safe for embedding in HTML/JS
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// registerOIDCRoutes registers the OIDC login and callback HTTP handlers
func (h *oidcHandler) registerOIDCRoutes(mux *http.ServeMux, rootPath string) {
	loginPath := "/auth/login"
	callbackPath := "/auth/callback"
	if rootPath != "" {
		loginPath = path.Join("/", rootPath, "auth/login")
		callbackPath = path.Join("/", rootPath, "auth/callback")
	}
	mux.HandleFunc(loginPath, h.handleLogin)
	mux.HandleFunc(callbackPath, h.handleCallback)
	log.Infof("OIDC login enabled at %s", loginPath)
}
