package retry

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			Namespace: metav1.NamespaceDefault,
		},
		Status: v1alpha1.RolloutStatus{Abort: true},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()
	cmd := NewCmdRetryRollout(o)
	o.AddKubectlFlags(cmd)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook"})
	err := cmd.Execute()
	assert.Nil(t, err)

	retried, err := o.RolloutsClient.ArgoprojV1alpha1().Rollouts(ro.Namespace).Get(context.Background(), "guestbook", metav1.GetOptions{})
	require.NoError(t, err)
	assert.False(t, retried.Status.Abort)
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

func TestRetryRolloutPreservesStatus(t *testing.T) {
	for _, tt := range []struct {
		name  string
		abort bool
		phase v1alpha1.RolloutPhase
	}{
		{name: "Aborted", abort: true, phase: v1alpha1.RolloutPhaseDegraded},
		{name: "Progressing", phase: v1alpha1.RolloutPhaseProgressing},
		{name: "Paused", phase: v1alpha1.RolloutPhasePaused},
		{name: "Healthy", phase: v1alpha1.RolloutPhaseHealthy},
	} {
		t.Run(tt.name, func(t *testing.T) {
			now := metav1.NewTime(time.Now().Truncate(time.Second))
			ro := &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{Name: "guestbook", Namespace: "test"},
				Status: v1alpha1.RolloutStatus{
					Abort:           tt.abort,
					Phase:           tt.phase,
					StableRS:        "stable",
					CurrentPodHash:  "candidate",
					PauseConditions: []v1alpha1.PauseCondition{{Reason: v1alpha1.PauseReasonCanaryPauseStep, StartTime: now}},
					BlueGreen: v1alpha1.BlueGreenStatus{
						ActiveSelector:                 "stable",
						PrePromotionAnalysisRunStatus:  &v1alpha1.RolloutAnalysisRunStatus{Name: "pre"},
						PostPromotionAnalysisRunStatus: &v1alpha1.RolloutAnalysisRunStatus{Name: "post"},
					},
					Canary: v1alpha1.CanaryStatus{
						CurrentStepAnalysisRunStatus:       &v1alpha1.RolloutAnalysisRunStatus{Name: "step"},
						CurrentBackgroundAnalysisRunStatus: &v1alpha1.RolloutAnalysisRunStatus{Name: "background"},
					},
				},
			}
			if tt.abort {
				ro.Status.AbortedAt = &now
			}
			expected := ro.DeepCopy()
			if tt.abort {
				expected.Status.Abort = false
				expected.Status.BlueGreen.PostPromotionAnalysisRunStatus = nil
				expected.Status.Canary.CurrentStepAnalysisRunStatus = nil
				expected.Status.Canary.CurrentBackgroundAnalysisRunStatus = nil
			}
			client := fakeroclient.NewSimpleClientset(ro)
			rollouts := client.ArgoprojV1alpha1().Rollouts(ro.Namespace)
			_, err := RetryRollout(rollouts, ro.Name)
			require.NoError(t, err)
			actual, err := rollouts.Get(context.Background(), ro.Name, metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, expected.Status, actual.Status)
		})
	}
}
