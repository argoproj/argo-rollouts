package set

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubetesting "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	cliopts "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	options "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options/fake"
)

// getRollout helper to get the rollout using the dynamic interface
func getRollout(t *testing.T, o *cliopts.ArgoRolloutsOptions, namespace, name string) *v1alpha1.Rollout {
	t.Helper()
	un, err := o.DynamicClient.Resource(v1alpha1.RolloutGVR).Namespace(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.FailNow()
	}
	var ro v1alpha1.Rollout
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(un.Object, &ro)
	if err != nil {
		panic(err)
	}
	return &ro
}

func TestSetCmdUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdSet(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "Usage:")
	assert.Contains(t, stderr, "set COMMAND")
}

func TestSetImageCmdUsage(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()
	cmd := NewCmdSetImage(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	for _, args := range [][]string{
		{},
		{"guestbook"},
		{"guestbook", "forgot-equals-sign"},
		{"guestbook", "too=many=equals=signs"},
	} {
		cmd.SetArgs(args)
		err := cmd.Execute()
		assert.Error(t, err)
		stdout := o.Out.(*bytes.Buffer).String()
		stderr := o.ErrOut.(*bytes.Buffer).String()
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, "Usage:")
		assert.Contains(t, stderr, "image ROLLOUT")
	}
}

func TestSetImageCmd(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  "guestbook",
							Image: "argoproj/rollouts-demo:blue",
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "foo",
							Image: "alpine:3.8",
						},
						{
							Name:  "guestbook",
							Image: "argoproj/rollouts-demo:blue",
						},
						{
							Name:  "bar",
							Image: "alpine:3.8",
						},
					},
					EphemeralContainers: []corev1.EphemeralContainer{
						{
							EphemeralContainerCommon: corev1.EphemeralContainerCommon{
								Name:  "guestbook",
								Image: "argoproj/rollouts-demo:blue",
							},
						},
					},
				},
			},
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()

	cmd := NewCmdSetImage(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook", "guestbook=argoproj/rollouts-demo:NEWIMAGE"})
	err := cmd.Execute()
	assert.Nil(t, err)

	modifiedRo := getRollout(t, o, ro.Namespace, ro.Name)
	assert.NoError(t, err)
	assert.Equal(t, "argoproj/rollouts-demo:NEWIMAGE", modifiedRo.Spec.Template.Spec.Containers[1].Image)
	assert.Equal(t, "alpine:3.8", modifiedRo.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(t, "alpine:3.8", modifiedRo.Spec.Template.Spec.Containers[2].Image)
	assert.Equal(t, "argoproj/rollouts-demo:NEWIMAGE", modifiedRo.Spec.Template.Spec.InitContainers[0].Image)
	assert.Equal(t, "argoproj/rollouts-demo:NEWIMAGE", modifiedRo.Spec.Template.Spec.EphemeralContainers[0].Image)

	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, stdout, "rollout \"guestbook\" image updated\n")
	assert.Empty(t, stderr)
}

func TestSetImageCmdStar(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  "guestbook",
							Image: "argoproj/rollouts-demo:blue",
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "foo",
							Image: "alpine:3.8",
						},
						{
							Name:  "guestbook",
							Image: "argoproj/rollouts-demo:blue",
						},
						{
							Name:  "bar",
							Image: "alpine:3.8",
						},
					},
					EphemeralContainers: []corev1.EphemeralContainer{
						{
							EphemeralContainerCommon: corev1.EphemeralContainerCommon{
								Name:  "guestbook",
								Image: "argoproj/rollouts-demo:blue",
							},
						},
					},
				},
			},
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()

	cmd := NewCmdSetImage(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook", "*=argoproj/rollouts-demo:NEWIMAGE"})
	err := cmd.Execute()
	assert.Nil(t, err)

	modifiedRo := getRollout(t, o, ro.Namespace, ro.Name)
	assert.Equal(t, "argoproj/rollouts-demo:NEWIMAGE", modifiedRo.Spec.Template.Spec.Containers[1].Image)
	assert.Equal(t, "argoproj/rollouts-demo:NEWIMAGE", modifiedRo.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(t, "argoproj/rollouts-demo:NEWIMAGE", modifiedRo.Spec.Template.Spec.Containers[2].Image)
	assert.Equal(t, "argoproj/rollouts-demo:NEWIMAGE", modifiedRo.Spec.Template.Spec.InitContainers[0].Image)
	assert.Equal(t, "argoproj/rollouts-demo:NEWIMAGE", modifiedRo.Spec.Template.Spec.EphemeralContainers[0].Image)

	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, stdout, "rollout \"guestbook\" image updated\n")
	assert.Empty(t, stderr)
}
func TestSetImageCmdRolloutNotFound(t *testing.T) {
	tf, o := options.NewFakeArgoRolloutsOptions()
	defer tf.Cleanup()

	cmd := NewCmdSetImage(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"does-not-exist", "guestbook=argoproj/rollouts-demo:yellow"})
	err := cmd.Execute()
	assert.Error(t, err)

	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: rollouts.argoproj.io \"does-not-exist\" not found\n", stderr)
}

