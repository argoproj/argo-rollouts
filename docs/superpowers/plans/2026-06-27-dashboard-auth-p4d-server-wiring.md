# Dashboard Auth — Plan 4d: Server Wiring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the completed `server/auth` library into the running dashboard: add a `--auth-mode` flag, assemble the auth components from Kubernetes settings, register the authN/authZ interceptors on the gRPC server, mount login/logout, and add stream-authorization to the two Watch handlers — so that with `--auth-mode=server` every RolloutService call is authenticated and authorized, while the default (`none`) keeps today's zero-auth local behavior byte-for-byte unchanged.

**Architecture:** One new file `server/auth_setup.go` (package `server`) holds `setupAuth(ctx)`, which builds — from the `KubeClientset` + `Namespace` already in `ServerOptions` — a `settings.SettingsManager`, a `session.SessionManager` (keyed by `GetSigningKey`), an `rbac.Enforcer` (loaded with `GetRBACConfig().PolicyCSV`), the authN `*auth.Interceptor`, the authZ `*auth.AuthzInterceptor`, and the `*auth.LoginHandler`. The components hang off a nil-able field on `ArgoRolloutsServer`; **nil means auth is disabled** and every wiring site is a guarded no-op, guaranteeing the `none` path is unchanged. `newGRPCServer` conditionally chains the interceptors; `newHTTPServer` conditionally mounts `/api/login` + `/api/logout`; the two stream handlers (`WatchRolloutInfo`, `WatchRolloutInfos`) call a guarded `authorizeStream` helper at their top (the unary authz interceptor cannot cover streams). TLS is explicitly out of scope (Plan 4e).

**Tech Stack:** Go, `google.golang.org/grpc`, cobra, the `server/auth` package and its subpackages (`settings`, `session`, `rbac`), `k8s.io/client-go/kubernetes/fake` for tests.

## Global Constraints

- Module `github.com/argoproj/argo-rollouts`. Files: `server/auth_setup.go` (new), `server/server.go` (edit), `pkg/kubectl-argo-rollouts/cmd/dashboard/dashboard.go` (edit).
- **Backward compatibility is sacred:** with `--auth-mode` unset/`none`, behavior is identical to today — no interceptors, no login routes, no authz in Watch handlers. Every auth wiring site MUST be guarded by `if s.auth != nil` (or auth-mode check). The default flag value is `none`.
- Auth modes: `none` (default, no auth) and `server` (full auth). Any other value is an error at startup.
- Interceptor order is load-bearing: `grpc.ChainUnaryInterceptor(s.auth.authn.Unary, s.auth.authz.Unary)` — authN first (it populates claims), authZ second (it reads them). Stream: `grpc.ChainStreamInterceptor(s.auth.authn.Stream)` (authZ for streams is done in the handlers).
- authN whitelist (methods that skip authentication): `"/rollout.RolloutService/Version"` only. `GetNamespace` requires authN (it reveals the served namespace) but no authZ (absent from the permission map). Login/logout are HTTP handlers that do NOT traverse the gRPC interceptors, so they need no whitelist entry.
- `setupAuth` fails loudly (returns error) if the signing key is absent/short (`GetSigningKey` already enforces ≥32 bytes) — no silent auth-disable. Signing-key auto-generation is deferred (Plan 4e).
- Token expiry: 24h (`24 * time.Hour`).
- `LoginHandler.Secure` is `false` for now (no TLS yet); Plan 4e sets it true under TLS.
- Reuse `auth.NewInterceptor`, `auth.NewAuthzInterceptor`, `auth.LoginHandler`, `auth.LogoutHandler`, `auth.EnforceClaims`, `auth.ClaimsFromContext`, `settings.NewSettingsManager`, `session.NewSessionManager`, `rbac.NewEnforcer`/`SetUserPolicy`. Do not reimplement.
- Tests: fake clientset seeding the dashboard Secret (`server.secretkey` ≥32 bytes) + rbac ConfigMap. testify.

