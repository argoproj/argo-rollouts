package validation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sunstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
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

const successCaseTlsVsvc = `apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: istio-vsvc
  namespace: default
spec:
  gateways:
  - istio-rollout-gateway
  hosts:
  - istio-rollout.dev.argoproj.io
  tls:
  - match:
    - port: 443
      sniHosts:
      - 'istio-rollout.dev.argoproj.io'
    route:
    - destination:
        host: stable
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

const failCaseTlsVsvc = `apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: istio-vsvc
  namespace: default
spec:
  gateways:
  - istio-rollout-gateway
  hosts:
  - istio-rollout.dev.argoproj.io
  tls:
  - match:
    - port: 443
      sniHosts:
      - 'istio-rollout.dev.argoproj.io'
    route:
    - destination:
        host: not-stable
      weight: 100
    - destination:
        host: canary
      weight: 0`

const failCaseNoRoutesVsvc = `apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: istio-vsvc
  namespace: default
spec:
  gateways:
  - istio-rollout-gateway
  hosts:
  - istio-rollout.dev.argoproj.io`

func getAnalysisTemplatesWithType() AnalysisTemplatesWithType {
	count := intstr.FromInt(1)
	return AnalysisTemplatesWithType{
		AnalysisTemplates: []*v1alpha1.AnalysisTemplate{{
			ObjectMeta: metav1.ObjectMeta{
				Name: "analysis-template-name",
			},
			Spec: v1alpha1.AnalysisTemplateSpec{
				Metrics: []v1alpha1.Metric{{
					Name:     "metric1-name",
					Interval: "1m",
					Count:    &count,
				}},
			},
		}},
		ClusterAnalysisTemplates: []*v1alpha1.ClusterAnalysisTemplate{{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster-analysis-template-name",
			},
			Spec: v1alpha1.AnalysisTemplateSpec{
				Metrics: []v1alpha1.Metric{{
					Name:     "metric2-name",
					Interval: "1m",
					Count:    &count,
				}},
			},
		}},
		TemplateType:    InlineAnalysis,
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

func getIngress() *v1beta1.Ingress {
	return &v1beta1.Ingress{
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
		AnalysisTemplatesWithType: []AnalysisTemplatesWithType{getAnalysisTemplatesWithType()},
		Ingresses:                 []ingressutil.Ingress{*ingressutil.NewLegacyIngress(getIngress())},
		ServiceWithType:           []ServiceWithType{getServiceWithType()},
		VirtualServices:           nil,
	}
	allErrs := ValidateRolloutReferencedResources(getRollout(), refResources)
	assert.Empty(t, allErrs)
}

func TestValidateAnalysisTemplatesWithType(t *testing.T) {
	t.Run("failure - invalid argument", func(t *testing.T) {
		rollout := getRollout()
		templates := getAnalysisTemplatesWithType()
		templates.AnalysisTemplates[0].Spec.Args = append(templates.AnalysisTemplates[0].Spec.Args, v1alpha1.Argument{Name: "invalid"})
		allErrs := ValidateAnalysisTemplatesWithType(rollout, templates)
		assert.Len(t, allErrs, 1)
		msg := fmt.Sprintf("spec.strategy.canary.steps[0].analysis.templates: Invalid value: \"templateNames: [analysis-template-name cluster-analysis-template-name]\": args.invalid was not resolved")
		assert.Equal(t, msg, allErrs[0].Error())
	})

	t.Run("success", func(t *testing.T) {
		rollout := getRollout()
		templates := getAnalysisTemplatesWithType()
		templates.AnalysisTemplates[0].Spec.Args = append(templates.AnalysisTemplates[0].Spec.Args, v1alpha1.Argument{Name: "valid"})
		templates.Args = []v1alpha1.AnalysisRunArgument{{Name: "valid", Value: "true"}}
		allErrs := ValidateAnalysisTemplatesWithType(rollout, templates)
		assert.Empty(t, allErrs)
	})

	t.Run("failure - duplicate metrics", func(t *testing.T) {
		rollout := getRollout()
		templates := getAnalysisTemplatesWithType()
		templates.AnalysisTemplates[0].Spec.Args = append(templates.AnalysisTemplates[0].Spec.Args, v1alpha1.Argument{Name: "metric1-name", Value: pointer.StringPtr("true")})
		templates.AnalysisTemplates[0].Spec.Args[0] = v1alpha1.Argument{Name: "valid", Value: pointer.StringPtr("true")}
		allErrs := ValidateAnalysisTemplatesWithType(rollout, templates)
		assert.Empty(t, allErrs)
	})

	t.Run("failure - duplicate MeasurementRetention", func(t *testing.T) {
		rollout := getRollout()
		rollout.Spec.Strategy.Canary.Steps = append(rollout.Spec.Strategy.Canary.Steps, v1alpha1.CanaryStep{
			Analysis: &v1alpha1.RolloutAnalysis{
				Templates: []v1alpha1.RolloutAnalysisTemplate{
					{
						TemplateName: "analysis-template-name",
					},
				},
				MeasurementRetention: []v1alpha1.MeasurementRetention{
					{
						MetricName: "example",
						Limit:      2,
					},
				},
			},
		})
		templates := getAnalysisTemplatesWithType()
		templates.AnalysisTemplates[0].Spec.Args = append(templates.AnalysisTemplates[0].Spec.Args, v1alpha1.Argument{Name: "valid"})
		templates.AnalysisTemplates[0].Spec.MeasurementRetention = []v1alpha1.MeasurementRetention{
			{
				MetricName: "example",
				Limit:      5,
			},
		}
		templates.Args = []v1alpha1.AnalysisRunArgument{{Name: "valid", Value: "true"}}

		allErrs := ValidateAnalysisTemplatesWithType(rollout, templates)
		assert.Len(t, allErrs, 1)
		msg := fmt.Sprintf("spec.strategy.canary.steps[0].analysis.templates: Invalid value: \"templateNames: [analysis-template-name cluster-analysis-template-name]\": two Measurement Retention metric rules have the same name 'example'")
		assert.Equal(t, msg, allErrs[0].Error())
	})

}

func TestValidateAnalysisTemplateWithType(t *testing.T) {
	t.Run("validate analysisTemplate - success", func(t *testing.T) {
		rollout := getRollout()
		templates := getAnalysisTemplatesWithType()
		allErrs := ValidateAnalysisTemplateWithType(rollout, templates.AnalysisTemplates[0], nil, templates.TemplateType, GetAnalysisTemplateWithTypeFieldPath(templates.TemplateType, templates.CanaryStepIndex))
		assert.Empty(t, allErrs)
	})

	t.Run("validate inline clusterAnalysisTemplate - failure", func(t *testing.T) {
		rollout := getRollout()
		count := intstr.FromInt(0)
		template := getAnalysisTemplatesWithType()
		template.ClusterAnalysisTemplates[0].Spec.Metrics[0].Count = &count
		allErrs := ValidateAnalysisTemplateWithType(rollout, nil, template.ClusterAnalysisTemplates[0], template.TemplateType, GetAnalysisTemplateWithTypeFieldPath(template.TemplateType, template.CanaryStepIndex))
		assert.Len(t, allErrs, 1)
		msg := fmt.Sprintf("AnalysisTemplate %s has metric %s which runs indefinitely. Invalid value for count: %s", "cluster-analysis-template-name", "metric2-name", count.String())
		expectedError := field.Invalid(GetAnalysisTemplateWithTypeFieldPath(template.TemplateType, template.CanaryStepIndex), template.ClusterAnalysisTemplates[0].Name, msg)
		assert.Equal(t, expectedError.Error(), allErrs[0].Error())
	})

	t.Run("validate inline analysisTemplate argument - success", func(t *testing.T) {
		rollout := getRollout()
		template := getAnalysisTemplatesWithType()
		template.AnalysisTemplates[0].Spec.Args = []v1alpha1.Argument{
			{
				Name:  "service-name",
				Value: pointer.StringPtr("service-name"),
			},
		}
		allErrs := ValidateAnalysisTemplateWithType(rollout, template.AnalysisTemplates[0], nil, template.TemplateType, GetAnalysisTemplateWithTypeFieldPath(template.TemplateType, template.CanaryStepIndex))
		assert.Empty(t, allErrs)
	})

	t.Run("validate background analysisTemplate - failure", func(t *testing.T) {
		rollout := getRollout()
		template := getAnalysisTemplatesWithType()
		template.TemplateType = BackgroundAnalysis
		template.AnalysisTemplates[0].Spec.Args = []v1alpha1.Argument{
			{
				Name: "service-name",
			},
		}
		allErrs := ValidateAnalysisTemplateWithType(rollout, template.AnalysisTemplates[0], nil, template.TemplateType, GetAnalysisTemplateWithTypeFieldPath(template.TemplateType, template.CanaryStepIndex))
		assert.NotEmpty(t, allErrs)

		rollout.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
			RolloutAnalysis: v1alpha1.RolloutAnalysis{
				Args: []v1alpha1.AnalysisRunArgument{
					{
						Name: "a-different-service-name",
					},
				},
			},
		}
		allErrs = ValidateAnalysisTemplateWithType(rollout, template.AnalysisTemplates[0], nil, template.TemplateType, GetAnalysisTemplateWithTypeFieldPath(template.TemplateType, template.CanaryStepIndex))
		assert.NotEmpty(t, allErrs)

		template.AnalysisTemplates[0].Spec.Args = append(template.AnalysisTemplates[0].Spec.Args, v1alpha1.Argument{Name: "second-service-name"})
		allErrs = ValidateAnalysisTemplateWithType(rollout, template.AnalysisTemplates[0], nil, template.TemplateType, GetAnalysisTemplateWithTypeFieldPath(template.TemplateType, template.CanaryStepIndex))
		assert.NotEmpty(t, allErrs)
	})

	// verify background analysis matches the arguments in rollout spec
	t.Run("validate background analysisTemplate - success", func(t *testing.T) {
		rollout := getRollout()

		templates := getAnalysisTemplatesWithType()
		templates.TemplateType = BackgroundAnalysis
		allErrs := ValidateAnalysisTemplateWithType(rollout, templates.AnalysisTemplates[0], nil, templates.TemplateType, GetAnalysisTemplateWithTypeFieldPath(templates.TemplateType, templates.CanaryStepIndex))
		assert.Empty(t, allErrs)

		// default value should be fine
		defaultValue := "value-name"
		templates.AnalysisTemplates[0].Spec.Args = []v1alpha1.Argument{
			{
				Name:  "service-name",
				Value: &defaultValue,
			},
		}
		allErrs = ValidateAnalysisTemplateWithType(rollout, templates.AnalysisTemplates[0], nil, templates.TemplateType, GetAnalysisTemplateWithTypeFieldPath(templates.TemplateType, templates.CanaryStepIndex))
		assert.Empty(t, allErrs)

		templates.AnalysisTemplates[0].Spec.Args = []v1alpha1.Argument{
			{
				Name:  "service-name",
				Value: pointer.StringPtr("service-name"),
			},
		}
		rollout.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
			RolloutAnalysis: v1alpha1.RolloutAnalysis{
				Args: []v1alpha1.AnalysisRunArgument{
					{
						Name: "service-name",
					},
				},
			},
		}
		allErrs = ValidateAnalysisTemplateWithType(rollout, templates.AnalysisTemplates[0], nil, templates.TemplateType, GetAnalysisTemplateWithTypeFieldPath(templates.TemplateType, templates.CanaryStepIndex))
		assert.Empty(t, allErrs)
	})

	// verify background analysis does not care about a metric that runs indefinitely
	t.Run("validate background analysisTemplate - success", func(t *testing.T) {
		rollout := getRollout()
		count := intstr.FromInt(0)
		templates := getAnalysisTemplatesWithType()
		templates.TemplateType = BackgroundAnalysis
		templates.AnalysisTemplates[0].Spec.Metrics[0].Count = &count
		allErrs := ValidateAnalysisTemplateWithType(rollout, templates.AnalysisTemplates[0], nil, templates.TemplateType, GetAnalysisTemplateWithTypeFieldPath(templates.TemplateType, templates.CanaryStepIndex))
		assert.Empty(t, allErrs)
	})
}

func TestValidateIngress(t *testing.T) {
	t.Run("validate ingress - success", func(t *testing.T) {
		ingress := ingressutil.NewLegacyIngress(getIngress())
		allErrs := ValidateIngress(getRollout(), ingress)
		assert.Empty(t, allErrs)
	})

	t.Run("validate ingress - failure", func(t *testing.T) {
		ingress := getIngress()
		ingress.Spec.Rules[0].HTTP.Paths[0].Backend.ServiceName = "not-stable-service"
		i := ingressutil.NewLegacyIngress(ingress)
		allErrs := ValidateIngress(getRollout(), i)
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

	t.Run("validate service with unmatch label - failure", func(t *testing.T) {
		svc := getServiceWithType()
		svc.Service.Spec.Selector = map[string]string{"app": "unmatch-rollout-label"}
		allErrs := ValidateService(svc, getRollout())
		assert.Len(t, allErrs, 1)
		expectedErr := field.Invalid(GetServiceWithTypeFieldPath(svc.Type), svc.Service.Name, "Service \"stable-service-name\" has unmatch lable \"app\" in rollout")
		assert.Equal(t, expectedErr.Error(), allErrs[0].Error())
	})

	t.Run("validate service with Rollout label - success", func(t *testing.T) {
		svc := getServiceWithType()
		svc.Service.Spec.Selector = map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "123-456"}
		allErrs := ValidateService(svc, getRollout())
		assert.Empty(t, allErrs)
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
							VirtualService: &v1alpha1.IstioVirtualService{
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

	t.Run("validate virtualService HTTP routes - success", func(t *testing.T) {
		vsvc := unstructured.StrToUnstructuredUnsafe(successCaseVsvc)
		allErrs := ValidateVirtualService(ro, *vsvc)
		assert.Empty(t, allErrs)
	})

	t.Run("validate virtualService HTTP routes - failure", func(t *testing.T) {
		vsvc := unstructured.StrToUnstructuredUnsafe(failCaseVsvc)
		allErrs := ValidateVirtualService(ro, *vsvc)
		assert.Len(t, allErrs, 1)
		expectedErr := field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "istio", "virtualService", "name"), "istio-vsvc", "Istio VirtualService has invalid HTTP routes. Error: Stable Service 'stable' not found in route")
		assert.Equal(t, expectedErr.Error(), allErrs[0].Error())
	})

	t.Run("validate virtualService TLS routes - success", func(t *testing.T) {
		vsvc := unstructured.StrToUnstructuredUnsafe(successCaseTlsVsvc)
		allErrs := ValidateVirtualService(ro, *vsvc)
		assert.Empty(t, allErrs)
	})

	t.Run("validate virtualService TLS routes - failure", func(t *testing.T) {
		vsvc := unstructured.StrToUnstructuredUnsafe(failCaseTlsVsvc)
		allErrs := ValidateVirtualService(ro, *vsvc)
		assert.Len(t, allErrs, 1)
		expectedErr := field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "istio", "virtualService", "name"), "istio-vsvc", "Istio VirtualService has invalid TLS routes. Error: Stable Service 'stable' not found in route")
		assert.Equal(t, expectedErr.Error(), allErrs[0].Error())
	})

	t.Run("validate virtualService no routes - failure", func(t *testing.T) {
		vsvc := unstructured.StrToUnstructuredUnsafe(failCaseNoRoutesVsvc)
		allErrs := ValidateVirtualService(ro, *vsvc)
		assert.Len(t, allErrs, 1)
		expectedErr := field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "istio", "virtualService", "name"), "istio-vsvc", "Unable to get HTTP and/or TLS routes for Istio VirtualService")
		assert.Equal(t, expectedErr.Error(), allErrs[0].Error())
	})
}

func TestValidateVirtualServices(t *testing.T) {
	multipleVirtualService := []v1alpha1.IstioVirtualService{{Name: "istio-vsvc", Routes: []string{"primary", "secondary"}}}

	ro := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService: "stable",
					CanaryService: "canary",
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Istio: &v1alpha1.IstioTrafficRouting{
							VirtualServices: multipleVirtualService,
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
		expectedErr := field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "istio", "virtualServices", "name"), "istio-vsvc", "Istio VirtualService has invalid HTTP routes. Error: Stable Service 'stable' not found in route")
		assert.Equal(t, expectedErr.Error(), allErrs[0].Error())

	})
}

func TestValidateRolloutVirtualServicesConfig(t *testing.T) {
	ro := v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{},
			},
		},
	}
	ro.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		Istio: &v1alpha1.IstioTrafficRouting{},
	}

	// Test when both virtualService and  virtualServices are not configured
	t.Run("validate No virtualService configured - fail", func(t *testing.T) {
		err := ValidateRolloutVirtualServicesConfig(&ro)
		fldPath := field.NewPath("spec", "strategy", "canary", "trafficRouting", "istio")
		expected := fmt.Sprintf("%s: Internal error: either VirtualService or VirtualServices must be configured", fldPath)
		assert.Equal(t, expected, err.Error())
	})

	ro.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		Istio: &v1alpha1.IstioTrafficRouting{
			VirtualService: &v1alpha1.IstioVirtualService{
				Name: "istio-vsvc1-name",
			},
			VirtualServices: []v1alpha1.IstioVirtualService{{Name: "istio-vsvc1-name", Routes: nil}},
		},
	}

	// Test when both virtualService and  virtualServices are  configured
	t.Run("validate both virtualService configured - fail", func(t *testing.T) {
		err := ValidateRolloutVirtualServicesConfig(&ro)
		fldPath := field.NewPath("spec", "strategy", "canary", "trafficRouting", "istio")
		expected := fmt.Sprintf("%s: Internal error: either VirtualService or VirtualServices must be configured", fldPath)
		assert.Equal(t, expected, err.Error())
	})

	ro.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		Istio: &v1alpha1.IstioTrafficRouting{
			VirtualService: &v1alpha1.IstioVirtualService{
				Name: "istio-vsvc1-name",
			},
		},
	}

	// Successful case where either virtualService or  virtualServices configured
	t.Run("validate either virtualService or  virtualServices configured - pass", func(t *testing.T) {
		err := ValidateRolloutVirtualServicesConfig(&ro)
		assert.Empty(t, err)
	})
}

func TestGetAnalysisTemplateWithTypeFieldPath(t *testing.T) {
	t.Run("get fieldPath for analysisTemplateType PrePromotionAnalysis", func(t *testing.T) {
		fldPath := GetAnalysisTemplateWithTypeFieldPath(PrePromotionAnalysis, 0)
		expectedFldPath := field.NewPath("spec", "strategy", "blueGreen", "prePromotionAnalysis", "templates")
		assert.Equal(t, expectedFldPath.String(), fldPath.String())
	})

	t.Run("get fieldPath for analysisTemplateType PostPromotionAnalysis", func(t *testing.T) {
		fldPath := GetAnalysisTemplateWithTypeFieldPath(PostPromotionAnalysis, 0)
		expectedFldPath := field.NewPath("spec", "strategy", "blueGreen", "postPromotionAnalysis", "templates")
		assert.Equal(t, expectedFldPath.String(), fldPath.String())
	})

	t.Run("get fieldPath for analysisTemplateType CanaryStep", func(t *testing.T) {
		fldPath := GetAnalysisTemplateWithTypeFieldPath(InlineAnalysis, 0)
		expectedFldPath := field.NewPath("spec", "strategy", "canary", "steps").Index(0).Child("analysis", "templates")
		assert.Equal(t, expectedFldPath.String(), fldPath.String())
	})

	t.Run("get fieldPath for analysisTemplateType that does not exist", func(t *testing.T) {
		fldPath := GetAnalysisTemplateWithTypeFieldPath("DoesNotExist", 0)
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

	t.Run("get pingService fieldPath", func(t *testing.T) {
		fldPath := GetServiceWithTypeFieldPath(PingService)
		expectedFldPath := field.NewPath("spec", "strategy", "canary", "pingPong", "pingService")
		assert.Equal(t, expectedFldPath.String(), fldPath.String())
	})

	t.Run("get pongService fieldPath", func(t *testing.T) {
		fldPath := GetServiceWithTypeFieldPath(PongService)
		expectedFldPath := field.NewPath("spec", "strategy", "canary", "pingPong", "pongService")
		assert.Equal(t, expectedFldPath.String(), fldPath.String())
	})

	t.Run("get fieldPath for serviceType that does not exist", func(t *testing.T) {
		fldPath := GetServiceWithTypeFieldPath("DoesNotExist")
		assert.Nil(t, fldPath)
	})
}

func TestValidateAmbassadorMapping(t *testing.T) {
	t.Run("will return no error if mapping is valid", func(t *testing.T) {
		// given
		t.Parallel()
		baseMapping := `
