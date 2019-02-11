package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
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

func TestReconcilePreviewService(t *testing.T) {
	tests := []struct {
		name                   string
		newRSDesiredReplicas   int
		newRSAvailableReplicas int
		activeSvc              *corev1.Service
		previewSvc             *corev1.Service
		expectedResult         bool
	}{
		{
			name:                   "Do not switch if the new RS isn't ready",
			activeSvc:              newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "test"}),
			previewSvc:             newService("preview", 80, nil),
			newRSDesiredReplicas:   5,
			newRSAvailableReplicas: 3,
			expectedResult:         true,
		},
		{
			name:                   "Continue if active service is already set to the newRS",
			activeSvc:              newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "57b9899597"}),
			newRSDesiredReplicas:   5,
			newRSAvailableReplicas: 5,
			expectedResult:         false,
		},
		{
			name:                   "Continue if active service doesn't have a selector",
			activeSvc:              newService("active", 80, map[string]string{}),
			newRSDesiredReplicas:   5,
			newRSAvailableReplicas: 5,
			expectedResult:         false,
		},
		{
			name:                   "Continue if active service selector doesn't match have DefaultRolloutUniqueLabelKey",
			activeSvc:              newService("active", 80, map[string]string{}),
			newRSDesiredReplicas:   5,
			newRSAvailableReplicas: 5,
			expectedResult:         false,
		},
		{
			name:                   "Continue if preview service is already set to the newRS",
			activeSvc:              newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "test"}),
			previewSvc:             newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "57b9899597"}),
			newRSDesiredReplicas:   5,
			newRSAvailableReplicas: 5,
			expectedResult:         false,
		},
		{
			name:                   "Switch if the new RS is ready",
			activeSvc:              newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "test"}),
			previewSvc:             newService("preview", 80, nil),
			newRSDesiredReplicas:   5,
			newRSAvailableReplicas: 5,
			expectedResult:         true,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			ro := newRollout("foo", 5, nil, nil)
			rs := newReplicaSetWithStatus(ro, "bar", test.newRSDesiredReplicas, test.newRSAvailableReplicas)
			f := newFixture(t)

			f.rolloutLister = append(f.rolloutLister, ro)
			f.objects = append(f.objects, ro)
			f.kubeobjects = append(f.kubeobjects, rs)
			if test.previewSvc != nil {
				f.kubeobjects = append(f.kubeobjects, test.previewSvc)
			}
			c, _, _ := f.newController()
			result, err := c.reconcilePreviewService(ro, rs, test.previewSvc, test.activeSvc)
			assert.NoError(t, err)
			assert.Equal(t, test.expectedResult, result)
		})
	}
}

func TestReconcileActiveService(t *testing.T) {
	tests := []struct {
		name           string
		activeSvc      *corev1.Service
		previewSvc     *corev1.Service
		expectedResult bool
	}{
		{
			name:           "Switch active service to New RS",
			activeSvc:      newService("active", 80, nil),
			expectedResult: true,
		},
		{
			name:           "Switch Preview selector to empty string",
			activeSvc:      newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "57b9899597"}),
			previewSvc:     newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "57b9899597"}),
			expectedResult: true,
		},
		{
			name:           "No switch required if the active service already points at new RS and the preview is not point at any RS",
			activeSvc:      newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "57b9899597"}),
			previewSvc:     newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: ""}),
			expectedResult: false,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			ro := newRollout("foo", 5, nil, nil)
			rs := newReplicaSetWithStatus(ro, "bar", 5, 5)
			f := newFixture(t)

			f.rolloutLister = append(f.rolloutLister, ro)
			f.objects = append(f.objects, ro)
			f.kubeobjects = append(f.kubeobjects, rs)
			if test.previewSvc != nil {
				f.kubeobjects = append(f.kubeobjects, test.previewSvc)
			}
			c, _, _ := f.newController()
			result, err := c.reconcileActiveService(ro, rs, test.previewSvc, test.activeSvc)
			assert.NoError(t, err)
			assert.Equal(t, test.expectedResult, result)
		})
	}
}

