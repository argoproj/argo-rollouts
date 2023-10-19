package promote

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	fakeroclient "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	options "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
)

func TestPromoteCmdUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdPromote(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "Usage:")
	assert.Contains(t, stderr, "promote ROLLOUT")
}

func TestPromoteUseBothSkipFlagError(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdPromote(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook", "--skip-current-step", "--skip-all-steps"})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, useBothSkipFlagsError)
}
func TestPromoteSkipFlagOnBlueGreenError(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{},
			},
		},
	}
	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()
	cmd := NewCmdPromote(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook", "-a"})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, skipFlagsWithBlueGreenError)
}
func TestPromoteSkipFlagOnNoStepCanaryError(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{},
			},
		},
	}
	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()
	cmd := NewCmdPromote(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook", "-c"})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, skipFlagWithNoStepCanaryError)
}

func TestPromoteNoStepCanary(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{},
			},
		},
	}
	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()
	cmd := NewCmdPromote(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook"})
	err := cmd.Execute()
	assert.NoError(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	assert.NotEmpty(t, stdout)
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stderr)
}

func TestPromoteCmdSuccesSkipAllSteps(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					Steps: []v1alpha1.CanaryStep{
						{
							SetWeight: pointer.Int32Ptr(1),
						},
						{
							SetWeight: pointer.Int32Ptr(2),
						},
					},
				},
			},
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()
	fakeClient := o.RolloutsClient.(*fakeroclient.Clientset)
	fakeClient.PrependReactor("patch", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if patchAction, ok := action.(kubetesting.PatchAction); ok {
			patchRo := v1alpha1.Rollout{}
			err := json.Unmarshal(patchAction.GetPatch(), &patchRo)
			if err != nil {
				panic(err)
			}
			ro.Status.CurrentStepIndex = patchRo.Status.CurrentStepIndex
		}
		return true, &ro, nil
	})

	cmd := NewCmdPromote(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook", "-a"})

	err := cmd.Execute()
	assert.Nil(t, err)
	assert.Equal(t, int32(2), *ro.Status.CurrentStepIndex)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, stdout, "rollout 'guestbook' promoted\n")
	assert.Empty(t, stderr)
}

func TestPromoteCmdSuccesFirstStepWithSkipFirstStep(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					Steps: []v1alpha1.CanaryStep{
						{
							SetWeight: pointer.Int32Ptr(1),
						},
						{
							SetWeight: pointer.Int32Ptr(2),
						},
					},
				},
			},
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()
	fakeClient := o.RolloutsClient.(*fakeroclient.Clientset)
	fakeClient.PrependReactor("patch", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if patchAction, ok := action.(kubetesting.PatchAction); ok {
			patchRo := v1alpha1.Rollout{}
			err := json.Unmarshal(patchAction.GetPatch(), &patchRo)
			if err != nil {
				panic(err)
			}
			ro.Status.CurrentStepIndex = patchRo.Status.CurrentStepIndex
		}
		return true, &ro, nil
	})

	cmd := NewCmdPromote(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook", "-c"})

	err := cmd.Execute()
	assert.Nil(t, err)
	assert.Equal(t, int32(1), *ro.Status.CurrentStepIndex)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, stdout, "rollout 'guestbook' promoted\n")
	assert.Empty(t, stderr)
}

func TestPromoteCmdSuccesFirstStep(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					Steps: []v1alpha1.CanaryStep{
						{
							SetWeight: pointer.Int32Ptr(1),
						},
						{
							SetWeight: pointer.Int32Ptr(2),
						},
					},
				},
			},
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()
	fakeClient := o.RolloutsClient.(*fakeroclient.Clientset)
	fakeClient.PrependReactor("patch", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if patchAction, ok := action.(kubetesting.PatchAction); ok {
			patchRo := v1alpha1.Rollout{}
			err := json.Unmarshal(patchAction.GetPatch(), &patchRo)
			if err != nil {
				panic(err)
			}
			ro.Status.CurrentStepIndex = patchRo.Status.CurrentStepIndex
		}
		return true, &ro, nil
	})

	cmd := NewCmdPromote(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook"})

	err := cmd.Execute()
	assert.Nil(t, err)
	assert.Equal(t, int32(1), *ro.Status.CurrentStepIndex)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, stdout, "rollout 'guestbook' promoted\n")
	assert.Empty(t, stderr)
}

func TestPromoteCmdSuccessDoNotGoPastLastStep(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					Steps: []v1alpha1.CanaryStep{
						{
							SetWeight: pointer.Int32Ptr(1),
						},
						{
							SetWeight: pointer.Int32Ptr(2),
						},
					},
				},
			},
		},
		Status: v1alpha1.RolloutStatus{
			CurrentStepIndex: pointer.Int32Ptr(2),
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()
	fakeClient := o.RolloutsClient.(*fakeroclient.Clientset)
	fakeClient.PrependReactor("patch", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if patchAction, ok := action.(kubetesting.PatchAction); ok {
			patchRo := v1alpha1.Rollout{}
			err := json.Unmarshal(patchAction.GetPatch(), &patchRo)
			if err != nil {
				panic(err)
			}
			ro.Status.CurrentStepIndex = patchRo.Status.CurrentStepIndex
		}
		return true, &ro, nil
	})

	cmd := NewCmdPromote(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook"})

	err := cmd.Execute()
	assert.Nil(t, err)
	assert.Equal(t, int32(2), *ro.Status.CurrentStepIndex)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, stdout, "rollout 'guestbook' promoted\n")
	assert.Empty(t, stderr)
}

