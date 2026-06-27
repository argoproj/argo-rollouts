# Dashboard Auth — Plan 4c: Authorization Interceptor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the gRPC unary authorization interceptor that, for each RolloutService call, looks up the required permission, extracts the target object (namespace/name) from the request, and enforces it against the RBAC enforcer using the caller's claims — turning the authenticated claims from Plan 4a into actual per-call access control.

**Architecture:** Two additions to the existing `server/auth` package. `objectFromRequest` derives the Casbin object string (`namespace/name`, or `namespace/*` for list/watch) from a request by **duck-typing** its getter methods (`GetNamespace`, `GetName`, `GetRollout`) — so the generic `auth` package never imports the rollout protobuf types, staying decoupled. `AuthzInterceptor` is a gRPC unary interceptor: it looks up `PermissionForMethod` (Plan 4b); for mapped methods it reads claims via `ClaimsFromContext` (Plan 4a) and calls `EnforceClaims` (Plan 4b) with the extracted object; unmapped methods (Version, GetNamespace) pass through (they require no authorization, only authentication, which the Plan 4a interceptor already enforced upstream). Fully unit-testable with fake requests and a fake enforcer. Stream authorization for the two Watch RPCs is handled at the handler level in the later wiring plan (the stream interceptor has no request to inspect); `EnforceClaims` is the shared primitive it will call.

**Tech Stack:** Go, `google.golang.org/grpc`, `github.com/golang-jwt/jwt/v5`, the existing `server/auth` package (PermissionForMethod, EnforceClaims, ClaimsFromContext, Enforcer), testify.

## Global Constraints

- Module path: `github.com/argoproj/argo-rollouts`. Package path: `server/auth` (package `auth`).
- `objectFromRequest` MUST NOT import the rollout protobuf package — extract via anonymous getter interfaces (`interface{ GetNamespace() string }`, etc.). This keeps `auth` free of a dependency on `pkg/apiclient/rollout`.
- Object format: `namespace + "/" + name`. Name resolution order: `GetName()` if present and non-empty, else `GetRollout()` if present and non-empty, else `"*"` (list/watch over a namespace). Namespace from `GetNamespace()` if present, else `""`.
- The interceptor authorizes ONLY methods present in `PermissionForMethod`; methods absent from the map pass through unauthorized-but-authenticated (Version/GetNamespace). It must NOT default-deny unmapped methods (that would break Version/GetNamespace) — but it also must never SKIP authz for a mapped method.
- Enforcement delegates entirely to `EnforceClaims` (deny-by-default, error→Internal already handled there); the interceptor returns its error unchanged and does NOT call the handler on denial.
- Reuse `Enforcer`, `EnforceClaims`, `PermissionForMethod`, `ClaimsFromContext` — do not reimplement.
- testify; fake request structs implementing the getter methods.

---

### Task 1: Object extraction from request (duck-typed)

**Files:**
- Create: `server/auth/object.go`
- Test: `server/auth/object_test.go`

**Interfaces:**
- Consumes: nothing (duck-typed).
- Produces:
  - `func objectFromRequest(req interface{}) string` (unexported)

- [ ] **Step 1: Write the failing test**

