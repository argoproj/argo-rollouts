# Dashboard Auth — Plan 7: Server-Mode Manifests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a deployable kustomize overlay that runs the dashboard in authenticated server mode — the three config objects (cm/secret/rbac-cm), least-privilege RBAC for the dashboard ServiceAccount to read them, and a patch adding `--auth-mode=server` — without changing the existing no-auth `dashboard-install`.

**Architecture:** A new overlay directory `manifests/dashboard-install-server/` whose `kustomization.yaml` bases on `../dashboard-install` and adds: `argo-rollouts-dashboard-cm` (url, anonymous-off, OIDC examples), `argo-rollouts-dashboard-secret` (EMPTY stub — the server refuses to start until a ≥32-byte `server.secretkey` is set, which is the safe default), `argo-rollouts-dashboard-rbac-cm` (default `role:readonly` + policy examples), a `Role` granting `get` on exactly those three objects (resourceName-restricted), a `RoleBinding` to the existing `argo-rollouts-dashboard` SA, and a strategic-merge patch setting the container `args` to `["dashboard", "--auth-mode=server"]` (the image ENTRYPOINT is `/bin/kubectl-argo-rollouts`, default CMD `["dashboard"]`). A generated rollup `manifests/dashboard-install-server.yaml` mirrors the existing `dashboard-install.yaml` convention. The default `dashboard-install` overlay is untouched, so existing users are unaffected.

**Tech Stack:** Kubernetes YAML, kustomize (via `kubectl kustomize`, v5 built-in). Validation: `kubectl kustomize <dir>` (build) + `kubectl apply --dry-run=client --validate=false -f -` (structural parse; the cluster-schema dry-run is unavailable offline) + content assertions with grep.

## Global Constraints

- All files under `manifests/dashboard-install-server/`. Do NOT modify `manifests/dashboard-install/` (the default no-auth install stays as-is) or the generated `manifests/dashboard-install.yaml`.
- Object names exactly: `argo-rollouts-dashboard-cm`, `argo-rollouts-dashboard-secret`, `argo-rollouts-dashboard-rbac-cm` (these are what `server/auth/settings` reads). Keys exactly: secret `server.secretkey` / `admin.password` / `tls.crt` / `tls.key`; cm `url` / `users.anonymous.enabled` / `oidc.config`; rbac-cm `policy.csv` / `policy.default` / `policy.matchMode`.
- The shipped Secret MUST be empty/placeholder (no real key) — the backend's `GetSigningKey` rejects a <32-byte key, so an unconfigured deploy fails safe (won't serve unauthenticated). Document loudly that operators must set `server.secretkey` (≥32 random bytes) and `admin.password` (bcrypt) before use. Never ship a real or weak signing key.
- The SA Role is least-privilege: `get` only, on `configmaps`/`secrets`, restricted via `resourceNames` to exactly the three dashboard objects. No `list`/`watch`, no namespace-wide secret access.
- Labels match the existing dashboard manifests (`app.kubernetes.io/component|name|part-of`).
- Reuse the existing SA `argo-rollouts-dashboard` (do not create a new one — the base provides it).
- Validation per task: `kubectl apply --dry-run=client --validate=false -f <file>` must parse each YAML; Task 3 additionally requires `kubectl kustomize manifests/dashboard-install-server` to build cleanly and contain the expected objects + `--auth-mode=server`.

---

### Task 1: Dashboard config objects (cm, secret stub, rbac-cm)

**Files:**
- Create: `manifests/dashboard-install-server/dashboard-cm.yaml`
- Create: `manifests/dashboard-install-server/dashboard-secret.yaml`
- Create: `manifests/dashboard-install-server/dashboard-rbac-cm.yaml`

- [ ] **Step 1: Write the three objects**

