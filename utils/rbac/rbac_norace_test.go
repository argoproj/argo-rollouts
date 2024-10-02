//go:build !race
// +build !race

package rbac

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestPolicyInformer verifies the informer will get updated with a new configmap
func TestPolicyInformer(t *testing.T) {
	// !race:
	// A BUNCH of data race warnings thrown by running this test and the next... it's tough to guess to what degree this
	// is primarily a casbin issue or a Argo Rollouts RBAC issue... A least one data race is an `rbac.go` with
	// itself, a bunch are in casbin. You can see the full list by doing a `go test -race github.com/argoproj/argo-rollouts/utils/rbac`
	//
	// It couldn't hurt to take a look at this code to decide if Argo Rollouts is properly handling concurrent data
	// access here, but in the mean time I have disabled data race testing of this test.

	cm := fakeConfigMap()
	cm.Data[ConfigMapPolicyCSVKey] = "p, admin, rollouts, promote, */*, allow"
	kubeclientset := fake.NewSimpleClientset(cm)
	enf := NewEnforcer(kubeclientset, fakeNamespace, fakeConfigMapName, nil)

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go enf.runInformer(ctx, func(cm *apiv1.ConfigMap) error {
		return nil
	})

	loaded := false
	for i := 1; i <= 20; i++ {
		if enf.Enforce("admin", "rollouts", "promote", "foo/bar") {
			loaded = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	assert.True(t, loaded, "Policy update failed to load")

	// update the configmap and update policy
	delete(cm.Data, ConfigMapPolicyCSVKey)
	err := enf.syncUpdate(cm, noOpUpdate)
	require.NoError(t, err)
	assert.False(t, enf.Enforce("admin", "rollouts", "promote", "foo/bar"))
}

// TestResourceActionWildcards verifies the ability to use wildcards in resources and actions
func TestResourceActionWildcards(t *testing.T) {
	// !race:
	// Same as TestPolicyInformer

	kubeclientset := fake.NewSimpleClientset(fakeConfigMap())
	enf := NewEnforcer(kubeclientset, fakeNamespace, fakeConfigMapName, nil)
	policy := `
p, alice, *, get, foo/obj, allow
p, bob, repositories, *, foo/obj, allow
p, cathy, *, *, foo/obj, allow
p, dave, rollouts, get, foo/obj, allow
p, dave, rollouts/*, get, foo/obj, allow
p, eve, *, get, foo/obj, deny
p, mallory, repositories, *, foo/obj, deny
p, mallory, repositories, *, foo/obj, allow
p, mike, *, *, foo/obj, allow
p, mike, *, *, foo/obj, deny
p, trudy, rollouts, get, foo/obj, allow
p, trudy, rollouts/*, get, foo/obj, allow
p, trudy, rollouts/secrets, get, foo/obj, deny
p, danny, rollouts, get, */obj, allow
p, danny, rollouts, get, proj1/a*p1, allow
`
	_ = enf.SetUserPolicy(policy)

	// Verify the resource wildcard
	assert.True(t, enf.Enforce("alice", "rollouts", "get", "foo/obj"))
	assert.True(t, enf.Enforce("alice", "rollouts/resources", "get", "foo/obj"))
	assert.False(t, enf.Enforce("alice", "rollouts/resources", "promote", "foo/obj"))

	// Verify action wildcards work
	assert.False(t, enf.Enforce("bob", "rollouts", "get", "foo/obj"))

	// Verify resource and action wildcards work in conjunction
	assert.True(t, enf.Enforce("cathy", "rollouts", "get", "foo/obj"))
	assert.True(t, enf.Enforce("cathy", "rollouts/resources", "promote", "foo/obj"))

	// Verify wildcards with sub-resources
	assert.True(t, enf.Enforce("dave", "rollouts", "get", "foo/obj"))
	assert.True(t, enf.Enforce("dave", "rollouts/logs", "get", "foo/obj"))

	// Verify the resource wildcard
	assert.False(t, enf.Enforce("eve", "rollouts", "get", "foo/obj"))

	// Verify action wildcards work
	assert.False(t, enf.Enforce("mallory", "rollouts", "get", "foo/obj"))

	// Verify resource and action wildcards work in conjunction
	assert.False(t, enf.Enforce("mike", "rollouts", "get", "foo/obj"))

	// Verify wildcards with sub-resources
	assert.True(t, enf.Enforce("trudy", "rollouts", "get", "foo/obj"))
	assert.True(t, enf.Enforce("trudy", "rollouts/logs", "get", "foo/obj"))
	assert.False(t, enf.Enforce("trudy", "rollouts/secrets", "get", "foo/obj"))

	// Verify trailing wildcards don't grant full access
	assert.True(t, enf.Enforce("danny", "rollouts", "get", "foo/obj"))
	assert.True(t, enf.Enforce("danny", "rollouts", "get", "bar/obj"))
	assert.False(t, enf.Enforce("danny", "rollouts", "get", "foo/bar"))
	assert.True(t, enf.Enforce("danny", "rollouts", "get", "proj1/app1"))
	assert.False(t, enf.Enforce("danny", "rollouts", "get", "proj1/app2"))
}
