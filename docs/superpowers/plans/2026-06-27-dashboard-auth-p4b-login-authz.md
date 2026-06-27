# Dashboard Auth — Plan 4b: Login + RBAC Authorization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the two pieces that make the authenticated dashboard actually usable and safe: an HTTP **login/logout** handler (verify credentials → issue a session cookie, with anti-enumeration), and the **authorization** layer (a RolloutService method→permission map plus an `EnforceClaims` helper that gates each call against the RBAC enforcer using the caller's claims).

**Architecture:** Three new files in the existing `server/auth` package. `login.go` exposes a `LoginHandler` (plain `http.Handler`) wired to two small interfaces — `CredentialVerifier` (satisfied by `settings.SettingsManager.VerifyUsernamePassword`) and `TokenIssuer` (satisfied by `session.SessionManager.Create`) — so it is unit-testable with fakes. `authz.go` extracts subject/groups from `jwt.MapClaims` and enforces a permission against an `Enforcer` interface (satisfied by `rbac.Enforcer`). `permissions.go` is a static table mapping each RolloutService gRPC `FullMethod` to a `(resource, action)`. Object (namespace/name) extraction from typed requests is intentionally deferred to Plan 4c, which holds the concrete request types and the interceptor wiring; this plan delivers the reusable, fully-tested primitives.

**Tech Stack:** Go, `net/http`, `encoding/json`, `crypto/rand`, `github.com/golang-jwt/jwt/v5`, `google.golang.org/grpc/codes` + `status`, the existing `server/auth/password` and `server/auth/rbac` packages, testify.

## Global Constraints

- Module path: `github.com/argoproj/argo-rollouts`. Package path: `server/auth` (package `auth`).
- **Anti-enumeration (login):** every credential failure — unknown user, disabled account, wrong password — returns the SAME generic `401` body ("invalid username or password"), and the handler runs one bcrypt comparison against a constant dummy hash on the failure path to flatten timing against the wrong-password path. Never leak which factor failed.
- Session cookie: name `AuthCookieName` (from Plan 4a, `argorollouts.token`), `HttpOnly`, `SameSite=Strict`, `Path=/`, `Secure` toggled by handler config (true under TLS). `MaxAge` = token expiry seconds. Logout clears it (`MaxAge=-1`).
- Token id (jti) is 16 random bytes from `crypto/rand`, hex-encoded — never a predictable counter.
- **Authorization fail-closed:** `EnforceClaims` returns `codes.PermissionDenied` unless some subject (the `sub` claim or one of the `groups`) is explicitly allowed; an enforcer error returns `codes.Internal` (not allow). Anonymous (empty claims) is enforced as the empty subject so the configured default role (and only that) can apply.
- The method→permission table covers exactly the mutating + read RolloutService RPCs. `Version` and `GetNamespace` are deliberately ABSENT (informational, no authz) — Plan 4c decides their authN whitelist status.
- Reuse `rbac` action/resource constants in the table (no magic strings). Reuse `password.VerifyPassword` for the dummy-hash timing burn.
- Do not import `settings`, `session`, or `rbac.Enforcer` concretely where an interface suffices — depend on local interfaces for testability. (`permissions.go` may import `rbac` for the action/resource consts — those are data, not behavior.)
- testify; `httptest` for the login handler.

---

### Task 1: Authorization claims helper

**Files:**
- Create: `server/auth/authz.go`
- Test: `server/auth/authz_test.go`

**Interfaces:**
- Consumes: `jwt.MapClaims`.
- Produces:
  - `type Enforcer interface { EnforceWithDefault(defaultRole, sub, res, act, obj string) (bool, error) }`
  - `func Subject(claims jwt.MapClaims) string`
  - `func Groups(claims jwt.MapClaims) []string`
  - `func EnforceClaims(enforcer Enforcer, defaultRole string, claims jwt.MapClaims, resource, action, object string) error`

- [ ] **Step 1: Write the failing test**

