// Package oidc implements the dashboard's OIDC SSO login and callback flow.
package oidc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/argoproj/argo-rollouts/server/auth"
)

const stateCookieName = "argorollouts.oidc-state"

// Claims is the identity extracted from a verified ID token.
type Claims struct {
	Subject string
	Groups  []string
}

// AuthCodeURLBuilder builds the IdP authorization URL for a given state.
type AuthCodeURLBuilder interface {
	AuthCodeURL(state string) string
}

// CodeExchanger exchanges an authorization code for a raw ID token.
type CodeExchanger interface {
	Exchange(ctx context.Context, code string) (rawIDToken string, err error)
}

// IDTokenVerifier verifies a raw ID token and returns its claims.
type IDTokenVerifier interface {
	Verify(ctx context.Context, rawIDToken string) (Claims, error)
}

// TokenIssuerWithGroups mints a dashboard session token carrying groups.
type TokenIssuerWithGroups interface {
	CreateWithGroups(subject string, groups []string, expiry time.Duration, id string) (string, error)
}

// Handler serves /auth/login and /auth/callback.
type Handler struct {
	URLBuilder  AuthCodeURLBuilder
	Exchanger   CodeExchanger
	Verifier    IDTokenVerifier
	Issuer      TokenIssuerWithGroups
	TokenExpiry time.Duration
}

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Login starts the OIDC flow: set a CSRF state cookie and redirect to the IdP.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   300,
	})
	http.Redirect(w, r, h.URLBuilder.AuthCodeURL(state), http.StatusFound)
}

// Callback completes the OIDC flow: validate state, exchange the code, verify
// the ID token, mint a session cookie, and redirect to the dashboard root.
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid authentication state", http.StatusUnauthorized)
		return
	}
	rawIDToken, err := h.Exchanger.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}
	claims, err := h.Verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}
	id, err := randomToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	token, err := h.Issuer.CreateWithGroups(claims.Subject, claims.Groups, h.TokenExpiry, id)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Clear the state cookie and set the session cookie. SameSite=Lax so the
	// cookie survives the top-level redirect back from the IdP. The clear cookie
	// mirrors the original attributes so browsers reliably overwrite it.
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	// Always set Secure: the session token must never travel over cleartext HTTP
	// (localhost is a browser secure context, so local dev still works).
	http.SetCookie(w, &http.Cookie{
		Name:     auth.AuthCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(h.TokenExpiry.Seconds()),
	})
	http.Redirect(w, r, "/", http.StatusFound)
}