apiVersion: getambassador.io/v2
kind:  Mapping
metadata:
  name: myapp-mapping
  namespace: default
spec:
  prefix: /myapp/
  rewrite: /myapp/
  service: myapp:8080`
		obj := unstructured.StrToUnstructuredUnsafe(baseMapping)

		// when
		errList := ValidateAmbassadorMapping(*obj)

		// then
		assert.NotNil(t, errList)
		assert.Equal(t, 0, len(errList))
	})
	t.Run("will return error if base mapping defines weight", func(t *testing.T) {
		// given
		t.Parallel()
		baseMapping := `
apiVersion: getambassador.io/v2
kind:  Mapping
metadata:
  name: myapp-mapping
  namespace: default
spec:
  weight: 20
  prefix: /myapp/
  rewrite: /myapp/
  service: myapp:8080`
		obj := toUnstructured(t, baseMapping)

		// when
		errList := ValidateAmbassadorMapping(*obj)

		// then
		assert.NotNil(t, errList)
		assert.Equal(t, 1, len(errList))
	})
}

func TestValidateAppMeshResource(t *testing.T) {
	t.Run("will return error with appmesh virtual-service", func(t *testing.T) {
		t.Parallel()
		manifest := `
apiVersion: appmesh.k8s.aws/v1beta2
kind: VirtualService
metadata:
  namespace: myns
  name: mysvc
