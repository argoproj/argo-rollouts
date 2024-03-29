package viewcontroller

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	rolloutsfake "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	v1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func newFakeRolloutController(namespace string, name string, objects ...runtime.Object) *RolloutViewController {
	rolloutsClientset := rolloutsfake.NewSimpleClientset(objects...)
	kubeClientset := k8sfake.NewSimpleClientset()
	return NewRolloutViewController(namespace, name, kubeClientset, rolloutsClientset)
}

func newFakeExperimentController(namespace string, name string, objects ...runtime.Object) *ExperimentViewController {
	rolloutsClientset := rolloutsfake.NewSimpleClientset(objects...)
	kubeClientset := k8sfake.NewSimpleClientset()
	return NewExperimentViewController(namespace, name, kubeClientset, rolloutsClientset)
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
	assert.Equal(t, roInfo.ObjectMeta.Name, "foo")
}

func TestRolloutControllerCallback(t *testing.T) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "test",
		},
	}

	callbackCalled := false
	var callbackCalledLock sync.Mutex // acquire before accessing callbackCalled

	cb := func(roInfo *rollout.RolloutInfo) {
		callbackCalledLock.Lock()
		defer callbackCalledLock.Unlock()
		callbackCalled = true
		assert.Equal(t, roInfo.ObjectMeta.Name, "foo")
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
		callbackCalledLock.Lock()
		isCallbackCalled := callbackCalled
		callbackCalledLock.Unlock()
		if isCallbackCalled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	callbackCalledLock.Lock()
	defer callbackCalledLock.Unlock()
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
	assert.Equal(t, roInfo.ObjectMeta.Name, "foo")
}

func TestExperimentControllerCallback(t *testing.T) {
	ro := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "test",
		},
	}

	var callbackCalledLock sync.Mutex // acquire before accessing callbackCalled
	callbackCalled := false
	cb := func(expInfo *rollout.ExperimentInfo) {
		callbackCalledLock.Lock()
		defer callbackCalledLock.Unlock()
		callbackCalled = true
		assert.Equal(t, expInfo.ObjectMeta.Name, "foo")
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
		callbackCalledLock.Lock()
		isCallbackCalled := callbackCalled
		callbackCalledLock.Unlock()
		if isCallbackCalled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	callbackCalledLock.Lock()
	defer callbackCalledLock.Unlock()
	assert.True(t, callbackCalled)
}
