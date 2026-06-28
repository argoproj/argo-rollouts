# Dashboard Auth — Plan 5: OIDC / SSO Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add OIDC single-sign-on: `/auth/login` redirects to the configured identity provider, `/auth/callback` verifies the returned ID token and mints a normal dashboard session token carrying the user's `sub` and `groups` — so SSO users flow through the exact same interceptor → RBAC path as local users, with no changes to Plans 4a–4e.

**Architecture:** Four units. `settings.GetOIDCConfig` parses the `oidc.config` ConfigMap entry (issuer, clientID, clientSecret, scopes, redirect URL). `session.CreateWithGroups` mints an HS256 session token embedding a `groups` claim (which `auth.Groups` already reads). `auth/oidc` (new sub-package) holds an `OIDCHandler` with `/auth/login` + `/auth/callback`, written against small injected interfaces (`AuthCodeURLBuilder`, `CodeExchanger`, `IDTokenVerifier`, `TokenIssuerWithGroups`) so the full flow — CSRF state, claim extraction, session minting, redirects — is unit-tested with fakes. The real go-oidc/oauth2 adapter (provider discovery + JWKS verification, which need a live IdP) is a thin layer built and wired in the final task. SSO and local login coexist; OIDC is enabled only when `oidc.config` is present.

**Tech Stack:** Go, `github.com/coreos/go-oidc/v3/oidc` (NEW dep), `golang.org/x/oauth2` (already direct), `github.com/golang-jwt/jwt/v5`, `gopkg.in/yaml.v3` or `sigs.k8s.io/yaml` (check which the repo uses), the `server/auth` packages, testify.

## Global Constraints

- Module `github.com/argoproj/argo-rollouts`. New package `server/auth/oidc` (package `oidc`). Edits: `server/auth/settings`, `server/auth/session`, `server/server.go`, `server/auth_setup.go`.
- The session token minted on callback is a NORMAL dashboard HS256 token (issuer `argo-rollouts`), NOT the raw IdP token — so the existing `session.Parse` / authN interceptor / RBAC path is unchanged. The IdP ID token is used only transiently during callback to establish identity.
- `groups` claim is a JSON array of strings; `auth.Groups` (Plan 4b) already reads it. Subject is the OIDC `sub` (or a configurable claim later — `sub` for now).
- **CSRF:** `/auth/login` generates a random `state`, stores it in a short-lived `HttpOnly` cookie, and `/auth/callback` rejects any request whose query `state` does not equal the cookie. No state match → `401`, no session minted.
- Every callback failure (bad state, exchange error, verify error) returns a generic error and mints NO session cookie — fail closed.
- The session cookie reuses `auth.AuthCookieName`, `HttpOnly`, `SameSite=Lax` (NOT Strict — the callback is a cross-site redirect from the IdP, Strict would drop the cookie on the top-level navigation), `Secure` per TLS, `Path=/`. (Local login keeps Strict; the OIDC callback specifically needs Lax to survive the IdP redirect.)
- OIDC is wired only when `oidc.config` is non-empty; absent → today's behavior (local login only). Backward-compatible.
- go-oidc/oauth2 network calls (provider discovery, token exchange, JWKS) live ONLY in the final adapter task; Tasks 1–3 must not make network calls and must be unit-testable offline.
- Reuse `session.SessionManager`, `auth.AuthCookieName`. testify; `httptest` for handlers.

---

### Task 1: OIDC config accessor

**Files:**
- Create: `server/auth/settings/oidc.go`
- Test: `server/auth/settings/oidc_test.go`

**Interfaces:**
- Consumes: `SettingsManager.configMapData`, `secretData`.
- Produces:
  - `const KeyOIDCConfig = "oidc.config"`
  - `type OIDCConfig struct { Issuer string; ClientID string; ClientSecret string; RequestedScopes []string; }`
  - `func (m *SettingsManager) GetOIDCConfig(ctx context.Context) (*OIDCConfig, bool, error)` — parses the `oidc.config` YAML from the ConfigMap; `(nil, false, nil)` if absent; error if present-but-malformed. `RequestedScopes` defaults to `["openid","profile","email","groups"]` when omitted.

- [ ] **Step 1: Write the failing test**

```go
package settings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetOIDCConfigParsed(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: ConfigMapName, Namespace: testNamespace},
		Data: map[string]string{KeyOIDCConfig: `
