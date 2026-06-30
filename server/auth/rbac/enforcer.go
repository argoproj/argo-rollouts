package rbac

import (
	"fmt"
	"strings"
	"sync"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	scas "github.com/casbin/casbin/v2/persist/string-adapter"
)

// Enforcer wraps a Casbin enforcer loaded with the dashboard model and policy.
type Enforcer struct {
	mu       sync.RWMutex
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
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.enforcer.Enforce(sub, res, act, obj)
}

// SetUserPolicy rebuilds the enforcer with the built-in policy plus the
// user-supplied policy (from argo-rollouts-dashboard-rbac-cm policy.csv).
// The rebuild happens outside the lock so readers are not blocked during the
// (potentially slow) casbin model construction; only the final pointer swap is
// protected.
func (e *Enforcer) SetUserPolicy(policyCSV string) error {
	combined := strings.TrimSpace(BuiltinPolicyCSV) + "\n" + strings.TrimSpace(policyCSV)
	// Build the new enforcer outside the lock — failed rebuilds never hold it.
	rebuilt, err := newFromPolicy(combined)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.enforcer = rebuilt.enforcer
	return nil
}

// EnforceWithDefault enforces directly for sub; if denied and defaultRole is
// non-empty, it retries enforcement as defaultRole. Empty defaultRole means
// deny-by-default (locked down).
//
// A single RLock covers both reads of e.enforcer. Do NOT call the exported
// Enforce method here — sync.RWMutex is not reentrant and would deadlock.
func (e *Enforcer) EnforceWithDefault(defaultRole, sub, res, act, obj string) (bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	ok, err := e.enforcer.Enforce(sub, res, act, obj)
	if err != nil {
		return false, err
	}
	if ok || defaultRole == "" {
		return ok, nil
	}
	return e.enforcer.Enforce(defaultRole, res, act, obj)
}