```go
package auth

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// recordingEnforcer allows (sub,res,act,obj) tuples present in its allow set.
type recordingEnforcer struct {
	allow map[string]bool // key: sub|res|act|obj
	err   error
	calls []string
}

func (e *recordingEnforcer) EnforceWithDefault(defaultRole, sub, res, act, obj string) (bool, error) {
	if e.err != nil {
		return false, e.err
	}
	key := sub + "|" + res + "|" + act + "|" + obj
	e.calls = append(e.calls, key)
	// defaultRole fallback: if sub not allowed, try the default role's key.
	if e.allow[key] {
		return true, nil
	}
	if defaultRole != "" {
		return e.allow[defaultRole+"|"+res+"|"+act+"|"+obj], nil
	}
	return false, nil
}

func TestSubjectAndGroups(t *testing.T) {
	claims := jwt.MapClaims{"sub": "alice", "groups": []interface{}{"dev", "ops"}}
	assert.Equal(t, "alice", Subject(claims))
	assert.Equal(t, []string{"dev", "ops"}, Groups(claims))

	assert.Equal(t, "", Subject(jwt.MapClaims{}))
	assert.Nil(t, Groups(jwt.MapClaims{}))
	assert.Equal(t, "", Subject(nil))
}

func TestEnforceClaimsAllowsSubject(t *testing.T) {
	e := &recordingEnforcer{allow: map[string]bool{"alice|rollouts|promote|prod/web": true}}
	claims := jwt.MapClaims{"sub": "alice"}
	assert.NoError(t, EnforceClaims(e, "", claims, "rollouts", "promote", "prod/web"))
}

func TestEnforceClaimsAllowsViaGroup(t *testing.T) {
	e := &recordingEnforcer{allow: map[string]bool{"ops|rollouts|abort|prod/web": true}}
	claims := jwt.MapClaims{"sub": "alice", "groups": []interface{}{"dev", "ops"}}
	assert.NoError(t, EnforceClaims(e, "", claims, "rollouts", "abort", "prod/web"))
}

func TestEnforceClaimsDeniedIsPermissionDenied(t *testing.T) {
	e := &recordingEnforcer{allow: map[string]bool{}}
	claims := jwt.MapClaims{"sub": "alice"}
	err := EnforceClaims(e, "", claims, "rollouts", "delete", "prod/web")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestEnforceClaimsEnforcerErrorIsInternal(t *testing.T) {
	e := &recordingEnforcer{err: assertAnErr{}}
	claims := jwt.MapClaims{"sub": "alice"}
	err := EnforceClaims(e, "", claims, "rollouts", "get", "prod/web")
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestEnforceClaimsAnonymousUsesDefaultRole(t *testing.T) {
	e := &recordingEnforcer{allow: map[string]bool{"role:readonly|rollouts|get|prod/web": true}}
	// empty claims => empty subject => default role applies
	assert.NoError(t, EnforceClaims(e, "role:readonly", jwt.MapClaims{}, "rollouts", "get", "prod/web"))
	// no default role => denied
	err := EnforceClaims(e, "", jwt.MapClaims{}, "rollouts", "get", "prod/web")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

type assertAnErr struct{}

func (assertAnErr) Error() string { return "boom" }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/ -run 'TestSubject|TestEnforceClaims' -v`
Expected: FAIL — `undefined: Subject`, `undefined: EnforceClaims`, `undefined: Enforcer`.

- [ ] **Step 3: Write minimal implementation**

```go
package auth

import (
	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Enforcer authorizes a subject against a resource/action/object. It is
// satisfied by *rbac.Enforcer.
type Enforcer interface {
	EnforceWithDefault(defaultRole, sub, res, act, obj string) (bool, error)
}

// Subject returns the "sub" claim, or "" if absent.
func Subject(claims jwt.MapClaims) string {
	if claims == nil {
		return ""
	}
	sub, _ := claims["sub"].(string)
	return sub
}

// Groups returns the string values of the "groups" claim, or nil.
func Groups(claims jwt.MapClaims) []string {
	if claims == nil {
		return nil
	}
	raw, ok := claims["groups"].([]interface{})
	if !ok {
		return nil
	}
	groups := make([]string, 0, len(raw))
	for _, g := range raw {
		if s, ok := g.(string); ok {
			groups = append(groups, s)
		}
	}
	return groups
}

// EnforceClaims authorizes the request. It tries the subject and each group as
// a Casbin subject (each with the default-role fallback). It returns nil if any
// is allowed, codes.PermissionDenied if none is, or codes.Internal on enforcer
// error. With empty claims it enforces the empty subject so only the configured
// default role can grant access.
func EnforceClaims(enforcer Enforcer, defaultRole string, claims jwt.MapClaims, resource, action, object string) error {
	subjects := make([]string, 0, 4)
	if sub := Subject(claims); sub != "" {
		subjects = append(subjects, sub)
	}
	subjects = append(subjects, Groups(claims)...)
	if len(subjects) == 0 {
		subjects = append(subjects, "") // anonymous: only default role can apply
	}
	for _, s := range subjects {
		allowed, err := enforcer.EnforceWithDefault(defaultRole, s, resource, action, object)
		if err != nil {
			return status.Error(codes.Internal, "authorization error")
		}
		if allowed {
			return nil
		}
	}
	return status.Error(codes.PermissionDenied, "permission denied")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/ -run 'TestSubject|TestEnforceClaims' -v`
