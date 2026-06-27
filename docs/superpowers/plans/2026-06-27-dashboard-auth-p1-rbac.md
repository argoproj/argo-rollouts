# Dashboard Auth — Plan 1: RBAC Package Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a self-contained Casbin-based RBAC package for the Argo Rollouts dashboard that defines rollouts resources/actions, ships three built-in roles, loads user policy, and exposes an `Enforce(subject, resource, action, object)` API.

**Architecture:** A new `server/auth/rbac` package wrapping Casbin v2 with an in-memory model + string-adapter policy. The package owns: (a) the Casbin model definition (matches argo-cd's `model.conf`), (b) a built-in policy (`role:admin`, `role:readonly`, `role:operator`), (c) constants for valid resources/actions, (d) an `Enforcer` type with `Enforce(...)` and `SetUserPolicy(csv)` methods. No server wiring, no k8s, no HTTP — pure library, fully unit-testable. This is the foundation that Plan 4's auth interceptor calls.

**Tech Stack:** Go, `github.com/casbin/casbin/v2`, standard `testing`.

## Global Constraints

- Casbin model MUST match argo-cd semantics: `m = g(r.sub, p.sub) && globMatch(r.res, p.res) && globMatch(r.act, p.act) && globMatch(r.obj, p.obj) && p.eff == allow`.
- Deny by default: no matching allow → deny.
- Object format: `<namespace>/<name>` glob.
- Valid resources: `rollouts`, `analysisruns`, `analysistemplates`, `clusteranalysistemplates`, `experiments`.
- Valid actions: `get`, `create`, `update`, `delete`, `promote`, `abort`, `retry`, `restart`, `pause`, `skip`, `setimage`, `undo`.
- Built-in roles: `role:readonly` (get all), `role:operator` (readonly + promote/abort/retry/restart/pause/skip), `role:admin` (`*` on `*`).
- Package path: `server/auth/rbac`. Go module path prefix: `github.com/argoproj/argo-rollouts` (verify against `go.mod`).
- Follow existing argo-rollouts conventions (table-driven tests, `require`/`assert` from `github.com/stretchr/testify` — already a dependency).

---

### Task 1: RBAC constants and model definition

**Files:**
- Create: `server/auth/rbac/model.go`
- Test: `server/auth/rbac/model_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `const ModelConf string` — Casbin model text.
  - `var ResourcesList []string`, `var ActionsList []string`.
  - `func IsValidResource(r string) bool`, `func IsValidAction(a string) bool`.
  - Action constants: `ActionGet, ActionCreate, ActionUpdate, ActionDelete, ActionPromote, ActionAbort, ActionRetry, ActionRestart, ActionPause, ActionSkip, ActionSetImage, ActionUndo` (all `string`).
  - Resource constants: `ResourceRollouts, ResourceAnalysisRuns, ResourceAnalysisTemplates, ResourceClusterAnalysisTemplates, ResourceExperiments` (all `string`).

- [ ] **Step 1: Write the failing test**

```go
package rbac

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModelConfNonEmpty(t *testing.T) {
	assert.Contains(t, ModelConf, "[request_definition]")
	assert.Contains(t, ModelConf, "[policy_effect]")
	assert.Contains(t, ModelConf, "globMatch")
}

func TestValidResource(t *testing.T) {
	assert.True(t, IsValidResource(ResourceRollouts))
	assert.True(t, IsValidResource(ResourceExperiments))
	assert.False(t, IsValidResource("applications"))
	assert.False(t, IsValidResource(""))
}

func TestValidAction(t *testing.T) {
	assert.True(t, IsValidAction(ActionPromote))
	assert.True(t, IsValidAction(ActionGet))
	assert.False(t, IsValidAction("sync"))
	assert.False(t, IsValidAction(""))
}

func TestListsCoverConstants(t *testing.T) {
	assert.Len(t, ResourcesList, 5)
	assert.Len(t, ActionsList, 12)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/rbac/ -run 'TestModelConfNonEmpty|TestValidResource|TestValidAction|TestListsCoverConstants' -v`
Expected: FAIL — `undefined: ModelConf`, `undefined: IsValidResource`, etc.

- [ ] **Step 3: Write minimal implementation**

```go
package rbac

// Resource constants — the Argo Rollouts API objects RBAC can grant over.
const (
	ResourceRollouts                 = "rollouts"
	ResourceAnalysisRuns             = "analysisruns"
	ResourceAnalysisTemplates        = "analysistemplates"
	ResourceClusterAnalysisTemplates = "clusteranalysistemplates"
	ResourceExperiments              = "experiments"
)

// Action constants — standard CRUD plus rollout lifecycle verbs.
const (
	ActionGet      = "get"
	ActionCreate   = "create"
	ActionUpdate   = "update"
	ActionDelete   = "delete"
	ActionPromote  = "promote"
	ActionAbort    = "abort"
	ActionRetry    = "retry"
	ActionRestart  = "restart"
	ActionPause    = "pause"
	ActionSkip     = "skip"
	ActionSetImage = "setimage"
	ActionUndo     = "undo"
)

// ResourcesList enumerates all valid resources.
var ResourcesList = []string{
	ResourceRollouts,
	ResourceAnalysisRuns,
	ResourceAnalysisTemplates,
	ResourceClusterAnalysisTemplates,
	ResourceExperiments,
}

// ActionsList enumerates all valid actions.
var ActionsList = []string{
	ActionGet, ActionCreate, ActionUpdate, ActionDelete,
	ActionPromote, ActionAbort, ActionRetry, ActionRestart,
	ActionPause, ActionSkip, ActionSetImage, ActionUndo,
}

// ModelConf is the Casbin model. Mirrors argo-cd semantics: role grouping (g),
// glob matching on resource/action/object, allow-override effect.
const ModelConf = `
[request_definition]
r = sub, res, act, obj

[policy_definition]
p = sub, res, act, obj, eff

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eff == allow)) && !some(where (p.eff == deny))

[matchers]
m = g(r.sub, p.sub) && globMatch(r.res, p.res) && globMatch(r.act, p.act) && globMatch(r.obj, p.obj)
`

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// IsValidResource reports whether r is a known RBAC resource.
func IsValidResource(r string) bool { return contains(ResourcesList, r) }

// IsValidAction reports whether a is a known RBAC action.
func IsValidAction(a string) bool { return contains(ActionsList, a) }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/rbac/ -run 'TestModelConfNonEmpty|TestValidResource|TestValidAction|TestListsCoverConstants' -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add server/auth/rbac/model.go server/auth/rbac/model_test.go
git commit -m "feat(rbac): rollouts resource/action constants and casbin model"
```

---

### Task 2: Built-in policy

**Files:**
- Create: `server/auth/rbac/builtin_policy.go`
- Test: `server/auth/rbac/builtin_policy_test.go`

**Interfaces:**
- Consumes: action/resource constants from Task 1.
- Produces:
  - `const BuiltinPolicyCSV string` — Casbin policy lines for the three roles.
  - `var BuiltinRoles []string` = `{"role:admin", "role:readonly", "role:operator"}`.

- [ ] **Step 1: Write the failing test**

```go
package rbac

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuiltinRoles(t *testing.T) {
	assert.Equal(t, []string{"role:admin", "role:readonly", "role:operator"}, BuiltinRoles)
}

func TestBuiltinPolicyMentionsRoles(t *testing.T) {
	for _, role := range BuiltinRoles {
		assert.Contains(t, BuiltinPolicyCSV, role, "policy should reference %s", role)
	}
}

func TestOperatorHasPromoteNotDelete(t *testing.T) {
	lines := strings.Split(BuiltinPolicyCSV, "\n")
	var hasPromote, hasDelete bool
	for _, l := range lines {
		if strings.Contains(l, "role:operator") && strings.Contains(l, "promote") {
			hasPromote = true
		}
		if strings.Contains(l, "role:operator") && strings.Contains(l, "delete") {
			hasDelete = true
		}
	}
	assert.True(t, hasPromote, "operator must have promote")
	assert.False(t, hasDelete, "operator must NOT have delete")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/rbac/ -run 'TestBuiltin|TestOperator' -v`
Expected: FAIL — `undefined: BuiltinRoles`, `undefined: BuiltinPolicyCSV`.

- [ ] **Step 3: Write minimal implementation**

```go
package rbac

// BuiltinRoles are the roles shipped by default.
var BuiltinRoles = []string{"role:admin", "role:readonly", "role:operator"}

// BuiltinPolicyCSV is the default Casbin policy. Format per line:
//   p, <sub>, <res>, <act>, <obj>, <eff>
//   g, <sub>, <role>
// readonly: get on everything.
// operator: readonly + lifecycle verbs (no create/delete/setimage/undo).
// admin: wildcard.
const BuiltinPolicyCSV = `
p, role:readonly, *, get, */*, allow

p, role:operator, *, get, */*, allow
p, role:operator, rollouts, promote, */*, allow
p, role:operator, rollouts, abort, */*, allow
p, role:operator, rollouts, retry, */*, allow
p, role:operator, rollouts, restart, */*, allow
p, role:operator, rollouts, pause, */*, allow
p, role:operator, rollouts, skip, */*, allow

p, role:admin, *, *, */*, allow
`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/rbac/ -run 'TestBuiltin|TestOperator' -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add server/auth/rbac/builtin_policy.go server/auth/rbac/builtin_policy_test.go
git commit -m "feat(rbac): built-in admin/readonly/operator policy"
```

---

### Task 3: Enforcer with built-in policy

**Files:**
- Create: `server/auth/rbac/enforcer.go`
- Test: `server/auth/rbac/enforcer_test.go`

**Interfaces:**
- Consumes: `ModelConf` (Task 1), `BuiltinPolicyCSV` (Task 2).
- Produces:
  - `type Enforcer struct { ... }`
  - `func NewEnforcer() (*Enforcer, error)` — loads model + built-in policy.
  - `func (e *Enforcer) Enforce(sub, res, act, obj string) (bool, error)`.

- [ ] **Step 1: Write the failing test**

```go
package rbac

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func enforce(t *testing.T, e *Enforcer, sub, res, act, obj string) bool {
	t.Helper()
	ok, err := e.Enforce(sub, res, act, obj)
	require.NoError(t, err)
	return ok
}

func TestBuiltinRoleMatrix(t *testing.T) {
	e, err := NewEnforcer()
	require.NoError(t, err)

	cases := []struct {
		name             string
		sub, res, act, obj string
		want             bool
	}{
		{"readonly get rollout", "role:readonly", "rollouts", "get", "prod/web", true},
		{"readonly cannot promote", "role:readonly", "rollouts", "promote", "prod/web", false},
		{"operator promote", "role:operator", "rollouts", "promote", "prod/web", true},
		{"operator get experiment", "role:operator", "experiments", "get", "prod/e1", true},
		{"operator cannot delete", "role:operator", "rollouts", "delete", "prod/web", false},
		{"operator cannot setimage", "role:operator", "rollouts", "setimage", "prod/web", false},
		{"admin delete", "role:admin", "rollouts", "delete", "prod/web", true},
		{"admin anything", "role:admin", "analysisruns", "abort", "any/thing", true},
		{"unknown subject denied", "role:nobody", "rollouts", "get", "prod/web", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, enforce(t, e, c.sub, c.res, c.act, c.obj))
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/rbac/ -run TestBuiltinRoleMatrix -v`
Expected: FAIL — `undefined: NewEnforcer`.

- [ ] **Step 3: Write minimal implementation**

```go
package rbac

import (
	"fmt"
	"strings"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	scas "github.com/casbin/casbin/v2/persist/string-adapter"
)

// Enforcer wraps a Casbin enforcer loaded with the dashboard model and policy.
type Enforcer struct {
	enforcer *casbin.Enforcer
}

func newFromPolicy(policyCSV string) (*Enforcer, error) {
	m, err := model.NewModelFromString(ModelConf)
	if err != nil {
		return nil, fmt.Errorf("load model: %w", err)
	}
	adapter := scas.NewAdapter(strings.TrimSpace(policyCSV))
	e, err := casbin.NewEnforcer(m, adapter)
	if err != nil {
		return nil, fmt.Errorf("new enforcer: %w", err)
	}
	return &Enforcer{enforcer: e}, nil
}

// NewEnforcer builds an enforcer pre-loaded with the built-in policy only.
func NewEnforcer() (*Enforcer, error) {
	return newFromPolicy(BuiltinPolicyCSV)
}

// Enforce returns true if subject sub may perform act on res/obj.
func (e *Enforcer) Enforce(sub, res, act, obj string) (bool, error) {
	return e.enforcer.Enforce(sub, res, act, obj)
}
```

Note: if `string-adapter` import path differs in the pinned Casbin version, run `go doc github.com/casbin/casbin/v2/persist/string-adapter` to confirm; argo-cd uses this same adapter, so the dependency resolves.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/rbac/ -run TestBuiltinRoleMatrix -v`
Expected: PASS (9 sub-tests). If `casbin` is not yet in `go.mod`, run `go get github.com/casbin/casbin/v2` first, then re-run.

- [ ] **Step 5: Commit**

```bash
git add server/auth/rbac/enforcer.go server/auth/rbac/enforcer_test.go go.mod go.sum
git commit -m "feat(rbac): enforcer over built-in policy with role matrix tests"
```

---

### Task 4: User policy + group bindings + default role

**Files:**
- Modify: `server/auth/rbac/enforcer.go`
- Test: `server/auth/rbac/user_policy_test.go`

**Interfaces:**
- Consumes: `Enforcer` (Task 3).
- Produces:
  - `func (e *Enforcer) SetUserPolicy(policyCSV string) error` — rebuilds the enforcer with built-in policy + appended user policy.
  - `func (e *Enforcer) EnforceWithDefault(defaultRole, sub, res, act, obj string) (bool, error)` — if direct enforce denies and `defaultRole != ""`, retry as `defaultRole`.

- [ ] **Step 1: Write the failing test**

```go
package rbac

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserPolicyGroupBinding(t *testing.T) {
	e, err := NewEnforcer()
	require.NoError(t, err)

	// Bind user alice to role:operator, and grant a custom narrow rule to bob.
	userCSV := `
g, alice, role:operator
p, bob, rollouts, get, dev/*, allow
`
	require.NoError(t, e.SetUserPolicy(userCSV))

	ok, err := e.Enforce("alice", "rollouts", "promote", "prod/web")
	require.NoError(t, err)
	assert.True(t, ok, "alice inherits operator promote")

	ok, err = e.Enforce("bob", "rollouts", "get", "dev/web")
	require.NoError(t, err)
	assert.True(t, ok, "bob get dev")

	ok, err = e.Enforce("bob", "rollouts", "get", "prod/web")
	require.NoError(t, err)
	assert.False(t, ok, "bob denied outside dev/*")
}

func TestEnforceWithDefaultRole(t *testing.T) {
	e, err := NewEnforcer()
	require.NoError(t, err)

	// carol has no binding; default role readonly grants get.
	ok, err := e.EnforceWithDefault("role:readonly", "carol", "rollouts", "get", "prod/web")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = e.EnforceWithDefault("role:readonly", "carol", "rollouts", "promote", "prod/web")
	require.NoError(t, err)
	assert.False(t, ok, "default readonly cannot promote")

	// empty default = locked down
	ok, err = e.EnforceWithDefault("", "carol", "rollouts", "get", "prod/web")
	require.NoError(t, err)
	assert.False(t, ok)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/rbac/ -run 'TestUserPolicyGroupBinding|TestEnforceWithDefaultRole' -v`
Expected: FAIL — `undefined: (*Enforcer).SetUserPolicy`, `undefined: (*Enforcer).EnforceWithDefault`.

- [ ] **Step 3: Write minimal implementation (append to `enforcer.go`)**

```go
// SetUserPolicy rebuilds the enforcer with the built-in policy plus the
// user-supplied policy (from argo-rollouts-dashboard-rbac-cm policy.csv).
func (e *Enforcer) SetUserPolicy(policyCSV string) error {
	combined := strings.TrimSpace(BuiltinPolicyCSV) + "\n" + strings.TrimSpace(policyCSV)
	rebuilt, err := newFromPolicy(combined)
	if err != nil {
		return err
	}
	e.enforcer = rebuilt.enforcer
	return nil
}

// EnforceWithDefault enforces directly for sub; if denied and defaultRole is
// non-empty, it retries enforcement as defaultRole. Empty defaultRole means
// deny-by-default (locked down).
func (e *Enforcer) EnforceWithDefault(defaultRole, sub, res, act, obj string) (bool, error) {
	ok, err := e.enforcer.Enforce(sub, res, act, obj)
	if err != nil {
		return false, err
	}
	if ok || defaultRole == "" {
		return ok, nil
	}
	return e.enforcer.Enforce(defaultRole, res, act, obj)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/rbac/ -run 'TestUserPolicyGroupBinding|TestEnforceWithDefaultRole' -v`
Expected: PASS (2 tests, all sub-assertions).

- [ ] **Step 5: Run the full package + vet**

Run: `go test ./server/auth/rbac/... && go vet ./server/auth/rbac/...`
Expected: ok, no vet complaints.

- [ ] **Step 6: Commit**

```bash
git add server/auth/rbac/enforcer.go server/auth/rbac/user_policy_test.go
git commit -m "feat(rbac): user policy, group bindings, default role fallback"
```

---

## Self-Review

**Spec coverage (vs §6 RBAC model):**
- Resources/actions/object format → Task 1. ✅
- Built-in admin/readonly/operator → Task 2. ✅
- `Enforce(sub,res,act,obj)` → Task 3. ✅
- `policy.csv` user policy + `policy.default` (deny-by-default) → Task 4. ✅
- Group/scope mapping (OIDC claims → groups) is consumed in Plan 4/5 via `g, <group>, <role>` lines fed to `SetUserPolicy`; the mechanism (group bindings) is verified in Task 4. ✅
- `policy.matchMode` glob vs regex → NOT in this plan. Built-in model uses `globMatch`. Note: regex mode deferred to a follow-up task in Plan 3/4 (settings supplies matchMode); flagged here as a known gap, acceptable since glob is the default.

**Placeholder scan:** No TBD/TODO; every code step has complete code. ✅

**Type consistency:** `Enforcer`, `NewEnforcer`, `Enforce`, `SetUserPolicy`, `EnforceWithDefault`, `newFromPolicy`, `ModelConf`, `BuiltinPolicyCSV`, `BuiltinRoles` used consistently across Tasks 1–4. ✅

**Known gap to carry into Plan 3:** `policy.matchMode=regex` support (swap `globMatch` for `regexMatch` in model when configured).