`dashboard-cm.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-dashboard-cm
  labels:
    app.kubernetes.io/component: argo-rollouts-dashboard
    app.kubernetes.io/name: argo-rollouts-dashboard
    app.kubernetes.io/part-of: argo-rollouts
data:
  # External URL the dashboard is served at. REQUIRED for OIDC (used to build
  # the redirect_uri). Example: https://rollouts.example.com
  url: ""
  # Allow unauthenticated access. Off by default; leave "false" for a secured
  # dashboard.
  users.anonymous.enabled: "false"
  # OIDC single sign-on (optional). Uncomment and fill to enable SSO. The
  # clientSecret belongs in argo-rollouts-dashboard-secret, not here.
  # oidc.config: |
  #   name: MyIDP
  #   issuer: https://idp.example.com
  #   clientID: argo-rollouts-dashboard
  #   requestedScopes: ["openid", "profile", "email", "groups"]
```

`dashboard-secret.yaml`:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: argo-rollouts-dashboard-secret
  labels:
    app.kubernetes.io/component: argo-rollouts-dashboard
    app.kubernetes.io/name: argo-rollouts-dashboard
    app.kubernetes.io/part-of: argo-rollouts
type: Opaque
stringData:
  # REQUIRED before the dashboard will start in server mode. The server rejects
  # a signing key shorter than 32 bytes, so this empty stub fails safe.
  # Generate one, e.g.:  openssl rand -base64 48
  server.secretkey: ""
  # Admin login password as a bcrypt hash. Generate, e.g.:
  #   htpasswd -nbBC 10 "" 'YOUR_PASSWORD' | tr -d ':\n' | sed 's/$2y/$2a/'
  # admin.password: ""
  # TLS certificate/key (optional). If unset, the server generates a self-signed
  # cert in server mode. Provide a real cert for production.
  # tls.crt: ""
  # tls.key: ""
```

`dashboard-rbac-cm.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-dashboard-rbac-cm
  labels:
    app.kubernetes.io/component: argo-rollouts-dashboard
    app.kubernetes.io/name: argo-rollouts-dashboard
    app.kubernetes.io/part-of: argo-rollouts
data:
  # Default role for authenticated users with no explicit grant. Built-in roles:
  # role:admin, role:readonly, role:operator. Empty string = deny by default.
  policy.default: role:readonly
  # Glob (default) or regex matching for policy objects.
  policy.matchMode: glob
  # Additional RBAC policy. Format:
  #   p, <subject>, <resource>, <action>, <namespace>/<name>, allow
  #   g, <user-or-group>, <role>
  # Resources: rollouts, analysisruns, analysistemplates,
  #   clusteranalysistemplates, experiments.
  # Actions: get, create, update, delete, promote, abort, retry, restart,
  #   pause, skip, setimage, undo.
  # Example:
  #   g, alice@example.com, role:admin
  #   p, role:operator, rollouts, promote, prod/*, allow
  policy.csv: ""
```

- [ ] **Step 2: Validate each parses**

Run:
```bash
for f in manifests/dashboard-install-server/dashboard-cm.yaml manifests/dashboard-install-server/dashboard-secret.yaml manifests/dashboard-install-server/dashboard-rbac-cm.yaml; do
  kubectl apply --dry-run=client --validate=false -f "$f";
done
```
Expected: each prints `<kind>/<name> created (dry run)` with no parse error.

- [ ] **Step 3: Commit**

```bash
git add manifests/dashboard-install-server/dashboard-cm.yaml manifests/dashboard-install-server/dashboard-secret.yaml manifests/dashboard-install-server/dashboard-rbac-cm.yaml
git commit -m "feat(manifests): dashboard auth config objects (cm/secret/rbac-cm)"
```

---

### Task 2: Least-privilege RBAC for the dashboard ServiceAccount

**Files:**
- Create: `manifests/dashboard-install-server/dashboard-auth-role.yaml`
- Create: `manifests/dashboard-install-server/dashboard-auth-rolebinding.yaml`

- [ ] **Step 1: Write the Role + RoleBinding**

`dashboard-auth-role.yaml` (the dashboard reads its own cm/secret via Get only, restricted to the three named objects):

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: argo-rollouts-dashboard-auth
  labels:
    app.kubernetes.io/component: argo-rollouts-dashboard
    app.kubernetes.io/name: argo-rollouts-dashboard
    app.kubernetes.io/part-of: argo-rollouts
rules:
  - apiGroups:
      - ""
    resources:
      - configmaps
    resourceNames:
      - argo-rollouts-dashboard-cm
      - argo-rollouts-dashboard-rbac-cm
    verbs:
      - get
  - apiGroups:
      - ""
    resources:
      - secrets
    resourceNames:
      - argo-rollouts-dashboard-secret
    verbs:
      - get
```

