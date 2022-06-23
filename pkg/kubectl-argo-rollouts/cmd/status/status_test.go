package status

import (
	"bytes"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info/testdata"
	options "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
	"github.com/stretchr/testify/assert"
)

const noWatch = "--watch=false"

func TestStatusUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdStatus(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()

	assert.Error(t, err)
}

func TestStatusRolloutNotFound(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdStatus(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"does-not-exist", noWatch})
	err := cmd.Execute()

	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: rollout.argoproj.io \"does-not-exist\" not found\n", stderr)
}

func TestWatchStatusRolloutNotFound(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdStatus(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"does-not-exist"})
	err := cmd.Execute()

	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: rollout.argoproj.io \"does-not-exist\" not found\n", stderr)
}

func TestStatusBlueGreenRollout(t *testing.T) {
	rolloutObjs := testdata.NewBlueGreenRollout()

	tf, o := options.NewFakeArgoRolloutsOptions(rolloutObjs.AllObjects()...)
	o.RESTClientGetter = tf.WithNamespace(rolloutObjs.Rollouts[0].Namespace)
	defer tf.Cleanup()
	cmd := NewCmdStatus(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{rolloutObjs.Rollouts[0].Name, noWatch})
	err := cmd.Execute()

	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, "Paused - BlueGreenPause\n", stdout)
	assert.Empty(t, stderr)
}

func TestStatusInvalidRollout(t *testing.T) {
	rolloutObjs := testdata.NewInvalidRollout()

	tf, o := options.NewFakeArgoRolloutsOptions(rolloutObjs.AllObjects()...)
	o.RESTClientGetter = tf.WithNamespace(rolloutObjs.Rollouts[0].Namespace)
	defer tf.Cleanup()
	cmd := NewCmdStatus(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{rolloutObjs.Rollouts[0].Name, noWatch})
	err := cmd.Execute()

	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, "Degraded\n", stdout)
	assert.Equal(t, "Error: The rollout is in a degraded state with message: InvalidSpec: The Rollout \"rollout-invalid\" is invalid: spec.template.metadata.labels: Invalid value: map[string]string{\"app\":\"doesnt-match\"}: `selector` does not match template `labels`\n", stderr)
}

func TestStatusAbortedRollout(t *testing.T) {
	rolloutObjs := testdata.NewAbortedRollout()

	tf, o := options.NewFakeArgoRolloutsOptions(rolloutObjs.AllObjects()...)
	o.RESTClientGetter = tf.WithNamespace(rolloutObjs.Rollouts[0].Namespace)
	defer tf.Cleanup()
	cmd := NewCmdStatus(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{rolloutObjs.Rollouts[0].Name, noWatch})
	err := cmd.Execute()

	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, "Degraded\n", stdout)
	assert.Equal(t, "Error: The rollout is in a degraded state with message: RolloutAborted: metric \"web\" assessed Failed due to failed (1) > failureLimit (0)\n", stderr)
}

func TestWatchAbortedRollout(t *testing.T) {
	rolloutObjs := testdata.NewAbortedRollout()

	tf, o := options.NewFakeArgoRolloutsOptions(rolloutObjs.AllObjects()...)
	o.RESTClientGetter = tf.WithNamespace(rolloutObjs.Rollouts[0].Namespace)
	defer tf.Cleanup()
	cmd := NewCmdStatus(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{rolloutObjs.Rollouts[0].Name})
	err := cmd.Execute()

	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, "Degraded - RolloutAborted: metric \"web\" assessed Failed due to failed (1) > failureLimit (0)\n", stdout)
	assert.Equal(t, "Error: The rollout is in a degraded state with message: RolloutAborted: metric \"web\" assessed Failed due to failed (1) > failureLimit (0)\n", stderr)
}

func TestWatchTimeoutRollout(t *testing.T) {
	rolloutObjs := testdata.NewBlueGreenRollout()

	tf, o := options.NewFakeArgoRolloutsOptions(rolloutObjs.AllObjects()...)
	o.RESTClientGetter = tf.WithNamespace(rolloutObjs.Rollouts[0].Namespace)
	defer tf.Cleanup()
	cmd := NewCmdStatus(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{rolloutObjs.Rollouts[0].Name, "--timeout=1s"})
	err := cmd.Execute()

	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, "Paused - BlueGreenPause\n", stdout)
	assert.Equal(t, "Error: Rollout status watch exceeded timeout\n", stderr)
}
