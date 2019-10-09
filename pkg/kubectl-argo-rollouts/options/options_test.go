package options_test

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	fakeoptions "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
)

func TestExample(t *testing.T) {
	_, o := fakeoptions.NewFakeArgoRolloutsOptions()
	example := `
  # do something
  %[1]s foo
`
	assert.Equal(t, "  # do something\n  kubectl argo rollouts foo", o.Example(example))
}

func TestUsageErr(t *testing.T) {
	_, o := fakeoptions.NewFakeArgoRolloutsOptions()

	var cmd = &cobra.Command{
		Use:               "foo SOMETHING",
		SilenceUsage:      true,
		PersistentPreRunE: o.PersistentPreRunE,
		RunE: func(c *cobra.Command, args []string) error {
			return o.UsageErr(c)
		},
	}
	err := cmd.Execute()
	assert.Error(t, err)
	stderr := o.ErrOut.(*bytes.Buffer).String()
	stdout := o.Out.(*bytes.Buffer).String()
	assert.Equal(t, "Usage:\n  foo SOMETHING [flags]\n\nFlags:\n  -h, --help   help for foo\n", stderr)
	assert.Empty(t, stdout)
}

func TestAddKubectlFlags(t *testing.T) {
	_, o := fakeoptions.NewFakeArgoRolloutsOptions()

	var cmd = &cobra.Command{
		Use:               "foo SOMETHING",
		SilenceUsage:      true,
		PersistentPreRunE: o.PersistentPreRunE,
		RunE: func(c *cobra.Command, args []string) error {
			o.Log.Debug("hello world")
			return nil
		},
	}
	o.AddKubectlFlags(cmd)

	cmd.SetArgs([]string{"-v", "9", "--loglevel", "debug"})
	err := cmd.Execute()
	assert.NoError(t, err)

	stderr := o.ErrOut.(*bytes.Buffer).String()
	stdout := o.Out.(*bytes.Buffer).String()
	assert.Contains(t, stderr, "hello world")
	assert.Empty(t, stdout)
}

func TestRolloutsClientset(t *testing.T) {
	iostreams, _, _, _ := genericclioptions.NewTestIOStreams()
	tf := cmdtesting.NewTestFactory().WithNamespace("foo")
	o := options.NewArgoRolloutsOptions(iostreams)
	o.RESTClientGetter = tf
	o.RESTClientGetter.ToRawKubeConfigLoader().Namespace()
	//o.ConfigFlags = tf

	var cmd = &cobra.Command{
		Use:               "foo SOMETHING",
		SilenceUsage:      true,
		PersistentPreRunE: o.PersistentPreRunE,
		RunE: func(c *cobra.Command, args []string) error {
			assert.Equal(t, "foo", o.Namespace())
			_ = o.RolloutsClientset()
			return nil
		},
	}
	o.AddKubectlFlags(cmd)

	err := cmd.Execute()
	assert.NoError(t, err)
}
