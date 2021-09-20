package completion

import (
	"bytes"
	"testing"

	options "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
	"github.com/tj/assert"
)

func TestShellNotFound(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdCompletion(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"does-not-exist"})
	err := cmd.Execute()

	assert.Error(t, err)
	assert.Equal(t, "invalid argument \"does-not-exist\" for \"completion\"", err.Error())

	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stderr)

	stdout := o.Out.(*bytes.Buffer).String()
	assert.Empty(t, stdout)

}

func TestFish(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdCompletion(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"fish"})
	err := cmd.Execute()
	assert.NoError(t, err)

	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stderr)

	stdout := o.Out.(*bytes.Buffer).String()
	assert.Contains(t, stdout, "fish completion")

}

func TestBash(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdCompletion(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"bash"})
	err := cmd.Execute()
	assert.NoError(t, err)

	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stderr)

	stdout := o.Out.(*bytes.Buffer).String()
	assert.Contains(t, stdout, "bash completion")

}

func TestZsh(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdCompletion(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"zsh"})
	err := cmd.Execute()
	assert.NoError(t, err)

	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stderr)

	stdout := o.Out.(*bytes.Buffer).String()
	assert.Contains(t, stdout, "zsh completion")

}

func TestPowershell(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdCompletion(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"powershell"})
	err := cmd.Execute()
	assert.NoError(t, err)

	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stderr)

	stdout := o.Out.(*bytes.Buffer).String()
	assert.Contains(t, stdout, "powershell completion")

}