Expected: PASS (6 tests).

- [ ] **Step 5: Commit**

```bash
git add server/auth/authz.go server/auth/authz_test.go
git commit -m "feat(auth): claims-based RBAC enforcement helper"
```

---

### Task 2: RolloutService method→permission table

**Files:**
- Create: `server/auth/permissions.go`
- Test: `server/auth/permissions_test.go`

**Interfaces:**
- Consumes: `rbac` resource/action constants.
- Produces:
  - `type Permission struct { Resource string; Action string }`
  - `func PermissionForMethod(fullMethod string) (Permission, bool)`

- [ ] **Step 1: Write the failing test**

```go
package auth

import (
	"testing"

	"github.com/argoproj/argo-rollouts/server/auth/rbac"
	"github.com/stretchr/testify/assert"
)

func TestPermissionForMutatingMethods(t *testing.T) {
	cases := map[string]Permission{
		"/rollout.RolloutService/PromoteRollout":  {rbac.ResourceRollouts, rbac.ActionPromote},
		"/rollout.RolloutService/AbortRollout":    {rbac.ResourceRollouts, rbac.ActionAbort},
		"/rollout.RolloutService/RetryRollout":    {rbac.ResourceRollouts, rbac.ActionRetry},
		"/rollout.RolloutService/RestartRollout":  {rbac.ResourceRollouts, rbac.ActionRestart},
		"/rollout.RolloutService/SetRolloutImage": {rbac.ResourceRollouts, rbac.ActionSetImage},
		"/rollout.RolloutService/UndoRollout":     {rbac.ResourceRollouts, rbac.ActionUndo},
	}
	for method, want := range cases {
		got, ok := PermissionForMethod(method)
		assert.True(t, ok, method)
		assert.Equal(t, want, got, method)
	}
}

func TestPermissionForReadMethods(t *testing.T) {
	for _, m := range []string{
		"/rollout.RolloutService/GetRolloutInfo",
		"/rollout.RolloutService/ListRolloutInfos",
		"/rollout.RolloutService/WatchRolloutInfo",
		"/rollout.RolloutService/WatchRolloutInfos",
	} {
		got, ok := PermissionForMethod(m)
		assert.True(t, ok, m)
		assert.Equal(t, rbac.ResourceRollouts, got.Resource, m)
		assert.Equal(t, rbac.ActionGet, got.Action, m)
	}
}

func TestPermissionAbsentForInformationalMethods(t *testing.T) {
	for _, m := range []string{
		"/rollout.RolloutService/Version",
		"/rollout.RolloutService/GetNamespace",
		"/rollout.RolloutService/Unknown",
	} {
		_, ok := PermissionForMethod(m)
		assert.False(t, ok, m)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/ -run TestPermission -v`
Expected: FAIL — `undefined: PermissionForMethod`, `undefined: Permission`.

- [ ] **Step 3: Write minimal implementation**

```go
package auth

import "github.com/argoproj/argo-rollouts/server/auth/rbac"

// Permission is the (resource, action) an RPC requires.
type Permission struct {
	Resource string
	Action   string
}

// methodPermissions maps RolloutService gRPC FullMethod names to the permission
// they require. Informational RPCs (Version, GetNamespace) are intentionally
// absent — they require no authorization.
var methodPermissions = map[string]Permission{
	"/rollout.RolloutService/GetRolloutInfo":    {rbac.ResourceRollouts, rbac.ActionGet},
	"/rollout.RolloutService/ListRolloutInfos":  {rbac.ResourceRollouts, rbac.ActionGet},
	"/rollout.RolloutService/WatchRolloutInfo":  {rbac.ResourceRollouts, rbac.ActionGet},
	"/rollout.RolloutService/WatchRolloutInfos": {rbac.ResourceRollouts, rbac.ActionGet},
	"/rollout.RolloutService/RestartRollout":    {rbac.ResourceRollouts, rbac.ActionRestart},
	"/rollout.RolloutService/PromoteRollout":    {rbac.ResourceRollouts, rbac.ActionPromote},
	"/rollout.RolloutService/AbortRollout":      {rbac.ResourceRollouts, rbac.ActionAbort},
	"/rollout.RolloutService/SetRolloutImage":   {rbac.ResourceRollouts, rbac.ActionSetImage},
	"/rollout.RolloutService/UndoRollout":       {rbac.ResourceRollouts, rbac.ActionUndo},
	"/rollout.RolloutService/RetryRollout":      {rbac.ResourceRollouts, rbac.ActionRetry},
}

// PermissionForMethod returns the permission required by a gRPC FullMethod, and
// whether the method requires authorization at all.
func PermissionForMethod(fullMethod string) (Permission, bool) {
	p, ok := methodPermissions[fullMethod]
	return p, ok
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/ -run TestPermission -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add server/auth/permissions.go server/auth/permissions_test.go
git commit -m "feat(auth): RolloutService method to RBAC permission map"
```

