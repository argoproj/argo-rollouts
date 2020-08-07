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
	RolloutProgressing AnalysisTemplateType = "Progressing"
	RolloutReplicaFailure AnalysisTemplateType = "ReplicaFailure"
)

type AnalysisTemplateWithPath struct {
	AnalysisTemplate v1alpha1.AnalysisTemplate
	FieldPath string
	Type AnalysisTemplateType
	// Type -> preAnalysis, CanaryStep (i)
}

type ReferencedResources struct {
	AnalysisTemplates []v1alpha1.AnalysisTemplate
	ClusterAnalysisTemplates []v1alpha1.ClusterAnalysisTemplate
	Ingresses []v1beta1.Ingress
	VirtualServices []unstructured.Unstructured
}

// return list of errors - no fieldPath, fieldErrorList

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

// Must run deterministically
func ValidateAnalysisTemplate(analysisTemplate v1alpha1.AnalysisTemplate) field.ErrorList {
	allErrs := field.ErrorList{}
	for _, metric := range analysisTemplate.Spec.Metrics {
		effectiveCount := metric.EffectiveCount()
		if effectiveCount == nil {
			allErrs = append(allErrs, nil) // "Metric metric.Name in analysisTemplate analysisTemplate.name runs indefinitely"
		}
	}
	return allErrs
}

// ALB or Nginx
// Nginx validates existing ingress for stable svc
// ALB checks for annotations
func ValidateIngress(rollout *v1alpha1.Rollout, ingress v1beta1.Ingress) field.ErrorList {
	allErrs := field.ErrorList{}
	trafficRouting := rollout.Spec.Strategy.Canary.TrafficRouting
	if trafficRouting.Nginx != nil {
		var hasStableServiceBackendRule bool
		for _, rule := range ingress.Spec.Rules {
			for _, path := range rule.HTTP.Paths {
				if path.Backend.ServiceName == rollout.Spec.Strategy.Canary.StableService {
					hasStableServiceBackendRule = true
				}
			}
		}
		if !hasStableServiceBackendRule {
			msg := fmt.Sprintf("ingress `%s` has no rules using service %s backend", ingress.Name, rollout.Spec.Strategy.Canary.StableService)
			err := field.Error{
				Type:     field.ErrorTypeRequired,
				Field:    ".Spec.Rules", // TODO: list RO field
				BadValue: nil,
				Detail:   msg,
			}
			allErrs = append(allErrs, &err)
		}
	} else if trafficRouting.ALB != nil {
		if !ingressutil.HasRuleWithService(&ingress, rollout.Spec.Strategy.Canary.StableService) {
			return fmt.Errorf("ingress %s does not use the stable service %s", ingress.Name, rollout.Spec.Strategy.Canary.StableService)
		}
	}
}

func ValidateVirtualService(rollout *v1alpha1.Rollout, obj unstructured.Unstructured) field.ErrorList {
	//allErrs := field.ErrorList{}
	newObj := obj.DeepCopy()
	httpRoutesI, err := istio.GetHttpRoutesI(newObj)
	if err != nil {
		return err
	}
	httpRoutes, err := istio.GetHttpRoutes(newObj, httpRoutesI)
	if err != nil {
		return err
	}
	err = istio.ValidateHTTPRoutes(rollout, httpRoutes)
	if err != nil {
		return err
	}
	return nil
}