---

### Task 1: Auth mode flag + ServerOptions field

**Files:**
- Modify: `server/server.go` (ServerOptions struct ~54-60)
- Modify: `pkg/kubectl-argo-rollouts/cmd/dashboard/dashboard.go`
- Test: `pkg/kubectl-argo-rollouts/cmd/dashboard/dashboard_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `ServerOptions.AuthMode string` field.
  - `--auth-mode` cobra flag (default `"none"`) wired into `ServerOptions.AuthMode`.
  - `const AuthModeNone = "none"`, `const AuthModeServer = "server"` in package `server`.

- [ ] **Step 1: Write the failing test**

```go
package dashboard

import (
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDashboardAuthModeFlagDefault(t *testing.T) {
	cmd := NewCmdDashboard(&options.ArgoRolloutsOptions{})
	f := cmd.Flags().Lookup("auth-mode")
	require.NotNil(t, f, "auth-mode flag must exist")
	assert.Equal(t, "none", f.DefValue, "auth-mode defaults to none (no behavior change)")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/kubectl-argo-rollouts/cmd/dashboard/ -run TestDashboardAuthModeFlag -v`
Expected: FAIL — flag `auth-mode` not found (Lookup returns nil).

- [ ] **Step 3: Implement**

In `server/server.go`, add the field to `ServerOptions` and the constants:

```go
type ServerOptions struct {
	KubeClientset     kubernetes.Interface
	RolloutsClientset rolloutclientset.Interface
	DynamicClientset  dynamic.Interface
	Namespace         string
	RootPath          string
	AuthMode          string
}
```

Add near the other server consts (e.g. after the `listenAddr`/`connectAddr` block):

```go
// Auth modes for the dashboard server.
const (
	AuthModeNone   = "none"
	AuthModeServer = "server"
)
```

In `dashboard.go`, add the flag var, set it on opts, and register the flag:

```go
// in RunE, add to the ServerOptions literal:
		opts := server.ServerOptions{
			Namespace:         namespace,
			KubeClientset:     kubeclientset,
			RolloutsClientset: rolloutclientset,
			DynamicClientset:  o.DynamicClientset(),
			RootPath:          rootPath,
			AuthMode:          authMode,
		}
```

```go
// declare alongside rootPath/port:
	var authMode string
// register alongside the other flags:
	cmd.Flags().StringVar(&authMode, "auth-mode", "none", "authentication mode: none (default, no auth) or server (require login + RBAC)")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/kubectl-argo-rollouts/cmd/dashboard/ -run TestDashboardAuthModeFlag -v && go build ./...`
Expected: PASS, and the whole module builds.

- [ ] **Step 5: Commit**

```bash
git add server/server.go pkg/kubectl-argo-rollouts/cmd/dashboard/
git commit -m "feat(server): add --auth-mode flag and ServerOptions.AuthMode"
```

---

### Task 2: Auth component assembly (setupAuth)

**Files:**
- Create: `server/auth_setup.go`
- Test: `server/auth_setup_test.go`

**Interfaces:**
- Consumes: `ServerOptions` (KubeClientset, Namespace, AuthMode); `settings`, `session`, `rbac`, `auth` packages.
- Produces:
  - `type authComponents struct { authn *auth.Interceptor; authz *auth.AuthzInterceptor; login *auth.LoginHandler; enforcer *rbac.Enforcer; defaultRole string }`
  - `func (s *ArgoRolloutsServer) setupAuth(ctx context.Context) (*authComponents, error)`
  - Adds an `auth *authComponents` field to `ArgoRolloutsServer` (nil = disabled).

- [ ] **Step 1: Write the failing test**

```go
package server

import (
	"context"
	"strings"
	"testing"

	"github.com/argoproj/argo-rollouts/server/auth/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestSetupAuthBuildsComponents(t *testing.T) {
	ns := "argo-rollouts"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: settings.SecretName, Namespace: ns},
		Data:       map[string][]byte{settings.KeyServerSignature: []byte(strings.Repeat("k", 32))},
	}
	rbacCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: settings.RBACConfigMapName, Namespace: ns},
		Data:       map[string]string{settings.KeyPolicyDefault: "role:readonly"},
	}
	client := k8sfake.NewSimpleClientset(secret, rbacCM)
	s := NewServer(ServerOptions{KubeClientset: client, Namespace: ns, AuthMode: AuthModeServer})

	comps, err := s.setupAuth(context.Background())
	require.NoError(t, err)
	require.NotNil(t, comps)
	assert.NotNil(t, comps.authn)
	assert.NotNil(t, comps.authz)
	assert.NotNil(t, comps.login)
	assert.NotNil(t, comps.enforcer)
	assert.Equal(t, "role:readonly", comps.defaultRole)
}

