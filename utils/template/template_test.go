package query

import (
	"fmt"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"k8s.io/utils/pointer"

	"github.com/stretchr/testify/assert"
)

func TestResolveArgsWithNoSubstitution(t *testing.T) {
	query, err := ResolveArgs("test", nil)
	assert.Nil(t, err)
	assert.Equal(t, "test", query)
}

func TestResolveArgsRemoveWhiteSpace(t *testing.T) {
	args := []v1alpha1.Argument{{
		Name:  "var",
		Value: pointer.StringPtr("foo"),
	}}
	query, err := ResolveArgs("test-{{ args.var }}", args)
	assert.Nil(t, err)
	assert.Equal(t, "test-foo", query)
}

func TestResolveArgsWithSubstitution(t *testing.T) {
	args := []v1alpha1.Argument{{
		Name:  "var",
		Value: pointer.StringPtr("foo"),
	}}
	query, err := ResolveArgs("test-{{args.var}}", args)
	assert.Nil(t, err)
	assert.Equal(t, "test-foo", query)
}

func TestInvalidTemplate(t *testing.T) {
	_, err := ResolveArgs("test-{{args.var", nil)
	assert.Equal(t, fmt.Errorf("Cannot find end tag=\"}}\" in the template=\"test-{{args.var\" starting from \"args.var\""), err)
}

func TestMissingArgs(t *testing.T) {
	_, err := ResolveArgs("test-{{args.var}}", nil)
	assert.NotNil(t, err)
	assert.Equal(t, fmt.Errorf("failed to resolve {{args.var}}"), err)
}