spec:
  awsName: mysvc.myns.svc.cluster.local
  provider:
    virtualRouter:
      virtualRouterRef:
        name: mysvc-vrouter
`
		obj := toUnstructured(t, manifest)
		refResources := ReferencedResources{
			AppMeshResources: []k8sunstructured.Unstructured{*obj},
		}
		errList := ValidateRolloutReferencedResources(getRollout(), refResources)
		assert.NotNil(t, errList)
		assert.Len(t, errList, 1)
		assert.Equal(t, errList[0].Detail, "Expected object kind to be VirtualRouter but is VirtualService")
	})

	t.Run("will return error when appmesh virtual-router has no routes", func(t *testing.T) {
		t.Parallel()
		manifest := `
apiVersion: appmesh.k8s.aws/v1beta2
kind: VirtualRouter
metadata:
  namespace: myns
  name: mysvc-vrouter
spec:
  routes:
`
		obj := toUnstructured(t, manifest)
		errList := ValidateAppMeshResource(*obj)
		assert.NotNil(t, errList)
		assert.Len(t, errList, 1)
		assert.Equal(t, errList[0].Field, field.NewPath("spec", "routes").String())
	})

	routeTypes := []string{"httpRoute", "tcpRoute", "grpcRoute", "http2Route"}
	for _, routeType := range routeTypes {
		t.Run(fmt.Sprintf("will succeed with valid appmesh virtual-router with %s", routeType), func(t *testing.T) {
			t.Parallel()
			manifest := fmt.Sprintf(`