func TestGetRolloutsForService(t *testing.T) {
	f := newFixture(t)

	s := newService("foo", 80, nil)
	ro1 := newBlueGreenRollout("bar", 0, nil, nil, "active", "")
	ro1.Spec.Strategy.BlueGreenStrategy.ActiveService = "foo"
	ro2 := newBlueGreenRollout("baz", 0, nil, nil, "active", "")
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
	ro1 := newBlueGreenRollout("bar", 0, nil, nil, "active", "")
	ro1.Spec.Strategy.BlueGreenStrategy.ActiveService = "foo"
	ro2 := newBlueGreenRollout("baz", 0, nil, nil, "active", "")
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
	ro1 := newBlueGreenRollout("bar", 0, nil, nil, "active", "")
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
	ro1 := newBlueGreenRollout("bar", 0, nil, nil, "active", "")
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

func TestGetActiveReplicaSet(t *testing.T) {
	rolloutNoActiveSelector := &v1alpha1.Rollout{}
	assert.Nil(t, GetActiveReplicaSet(rolloutNoActiveSelector, nil))

	rollout := &v1alpha1.Rollout{
		Status: v1alpha1.RolloutStatus{
			ActiveSelector: "1234",
		},
	}
	rs1 := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "abcd"},
		},
	}
	assert.Nil(t, GetActiveReplicaSet(rollout, []*appsv1.ReplicaSet{rs1}))

	rs2 := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "1234"},
		},
	}
	assert.Equal(t, rs2, GetActiveReplicaSet(rollout, []*appsv1.ReplicaSet{nil, rs1, rs2}))

}

func TestGetPreviewAndActiveServices(t *testing.T) {

	f := newFixture(t)
	expActive := newService("active", 80, nil)
	expPreview := newService("preview", 80, nil)
	f.kubeobjects = append(f.kubeobjects, expActive)
	f.kubeobjects = append(f.kubeobjects, expPreview)
	rollout := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreenStrategy: &v1alpha1.BlueGreenStrategy{
					PreviewService: "preview",
					ActiveService:  "active",
				},
			},
		},
	}
	c, _, _ := f.newController()
	t.Run("Get Both", func(t *testing.T) {
		preview, active, err := c.getPreviewAndActiveServices(rollout)
		assert.Nil(t, err)
		assert.Equal(t, expPreview, preview)
		assert.Equal(t, expActive, active)
	})
	t.Run("Preview not found", func(t *testing.T) {
		noPreviewSvcRollout := rollout.DeepCopy()
		noPreviewSvcRollout.Spec.Strategy.BlueGreenStrategy.PreviewService = "not-preview"
		_, _, err := c.getPreviewAndActiveServices(noPreviewSvcRollout)
		assert.NotNil(t, err)
		assert.True(t, errors.IsNotFound(err))
	})
	t.Run("Active not found", func(t *testing.T) {
		noActiveSvcRollout := rollout.DeepCopy()
		noActiveSvcRollout.Spec.Strategy.BlueGreenStrategy.ActiveService = "not-active"
		_, _, err := c.getPreviewAndActiveServices(noActiveSvcRollout)
		assert.NotNil(t, err)
		assert.True(t, errors.IsNotFound(err))
	})

	t.Run("Invalid Spec: No Active Svc", func(t *testing.T) {
		noActiveSvcRollout := rollout.DeepCopy()
		noActiveSvcRollout.Spec.Strategy.BlueGreenStrategy.ActiveService = ""
		_, _, err := c.getPreviewAndActiveServices(noActiveSvcRollout)
		assert.NotNil(t, err)
		assert.EqualError(t, err, "Invalid Spec: Rollout missing field ActiveService")
	})

}
