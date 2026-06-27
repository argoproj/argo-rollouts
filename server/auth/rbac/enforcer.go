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
