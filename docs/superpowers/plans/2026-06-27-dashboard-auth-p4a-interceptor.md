# Dashboard Auth — Plan 4a: gRPC Auth Interceptor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the authentication layer that turns a request token into verified claims on the context: a claims context helper, a token-from-metadata extractor, and gRPC unary + stream interceptors that enforce "valid token (or anonymous) required" before any RolloutService handler runs.

**Architecture:** A new `server/auth` package (sibling to the existing `server/auth/rbac`, `server/auth/session`, etc. subpackages — this is the package root). It depends only on a small `TokenVerifier` interface (which `session.SessionManager` satisfies), so it is fully unit-testable with a fake verifier and hand-built gRPC metadata — no running server. The interceptors extract a bearer token or auth cookie from incoming gRPC metadata, call `Verifier.Parse`, and inject the resulting claims into the context for downstream handlers (and Plan 4b's RBAC enforcement). A configurable whitelist skips auth for unauthenticated endpoints; an anonymous-enabled flag allows tokenless access when configured. Plan 4c wires the real `SessionManager` and settings into this.

**Tech Stack:** Go, `google.golang.org/grpc` (v1.80.0) + `grpc/metadata`, `grpc/codes`, `grpc/status`, `github.com/golang-jwt/jwt/v5`, `net/http` (cookie parsing), testify.

## Global Constraints

- Module path: `github.com/argoproj/argo-rollouts`. Package path: `server/auth` (package name `auth`).
- Claims type is `jwt.MapClaims` (from jwt/v5) — matches `session.SessionManager.Parse`'s return type.
- `TokenVerifier` interface: `Parse(token string) (jwt.MapClaims, error)`. Do NOT import the `session` package here — depend on the interface so the package stays decoupled and testable.
- Token sources, in priority order: gRPC metadata key `authorization` (strip a leading `Bearer ` prefix, case-insensitive), then metadata key `cookie` (parse the cookie named by `AuthCookieName`). First non-empty wins.
- `AuthCookieName = "argorollouts.token"`.
- Failure semantics: a present-but-invalid token always returns `status.Error(codes.Unauthenticated, ...)` — never falls through to anonymous. A missing token returns Unauthenticated UNLESS anonymous access is enabled, in which case the handler runs with empty (anonymous) claims injected.
- Whitelisted methods (by gRPC `FullMethod` string) skip all auth and run unchanged.
- The context key MUST be an unexported custom type (not a bare string) to avoid collisions — standard Go context-key hygiene.
- Interceptors must not mutate the incoming request or metadata; they only derive a new context.
- testify `assert`/`require`. Tests build metadata with `metadata.NewIncomingContext`.

---

### Task 1: Claims context + token extraction

**Files:**
- Create: `server/auth/claims.go`
- Create: `server/auth/token.go`
- Test: `server/auth/token_test.go`

**Interfaces:**
- Consumes: nothing (other packages).
- Produces:
  - `type TokenVerifier interface { Parse(token string) (jwt.MapClaims, error) }`
  - `const AuthCookieName = "argorollouts.token"`
  - `func ContextWithClaims(ctx context.Context, claims jwt.MapClaims) context.Context`
  - `func ClaimsFromContext(ctx context.Context) (jwt.MapClaims, bool)`
  - `func tokenFromContext(ctx context.Context) string` (unexported)

- [ ] **Step 1: Write the failing test**

```go
package auth

import (
	"context"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"
)

func TestClaimsRoundTrip(t *testing.T) {
	claims := jwt.MapClaims{"sub": "alice"}
	ctx := ContextWithClaims(context.Background(), claims)

	got, ok := ClaimsFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, "alice", got["sub"])
}

func TestClaimsFromContextAbsent(t *testing.T) {
	_, ok := ClaimsFromContext(context.Background())
	assert.False(t, ok)
}

func TestTokenFromAuthorizationHeader(t *testing.T) {
	md := metadata.Pairs("authorization", "Bearer abc.def.ghi")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	assert.Equal(t, "abc.def.ghi", tokenFromContext(ctx))
}

func TestTokenFromAuthorizationNoBearerPrefix(t *testing.T) {
	md := metadata.Pairs("authorization", "abc.def.ghi")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	assert.Equal(t, "abc.def.ghi", tokenFromContext(ctx))
}

func TestTokenFromCookie(t *testing.T) {
	md := metadata.Pairs("cookie", AuthCookieName+"=cookie.token.val; other=x")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	assert.Equal(t, "cookie.token.val", tokenFromContext(ctx))
}

func TestTokenAuthorizationBeatsCookie(t *testing.T) {
	md := metadata.Pairs(
		"authorization", "Bearer header.token",
		"cookie", AuthCookieName+"=cookie.token",
	)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	assert.Equal(t, "header.token", tokenFromContext(ctx))
}

func TestTokenAbsent(t *testing.T) {
	assert.Equal(t, "", tokenFromContext(context.Background()))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/ -v`
Expected: FAIL — `undefined: ContextWithClaims`, `undefined: tokenFromContext`, `undefined: AuthCookieName`, etc.

- [ ] **Step 3: Write minimal implementation**

`server/auth/claims.go`:

```go
// Package auth provides authentication for the Argo Rollouts dashboard server:
// extracting a request token, verifying it into claims, and enforcing that
// verification via gRPC interceptors.
package auth

import (
	"context"

	"github.com/golang-jwt/jwt/v5"
)

// TokenVerifier verifies a token string and returns its claims. It is
// satisfied by session.SessionManager.
type TokenVerifier interface {
	Parse(token string) (jwt.MapClaims, error)
}

// contextKey is an unexported type for context keys defined in this package,
// to prevent collisions with keys from other packages.
type contextKey string

const claimsContextKey contextKey = "auth.claims"

// ContextWithClaims returns a copy of ctx carrying claims.
func ContextWithClaims(ctx context.Context, claims jwt.MapClaims) context.Context {
	return context.WithValue(ctx, claimsContextKey, claims)
}

// ClaimsFromContext returns the claims carried by ctx, if any.
func ClaimsFromContext(ctx context.Context) (jwt.MapClaims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(jwt.MapClaims)
	return claims, ok
}
```

`server/auth/token.go`:

```go
package auth

import (
	"context"
	"net/http"
	"strings"

	"google.golang.org/grpc/metadata"
)

// AuthCookieName is the cookie that carries the dashboard session token.
const AuthCookieName = "argorollouts.token"

// tokenFromContext extracts a session token from incoming gRPC metadata. It
// checks the "authorization" header (stripping a "Bearer " prefix) first, then
// the auth cookie. It returns "" if no token is present.
func tokenFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	if vals := md.Get("authorization"); len(vals) > 0 && vals[0] != "" {
		return strings.TrimSpace(stripBearer(vals[0]))
	}
	if vals := md.Get("cookie"); len(vals) > 0 && vals[0] != "" {
		if tok := cookieValue(vals[0], AuthCookieName); tok != "" {
			return tok
		}
	}
	return ""
}

// stripBearer removes a leading "Bearer " prefix (case-insensitive) if present.
func stripBearer(v string) string {
	const prefix = "bearer "
	if len(v) >= len(prefix) && strings.EqualFold(v[:len(prefix)], prefix) {
		return v[len(prefix):]
	}
	return v
}

// cookieValue parses a Cookie header value and returns the named cookie's value.
func cookieValue(cookieHeader, name string) string {
	header := http.Header{}
	header.Add("Cookie", cookieHeader)
	req := http.Request{Header: header}
	c, err := req.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/ -v`
Expected: PASS (7 tests). Run `go mod tidy` only if the toolchain complains (grpc already a direct dep).

- [ ] **Step 5: Commit**

```bash
git add server/auth/claims.go server/auth/token.go server/auth/token_test.go
git commit -m "feat(auth): claims context and gRPC token extraction"
```

---

### Task 2: Unary auth interceptor

**Files:**
- Create: `server/auth/interceptor.go`
- Test: `server/auth/interceptor_test.go`

**Interfaces:**
- Consumes: `TokenVerifier`, `ContextWithClaims`, `tokenFromContext` (Task 1).
- Produces:
  - `type Interceptor struct { Verifier TokenVerifier; AnonymousEnabled bool; Whitelist map[string]bool }`
  - `func NewInterceptor(verifier TokenVerifier, anonymousEnabled bool, whitelist map[string]bool) *Interceptor`
  - `func (i *Interceptor) Unary(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error)`
  - unexported `func (i *Interceptor) authenticate(ctx context.Context, fullMethod string) (context.Context, error)` (shared by Unary and the Task 3 Stream interceptor).

- [ ] **Step 1: Write the failing test**

```go
package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// fakeVerifier returns fixed claims/err regardless of token, recording the
// token it was given.
type fakeVerifier struct {
	claims jwt.MapClaims
	err    error
	seen   string
}

func (f *fakeVerifier) Parse(token string) (jwt.MapClaims, error) {
	f.seen = token
	return f.claims, f.err
}

func ctxWithToken(token string) context.Context {
	md := metadata.Pairs("authorization", "Bearer "+token)
	return metadata.NewIncomingContext(context.Background(), md)
}

func okHandler(_ context.Context, _ interface{}) (interface{}, error) {
	return "ok", nil
}

func TestUnaryValidToken(t *testing.T) {
	v := &fakeVerifier{claims: jwt.MapClaims{"sub": "alice"}}
	i := NewInterceptor(v, false, nil)

	var seenClaims jwt.MapClaims
	handler := func(ctx context.Context, _ interface{}) (interface{}, error) {
		seenClaims, _ = ClaimsFromContext(ctx)
		return "ok", nil
	}
	resp, err := i.Unary(ctxWithToken("good"), nil, &grpc.UnaryServerInfo{FullMethod: "/rollout.RolloutService/PromoteRollout"}, handler)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.Equal(t, "alice", seenClaims["sub"])
	assert.Equal(t, "good", v.seen)
}

func TestUnaryInvalidTokenRejected(t *testing.T) {
	v := &fakeVerifier{err: errors.New("bad signature")}
	i := NewInterceptor(v, true, nil) // even with anonymous enabled, a BAD token is rejected

	_, err := i.Unary(ctxWithToken("forged"), nil, &grpc.UnaryServerInfo{FullMethod: "/rollout.RolloutService/PromoteRollout"}, okHandler)
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestUnaryMissingTokenNoAnonymous(t *testing.T) {
	v := &fakeVerifier{}
	i := NewInterceptor(v, false, nil)

	_, err := i.Unary(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/rollout.RolloutService/ListRolloutInfos"}, okHandler)
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestUnaryMissingTokenAnonymousAllowed(t *testing.T) {
	v := &fakeVerifier{}
	i := NewInterceptor(v, true, nil)

	var hadClaims bool
	handler := func(ctx context.Context, _ interface{}) (interface{}, error) {
		_, hadClaims = ClaimsFromContext(ctx)
		return "ok", nil
	}
	resp, err := i.Unary(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/rollout.RolloutService/ListRolloutInfos"}, handler)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.True(t, hadClaims, "anonymous request still gets (empty) claims injected")
	assert.Empty(t, v.seen, "verifier not called when no token present")
}

func TestUnaryWhitelistSkipsAuth(t *testing.T) {
	v := &fakeVerifier{err: errors.New("should not be called")}
	wl := map[string]bool{"/session.SessionService/Create": true}
	i := NewInterceptor(v, false, wl)

	resp, err := i.Unary(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/session.SessionService/Create"}, okHandler)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.Empty(t, v.seen, "whitelisted method must not invoke the verifier")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/ -run TestUnary -v`
Expected: FAIL — `undefined: NewInterceptor`.

- [ ] **Step 3: Write minimal implementation**

```go
package auth

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Interceptor authenticates incoming gRPC requests by verifying a session
// token into claims on the context.
type Interceptor struct {
	Verifier         TokenVerifier
	AnonymousEnabled bool
	Whitelist        map[string]bool
}

// NewInterceptor returns an Interceptor. whitelist maps gRPC FullMethod names
// that skip authentication; it may be nil.
func NewInterceptor(verifier TokenVerifier, anonymousEnabled bool, whitelist map[string]bool) *Interceptor {
	if whitelist == nil {
		whitelist = map[string]bool{}
	}
	return &Interceptor{Verifier: verifier, AnonymousEnabled: anonymousEnabled, Whitelist: whitelist}
}

// authenticate returns a context enriched with verified claims, or an error if
// authentication fails. Whitelisted methods return ctx unchanged.
func (i *Interceptor) authenticate(ctx context.Context, fullMethod string) (context.Context, error) {
	if i.Whitelist[fullMethod] {
		return ctx, nil
	}
	token := tokenFromContext(ctx)
	if token == "" {
		if i.AnonymousEnabled {
			return ContextWithClaims(ctx, jwt.MapClaims{}), nil
		}
		return nil, status.Error(codes.Unauthenticated, "no authentication token provided")
	}
	claims, err := i.Verifier.Parse(token)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid authentication token")
	}
	return ContextWithClaims(ctx, claims), nil
}

// Unary is a grpc.UnaryServerInterceptor enforcing authentication.
func (i *Interceptor) Unary(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	newCtx, err := i.authenticate(ctx, info.FullMethod)
	if err != nil {
		return nil, err
	}
	return handler(newCtx, req)
}
```

Note: add the `jwt` import (`"github.com/golang-jwt/jwt/v5"`) to this file for the `jwt.MapClaims{}` anonymous-claims literal.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/ -v`
Expected: PASS (Task 1 + Task 2 tests).

- [ ] **Step 5: Commit**

```bash
git add server/auth/interceptor.go server/auth/interceptor_test.go
git commit -m "feat(auth): unary gRPC authentication interceptor"
```

---

### Task 3: Stream auth interceptor

**Files:**
- Modify: `server/auth/interceptor.go`
- Test: `server/auth/stream_test.go`

**Interfaces:**
- Consumes: `Interceptor.authenticate` (Task 2).
- Produces:
  - `func (i *Interceptor) Stream(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error`
  - unexported `type wrappedStream struct { grpc.ServerStream; ctx context.Context }` overriding `Context()`.

- [ ] **Step 1: Write the failing test**

```go
package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// fakeServerStream is a minimal grpc.ServerStream carrying a context.
type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (f *fakeServerStream) Context() context.Context { return f.ctx }

func TestStreamValidTokenInjectsClaims(t *testing.T) {
	v := &fakeVerifier{claims: jwt.MapClaims{"sub": "alice"}}
	i := NewInterceptor(v, false, nil)

	md := metadata.Pairs("authorization", "Bearer good")
	base := metadata.NewIncomingContext(context.Background(), md)
	ss := &fakeServerStream{ctx: base}

	var seenSub interface{}
	handler := func(_ interface{}, stream grpc.ServerStream) error {
		c, _ := ClaimsFromContext(stream.Context())
		seenSub = c["sub"]
		return nil
	}
	err := i.Stream(nil, ss, &grpc.StreamServerInfo{FullMethod: "/rollout.RolloutService/WatchRolloutInfos"}, handler)
	require.NoError(t, err)
	assert.Equal(t, "alice", seenSub)
}

func TestStreamInvalidTokenRejected(t *testing.T) {
	v := &fakeVerifier{err: errors.New("bad")}
	i := NewInterceptor(v, false, nil)

	md := metadata.Pairs("authorization", "Bearer forged")
	ss := &fakeServerStream{ctx: metadata.NewIncomingContext(context.Background(), md)}

	err := i.Stream(nil, ss, &grpc.StreamServerInfo{FullMethod: "/rollout.RolloutService/WatchRolloutInfos"}, func(_ interface{}, _ grpc.ServerStream) error {
		return nil
	})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/ -run TestStream -v`
Expected: FAIL — `undefined: (*Interceptor).Stream`.

- [ ] **Step 3: Write minimal implementation (append to `interceptor.go`)**

```go
// wrappedStream overrides a ServerStream's context so downstream handlers see
// the authenticated claims.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }

// Stream is a grpc.StreamServerInterceptor enforcing authentication.
func (i *Interceptor) Stream(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	newCtx, err := i.authenticate(ss.Context(), info.FullMethod)
	if err != nil {
		return err
	}
	return handler(srv, &wrappedStream{ServerStream: ss, ctx: newCtx})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/ -v`
Expected: PASS (all Task 1–3 tests).

- [ ] **Step 5: Run full auth tree + vet**

Run: `go test ./server/auth/... && go vet ./server/auth/...`
Expected: ok across `auth`, `auth/password`, `auth/rbac`, `auth/session`, `auth/settings`; no vet complaints.

- [ ] **Step 6: Commit**

```bash
git add server/auth/interceptor.go server/auth/stream_test.go
git commit -m "feat(auth): stream gRPC authentication interceptor"
```

---

## Self-Review

**Spec coverage (vs design §3 + §5):**
- AuthN interceptor extracts token → validates → injects claims into ctx → Tasks 2 & 3. ✅
- Token from cookie or Authorization header (design §5 data flow) → Task 1. ✅
- Whitelisted no-auth endpoints (login, healthz) → Task 2 whitelist (the actual method names are supplied by Plan 4c wiring). ✅
- Anonymous fallback when enabled (design §7) → Task 2. ✅
- Claims-in-context for downstream RBAC enforcement → Task 1 `ContextWithClaims`/`ClaimsFromContext`. ✅
- RBAC enforcement itself (claims → resource/action/object → enforce) → NOT here; Plan 4b. This plan only does authN (who are you), not authZ (what may you do). Stated boundary, not a gap.
- Token auto-refresh (<5min) from argo-cd → deferred; out of scope (Plan 4c may add).

**Placeholder scan:** No TBD/TODO; every code step has complete code. ✅

**Type consistency:** `TokenVerifier`, `Interceptor`, `NewInterceptor`, `Unary`, `Stream`, `authenticate`, `wrappedStream`, `ContextWithClaims`, `ClaimsFromContext`, `tokenFromContext`, `AuthCookieName`, `cookieValue`, `stripBearer` consistent across Tasks 1–3. ✅

**Security notes:**
- A present-but-invalid token is rejected with Unauthenticated and never falls through to anonymous — verified by `TestUnaryInvalidTokenRejected` (anonymous enabled + bad token → still rejected). This prevents an attacker from downgrading to anonymous by sending garbage.
- The generic "invalid authentication token" message does not leak whether the failure was signature/expiry/issuer (avoids an oracle) — the specific reason stays in the verifier and is not surfaced to the caller.
- Context key is an unexported `contextKey` type, not a bare string — no cross-package collision or external spoofing of claims.
- The whitelist must be applied to the gRPC-gateway path too; since the dashboard multiplexes via cmux and the gateway dials the gRPC server in-process, gateway calls traverse this interceptor — Plan 4c confirms the login endpoint is an HTTP handler that does NOT go through the gateway, so it needs no whitelist entry; the whitelist covers any gRPC method that must stay unauthenticated (e.g. Version).

**Carried forward to Plan 4b/4c:**
- RBAC enforcement: a per-method map `FullMethod → (resource, action)` plus object (namespace/name) extraction from the request, enforced via `rbac.Enforcer.EnforceWithDefault` using the subject/groups from `ClaimsFromContext`.
- Anti-enumeration login (collapse VerifyUsernamePassword errors + dummy bcrypt) — Plan 4b.
- The concrete whitelist contents and the `Version`/healthz decision — Plan 4c wiring.
- Token auto-refresh and HTTP-cookie issuance on login — Plan 4b/4c.
