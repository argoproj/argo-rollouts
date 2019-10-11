package resume

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

func TestResumeCmdUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdResume(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "Usage:")
	assert.Contains(t, stderr, "resume ROLLOUT")
}

func TestResumeCmdSuccess(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: "test",
		},
		Spec: v1alpha1.RolloutSpec{
			Paused: true,
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()
	fakeClient := o.RolloutsClient.(*fakeroclient.Clientset)
	fakeClient.ReactionChain = nil
	fakeClient.AddReactor("patch", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if patchAction, ok := action.(kubetesting.PatchAction); ok {
			if string(patchAction.GetPatch()) == `{"spec":{"paused":false}}` {
				ro.Spec.Paused = false
			}
		}
		return true, &ro, nil
	})

	cmd := NewCmdResume(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook", "-n", "test"})
	err := cmd.Execute()
	assert.Nil(t, err)

	assert.False(t, ro.Spec.Paused)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, stdout, "rollout 'guestbook' resumed\n")
	assert.Empty(t, stderr)
}

func TestResumeCmdError(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions(&v1alpha1.Rollout{})
	defer tf.Cleanup()
	cmd := NewCmdResume(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"doesnotexist", "-n", "test"})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: rollouts.argoproj.io \"doesnotexist\" not found\n", stderr)
}