`dashboard-auth-rolebinding.yaml`:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: argo-rollouts-dashboard-auth
  labels:
    app.kubernetes.io/component: argo-rollouts-dashboard
    app.kubernetes.io/name: argo-rollouts-dashboard
    app.kubernetes.io/part-of: argo-rollouts
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: argo-rollouts-dashboard-auth
subjects:
  - kind: ServiceAccount
    name: argo-rollouts-dashboard
```

(The `RoleBinding` and `ServiceAccount` are namespaced; kustomize/`namespace:` injects the namespace at apply time. Leave the SA subject namespace unset so the overlay's `namespace` or the applying namespace governs — matching how the base binds.)

- [ ] **Step 2: Validate each parses**

Run:
```bash
kubectl apply --dry-run=client --validate=false -f manifests/dashboard-install-server/dashboard-auth-role.yaml
kubectl apply --dry-run=client --validate=false -f manifests/dashboard-install-server/dashboard-auth-rolebinding.yaml
```
Expected: `role.rbac.authorization.k8s.io/argo-rollouts-dashboard-auth created (dry run)` and the rolebinding likewise.

- [ ] **Step 3: Commit**

```bash
git add manifests/dashboard-install-server/dashboard-auth-role.yaml manifests/dashboard-install-server/dashboard-auth-rolebinding.yaml
git commit -m "feat(manifests): least-privilege RBAC for dashboard to read its config"
```

---

### Task 3: Overlay kustomization + deployment patch + rollup

**Files:**
- Create: `manifests/dashboard-install-server/deployment-auth-patch.yaml`
- Create: `manifests/dashboard-install-server/kustomization.yaml`
- Create: `manifests/dashboard-install-server.yaml` (generated rollup)
- Create: `manifests/dashboard-install-server/README.md`

- [ ] **Step 1: Write the deployment patch** (`deployment-auth-patch.yaml`)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: argo-rollouts-dashboard
spec:
  template:
    spec:
      containers:
        - name: argo-rollouts-dashboard
          args:
            - dashboard
            - --auth-mode=server
```

- [ ] **Step 2: Write the overlay kustomization** (`kustomization.yaml`)

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

# Authenticated server-mode dashboard: the no-auth base plus auth config,
# least-privilege RBAC, and the --auth-mode=server flag.
resources:
  - ../dashboard-install
  - dashboard-cm.yaml
  - dashboard-secret.yaml
  - dashboard-rbac-cm.yaml
  - dashboard-auth-role.yaml
  - dashboard-auth-rolebinding.yaml

patches:
  - path: deployment-auth-patch.yaml
    target:
      kind: Deployment
      name: argo-rollouts-dashboard
```

- [ ] **Step 3: Verify the overlay builds and is correct**

Run: `kubectl kustomize manifests/dashboard-install-server`
Expected: builds with no error and includes the base objects + the three config objects + Role + RoleBinding + the patched Deployment.

Run these assertions (each must print a match):
```bash
OUT=$(kubectl kustomize manifests/dashboard-install-server)
echo "$OUT" | grep -q "name: argo-rollouts-dashboard-secret" && echo "secret OK"
echo "$OUT" | grep -q "name: argo-rollouts-dashboard-rbac-cm" && echo "rbac-cm OK"
echo "$OUT" | grep -q "kind: Role" && echo "role OK"
echo "$OUT" | grep -q -- "--auth-mode=server" && echo "auth-mode arg OK"
echo "$OUT" | grep -q "resourceNames" && echo "least-privilege OK"
```
Expected: all five "OK" lines print. Also confirm the build parses as a whole:
`kubectl kustomize manifests/dashboard-install-server | kubectl apply --dry-run=client --validate=false -f -` exits 0.

- [ ] **Step 4: Generate the rollup** (`manifests/dashboard-install-server.yaml`)

Run: `kubectl kustomize manifests/dashboard-install-server > manifests/dashboard-install-server.yaml`
Then verify the rollup also parses: `kubectl apply --dry-run=client --validate=false -f manifests/dashboard-install-server.yaml` exits 0.

- [ ] **Step 5: Write the README** (`manifests/dashboard-install-server/README.md`)

```markdown
# Argo Rollouts Dashboard — authenticated server mode