func TestSetupAuthErrorsWithoutSigningKey(t *testing.T) {
	ns := "argo-rollouts"
	client := k8sfake.NewSimpleClientset() // no secret => no signing key
	s := NewServer(ServerOptions{KubeClientset: client, Namespace: ns, AuthMode: AuthModeServer})

	_, err := s.setupAuth(context.Background())
	assert.Error(t, err, "missing/short signing key must fail loudly, not silently disable auth")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/ -run TestSetupAuth -v`
Expected: FAIL — `undefined: (*ArgoRolloutsServer).setupAuth`, `undefined: authComponents`.

- [ ] **Step 3: Implement**

Add the field to `ArgoRolloutsServer` in `server.go`:

```go
type ArgoRolloutsServer struct {
	Options ServerOptions
	stopCh  chan struct{}
	auth    *authComponents
}
```

Create `server/auth_setup.go`:

```go
package server

import (
	"context"
	"fmt"
	"time"

	"github.com/argoproj/argo-rollouts/server/auth"
	"github.com/argoproj/argo-rollouts/server/auth/rbac"
	"github.com/argoproj/argo-rollouts/server/auth/session"
	"github.com/argoproj/argo-rollouts/server/auth/settings"
)

// tokenExpiry is the lifetime of a dashboard session token.
const tokenExpiry = 24 * time.Hour

// authNWhitelist lists gRPC methods that skip authentication entirely.
var authNWhitelist = map[string]bool{
	"/rollout.RolloutService/Version": true,
}

// authComponents holds the assembled authentication/authorization machinery.
type authComponents struct {
	authn       *auth.Interceptor
	authz       *auth.AuthzInterceptor
	login       *auth.LoginHandler
	enforcer    *rbac.Enforcer
	defaultRole string
}

// setupAuth builds the auth components from the dashboard's Kubernetes settings.
// It returns an error (never a silently-disabled result) if required config —
// notably the signing key — is missing or invalid.
func (s *ArgoRolloutsServer) setupAuth(ctx context.Context) (*authComponents, error) {
	sm := settings.NewSettingsManager(s.Options.KubeClientset, s.Options.Namespace)

	key, err := sm.GetSigningKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth setup: %w", err)
	}
	sessionMgr := session.NewSessionManager(key)

	rbacCfg, err := sm.GetRBACConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth setup: load rbac config: %w", err)
	}
	enforcer, err := rbac.NewEnforcer()
	if err != nil {
		return nil, fmt.Errorf("auth setup: new enforcer: %w", err)
	}
	if err := enforcer.SetUserPolicy(rbacCfg.PolicyCSV); err != nil {
		return nil, fmt.Errorf("auth setup: load policy: %w", err)
	}

	anonymous, err := sm.AnonymousEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth setup: anonymous flag: %w", err)
	}

	return &authComponents{
		authn:       auth.NewInterceptor(sessionMgr, anonymous, authNWhitelist),
		authz:       auth.NewAuthzInterceptor(enforcer, rbacCfg.DefaultRole),
		login:       &auth.LoginHandler{Verifier: sm, Issuer: sessionMgr, TokenExpiry: tokenExpiry, Secure: false},
		enforcer:    enforcer,
		defaultRole: rbacCfg.DefaultRole,
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/ -run TestSetupAuth -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add server/server.go server/auth_setup.go server/auth_setup_test.go
git commit -m "feat(server): assemble auth components from k8s settings"
```

---

### Task 3: Wire interceptors, login routes, and stream authz

**Files:**
- Modify: `server/server.go` (Run, newGRPCServer, newHTTPServer, WatchRolloutInfo, WatchRolloutInfos)
- Test: `server/auth_setup_test.go` (add stream-authz tests)

**Interfaces:**
- Consumes: `authComponents` (Task 2), `auth.EnforceClaims`, `auth.ClaimsFromContext`, `auth.LogoutHandler`, `rbac` consts.
- Produces:
  - `Run` calls `setupAuth` when `AuthMode == AuthModeServer` and stores the result on `s.auth` (error → fatal).
  - `newGRPCServer` chains interceptors when `s.auth != nil`.
  - `newHTTPServer` mounts `/api/login` + `/api/logout` (rootPath-aware) when `s.auth != nil`, before the static handler.
  - `func (s *ArgoRolloutsServer) authorizeStream(ctx context.Context, action, object string) error` — guarded (nil auth → nil); both Watch handlers call it first.

- [ ] **Step 1: Write the failing test**

```go
package server

import (
	"context"
	"strings"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/server/auth"
	"github.com/argoproj/argo-rollouts/server/auth/rbac"
	"github.com/argoproj/argo-rollouts/server/auth/settings"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

// fakeWatchStream is a minimal RolloutService_WatchRolloutInfoServer carrying a context.
type fakeWatchStream struct {
	rollout.RolloutService_WatchRolloutInfoServer
	ctx context.Context
}

func (f *fakeWatchStream) Context() context.Context { return f.ctx }

func authedServer(t *testing.T, policyCSV string) *ArgoRolloutsServer {
	t.Helper()
	ns := "argo-rollouts"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: settings.SecretName, Namespace: ns},
		Data:       map[string][]byte{settings.KeyServerSignature: []byte(strings.Repeat("k", 32))},
	}
	rbacCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: settings.RBACConfigMapName, Namespace: ns},
		Data:       map[string]string{settings.KeyPolicyCSV: policyCSV},
	}
	client := k8sfake.NewSimpleClientset(secret, rbacCM)
	s := NewServer(ServerOptions{KubeClientset: client, Namespace: ns, AuthMode: AuthModeServer})
	comps, err := s.setupAuth(context.Background())
	require.NoError(t, err)
	s.auth = comps
	return s
}

