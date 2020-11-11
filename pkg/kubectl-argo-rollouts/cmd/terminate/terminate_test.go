package terminate

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

func TestTerminateCmdUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdTerminate(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "Usage:\n  terminate <analysisrun|experiment> RESOURCE")
}

func TestTerminateAnalysisCmdUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdTerminateAnalysisRun(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "Usage:\n  analysisrun ANALYSISRUN")
	assert.Contains(t, stderr, "Aliases:\n  analysisrun, ar, analysisruns")
}

func TestTerminateExperimentCmdUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdTerminateExperiment(o)
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

func TestTerminateAnalysisRunCmd(t *testing.T) {
	ar := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: "test",
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ar)
	defer tf.Cleanup()
	retried := false
	fakeClient := o.RolloutsClient.(*fakeroclient.Clientset)
	fakeClient.PrependReactor("patch", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if patchAction, ok := action.(kubetesting.PatchAction); ok {
			if string(patchAction.GetPatch()) == terminatePatch {
				retried = true
			}
		}
		return true, &ar, nil
	})

	cmd := NewCmdTerminateAnalysisRun(o)
	o.AddKubectlFlags(cmd)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook", "-n", "test"})
	err := cmd.Execute()
	assert.Nil(t, err)

	assert.True(t, retried)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, stdout, "analysisRun 'guestbook' terminated\n")
	assert.Empty(t, stderr)
}

func TestTerminateAnalysisRunCmdError(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions(&v1alpha1.AnalysisRun{})
	defer tf.Cleanup()
	cmd := NewCmdTerminateAnalysisRun(o)
	o.AddKubectlFlags(cmd)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"doesnotexist", "-n", "test"})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: analysisruns.argoproj.io \"doesnotexist\" not found\n", stderr)
}

func TestTerminateExperimentCmd(t *testing.T) {
	ex := v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: "test",
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ex)
	defer tf.Cleanup()
	retried := false
	fakeClient := o.RolloutsClient.(*fakeroclient.Clientset)
	fakeClient.PrependReactor("patch", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if patchAction, ok := action.(kubetesting.PatchAction); ok {
			if string(patchAction.GetPatch()) == terminatePatch {
				retried = true
			}
		}
		return true, &ex, nil
	})

	cmd := NewCmdTerminateExperiment(o)
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

func TestTerminateExperimentCmdError(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions(&v1alpha1.AnalysisRun{})
	defer tf.Cleanup()
	cmd := NewCmdTerminateExperiment(o)
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