apiVersion: appmesh.k8s.aws/v1beta2
kind: VirtualRouter
metadata:
  namespace: myns
  name: mysvc-vrouter
spec:
  routes:
    - name: primary
      %s:
        action:
          weightedTargets:
            - virtualNodeRef:
                name: mysvc-canary-vn
              weight: 0
            - virtualNodeRef:
                name: mysvc-stable-vn
              weight: 100
`, routeType)
			obj := toUnstructured(t, manifest)
			errList := ValidateAppMeshResource(*obj)
			assert.NotNil(t, errList)
			assert.Len(t, errList, 0)
		})
	}

	t.Run("will return error with appmesh virtual-router with unsupported route type", func(t *testing.T) {
		t.Parallel()
		manifest := `
apiVersion: appmesh.k8s.aws/v1beta2
kind: VirtualRouter
metadata:
  namespace: myns
  name: mysvc-vrouter
spec:
  routes:
    - name: primary
      badRouteType:
`
		obj := toUnstructured(t, manifest)
		errList := ValidateAppMeshResource(*obj)
		assert.NotNil(t, errList)
		assert.Len(t, errList, 1)
		assert.Equal(t, field.NewPath("spec", "routes").Index(0).String(), errList[0].Field)
	})

	t.Run("will return error when appmesh virtual-router has route that is not a struct", func(t *testing.T) {
		t.Parallel()
		manifest := `
