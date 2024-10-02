package rbacpolicy

import (
	"fmt"
	"testing"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/argoproj/argo-rollouts/common"
	"github.com/argoproj/argo-rollouts/test"
	"github.com/argoproj/argo-rollouts/utils/rbac"
)

func TestEnforceAllPolicies(t *testing.T) {
	kubeclientset := fake.NewSimpleClientset(test.NewFakeConfigMap())
	enf := rbac.NewEnforcer(kubeclientset, test.FakeArgoRolloutsNamespace, common.ArgoRolloutsConfigMapName, nil)
	enf.EnableLog(true)
	_ = enf.SetBuiltinPolicy(`p, alice, rollouts, get, my-ns/*, allow` + "\n" + `p, alice, logs, get, my-ns/*, allow` + "\n" + `p, alice, updatecontainer, promote, my-ns/*, allow`)
	_ = enf.SetUserPolicy(`p, bob, rollouts, promote, my-ns/*, allow` + "\n" + `p, bob, logs, get, my-ns/*, allow` + "\n" + `p, bob, updatecontainer, promote, my-ns/*, allow`)
	rbacEnf := NewRBACPolicyEnforcer(enf)
	enf.SetClaimsEnforcerFunc(rbacEnf.EnforceClaims)

	claims := jwt.MapClaims{"sub": "alice"}
	assert.True(t, enf.Enforce(claims, "rollouts", "promote", "my-ns/my-app"))
	assert.True(t, enf.Enforce(claims, "logs", "get", "my-ns/my-app"))
	assert.True(t, enf.Enforce(claims, "updatecontainer", "promote", "my-ns/my-app"))

	claims = jwt.MapClaims{"sub": "bob"}
	assert.True(t, enf.Enforce(claims, "rollouts", "promote", "my-ns/my-app"))
	assert.True(t, enf.Enforce(claims, "logs", "get", "my-ns/my-app"))
	assert.True(t, enf.Enforce(claims, "updatecontainer", "promote", "my-ns/my-app"))

	claims = jwt.MapClaims{"sub": "my-role", "iat": 1234}
	assert.True(t, enf.Enforce(claims, "rollouts", "promote", "my-ns/my-app"))

	claims = jwt.MapClaims{"groups": []string{"my-org:my-team"}}
	assert.True(t, enf.Enforce(claims, "rollouts", "promote", "my-ns/my-app"))

	claims = jwt.MapClaims{"sub": "cathy"}
	assert.False(t, enf.Enforce(claims, "rollouts", "promote", "my-ns/my-app"))

	// AWS cognito returns its groups in  cognito:groups
	rbacEnf.SetScopes([]string{"cognito:groups"})
	claims = jwt.MapClaims{"cognito:groups": []string{"my-org:my-team"}}
	assert.True(t, enf.Enforce(claims, "rollouts", "promote", "my-ns/my-app"))
}

func TestEnforceActionActions(t *testing.T) {
	kubeclientset := fake.NewSimpleClientset(test.NewFakeConfigMap())
	enf := rbac.NewEnforcer(kubeclientset, test.FakeArgoRolloutsNamespace, common.ArgoRolloutsConfigMapName, nil)
	enf.EnableLog(true)
	_ = enf.SetBuiltinPolicy(fmt.Sprintf(`p, alice, rollouts, %s/*, my-ns/*, allow
p, bob, rollouts, %s/argoproj.io/Rollouts/*, my-ns/*, allow
p, cam, rollouts, %s/argoproj.io/Rollouts/updatecontainer, my-ns/*, allow
`, ActionAction, ActionAction, ActionAction))
	rbacEnf := NewRBACPolicyEnforcer(enf)
	enf.SetClaimsEnforcerFunc(rbacEnf.EnforceClaims)

	// Alice has wild-card approval for all actions
	claims := jwt.MapClaims{"sub": "alice"}
	assert.True(t, enf.Enforce(claims, "rollouts", ActionAction+"/argoproj.io/Rollouts/updatecontainer", "my-ns/my-app"))
	// Bob has wild-card approval for all actions under argoproj.io/Rollout
	claims = jwt.MapClaims{"sub": "bob"}
	assert.True(t, enf.Enforce(claims, "rollouts", ActionAction+"/argoproj.io/Rollouts/updatecontainer", "my-ns/my-app"))
	// Cam only has approval for actions/argoproj.io/Rollout:updatecontainer
	claims = jwt.MapClaims{"sub": "cam"}
	assert.True(t, enf.Enforce(claims, "rollouts", ActionAction+"/argoproj.io/Rollouts/updatecontainer", "my-ns/my-app"))

	// Eve does not have approval for any actions
	claims = jwt.MapClaims{"sub": "eve"}
	assert.False(t, enf.Enforce(claims, "rollouts", ActionAction+"/argoproj.io/Rollouts/updatecontainer", "my-ns/my-app"))
}

