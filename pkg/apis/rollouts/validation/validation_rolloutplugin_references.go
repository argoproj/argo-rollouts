package validation

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
)

// ReferencedRolloutPluginResources holds resources referenced by a RolloutPlugin
// that need to be validated.
type ReferencedRolloutPluginResources struct {
	AnalysisTemplatesWithType []AnalysisTemplatesWithType
}

// ValidateRolloutPluginReferencedResources validates all resources referenced by a RolloutPlugin.
func ValidateRolloutPluginReferencedResources(rp *v1alpha1.RolloutPlugin, refs ReferencedRolloutPluginResources) field.ErrorList {
	allErrs := field.ErrorList{}
	for _, templates := range refs.AnalysisTemplatesWithType {
		allErrs = append(allErrs, ValidateAnalysisTemplateForRolloutPlugin(rp, templates)...)
	}
	return allErrs
}

// ValidateAnalysisTemplateForRolloutPlugin validates analysis templates referenced by a RolloutPlugin.
// Checks metric-uniqueness and argument resolution.
func ValidateAnalysisTemplateForRolloutPlugin(rp *v1alpha1.RolloutPlugin, templates AnalysisTemplatesWithType) field.ErrorList {
	allErrs := field.ErrorList{}
	fldPath := GetAnalysisTemplateWithTypeFieldPath(templates.TemplateType, templates.CanaryStepIndex)
	if fldPath == nil {
		return allErrs
	}

	templateNames := GetAnalysisTemplateNames(templates)
	value := fmt.Sprintf("templateNames: %s", templateNames)

	args := make([]v1alpha1.Argument, 0, len(templates.Args))
	for _, a := range templates.Args {
		arg := v1alpha1.Argument{Name: a.Name}
		if a.Value != "" {
			val := a.Value
			arg.Value = &val
		}
		args = append(args, arg)
	}

	_, err := analysisutil.NewAnalysisRunFromTemplates(
		templates.AnalysisTemplates,
		templates.ClusterAnalysisTemplates,
		args,
		[]v1alpha1.DryRun{},
		[]v1alpha1.MeasurementRetention{},
		make(map[string]string),
		make(map[string]string),
		"", "", "",
	)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath, value, err.Error()))
		return allErrs
	}

	// For non-background analyses, validate that metrics have finite counts
	if templates.TemplateType != BackgroundAnalysis {
		for _, template := range templates.AnalysisTemplates {
			allErrs = append(allErrs, validateRolloutPluginAnalysisTemplate(template.Name, template.Spec, fldPath)...)
		}
		for _, clusterTemplate := range templates.ClusterAnalysisTemplates {
			allErrs = append(allErrs, validateRolloutPluginAnalysisTemplate(clusterTemplate.Name, clusterTemplate.Spec, fldPath)...)
		}
	}

	return allErrs
}

// validateRolloutPluginAnalysisTemplate validates that a template's metrics have finite counts.
func validateRolloutPluginAnalysisTemplate(templateName string, spec v1alpha1.AnalysisTemplateSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	// Set placeholder values for args without values so metric resolution works
	argsCopy := make([]v1alpha1.Argument, len(spec.Args))
	copy(argsCopy, spec.Args)
	for i, arg := range argsCopy {
		if arg.ValueFrom == nil && arg.Value == nil {
			placeholder := "dummy-value"
			argsCopy[i].Value = &placeholder
		}
	}

	resolvedMetrics, err := validateAnalysisMetrics(spec.Metrics, argsCopy)
	if err != nil {
		msg := fmt.Sprintf("AnalysisTemplate %s: %v", templateName, err)
		allErrs = append(allErrs, field.Invalid(fldPath, templateName, msg))
		return allErrs
	}

	for _, metric := range resolvedMetrics {
		effectiveCount := metric.EffectiveCount()
		if effectiveCount == nil {
			msg := fmt.Sprintf("AnalysisTemplate %s has metric %s which runs indefinitely. Invalid value for count: %s", templateName, metric.Name, metric.Count)
			allErrs = append(allErrs, field.Invalid(fldPath, templateName, msg))
		}
	}

	return allErrs
}
