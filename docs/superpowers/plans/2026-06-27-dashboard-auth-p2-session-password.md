# Dashboard Auth — Plan 2: Session + Password Packages Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build two self-contained packages — `server/auth/password` (bcrypt hashing/verification) and `server/auth/session` (HS256 JWT session manager) — that later tasks use to authenticate local users and issue/validate dashboard session tokens.

**Architecture:** Two pure libraries, no server/HTTP/k8s. `password` wraps `golang.org/x/crypto/bcrypt` with a length guard. `session` wraps `github.com/golang-jwt/jwt/v5`: a `SessionManager` holds the HS256 signing key and exposes `Create` (sign a token with registered claims) and `Parse` (verify signature, algorithm, and issuer, reject expired). Mirrors argo-cd's `util/session` + `util/password` semantics. Foundation for Plan 3 (settings/account store) and Plan 4 (auth interceptor).

**Tech Stack:** Go, `github.com/golang-jwt/jwt/v5` (v5.3.1, already in module graph), `golang.org/x/crypto/bcrypt` (x/crypto v0.53.0, already in module graph), `github.com/stretchr/testify`.

## Global Constraints

- Module path: `github.com/argoproj/argo-rollouts`.
- Package paths: `server/auth/password`, `server/auth/session`.
- JWT signing algorithm: **HS256 only**. `Parse` MUST reject any other `alg` (including `none` and RS256) — guard with both a keyfunc method-type check and `jwt.WithValidMethods([]string{"HS256"})`.
- Session token issuer constant: `SessionManagerClaimsIssuer = "argo-rollouts"`. `Parse` MUST validate the issuer.
- bcrypt cost: `bcrypt.DefaultCost`. Enforce bcrypt's 72-byte input limit explicitly (reject longer with a clear error) rather than letting bcrypt silently truncate.
- Both jwt/v5 and x/crypto are currently transitive (`// indirect`). After importing, run `go mod tidy` so they move to the direct-require block (avoid the `// indirect`-on-direct-dep issue from Plan 1).
- testify `assert`/`require` for tests. Table-driven where it fits.
- Deny/fail closed: any verification error returns a non-nil error; never return a valid result on a parse/verify failure path.

---

### Task 1: Password hashing and verification

**Files:**
- Create: `server/auth/password/password.go`
- Test: `server/auth/password/password_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `const MaxPasswordLength = 72`
  - `func HashPassword(password string) (string, error)` — bcrypt hash at `DefaultCost`; errors if `len(password) > MaxPasswordLength`.
  - `func VerifyPassword(password, hashedPassword string) error` — nil if match, non-nil otherwise.

- [ ] **Step 1: Write the failing test**

```go
package password

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashThenVerify(t *testing.T) {
	hash, err := HashPassword("correct horse")
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.NotEqual(t, "correct horse", hash, "hash must not equal plaintext")
	assert.NoError(t, VerifyPassword("correct horse", hash))
}

func TestVerifyWrongPassword(t *testing.T) {
	hash, err := HashPassword("correct horse")
	require.NoError(t, err)
	assert.Error(t, VerifyPassword("battery staple", hash))
}

func TestHashDistinctSalts(t *testing.T) {
	h1, err := HashPassword("same")
	require.NoError(t, err)
	h2, err := HashPassword("same")
	require.NoError(t, err)
	assert.NotEqual(t, h1, h2, "bcrypt salts each hash differently")
}

func TestHashRejectsOverLongPassword(t *testing.T) {
	_, err := HashPassword(strings.Repeat("a", MaxPasswordLength+1))
	require.Error(t, err)
}