func TestInvalidatedCache(t *testing.T) {
	kubeclientset := fake.NewSimpleClientset(test.NewFakeConfigMap())
	enf := rbac.NewEnforcer(kubeclientset, test.FakeArgoRolloutsNamespace, common.ArgoRolloutsConfigMapName, nil)
	enf.EnableLog(true)
	_ = enf.SetBuiltinPolicy(`p, alice, rollouts, promote, my-ns/*, allow` + "\n" + `p, alice, rollouts, updatecontainer, my-ns/*, allow`)
	_ = enf.SetUserPolicy(`p, bob, rollouts, promote, my-ns/*, allow` + "\n" + `p, bob, rollouts, get, my-ns/*, allow` + "\n" + `p, bob, rollouts, updatecontainer, my-ns/*, allow`)
	rbacEnf := NewRBACPolicyEnforcer(enf)
	enf.SetClaimsEnforcerFunc(rbacEnf.EnforceClaims)

	claims := jwt.MapClaims{"sub": "alice"}
	assert.True(t, enf.Enforce(claims, "rollouts", "promote", "my-ns/my-app"))

	claims = jwt.MapClaims{"sub": "bob"}
	assert.True(t, enf.Enforce(claims, "rollouts", "promote", "my-ns/my-app"))
	assert.True(t, enf.Enforce(claims, "rollouts", "updatecontainer", "my-ns/my-app"))
  assert.False(t, enf.Enforce(claims, "rollouts", "get", "my-ns/my-app"))

	_ = enf.SetBuiltinPolicy(`p, alice, rollouts, promote, my-ns2/*, allow` + "\n" + `p, alice, rollouts, updatecontainer, my-ns2/*, allow`)
	_ = enf.SetUserPolicy(`p, bob, rollouts, promote, my-ns2/*, allow` + "\n" + `p, bob, rollouts, get, my-ns2/*, allow` + "\n" + `p, bob, rollouts, updatecontainer, my-ns2/*, allow`)
	claims = jwt.MapClaims{"sub": "alice"}
	assert.True(t, enf.Enforce(claims, "rollouts", "promote", "my-ns2/my-app"))
  assert.True(t, enf.Enforce(claims, "rollouts", "updatecontainer", "my-ns2/my-app"))
  assert.False(t, enf.Enforce(claims, "rollouts", "get", "my-ns2/my-app"))

	claims = jwt.MapClaims{"sub": "bob"}
	assert.True(t, enf.Enforce(claims, "rollouts", "promote", "my-ns2/my-app"))

	claims = jwt.MapClaims{"sub": "alice"}
	assert.False(t, enf.Enforce(claims, "rollouts", "promote", "my-ns/my-app"))

	claims = jwt.MapClaims{"sub": "bob"}
	assert.False(t, enf.Enforce(claims, "rollouts", "promote", "my-ns/my-app"))
}

func TestGetScopes_DefaultScopes(t *testing.T) {
	rbacEnforcer := NewRBACPolicyEnforcer(nil)

	scopes := rbacEnforcer.GetScopes()
	assert.Equal(t, scopes, defaultScopes)
}

func TestGetScopes_CustomScopes(t *testing.T) {
	rbacEnforcer := NewRBACPolicyEnforcer(nil)
	customScopes := []string{"custom"}
	rbacEnforcer.SetScopes(customScopes)

	scopes := rbacEnforcer.GetScopes()
	assert.Equal(t, scopes, customScopes)
}