name: Okta
issuer: https://example.okta.com
clientID: abc123
clientSecret: shh
requestedScopes:
  - openid
  - groups
`},
	}
	m := NewSettingsManager(fake.NewSimpleClientset(cm), testNamespace)

	cfg, ok, err := m.GetOIDCConfig(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "https://example.okta.com", cfg.Issuer)
	assert.Equal(t, "abc123", cfg.ClientID)
	assert.Equal(t, "shh", cfg.ClientSecret)
	assert.Equal(t, []string{"openid", "groups"}, cfg.RequestedScopes)
}

func TestGetOIDCConfigDefaultScopes(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: ConfigMapName, Namespace: testNamespace},
		Data:       map[string]string{KeyOIDCConfig: "issuer: https://i\nclientID: c\n"},
	}
	m := NewSettingsManager(fake.NewSimpleClientset(cm), testNamespace)

	cfg, ok, err := m.GetOIDCConfig(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{"openid", "profile", "email", "groups"}, cfg.RequestedScopes)
}

func TestGetOIDCConfigAbsent(t *testing.T) {
	m := NewSettingsManager(fake.NewSimpleClientset(), testNamespace)
	cfg, ok, err := m.GetOIDCConfig(context.Background())
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, cfg)
}

func TestGetOIDCConfigMalformed(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: ConfigMapName, Namespace: testNamespace},
		Data:       map[string]string{KeyOIDCConfig: "::: not yaml :::"},
	}
	m := NewSettingsManager(fake.NewSimpleClientset(cm), testNamespace)
	_, ok, err := m.GetOIDCConfig(context.Background())
	assert.Error(t, err)
	assert.False(t, ok)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/settings/ -run TestGetOIDCConfig -v`
Expected: FAIL — `undefined: GetOIDCConfig`, `undefined: KeyOIDCConfig`.

Before implementing, confirm the YAML package the repo already uses: run `grep -rl "sigs.k8s.io/yaml\|gopkg.in/yaml" go.sum | head` and pick the one already in the module (prefer `sigs.k8s.io/yaml` if present, else `gopkg.in/yaml.v3`). Use that import in Step 3.

- [ ] **Step 3: Write minimal implementation**

```go
package settings

import (
	"context"
	"fmt"

	"sigs.k8s.io/yaml" // or gopkg.in/yaml.v3 — match the repo's existing dependency
)

// KeyOIDCConfig is the argo-rollouts-dashboard-cm key holding the OIDC config YAML.
const KeyOIDCConfig = "oidc.config"

// OIDCConfig is the dashboard's OIDC provider configuration.
type OIDCConfig struct {
	Issuer          string   `json:"issuer"`
	ClientID        string   `json:"clientID"`
	ClientSecret    string   `json:"clientSecret"`
	RequestedScopes []string `json:"requestedScopes,omitempty"`
}

var defaultOIDCScopes = []string{"openid", "profile", "email", "groups"}

// GetOIDCConfig parses the oidc.config entry. Returns (nil, false, nil) when not
// configured, and an error when present but malformed.
func (m *SettingsManager) GetOIDCConfig(ctx context.Context) (*OIDCConfig, bool, error) {
	data, err := m.configMapData(ctx, ConfigMapName)
	if err != nil {
		return nil, false, err
	}
	raw := data[KeyOIDCConfig]
	if raw == "" {
		return nil, false, nil
	}
	var cfg OIDCConfig
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", KeyOIDCConfig, err)
	}
	if len(cfg.RequestedScopes) == 0 {
		cfg.RequestedScopes = defaultOIDCScopes
	}
	return &cfg, true, nil
}
```

Note: `sigs.k8s.io/yaml` unmarshals via JSON tags (hence the `json:` struct tags). If the repo uses `gopkg.in/yaml.v3` instead, switch the tags to `yaml:` and the import accordingly. The implementer must match the repo's existing YAML library.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/settings/ -run TestGetOIDCConfig -v`
Expected: PASS (4 tests). `go mod tidy` if the YAML lib needs promoting.

- [ ] **Step 5: Commit**

```bash
git add server/auth/settings/oidc.go server/auth/settings/oidc_test.go go.mod go.sum
git commit -m "feat(settings): parse OIDC provider config from configmap"
```

---

### Task 2: Session token with groups

