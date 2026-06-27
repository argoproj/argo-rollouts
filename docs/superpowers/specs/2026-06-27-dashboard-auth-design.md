# Argo Rollouts Dashboard — Authentication & Authorization Design

**Date:** 2026-06-27
**Status:** Draft (design approved, pending spec review)
**Target repo:** `argo-rollouts`

## 1. Problem & Goal

The Argo Rollouts dashboard (`kubectl argo rollouts dashboard`) ships today with
**zero authentication or authorization**. It binds `0.0.0.0:3100`, serves a
gRPC-gateway API (`/api/`) and static UI (`/`) with no auth middleware, and uses
`grpc.WithInsecure()` (no TLS). This is acceptable for its current model — a
**local, single-user tool** that runs on a developer's laptop and acts with that
developer's kubeconfig credentials.

**Goal:** Add full-parity authentication & authorization (matching Argo CD's
stack: OIDC/SSO, local accounts, JWT sessions, Casbin RBAC, TLS) so the dashboard
can run as a **shared, in-cluster, multi-user server** — as a real upstream
feature contributed to `argo-rollouts`.

### Evidence: current state (argo-rollouts)

| Concern | Location | Finding |
|---|---|---|
| Dashboard cmd | `pkg/kubectl-argo-rollouts/cmd/dashboard/dashboard.go:21-54` | Flags only `--port` (default 3100), `--root-path`. No auth flags. |
| Bind address | `server/server.go:79-87` | Binds `0.0.0.0`, `net.JoinHostPort`. |
| Routes | `server/server.go:115-122` | `/api/` (gRPC gateway) + `/` (static) — no middleware. |
| Static handler | `server/server_static.go:29` | No auth checks. |
| Transport | `server/server.go:104` | `grpc.WithInsecure()` — no TLS. |
| Auth keywords | `server/`, `cmd/dashboard/` | Zero matches for auth/token/login/rbac/authoriz/authenticat/middleware/cors. |

### Reference: Argo CD auth architecture (source of vendored code)

| Mechanism | Key files (argo-cd) | Purpose |
|---|---|---|
| JWT sessions | `util/session/sessionmanager.go` | HS256 sign/verify, auto-refresh, issuer `argocd` |
| Local accounts | `util/settings/accounts.go`, `util/password/password.go` | bcrypt password storage |
| OIDC/SSO | `util/oidc/*`, `util/dex/*` | OAuth2 flow, external IdP, optional bundled Dex |
| API keys | `util/session/sessionmanager.go` (subject `name:capability`) | account tokens |
| RBAC | `util/rbac/rbac.go`, `assets/model.conf`, `assets/builtin-policy.csv` | Casbin `(sub,res,act,obj)` |
| RBAC enforcement | `server/rbacpolicy/rbacpolicy.go` | claims → groups → roles, project scoping |
| Auth interceptor | `server/server.go:Authenticate()`, gRPC `grpc_auth.UnaryServerInterceptor` | token → claims in ctx |
| TLS | `server/server.go` (cmux, cert reload) | transport security |
| Config | `argocd-cm`, `argocd-secret`, `argocd-rbac-cm` | all auth knobs |

## 2. Decisions (locked during brainstorming)

1. **Goal:** Real upstream feature, full-parity auth.
2. **Deployment model — Option A:** Add a NEW shared-server mode. Existing local
   mode (`kubectl argo rollouts dashboard`) stays **unchanged** — no auth,
   localhost use. Zero breakage for current users.