apiVersion: appmesh.k8s.aws/v1beta2
kind: VirtualRouter
metadata:
  namespace: myns
  name: mysvc-vrouter
spec:
  routes:
    - invalid-spec
`
		obj := toUnstructured(t, manifest)
		errList := ValidateAppMeshResource(*obj)
		assert.NotNil(t, errList)
		assert.Len(t, errList, 1)
		assert.Equal(t, field.NewPath("spec", "routes").Index(0).String(), errList[0].Field)
	})

	t.Run("will return error when appmesh virtual-router has routes with no targets", func(t *testing.T) {
		t.Parallel()
		manifest := `
apiVersion: appmesh.k8s.aws/v1beta2
kind: VirtualRouter
metadata:
  namespace: myns
  name: mysvc-vrouter
spec:
  routes:
    - name: primary
      httpRoute:
        match:
          prefix: /
        action:
`
		obj := toUnstructured(t, manifest)
		errList := ValidateAppMeshResource(*obj)
		assert.NotNil(t, errList)
		assert.Len(t, errList, 1)
		assert.Equal(t, field.NewPath("spec", "routes").Index(0).Child("httpRoute").Child("action").Child("weightedTargets").String(), errList[0].Field)
	})

	t.Run("will return error when appmesh virtual-router has routes with 1 target", func(t *testing.T) {
		t.Parallel()
		manifest := `
