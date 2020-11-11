package retry

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubetesting "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	fakeroclient "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	options "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
)

func TestRetryCmdUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdRetry(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "Usage:\n  retry <rollout|experiment> RESOURCE")
}

func TestRetryRolloutCmdUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdRetryRollout(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "Usage:\n  rollout ROLLOUT")
	assert.Contains(t, stderr, "Aliases:\n  rollout, ro, rollouts")
}

func TestRetryExperimentCmdUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdRetryExperiment(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "Usage:\n  experiment EXPERIMENT")
	assert.Contains(t, stderr, "Aliases:\n  experiment, exp, experiments")
}

func TestRetryRolloutCmd(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: "test",
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()
	retried := false
	fakeClient := o.RolloutsClient.(*fakeroclient.Clientset)
	fakeClient.PrependReactor("patch", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if patchAction, ok := action.(kubetesting.PatchAction); ok {
			if string(patchAction.GetPatch()) == retryRolloutPatch {
				retried = true
			}
		}
		return true, &ro, nil
	})

	cmd := NewCmdRetryRollout(o)
	o.AddKubectlFlags(cmd)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook", "-n", "test"})
	err := cmd.Execute()
	assert.Nil(t, err)

	assert.True(t, retried)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, stdout, "rollout 'guestbook' retried\n")
	assert.Empty(t, stderr)
}

func TestRetryRolloutCmdError(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions(&v1alpha1.Rollout{})
	defer tf.Cleanup()
	cmd := NewCmdRetryRollout(o)
	o.AddKubectlFlags(cmd)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"doesnotexist", "-n", "test"})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: rollouts.argoproj.io \"doesnotexist\" not found\n", stderr)
}

func TestRetryExperimentCmd(t *testing.T) {
	ro := v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: "test",
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()
	retried := false
	fakeClient := o.RolloutsClient.(*fakeroclient.Clientset)
	fakeClient.PrependReactor("patch", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if patchAction, ok := action.(kubetesting.PatchAction); ok {
			if string(patchAction.GetPatch()) == retryExperimentPatch {
				retried = true
			}
		}
		return true, &ro, nil
	})

	cmd := NewCmdRetryExperiment(o)
	o.AddKubectlFlags(cmd)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook", "-n", "test"})
	err := cmd.Execute()
	assert.Nil(t, err)

	assert.True(t, retried)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, stdout, "experiment 'guestbook' retried\n")
	assert.Empty(t, stderr)
}

func TestRetryExperimentCmdError(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions(&v1alpha1.Rollout{})
	defer tf.Cleanup()
	cmd := NewCmdRetryExperiment(o)
	o.AddKubectlFlags(cmd)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"doesnotexist", "-n", "test"})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: experiments.argoproj.io \"doesnotexist\" not found\n", stderr)
}