func TestPromoteCmdSuccessUnpause(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Paused: true,
		},
		Status: v1alpha1.RolloutStatus{
			PauseConditions: []v1alpha1.PauseCondition{{
				Reason: v1alpha1.PauseReasonCanaryPauseStep,
			}},
			ControllerPause: true,
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()
	fakeClient := o.RolloutsClient.(*fakeroclient.Clientset)
	fakeClient.PrependReactor("patch", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if patchAction, ok := action.(kubetesting.PatchAction); ok {
			if string(patchAction.GetPatch()) == unpausePatch {
				ro.Status.PauseConditions = nil
				ro.Spec.Paused = false
			}
		}
		return true, &ro, nil
	})

	cmd := NewCmdPromote(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook"})
	err := cmd.Execute()
	assert.Nil(t, err)

	assert.Nil(t, ro.Status.PauseConditions)
	assert.False(t, ro.Spec.Paused)
	assert.True(t, ro.Status.ControllerPause)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, stdout, "rollout 'guestbook' promoted\n")
	assert.Empty(t, stderr)
}

func TestPromoteCmdPatchError(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Paused: true,
		},
		Status: v1alpha1.RolloutStatus{
			PauseConditions: []v1alpha1.PauseCondition{{
				Reason: v1alpha1.PauseReasonCanaryPauseStep,
			}},
			ControllerPause: true,
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()
	cmd := NewCmdPromote(o)
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

func TestPromoteCmdNotFoundError(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions(&v1alpha1.Rollout{})
	defer tf.Cleanup()
	cmd := NewCmdPromote(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"doesnotexist"})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: rollouts.argoproj.io \"doesnotexist\" not found\n", stderr)
}

func TestPromoteCmdFull(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{},
		Status: v1alpha1.RolloutStatus{
			StableRS:       "abc123",
			CurrentPodHash: "def456",
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()
	fakeClient := o.RolloutsClient.(*fakeroclient.Clientset)
	fakeClient.PrependReactor("patch", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if patchAction, ok := action.(kubetesting.PatchAction); ok {
			if string(patchAction.GetPatch()) == promoteFullPatch {
				ro.Status.PromoteFull = true
			}
		}
		return true, &ro, nil
	})

	cmd := NewCmdPromote(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook", "--full"})
	err := cmd.Execute()
	assert.Nil(t, err)

	assert.True(t, ro.Status.PromoteFull)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, stdout, "rollout 'guestbook' fully promoted\n")
	assert.Empty(t, stderr)
}

func TestPromoteCmdAlreadyFullyPromoted(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{},
		Status: v1alpha1.RolloutStatus{
			StableRS:       "abc123",
			CurrentPodHash: "abc123",
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()
	fakeClient := o.RolloutsClient.(*fakeroclient.Clientset)
	fakeClient.PrependReactor("patch", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if _, ok := action.(kubetesting.PatchAction); ok {
			// should not be called if we are already fully promoted
			t.FailNow()
		}
		return true, &ro, nil
	})

	cmd := NewCmdPromote(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook", "--full"})
	err := cmd.Execute()
	assert.Nil(t, err)

	assert.False(t, ro.Status.PromoteFull)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, stdout, "rollout 'guestbook' fully promoted\n")
	assert.Empty(t, stderr)
}

func TestPromoteInconclusiveStep(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Paused: true,
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					Steps: []v1alpha1.CanaryStep{
						{
							SetWeight: pointer.Int32Ptr(1),
						},
						{
							SetWeight: pointer.Int32Ptr(2),
						},
					},
				},
			},
		},
		Status: v1alpha1.RolloutStatus{
			PauseConditions: []v1alpha1.PauseCondition{{
				Reason: v1alpha1.PauseReasonCanaryPauseStep,
			}},
			ControllerPause: true,
			Canary: v1alpha1.CanaryStatus{
				CurrentStepAnalysisRunStatus: &v1alpha1.RolloutAnalysisRunStatus{
					Status: v1alpha1.AnalysisPhaseInconclusive,
				},
			},
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()
	fakeClient := o.RolloutsClient.(*fakeroclient.Clientset)
	fakeClient.PrependReactor("patch", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if patchAction, ok := action.(kubetesting.PatchAction); ok {
			patchRo := v1alpha1.Rollout{}
			err := json.Unmarshal(patchAction.GetPatch(), &patchRo)
			if err != nil {
				panic(err)
			}
			ro.Status.CurrentStepIndex = patchRo.Status.CurrentStepIndex
			ro.Status.ControllerPause = patchRo.Status.ControllerPause
			ro.Status.PauseConditions = patchRo.Status.PauseConditions
		}
		return true, &ro, nil
	})

	cmd := NewCmdPromote(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook"})

	err := cmd.Execute()
	assert.Nil(t, err)
	assert.Equal(t, false, ro.Status.ControllerPause)
	assert.Empty(t, ro.Status.PauseConditions)

	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, stdout, "rollout 'guestbook' promoted\n")
	assert.Empty(t, stderr)
}
