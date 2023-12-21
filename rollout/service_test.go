package rollout

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/aws"
	"github.com/argoproj/argo-rollouts/utils/aws/mocks"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"
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
	assert.JSONEq(t, calculatePatch(r, fmt.Sprintf(expectedPatch, pausedCondition, conditions.InvalidSpecReason, strings.ReplaceAll(errmsg, "\"", "\\\""))), patch)
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
	assert.JSONEq(t, calculatePatch(r, fmt.Sprintf(expectedPatch, pausedCondition, conditions.InvalidSpecReason, strings.ReplaceAll(errmsg, "\"", "\\\""))), patch)

}

func newEndpoints(name string, ips ...string) *corev1.Endpoints {
	ep := corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{},
			},
		},
	}
	for _, ip := range ips {
		address := corev1.EndpointAddress{
			IP: ip,
		}
		ep.Subsets[0].Addresses = append(ep.Subsets[0].Addresses, address)
	}
	return &ep
}

func newTargetGroupBinding(name string) *unstructured.Unstructured {
	return unstructuredutil.StrToUnstructuredUnsafe(fmt.Sprintf(`
apiVersion: elbv2.k8s.aws/v1beta1
kind: TargetGroupBinding
metadata:
  name: %s
  namespace: default
spec:
  serviceRef:
    name: %s
    port: 80
  targetGroupARN: arn::1234
  targetType: ip
`, name, name))
}

