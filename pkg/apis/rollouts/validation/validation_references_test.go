package validation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/unstructured"
)

const successCaseVsvc = `apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: istio-vsvc
  namespace: default
spec:
  gateways:
  - istio-rollout-gateway
  hosts:
  - istio-rollout.dev.argoproj.io
  http:
  - name: primary
    route:
    - destination:
        host: 'stable'
      weight: 100
    - destination:
        host: canary
      weight: 0
  - name: secondary
    route:
    - destination:
        host: 'stable'
      weight: 100
    - destination:
        host: canary
      weight: 0`

const failCaseVsvc = `apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: istio-vsvc
  namespace: default
spec:
  gateways:
  - istio-rollout-gateway
  hosts:
  - istio-rollout.dev.argoproj.io
  http:
  - name: primary
    route:
    - destination:
        host: 'not-stable'
      weight: 100
    - destination:
        host: canary
      weight: 0
  - name: secondary
    route:
    - destination:
        host: 'not-stable'
      weight: 100
    - destination:
        host: canary
      weight: 0`

func getAnalysisTemplateWithType() AnalysisTemplateWithType {
	count := intstr.FromInt(1)
	return AnalysisTemplateWithType{
		AnalysisTemplate: &v1alpha1.AnalysisTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Name: "analysis-template-name",
			},
			Spec: v1alpha1.AnalysisTemplateSpec{
				Metrics: []v1alpha1.Metric{{
					Name:     "metric-name",
					Interval: "1m",
					Count:    &count,
				}},
			},
		},
		TemplateType:    InlineAnalysis,
		AnalysisIndex:   0,
		CanaryStepIndex: 0,
	}
}

func getRollout() *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService: "stable-service-name",
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						ALB: &v1alpha1.ALBTrafficRouting{
							Ingress: "alb-ingress",
						},
					},
				},
			},
		},
	}
}

func getIngress() v1beta1.Ingress {
	return v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "alb-ingress",
		},
		Spec: v1beta1.IngressSpec{
			Rules: []v1beta1.IngressRule{{
				Host: "fakehost.example.com",
				IngressRuleValue: v1beta1.IngressRuleValue{
					HTTP: &v1beta1.HTTPIngressRuleValue{
						Paths: []v1beta1.HTTPIngressPath{{
							Path: "/foo",
							Backend: v1beta1.IngressBackend{
								ServiceName: "stable-service-name",
								ServicePort: intstr.FromString("use-annotations"),
							},
						}},
					},
				},
			}},
		},
	}
}

func getServiceWithType() ServiceWithType {
	return ServiceWithType{
		Service: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "stable-service-name",
			},
		},
		Type: StableService,
	}
}

func TestValidateRolloutReferencedResources(t *testing.T) {
	refResources := ReferencedResources{
		AnalysisTemplateWithType: []AnalysisTemplateWithType{getAnalysisTemplateWithType()},
		Ingresses:                []v1beta1.Ingress{getIngress()},
		ServiceWithType:          []ServiceWithType{getServiceWithType()},
		VirtualServices:          nil,
	}
	allErrs := ValidateRolloutReferencedResources(getRollout(), refResources)
	assert.Empty(t, allErrs)
}

func TestValidateAnalysisTemplateWithType(t *testing.T) {
	t.Run("validate analysisTemplate - success", func(t *testing.T) {
		template := getAnalysisTemplateWithType()
		allErrs := ValidateAnalysisTemplateWithType(template)
		assert.Empty(t, allErrs)
	})

	t.Run("validate inline analysisTemplate - failure", func(t *testing.T) {
		count := intstr.FromInt(0)
		template := getAnalysisTemplateWithType()
		template.AnalysisTemplate.Spec.Metrics[0].Count = &count
		allErrs := ValidateAnalysisTemplateWithType(template)
		assert.Len(t, allErrs, 1)
		msg := fmt.Sprintf("AnalysisTemplate %s has metric %s which runs indefinitely. Invalid value for count: %s", "analysis-template-name", "metric-name", count.String())
		expectedError := field.Invalid(GetAnalysisTemplateWithTypeFieldPath(template.TemplateType, template.AnalysisIndex, template.CanaryStepIndex), template.AnalysisTemplate.Name, msg)
		assert.Equal(t, expectedError.Error(), allErrs[0].Error())
	})

	// verify background analysis does not care about a metric that runs indefinitely
	t.Run("validate background analysisTemplate - success", func(t *testing.T) {
		count := intstr.FromInt(0)
		template := getAnalysisTemplateWithType()
		template.TemplateType = BackgroundAnalysis
		template.AnalysisTemplate.Spec.Metrics[0].Count = &count
		allErrs := ValidateAnalysisTemplateWithType(template)
		assert.Empty(t, allErrs)
	})
}

func TestValidateIngress(t *testing.T) {
	t.Run("validate ingress - success", func(t *testing.T) {
		ingress := getIngress()
		allErrs := ValidateIngress(getRollout(), ingress)
		assert.Empty(t, allErrs)
	})

	t.Run("validate ingress - failure", func(t *testing.T) {
		ingress := getIngress()
		ingress.Spec.Rules[0].HTTP.Paths[0].Backend.ServiceName = "not-stable-service"
		allErrs := ValidateIngress(getRollout(), ingress)
		expectedErr := field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "alb", "ingress"), ingress.Name, "ingress `alb-ingress` has no rules using service stable-service-name backend")
		assert.Equal(t, expectedErr.Error(), allErrs[0].Error())
	})
}

