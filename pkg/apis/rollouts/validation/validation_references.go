package validation

import (
	"fmt"

	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	serviceutil "github.com/argoproj/argo-rollouts/utils/service"

	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/ambassador"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/istio"
)

// Controller will validate references in reconciliation

// RolloutConditionType defines the conditions of Rollout
type AnalysisTemplateType string

const (
	PrePromotionAnalysis  AnalysisTemplateType = "PrePromotionAnalysis"
	PostPromotionAnalysis AnalysisTemplateType = "PostPromotionAnalysis"
	InlineAnalysis        AnalysisTemplateType = "InlineAnalysis"
	BackgroundAnalysis    AnalysisTemplateType = "BackgroundAnalysis"
)

type AnalysisTemplateWithType struct {
	AnalysisTemplate        *v1alpha1.AnalysisTemplate
	ClusterAnalysisTemplate *v1alpha1.ClusterAnalysisTemplate
	TemplateType            AnalysisTemplateType
	AnalysisIndex           int
	// Used only for InlineAnalysis
	CanaryStepIndex int
}

type ServiceType string

const (
	StableService  ServiceType = "StableService"
	CanaryService  ServiceType = "CanaryService"
	ActiveService  ServiceType = "ActiveService"
	PreviewService ServiceType = "PreviewService"
)

type ServiceWithType struct {
	Service *corev1.Service
	Type    ServiceType
}

type ReferencedResources struct {
	AnalysisTemplateWithType []AnalysisTemplateWithType
	Ingresses                []v1beta1.Ingress
	ServiceWithType          []ServiceWithType
	VirtualServices          []unstructured.Unstructured
	AmbassadorMappings       []unstructured.Unstructured
}

func ValidateRolloutReferencedResources(rollout *v1alpha1.Rollout, referencedResources ReferencedResources) field.ErrorList {
	allErrs := field.ErrorList{}
	for _, service := range referencedResources.ServiceWithType {
		allErrs = append(allErrs, ValidateService(service, rollout)...)
	}
	for _, template := range referencedResources.AnalysisTemplateWithType {
		allErrs = append(allErrs, ValidateAnalysisTemplateWithType(rollout, template)...)
	}
	for _, ingress := range referencedResources.Ingresses {
		allErrs = append(allErrs, ValidateIngress(rollout, ingress)...)
	}
	for _, vsvc := range referencedResources.VirtualServices {
		allErrs = append(allErrs, ValidateVirtualService(rollout, vsvc)...)
	}
	for _, mapping := range referencedResources.AmbassadorMappings {
		allErrs = append(allErrs, ValidateAmbassadorMapping(mapping)...)
	}
	return allErrs
}

func ValidateService(svc ServiceWithType, rollout *v1alpha1.Rollout) field.ErrorList {
	allErrs := field.ErrorList{}
	fldPath := GetServiceWithTypeFieldPath(svc.Type)
	if fldPath == nil {
		return allErrs
	}

	service := svc.Service
	rolloutManagingService, exists := serviceutil.HasManagedByAnnotation(service)
	if exists && rolloutManagingService != rollout.Name {
		msg := fmt.Sprintf(conditions.ServiceReferencingManagedService, service.Name)
		allErrs = append(allErrs, field.Invalid(fldPath, service.Name, msg))
	}
	return allErrs
}

func ValidateAnalysisTemplateWithType(rollout *v1alpha1.Rollout, template AnalysisTemplateWithType) field.ErrorList {
	allErrs := field.ErrorList{}
	fldPath := GetAnalysisTemplateWithTypeFieldPath(template.TemplateType, template.AnalysisIndex, template.CanaryStepIndex)
	if fldPath == nil {
		return allErrs
	}

	var templateSpec v1alpha1.AnalysisTemplateSpec
	var templateName string
	var args []v1alpha1.Argument

	if template.ClusterAnalysisTemplate != nil {
		templateName, templateSpec, args = template.ClusterAnalysisTemplate.Name, template.ClusterAnalysisTemplate.Spec, template.ClusterAnalysisTemplate.Spec.Args
	} else if template.AnalysisTemplate != nil {
		templateName, templateSpec, args = template.AnalysisTemplate.Name, template.AnalysisTemplate.Spec, template.AnalysisTemplate.Spec.Args
	}

	err := analysisutil.ResolveArgs(args)
	if err != nil {
		msg := fmt.Sprintf("AnalysisTemplate %s has invalid arguments: %v", templateName, err)
		allErrs = append(allErrs, field.Invalid(fldPath, templateName, msg))
	}

	if template.TemplateType != BackgroundAnalysis {
		setArgValuePlaceHolder(templateSpec.Args)
		resolvedMetrics, err := analysisutil.ResolveMetrics(templateSpec.Metrics, templateSpec.Args)
		if err != nil {
			msg := fmt.Sprintf("AnalysisTemplate %s: %v", templateName, err)
			allErrs = append(allErrs, field.Invalid(fldPath, templateName, msg))
		} else {
			for _, metric := range resolvedMetrics {
				effectiveCount := metric.EffectiveCount()
				if effectiveCount == nil {
					msg := fmt.Sprintf("AnalysisTemplate %s has metric %s which runs indefinitely. Invalid value for count: %s", templateName, metric.Name, metric.Count)
					allErrs = append(allErrs, field.Invalid(fldPath, templateName, msg))
				}
			}
		}
	} else if template.TemplateType == BackgroundAnalysis && len(templateSpec.Args) > 0 {
		for _, arg := range templateSpec.Args {
			if arg.Value != nil || arg.ValueFrom != nil {
				continue
			}
			if rollout.Spec.Strategy.Canary == nil || rollout.Spec.Strategy.Canary.Analysis == nil || rollout.Spec.Strategy.Canary.Analysis.Args == nil {
				allErrs = append(allErrs, field.Invalid(fldPath, templateName, "missing analysis arguments in rollout spec"))
				continue
			}

			foundArg := false
			for _, rolloutArg := range rollout.Spec.Strategy.Canary.Analysis.Args {
				if arg.Name == rolloutArg.Name {
					foundArg = true
					break
				}
			}
			if !foundArg {
				allErrs = append(allErrs, field.Invalid(fldPath, templateName, arg.Name))
			}
		}
	}

	return allErrs
}

