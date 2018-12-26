package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/kubernetes/pkg/controller"
)

func newService(name string, port int, selector map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Ports: []corev1.ServicePort{{
				Protocol:   "TCP",
				Port:       int32(port),
				TargetPort: intstr.FromInt(port),
			}},
		},
	}
}

func TestGetRolloutsForService(t *testing.T) {
	f := newFixture(t)

	s := newService("foo", 80, nil)
	ro1 := newRollout("bar", 0, nil, nil, "", "")
	ro1.Spec.Strategy.BlueGreenStrategy.ActiveService = "foo"
	ro2 := newRollout("baz", 0, nil, nil, "", "")
	ro2.Spec.Strategy.BlueGreenStrategy.ActiveService = "foo2"
	f.rolloutLister = append(f.rolloutLister, ro1, ro2)
	f.objects = append(f.objects, ro1, ro2)

	// Create the fixture but don't start it,
	// so nothing happens in the background.
	c, _, _ := f.newController()

	rollouts, err := c.getRolloutsForService(s)
	assert.Nil(t, err)

	assert.Len(t, rollouts, 1)
	assert.Equal(t, ro1, rollouts[0])
}

func TestHandleServiceEnqueueRollout(t *testing.T) {
	f := newFixture(t)

	s := newService("foo", 80, nil)
	ro1 := newRollout("bar", 0, nil, nil, "", "")
	ro1.Spec.Strategy.BlueGreenStrategy.ActiveService = "foo"
	ro2 := newRollout("baz", 0, nil, nil, "", "")
	ro2.Spec.Strategy.BlueGreenStrategy.ActiveService = "foo2"
	f.objects = append(f.objects, ro1, ro2)

	// Create the fixture but don't start it,
	// so nothing happens in the background.
	c, _, _ := f.newController()

	c.handleService(s)
	assert.Equal(t, c.workqueue.Len(), 1)

	key, done := c.workqueue.Get()
	assert.NotNil(t, key)
	assert.False(t, done)
	expectedKey, _ := controller.KeyFunc(ro1)
	assert.Equal(t, key.(string), expectedKey)
}

func TestHandleServiceNoAdditions(t *testing.T) {
	f := newFixture(t)

	s := newService("foo", 80, nil)
	ro1 := newRollout("bar", 0, nil, nil, "", "")
	ro1.Spec.Strategy.BlueGreenStrategy.ActiveService = "notFoo"
	f.objects = append(f.objects, ro1)

	// Create the fixture but don't start it,
	// so nothing happens in the background.
	c, _, _ := f.newController()

	c.handleService(s)
	assert.Equal(t, c.workqueue.Len(), 0)
}

func TestHandleServiceNoExistingRollouts(t *testing.T) {
	f := newFixture(t)

	s := newService("foo", 80, nil)
	// Create the fixture but don't start it,
	// so nothing happens in the background.
	c, _, _ := f.newController()

	c.handleService(s)
	assert.Equal(t, c.workqueue.Len(), 0)
}

func TestUpdateServiceEnqueueRollout(t *testing.T) {
	f := newFixture(t)

	oldSvc := newService("foo", 80, nil)
	newSvc := oldSvc.DeepCopy()
	newSvc.ResourceVersion = "2"
	ro1 := newRollout("bar", 0, nil, nil, "", "")
	ro1.Spec.Strategy.BlueGreenStrategy.ActiveService = "foo"
	f.objects = append(f.objects, ro1)
	// Create the fixture but don't start it,
	// so nothing happens in the background.
	c, _, _ := f.newController()

	c.updateService(oldSvc, newSvc)
	assert.Equal(t, c.workqueue.Len(), 1)

	key, done := c.workqueue.Get()
	assert.NotNil(t, key)
	assert.False(t, done)
	expectedKey, _ := controller.KeyFunc(ro1)
	assert.Equal(t, key.(string), expectedKey)
}

func TestUpdateServiceSameService(t *testing.T) {
	f := newFixture(t)

	s := newService("foo", 80, nil)

	// Create the fixture but don't start it,
	// so nothing happens in the background.
	c, _, _ := f.newController()

	c.updateService(s, s)
	assert.Equal(t, c.workqueue.Len(), 0)
}