func TestValidateService(t *testing.T) {
	t.Run("validate service - success", func(t *testing.T) {
		svc := getServiceWithType()
		allErrs := ValidateService(svc, getRollout())
		assert.Empty(t, allErrs)
	})

	t.Run("validate service - failure", func(t *testing.T) {
		svc := getServiceWithType()
		svc.Service.Annotations = map[string]string{v1alpha1.ManagedByRolloutsKey: "not-rollout-name"}
		allErrs := ValidateService(svc, getRollout())
		assert.Len(t, allErrs, 1)
		expectedErr := field.Invalid(GetServiceWithTypeFieldPath(svc.Type), svc.Service.Name, "Service \"stable-service-name\" is managed by another Rollout")
		assert.Equal(t, expectedErr.Error(), allErrs[0].Error())
	})
}

func TestValidateVirtualService(t *testing.T) {
	ro := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService: "stable",
					CanaryService: "canary",
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Istio: &v1alpha1.IstioTrafficRouting{
							VirtualService: v1alpha1.IstioVirtualService{
								Name: "istio-vsvc-name",
								Routes: []string{
									"primary",
									"secondary",
								},
							},
						},
					},
				},
			},
		},
	}

	t.Run("validate virtualService - success", func(t *testing.T) {
		vsvc := unstructured.StrToUnstructuredUnsafe(successCaseVsvc)
		allErrs := ValidateVirtualService(ro, *vsvc)
		assert.Empty(t, allErrs)
	})

	t.Run("validate virtualService - failure", func(t *testing.T) {
		vsvc := unstructured.StrToUnstructuredUnsafe(failCaseVsvc)
		allErrs := ValidateVirtualService(ro, *vsvc)
		assert.Len(t, allErrs, 1)
		expectedErr := field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "istio", "virtualService", "name"), "istio-vsvc-name", "Istio VirtualService has invalid HTTP routes. Error: Stable Service 'stable' not found in route")
		assert.Equal(t, expectedErr.Error(), allErrs[0].Error())

	})
}

func TestGetAnalysisTemplateWithTypeFieldPath(t *testing.T) {
	t.Run("get fieldPath for analysisTemplateType PrePromotionAnalysis", func(t *testing.T) {
		fldPath := GetAnalysisTemplateWithTypeFieldPath(PrePromotionAnalysis, 0, 0)
		expectedFldPath := field.NewPath("spec", "strategy", "blueGreen", "prePromotionAnalysis", "templates").Index(0).Child("templateName")
		assert.Equal(t, expectedFldPath.String(), fldPath.String())
	})

	t.Run("get fieldPath for analysisTemplateType PostPromotionAnalysis", func(t *testing.T) {
		fldPath := GetAnalysisTemplateWithTypeFieldPath(PostPromotionAnalysis, 0, 0)
		expectedFldPath := field.NewPath("spec", "strategy", "blueGreen", "postPromotionAnalysis", "templates").Index(0).Child("templateName")
		assert.Equal(t, expectedFldPath.String(), fldPath.String())
	})

	t.Run("get fieldPath for analysisTemplateType CanaryStep", func(t *testing.T) {
		fldPath := GetAnalysisTemplateWithTypeFieldPath(InlineAnalysis, 0, 0)
		expectedFldPath := field.NewPath("spec", "strategy", "canary", "steps").Index(0).Child("analysis", "templates").Index(0).Child("templateName")
		assert.Equal(t, expectedFldPath.String(), fldPath.String())
	})

	t.Run("get fieldPath for analysisTemplateType that does not exist", func(t *testing.T) {
		fldPath := GetAnalysisTemplateWithTypeFieldPath("DoesNotExist", 0, 0)
		assert.Nil(t, fldPath)
	})
}

func TestGetServiceWithTypeFieldPath(t *testing.T) {
	t.Run("get activeService fieldPath", func(t *testing.T) {
		fldPath := GetServiceWithTypeFieldPath(ActiveService)
		expectedFldPath := field.NewPath("spec", "strategy", "blueGreen", "activeService")
		assert.Equal(t, expectedFldPath.String(), fldPath.String())
	})

	t.Run("get previewService fieldPath", func(t *testing.T) {
		fldPath := GetServiceWithTypeFieldPath(PreviewService)
		expectedFldPath := field.NewPath("spec", "strategy", "blueGreen", "previewService")
		assert.Equal(t, expectedFldPath.String(), fldPath.String())
	})

	t.Run("get canaryService fieldPath", func(t *testing.T) {
		fldPath := GetServiceWithTypeFieldPath(CanaryService)
		expectedFldPath := field.NewPath("spec", "strategy", "canary", "canaryService")
		assert.Equal(t, expectedFldPath.String(), fldPath.String())
	})

	t.Run("get stableService fieldPath", func(t *testing.T) {
		fldPath := GetServiceWithTypeFieldPath(StableService)
		expectedFldPath := field.NewPath("spec", "strategy", "canary", "stableService")
		assert.Equal(t, expectedFldPath.String(), fldPath.String())
	})

	t.Run("get fieldPath for serviceType that does not exist", func(t *testing.T) {
		fldPath := GetServiceWithTypeFieldPath("DoesNotExist")
		assert.Nil(t, fldPath)
	})
}