apiVersion: appmesh.k8s.aws/v1beta2
kind: VirtualRouter
metadata:
  namespace: myns
  name: mysvc-vrouter
spec:
  routes:
    - name: primary
      httpRoute:
        match:
          prefix: /
        action:
          weightedTargets:
            - virtualNodeRef:
                name: only-target
              weight: 100
`
		obj := toUnstructured(t, manifest)
		errList := ValidateAppMeshResource(*obj)
		assert.NotNil(t, errList)
		assert.Len(t, errList, 1)
		assert.Equal(t, field.NewPath("spec", "routes").Index(0).Child("httpRoute").Child("action").Child("weightedTargets").String(), errList[0].Field)
	})

	t.Run("will return error when appmesh virtual-router has routes with 3 targets", func(t *testing.T) {
		t.Parallel()
		manifest := `
apiVersion: appmesh.k8s.aws/v1beta2
kind: VirtualRouter
metadata:
  namespace: myns
  name: mysvc-vrouter
spec:
  routes:
    - name: primary
      httpRoute:
        match:
          prefix: /
        action:
          weightedTargets:
            - virtualNodeRef:
                name: target-1
              weight: 10
            - virtualNodeRef:
                name: target-2
              weight: 10
            - virtualNodeRef:
                name: target-3
              weight: 80
