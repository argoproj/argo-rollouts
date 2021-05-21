package rollout

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
)

func newService(name string, port int, selector map[string]string, ro *v1alpha1.Rollout) *corev1.Service {
	s := &corev1.Service{
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
	if ro != nil {
		s.Annotations = map[string]string{
			v1alpha1.ManagedByRolloutsKey: ro.Name,
		}
	}
	return s
}

func TestGetPreviewAndActiveServices(t *testing.T) {

	f := newFixture(t)
	defer f.Close()
	expActive := newService("active", 80, nil, nil)
	expPreview := newService("preview", 80, nil, nil)
	otherRo := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name: "other-ro",
		},
	}
	otherRoSvc := newService("other-svc", 80, nil, otherRo)
	f.kubeobjects = append(f.kubeobjects, expActive, expPreview, otherRoSvc)
	f.serviceLister = append(f.serviceLister, expActive, expPreview, otherRoSvc)
	rollout := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{
					PreviewService: "preview",
					ActiveService:  "active",
				},
			},
		},
	}
	c, _, _ := f.newController(noResyncPeriodFunc)
	t.Run("Get Both", func(t *testing.T) {
		roCtx, err := c.newRolloutContext(rollout)
		assert.NoError(t, err)
		preview, active, err := roCtx.getPreviewAndActiveServices()
		assert.Nil(t, err)
		assert.Equal(t, expPreview, preview)
		assert.Equal(t, expActive, active)
	})
	t.Run("Preview not found", func(t *testing.T) {
		noPreviewSvcRollout := rollout.DeepCopy()
		noPreviewSvcRollout.Spec.Strategy.BlueGreen.PreviewService = "not-preview"
		roCtx, err := c.newRolloutContext(noPreviewSvcRollout)
		assert.NoError(t, err)
		_, _, err = roCtx.getPreviewAndActiveServices()
		assert.NotNil(t, err)
		assert.True(t, errors.IsNotFound(err))
	})
	t.Run("Active not found", func(t *testing.T) {
		noActiveSvcRollout := rollout.DeepCopy()
		noActiveSvcRollout.Spec.Strategy.BlueGreen.ActiveService = "not-active"
		roCtx, err := c.newRolloutContext(noActiveSvcRollout)
		assert.NoError(t, err)
		_, _, err = roCtx.getPreviewAndActiveServices()
		assert.NotNil(t, err)
		assert.True(t, errors.IsNotFound(err))
	})

	t.Run("Invalid Spec: No Active Svc", func(t *testing.T) {
		noActiveSvcRollout := rollout.DeepCopy()
		noActiveSvcRollout.Spec.Strategy.BlueGreen.ActiveService = ""
		roCtx, err := c.newRolloutContext(noActiveSvcRollout)
		assert.NoError(t, err)
		_, _, err = roCtx.getPreviewAndActiveServices()
		assert.NotNil(t, err)
		assert.EqualError(t, err, "service \"\" not found")
	})
}

func TestActiveServiceNotFound(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r := newBlueGreenRollout("foo", 1, nil, "active-svc", "preview-svc")
	r.Status.Conditions = []v1alpha1.RolloutCondition{}
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)
	previewSvc := newService("preview-svc", 80, nil, r)
	notUsedActiveSvc := newService("active-svc", 80, nil, nil)
	f.kubeobjects = append(f.kubeobjects, previewSvc)
	f.serviceLister = append(f.serviceLister, previewSvc)

	patchIndex := f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))

	patch := f.getPatchedRollout(patchIndex)
	errmsg := "The Rollout \"foo\" is invalid: spec.strategy.blueGreen.activeService: Invalid value: \"active-svc\": service \"active-svc\" not found"
	expectedPatch := `{
			"status": {
				"conditions": [%s],
				"phase": "Degraded",
				"message": "%s: %s"
			}
		}`
	_, pausedCondition := newInvalidSpecCondition(conditions.InvalidSpecReason, notUsedActiveSvc, errmsg)
	assert.Equal(t, calculatePatch(r, fmt.Sprintf(expectedPatch, pausedCondition, conditions.InvalidSpecReason, strings.ReplaceAll(errmsg, "\"", "\\\""))), patch)
}

func TestPreviewServiceNotFound(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r := newBlueGreenRollout("foo", 1, nil, "active-svc", "preview-svc")
	r.Status.Conditions = []v1alpha1.RolloutCondition{}
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)
	activeSvc := newService("active-svc", 80, nil, nil)
	notUsedPreviewSvc := newService("preview-svc", 80, nil, nil)
	f.kubeobjects = append(f.kubeobjects, activeSvc)
	f.serviceLister = append(f.serviceLister)

	patchIndex := f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))

	patch := f.getPatchedRollout(patchIndex)
	errmsg := "The Rollout \"foo\" is invalid: spec.strategy.blueGreen.previewService: Invalid value: \"preview-svc\": service \"preview-svc\" not found"
	expectedPatch := `{
			"status": {
				"conditions": [%s],
				"phase": "Degraded",
				"message": "%s: %s"
			}
		}`
	_, pausedCondition := newInvalidSpecCondition(conditions.InvalidSpecReason, notUsedPreviewSvc, errmsg)
	assert.Equal(t, calculatePatch(r, fmt.Sprintf(expectedPatch, pausedCondition, conditions.InvalidSpecReason, strings.ReplaceAll(errmsg, "\"", "\\\""))), patch)

}