func TestSetImageCmdContainerNotFound(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "guestbook",
							Image: "argoproj/rollouts-demo:blue",
						},
					},
				},
			},
		},
	}
	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()

	cmd := NewCmdSetImage(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook", "typo=argoproj/rollouts-demo:yellow"})
	err := cmd.Execute()
	assert.Error(t, err)

	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Empty(t, stdout)
	assert.Equal(t, "Error: unable to find container named \"typo\"\n", stderr)
}

func TestSetImageConflict(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "foo",
							Image: "alpine:3.8",
						},
						{
							Name:  "guestbook",
							Image: "argoproj/rollouts-demo:blue",
						},
					},
				},
			},
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro)
	defer tf.Cleanup()

	updateCalls := 0
	fakeClient := o.DynamicClient.(*dynamicfake.FakeDynamicClient)
	fakeClient.PrependReactor("update", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if updateCalls > 0 {
			return true, &ro, nil
		}
		updateCalls++
		return true, nil, k8serr.NewConflict(schema.GroupResource{}, "guestbook", errors.New("intentional-error"))
	})

	cmd := NewCmdSetImage(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook", "guestbook=argoproj/rollouts-demo:yellow"})
	err := cmd.Execute()
	assert.Nil(t, err)

	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, stdout, "rollout \"guestbook\" image updated\n")
	assert.Empty(t, stderr)
	assert.True(t, updateCalls > 0)
}

func TestSetImageWorkloadRef(t *testing.T) {
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			WorkloadRef: &v1alpha1.ObjectRef{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "guestbook",
			},
		},
	}
	deploy := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guestbook",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  "guestbook",
							Image: "argoproj/rollouts-demo:blue",
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "foo",
							Image: "alpine:3.8",
						},
						{
							Name:  "guestbook",
							Image: "argoproj/rollouts-demo:blue",
						},
						{
							Name:  "bar",
							Image: "alpine:3.8",
						},
					},
					EphemeralContainers: []corev1.EphemeralContainer{
						{
							EphemeralContainerCommon: corev1.EphemeralContainerCommon{
								Name:  "guestbook",
								Image: "argoproj/rollouts-demo:blue",
							},
						},
					},
				},
			},
		},
	}

	tf, o := options.NewFakeArgoRolloutsOptions(&ro, &deploy)
	defer tf.Cleanup()

	cmd := NewCmdSetImage(o)
	cmd.PersistentPreRunE = o.PersistentPreRunE
	cmd.SetArgs([]string{"guestbook", "guestbook=argoproj/rollouts-demo:NEWIMAGE"})
	err := cmd.Execute()
	assert.NoError(t, err)

	newDeployUn, err := o.DynamicClientset().Resource(deploymentGVR).Namespace(ro.Namespace).Get(context.Background(), "guestbook", metav1.GetOptions{})
	assert.NoError(t, err)
	var newDeploy appsv1.Deployment
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(newDeployUn.Object, &newDeploy)
	assert.NoError(t, err)
	assert.Equal(t, "argoproj/rollouts-demo:NEWIMAGE", newDeploy.Spec.Template.Spec.Containers[1].Image)
	assert.Equal(t, "alpine:3.8", newDeploy.Spec.Template.Spec.Containers[0].Image)
	assert.Equal(t, "alpine:3.8", newDeploy.Spec.Template.Spec.Containers[2].Image)
	assert.Equal(t, "argoproj/rollouts-demo:NEWIMAGE", newDeploy.Spec.Template.Spec.InitContainers[0].Image)
	assert.Equal(t, "argoproj/rollouts-demo:NEWIMAGE", newDeploy.Spec.Template.Spec.EphemeralContainers[0].Image)

	stdout := o.Out.(*bytes.Buffer).String()
	stderr := o.ErrOut.(*bytes.Buffer).String()
	assert.Equal(t, stdout, "deployment \"guestbook\" image updated\n")
	assert.Empty(t, stderr)
}
