package validation

import (
	"fmt"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	serviceutil "github.com/argoproj/argo-rollouts/utils/service"

	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation/field"
	corev1 "k8s.io/api/core/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/istio"
)

// Controller will validate references in reconciliation

// RolloutConditionType defines the conditions of Rollout
type AnalysisTemplateType string

const (
	PrePromotionAnalysis  AnalysisTemplateType = "PrePromotionAnalysis"
	PostPromotionAnalysis AnalysisTemplateType = "PostPromotionAnalysis"
	CanaryStepIndex       AnalysisTemplateType = "CanaryStep"
)

type AnalysisTemplateWithType struct {
	AnalysisTemplate        *v1alpha1.AnalysisTemplate
	ClusterAnalysisTemplate *v1alpha1.ClusterAnalysisTemplate
	TemplateType            AnalysisTemplateType
}

type ServiceType string

const (
	StableService ServiceType = "StableService"
	CanaryService ServiceType = "CanaryService"
	ActiveService ServiceType = "ActiveService"
	PreviewService ServiceType = "PreviewService"
)

type ServiceWithType struct {
	Service *corev1.Service
	Type ServiceType
}

type ReferencedResources struct {
	AnalysisTemplateWithType []AnalysisTemplateWithType
	Ingresses                []v1beta1.Ingress
	ServiceWithType			 []ServiceWithType
	VirtualServices          []unstructured.Unstructured
}

func ValidateRolloutReferencedResources(rollout *v1alpha1.Rollout, referencedResources ReferencedResources) field.ErrorList { //field.ErrorList {
	allErrs := field.ErrorList{}
	for _, service := range referencedResources.ServiceWithType {
		allErrs = append(allErrs, ValidateService(service, rollout)...)
	}
	for _, template := range referencedResources.AnalysisTemplateWithType {
		allErrs = append(allErrs, ValidateAnalysisTemplateWithType(template)...)
	}
	for _, ingress := range referencedResources.Ingresses {
		allErrs = append(allErrs, ValidateIngress(rollout, ingress)...)
	}
	for _, vsvc := range referencedResources.VirtualServices {
		allErrs = append(allErrs, ValidateVirtualService(rollout, vsvc)...)
	}
	return allErrs
}

func ValidateService(svc ServiceWithType, rollout *v1alpha1.Rollout) field.ErrorList {
	allErrs := field.ErrorList{}
	fldPath := field.NewPath("spec", "strategy")
	switch svc.Type {
	case ActiveService:
		fldPath = fldPath.Child("blueGreen", "activeService")
	case PreviewService:
		fldPath = fldPath.Child("blueGreen", "previewService")
	case CanaryService:
		fldPath = fldPath.Child("canary", "canaryService")
	case StableService:
		fldPath = fldPath.Child("canary", "stableService")
	default:
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

func ValidateAnalysisTemplateWithType(template AnalysisTemplateWithType) field.ErrorList {
	allErrs := field.ErrorList{}
	fldPath := field.NewPath("spec", "strategy")
	switch template.TemplateType {
	case PrePromotionAnalysis:
		fldPath = fldPath.Child("blueGreen", "prePromotionAnalysis", "templates")
	case PostPromotionAnalysis:
		fldPath = fldPath.Child("blueGreen", "postPromotionAnalysis", "templates")
	case CanaryStepIndex:
		fldPath = fldPath.Child("canary", "steps")
	default:
		return allErrs
	}

	var templateSpec v1alpha1.AnalysisTemplateSpec
	var templateName string
	if template.ClusterAnalysisTemplate != nil {
		templateName, templateSpec = template.ClusterAnalysisTemplate.Name, template.ClusterAnalysisTemplate.Spec
	} else if template.AnalysisTemplate != nil {
		templateName, templateSpec = template.AnalysisTemplate.Name, template.AnalysisTemplate.Spec
	}
	for _, metric := range templateSpec.Metrics {
		effectiveCount := metric.EffectiveCount()
		if effectiveCount == nil {
			msg := fmt.Sprintf("AnalysisTemplate %s has metric %s which runs indefinitely", templateName, metric.Name)
			allErrs = append(allErrs, field.Invalid(fldPath, templateName, msg))
		}
	}
	return allErrs
}

func ValidateIngress(rollout *v1alpha1.Rollout, ingress v1beta1.Ingress) field.ErrorList {
	allErrs := field.ErrorList{}
	fldPath := field.NewPath("spec", "strategy", "canary", "trafficRouting")
	var value interface{}
	if rollout.Spec.Strategy.Canary.TrafficRouting.Nginx != nil {
		fldPath = fldPath.Child("nginx")
		value = rollout.Spec.Strategy.Canary.TrafficRouting.Nginx
	} else if rollout.Spec.Strategy.Canary.TrafficRouting.ALB != nil {
		fldPath = fldPath.Child("alb")
		value = rollout.Spec.Strategy.Canary.TrafficRouting.ALB
	} else {
		return allErrs
	}
	if !ingressutil.HasRuleWithService(&ingress, rollout.Spec.Strategy.Canary.StableService) {
		msg := fmt.Sprintf("ingress `%s` has no rules using service %s backend", ingress.Name, rollout.Spec.Strategy.Canary.StableService)
		allErrs = append(allErrs, field.Invalid(fldPath, value, msg))
	}
	return allErrs
}

func ValidateVirtualService(rollout *v1alpha1.Rollout, obj unstructured.Unstructured) field.ErrorList {
	allErrs := field.ErrorList{}
	newObj := obj.DeepCopy()
	fldPath := field.NewPath("spec", "strategy", "canary", "trafficRouting", "istio")
	value := rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Routes
	httpRoutesI, err := istio.GetHttpRoutesI(newObj)
	if err != nil {
		msg := fmt.Sprintf("Unable to get HTTP routes for Istio VirtualService")
		allErrs = append(allErrs, field.Invalid(fldPath, value, msg))
	}
	httpRoutes, err := istio.GetHttpRoutes(newObj, httpRoutesI)
	if err != nil {
		msg := fmt.Sprintf("Unable to get HTTP routes for Istio VirtualService")
		allErrs = append(allErrs, field.Invalid(fldPath, value, msg))
	}
	err = istio.ValidateHTTPRoutes(rollout, httpRoutes)
	if err != nil {
		msg := fmt.Sprintf("Istio VirtualService has invalid HTTP routes. Error: %s", err.Error())
		allErrs = append(allErrs, field.Invalid(fldPath, value, msg))
	}
	return allErrs
}
