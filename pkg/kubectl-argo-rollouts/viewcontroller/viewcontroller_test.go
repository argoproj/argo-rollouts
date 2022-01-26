package viewcontroller

import (
	"context"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	rolloutsfake "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	v1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func newFakeRolloutController(namespace string, name string, objects ...runtime.Object) *RolloutViewController {

	var rolloutObjs []runtime.Object
	var kubeObjs []runtime.Object

	for _, o := range objects {
		switch typedO := o.(type) {
		case *v1alpha1.Rollout:
			typedO.TypeMeta = metav1.TypeMeta{
				Kind:       "Rollout",
				APIVersion: "argoproj.io/v1alpha1",
			}
			rolloutObjs = append(rolloutObjs, o)
		default:
			kubeObjs = append(kubeObjs, o)
		}
	}

	rolloutsClientset := rolloutsfake.NewSimpleClientset(rolloutObjs...)
	kubeClientset := k8sfake.NewSimpleClientset(kubeObjs...)
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
			UID:       "a17d1089-fae6-11e9-a15b-42010aa80033",
		},
		Spec: v1alpha1.RolloutSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "foo",
				},
			},
		},
	}

	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rs1",
			Namespace: "test",
			Labels: map[string]string{
				"app": "foo",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					UID: "a17d1089-fae6-11e9-a15b-42010aa80033",
				},
			},
		},
	}

	c := newFakeRolloutController(ro.Namespace, ro.Name, ro, rs)
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	c.Start(ctx)
	cancel()
	roInfo, err := c.GetRolloutInfo()
	assert.NoError(t, err)
	assert.Equal(t, roInfo.ObjectMeta.Name, "foo")
	assert.Equal(t, 1, len(roInfo.GetReplicaSets()))
}

func TestRolloutControllerCallback(t *testing.T) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "test",
		},
	}

	callbackCalled := false
	cb := func(roInfo *rollout.RolloutInfo) {
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
	assert.Equal(t, roInfo.ObjectMeta.Name, "foo")
}

func TestExperimentControllerCallback(t *testing.T) {
	ro := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "test",
		},
	}

	callbackCalled := false
	cb := func(expInfo *rollout.ExperimentInfo) {
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
		if callbackCalled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.True(t, callbackCalled)
}
