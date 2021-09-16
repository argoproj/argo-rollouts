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

	for _, filename := range []string{"testdata/valid.yml", "testdata/valid-workload-ref.yaml", "testdata/valid-with-another-empty-object.yml"} {
		cmd.SetArgs([]string{"-f", filename})
		err := cmd.Execute()
		assert.NoError(t, err)

		stdout := o.Out.(*bytes.Buffer).String()
		assert.Empty(t, stdout)
	}
}

func TestLintInvalidRollout(t *testing.T) {
	var runCmd func(string, string)

	tests := []struct {
		filename string
		errmsg   string
	}{

		{
			"testdata/invalid.yml",
			"Error: spec.strategy.maxSurge: Invalid value: intstr.IntOrString{Type:0, IntVal:0, StrVal:\"\"}: MaxSurge and MaxUnavailable both can not be zero\n",
		},
		{
			"testdata/invalid-multiple-docs.yml",
			"Error: spec.strategy.maxSurge: Invalid value: intstr.IntOrString{Type:0, IntVal:0, StrVal:\"\"}: MaxSurge and MaxUnavailable both can not be zero\n",
		},

		{
			"testdata/invalid-unknown-field.yml",
			"Error: error unmarshaling JSON: while decoding JSON: json: unknown field \"unknown-strategy\"\n",
		},
	}

	runCmd = func(filename string, errmsg string) {
		tf, o := options.NewFakeArgoRolloutsOptions()
		defer tf.Cleanup()

		cmd := NewCmdLint(o)
		cmd.PersistentPreRunE = o.PersistentPreRunE
		cmd.SetArgs([]string{"-f", filename})
		err := cmd.Execute()
		assert.Error(t, err)

		stdout := o.Out.(*bytes.Buffer).String()
		stderr := o.ErrOut.(*bytes.Buffer).String()
		assert.Empty(t, stdout)
		assert.Equal(t, errmsg, stderr)
	}

	for _, t := range tests {
		runCmd(t.filename, t.errmsg)
	}
}
