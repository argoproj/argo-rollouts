package viewcontroller

import (
	"context"
	"testing"
	"time"

	rolloutsfake "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	v1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func newFakeController(namespace string, name string, objects ...runtime.Object) *RolloutViewController {
	rolloutsClientset := rolloutsfake.NewSimpleClientset(objects...)
	kubeClientset := k8sfake.NewSimpleClientset()
	return NewController(namespace, name, kubeClientset, rolloutsClientset)
}

func TestController(t *testing.T) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "test",
		},
	}
	c := newFakeController(ro.Namespace, ro.Name, ro)
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	c.Start(ctx)
	cancel()
	roInfo, err := c.GetRolloutInfo()
	assert.NoError(t, err)
	assert.Equal(t, roInfo.Name, "foo")
}

func TestControllerCallback(t *testing.T) {
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

	c := newFakeController(ro.Namespace, ro.Name, ro)
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
