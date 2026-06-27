package auth

import "testing"

import "github.com/stretchr/testify/assert"

type nsNameReq struct{ ns, name string }

func (r nsNameReq) GetNamespace() string { return r.ns }
func (r nsNameReq) GetName() string      { return r.name }

type nsRolloutReq struct{ ns, rollout string }

func (r nsRolloutReq) GetNamespace() string { return r.ns }
func (r nsRolloutReq) GetRollout() string   { return r.rollout }

type nsOnlyReq struct{ ns string }

func (r nsOnlyReq) GetNamespace() string { return r.ns }

// nameAndRolloutReq exposes BOTH GetName and GetRollout, to prove precedence.
type nameAndRolloutReq struct{ ns, name, rollout string }

func (r nameAndRolloutReq) GetNamespace() string { return r.ns }
func (r nameAndRolloutReq) GetName() string      { return r.name }
func (r nameAndRolloutReq) GetRollout() string   { return r.rollout }

func TestObjectFromNamespaceAndName(t *testing.T) {
	assert.Equal(t, "prod/web", objectFromRequest(nsNameReq{ns: "prod", name: "web"}))
}

func TestObjectFromNamespaceAndRollout(t *testing.T) {
	// SetImage/Undo requests expose the rollout name via GetRollout().
	assert.Equal(t, "prod/api", objectFromRequest(nsRolloutReq{ns: "prod", rollout: "api"}))
}

func TestObjectNamespaceOnlyIsWildcardName(t *testing.T) {
	// List/Watch over a namespace => name wildcard.
	assert.Equal(t, "prod/*", objectFromRequest(nsOnlyReq{ns: "prod"}))
}

func TestObjectEmptyRequest(t *testing.T) {
	// A request exposing no getters => "/*".
	assert.Equal(t, "/*", objectFromRequest(struct{}{}))
}

func TestObjectNamePreferredOverRollout(t *testing.T) {
	// If both GetName and GetRollout exist, a non-empty GetName wins.
	assert.Equal(t, "prod/web", objectFromRequest(nameAndRolloutReq{ns: "prod", name: "web", rollout: "api"}))
}

func TestObjectFallsBackToRolloutWhenNameEmpty(t *testing.T) {
	// Both getters present but GetName empty => GetRollout is used.
	assert.Equal(t, "prod/api", objectFromRequest(nameAndRolloutReq{ns: "prod", name: "", rollout: "api"}))
}
