package query

import (
	"fmt"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"

	"github.com/stretchr/testify/assert"
)

func TestBuildQueryWithNoSubstitution(t *testing.T) {
	query, err := BuildQuery("test", nil)
	assert.Nil(t, err)
	assert.Equal(t, "test", query)
}

func TestBuildQueryWithSubstitution(t *testing.T) {
	args := []v1alpha1.Argument{{
		Name:  "var",
		Value: "foo",
	}}
	query, err := BuildQuery("test-{{input.var}}", args)
	assert.Nil(t, err)
	assert.Equal(t, "test-foo", query)
}

func TestInvalidTemplate(t *testing.T) {
	_, err := BuildQuery("test-{{input.var", nil)
	assert.Equal(t, fmt.Errorf("Cannot find end tag=\"}}\" in the template=\"test-{{input.var\" starting from \"input.var\""), err)
}

func TestMissingArgs(t *testing.T) {
	_, err := BuildQuery("test-{{input.var}}", nil)
	assert.NotNil(t, err)
	assert.Equal(t, fmt.Errorf("failed to resolve {{input.var}}"), err)
}
