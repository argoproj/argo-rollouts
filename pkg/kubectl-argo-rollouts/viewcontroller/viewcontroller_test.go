package viewcontroller

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/workqueue"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	rolloutsfake "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"

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

type observedWorkQueue struct {
	workqueue.RateLimitingInterface
	getStarted chan struct{}
	getStopped chan struct{}
}

func (q *observedWorkQueue) Get() (any, bool) {
	close(q.getStarted)
	defer close(q.getStopped)
	return q.RateLimitingInterface.Get()
}

func TestViewControllerRunStopsIdleWorker(t *testing.T) {
	for _, kind := range []string{"rollout", "experiment"} {
		t.Run(kind, func(t *testing.T) {
			for i := 0; i < 10; i++ {
				var c *viewController
				if kind == "rollout" {
					c = newFakeRolloutController("test", "foo").viewController
				} else {
					c = newFakeExperimentController("test", "foo").viewController
				}
				q := &observedWorkQueue{
					RateLimitingInterface: c.workqueue,
					getStarted:            make(chan struct{}),
					getStopped:            make(chan struct{}),
				}
				t.Cleanup(q.ShutDown)
				c.workqueue = q
				c.callbacks = []func(any){func(any) {}}
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				done := make(chan error, 1)
				go func() { done <- c.Run(ctx) }()

				select {
				case <-q.getStarted:
				case <-time.After(time.Second):
					t.Fatal("worker did not start")
				}
				cancel()
				select {
				case err := <-done:
					require.NoError(t, err)
				case <-time.After(time.Second):
					t.Fatal("Run did not stop after cancellation")
				}
				assert.True(t, q.ShuttingDown())
				assert.Empty(t, c.callbacks)
				select {
				case <-q.getStopped:
				default:
					t.Fatal("Run returned before the worker exited Get")
				}
			}
		})
	}
}

func TestViewControllerRunWaitsForCallback(t *testing.T) {
	c := newFakeRolloutController("test", "foo")
	t.Cleanup(c.workqueue.ShutDown)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callbackStarted := make(chan struct{})
	callbackStopped := make(chan struct{})
	releaseCallback := make(chan struct{})
	defer close(releaseCallback)
	c.getObj = func() (any, error) { return &rollout.RolloutInfo{}, nil }
	c.RegisterCallback(func(*rollout.RolloutInfo) {
		close(callbackStarted)
		<-releaseCallback
		close(callbackStopped)
	})
	c.workqueue.Add("foo")
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	select {
	case <-callbackStarted:
	case <-time.After(time.Second):
		t.Fatal("callback did not start")
	}
	cancel()
	require.Eventually(t, c.workqueue.ShuttingDown, time.Second, time.Millisecond)
	select {
	case <-done:
		t.Fatal("Run returned while a callback was still running")
	default:
	}
	releaseCallback <- struct{}{}
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after the callback returned")
	}
	select {
	case <-callbackStopped:
	default:
		t.Fatal("Run returned before the callback finished")
	}
	assert.True(t, c.workqueue.ShuttingDown())
}

func TestViewControllerRunWithCancelledContext(t *testing.T) {
	c := newFakeRolloutController("test", "foo")
	t.Cleanup(c.workqueue.ShutDown)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run did not stop with an already-cancelled context")
	}
	assert.True(t, c.workqueue.ShuttingDown())
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