**Files:**
- Modify: `server/auth/session/sessionmanager.go`
- Test: `server/auth/session/groups_test.go`

**Interfaces:**
- Consumes: `SessionManager` (Plan 2).
- Produces:
  - `func (mgr *SessionManager) CreateWithGroups(subject string, groups []string, expiry time.Duration, id string) (string, error)` — an HS256 token like `Create`, plus a `groups` claim (JSON array of strings) when groups is non-empty.

- [ ] **Step 1: Write the failing test**

```go
package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateWithGroupsRoundTrip(t *testing.T) {
	mgr := NewSessionManager([]byte("test-signing-key-at-least-32bytes!!"))
	tok, err := mgr.CreateWithGroups("alice", []string{"dev", "ops"}, time.Hour, "jti-1")
	require.NoError(t, err)

	claims, err := mgr.Parse(tok)
	require.NoError(t, err)
	assert.Equal(t, "alice", claims["sub"])

	raw, ok := claims["groups"].([]interface{})
	require.True(t, ok, "groups claim present as array")
	assert.ElementsMatch(t, []interface{}{"dev", "ops"}, raw)
}

func TestCreateWithGroupsEmptyOmitsClaim(t *testing.T) {
	mgr := NewSessionManager([]byte("test-signing-key-at-least-32bytes!!"))
	tok, err := mgr.CreateWithGroups("bob", nil, time.Hour, "jti-2")
	require.NoError(t, err)

	claims, err := mgr.Parse(tok)
	require.NoError(t, err)
	_, hasGroups := claims["groups"]
	assert.False(t, hasGroups, "no groups claim when none supplied")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/session/ -run TestCreateWithGroups -v`
Expected: FAIL — `undefined: (*SessionManager).CreateWithGroups`.

- [ ] **Step 3: Write minimal implementation (append to `sessionmanager.go`)**

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/session/ -run TestCreateWithGroups -v`
Expected: PASS (2 tests). The token must satisfy `Parse` (issuer + exp present), which the test confirms.

- [ ] **Step 5: Commit**

```bash
git add server/auth/session/sessionmanager.go server/auth/session/groups_test.go
git commit -m "feat(session): mint session token carrying groups claim"
```

---

### Task 3: OIDC login/callback handlers (flow logic, injected dependencies)

**Files:**
- Create: `server/auth/oidc/oidc.go`
- Test: `server/auth/oidc/oidc_test.go`

**Interfaces:**
- Consumes: `auth.AuthCookieName`.
- Produces:
  - `type Claims struct { Subject string; Groups []string }`
  - `type AuthCodeURLBuilder interface { AuthCodeURL(state string) string }`
  - `type CodeExchanger interface { Exchange(ctx context.Context, code string) (rawIDToken string, err error) }`
  - `type IDTokenVerifier interface { Verify(ctx context.Context, rawIDToken string) (Claims, error) }`
  - `type TokenIssuerWithGroups interface { CreateWithGroups(subject string, groups []string, expiry time.Duration, id string) (string, error) }`
  - `type Handler struct { URLBuilder AuthCodeURLBuilder; Exchanger CodeExchanger; Verifier IDTokenVerifier; Issuer TokenIssuerWithGroups; TokenExpiry time.Duration; Secure bool }`
  - `func (h *Handler) Login(w http.ResponseWriter, r *http.Request)` — sets a random state cookie, redirects to the IdP.
  - `func (h *Handler) Callback(w http.ResponseWriter, r *http.Request)` — validates state, exchanges, verifies, mints a session cookie, redirects to `/`.
  - `const stateCookieName = "argorollouts.oidc-state"`

- [ ] **Step 1: Write the failing test**

```go
package oidc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/server/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeURLBuilder struct{}

func (fakeURLBuilder) AuthCodeURL(state string) string { return "https://idp/authorize?state=" + state }

type fakeExchanger struct {
	raw string
	err error
}

func (f fakeExchanger) Exchange(_ context.Context, _ string) (string, error) { return f.raw, f.err }

type fakeVerifier struct {
	claims Claims
	err    error
}

func (f fakeVerifier) Verify(_ context.Context, _ string) (Claims, error) { return f.claims, f.err }

type fakeIssuer struct{ token string }

func (f fakeIssuer) CreateWithGroups(_ string, _ []string, _ time.Duration, _ string) (string, error) {
	return f.token, nil
}

