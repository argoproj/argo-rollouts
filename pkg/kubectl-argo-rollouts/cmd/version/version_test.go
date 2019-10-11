package version

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	options "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
)

func TestVersionCmd(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdVersion(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	assert.Contains(t, stdout, "kubectl-argo-rollouts: v99.99.99+unknown\n")
	assert.Contains(t, stdout, "BuildDate: 1970-01-01T00:00:00Z\n")
}

func TestVersionCmdShort(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdVersion(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"--short"})
	err := cmd.Execute()
	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	assert.Equal(t, "kubectl-argo-rollouts: v99.99.99+unknown\n", stdout)
}
