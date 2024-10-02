package rbacpolicy

import (
	"github.com/golang-jwt/jwt/v4"
	log "github.com/sirupsen/logrus"
	jwtutil "github.com/argoproj/argo-rollouts/utils/jwt"
	"github.com/argoproj/argo-rollouts/utils/rbac"
)

const (
	// please add new items to Resources
	ResourceRollouts = "rollouts"

	// please add new items to Actions
	ActionGet             = "get"
	ActionUpdateContainer = "updatecontainer"
	ActionRestart         = "restart"
	ActionRetry           = "retry"
	ActionAbort           = "abort"
	ActionPromote         = "promote"
	ActionPromoteFull     = "promotefull"
  ActionAction          = "action"
)

var (
	defaultScopes = []string{"groups"}
	Resources     = []string{
		ResourceRollouts,
	}
	Actions = []string{
		ActionGet,
		ActionUpdateContainer,
    ActionRestart,
    ActionRetry,
    ActionAbort,
    ActionPromote,
    ActionPromoteFull,
	}
)

// RBACPolicyEnforcer provides an RBAC Claims Enforcer.
type RBACPolicyEnforcer struct {
	enf        *rbac.Enforcer
	scopes     []string
}

// NewRBACPolicyEnforcer returns a new RBAC Enforcer for the Argo Rollouts API Server
func NewRBACPolicyEnforcer(enf *rbac.Enforcer) *RBACPolicyEnforcer {
	return &RBACPolicyEnforcer{
		enf:        enf,
		scopes:     nil,
	}
}

func (p *RBACPolicyEnforcer) SetScopes(scopes []string) {
	p.scopes = scopes
}

func (p *RBACPolicyEnforcer) GetScopes() []string {
	scopes := p.scopes
	if scopes == nil {
		scopes = defaultScopes
	}
	return scopes
}

// EnforceClaims is an RBAC claims enforcer specific to the Argo Rollout API server
func (p *RBACPolicyEnforcer) EnforceClaims(claims jwt.Claims, rvals ...interface{}) bool {
	mapClaims, err := jwtutil.MapClaims(claims)
	if err != nil {
		return false
	}

	subject := jwtutil.StringField(mapClaims, "sub")
	// Check if the request is for an application resource. We have special enforcement which takes
	// into consideration the project's token and group bindings
	var runtimePolicy string

	// NOTE: This calls prevent multiple creation of the wrapped enforcer
	enforcer := p.enf.CreateEnforcerWithRuntimePolicy(runtimePolicy)

	// Check the subject. This is typically the 'admin' case.
	// NOTE: the call to EnforceWithCustomEnforcer will also consider the default role
	vals := append([]interface{}{subject}, rvals[1:]...)
	if p.enf.EnforceWithCustomEnforcer(enforcer, vals...) {
		return true
	}

	scopes := p.scopes
	if scopes == nil {
		scopes = defaultScopes
	}
	// Finally check if any of the user's groups grant them permissions
	groups := jwtutil.GetScopeValues(mapClaims, scopes)

	// Get groups to reduce the amount to checking groups
	groupingPolicies, err := enforcer.GetGroupingPolicy()
	if err != nil {
		log.WithError(err).Error("failed to get grouping policy")
		return false
	}
	for gidx := range groups {
		for gpidx := range groupingPolicies {
			// Prefilter user groups by groups defined in the model
			if groupingPolicies[gpidx][0] == groups[gidx] {
				vals := append([]interface{}{groups[gidx]}, rvals[1:]...)
				if p.enf.EnforceWithCustomEnforcer(enforcer, vals...) {
					return true
				}
				break
			}
		}
	}
	logCtx := log.WithFields(log.Fields{"claims": claims, "rval": rvals, "subject": subject, "groups": groups, "scopes": scopes})
	logCtx.Debug("enforce failed")
	return false
}
