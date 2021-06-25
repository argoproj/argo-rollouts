package undo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info/testdata"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubetesting "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	options "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
)

func TestUndoCmdUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdUndo(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "Usage:")
	assert.Contains(t, stderr, "undo ROLLOUT")
}

func TestUndoCmd(t *testing.T) {
	rolloutObjs := testdata.NewCanaryRollout()
	ro := rolloutObjs.Rollouts[0]
	tf, o := options.NewFakeArgoRolloutsOptions(rolloutObjs.AllObjects()...)
	o.RESTClientGetter = tf.WithNamespace(ro.Namespace)
	defer tf.Cleanup()
	fakeClient := o.DynamicClient.(*dynamicfake.FakeDynamicClient)
	fakeClient.PrependReactor("patch", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if patchAction, ok := action.(kubetesting.PatchAction); ok {
			type patch struct {
				Value corev1.PodTemplateSpec `json:"value"`
			}
			patchRo := []patch{}
			err := json.Unmarshal(patchAction.GetPatch(), &patchRo)
			if err != nil {
				panic(err)
			}
			ro.Spec.Template = patchRo[0].Value
		}
		return true, ro, nil
	})

	cmd := NewCmdUndo(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{ro.Name})

	err := cmd.Execute()
	assert.Nil(t, err)
	var rs *v1.ReplicaSet
	for _, rs = range rolloutObjs.ReplicaSets {
		if rs.Name == "canary-demo-877894d5b" {
			break
		}
	}
	delete(rs.Spec.Template.Labels, v1alpha1.DefaultRolloutUniqueLabelKey)
	assert.Equal(t, rs.Spec.Template, ro.Spec.Template)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, fmt.Sprintf("rollout '%s' undo\n", ro.Name), stdout)
	assert.Empty(t, stderr)
}

func TestUndoCmdToRevision(t *testing.T) {
	rolloutObjs := testdata.NewCanaryRollout()
	ro := rolloutObjs.Rollouts[0]
	tf, o := options.NewFakeArgoRolloutsOptions(rolloutObjs.AllObjects()...)
	o.RESTClientGetter = tf.WithNamespace(ro.Namespace)
	defer tf.Cleanup()
	fakeClient := o.DynamicClient.(*dynamicfake.FakeDynamicClient)
	fakeClient.PrependReactor("patch", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if patchAction, ok := action.(kubetesting.PatchAction); ok {
			type patch struct {
				Value corev1.PodTemplateSpec `json:"value"`
			}
			patchRo := []patch{}
			err := json.Unmarshal(patchAction.GetPatch(), &patchRo)
			if err != nil {
				panic(err)
			}
			ro.Spec.Template = patchRo[0].Value
		}
		return true, ro, nil
	})

	cmd := NewCmdUndo(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{ro.Name, "--to-revision=29"})

	err := cmd.Execute()
	assert.Nil(t, err)
	var rs *v1.ReplicaSet
	for _, rs = range rolloutObjs.ReplicaSets {
		if rs.Name == "canary-demo-859c99b45c" {
			break
		}
	}
	delete(rs.Spec.Template.Labels, v1alpha1.DefaultRolloutUniqueLabelKey)
	assert.Equal(t, rs.Spec.Template, ro.Spec.Template)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, fmt.Sprintf("rollout '%s' undo\n", ro.Name), stdout)
	assert.Empty(t, stderr)
}

func TestUndoCmdToRevisionOfWorkloadRef(t *testing.T) {

	roTests := []struct {
		idx     int
		refName string
		refType string
	}{
		{1, "canary-demo-65fb5ffc84", "ReplicaSet"},
		{2, "canary-demo-deploy", "Deployment"},
	}

	for _, roTest := range roTests {
		rolloutObjs := testdata.NewCanaryRollout()
		ro := rolloutObjs.Rollouts[roTest.idx]
		ro.Spec.Template = corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
		}
		tf, o := options.NewFakeArgoRolloutsOptions(rolloutObjs.AllObjects()...)
		o.RESTClientGetter = tf.WithNamespace(ro.Namespace)
		defer tf.Cleanup()
		cmd := NewCmdUndo(o)
		cmd.PersistentPreRunE = o.PersistentPreRunE
		cmd.SetArgs([]string{ro.Name, "--to-revision=29"})

		err := cmd.Execute()
		assert.Nil(t, err)

		// Verify the current RS has been patched by the oldRS's template
		switch roTest.refType {
		case "Deployment":
			currentRs, _ := o.KubeClient.AppsV1().Deployments(ro.Namespace).Get(context.TODO(), "canary-demo-deploy", metav1.GetOptions{})
			assert.Equal(t, "argoproj/rollouts-demo:asdf", currentRs.Spec.Template.Spec.Containers[0].Image)
		case "ReplicaSet":
			currentRs, _ := o.KubeClient.AppsV1().ReplicaSets(ro.Namespace).Get(context.TODO(), "canary-demo-65fb5ffc84", metav1.GetOptions{})
			assert.Equal(t, "argoproj/rollouts-demo:asdf", currentRs.Spec.Template.Spec.Containers[0].Image)
		}
		stdout := o.Out.(*bytes.Buffer).String()
		stderr := o.ErrOut.(*bytes.Buffer).String()
		assert.Equal(t, fmt.Sprintf("rollout '%s' undo\n", ro.Name), stdout)
		assert.Empty(t, stderr)
	}
}

func TestUndoCmdToRevisionSkipCurrent(t *testing.T) {
	rolloutObjs := testdata.NewCanaryRollout()
	ro := rolloutObjs.Rollouts[0]
	tf, o := options.NewFakeArgoRolloutsOptions(rolloutObjs.AllObjects()...)
	o.RESTClientGetter = tf.WithNamespace(ro.Namespace)
	defer tf.Cleanup()

	cmd := NewCmdUndo(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	revision := "31"
	cmd.SetArgs([]string{ro.Name, "--to-revision=" + revision})

	err := cmd.Execute()
	assert.Nil(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, fmt.Sprintf("skipped rollback (current template already matches revision %s)", revision), stdout)
	assert.Empty(t, stderr)
}

func TestUndoCmdToRevisionNotFoundError(t *testing.T) {
	rolloutObjs := testdata.NewCanaryRollout()
	ro := rolloutObjs.Rollouts[0]
	tf, o := options.NewFakeArgoRolloutsOptions(rolloutObjs.AllObjects()...)
	o.RESTClientGetter = tf.WithNamespace(ro.Namespace)
	defer tf.Cleanup()

	cmd := NewCmdUndo(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	revision := "1"
	cmd.SetArgs([]string{ro.Name, "--to-revision=" + revision})

	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, fmt.Sprintf("Error: unable to find specified revision %v in history\n", revision), stderr)
}

func TestUndoCmdNoRevisionError(t *testing.T) {
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

	cmd := NewCmdUndo(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{ro.Name})

	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, fmt.Sprintf("Error: no revision found for rollout %q\n", ro.Name), stderr)
}

func TestUndoCmdError(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions(&v1alpha1.Rollout{})
	defer tf.Cleanup()
	cmd := NewCmdUndo(o)
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