func newIngress(name string, canary, stable *corev1.Service) *extensionsv1beta1.Ingress {
	ingress := extensionsv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: extensionsv1beta1.IngressSpec{
			Rules: []extensionsv1beta1.IngressRule{
				{
					Host: "fakehost.example.com",
					IngressRuleValue: extensionsv1beta1.IngressRuleValue{
						HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
							Paths: []extensionsv1beta1.HTTPIngressPath{
								{
									Path: "/foo",
									Backend: extensionsv1beta1.IngressBackend{
										ServiceName: "root",
										ServicePort: intstr.FromString("use-annotations"),
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return &ingress
}

// TestBlueGreenAWSVerifyTargetGroupsNotYetReady verifies we don't proceed with setting stable with
// the blue-green strategy until target group verification is successful
func TestBlueGreenAWSVerifyTargetGroupsNotYetReady(t *testing.T) {
	defaults.SetVerifyTargetGroup(true)
	defer defaults.SetVerifyTargetGroup(false)

	// Allow us to fake out the AWS API
	fakeELB := mocks.ELBv2APIClient{}
	aws.NewClient = aws.FakeNewClientFunc(&fakeELB)
	defer func() {
		aws.NewClient = aws.DefaultNewClientFunc
	}()

	f := newFixture(t)
	defer f.Close()

	tgb := newTargetGroupBinding("active")
	ep := newEndpoints("active", "1.2.3.4", "5.6.7.8", "2.4.6.8")
	thOut := elbv2.DescribeTargetHealthOutput{
		TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.StringPtr("1.2.3.4"),
					Port: pointer.Int32Ptr(80),
				},
			},
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.StringPtr("5.6.7.8"),
					Port: pointer.Int32Ptr(80),
				},
			},
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.StringPtr("2.4.6.8"), // irrelevant
					Port: pointer.Int32Ptr(81),         // wrong port
				},
			},
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.StringPtr("9.8.7.6"), // irrelevant ip
					Port: pointer.Int32Ptr(80),
				},
			},
		},
	}
	fakeELB.On("DescribeTargetHealth", mock.Anything, mock.Anything).Return(&thOut, nil)

	r1 := newBlueGreenRollout("foo", 3, nil, "active", "")
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 3, 3)
	rs2 := newReplicaSetWithStatus(r2, 3, 3)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	svc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}, r2)
	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, rs1PodHash, 3, 3, 6, 3, false, true, false)
	r2.Status.Message = ""
	r2.Status.ObservedGeneration = strconv.Itoa(int(r2.Generation))
	completedHealthyCondition, _ := newHealthyCondition(true)
	conditions.SetRolloutCondition(&r2.Status, completedHealthyCondition)
	progressingCondition, _ := newProgressingCondition(conditions.NewRSAvailableReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)
	completedCondition, _ := newCompletedCondition(false)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2, tgb)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2, svc, ep)
	f.serviceLister = append(f.serviceLister, svc)

	f.expectGetEndpointsAction(ep)
	patchIndex := f.expectPatchRolloutAction(r2) // update status message
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{"status":{"message":"waiting for post-promotion verification to complete"}}`
	assert.Equal(t, expectedPatch, patch)
	f.assertEvents([]string{
		conditions.TargetGroupUnverifiedReason,
	})
}

// TestBlueGreenAWSVerifyTargetGroupsReady verifies we proceed with setting stable with
// the blue-green strategy when target group verification is successful
func TestBlueGreenAWSVerifyTargetGroupsReady(t *testing.T) {
	defaults.SetVerifyTargetGroup(true)
	defer defaults.SetVerifyTargetGroup(false)

	// Allow us to fake out the AWS API
	fakeELB := mocks.ELBv2APIClient{}
	aws.NewClient = aws.FakeNewClientFunc(&fakeELB)
	defer func() {
		aws.NewClient = aws.DefaultNewClientFunc
	}()

	f := newFixture(t)
	defer f.Close()

	tgb := newTargetGroupBinding("active")
	ep := newEndpoints("active", "1.2.3.4", "5.6.7.8", "2.4.6.8")
	thOut := elbv2.DescribeTargetHealthOutput{
		TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.StringPtr("1.2.3.4"),
					Port: pointer.Int32Ptr(80),
				},
			},
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.StringPtr("5.6.7.8"),
					Port: pointer.Int32Ptr(80),
				},
			},
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.StringPtr("2.4.6.8"),
					Port: pointer.Int32Ptr(80),
				},
			},
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.StringPtr("9.8.7.6"), // irrelevant ip
					Port: pointer.Int32Ptr(80),
				},
			},
		},
	}
	fakeELB.On("DescribeTargetHealth", mock.Anything, mock.Anything).Return(&thOut, nil)

	r1 := newBlueGreenRollout("foo", 3, nil, "active", "")
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 3, 3)
	rs2 := newReplicaSetWithStatus(r2, 3, 3)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	svc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}, r2)
	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, rs1PodHash, 3, 3, 6, 3, false, true, false)
	r2.Status.Message = "waiting for post-promotion verification to complete"
	r2.Status.ObservedGeneration = strconv.Itoa(int(r2.Generation))
	completedCondition, _ := newHealthyCondition(true)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)
	progressingCondition, _ := newProgressingCondition(conditions.NewRSAvailableReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)
	completedCond := conditions.NewRolloutCondition(v1alpha1.RolloutCompleted, corev1.ConditionTrue, conditions.RolloutCompletedReason, conditions.RolloutCompletedReason)
	conditions.SetRolloutCondition(&r2.Status, *completedCond)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2, tgb)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2, svc, ep)
	f.serviceLister = append(f.serviceLister, svc)

	f.expectGetEndpointsAction(ep)
	patchIndex := f.expectPatchRolloutAction(r2) // update status message
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := fmt.Sprintf(`{"status":{"message":null,"phase":"Healthy","stableRS":"%s"}}`, rs2PodHash)
	assert.Equal(t, expectedPatch, patch)
	f.assertEvents([]string{
		conditions.TargetGroupVerifiedReason,
		conditions.RolloutCompletedReason,
	})
}

// TestCanaryAWSVerifyTargetGroupsNotYetReady verifies we don't proceed with scale down of old
// ReplicaSets in the canary strategy until target group verification is successful
func TestCanaryAWSVerifyTargetGroupsNotYetReady(t *testing.T) {
	defaults.SetVerifyTargetGroup(true)
	defer defaults.SetVerifyTargetGroup(false)

	// Allow us to fake out the AWS API
	fakeELB := mocks.ELBv2APIClient{}
	aws.NewClient = aws.FakeNewClientFunc(&fakeELB)
	defer func() {
		aws.NewClient = aws.DefaultNewClientFunc
	}()

	f := newFixture(t)
	defer f.Close()

	tgb := newTargetGroupBinding("stable")
	ep := newEndpoints("stable", "1.2.3.4", "5.6.7.8", "2.4.6.8")
	thOut := elbv2.DescribeTargetHealthOutput{
		TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.String("1.2.3.4"),
					Port: pointer.Int32(80),
				},
			},
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.String("5.6.7.8"),
					Port: pointer.Int32(80),
				},
			},
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.String("2.4.6.8"), // irrelevant
					Port: pointer.Int32(81),         // wrong port
				},
			},
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.String("9.8.7.6"), // irrelevant ip
					Port: pointer.Int32(80),
				},
			},
		},
	}
	fakeELB.On("DescribeTargetHealth", mock.Anything, mock.Anything).Return(&thOut, nil)

	r1 := newCanaryRollout("foo", 3, nil, []v1alpha1.CanaryStep{{
		SetWeight: pointer.Int32(10),
	}}, pointer.Int32(0), intstr.FromString("25%"), intstr.FromString("25%"))

	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		ALB: &v1alpha1.ALBTrafficRouting{
			Ingress:     "ingress",
			RootService: "root",
		},
	}
	r1.Spec.Strategy.Canary.CanaryService = "canary"
	r1.Spec.Strategy.Canary.StableService = "stable"
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 3, 3)
	rs2 := newReplicaSetWithStatus(r2, 3, 3)

	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	rootSvc := newService("root", 80, map[string]string{"app": "foo"}, nil)
	stableSvc := newService("canary", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}, r2)
	canarySvc := newService("stable", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}, r2)
	ing := newIngress("ingress", canarySvc, stableSvc)

	r2 = updateCanaryRolloutStatus(r2, rs2PodHash, 6, 3, 6, false)
	r2.Status.Message = ""
	r2.Status.ObservedGeneration = strconv.Itoa(int(r2.Generation))
	r2.Status.StableRS = rs2PodHash
	r2.Status.CurrentStepIndex = pointer.Int32(1)
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)
	healthyCondition, _ := newHealthyCondition(false)
	conditions.SetRolloutCondition(&r2.Status, healthyCondition)
	progressingCondition, _ := newProgressingCondition(conditions.NewRSAvailableReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)
	completedCondition, _ := newCompletedCondition(true)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)
	_, r2.Status.Canary.Weights = calculateWeightStatus(r2, rs2PodHash, rs2PodHash, 0)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2, tgb)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2, ing, rootSvc, canarySvc, stableSvc, ep)
	f.serviceLister = append(f.serviceLister, rootSvc, canarySvc, stableSvc)
	f.ingressLister = append(f.ingressLister, ingressutil.NewLegacyIngress(ing))

	f.expectGetEndpointsAction(ep)
	f.run(getKey(r2, t))
	f.assertEvents([]string{
		conditions.TargetGroupUnverifiedReason,
	})
}

// TestCanaryAWSVerifyTargetGroupsReady verifies we proceed with scale down of old
// ReplicaSets in the canary strategy after target group verification is successful
func TestCanaryAWSVerifyTargetGroupsReady(t *testing.T) {
	defaults.SetVerifyTargetGroup(true)
	defer defaults.SetVerifyTargetGroup(false)

	// Allow us to fake out the AWS API
	fakeELB := mocks.ELBv2APIClient{}
	aws.NewClient = aws.FakeNewClientFunc(&fakeELB)
	defer func() {
		aws.NewClient = aws.DefaultNewClientFunc
	}()

	f := newFixture(t)
	defer f.Close()

	tgb := newTargetGroupBinding("stable")
	ep := newEndpoints("stable", "1.2.3.4", "5.6.7.8", "2.4.6.8")
	thOut := elbv2.DescribeTargetHealthOutput{
		TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.String("1.2.3.4"),
					Port: pointer.Int32(80),
				},
			},
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.String("5.6.7.8"),
					Port: pointer.Int32(80),
				},
			},
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.String("2.4.6.8"), // irrelevant
					Port: pointer.Int32(80),         // wrong port
				},
			},
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.String("9.8.7.6"), // irrelevant ip
					Port: pointer.Int32(80),
				},
			},
		},
	}
	fakeELB.On("DescribeTargetHealth", mock.Anything, mock.Anything).Return(&thOut, nil)

	r1 := newCanaryRollout("foo", 3, nil, []v1alpha1.CanaryStep{{
		SetWeight: pointer.Int32(10),
	}}, pointer.Int32(0), intstr.FromString("25%"), intstr.FromString("25%"))
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		ALB: &v1alpha1.ALBTrafficRouting{
			Ingress:     "ingress",
			RootService: "root",
		},
	}
	r1.Spec.Strategy.Canary.CanaryService = "canary"
	r1.Spec.Strategy.Canary.StableService = "stable"
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 3, 3)
	rs2 := newReplicaSetWithStatus(r2, 3, 3)

	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	rootSvc := newService("root", 80, map[string]string{"app": "foo"}, nil)
	stableSvc := newService("canary", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}, r2)
	canarySvc := newService("stable", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}, r2)
	ing := newIngress("ingress", canarySvc, stableSvc)

	r2 = updateCanaryRolloutStatus(r2, rs2PodHash, 6, 3, 6, false)
	r2.Status.Message = ""
	r2.Status.ObservedGeneration = strconv.Itoa(int(r2.Generation))
	r2.Status.StableRS = rs2PodHash
	r2.Status.CurrentStepIndex = pointer.Int32(1)
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)
	healthyCondition, _ := newHealthyCondition(false)
	conditions.SetRolloutCondition(&r2.Status, healthyCondition)
	progressingCondition, _ := newProgressingCondition(conditions.NewRSAvailableReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)
	completedCondition, _ := newCompletedCondition(true)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)
	_, r2.Status.Canary.Weights = calculateWeightStatus(r2, rs2PodHash, rs2PodHash, 0)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2, tgb)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2, ing, rootSvc, canarySvc, stableSvc, ep)
	f.serviceLister = append(f.serviceLister, rootSvc, canarySvc, stableSvc)
	f.ingressLister = append(f.ingressLister, ingressutil.NewLegacyIngress(ing))

	f.expectGetEndpointsAction(ep)
	scaleDownRSIndex := f.expectPatchReplicaSetAction(rs1)
	f.run(getKey(r2, t))
	f.verifyPatchedReplicaSet(scaleDownRSIndex, 30)
	f.assertEvents([]string{
		conditions.TargetGroupVerifiedReason,
	})
}

// TestCanaryAWSVerifyTargetGroupsSkip verifies we skip unnecessary verification if scaledown
// annotation does not need to happen
func TestCanaryAWSVerifyTargetGroupsSkip(t *testing.T) {
	defaults.SetVerifyTargetGroup(true)
	defer defaults.SetVerifyTargetGroup(false)

	f := newFixture(t)
	defer f.Close()

	r1 := newCanaryRollout("foo", 3, nil, []v1alpha1.CanaryStep{{
		SetWeight: pointer.Int32(10),
	}}, pointer.Int32(0), intstr.FromString("25%"), intstr.FromString("25%"))
	r1.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		ALB: &v1alpha1.ALBTrafficRouting{
			Ingress:     "ingress",
			RootService: "root",
		},
	}
	r1.Spec.Strategy.Canary.CanaryService = "canary"
	r1.Spec.Strategy.Canary.StableService = "stable"
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 3, 3)
	// set an annotation on old RS to cause verification to be skipped
	rs1.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = timeutil.Now().Add(600 * time.Second).UTC().Format(time.RFC3339)
	rs2 := newReplicaSetWithStatus(r2, 3, 3)

	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	rootSvc := newService("root", 80, map[string]string{"app": "foo"}, nil)
	stableSvc := newService("canary", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}, r2)
	canarySvc := newService("stable", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}, r2)
	ing := newIngress("ingress", canarySvc, stableSvc)

	r2 = updateCanaryRolloutStatus(r2, rs2PodHash, 6, 3, 6, false)
	r2.Status.Message = ""
	r2.Status.ObservedGeneration = strconv.Itoa(int(r2.Generation))
	r2.Status.StableRS = rs2PodHash
	r2.Status.CurrentStepIndex = pointer.Int32(1)
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)
	healthyCondition, _ := newHealthyCondition(false)
	conditions.SetRolloutCondition(&r2.Status, healthyCondition)
	progressingCondition, _ := newProgressingCondition(conditions.NewRSAvailableReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)
	completedCondition, _ := newCompletedCondition(true)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)
	_, r2.Status.Canary.Weights = calculateWeightStatus(r2, rs2PodHash, rs2PodHash, 0)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2, ing, rootSvc, canarySvc, stableSvc)
	f.serviceLister = append(f.serviceLister, rootSvc, canarySvc, stableSvc)
	f.ingressLister = append(f.ingressLister, ingressutil.NewLegacyIngress(ing))

	f.run(getKey(r2, t)) // there should be no api calls
	f.assertEvents(nil)
}

// TestShouldVerifyTargetGroups returns whether or not we should verify the target group
func TestShouldVerifyTargetGroups(t *testing.T) {
	defaults.SetVerifyTargetGroup(true)
	defer defaults.SetVerifyTargetGroup(false)

	f := newFixture(t)
	defer f.Close()
	ctrl, _, _ := f.newController(noResyncPeriodFunc)

	t.Run("CanaryNotUsingTrafficRouting", func(t *testing.T) {
		ro := newCanaryRollout("foo", 3, nil, nil, nil, intstr.FromString("25%"), intstr.FromString("25%"))
		roCtx, err := ctrl.newRolloutContext(ro)
		roCtx.newRS = newReplicaSetWithStatus(ro, 3, 3)
		stableSvc := newService("stable", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: roCtx.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]}, ro)
		assert.NoError(t, err)
		assert.False(t, roCtx.shouldVerifyTargetGroup(stableSvc))
	})
	t.Run("CanaryNotFullyPromoted", func(t *testing.T) {
		ro := newCanaryRollout("foo", 3, nil, nil, nil, intstr.FromString("25%"), intstr.FromString("25%"))
		ro.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			ALB: &v1alpha1.ALBTrafficRouting{
				Ingress: "ingress",
			},
		}
		roCtx, err := ctrl.newRolloutContext(ro)
		roCtx.newRS = newReplicaSetWithStatus(ro, 3, 3)
		stableSvc := newService("stable", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: roCtx.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]}, ro)
		ro.Status.StableRS = "somethingelse"
		assert.NoError(t, err)
		assert.False(t, roCtx.shouldVerifyTargetGroup(stableSvc))
	})
	t.Run("CanaryFullyPromoted", func(t *testing.T) {
		ro := newCanaryRollout("foo", 3, nil, nil, nil, intstr.FromString("25%"), intstr.FromString("25%"))
		ro.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			ALB: &v1alpha1.ALBTrafficRouting{
				Ingress: "ingress",
			},
		}
		roCtx, err := ctrl.newRolloutContext(ro)
		roCtx.newRS = newReplicaSetWithStatus(ro, 3, 3)
		stableSvc := newService("stable", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: roCtx.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]}, ro)
		ro.Status.StableRS = roCtx.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		assert.NoError(t, err)
		assert.True(t, roCtx.shouldVerifyTargetGroup(stableSvc))
	})
	t.Run("BlueGreenFullyPromoted", func(t *testing.T) {
		ro := newBlueGreenRollout("foo", 3, nil, "active-svc", "")
		roCtx, err := ctrl.newRolloutContext(ro)
		roCtx.newRS = newReplicaSetWithStatus(ro, 3, 3)
		activeSvc := newService("active-svc", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: roCtx.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]}, ro)
		ro.Status.StableRS = roCtx.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		assert.NoError(t, err)
		assert.False(t, roCtx.shouldVerifyTargetGroup(activeSvc))
	})
	t.Run("BlueGreenBeforePromotion", func(t *testing.T) {
		ro := newBlueGreenRollout("foo", 3, nil, "active-svc", "")
		roCtx, err := ctrl.newRolloutContext(ro)
		roCtx.newRS = newReplicaSetWithStatus(ro, 3, 3)
		activeSvc := newService("active-svc", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "oldrshash"}, ro)
		ro.Status.StableRS = "oldrshash"
		assert.NoError(t, err)
		assert.False(t, roCtx.shouldVerifyTargetGroup(activeSvc))
	})
	t.Run("BlueGreenAfterPromotionAfterPromotionAnalysisStarted", func(t *testing.T) {
		ro := newBlueGreenRollout("foo", 3, nil, "active-svc", "")
		roCtx, err := ctrl.newRolloutContext(ro)
		roCtx.newRS = newReplicaSetWithStatus(ro, 3, 3)
		activeSvc := newService("active-svc", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: roCtx.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]}, ro)
		ro.Status.StableRS = "oldrshash"
		ro.Status.BlueGreen.PostPromotionAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{}
		assert.NoError(t, err)
		assert.False(t, roCtx.shouldVerifyTargetGroup(activeSvc))
	})
	t.Run("BlueGreenAfterPromotion", func(t *testing.T) {
		ro := newBlueGreenRollout("foo", 3, nil, "active-svc", "")
		roCtx, err := ctrl.newRolloutContext(ro)
		roCtx.newRS = newReplicaSetWithStatus(ro, 3, 3)
		activeSvc := newService("active-svc", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: roCtx.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]}, ro)
		ro.Status.StableRS = "oldrshash"
		assert.NoError(t, err)
		assert.True(t, roCtx.shouldVerifyTargetGroup(activeSvc))
	})
}

// TestDelayCanaryStableServiceLabelInjection verifies we don't inject pod hash labels to the canary
// or stable service before the pods for them are ready.
func TestDelayCanaryStableServiceLabelInjection(t *testing.T) {
	ro1 := newCanaryRollout("foo", 3, nil, nil, nil, intstr.FromInt(1), intstr.FromInt(1))
	ro1.Spec.Strategy.Canary.CanaryService = "canary"
	ro1.Spec.Strategy.Canary.StableService = "stable"
	canarySvc := newService("canary", 80, ro1.Spec.Selector.MatchLabels, nil)
	stableSvc := newService("stable", 80, ro1.Spec.Selector.MatchLabels, nil)
	ro2 := bumpVersion(ro1)

	f := newFixture(t)
	defer f.Close()
	f.kubeobjects = append(f.kubeobjects, canarySvc, stableSvc)
	f.serviceLister = append(f.serviceLister, canarySvc, stableSvc)

	{
		// first ensure we don't update service because new/stable are both not available
		ctrl, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := ctrl.newRolloutContext(ro1)
		assert.NoError(t, err)

		roCtx.newRS = newReplicaSetWithStatus(ro1, 3, 0)
		roCtx.stableRS = newReplicaSetWithStatus(ro2, 3, 0)

		err = roCtx.reconcileStableAndCanaryService()
		assert.NoError(t, err)
		_, canaryInjected := canarySvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
		assert.False(t, canaryInjected)
		_, stableInjected := stableSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
		assert.False(t, stableInjected)
	}
	{
		// ensure we don't update service because new/stable are both partially available on an adoption of service reconcile
		ctrl, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := ctrl.newRolloutContext(ro1)
		assert.NoError(t, err)

		roCtx.newRS = newReplicaSetWithStatus(ro1, 3, 1)
		roCtx.stableRS = newReplicaSetWithStatus(ro2, 3, 1)

		err = roCtx.reconcileStableAndCanaryService()
		assert.NoError(t, err)
		_, canaryInjected := canarySvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
		assert.False(t, canaryInjected)
		_, stableInjected := stableSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
		assert.False(t, stableInjected)
	}
	{
		// next ensure we do update service because new/stable are now available
		ctrl, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := ctrl.newRolloutContext(ro1)
		assert.NoError(t, err)

		roCtx.newRS = newReplicaSetWithStatus(ro1, 3, 3)
		roCtx.stableRS = newReplicaSetWithStatus(ro2, 3, 3)

		err = roCtx.reconcileStableAndCanaryService()
		assert.NoError(t, err)
		_, canaryInjected := canarySvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
		assert.True(t, canaryInjected)
		_, stableInjected := stableSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
		assert.True(t, stableInjected)
	}

}

// TestDelayCanaryStableServiceDelayOnAdoptedService verifies allow partial readiness of pods when switching labels
// on an adopted services, but that if there is zero readiness we will not switch
func TestDelayCanaryStableServiceDelayOnAdoptedService(t *testing.T) {
	ro1 := newCanaryRollout("foo", 3, nil, nil, nil, intstr.FromInt(1), intstr.FromInt(1))
	ro1.Spec.Strategy.Canary.CanaryService = "canary"
	ro1.Spec.Strategy.Canary.StableService = "stable"
	//Setup services that are already adopted by rollouts
	stableSvc := newService("stable", 80, ro1.Spec.Selector.MatchLabels, ro1)
	ro2 := bumpVersion(ro1)
	canarySvc := newService("canary", 80, ro1.Spec.Selector.MatchLabels, ro2)

	f := newFixture(t)
	defer f.Close()
	f.kubeobjects = append(f.kubeobjects, canarySvc, stableSvc)
	f.serviceLister = append(f.serviceLister, canarySvc, stableSvc)

	t.Run("AdoptedService No Availability", func(t *testing.T) {
		// first ensure we don't update service because new/stable are both not available
		ctrl, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := ctrl.newRolloutContext(ro1)
		assert.NoError(t, err)

		roCtx.newRS = newReplicaSetWithStatus(ro1, 3, 0)
		roCtx.stableRS = newReplicaSetWithStatus(ro2, 3, 0)

		err = roCtx.reconcileStableAndCanaryService()
		assert.NoError(t, err)
		canaryHash2, canaryInjected := canarySvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
		assert.False(t, canaryInjected)
		fmt.Println(canaryHash2)
		stableHash2, stableInjected := stableSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
		assert.False(t, stableInjected)
		fmt.Println(stableHash2)
	})
	t.Run("AdoptedService Partial Availability", func(t *testing.T) {
		// ensure we do change selector on partially available replica sets
		ctrl, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := ctrl.newRolloutContext(ro1)
		assert.NoError(t, err)

		roCtx.newRS = newReplicaSetWithStatus(ro1, 3, 1)
		roCtx.stableRS = newReplicaSetWithStatus(ro2, 3, 2)

		err = roCtx.reconcileStableAndCanaryService()
		assert.NoError(t, err)
		canaryHash2, canaryInjected := canarySvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
		assert.True(t, canaryInjected)
		fmt.Println(canaryHash2)
		stableHash2, stableInjected := stableSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
		assert.True(t, stableInjected)
		fmt.Println(stableHash2)
	})

}
