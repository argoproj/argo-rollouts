package validation

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ghodss/yaml"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
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

var analysisTemplate = v1alpha1.AnalysisTemplate{
	ObjectMeta: metav1.ObjectMeta{
		Name: "analysis-template-name",
	},
	Spec: v1alpha1.AnalysisTemplateSpec{
		Metrics: []v1alpha1.Metric{{
			Name:     "metric-name",
			Interval: "1m",
			Count:    1,
		}},
	},
}

var analysisTemplateWithType = AnalysisTemplateWithType{
	AnalysisTemplate: &analysisTemplate,
	TemplateType:     CanaryStep,
	AnalysisIndex:    0,
	CanaryStepIndex:  0,
}

var ro = &v1alpha1.Rollout{
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

var ingress = v1beta1.Ingress{
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

var svc = ServiceWithType{
	Service: &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "stable-service-name",
		},
	},
	Type: StableService,
}

func strToUnstructured(yamlStr string) *unstructured.Unstructured {
	obj := make(map[string]interface{})
	yamlStr = strings.ReplaceAll(yamlStr, "\t", "    ")
	err := yaml.Unmarshal([]byte(yamlStr), &obj)
	if err != nil {
		panic(err)
	}
	return &unstructured.Unstructured{Object: obj}
}

func TestValidateRolloutReferencedResources(t *testing.T) {
	refResources := ReferencedResources{
		AnalysisTemplateWithType: []AnalysisTemplateWithType{analysisTemplateWithType},
		Ingresses:                []v1beta1.Ingress{ingress},
		ServiceWithType:          []ServiceWithType{svc},
		VirtualServices:          nil,
	}
	allErrs := ValidateRolloutReferencedResources(ro, refResources)
	assert.Empty(t, allErrs)
}

func TestValidateAnalysisTemplateWithType(t *testing.T) {
	// Success - specify count so that metric runs deterministically
	allErrs := ValidateAnalysisTemplateWithType(analysisTemplateWithType)
	assert.Empty(t, allErrs)

	// Fail case - AnalysisTemplate runs indefinitely
	analysisTemplate.Spec.Metrics[0].Count = 0
	allErrs = ValidateAnalysisTemplateWithType(analysisTemplateWithType)
	assert.Len(t, allErrs, 1)
	assert.Equal(t, "spec.strategy.canary.steps[0].analysis.templates[0].templateName: Invalid value: \"analysis-template-name\": AnalysisTemplate analysis-template-name has metric metric-name which runs indefinitely", allErrs[0].Error())
}

func TestValidateIngress(t *testing.T) {
	// Success
	allErrs := ValidateIngress(ro, ingress)
	assert.Empty(t, allErrs)

	// Failure
	ingress.Spec.Rules[0].HTTP.Paths[0].Backend.ServiceName = "not-stable-service"
	allErrs = ValidateIngress(ro, ingress)
	assert.Equal(t, "spec.strategy.canary.trafficRouting.alb.ingress: Invalid value: \"alb-ingress\": ingress `alb-ingress` has no rules using service stable-service-name backend", allErrs[0].Error())
}

func TestValidateService(t *testing.T) {
	// Success
	allErrs := ValidateService(svc, ro)
	assert.Empty(t, allErrs)

	// Failure - Service managed by another Rollout
	svc.Service.Annotations = map[string]string{v1alpha1.ManagedByRolloutsKey: "not-rollout-name"}
	allErrs = ValidateService(svc, ro)
	assert.Len(t, allErrs, 1)
	assert.Equal(t, "spec.strategy.canary.stableService: Invalid value: \"stable-service-name\": Service \"stable-service-name\" is managed by another Rollout", allErrs[0].Error())
}

// TODO: Incorrect behavior - test passed when RO routes were empty
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
								Name: "istio-vsvc",
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

	// Success
	vsvc := strToUnstructured(successCaseVsvc)
	allErrs := ValidateVirtualService(ro, *vsvc)
	assert.Empty(t, allErrs)

	// Failure
	vsvc = strToUnstructured(failCaseVsvc)
	allErrs = ValidateVirtualService(ro, *vsvc)
	assert.Len(t, allErrs, 1)
	assert.Equal(t, "spec.strategy.canary.trafficRouting.istio.virtualService.name: Invalid value: \"istio-vsvc\": Istio VirtualService has invalid HTTP routes. Error: Stable Service 'stable' not found in route", allErrs[0].Error())
}

func TestGetAnalysisTemplateWithTypeFieldPath(t *testing.T) {
	// PrePromotionAnalysis
	fldPath := GetAnalysisTemplateWithTypeFieldPath(PrePromotionAnalysis, 0, 0)
	assert.Equal(t, "spec.strategy.blueGreen.prePromotionAnalysis.templates[0].templateName", fldPath.String())

	//PostPromotionAnalysis
	fldPath = GetAnalysisTemplateWithTypeFieldPath(PostPromotionAnalysis, 0, 0)
	assert.Equal(t, "spec.strategy.blueGreen.postPromotionAnalysis.templates[0].templateName", fldPath.String())

	// CanaryStep
	fldPath = GetAnalysisTemplateWithTypeFieldPath(CanaryStep, 0, 0)
	assert.Equal(t, "spec.strategy.canary.steps[0].analysis.templates[0].templateName", fldPath.String())

	// AnalysisTemplateType does not exist
	fldPath = GetAnalysisTemplateWithTypeFieldPath("DoesNotExist", 0, 0)
	assert.Nil(t, fldPath)
}

func TestGetServiceWithTypeFieldPath(t *testing.T) {
	// ActiveService
	fldPath := GetServiceWithTypeFieldPath(ActiveService)
	assert.Equal(t, "spec.strategy.blueGreen.activeService", fldPath.String())

	// PreviewService
	fldPath = GetServiceWithTypeFieldPath(PreviewService)
	assert.Equal(t, "spec.strategy.blueGreen.previewService", fldPath.String())

	// CanaryService
	fldPath = GetServiceWithTypeFieldPath(CanaryService)
	assert.Equal(t, "spec.strategy.canary.canaryService", fldPath.String())

	// StableService
	fldPath = GetServiceWithTypeFieldPath(StableService)
	assert.Equal(t, "spec.strategy.canary.stableService", fldPath.String())

	// ServiceType does not exist
	fldPath = GetServiceWithTypeFieldPath("DoesNotExist")
	assert.Nil(t, fldPath)
}