`
		obj := toUnstructured(t, manifest)
		errList := ValidateAppMeshResource(*obj)
		assert.NotNil(t, errList)
		assert.Len(t, errList, 1)
		assert.Equal(t, field.NewPath("spec", "routes").Index(0).Child("httpRoute").Child("action").Child("weightedTargets").String(), errList[0].Field)
	})
}

func toUnstructured(t *testing.T, manifest string) *k8sunstructured.Unstructured {
	t.Helper()
	obj := &k8sunstructured.Unstructured{}

	dec := yaml.NewDecodingSerializer(k8sunstructured.UnstructuredJSONScheme)
	_, _, err := dec.Decode([]byte(manifest), nil, obj)
	if err != nil {
		t.Fatal(err)
	}
	return obj
}

func TestValidateAnalysisMetrics(t *testing.T) {
	count, failureLimit := "5", "1"
	args := []v1alpha1.Argument{
		{
			Name:  "count",
			Value: &count,
		},
		{
			Name:  "failure-limit",
			Value: &failureLimit,
		},
		{
			Name: "secret",
			ValueFrom: &v1alpha1.ValueFrom{
				SecretKeyRef: &v1alpha1.SecretKeyRef{
					Name: "web-metric-secret",
					Key:  "apikey",
				},
			},
		},
	}

	countVal := intstr.FromString("{{args.count}}")
	failureLimitVal := intstr.FromString("{{args.failure-limit}}")
	metrics := []v1alpha1.Metric{{
		Name:         "metric-name",
		Count:        &countVal,
		FailureLimit: &failureLimitVal,
	}}

	t.Run("Success", func(t *testing.T) {
		resolvedMetrics, err := validateAnalysisMetrics(metrics, args)
		assert.Nil(t, err)
		assert.Equal(t, count, resolvedMetrics[0].Count.String())
		assert.Equal(t, failureLimit, resolvedMetrics[0].FailureLimit.String())
	})

	t.Run("Error: arg has both Value and ValueFrom", func(t *testing.T) {
		args[2].Value = pointer.StringPtr("secret-value")
		_, err := validateAnalysisMetrics(metrics, args)
		assert.NotNil(t, err)
		assert.Equal(t, "arg 'secret' has both Value and ValueFrom fields", err.Error())

	})
}
