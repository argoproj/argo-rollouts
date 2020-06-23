package validation

import (
	"fmt"
	"github.com/argoproj/argo-rollouts/utils/defaults"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	unversionedvalidation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/kubernetes/pkg/apis/apps/validation"
	apivalidation "k8s.io/kubernetes/pkg/apis/core/validation"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	//"github.com/argoproj/argo-rollouts/utils/defaults"
)

const (
	// Validate Spec constants

	// InvalidSpecReason indicates that the spec is invalid
	InvalidSpecReason = "InvalidSpec"
	// MissingFieldMessage the message to indicate rollout is missing a field
	MissingFieldMessage = "Rollout has missing field '%s'"
	// RolloutSelectAllMessage the message to indicate that the rollout has an empty selector
	RolloutSelectAllMessage = "This rollout is selecting all pods. A non-empty selector is required."
	// InvalidSetWeightMessage indicates the setweight value needs to be between 0 and 100
	InvalidSetWeightMessage = "SetWeight needs to be between 0 and 100"
	// InvalidDurationMessage indicates the Duration value needs to be greater than 0
	InvalidDurationMessage = "Duration needs to be greater than 0"
	// InvalidMaxSurgeMaxUnavailable indicates both maxSurge and MaxUnavailable can not be set to zero
	InvalidMaxSurgeMaxUnavailable = "MaxSurge and MaxUnavailable both can not be zero"
	// InvalidStepMessage indicates that a step must have either setWeight or pause set
	InvalidStepMessage = "Step must have one of the following set: experiment, setWeight, or pause"
	// ScaleDownDelayLongerThanDeadlineMessage indicates the ScaleDownDelaySeconds is longer than ProgressDeadlineSeconds
	ScaleDownDelayLongerThanDeadlineMessage = "ScaleDownDelaySeconds cannot be longer than ProgressDeadlineSeconds"
	// RolloutMinReadyLongerThanDeadlineMessage indicates the MinReadySeconds is longer than ProgressDeadlineSeconds
	RolloutMinReadyLongerThanDeadlineMessage = "MinReadySeconds cannot be longer than ProgressDeadlineSeconds"
	// InvalidStrategyMessage indiciates that multiple strategies can not be listed
	InvalidStrategyMessage = "Multiple Strategies can not be listed"
	// DuplicatedServicesMessage the message to indicate that the rollout uses the same service for the active and preview services
	DuplicatedServicesMessage = "This rollout uses the same service for the active and preview services, but two different services are required."
	// ScaleDownLimitLargerThanRevisionLimit the message to indicate that the rollout's revision history limit can not be smaller than the rollout's scale down limit
	ScaleDownLimitLargerThanRevisionLimit = "This rollout's revision history limit can not be smaller than the rollout's scale down limit"
	// AvailableReason the reason to indicate that the rollout is serving traffic from the active service
	AvailableReason = "AvailableReason"
	// NotAvailableMessage the message to indicate that the Rollout does not have min availability
	NotAvailableMessage = "Rollout does not have minimum availability"
	// AvailableMessage the message to indicate that the Rollout does have min availability
	AvailableMessage = "Rollout has minimum availability"
)

// called in strategy.go -> create.go
func ValidateRollout(ro *v1alpha1.Rollout) field.ErrorList {
	error := ValidateRolloutSpec(&ro.Spec, field.NewPath("spec"))
	return error
}

// ValidateRolloutSpec Checks for a valid spec otherwise returns a invalidSpec condition.
// TODO: don't use prevCond > syncHandler needs to take care of prevCond formatting
func ValidateRolloutSpec(spec *v1alpha1.RolloutSpec, fldPath *field.Path) field.ErrorList {//*v1alpha1.RolloutCondition {
	// ValidatePodTemplateSpec for `spec.template`
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, apivalidation.ValidateNonnegativeField(int64(*spec.Replicas), fldPath.Child("replicas"))...)

	if spec.Selector == nil {
		allErrs = append(allErrs, field.Required(fldPath.Child("selector"), ""))
	} else {
		allErrs = append(allErrs, unversionedvalidation.ValidateLabelSelector(spec.Selector, fldPath.Child("selector"))...)
		if len(spec.Selector.MatchLabels)+len(spec.Selector.MatchExpressions) == 0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("selector"), spec.Selector, "empty selector is invalid for deployment"))
		}
	}

	selector, err := metav1.LabelSelectorAsSelector(spec.Selector)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("selector"), spec.Selector, "invalid label selector"))
	} else {
		allErrs = append(allErrs, validation.ValidatePodTemplateSpecForReplicaSet(&spec.Template, selector, *spec.Replicas, fldPath.Child("template"))...)
	}

	allErrs = append(allErrs, apivalidation.ValidateNonnegativeField(int64(spec.MinReadySeconds), fldPath.Child("minReadySeconds"))...)
	if spec.RevisionHistoryLimit != nil {
		// zero is a valid RevisionHistoryLimit
		allErrs = append(allErrs, apivalidation.ValidateNonnegativeField(int64(*spec.RevisionHistoryLimit), fldPath.Child("revisionHistoryLimit"))...)
	}
	if spec.ProgressDeadlineSeconds != nil {
		allErrs = append(allErrs, apivalidation.ValidateNonnegativeField(int64(*spec.ProgressDeadlineSeconds), fldPath.Child("progressDeadlineSeconds"))...)
		if *spec.ProgressDeadlineSeconds <= spec.MinReadySeconds {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("progressDeadlineSeconds"), spec.ProgressDeadlineSeconds, "must be greater than minReadySeconds"))
		}
	}

	// TODO: Check
	//if rollout.Spec.MinReadySeconds > defaults.GetProgressDeadlineSecondsOrDefault(rollout) {
	//	return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, RolloutMinReadyLongerThanDeadlineMessage)
	//}

	allErrs = append(allErrs, ValidateRolloutStrategy(spec.Strategy, fldPath.Child("strategy"))...)

	return allErrs
}

