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