---

### Task 3: Login + logout HTTP handlers

**Files:**
- Create: `server/auth/login.go`
- Test: `server/auth/login_test.go`

**Interfaces:**
- Consumes: `AuthCookieName` (Plan 4a), `password.HashPassword`/`password.VerifyPassword`.
- Produces:
  - `type CredentialVerifier interface { VerifyUsernamePassword(ctx context.Context, username, password string) error }`
  - `type TokenIssuer interface { Create(subject string, expiry time.Duration, id string) (string, error) }`
  - `type LoginHandler struct { Verifier CredentialVerifier; Issuer TokenIssuer; TokenExpiry time.Duration; Secure bool }`
  - `func (h *LoginHandler) ServeHTTP(w http.ResponseWriter, r *http.Request)`
  - `func LogoutHandler(w http.ResponseWriter, r *http.Request)`

- [ ] **Step 1: Write the failing test**

```go
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCredVerifier struct{ err error }

func (f fakeCredVerifier) VerifyUsernamePassword(_ context.Context, _, _ string) error {
	return f.err
}

type fakeIssuer struct {
	token    string
	err      error
	seenSub  string
	seenExp  time.Duration
}

func (f *fakeIssuer) Create(subject string, expiry time.Duration, _ string) (string, error) {
	f.seenSub = subject
	f.seenExp = expiry
	return f.token, f.err
}

func postLogin(h http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestLoginSuccessSetsCookieAndToken(t *testing.T) {
	h := &LoginHandler{Verifier: fakeCredVerifier{}, Issuer: &fakeIssuer{token: "tok.123"}, TokenExpiry: time.Hour}
	rec := postLogin(h, `{"username":"alice","password":"s3cret"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "tok.123", resp["token"])

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, AuthCookieName, cookies[0].Name)
	assert.Equal(t, "tok.123", cookies[0].Value)
	assert.True(t, cookies[0].HttpOnly)
	assert.Equal(t, http.SameSiteStrictMode, cookies[0].SameSite)
}

func TestLoginBadCredentialsGeneric(t *testing.T) {
	// Different underlying errors must produce identical responses (no enumeration).
	for _, underlying := range []error{
		errors.New(`account "ghost" not found`),
		errors.New(`account "bob" is disabled`),
		errors.New("crypto/bcrypt: hashedPassword is not the hash of the given password"),
	} {
		h := &LoginHandler{Verifier: fakeCredVerifier{err: underlying}, Issuer: &fakeIssuer{token: "x"}, TokenExpiry: time.Hour}
		rec := postLogin(h, `{"username":"u","password":"p"}`)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Empty(t, rec.Result().Cookies(), "no cookie on failure")
		assert.NotContains(t, rec.Body.String(), "not found")
		assert.NotContains(t, rec.Body.String(), "disabled")
		assert.Equal(t, "invalid username or password\n", rec.Body.String())
	}
}