func TestVerifyRejectsGarbageHash(t *testing.T) {
	assert.Error(t, VerifyPassword("x", "not-a-bcrypt-hash"))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/password/ -v`
Expected: FAIL — `undefined: HashPassword`, `undefined: VerifyPassword`, `undefined: MaxPasswordLength`.

- [ ] **Step 3: Write minimal implementation**

```go
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
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/password/ -v`
Expected: PASS (5 tests). If x/crypto needs promoting, run `go mod tidy` after.

- [ ] **Step 5: Commit**

```bash
go mod tidy
git add server/auth/password/ go.mod go.sum
git commit -m "feat(auth): bcrypt password hashing and verification"
```

---

### Task 2: Session manager — token creation

**Files:**
- Create: `server/auth/session/sessionmanager.go`
- Test: `server/auth/session/sessionmanager_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `const SessionManagerClaimsIssuer = "argo-rollouts"`
  - `type SessionManager struct { signingKey []byte }`
  - `func NewSessionManager(signingKey []byte) *SessionManager`
  - `func (mgr *SessionManager) Create(subject string, expiry time.Duration, id string) (string, error)` — signs an HS256 JWT with registered claims (Issuer, Subject, IssuedAt, ExpiresAt=now+expiry, ID).

- [ ] **Step 1: Write the failing test**

```go
package session

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateReturnsThreePartToken(t *testing.T) {
	mgr := NewSessionManager([]byte("test-signing-key"))
	tok, err := mgr.Create("alice", time.Hour, "jti-1")
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(tok, "."), "JWT has three dot-separated parts")
}

func TestCreateDistinctSubjectsDistinctTokens(t *testing.T) {
	mgr := NewSessionManager([]byte("test-signing-key"))
	a, err := mgr.Create("alice", time.Hour, "jti-1")
	require.NoError(t, err)
	b, err := mgr.Create("bob", time.Hour, "jti-2")
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/session/ -v`
Expected: FAIL — `undefined: NewSessionManager`, `undefined: SessionManager`.

- [ ] **Step 3: Write minimal implementation**

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/session/ -v`
Expected: PASS (2 tests). If jwt/v5 needs promoting, run `go mod tidy` after.

- [ ] **Step 5: Commit**

```bash
go mod tidy
git add server/auth/session/ go.mod go.sum
git commit -m "feat(auth): session manager HS256 token creation"
```

---

### Task 3: Session manager — token parsing and verification

**Files:**
- Modify: `server/auth/session/sessionmanager.go`
- Test: `server/auth/session/parse_test.go`

**Interfaces:**
- Consumes: `SessionManager`, `Create`, `SessionManagerClaimsIssuer` (Task 2).
- Produces:
  - `func (mgr *SessionManager) Parse(tokenString string) (jwt.MapClaims, error)` — verifies HS256 signature, restricts valid methods to HS256, validates issuer, rejects expired tokens; returns the claims on success.

- [ ] **Step 1: Write the failing test**

```go
package session

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRoundTrip(t *testing.T) {
	mgr := NewSessionManager([]byte("test-signing-key"))
	tok, err := mgr.Create("alice", time.Hour, "jti-1")
	require.NoError(t, err)

	claims, err := mgr.Parse(tok)
	require.NoError(t, err)
	assert.Equal(t, "alice", claims["sub"])
	assert.Equal(t, SessionManagerClaimsIssuer, claims["iss"])
}

func TestParseRejectsExpired(t *testing.T) {
	mgr := NewSessionManager([]byte("test-signing-key"))
	tok, err := mgr.Create("alice", -time.Hour, "jti-1") // already expired
	require.NoError(t, err)

	_, err = mgr.Parse(tok)
	assert.Error(t, err)
}

func TestParseRejectsWrongKey(t *testing.T) {
	signer := NewSessionManager([]byte("key-A"))
	verifier := NewSessionManager([]byte("key-B"))
	tok, err := signer.Create("alice", time.Hour, "jti-1")
	require.NoError(t, err)

	_, err = verifier.Parse(tok)
	assert.Error(t, err, "signature forged under a different key must be rejected")
}

func TestParseRejectsWrongIssuer(t *testing.T) {
	mgr := NewSessionManager([]byte("test-signing-key"))
	// Hand-craft a token with a different issuer but the correct key.
	claims := jwt.RegisteredClaims{
		Issuer:    "someone-else",
		Subject:   "alice",
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-signing-key"))
	require.NoError(t, err)

	_, err = mgr.Parse(tok)
	assert.Error(t, err, "issuer mismatch must be rejected")
}

func TestParseRejectsAlgNone(t *testing.T) {
	mgr := NewSessionManager([]byte("test-signing-key"))
	// alg=none token (unsigned) must never be accepted.
	claims := jwt.RegisteredClaims{
		Issuer:    SessionManagerClaimsIssuer,
		Subject:   "attacker",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = mgr.Parse(tok)
	assert.Error(t, err, "alg=none must be rejected (algorithm-confusion guard)")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/session/ -run TestParse -v`
Expected: FAIL — `undefined: (*SessionManager).Parse`.

- [ ] **Step 3: Write minimal implementation (append to `sessionmanager.go`)**

```go
// Parse verifies tokenString's HS256 signature and issuer and returns its
// claims. It rejects any non-HS256 algorithm (including "none"), a bad
// signature, a wrong issuer, or an expired token.
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
	)
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	return claims, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/session/ -v`
Expected: PASS (all Task 2 + Task 3 tests).

- [ ] **Step 5: Run full auth tree + vet**

Run: `go test ./server/auth/... && go vet ./server/auth/...`
Expected: ok across `password`, `rbac` (Plan 1), `session`; no vet complaints.

- [ ] **Step 6: Commit**

```bash
git add server/auth/session/
git commit -m "feat(auth): session token parsing with HS256/issuer/expiry validation"
```

---

## Self-Review

**Spec coverage (vs design §4 — vendored `session` + `password`):**
- bcrypt password hashing/verification → Task 1. ✅
- JWT HS256 sign with issuer `argo-rollouts`, expiry → Task 2. ✅
- JWT verify: signature + algorithm + issuer + expiry → Task 3. ✅
- Account token (apiKey capability) + VerifyUsernamePassword against an account store → NOT here; depends on the settings/account store, which is Plan 3. Flagged as a known forward dependency, not a gap in P2's scope.
- Auto-refresh (<5min remaining) from argo-cd → deferred; not needed until the interceptor (Plan 4). Out of P2 scope.

**Placeholder scan:** No TBD/TODO; every code step has complete code. ✅

**Type consistency:** `HashPassword`/`VerifyPassword`/`MaxPasswordLength` (password pkg); `SessionManager`/`NewSessionManager`/`Create`/`Parse`/`SessionManagerClaimsIssuer` (session pkg) used consistently across Tasks 1–3. ✅

**Security note:** The alg-confusion guard is doubled (keyfunc method-type check AND `WithValidMethods`) intentionally — defense in depth against the classic JWT `alg=none`/RS256-as-HMAC attack. Task 3's `TestParseRejectsAlgNone` and `TestParseRejectsWrongKey` exercise it.

**Carried forward to Plan 3:** account store (`accounts.<name>.password`, admin password from secret), `VerifyUsernamePassword(username, password)`, account/apiKey token capability, and configurable token expiry from `argo-rollouts-dashboard-cm`.
