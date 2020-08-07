package validation

import (
	"fmt"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/istio"
)
// Controller will validate references in reconciliation

// RolloutConditionType defines the conditions of Rollout
type AnalysisTemplateType string

const (
	PrePromotionAnalysis AnalysisTemplateType = "PrePromotionAnalysis"
	PostPromotionAnalysis AnalysisTemplateType = "PostPromotionAnalysis"
	CanaryStepIndexLabel AnalysisTemplateType = "CanaryStepIndex"
)

type AnalysisTemplateWithType struct {
	AnalysisTemplate v1alpha1.AnalysisTemplate
	ClusterAnalysisTemplate v1alpha1.ClusterAnalysisTemplate
	Type AnalysisTemplateType
}

type ReferencedResources struct {
	AnalysisTemplates []v1alpha1.AnalysisTemplate
	ClusterAnalysisTemplates []v1alpha1.ClusterAnalysisTemplate
	Ingresses []v1beta1.Ingress
	VirtualServices []unstructured.Unstructured
}

func ValidateRolloutReferencedResources(rollout *v1alpha1.Rollout, referencedResources ReferencedResources) field.ErrorList {//field.ErrorList {
	allErrs := field.ErrorList{}
	for _, analysisTemplate := range referencedResources.AnalysisTemplates {
		allErrs = append(allErrs, ValidateAnalysisTemplate(analysisTemplate)...)
	}
	for _, ingress := range referencedResources.Ingresses {
		allErrs = append(allErrs, ValidateIngress(rollout, ingress)...)
	}
	for _, vsvc := range referencedResources.VirtualServices {
		allErrs = append(allErrs, ValidateVirtualService(rollout, vsvc)...)
	}
	return allErrs
}

// TODO: Handle ClusterAnalysisTemplate
func ValidateAnalysisTemplate(analysisTemplate v1alpha1.AnalysisTemplate) field.ErrorList {
	allErrs := field.ErrorList{}
	fieldPath := ""
	for _, metric := range analysisTemplate.Spec.Metrics {
		effectiveCount := metric.EffectiveCount()
		if effectiveCount == nil {
			msg := fmt.Sprintf("AnalysisTemplate %s has metric %s which runs indefinitely", metric.Name, analysisTemplate.Name)
			allErrs = append(allErrs, &field.Error{field.ErrorTypeForbidden, fieldPath, nil, msg})
		}
	}
	return allErrs
}

func ValidateIngress(rollout *v1alpha1.Rollout, ingress v1beta1.Ingress) field.ErrorList {
	allErrs := field.ErrorList{}
	fieldPath := ".Spec.Strategy.Canary.TrafficRouting"
	if rollout.Spec.Strategy.Canary.TrafficRouting.Nginx != nil {
		fieldPath += ".Nginx"
	} else {
		fieldPath += ".ALB"
	}
	if !ingressutil.HasRuleWithService(&ingress, rollout.Spec.Strategy.Canary.StableService) {
		msg := fmt.Sprintf("ingress `%s` has no rules using service %s backend", ingress.Name, rollout.Spec.Strategy.Canary.StableService)
		allErrs = append(allErrs, &field.Error{field.ErrorTypeRequired, fieldPath, nil, msg})
	}
	return allErrs
}

func ValidateVirtualService(rollout *v1alpha1.Rollout, obj unstructured.Unstructured) field.ErrorList {
	allErrs := field.ErrorList{}
	newObj := obj.DeepCopy()
	fieldPath := "rollout.Spec.Strategy.Canary.TrafficRouting.Istio"
	httpRoutesI, err := istio.GetHttpRoutesI(newObj)
	if err != nil {
		msg := fmt.Sprintf("Unable to get HTTP routes for Istio VirtualService")
		allErrs = append(allErrs, &field.Error{field.ErrorTypeInvalid, fieldPath, nil, msg})
	}
	httpRoutes, err := istio.GetHttpRoutes(newObj, httpRoutesI)
	if err != nil {
		msg := fmt.Sprintf("Unable to get HTTP routes for Istio VirtualService")
		allErrs = append(allErrs, &field.Error{field.ErrorTypeInvalid, fieldPath, nil, msg})
	}
	err = istio.ValidateHTTPRoutes(rollout, httpRoutes)
	if err != nil {
		msg := fmt.Sprintf("Istio VirtualService has invalid HTTP routes. Error: %s", err.Error())
		allErrs = append(allErrs, &field.Error{field.ErrorTypeInvalid, fieldPath, nil, msg})
	}
	return nil
}