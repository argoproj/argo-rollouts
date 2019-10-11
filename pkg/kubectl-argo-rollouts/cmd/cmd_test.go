package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	options "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
)

func TestCmdArgoRolloutsCmdUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdArgoRollouts(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "Usage:")
	assert.Contains(t, stderr, "kubectl-argo-rollouts COMMAND")
}