```go
package auth

import "testing"

import "github.com/stretchr/testify/assert"

type nsNameReq struct{ ns, name string }

func (r nsNameReq) GetNamespace() string { return r.ns }
func (r nsNameReq) GetName() string      { return r.name }

type nsRolloutReq struct{ ns, rollout string }

func (r nsRolloutReq) GetNamespace() string { return r.ns }
func (r nsRolloutReq) GetRollout() string   { return r.rollout }

type nsOnlyReq struct{ ns string }

func (r nsOnlyReq) GetNamespace() string { return r.ns }

// nameAndRolloutReq exposes BOTH GetName and GetRollout, to prove precedence.
type nameAndRolloutReq struct{ ns, name, rollout string }

func (r nameAndRolloutReq) GetNamespace() string { return r.ns }
func (r nameAndRolloutReq) GetName() string      { return r.name }
func (r nameAndRolloutReq) GetRollout() string   { return r.rollout }

func TestObjectFromNamespaceAndName(t *testing.T) {
	assert.Equal(t, "prod/web", objectFromRequest(nsNameReq{ns: "prod", name: "web"}))
}

func TestObjectFromNamespaceAndRollout(t *testing.T) {
	// SetImage/Undo requests expose the rollout name via GetRollout().
	assert.Equal(t, "prod/api", objectFromRequest(nsRolloutReq{ns: "prod", rollout: "api"}))
}

func TestObjectNamespaceOnlyIsWildcardName(t *testing.T) {
	// List/Watch over a namespace => name wildcard.
	assert.Equal(t, "prod/*", objectFromRequest(nsOnlyReq{ns: "prod"}))
}

func TestObjectEmptyRequest(t *testing.T) {
	// A request exposing no getters => "/*".
	assert.Equal(t, "/*", objectFromRequest(struct{}{}))
}

func TestObjectNamePreferredOverRollout(t *testing.T) {
	// If both GetName and GetRollout exist, a non-empty GetName wins.
	assert.Equal(t, "prod/web", objectFromRequest(nameAndRolloutReq{ns: "prod", name: "web", rollout: "api"}))
}

func TestObjectFallsBackToRolloutWhenNameEmpty(t *testing.T) {
	// Both getters present but GetName empty => GetRollout is used.
	assert.Equal(t, "prod/api", objectFromRequest(nameAndRolloutReq{ns: "prod", name: "", rollout: "api"}))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/ -run TestObject -v`
Expected: FAIL — `undefined: objectFromRequest`.

- [ ] **Step 3: Write minimal implementation**

```go
package auth

// objectFromRequest derives the RBAC object ("namespace/name") for an RPC
// request by duck-typing its getter methods, so this package does not depend on
// the rollout protobuf types. Name resolves from GetName, then GetRollout, then
// "*" (namespace-wide, e.g. list/watch). Namespace resolves from GetNamespace.
func objectFromRequest(req interface{}) string {
	namespace := ""
	if r, ok := req.(interface{ GetNamespace() string }); ok {
		namespace = r.GetNamespace()
	}

	name := ""
	if r, ok := req.(interface{ GetName() string }); ok {
		name = r.GetName()
	}
	if name == "" {
		if r, ok := req.(interface{ GetRollout() string }); ok {
			name = r.GetRollout()
		}
	}
	if name == "" {
		name = "*"
	}

	return namespace + "/" + name
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/ -run TestObject -v`
Expected: PASS (6 tests).

- [ ] **Step 5: Commit**

```bash
git add server/auth/object.go server/auth/object_test.go
git commit -m "feat(auth): duck-typed RBAC object extraction from requests"
```

---

### Task 2: Authorization unary interceptor

**Files:**
- Create: `server/auth/authz_interceptor.go`
- Test: `server/auth/authz_interceptor_test.go`

**Interfaces:**
- Consumes: `Enforcer`, `EnforceClaims`, `PermissionForMethod`, `ClaimsFromContext`, `objectFromRequest`.
- Produces:
  - `type AuthzInterceptor struct { Enforcer Enforcer; DefaultRole string }`
  - `func NewAuthzInterceptor(enforcer Enforcer, defaultRole string) *AuthzInterceptor`
  - `func (a *AuthzInterceptor) Unary(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error)`

- [ ] **Step 1: Write the failing test**

