# Dashboard Auth — Plan 3: Settings + Account Store Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a `server/auth/settings` package: a `SettingsManager` that reads the dashboard's ConfigMaps/Secret from Kubernetes and exposes typed accessors for the JWT signing key, local accounts (with `VerifyUsernamePassword`), and RBAC/auth configuration — the bridge between Plan 2's crypto primitives and Plan 4's interceptor.

**Architecture:** One package wrapping a `kubernetes.Interface` + namespace (both already carried by the dashboard's `ServerOptions`). It reads three objects — `argo-rollouts-dashboard-secret`, `argo-rollouts-dashboard-cm`, `argo-rollouts-dashboard-rbac-cm` — via the existing repo pattern `clientset.CoreV1().ConfigMaps(ns).Get(...)` / `.Secrets(ns).Get(...)`. A missing ConfigMap/Secret is treated as empty defaults (not an error); a missing/weak signing key IS an error. Account verification reuses `server/auth/password`. No HTTP, no caching (Plan 4 adds watch/cache). Tested with `k8s.io/client-go/kubernetes/fake`.

**Tech Stack:** Go, `k8s.io/client-go` (v0.34.5, incl. `kubernetes/fake`), `k8s.io/api/core/v1`, `k8s.io/apimachinery` (metav1, apierrors), `github.com/argoproj/argo-rollouts/server/auth/password`, testify.

## Global Constraints

- Module path: `github.com/argoproj/argo-rollouts`. Package path: `server/auth/settings`.
- Object names (constants): Secret `argo-rollouts-dashboard-secret`; ConfigMap `argo-rollouts-dashboard-cm`; RBAC ConfigMap `argo-rollouts-dashboard-rbac-cm`.
- Secret keys: `server.secretkey` (JWT HS256 key), `admin.password` (bcrypt), `admin.passwordMtime`, `accounts.<name>.password`.
- ConfigMap keys (`...-cm`): `url`, `oidc.config`, `dex.config`, `users.anonymous.enabled`, `admin.enabled`, `accounts.<name>`, `accounts.<name>.enabled`.
- RBAC ConfigMap keys (`...-rbac-cm`): `policy.csv`, `policy.default`, `policy.matchMode`, `scopes`.
- Signing key MUST be at least 32 bytes; `GetSigningKey` errors if absent or shorter (closes the deferred Plan 2 key-length gap at the load boundary).
- A missing ConfigMap or Secret is NOT an error — treat as empty data (mirrors argo-cd's tolerant settings loading). Only a genuine API error (not NotFound) propagates.
- Account verification reuses `password.VerifyPassword`; never compare hashes by hand.
- Fail closed: a disabled or unknown account fails verification with a non-nil error; never authenticate on a lookup miss.
- Tests use `k8s.io/client-go/kubernetes/fake.NewSimpleClientset(...)` seeding real `corev1.Secret`/`corev1.ConfigMap` objects, and real bcrypt hashes from `password.HashPassword` (no hand-written hashes).

---

### Task 1: SettingsManager skeleton + signing key

**Files:**
- Create: `server/auth/settings/settings.go`
- Test: `server/auth/settings/settings_test.go`

**Interfaces:**
- Consumes: nothing (other packages).
- Produces:
  - Constants: `SecretName`, `ConfigMapName`, `RBACConfigMapName`; key consts `KeyServerSignature = "server.secretkey"`.
  - `const MinSigningKeyLength = 32`
  - `type SettingsManager struct { clientset kubernetes.Interface; namespace string }`
  - `func NewSettingsManager(clientset kubernetes.Interface, namespace string) *SettingsManager`
  - unexported helpers `func (m *SettingsManager) secretData(ctx context.Context) (map[string][]byte, error)` and `func (m *SettingsManager) configMapData(ctx context.Context, name string) (map[string]string, error)` (NotFound → empty map, nil error).
  - `func (m *SettingsManager) GetSigningKey(ctx context.Context) ([]byte, error)` — returns `server.secretkey`; errors if absent or `< MinSigningKeyLength`.

- [ ] **Step 1: Write the failing test**

```go
package settings

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const testNamespace = "argo-rollouts"

func secretWith(data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: testNamespace},
		Data:       data,
	}
}

func TestGetSigningKeyReturnsKey(t *testing.T) {
	key := []byte(strings.Repeat("k", MinSigningKeyLength))
	client := fake.NewSimpleClientset(secretWith(map[string][]byte{KeyServerSignature: key}))
	m := NewSettingsManager(client, testNamespace)

	got, err := m.GetSigningKey(context.Background())
	require.NoError(t, err)
	assert.Equal(t, key, got)
}

func TestGetSigningKeyRejectsShortKey(t *testing.T) {
	client := fake.NewSimpleClientset(secretWith(map[string][]byte{KeyServerSignature: []byte("too-short")}))
	m := NewSettingsManager(client, testNamespace)

	_, err := m.GetSigningKey(context.Background())
	assert.Error(t, err)
}

func TestGetSigningKeyMissingSecret(t *testing.T) {
	client := fake.NewSimpleClientset() // no secret at all
	m := NewSettingsManager(client, testNamespace)

	_, err := m.GetSigningKey(context.Background())
	assert.Error(t, err, "missing signing key must be an error, not empty success")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/settings/ -v`
Expected: FAIL — `undefined: NewSettingsManager`, `undefined: SecretName`, etc.

- [ ] **Step 3: Write minimal implementation**

```go
// Package settings loads the Argo Rollouts dashboard's authentication
// configuration (signing key, local accounts, RBAC) from Kubernetes
// ConfigMaps and a Secret.
package settings

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Kubernetes object names for the dashboard's auth configuration.
const (
	SecretName        = "argo-rollouts-dashboard-secret"
	ConfigMapName     = "argo-rollouts-dashboard-cm"
	RBACConfigMapName = "argo-rollouts-dashboard-rbac-cm"
)

// Secret data keys.
const (
	KeyServerSignature = "server.secretkey"
)

// MinSigningKeyLength is the minimum acceptable HS256 signing key length.
// A shorter key is trivially brute-forced, so it is rejected at load time.
const MinSigningKeyLength = 32

// SettingsManager reads dashboard auth settings from Kubernetes.
type SettingsManager struct {
	clientset kubernetes.Interface
	namespace string
}

// NewSettingsManager returns a SettingsManager that reads the dashboard
// Secret/ConfigMaps from namespace using clientset.
func NewSettingsManager(clientset kubernetes.Interface, namespace string) *SettingsManager {
	return &SettingsManager{clientset: clientset, namespace: namespace}
}

// secretData returns the dashboard Secret's data, or an empty map if the
// Secret does not exist. Only non-NotFound API errors propagate.
func (m *SettingsManager) secretData(ctx context.Context) (map[string][]byte, error) {
	secret, err := m.clientset.CoreV1().Secrets(m.namespace).Get(ctx, SecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return map[string][]byte{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get secret %s: %w", SecretName, err)
	}
	if secret.Data == nil {
		return map[string][]byte{}, nil
	}
	return secret.Data, nil
}

// configMapData returns the named ConfigMap's data, or an empty map if it
// does not exist. Only non-NotFound API errors propagate.
func (m *SettingsManager) configMapData(ctx context.Context, name string) (map[string]string, error) {
	cm, err := m.clientset.CoreV1().ConfigMaps(m.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get configmap %s: %w", name, err)
	}
	if cm.Data == nil {
		return map[string]string{}, nil
	}
	return cm.Data, nil
}

// GetSigningKey returns the HS256 signing key from the dashboard Secret. It
// errors if the key is absent or shorter than MinSigningKeyLength.
func (m *SettingsManager) GetSigningKey(ctx context.Context) ([]byte, error) {
	data, err := m.secretData(ctx)
	if err != nil {
		return nil, err
	}
	key := data[KeyServerSignature]
	if len(key) < MinSigningKeyLength {
		return nil, fmt.Errorf("signing key %q is missing or shorter than %d bytes", KeyServerSignature, MinSigningKeyLength)
	}
	return key, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/settings/ -v`
Expected: PASS (3 tests). Run `go mod tidy` if the toolchain reports a missing require (client-go is already direct via existing repo usage; no new dep expected).

- [ ] **Step 5: Commit**

```bash
git add server/auth/settings/
git commit -m "feat(settings): settings manager skeleton and signing-key accessor"
```

---

### Task 2: Local account store + VerifyUsernamePassword

**Files:**
- Modify: `server/auth/settings/settings.go`
- Create: `server/auth/settings/accounts.go`
- Test: `server/auth/settings/accounts_test.go`

**Interfaces:**
- Consumes: `SettingsManager`, `secretData`, `configMapData` (Task 1); `password.VerifyPassword` / `password.HashPassword`.
- Produces:
  - Key consts: `KeyAdminPassword = "admin.password"`, `KeyAdminEnabled = "admin.enabled"`.
  - `const AdminUsername = "admin"`
  - `type Account struct { Enabled bool; PasswordHash string }`
  - `func (m *SettingsManager) GetAccount(ctx context.Context, name string) (Account, error)` — admin from `admin.password`/`admin.enabled`(default true); named accounts from `accounts.<name>.password`/`accounts.<name>.enabled`(default true); error if no password stored.
  - `func (m *SettingsManager) VerifyUsernamePassword(ctx context.Context, username, password string) error` — errors if account missing, disabled, or password mismatch; nil on success.

- [ ] **Step 1: Write the failing test**

```go
package settings

import (
	"context"
	"testing"

	"github.com/argoproj/argo-rollouts/server/auth/password"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func cmWith(name string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Data:       data,
	}
}

func TestVerifyAdminPassword(t *testing.T) {
	hash, err := password.HashPassword("s3cret")
	require.NoError(t, err)
	client := fake.NewSimpleClientset(
		secretWith(map[string][]byte{KeyAdminPassword: []byte(hash)}),
	)
	m := NewSettingsManager(client, testNamespace)

	assert.NoError(t, m.VerifyUsernamePassword(context.Background(), AdminUsername, "s3cret"))
	assert.Error(t, m.VerifyUsernamePassword(context.Background(), AdminUsername, "wrong"))
}

func TestVerifyAdminDisabled(t *testing.T) {
	hash, err := password.HashPassword("s3cret")
	require.NoError(t, err)
	client := fake.NewSimpleClientset(
		secretWith(map[string][]byte{KeyAdminPassword: []byte(hash)}),
		cmWith(ConfigMapName, map[string]string{KeyAdminEnabled: "false"}),
	)
	m := NewSettingsManager(client, testNamespace)

	assert.Error(t, m.VerifyUsernamePassword(context.Background(), AdminUsername, "s3cret"),
		"disabled admin must not authenticate even with the right password")
}

func TestVerifyNamedAccount(t *testing.T) {
	hash, err := password.HashPassword("devpass")
	require.NoError(t, err)
	client := fake.NewSimpleClientset(
		secretWith(map[string][]byte{"accounts.dev.password": []byte(hash)}),
	)
	m := NewSettingsManager(client, testNamespace)

	assert.NoError(t, m.VerifyUsernamePassword(context.Background(), "dev", "devpass"))
	assert.Error(t, m.VerifyUsernamePassword(context.Background(), "dev", "nope"))
}

func TestVerifyUnknownAccount(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewSettingsManager(client, testNamespace)

	assert.Error(t, m.VerifyUsernamePassword(context.Background(), "ghost", "whatever"),
		"unknown account must fail closed")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/settings/ -run TestVerify -v`
Expected: FAIL — `undefined: AdminUsername`, `undefined: (*SettingsManager).VerifyUsernamePassword`, `undefined: KeyAdminPassword`.

- [ ] **Step 3: Write `accounts.go`**

```go
package settings

import (
	"context"
	"fmt"
	"strconv"

	"github.com/argoproj/argo-rollouts/server/auth/password"
)

// Account-related data keys.
const (
	KeyAdminPassword = "admin.password"
	KeyAdminEnabled  = "admin.enabled"
)

// AdminUsername is the built-in administrator account name.
const AdminUsername = "admin"

// Account is a local dashboard account.
type Account struct {
	Enabled      bool
	PasswordHash string
}

// parseBoolDefault returns the parsed boolean for raw, or def if raw is empty
// or unparseable.
func parseBoolDefault(raw string, def bool) bool {
	if raw == "" {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return def
	}
	return v
}

// GetAccount returns the named local account. The admin account is read from
// admin.password / admin.enabled; other accounts from accounts.<name>.password
// / accounts.<name>.enabled. Accounts are enabled by default. An account with
// no stored password hash is reported as not found.
func (m *SettingsManager) GetAccount(ctx context.Context, name string) (Account, error) {
	secret, err := m.secretData(ctx)
	if err != nil {
		return Account{}, err
	}
	cm, err := m.configMapData(ctx, ConfigMapName)
	if err != nil {
		return Account{}, err
	}

	var hashKey, enabledKey string
	if name == AdminUsername {
		hashKey, enabledKey = KeyAdminPassword, KeyAdminEnabled
	} else {
		hashKey = fmt.Sprintf("accounts.%s.password", name)
		enabledKey = fmt.Sprintf("accounts.%s.enabled", name)
	}

	hash := string(secret[hashKey])
	if hash == "" {
		return Account{}, fmt.Errorf("account %q not found", name)
	}
	return Account{
		Enabled:      parseBoolDefault(cm[enabledKey], true),
		PasswordHash: hash,
	}, nil
}

// VerifyUsernamePassword returns nil if username exists, is enabled, and
// password matches its stored hash. Any miss, disabled account, or mismatch
// returns a non-nil error (fail closed).
func (m *SettingsManager) VerifyUsernamePassword(ctx context.Context, username, pass string) error {
	account, err := m.GetAccount(ctx, username)
	if err != nil {
		return err
	}
	if !account.Enabled {
		return fmt.Errorf("account %q is disabled", username)
	}
	return password.VerifyPassword(pass, account.PasswordHash)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./server/auth/settings/ -v`
Expected: PASS (Task 1 + Task 2 tests).

- [ ] **Step 5: Commit**

```bash
git add server/auth/settings/
git commit -m "feat(settings): local account store and VerifyUsernamePassword"
```

---

### Task 3: RBAC config + auth flags accessors

**Files:**
- Create: `server/auth/settings/rbac.go`
- Test: `server/auth/settings/rbac_test.go`

**Interfaces:**
- Consumes: `SettingsManager`, `configMapData` (Task 1).
- Produces:
  - Key consts: `KeyPolicyCSV = "policy.csv"`, `KeyPolicyDefault = "policy.default"`, `KeyPolicyMatchMode = "policy.matchMode"`, `KeyAnonymousEnabled = "users.anonymous.enabled"`, `KeyURL = "url"`.
  - `type RBACConfig struct { PolicyCSV string; DefaultRole string; MatchMode string }`
  - `func (m *SettingsManager) GetRBACConfig(ctx context.Context) (RBACConfig, error)` — `MatchMode` defaults to `"glob"` when unset.
  - `func (m *SettingsManager) AnonymousEnabled(ctx context.Context) (bool, error)` — default false.
  - `func (m *SettingsManager) GetURL(ctx context.Context) (string, error)`.

- [ ] **Step 1: Write the failing test**

```go
package settings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetRBACConfig(t *testing.T) {
	client := fake.NewSimpleClientset(
		cmWith(RBACConfigMapName, map[string]string{
			KeyPolicyCSV:     "g, alice, role:operator",
			KeyPolicyDefault: "role:readonly",
		}),
	)
	m := NewSettingsManager(client, testNamespace)

	cfg, err := m.GetRBACConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "g, alice, role:operator", cfg.PolicyCSV)
	assert.Equal(t, "role:readonly", cfg.DefaultRole)
	assert.Equal(t, "glob", cfg.MatchMode, "match mode defaults to glob when unset")
}

func TestGetRBACConfigEmpty(t *testing.T) {
	client := fake.NewSimpleClientset() // no rbac cm
	m := NewSettingsManager(client, testNamespace)

	cfg, err := m.GetRBACConfig(context.Background())
	require.NoError(t, err)
	assert.Empty(t, cfg.PolicyCSV)
	assert.Empty(t, cfg.DefaultRole)
	assert.Equal(t, "glob", cfg.MatchMode)
}

func TestAnonymousEnabledDefaultFalse(t *testing.T) {
	client := fake.NewSimpleClientset()
	m := NewSettingsManager(client, testNamespace)

	enabled, err := m.AnonymousEnabled(context.Background())
	require.NoError(t, err)
	assert.False(t, enabled, "anonymous access is off by default")
}

func TestAnonymousEnabledTrue(t *testing.T) {
	client := fake.NewSimpleClientset(
		cmWith(ConfigMapName, map[string]string{KeyAnonymousEnabled: "true"}),
	)
	m := NewSettingsManager(client, testNamespace)

	enabled, err := m.AnonymousEnabled(context.Background())
	require.NoError(t, err)
	assert.True(t, enabled)
}

func TestGetURL(t *testing.T) {
	client := fake.NewSimpleClientset(
		cmWith(ConfigMapName, map[string]string{KeyURL: "https://rollouts.example.com"}),
	)
	m := NewSettingsManager(client, testNamespace)

	url, err := m.GetURL(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "https://rollouts.example.com", url)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/settings/ -run 'TestGetRBAC|TestAnonymous|TestGetURL' -v`
Expected: FAIL — `undefined: (*SettingsManager).GetRBACConfig`, `undefined: KeyPolicyCSV`, etc.

- [ ] **Step 3: Write minimal implementation (`rbac.go`)**

```go
package settings

import "context"

// RBAC ConfigMap and auth-flag data keys.
const (
	KeyPolicyCSV        = "policy.csv"
	KeyPolicyDefault    = "policy.default"
	KeyPolicyMatchMode  = "policy.matchMode"
	KeyAnonymousEnabled = "users.anonymous.enabled"
	KeyURL              = "url"
)

// defaultMatchMode is used when policy.matchMode is unset.
const defaultMatchMode = "glob"

// RBACConfig holds the dashboard's RBAC policy configuration, suitable for
// feeding into the rbac package's enforcer.
type RBACConfig struct {
	PolicyCSV   string
	DefaultRole string
	MatchMode   string
}

// GetRBACConfig reads the RBAC ConfigMap. MatchMode defaults to "glob".
func (m *SettingsManager) GetRBACConfig(ctx context.Context) (RBACConfig, error) {
	data, err := m.configMapData(ctx, RBACConfigMapName)
	if err != nil {
		return RBACConfig{}, err
	}
	matchMode := data[KeyPolicyMatchMode]
	if matchMode == "" {
		matchMode = defaultMatchMode
	}
	return RBACConfig{
		PolicyCSV:   data[KeyPolicyCSV],
		DefaultRole: data[KeyPolicyDefault],
		MatchMode:   matchMode,
	}, nil
}

// AnonymousEnabled reports whether unauthenticated access is enabled (default false).
func (m *SettingsManager) AnonymousEnabled(ctx context.Context) (bool, error) {
	data, err := m.configMapData(ctx, ConfigMapName)
	if err != nil {
		return false, err
	}
	return parseBoolDefault(data[KeyAnonymousEnabled], false), nil
}

// GetURL returns the configured external dashboard URL (empty if unset).
func (m *SettingsManager) GetURL(ctx context.Context) (string, error) {
	data, err := m.configMapData(ctx, ConfigMapName)
	if err != nil {
		return "", err
	}
	return data[KeyURL], nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/settings/ -v`
Expected: PASS (all Task 1–3 tests).

- [ ] **Step 5: Run full auth tree + vet**

Run: `go test ./server/auth/... && go vet ./server/auth/...`
Expected: ok across `password`, `rbac`, `session`, `settings`; no vet complaints.

- [ ] **Step 6: Commit**

```bash
git add server/auth/settings/
git commit -m "feat(settings): RBAC config and anonymous/url accessors"
```

---

## Self-Review

**Spec coverage (vs design §4 + §7):**
- Signing key from `server.secretkey` (with ≥32-byte enforcement, closing Plan 2's deferred gap) → Task 1. ✅
- Local accounts (admin + `accounts.<name>`), bcrypt verification, enabled flags → Task 2. ✅
- `VerifyUsernamePassword` reusing `password.VerifyPassword`, fail-closed → Task 2. ✅
- RBAC config (`policy.csv`, `policy.default`, `policy.matchMode`) for feeding the enforcer → Task 3. ✅
- `users.anonymous.enabled` (default false), `url` → Task 3. ✅
- Tolerant loading (missing cm/secret → empty defaults) → Task 1 helpers. ✅
- OIDC/Dex config (`oidc.config`, `dex.config`) raw passthrough → NOT here; consumed by Plan 5 (OIDC). The cm keys are reserved; a raw accessor lands in Plan 5 to avoid speculative parsing now (YAGNI).
- Account tokens (`admin.tokens`, apiKey capability) → NOT here; deferred to a later task (token issuance lives with the session/account service in Plan 4+). Flagged, not a P3 gap.
- `policy.matchMode=regex` actually switching the enforcer model → exposed here as config; the enforcer swap is Plan 4 wiring (carried from Plan 1's known gap).

**Placeholder scan:** No TBD/TODO; every code step has complete code. ✅

**Type consistency:** `SettingsManager`, `NewSettingsManager`, `secretData`, `configMapData`, `GetSigningKey`, `GetAccount`, `VerifyUsernamePassword`, `Account`, `RBACConfig`, `GetRBACConfig`, `AnonymousEnabled`, `GetURL`, `parseBoolDefault` consistent across Tasks 1–3. ✅

**Security notes:**
- Fail-closed verification: unknown/disabled account → error before any password check reaches a usable state. Exercised by `TestVerifyUnknownAccount`, `TestVerifyAdminDisabled`.
- Signing-key length enforced at the load boundary — the only place all consumers funnel through — so Plan 4's `NewSessionManager(key)` receives a validated key.
- Tests seed real bcrypt hashes via `password.HashPassword`, so verification exercises the real crypto path, not a stub.

**Carried forward to Plan 4/5:** OIDC/Dex raw config accessor (P5); account token capability + issuance (P4+); regex match-mode enforcer swap (P4); signing-key auto-generation+persist on first startup if absent (P4 wiring — P3 errors rather than silently inventing a key, which is the safe default for a read-only settings layer).
