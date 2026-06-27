package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"
)

// CredentialVerifier verifies a username/password. Satisfied by
// settings.SettingsManager.
type CredentialVerifier interface {
	VerifyUsernamePassword(ctx context.Context, username, password string) error
}

// TokenIssuer mints a signed session token. Satisfied by session.SessionManager.
type TokenIssuer interface {
	Create(subject string, expiry time.Duration, id string) (string, error)
}

// LoginHandler handles POST /api/login: verify credentials, issue a session
// token, set it as an HttpOnly cookie, and return it in the JSON body.
type LoginHandler struct {
	Verifier    CredentialVerifier
	Issuer      TokenIssuer
	TokenExpiry time.Duration
	Secure      bool // set the cookie Secure flag (under TLS)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *LoginHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.Verifier.VerifyUsernamePassword(r.Context(), req.Username, req.Password); err != nil {
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
		return
	}
	id, err := randomID()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	token, err := h.Issuer.Create(req.Username, h.TokenExpiry, id)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     AuthCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.Secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(h.TokenExpiry.Seconds()),
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
}

// LogoutHandler clears the session cookie.
func LogoutHandler(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     AuthCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
		SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusOK)
}

// randomID returns 16 random bytes hex-encoded, for use as a token jti.
func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