```go
package auth

import (
	"context"
	"testing"

	"github.com/argoproj/argo-rollouts/server/auth/rbac"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// allowEnforcer allows a fixed (sub,res,act,obj) tuple.
type allowEnforcer struct{ allowKey string }

func (e allowEnforcer) EnforceWithDefault(_ , sub, res, act, obj string) (bool, error) {
	return sub+"|"+res+"|"+act+"|"+obj == e.allowKey, nil
}

func promoteReq(ns, name string) interface{} { return nsNameReq{ns: ns, name: name} }

func claimsCtx(sub string) context.Context {
	return ContextWithClaims(context.Background(), jwt.MapClaims{"sub": sub})
}

func TestAuthzAllowsPermittedCall(t *testing.T) {
	e := allowEnforcer{allowKey: "alice|rollouts|promote|prod/web"}
	a := NewAuthzInterceptor(e, "")

	called := false
	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		called = true
		return "ok", nil
	}
	resp, err := a.Unary(claimsCtx("alice"), promoteReq("prod", "web"),
		&grpc.UnaryServerInfo{FullMethod: "/rollout.RolloutService/PromoteRollout"}, handler)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.True(t, called)
}

func TestAuthzDeniesUnpermittedCall(t *testing.T) {
	e := allowEnforcer{allowKey: "nobody|x|x|x"}
	a := NewAuthzInterceptor(e, "")

	called := false
	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		called = true
		return "ok", nil
	}
	_, err := a.Unary(claimsCtx("alice"), promoteReq("prod", "web"),
		&grpc.UnaryServerInfo{FullMethod: "/rollout.RolloutService/PromoteRollout"}, handler)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, called, "handler must not run on denial")
}

func TestAuthzUnmappedMethodPassesThrough(t *testing.T) {
	// Version is not in the permission map => no authz, handler runs.
	e := allowEnforcer{allowKey: "no|no|no|no"}
	a := NewAuthzInterceptor(e, "")

	called := false
	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		called = true
		return "v", nil
	}
	resp, err := a.Unary(claimsCtx("alice"), struct{}{},
		&grpc.UnaryServerInfo{FullMethod: "/rollout.RolloutService/Version"}, handler)
	require.NoError(t, err)
	assert.Equal(t, "v", resp)
	assert.True(t, called)
}

func TestAuthzObjectScoping(t *testing.T) {
	// alice may promote in prod/* only; prod/web allowed, dev/web denied.
	e := allowEnforcer{allowKey: "alice|rollouts|promote|prod/web"}
	a := NewAuthzInterceptor(e, "")
	handler := func(_ context.Context, _ interface{}) (interface{}, error) { return "ok", nil }

	_, err := a.Unary(claimsCtx("alice"), promoteReq("dev", "web"),
		&grpc.UnaryServerInfo{FullMethod: "/rollout.RolloutService/PromoteRollout"}, handler)
	assert.Equal(t, codes.PermissionDenied, status.Code(err), "wrong namespace denied")
}

func TestAuthzUsesRbacConstants(t *testing.T) {
	// Sanity: the SetImage method maps to setimage and reads the rollout name.
	perm, ok := PermissionForMethod("/rollout.RolloutService/SetRolloutImage")
	require.True(t, ok)
	assert.Equal(t, rbac.ResourceRollouts, perm.Resource)
	assert.Equal(t, rbac.ActionSetImage, perm.Action)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/ -run TestAuthz -v`
Expected: FAIL — `undefined: NewAuthzInterceptor`.

- [ ] **Step 3: Write minimal implementation**

```go
package auth

import (
	"context"

	"google.golang.org/grpc"
)

// AuthzInterceptor enforces RBAC on RolloutService calls using the claims placed
// on the context by the authentication interceptor.
type AuthzInterceptor struct {
	Enforcer    Enforcer
	DefaultRole string
}

// NewAuthzInterceptor returns an AuthzInterceptor backed by enforcer. defaultRole
// is applied for callers without an explicit grant (may be "").
func NewAuthzInterceptor(enforcer Enforcer, defaultRole string) *AuthzInterceptor {
	return &AuthzInterceptor{Enforcer: enforcer, DefaultRole: defaultRole}
}

// Unary is a grpc.UnaryServerInterceptor enforcing authorization. Methods absent
// from the permission map require no authorization and pass through.
func (a *AuthzInterceptor) Unary(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	perm, ok := PermissionForMethod(info.FullMethod)
	if !ok {
		return handler(ctx, req)
	}
	claims, _ := ClaimsFromContext(ctx)
	object := objectFromRequest(req)
	if err := EnforceClaims(a.Enforcer, a.DefaultRole, claims, perm.Resource, perm.Action, object); err != nil {
		return nil, err
	}
	return handler(ctx, req)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/ -run TestAuthz -v`
