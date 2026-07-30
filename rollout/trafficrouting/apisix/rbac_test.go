package apisix

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

func TestAPISIXRouteRBAC(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	require.True(t, ok)

	repositoryRoot := filepath.Join(filepath.Dir(testFile), "..", "..", "..")
	manifestPaths := []string{
		filepath.Join(repositoryRoot, "manifests", "role", "argo-rollouts-clusterrole.yaml"),
		filepath.Join(repositoryRoot, "manifests", "install.yaml"),
		filepath.Join(repositoryRoot, "manifests", "namespace-install.yaml"),
	}

	for _, manifestPath := range manifestPaths {
		t.Run(filepath.Base(manifestPath), func(t *testing.T) {
			role := readArgoRolloutsRole(t, manifestPath)
			assertAPISIXRouteRBAC(t, role.Rules)
		})
	}
}

func readArgoRolloutsRole(t *testing.T, manifestPath string) rbacv1.Role {
	t.Helper()

	manifest, err := os.ReadFile(manifestPath)
	require.NoError(t, err)

	for _, document := range bytes.Split(manifest, []byte("\n---\n")) {
		var role rbacv1.Role
		require.NoError(t, yaml.Unmarshal(document, &role))
		if (role.Kind == "Role" || role.Kind == "ClusterRole") &&
			role.Name == "argo-rollouts" {
			return role
		}
	}

	t.Fatalf("argo-rollouts Role or ClusterRole not found in %s", manifestPath)
	return rbacv1.Role{}
}

func assertAPISIXRouteRBAC(t *testing.T, rules []rbacv1.PolicyRule) {
	t.Helper()

	var matchingRules []rbacv1.PolicyRule
	for _, rule := range rules {
		if assert.ObjectsAreEqual([]string{"apisix.apache.org"}, rule.APIGroups) &&
			assert.ObjectsAreEqual([]string{"apisixroutes"}, rule.Resources) {
			matchingRules = append(matchingRules, rule)
		}
	}

	require.Len(t, matchingRules, 1)
	assert.Equal(
		t,
		[]string{"watch", "get", "update", "create", "delete"},
		matchingRules[0].Verbs,
	)
}
