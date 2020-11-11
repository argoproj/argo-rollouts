package viewcontroller

import (
	"context"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	v1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func newFakeRolloutController(namespace string, name string, objects ...runtime.Object) *RolloutViewController {
	dynamicClientset := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), objects...)
	kubeClientset := k8sfake.NewSimpleClientset()
	return NewRolloutViewController(namespace, name, kubeClientset, dynamicClientset)
}

func newFakeExperimentController(namespace string, name string, objects ...runtime.Object) *ExperimentViewController {
	dynamicClientset := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), objects...)
	kubeClientset := k8sfake.NewSimpleClientset()
	return NewExperimentViewController(namespace, name, kubeClientset, dynamicClientset)
}

func TestRolloutController(t *testing.T) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "test",
		},
	}
	c := newFakeRolloutController(ro.Namespace, ro.Name, ro)
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	c.Start(ctx)
	cancel()
	roInfo, err := c.GetRolloutInfo()
	assert.NoError(t, err)
	assert.Equal(t, roInfo.Name, "foo")
}

func TestRolloutControllerCallback(t *testing.T) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "test",
		},
	}

	callbackCalled := false
	cb := func(roInfo *info.RolloutInfo) {
		callbackCalled = true
		assert.Equal(t, roInfo.Name, "foo")
	}

	c := newFakeRolloutController(ro.Namespace, ro.Name, ro)
	c.RegisterCallback(cb)
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c.Start(ctx)
	go c.Run(ctx)
	time.Sleep(time.Second)
	for i := 0; i < 100; i++ {
		if callbackCalled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.True(t, callbackCalled)
}

func TestExperimentController(t *testing.T) {
	ro := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "test",
		},
	}
	c := newFakeExperimentController(ro.Namespace, ro.Name, ro)
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	c.Start(ctx)
	cancel()
	roInfo, err := c.GetExperimentInfo()
	assert.NoError(t, err)
	assert.Equal(t, roInfo.Name, "foo")
}

func TestExperimentControllerCallback(t *testing.T) {
	ro := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "test",
		},
	}

	callbackCalled := false
	cb := func(expInfo *info.ExperimentInfo) {
		callbackCalled = true
		assert.Equal(t, expInfo.Name, "foo")
	}

	c := newFakeExperimentController(ro.Namespace, ro.Name, ro)
	c.RegisterCallback(cb)
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c.Start(ctx)
	go c.Run(ctx)
	time.Sleep(time.Second)
	for i := 0; i < 100; i++ {
		if callbackCalled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.True(t, callbackCalled)
}