Deploys the dashboard with authentication, RBAC, and TLS enabled
(`--auth-mode=server`). The default `manifests/dashboard-install` runs the
dashboard with NO authentication and is unchanged.

## Before you apply

1. Set a signing key (the server refuses to start without one ≥32 bytes):

       kubectl -n <ns> create secret generic argo-rollouts-dashboard-secret \
         --from-literal=server.secretkey="$(openssl rand -base64 48)" \
         --dry-run=client -o yaml | kubectl apply -f -

   Or edit `dashboard-secret.yaml` before applying.

2. Set an admin password (bcrypt) in the same secret under `admin.password`,
   or configure OIDC in `argo-rollouts-dashboard-cm` (`oidc.config`) and set
   `url` to the dashboard's external address.

3. Review `argo-rollouts-dashboard-rbac-cm` — `policy.default: role:readonly`
   grants every authenticated user read access; tighten as needed.

## Apply

    kubectl apply -k manifests/dashboard-install-server     # or
    kubectl apply -f manifests/dashboard-install-server.yaml

## Notes

- TLS: a self-signed cert is generated if `tls.crt`/`tls.key` are not set in
  the secret. Use `--insecure` (edit the deployment args) to disable TLS when
  terminating it at an ingress.
- The dashboard ServiceAccount is granted `get` on exactly its three config
  objects (least privilege) in addition to the rollouts access from the base.
```

- [ ] **Step 6: Commit**

```bash
git add manifests/dashboard-install-server/ manifests/dashboard-install-server.yaml
git commit -m "feat(manifests): server-mode dashboard overlay and rollup"
```

---

## Self-Review

**Spec coverage (vs design §7 manifests):**
- `dashboard-install.yaml`-style deployable for server mode → the overlay + rollup. ✅
- Deployment + SA + Service (from base) + the three cm/secret stubs → Tasks 1 + 3. ✅
- RBAC for the SA (read its own cm/secret) → Task 2, least-privilege. ✅
- `--auth-mode=server` wired → Task 3 patch. ✅
- Backward compatible: default `dashboard-install` untouched → new overlay directory only. ✅
- Bundled Dex (LDAP/SAML/GitHub upstream) → NOT included; this overlay supports any external OIDC IdP. A bundled-Dex variant is a follow-up overlay. Stated scope cut.

**Placeholder scan:** No TBD/TODO. The Secret is intentionally empty (fail-safe) with operator instructions — that is the design, not a placeholder gap. ✅

**Consistency:** Object names and keys match exactly what `server/auth/settings` reads (`SecretName`/`ConfigMapName`/`RBACConfigMapName` and their key consts). Labels match the base dashboard manifests. ✅

**Security notes:**
- No secret material shipped: empty `server.secretkey` → backend fails safe (won't serve). README forces the operator to set a strong key.
- Least-privilege RBAC: `get` on three named objects only — no namespace-wide secret read, no list/watch.
- `policy.default: role:readonly` is a documented, conservative default (authenticated → read-only); operators tighten via `policy.csv`.
- TLS on by default in server mode (self-signed fallback), per Plan 4e.

**Carried forward:**
- A bundled-Dex overlay (Dex Deployment + connectors cm) for LDAP/SAML/GitHub login.
- A `namespace:` field / namespaced rollout of the overlay if a non-default namespace is standard.
- CI to regenerate `dashboard-install-server.yaml` from the overlay (mirror the existing `make manifests`).
- Surfacing the overlay in `manifests/README.md` / docs.