func newHandler() *Handler {
	return &Handler{
		URLBuilder:  fakeURLBuilder{},
		Exchanger:   fakeExchanger{raw: "raw-id-token"},
		Verifier:    fakeVerifier{claims: Claims{Subject: "alice", Groups: []string{"dev"}}},
		Issuer:      fakeIssuer{token: "session.jwt"},
		TokenExpiry: time.Hour,
	}
}

func cookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestLoginRedirectsAndSetsState(t *testing.T) {
	h := newHandler()
	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	rec := httptest.NewRecorder()
	h.Login(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	state := cookie(rec, stateCookieName)
	require.NotNil(t, state)
	assert.NotEmpty(t, state.Value)
	assert.True(t, state.HttpOnly)
	assert.Contains(t, rec.Header().Get("Location"), "state="+state.Value)
}

func doCallback(t *testing.T, h *Handler, stateCookieVal, queryState string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc&state="+queryState, nil)
	if stateCookieVal != "" {
		req.AddCookie(&http.Cookie{Name: stateCookieName, Value: stateCookieVal})
	}
	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	return rec
}

func TestCallbackSuccessSetsSessionCookie(t *testing.T) {
	h := newHandler()
	rec := doCallback(t, h, "xyz", "xyz")

	assert.Equal(t, http.StatusFound, rec.Code)
	sess := cookie(rec, auth.AuthCookieName)
	require.NotNil(t, sess)
	assert.Equal(t, "session.jwt", sess.Value)
	assert.True(t, sess.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, sess.SameSite)
}

func TestCallbackStateMismatchRejected(t *testing.T) {
	h := newHandler()
	rec := doCallback(t, h, "xyz", "DIFFERENT")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Nil(t, cookie(rec, auth.AuthCookieName), "no session on state mismatch")
}

func TestCallbackMissingStateCookieRejected(t *testing.T) {
	h := newHandler()
	rec := doCallback(t, h, "", "xyz")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCallbackExchangeErrorRejected(t *testing.T) {
	h := newHandler()
	h.Exchanger = fakeExchanger{err: errors.New("exchange failed")}
	rec := doCallback(t, h, "xyz", "xyz")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Nil(t, cookie(rec, auth.AuthCookieName))
}

func TestCallbackVerifyErrorRejected(t *testing.T) {
	h := newHandler()
	h.Verifier = fakeVerifier{err: errors.New("bad id token")}
	rec := doCallback(t, h, "xyz", "xyz")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Nil(t, cookie(rec, auth.AuthCookieName))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/oidc/ -v`
Expected: FAIL — `undefined: Handler`, `undefined: stateCookieName`, etc.

- [ ] **Step 3: Write minimal implementation**

```go
// Package oidc implements the dashboard's OIDC SSO login and callback flow.
package oidc

import (
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
	Secure      bool
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
		Secure:   h.Secure,
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
	// cookie survives the top-level redirect back from the IdP.
	http.SetCookie(w, &http.Cookie{Name: stateCookieName, Value: "", Path: "/", MaxAge: -1})
	http.SetCookie(w, &http.Cookie{
		Name:     auth.AuthCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(h.TokenExpiry.Seconds()),
	})
	http.Redirect(w, r, "/", http.StatusFound)
}
```

Note: the test file imports `context` (referenced via the interface signatures) — ensure the test's import block includes it; the implementation imports `context` indirectly through the interface definitions, so add `"context"` to `oidc.go`'s imports.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/oidc/ -v`
Expected: PASS (6 tests).

- [ ] **Step 5: Commit**

```bash
git add server/auth/oidc/oidc.go server/auth/oidc/oidc_test.go
git commit -m "feat(oidc): login/callback flow with CSRF state and session minting"
```

---

### Task 4: go-oidc adapter + server wiring

**Files:**
- Create: `server/auth/oidc/provider.go` (real go-oidc/oauth2 adapter)
- Modify: `server/auth_setup.go` (build the OIDC handler when configured)
- Modify: `server/server.go` (mount `/auth/login` + `/auth/callback`)
- Test: `server/auth/oidc/provider_test.go` (constructor wiring only — no network)

**Interfaces:**
- Consumes: `settings.OIDCConfig`, `go-oidc`, `oauth2`, the Task 3 interfaces, `session.SessionManager`.
- Produces:
  - `func NewProvider(ctx context.Context, cfg ProviderConfig) (*Handler, error)` — discovers the issuer, builds the oauth2 config + ID-token verifier, returns a ready `Handler`. `ProviderConfig` carries issuer/clientID/clientSecret/scopes/redirectURL/expiry/secure + the `TokenIssuerWithGroups`.
  - Concrete adapters implementing `AuthCodeURLBuilder`/`CodeExchanger`/`IDTokenVerifier` over `oauth2.Config` + `*oidc.IDTokenVerifier` (extract `sub` and `groups` claims in `Verify`).

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/coreos/go-oidc/v3/oidc && go mod tidy`
Expected: go-oidc/v3 added to `go.mod` (direct).

- [ ] **Step 2: Write the failing test (constructor wiring, offline)**

```go
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
```

- [ ] **Step 3: Implement `provider.go`**

```go
package oidc

import (
	"context"
	"fmt"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// ProviderConfig configures a real OIDC provider Handler.
type ProviderConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
	SessionIssuer      TokenIssuerWithGroups
	TokenExpiry  time.Duration
	Secure       bool
}

// oauth2Adapter implements AuthCodeURLBuilder + CodeExchanger over oauth2.Config.
type oauth2Adapter struct{ cfg *oauth2.Config }

func newOAuth2Adapter(clientID, clientSecret, authURL, tokenURL, redirectURL string, scopes []string) *oauth2Adapter {
	return &oauth2Adapter{cfg: &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     oauth2.Endpoint{AuthURL: authURL, TokenURL: tokenURL},
		RedirectURL:  redirectURL,
		Scopes:       scopes,
	}}
}

func (a *oauth2Adapter) AuthCodeURL(state string) string { return a.cfg.AuthCodeURL(state) }

func (a *oauth2Adapter) Exchange(ctx context.Context, code string) (string, error) {
	tok, err := a.cfg.Exchange(ctx, code)
	if err != nil {
		return "", err
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return "", fmt.Errorf("no id_token in token response")
	}
	return raw, nil
}

// verifierAdapter implements IDTokenVerifier over a go-oidc verifier.
type verifierAdapter struct{ v *gooidc.IDTokenVerifier }

func (a *verifierAdapter) Verify(ctx context.Context, raw string) (Claims, error) {
	idToken, err := a.v.Verify(ctx, raw)
	if err != nil {
		return Claims{}, err
	}
	var c struct {
		Subject string   `json:"sub"`
		Groups  []string `json:"groups"`
	}
	if err := idToken.Claims(&c); err != nil {
		return Claims{}, err
	}
	if c.Subject == "" {
		c.Subject = idToken.Subject
	}
	return Claims{Subject: c.Subject, Groups: c.Groups}, nil
}

// NewProvider discovers the issuer and returns a ready Handler.
func NewProvider(ctx context.Context, cfg ProviderConfig) (*Handler, error) {
	if cfg.Issuer == "" {
		return nil, fmt.Errorf("oidc: issuer is required")
	}
	provider, err := gooidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc: discover %s: %w", cfg.Issuer, err)
	}
	oa := newOAuth2Adapter(cfg.ClientID, cfg.ClientSecret, provider.Endpoint().AuthURL, provider.Endpoint().TokenURL, cfg.RedirectURL, cfg.Scopes)
	verifier := provider.Verifier(&gooidc.Config{ClientID: cfg.ClientID})
	return &Handler{
		URLBuilder:  oa,
		Exchanger:   oa,
		Verifier:    &verifierAdapter{v: verifier},
		Issuer:      cfg.SessionIssuer,
		TokenExpiry: cfg.TokenExpiry,
		Secure:      cfg.Secure,
	}, nil
}
```

(Add `"time"` to the import block.)

- [ ] **Step 4: Wire into the server**

In `server/auth_setup.go`, after building `sessionMgr` and reading settings, build the OIDC handler when configured and store it on `authComponents`:

```go
// add field to authComponents:
	oidc *oidc.Handler

// in setupAuth, after sessionMgr is built and rbacCfg loaded:
	var oidcHandler *oidc.Handler
	if oidcCfg, ok, ocErr := sm.GetOIDCConfig(ctx); ocErr != nil {
		return nil, fmt.Errorf("auth setup: oidc config: %w", ocErr)
	} else if ok {
		url, _ := sm.GetURL(ctx)
		h, err := oidc.NewProvider(ctx, oidc.ProviderConfig{
			Issuer:       oidcCfg.Issuer,
			ClientID:     oidcCfg.ClientID,
			ClientSecret: oidcCfg.ClientSecret,
			RedirectURL:  strings.TrimSuffix(url, "/") + "/auth/callback",
			Scopes:       oidcCfg.RequestedScopes,
			SessionIssuer:      sessionMgr,
			TokenExpiry:  tokenExpiry,
			Secure:       s.tlsConfig != nil,
		})
		if err != nil {
			return nil, fmt.Errorf("auth setup: oidc provider: %w", err)
		}
		oidcHandler = h
	}
	// include oidc: oidcHandler in the returned &authComponents{...}
```

In `server/server.go` `newHTTPServer`, mount the OIDC routes alongside login/logout (inside the `s.auth != nil` block):

```go
	if s.auth != nil && s.auth.oidc != nil {
		mux.HandleFunc("/auth/login", s.auth.oidc.Login)
		mux.HandleFunc("/auth/callback", s.auth.oidc.Callback)
	}
```

Add `/auth/login` and `/auth/callback` to the authN whitelist consideration: these are HTTP handlers, NOT gRPC methods, so they bypass the gRPC interceptors entirely — no whitelist entry needed (same as /api/login).

- [ ] **Step 5: Build + full regression**

Run: `go build ./... && go test ./server/... && go vet ./server/...`
Expected: module builds; the new oidc package + settings + session tests pass; existing server/auth tests still pass; none-mode and local-login unaffected.

- [ ] **Step 6: Commit**

```bash
git add server/auth/oidc/provider.go server/auth/oidc/provider_test.go server/auth_setup.go server/server.go go.mod go.sum
git commit -m "feat(oidc): go-oidc adapter and server wiring"
```

---

## Self-Review

**Spec coverage (vs design §4 OIDC + §5 SSO flow):**
- OIDC config from `oidc.config` → Task 1. ✅
- Session token carrying OIDC identity (sub+groups) so RBAC works unchanged → Task 2. ✅
- `/auth/login` + `/auth/callback` with CSRF state, code exchange, ID-token verify, session minting → Task 3 (logic) + Task 4 (real adapter). ✅
- go-oidc/oauth2 integration → Task 4. ✅
- Groups → RBAC: `CreateWithGroups` writes the `groups` claim that `auth.Groups` already consumes (Plan 4b). ✅
- Bundled Dex → NOT included; this plan supports any external OIDC IdP (Dex-as-a-pod is a manifests concern, Plan 7). Stated scope cut.
- `groups`/custom claim mapping, token refresh → `sub`/`groups` only for now; configurable claim names are a future enhancement.

**Placeholder scan:** No TBD/TODO; each step has complete code. The YAML-library choice is the one repo-existing decision the implementer confirms in Task 1 Step 2. ✅

**Type consistency:** `OIDCConfig`/`GetOIDCConfig`, `CreateWithGroups`, `Claims`/`Handler`/`Login`/`Callback`/`stateCookieName`, `NewProvider`/`ProviderConfig`/adapters, and `authComponents.oidc` consistent across Tasks 1–4. ✅

**Security notes:**
- CSRF: state cookie compared to query state on callback; mismatch/absent → 401, no session. Tested.
- Fail closed: exchange/verify errors → 401, no session cookie. Tested.
- Minted token is a standard dashboard HS256 token — same verification, expiry, and revocation story as local login; the IdP token never becomes the session credential.
- `SameSite=Lax` on the OIDC session + state cookies (the callback is a cross-site top-level redirect; Strict would drop them). Local login keeps Strict. Documented.
- ID-token audience is checked by `oidc.Config{ClientID}` in the verifier; signature via the provider JWKS.

**Carried forward / deferred:**
- Bundled Dex connector (LDAP/SAML/GitHub upstream) → Plan 7 manifests.
- Configurable username/groups claim names; token refresh; PKCE; `end_session_endpoint` (IdP logout).
- The provider discovery + JWKS path (Task 4 adapter) is only constructor-tested offline; true verification needs a live IdP or a mock OIDC server in an integration test (recommended follow-up).