func TestAuthorizeStreamDeniesUnpermitted(t *testing.T) {
	s := authedServer(t, "") // empty policy: nobody allowed
	ctx := auth.ContextWithClaims(context.Background(), jwt.MapClaims{"sub": "alice"})
	err := s.authorizeStream(ctx, rbac.ActionGet, "prod/web")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestAuthorizeStreamAllowsPermitted(t *testing.T) {
	s := authedServer(t, "g, alice, role:readonly") // readonly grants get on everything
	ctx := auth.ContextWithClaims(context.Background(), jwt.MapClaims{"sub": "alice"})
	err := s.authorizeStream(ctx, rbac.ActionGet, "prod/web")
	assert.NoError(t, err)
}

func TestAuthorizeStreamNilAuthIsNoop(t *testing.T) {
	s := NewServer(ServerOptions{}) // auth disabled (s.auth nil)
	err := s.authorizeStream(context.Background(), rbac.ActionGet, "prod/web")
	assert.NoError(t, err, "auth disabled => no authorization enforced")
}

// Exercises the REAL WatchRolloutInfo handler: a denied caller must be rejected
// before the handler touches any controller/client.
func TestWatchRolloutInfoDeniedBeforeWork(t *testing.T) {
	s := authedServer(t, "") // empty policy: alice denied
	ctx := auth.ContextWithClaims(context.Background(), jwt.MapClaims{"sub": "alice"})
	stream := &fakeWatchStream{ctx: ctx}
	err := s.WatchRolloutInfo(&rollout.RolloutInfoQuery{Namespace: "prod", Name: "web"}, stream)
	assert.Equal(t, codes.PermissionDenied, status.Code(err),
		"unauthorized watch must be denied at the handler, not served")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/ -run TestAuthorizeStream -v`
Expected: FAIL — `undefined: (*ArgoRolloutsServer).authorizeStream`.

- [ ] **Step 3: Implement**

Add `authorizeStream` to `server/auth_setup.go`:

```go
import (
	// add to the existing import block:
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	// (auth, rbac already conceptually available; ensure auth + rbac imported)
)

// authorizeStream enforces RBAC for a streaming RPC using the claims on ctx.
// It is a no-op when auth is disabled (s.auth == nil).
func (s *ArgoRolloutsServer) authorizeStream(ctx context.Context, action, object string) error {
	if s.auth == nil {
		return nil
	}
	claims, _ := auth.ClaimsFromContext(ctx)
	return auth.EnforceClaims(s.auth.enforcer, s.auth.defaultRole, claims, rbac.ResourceRollouts, action, object)
}
```

(Note: `codes`/`status` are NOT needed in auth_setup.go itself — `EnforceClaims` already returns a status error. Only import what you use; the test file imports codes/status.)

In `server.go`'s `Run`, build auth before constructing the servers:

```go
func (s *ArgoRolloutsServer) Run(ctx context.Context, port int, dashboard bool) {
	if s.Options.AuthMode == AuthModeServer {
		comps, err := s.setupAuth(ctx)
		errors.CheckError(err)
		s.auth = comps
	} else if s.Options.AuthMode != "" && s.Options.AuthMode != AuthModeNone {
		errors.CheckError(fmt.Errorf("invalid --auth-mode %q (want %q or %q)", s.Options.AuthMode, AuthModeNone, AuthModeServer))
	}

	httpServer := s.newHTTPServer(ctx, port)
	grpcServer := s.newGRPCServer()
	// ... rest unchanged ...
```

In `newGRPCServer`, chain interceptors when auth is enabled. **CRITICAL:** the service impl is a *separate* `ArgoRolloutsServer` instance — it MUST inherit `s.auth`, otherwise the stream handlers' `authorizeStream` (a method on that instance) sees `auth == nil` and silently skips authorization for the Watch RPCs.

```go
func (s *ArgoRolloutsServer) newGRPCServer() *grpc.Server {
	var opts []grpc.ServerOption
	if s.auth != nil {
		opts = append(opts,
			grpc.ChainUnaryInterceptor(s.auth.authn.Unary, s.auth.authz.Unary),
			grpc.ChainStreamInterceptor(s.auth.authn.Stream),
		)
	}
	grpcS := grpc.NewServer(opts...)
	rolloutsServer := NewServer(s.Options)
	rolloutsServer.auth = s.auth // share auth so stream handlers enforce authz
	rollout.RegisterRolloutServiceServer(grpcS, rolloutsServer)
	return grpcS
}
```

In `newHTTPServer`, mount login/logout before the static handler (replace the `mux.HandleFunc("/", ...)` tail):

```go
	mux.Handle(apiPath, apiHandler)
	if s.auth != nil {
		loginPath := "/api/login"
		logoutPath := "/api/logout"
		if s.Options.RootPath != "" {
			loginPath = path.Join("/", s.Options.RootPath, "api/login")
			logoutPath = path.Join("/", s.Options.RootPath, "api/logout")
		}
		mux.Handle(loginPath, s.auth.login)
		mux.HandleFunc(logoutPath, auth.LogoutHandler)
	}
	mux.HandleFunc("/", s.staticFileHttpHandler)
```

(Add `"github.com/argoproj/argo-rollouts/server/auth"` to `server.go`'s imports.)

At the top of `WatchRolloutInfo` (after `ctx := ws.Context()`):

```go
	if err := s.authorizeStream(ctx, rbac.ActionGet, q.GetNamespace()+"/"+q.GetName()); err != nil {
		return err
	}
```

At the top of `WatchRolloutInfos` (first statement of the method body, using `ws.Context()`):

```go
	if err := s.authorizeStream(ws.Context(), rbac.ActionGet, q.GetNamespace()+"/*"); err != nil {
		return err
	}
```

(Add `"github.com/argoproj/argo-rollouts/server/auth/rbac"` to `server.go`'s imports for `rbac.ActionGet`/`rbac.ResourceRollouts`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/ -run 'TestAuthorizeStream|TestWatchRolloutInfoDeniedBeforeWork' -v`
Expected: PASS (4 tests — 3 authorizeStream + the real-handler deny).

- [ ] **Step 5: Build + full server/auth regression**

Run: `go build ./... && go test ./server/... && go vet ./server/...`
Expected: module builds; existing `server` tests still pass (auth disabled paths unchanged); auth tree green.

- [ ] **Step 6: Commit**

```bash
git add server/server.go server/auth_setup.go server/auth_setup_test.go
git commit -m "feat(server): register auth interceptors, login routes, stream authz"
```

---

## Self-Review

**Spec coverage (vs design §3 + §5):**
- Server-mode toggle (`--auth-mode`) with unchanged `none` default → Task 1. ✅
- Build SettingsManager/SessionManager/Enforcer from k8s → Task 2. ✅
- authN before authZ interceptor chain on the gRPC server → Task 3. ✅
- Login/logout HTTP endpoints mounted → Task 3. ✅
- Stream authorization for the two Watch RPCs (the P4c carry-forward) → Task 3 `authorizeStream`. ✅
- Version whitelisted from authN; GetNamespace authN-but-not-authZ → Task 2 whitelist + permission-map absence. ✅
- TLS, signing-key auto-gen, settings caching, regex matchMode → explicitly DEFERRED to Plan 4e (stated, not gaps).

**Placeholder scan:** No TBD/TODO; each step gives the exact code and the precise insertion site (anchored to current line refs). ✅

**Type consistency:** `authComponents`, `setupAuth`, `authorizeStream`, `s.auth`, `AuthModeNone/Server`, and the consumed `auth.*`/`settings.*`/`session.*`/`rbac.*` symbols all match their definitions from Plans 1–4c. ✅

**Backward-compat & security notes:**
- Every wiring site is guarded by `s.auth != nil` (or auth-mode check); with `none`, `newGRPCServer` adds no interceptors, `newHTTPServer` mounts no login routes, and `authorizeStream` returns nil — today's behavior is byte-identical. Verified by `TestAuthorizeStreamNilAuthIsNoop` and the unchanged existing `server` tests.
- `setupAuth` returns an error (fatal in `Run`) on a missing/short signing key — auth never silently disables in server mode.
- Stream authz closes the exact gap P4c's final review flagged: both Watch handlers enforce `get` on their namespace/name (or namespace/\*) before touching any controller/client.
- Interceptor order (authN→authZ) is fixed in `ChainUnaryInterceptor`, matching the dependency (authZ reads claims authN sets).

**Carried forward to Plan 4e / later:**
- TLS (listener + `LoginHandler.Secure=true` + replace the gateway's `grpc.WithInsecure`), signing-key auto-generation+persist, `SettingsManager` caching/watch (currently re-reads per request — but `setupAuth` runs once at startup, so policy changes need a restart until a watch is added), regex `matchMode` enforcer-model swap, and live policy reload.
- E2E verification (start server-mode, login, exercise allow/deny) needs a cluster or envtest — recommend a follow-up integration test outside the unit suite.