Expected: PASS (5 tests).

- [ ] **Step 5: Run full auth tree + vet**

Run: `go test ./server/auth/... && go vet ./server/auth/...`
Expected: ok across `auth` and all subpackages; no vet complaints.

- [ ] **Step 6: Commit**

```bash
git add server/auth/authz_interceptor.go server/auth/authz_interceptor_test.go
git commit -m "feat(auth): unary RBAC authorization interceptor"
```

---

## Self-Review

**Spec coverage (vs design §3 + §6):**
- Per-call RBAC enforcement (claims → resource/action/object → enforce) → Task 2. ✅
- Object (namespace/name) extraction from typed requests, incl. SetImage/Undo's `GetRollout` and list/watch namespace-wildcard → Task 1. ✅
- Unmapped informational methods (Version/GetNamespace) pass through → Task 2. ✅
- Decoupling: `auth` does not import rollout protobuf types → duck-typed getters in Task 1. ✅
- Stream (Watch) authorization → NOT here; the stream interceptor cannot see the request, so the two Watch handlers call `EnforceClaims` directly in the wiring plan (P4d). Stated boundary.

**Placeholder scan:** No TBD/TODO; every code step has complete code. ✅

**Type consistency:** `objectFromRequest`, `AuthzInterceptor`, `NewAuthzInterceptor`, `Unary`, and the consumed `Enforcer`/`EnforceClaims`/`PermissionForMethod`/`ClaimsFromContext` are consistent and already defined in Plans 4a/4b. ✅

**Security notes:**
- Deny-by-default flows from `EnforceClaims` (Plan 4b, already reviewed); the interceptor never calls the handler on a non-nil enforce error — verified by `TestAuthzDeniesUnpermittedCall` (`called == false`).
- Object scoping is enforced: a correct namespace/name is required, so a `prod/*` grant does not authorize `dev/web` — verified by `TestAuthzObjectScoping`.
- Unmapped methods pass through by design (Version/GetNamespace); they are still authenticated by the Plan 4a interceptor upstream. The map is exhaustive over mutating/read RPCs (Plan 4b), so no privileged method is silently skipped.

**Carried forward to Plan 4d (server wiring — the integration finale):**
- Register, in `newGRPCServer` (server.go:127), `grpc.ChainUnaryInterceptor(authnInterceptor.Unary, authzInterceptor.Unary)` and `grpc.ChainStreamInterceptor(authnInterceptor.Stream, ...)` — authN before authZ — ONLY when `--auth-mode=server`.
- Stream authz for `WatchRolloutInfo`/`WatchRolloutInfos`: call `EnforceClaims` at the top of those two handlers (server.go) with the object from the query (the stream interceptor lacks the request).
- Build the dependencies in `NewServer`/`Run`: `settings.SettingsManager` (CredentialVerifier, defaultRole/anonymous source), `session.SessionManager` (TokenIssuer + TokenVerifier from `GetSigningKey`), `rbac.Enforcer` (`SetUserPolicy` from `GetRBACConfig`), plus an authN-whitelist for non-RPC HTTP paths.
- Mount `LoginHandler` at `/api/login`, `LogoutHandler` at `/api/logout` in `newHTTPServer` before the static handler; make logout `Secure`-aware under TLS.
- `--auth-mode` flag in `cmd/dashboard/dashboard.go`; add auth fields to `ServerOptions`; local mode unchanged.
- TLS in server mode (replace `grpc.WithInsecure()` for the gateway dial / add a TLS listener); settings caching/watch; regex matchMode model swap; signing-key auto-gen.
