package lint

import (
	"bytes"
	"testing"

	options "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
	"github.com/stretchr/testify/assert"
)

func TestLintValidRollout(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()

	cmd := NewCmdLint(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"-f", "testdata/valid.yml"})
	err := cmd.Execute()
	assert.NoError(t, err)

	stdout := o.Out.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
}

func TestLintInvalidRollout(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()

	cmd := NewCmdLint(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"-f", "testdata/invalid.yml"})
	err := cmd.Execute()
	assert.Error(t, err)

	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: spec.strategy.maxSurge: Invalid value: intstr.IntOrString{Type:0, IntVal:0, StrVal:\"\"}: MaxSurge and MaxUnavailable both can not be zero\n", stderr)
}