func ValidateRolloutStrategy(strategy v1alpha1.RolloutStrategy, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if strategy.BlueGreen == nil && strategy.Canary == nil {

	}
	if strategy.BlueGreen != nil && strategy.Canary != nil {

	}
	if strategy.BlueGreen != nil {
		allErrs = append(allErrs, ValidateRolloutStrategyBlueGreen(strategy.BlueGreen)...)
	}
	if strategy.Canary != nil {

	}
	//if rollout.Spec.Strategy.Canary == nil && rollout.Spec.Strategy.BlueGreen == nil {
	//	message := fmt.Sprintf(MissingFieldMessage, ".Spec.Strategy.Canary or .Spec.Strategy.BlueGreen")
	//	return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, message)
	//}
	//
	//if rollout.Spec.Strategy.Canary != nil && rollout.Spec.Strategy.BlueGreen != nil {
	//	return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, InvalidStrategyMessage)
	//}
	//
	//if rollout.Spec.Strategy.BlueGreen != nil {
	//	allErrs = append(allErrs, ValidateRolloutStrategyBlueGreen(rollout)...)
	//}
	//
	//if rollout.Spec.Strategy.Canary != nil {
	//	allErrs = append(allErrs, ValidateRolloutStrategyCanary(rollout)...)
	//}
}

//func ValidateRolloutStrategyBlueGreen(blueGreen *v1alpha1.BlueGreenStrategy) field.ErrorList {
//	allErrs := field.ErrorList{}
//	if blueGreen.ActiveService == blueGreen.PreviewService {
//		return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, DuplicatedServicesMessage)
//	}
//	revisionHistoryLimit := defaults.GetRevisionHistoryLimitOrDefault(rollout)
//	if blueGreen.ScaleDownDelayRevisionLimit != nil && revisionHistoryLimit < *blueGreen.ScaleDownDelayRevisionLimit {
//		return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, ScaleDownLimitLargerThanRevisionLimit)
//	}
//	if blueGreen.AntiAffinity != nil {
//		reason, message := invalidAntiAffinity(*blueGreen.AntiAffinity, "BlueGreen")
//		if reason != "" {
//			return newInvalidSpecRolloutCondition(prevCond, reason, message)
//		}
//	}
//	return allErrs
//}
//
//
//func ValidateRolloutStrategyCanary(rollout *v1alpha1.CanaryStrategy) field.ErrorList {
//	allErrs := field.ErrorList{}
//	if invalidMaxSurgeMaxUnavailable(rollout) {
//		return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, InvalidMaxSurgeMaxUnavailable)
//	}
//	for _, step := range rollout.Spec.Strategy.Canary.Steps {
//		if hasMultipleStepsType(step) {
//			return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, InvalidStepMessage)
//		}
//		if step.Experiment == nil && step.Pause == nil && step.SetWeight == nil && step.Analysis == nil {
//			return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, InvalidStepMessage)
//		}
//		if step.SetWeight != nil && (*step.SetWeight < 0 || *step.SetWeight > 100) {
//			return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, InvalidSetWeightMessage)
//		}
//		if step.Pause != nil && step.Pause.DurationSeconds() < 0 {
//			return newInvalidSpecRolloutCondition(prevCond, InvalidSpecReason, InvalidDurationMessage)
//		}
//	}
//	if rollout.Spec.Strategy.Canary.AntiAffinity != nil {
//		reason, message := invalidAntiAffinity(*rollout.Spec.Strategy.Canary.AntiAffinity, "Canary")
//		if reason != "" {
//			return newInvalidSpecRolloutCondition(prevCond, reason, message)
//		}
//	}
//	return allErrs
//}
//
//func invalidAntiAffinity(affinity v1alpha1.AntiAffinity, strategy string) (string, string) {
//	if affinity.PreferredDuringSchedulingIgnoredDuringExecution == nil && affinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
//		return InvalidSpecReason, fmt.Sprintf(MissingFieldMessage, fmt.Sprintf(".Spec.Strategy.%[1]s.AntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution or .Spec.Strategy.%[1]s.AntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution", strategy))
//	}
//	if affinity.PreferredDuringSchedulingIgnoredDuringExecution != nil && affinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
//		return InvalidSpecReason, "Multiple Anti-Affinity Strategies can not be listed"
//	}
//	return "", ""
//}
