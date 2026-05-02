package validation

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const (
	// RolloutPluginInvalidStepMessage indicates that a RolloutPlugin step must have exactly one of setWeight, pause, or analysis
	RolloutPluginInvalidStepMessage = "Step must have exactly one of: setWeight, pause, or analysis"
)

// ValidateRolloutPlugin validates the RolloutPlugin spec.
// Returns the first error message, or empty string if valid.
func ValidateRolloutPlugin(rp *v1alpha1.RolloutPlugin) string {
	spec := rp.Spec

	if spec.WorkloadRef.Name == "" {
		return "RolloutPlugin spec.workloadRef.name is required"
	}
	if spec.WorkloadRef.Kind == "" {
		return "RolloutPlugin spec.workloadRef.kind is required"
	}
	if spec.WorkloadRef.APIVersion == "" {
		return "RolloutPlugin spec.workloadRef.apiVersion is required"
	}

	if spec.Plugin.Name == "" {
		return "RolloutPlugin spec.plugin.name is required"
	}

	if spec.Strategy.Canary == nil {
		return "RolloutPlugin spec.strategy.canary is required"
	}

	if spec.ProgressDeadlineSeconds != nil && *spec.ProgressDeadlineSeconds <= 0 {
		return "RolloutPlugin spec.progressDeadlineSeconds must be greater than 0"
	}

	// Validate canary steps
	errs := ValidateRolloutPluginStrategyCanary(rp, field.NewPath("spec", "strategy", "canary"))
	if len(errs) > 0 {
		return errs[0].Error()
	}

	return ""
}

// ValidateRolloutPluginStrategyCanary validates canary strategy steps for RolloutPlugin.
// RolloutPlugin only supports setWeight, pause, and analysis step types.
func ValidateRolloutPluginStrategyCanary(rp *v1alpha1.RolloutPlugin, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	canary := rp.Spec.Strategy.Canary
	if canary == nil {
		return allErrs
	}

	if len(canary.Steps) == 0 {
		return allErrs
	}

	const maxTrafficWeight int32 = 100

	for i, step := range canary.Steps {
		stepFldPath := fldPath.Child("steps").Index(i)

		stepTypeCount := 0
		if step.SetWeight != nil {
			stepTypeCount++
		}
		if step.Pause != nil {
			stepTypeCount++
		}
		if step.Analysis != nil {
			stepTypeCount++
		}

		// Must have exactly one supported step type
		if stepTypeCount == 0 {
			allErrs = append(allErrs, field.Invalid(stepFldPath, fmt.Sprintf("step %d", i), RolloutPluginInvalidStepMessage))
		} else if stepTypeCount > 1 {
			allErrs = append(allErrs, field.Invalid(stepFldPath, fmt.Sprintf("step %d", i), RolloutPluginInvalidStepMessage))
		}

		if step.SetWeight != nil && (*step.SetWeight < 0 || *step.SetWeight > maxTrafficWeight) {
			allErrs = append(allErrs, field.Invalid(stepFldPath.Child("setWeight"), *step.SetWeight, fmt.Sprintf(InvalidSetWeightMessage, maxTrafficWeight)))
		}
		if step.Pause != nil && step.Pause.DurationSeconds() < 0 {
			allErrs = append(allErrs, field.Invalid(stepFldPath.Child("pause").Child("duration"), step.Pause.DurationSeconds(), InvalidDurationMessage))
		}
	}

	return allErrs
}