func TestLoginRejectsNonPost(t *testing.T) {
	h := &LoginHandler{Verifier: fakeCredVerifier{}, Issuer: &fakeIssuer{}, TokenExpiry: time.Hour}
	req := httptest.NewRequest(http.MethodGet, "/api/login", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestLoginRejectsMalformedBody(t *testing.T) {
	h := &LoginHandler{Verifier: fakeCredVerifier{}, Issuer: &fakeIssuer{}, TokenExpiry: time.Hour}
	rec := postLogin(h, `{not json`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestLoginSubjectAndExpiryPassedToIssuer(t *testing.T) {
	iss := &fakeIssuer{token: "t"}
	h := &LoginHandler{Verifier: fakeCredVerifier{}, Issuer: iss, TokenExpiry: 2 * time.Hour}
	postLogin(h, `{"username":"carol","password":"p"}`)
	assert.Equal(t, "carol", iss.seenSub)
	assert.Equal(t, 2*time.Hour, iss.seenExp)
}

func TestLogoutClearsCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	rec := httptest.NewRecorder()
	LogoutHandler(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, AuthCookieName, cookies[0].Name)
	assert.Equal(t, "", cookies[0].Value)
	assert.True(t, cookies[0].MaxAge < 0, "logout cookie expires immediately")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/ -run 'TestLogin|TestLogout' -v`
Expected: FAIL — `undefined: LoginHandler`, `undefined: LogoutHandler`.

- [ ] **Step 3: Write minimal implementation**

```go
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/argoproj/argo-rollouts/server/auth/password"
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

// dummyHash equalizes the failure-path timing against a real bcrypt comparison,
// so an unknown user is not distinguishable from a wrong password by latency.
var dummyHash, _ = password.HashPassword("argo-rollouts-login-timing-equalizer")

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *LoginHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.Verifier.VerifyUsernamePassword(r.Context(), req.Username, req.Password); err != nil {
		// Burn one bcrypt comparison to flatten timing, then return a generic
		// error that does not reveal which factor failed.
		_ = password.VerifyPassword(req.Password, dummyHash)
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/ -run 'TestLogin|TestLogout' -v`
Expected: PASS (6 tests).

- [ ] **Step 5: Run full auth tree + vet**

Run: `go test ./server/auth/... && go vet ./server/auth/...`
Expected: ok across `auth` and all subpackages; no vet complaints.

- [ ] **Step 6: Commit**

```bash
git add server/auth/login.go server/auth/login_test.go
git commit -m "feat(auth): login/logout HTTP handlers with anti-enumeration"
```

---

## Self-Review

**Spec coverage (vs design §5 data flow + §6 RBAC + §9 security):**
- Login: username/password → verify → issue JWT → Set-Cookie → Task 3. ✅
- Anti-enumeration (generic error + dummy bcrypt) — carried-forward P3 review requirement → Task 3. ✅
- Logout clears cookie → Task 3. ✅
- RBAC enforcement from claims (subject + groups, default role, deny-by-default) → Task 1. ✅
- Per-method permission mapping (FullMethod → resource/action) → Task 2. ✅
- Object (namespace/name) extraction from typed requests → NOT here; Plan 4c (it owns the request types: note Promote/Abort/Retry/Restart/GetRolloutInfo use `GetName`, SetRolloutImage/UndoRollout use `GetRollout`, list/watch use `GetNamespace` only → object `ns/*`). Stated boundary.
- OIDC group mapping/scopes → Plan 5 (Groups() already reads the `groups` claim that OIDC will populate).

**Placeholder scan:** No TBD/TODO; every code step has complete code. ✅

**Type consistency:** `Enforcer`, `Subject`, `Groups`, `EnforceClaims`, `Permission`, `PermissionForMethod`, `CredentialVerifier`, `TokenIssuer`, `LoginHandler`, `LogoutHandler`, `randomID`, `dummyHash` consistent across Tasks 1–3. `Enforcer.EnforceWithDefault` signature matches `rbac.Enforcer.EnforceWithDefault` (Plan 1) exactly. ✅

**Security notes:**
- Anti-enumeration: the three distinct `settings.VerifyUsernamePassword` errors (not-found / disabled / mismatch) all collapse to one `401` body, verified by `TestLoginBadCredentialsGeneric`. The dummy bcrypt on the failure path flattens the unknown-user (no bcrypt in settings) vs wrong-password (one bcrypt in settings) timing difference. KNOWN residual: the wrong-password path runs two bcrypts (settings + dummy) vs one for unknown-user — a small skew in the opposite, non-revealing direction; acceptable and noted.
- Deny-by-default authz: `EnforceClaims` only returns nil on an explicit allow; enforcer errors map to `codes.Internal` (fail closed, not allow).
- Cookie is `HttpOnly` + `SameSite=Strict` (CSRF/JS-exfil resistance); `Secure` is config-gated so Plan 4c sets it true under TLS.
- Token jti from `crypto/rand`, not guessable.

**Carried forward to Plan 4c (wiring):**
- An authorization interceptor (or per-handler calls) that: looks up `PermissionForMethod(info.FullMethod)`; if found, extracts `object = namespace + "/" + name` from the typed request (using the per-type getters above; list/watch → `namespace/*`); reads claims via `ClaimsFromContext`; calls `EnforceClaims(enforcer, defaultRole, claims, perm.Resource, perm.Action, object)`. Methods absent from the table require no authz.
- Mounting `LoginHandler` at `/api/login` and `LogoutHandler` at `/api/logout` in `newHTTPServer` (before the static handler), and the authN whitelist for these gRPC-independent HTTP paths.
- Setting `LoginHandler.Secure = true` when TLS is enabled.
- Wiring `settings.SettingsManager` as `CredentialVerifier`/source of `defaultRole`, `session.SessionManager` as `TokenIssuer`, `rbac.Enforcer` as `Enforcer`.
