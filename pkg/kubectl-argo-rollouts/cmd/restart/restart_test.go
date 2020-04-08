package restart

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubetesting "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	fakeroclient "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	options "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
)

func TestRestartCmdUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdRestart(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "Usage:")
	assert.Contains(t, stderr, "restart ROLLOUT")
}

func TestRestartInvalidIn(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdRestart(o)
	o.AddKubectlFlags(cmd)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook", "--in", "not-valid-time"})
	assert.Panics(t, func() {
		cmd.Execute()
	})
}

func TestRestartCmdSuccessSetNow(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	now := metav1.Now()
	o.Now = func() metav1.Time {
		return now
	}
	defer tf.Cleanup()
	fakeClient := o.RolloutsClient.(*fakeroclient.Clientset)
	fakeClient.PrependReactor("patch", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if patchAction, ok := action.(kubetesting.PatchAction); ok {
			if string(patchAction.GetPatch()) == fmt.Sprintf(restartPatch, now.UTC().Format(time.RFC3339)) {
				ro.Spec.RestartAt = now.DeepCopy()
			}
		}
		return true, &ro, nil
	})

	cmd := NewCmdRestart(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	o.AddKubectlFlags(cmd)
	cmd.SetArgs([]string{"guestbook"})
	err := cmd.Execute()
	assert.Nil(t, err)

	expectedTime := metav1.NewTime(now.UTC())
	assert.True(t, ro.Spec.RestartAt.Equal(&expectedTime))
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, "rollout 'guestbook' restarts in 0s\n", stdout)
	assert.Empty(t, stderr)
}

func TestRestartCmdSuccessSetIn10Minutes(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	now := metav1.Now()
	expectedTime := metav1.NewTime(now.Add(10 * time.Minute))
	o.Now = func() metav1.Time {
		return *now.DeepCopy()
	}
	defer tf.Cleanup()
	fakeClient := o.RolloutsClient.(*fakeroclient.Clientset)
	fakeClient.PrependReactor("patch", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if patchAction, ok := action.(kubetesting.PatchAction); ok {
			if string(patchAction.GetPatch()) == fmt.Sprintf(restartPatch, expectedTime.UTC().Format(time.RFC3339)) {
				ro.Spec.RestartAt = expectedTime.DeepCopy()
			}
		}
		return true, &ro, nil
	})

	cmd := NewCmdRestart(o)
	o.AddKubectlFlags(cmd)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook", "--in", "10m"})
	err := cmd.Execute()
	assert.Nil(t, err)

	assert.True(t, ro.Spec.RestartAt.Equal(&expectedTime))
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, "rollout 'guestbook' restarts in 10m\n", stdout)
	assert.Empty(t, stderr)
}

func TestRestartCmdPatchError(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()
	cmd := NewCmdRestart(o)
	o.AddKubectlFlags(cmd)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook"})
	fakeClient := o.RolloutsClient.(*fakeroclient.Clientset)
	fakeClient.PrependReactor("patch", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, fmt.Errorf("Intentional Error")
	})

	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: Intentional Error\n", stderr)
}

func TestRestartCmdNotFoundError(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions(&v1alpha1.Rollout{})
	defer tf.Cleanup()
	cmd := NewCmdRestart(o)
	o.AddKubectlFlags(cmd)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"doesnotexist"})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: rollouts.argoproj.io \"doesnotexist\" not found\n", stderr)
}