3. **Code reuse — Option #3:** Copy/vendor argo-cd's auth packages into
   argo-rollouts and adapt (rename configmaps, swap RBAC model). Self-contained,
   most mergeable. Shared-library extraction (#1) is a documented future step.
4. **RBAC actions:** Full verb set (argo-cd style) — standard CRUD + rollout
   lifecycle verbs as first-class actions.
5. **Built-in roles:** `role:admin`, `role:readonly`, `role:operator`.

## 3. Architecture

Two run modes for the dashboard binary, selected by a flag (e.g. `--auth-mode`,
default `local`):

```
LOCAL MODE (unchanged, default):
  kubectl argo rollouts dashboard  ──► localhost:3100, no auth, uses your kubeconfig

SERVER MODE (new, --auth-mode=server):
  rollouts-dashboard-server (Deployment in cluster)
     ├─ TLS listener (cmux: gRPC + HTTP on one port, like argo-cd)
     ├─ AuthN interceptor  → validates JWT / OIDC, injects claims into ctx
     ├─ AuthZ (Casbin)     → checks claims vs policy per call
     ├─ uses its OWN ServiceAccount to talk to k8s (not per-user kubeconfig)
     └─ serves same UI + /api/ gRPC-gateway
```

- Server mode runs as an in-cluster Deployment with its own ServiceAccount
  identity. User permissions are enforced by **our** RBAC layer, not k8s RBAC.
- Config lives in three new objects (renamed copies of argo-cd's):
  `argo-rollouts-dashboard-cm`, `argo-rollouts-dashboard-secret`,
  `argo-rollouts-dashboard-rbac-cm`.

## 4. Components / packages to vendor

Copy from argo-cd into `argo-rollouts` (e.g. under `server/auth/...`), rename
module paths and configmap references:

| Vendored pkg | Source (argo-cd) | Adaptation |
|---|---|---|
| `session` | `util/session/sessionmanager.go` | JWT HS256, issuer `argo-rollouts`, signing key from new secret |
| `password` | `util/password/password.go` | bcrypt — unchanged |
| `rbac` | `util/rbac/rbac.go` + `model.conf` | new resource/action set + new builtin-policy.csv |
| `oidc` | `util/oidc/*` | endpoints unchanged; reads new cm |
| `dex` | `util/dex/*` | optional bundled Dex for SSO connectors |
| `settings` | subset of `util/settings/*` | only auth-relevant keys; new cm/secret names |
| server wiring | `server/server.go` interceptors | port `Authenticate()`, gRPC auth interceptor, claims-in-ctx |

New (not vendored):
- `server/auth/rbac_model.go` — rollouts resources/actions + built-in policy.
- `--auth-mode` flag plumbing in `cmd/dashboard` + `server`.
- Account/session gRPC service for login/logout.

## 5. Auth data flow

```
Browser ─login(user,pass)─► /api/session.Create
   └─ verify bcrypt vs argo-rollouts-dashboard-secret → issue JWT (HS256) → Set-Cookie

Browser ─any /api call (cookie/Bearer)─► gRPC auth interceptor
   └─ Authenticate(): extract token → session.Parse() (or OIDC verify) → claims into ctx
        └─ handler → rbac.Enforce(subject, resource, action, object)
              allow → serve   |   deny → PermissionDenied

SSO path: /auth/login → OIDC/Dex → /auth/callback → exchange code → issue session JWT
Whitelisted (no auth): session.Create (login), /healthz, static UI assets
```

## 6. RBAC model (concrete)

Casbin policy `p = sub, res, act, obj`, glob match (argo-cd model).

- **resources:** `rollouts`, `analysisruns`, `analysistemplates`,
  `clusteranalysistemplates`, `experiments`
- **actions:** `get`, `create`, `update`, `delete`, `promote`, `abort`,
  `retry`, `restart`, `pause`, `skip`, `setimage`, `undo`
- **object:** `<namespace>/<name>` glob (e.g. `prod/*`)

**Built-in roles:**

| Role | Grants |
|---|---|
| `role:readonly` | `get` on all resources/objects |
| `role:operator` | readonly + `promote, abort, retry, restart, pause, skip` (no create/delete/setimage) |
| `role:admin` | `*` on `*` |

Example: `p, role:operator, rollouts, promote, */*, allow`

## 7. Config keys

**`argo-rollouts-dashboard-secret`** (Secret)
- `server.secretkey` — JWT HS256 signing key (auto-generated if absent)
- `admin.password` (bcrypt), `admin.passwordMtime`, `admin.enabled`
- `accounts.<name>.password` — local users
- `oidc.clientSecret`, `tls.crt`, `tls.key`

**`argo-rollouts-dashboard-cm`** (ConfigMap)
- `url` — external dashboard URL (OIDC redirect base)
- `oidc.config` — issuer, clientID, requestedScopes, etc.
- `dex.config` — optional bundled Dex connectors
- `users.anonymous.enabled` — fallback unauth access (default `false`)
- `accounts.<name>` — declare local accounts + capabilities (login/apiKey)

**`argo-rollouts-dashboard-rbac-cm`** (ConfigMap)
- `policy.csv` — user policy (extends/overrides built-in)
- `policy.default` — default role for authenticated users (e.g. `role:readonly` or empty)
- `policy.matchMode` — `glob` | `regex`
- `scopes` — OIDC claim(s) mapped to groups

**Manifests:** new `manifests/dashboard-install.yaml` — Deployment + ServiceAccount
+ k8s RBAC for the SA + Service + the three cm/secret stubs.

## 8. Testing strategy

- **Unit:** session sign/verify (valid / expired / tampered); bcrypt verify;
  `rbac.Enforce` truth table per built-in role; OIDC token verify (mock provider).
- **Interceptor:** table-test `Authenticate()` — cookie vs Bearer, missing/bad
  token, anonymous on/off, whitelisted paths.
- **RBAC matrix:** each role × each `(resource, action, object)` → expected allow/deny.
- **E2E:** start server mode, login → cookie → call `promote` (allowed for
  operator, denied for readonly), assert gRPC codes.
- **Regression:** local mode with no flag still serves with zero auth — behavior
  unchanged.

## 9. Security

- TLS **on by default** in server mode (self-signed generated if no cert, like argo-cd).
- Replace `grpc.WithInsecure()` with TLS credentials in server mode.
- Signing key and passwords only in the Secret, never the ConfigMap. bcrypt cost ≥ default.
- **Deny by default:** no matching policy → `PermissionDenied`. Empty `policy.default` = locked down.
- Anonymous access **off** by default.

## 10. Open risks

1. **Code drift** — vendored auth won't auto-receive argo-cd CVE fixes. Mitigation:
   document a sync procedure; plan future shared-library extraction (reuse option #1).
2. **Dex bundling** — adds image size/complexity. Consider making Dex optional
   (external IdP only when Dex not bundled).
3. **Maintainer buy-in** — large change. Likely needs an enhancement proposal first,
   then split into stacked PRs (server mode → vendored auth → RBAC → UI).
4. **Powerful server ServiceAccount** — the dashboard SA is broad; our RBAC is the
   only gate. Misconfiguration risks over-broad access. Needs clear operator docs.
5. **UI work** — login page, logout, token handling in `ui/`. Scope as a sub-phase.

## 11. Future work (out of scope for first PR)

- Extract vendored auth into a shared `argoproj` module (reuse option #1) to end
  code duplication across argo-cd and argo-rollouts.
- API-key management UI.
- Project-scoped RBAC (argo-cd `proj:` subjects) if rollouts gains a project concept.