func setArgValuePlaceHolder(Args []v1alpha1.Argument) {
	for i, arg := range Args {
		if arg.ValueFrom == nil && arg.Value == nil {
			argVal := "dummy-value"
			Args[i].Value = &argVal
		}
	}
}

func ValidateIngress(rollout *v1alpha1.Rollout, ingress v1beta1.Ingress) field.ErrorList {
	allErrs := field.ErrorList{}
	fldPath := field.NewPath("spec", "strategy", "canary", "trafficRouting")
	var ingressName string
	var serviceName string
	if rollout.Spec.Strategy.Canary.TrafficRouting.Nginx != nil {
		fldPath = fldPath.Child("nginx").Child("stableIngress")
		serviceName = rollout.Spec.Strategy.Canary.StableService
		ingressName = rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress
	} else if rollout.Spec.Strategy.Canary.TrafficRouting.ALB != nil {
		fldPath = fldPath.Child("alb").Child("ingress")
		ingressName = rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingress
		serviceName = rollout.Spec.Strategy.Canary.StableService
		if rollout.Spec.Strategy.Canary.TrafficRouting.ALB.RootService != "" {
			serviceName = rollout.Spec.Strategy.Canary.TrafficRouting.ALB.RootService
		}

	} else {
		return allErrs
	}
	if !ingressutil.HasRuleWithService(&ingress, serviceName) {
		msg := fmt.Sprintf("ingress `%s` has no rules using service %s backend", ingress.Name, serviceName)
		allErrs = append(allErrs, field.Invalid(fldPath, ingressName, msg))
	}
	return allErrs
}

func ValidateVirtualService(rollout *v1alpha1.Rollout, obj unstructured.Unstructured) field.ErrorList {
	allErrs := field.ErrorList{}
	newObj := obj.DeepCopy()
	fldPath := field.NewPath("spec", "strategy", "canary", "trafficRouting", "istio", "virtualService", "name")
	vsvcName := rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Name
	httpRoutesI, err := istio.GetHttpRoutesI(newObj)
	if err != nil {
		msg := fmt.Sprintf("Unable to get HTTP routes for Istio VirtualService")
		allErrs = append(allErrs, field.Invalid(fldPath, vsvcName, msg))
	}
	httpRoutes, err := istio.GetHttpRoutes(newObj, httpRoutesI)
	if err != nil {
		msg := fmt.Sprintf("Unable to get HTTP routes for Istio VirtualService")
		allErrs = append(allErrs, field.Invalid(fldPath, vsvcName, msg))
	}
	err = istio.ValidateHTTPRoutes(rollout, httpRoutes)
	if err != nil {
		msg := fmt.Sprintf("Istio VirtualService has invalid HTTP routes. Error: %s", err.Error())
		allErrs = append(allErrs, field.Invalid(fldPath, vsvcName, msg))
	}
	return allErrs
}

func ValidateAmbassadorMapping(obj unstructured.Unstructured) field.ErrorList {
	allErrs := field.ErrorList{}
	fldPath := field.NewPath("spec", "weight")
	weight := ambassador.GetMappingWeight(&obj)
	if weight != 0 {
		msg := fmt.Sprintf("Ambassador mapping %q can not define weight", obj.GetName())
		allErrs = append(allErrs, field.Invalid(fldPath, obj.GetName(), msg))
	}
	return allErrs
}

func GetServiceWithTypeFieldPath(serviceType ServiceType) *field.Path {
	fldPath := field.NewPath("spec", "strategy")
	switch serviceType {
	case ActiveService:
		fldPath = fldPath.Child("blueGreen", "activeService")
	case PreviewService:
		fldPath = fldPath.Child("blueGreen", "previewService")
	case CanaryService:
		fldPath = fldPath.Child("canary", "canaryService")
	case StableService:
		fldPath = fldPath.Child("canary", "stableService")
	default:
		return nil
	}
	return fldPath
}

func GetAnalysisTemplateWithTypeFieldPath(templateType AnalysisTemplateType, analysisIndex int, canaryStepIndex int) *field.Path {
	fldPath := field.NewPath("spec", "strategy")
	switch templateType {
	case PrePromotionAnalysis:
		fldPath = fldPath.Child("blueGreen", "prePromotionAnalysis", "templates")
	case PostPromotionAnalysis:
		fldPath = fldPath.Child("blueGreen", "postPromotionAnalysis", "templates")
	case InlineAnalysis:
		fldPath = fldPath.Child("canary", "steps").Index(canaryStepIndex).Child("analysis", "templates")
	case BackgroundAnalysis:
		fldPath = fldPath.Child("canary", "analysis", "templates")
	default:
		// No path specified
		return nil
	}
	fldPath = fldPath.Index(analysisIndex).Child("templateName")
	return fldPath
}
