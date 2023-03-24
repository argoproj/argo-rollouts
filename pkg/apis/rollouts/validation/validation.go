package validation

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	unversionedvalidation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/intstr"
	validationutil "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/kubernetes/pkg/apis/apps/validation"
	"k8s.io/kubernetes/pkg/apis/core"
	corev1defaults "k8s.io/kubernetes/pkg/apis/core/v1"
	apivalidation "k8s.io/kubernetes/pkg/apis/core/validation"
	"k8s.io/kubernetes/pkg/fieldpath"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
)

const (
	// Validate Spec constants

	// MissingFieldMessage the message to indicate rollout is missing a field
	MissingFieldMessage = "Rollout has missing field '%s'"
	// InvalidSetWeightMessage indicates the setweight value needs to be between 0 and 100
	InvalidSetWeightMessage = "SetWeight needs to be between 0 and 100"
	// InvalidCanaryExperimentTemplateWeightWithoutTrafficRouting indicates experiment weight cannot be set without trafficRouting
	InvalidCanaryExperimentTemplateWeightWithoutTrafficRouting = "Experiment template weight cannot be set unless TrafficRouting is enabled"
	// InvalidSetCanaryScaleTrafficPolicy indicates that TrafficRouting, required for SetCanaryScale, is missing
	InvalidSetCanaryScaleTrafficPolicy = "SetCanaryScale requires TrafficRouting to be set"
	// InvalidSetHeaderRouteTrafficPolicy indicates that TrafficRouting required for SetHeaderRoute is missing
	InvalidSetHeaderRouteTrafficPolicy = "SetHeaderRoute requires TrafficRouting, supports Istio and ALB and Apisix"
	// InvalidSetMirrorRouteTrafficPolicy indicates that TrafficRouting, required for SetCanaryScale, is missing
	InvalidSetMirrorRouteTrafficPolicy = "SetMirrorRoute requires TrafficRouting, supports Istio only"
	// InvalidStringMatchMultipleValuePolicy indicates that SetCanaryScale, has multiple values set
	InvalidStringMatchMultipleValuePolicy = "StringMatch match value must have exactly one of the following: exact, regex, prefix"
	// InvalidStringMatchMissedValuePolicy indicates that SetCanaryScale, has multiple values set
	InvalidStringMatchMissedValuePolicy = "StringMatch value missed, match value must have one of the following: exact, regex, prefix"
	// InvalidSetHeaderRouteALBValuePolicy indicates that SetHeaderRouting using with ALB missed the 'exact' value
	InvalidSetHeaderRouteALBValuePolicy = "SetHeaderRoute match value invalid. ALB supports 'exact' value only"
	// InvalidDurationMessage indicates the Duration value needs to be greater than 0
	InvalidDurationMessage = "Duration needs to be greater than 0"
	// InvalidMaxSurgeMaxUnavailable indicates both maxSurge and MaxUnavailable can not be set to zero
	InvalidMaxSurgeMaxUnavailable = "MaxSurge and MaxUnavailable both can not be zero"
	// InvalidStepMessage indicates that a step must have either setWeight or pause set
	InvalidStepMessage = "Step must have one of the following set: experiment, setWeight, setCanaryScale or pause"
	// InvalidStrategyMessage indicates that multiple strategies can not be listed
	InvalidStrategyMessage = "Multiple Strategies can not be listed"
	// DuplicatedServicesBlueGreenMessage the message to indicate that the rollout uses the same service for the active and preview services
	DuplicatedServicesBlueGreenMessage = "This rollout uses the same service for the active and preview services, but two different services are required."
	// DuplicatedServicesCanaryMessage indicates that the rollout uses the same service for the stable and canary services
	DuplicatedServicesCanaryMessage = "This rollout uses the same service for the stable and canary services, but two different services are required."
	// InvalidAntiAffinityStrategyMessage indicates that Anti-Affinity can only have one strategy listed
	InvalidAntiAffinityStrategyMessage = "AntiAffinity must have exactly one strategy listed"
	// InvalidAntiAffinityWeightMessage indicates that Anti-Affinity must have weight between 1-100
	InvalidAntiAffinityWeightMessage = "AntiAffinity weight must be between 1-100"
	// ScaleDownLimitLargerThanRevisionLimit the message to indicate that the rollout's revision history limit can not be smaller than the rollout's scale down limit
	ScaleDownLimitLargerThanRevisionLimit = "This rollout's revision history limit can not be smaller than the rollout's scale down limit"
	// InvalidTrafficRoutingMessage indicates that both canary and stable service must be set to use Traffic Routing
	InvalidTrafficRoutingMessage = "Canary service and Stable service must to be set to use Traffic Routing"
	// InvalidAnalysisArgsMessage indicates that arguments provided in analysis steps are refrencing un-supported metadatafield.
	//supported fields are "metadata.annotations", "metadata.labels", "metadata.name", "metadata.namespace", "metadata.uid"
	InvalidAnalysisArgsMessage = "Analyses arguments must refer to valid object metadata supported by downwardAPI"
	// InvalidCanaryScaleDownDelay indicates that canary.scaleDownDelaySeconds cannot be used
	InvalidCanaryScaleDownDelay = "Canary scaleDownDelaySeconds can only be used with traffic routing"
	// InvalidCanaryDynamicStableScale indicates that canary.dynamicStableScale cannot be used
	InvalidCanaryDynamicStableScale = "Canary dynamicStableScale can only be used with traffic routing"
	// InvalidCanaryDynamicStableScaleWithScaleDownDelay indicates that canary.dynamicStableScale cannot be used with scaleDownDelaySeconds
	InvalidCanaryDynamicStableScaleWithScaleDownDelay = "Canary dynamicStableScale cannot be used with scaleDownDelaySeconds"
	// InvalidPingPongProvidedMessage indicates that both ping and pong service must be set to use Ping-Pong feature
	InvalidPingPongProvidedMessage = "Ping service and Pong service must to be set to use Ping-Pong feature"
	// DuplicatedPingPongServicesMessage indicates that the rollout uses the same service for the ping and pong services
	DuplicatedPingPongServicesMessage = "This rollout uses the same service for the ping and pong services, but two different services are required."
	// MissedAlbRootServiceMessage indicates that the rollout with ALB TrafficRouting and ping pong feature enabled must have root service provided
	MissedAlbRootServiceMessage = "Root service field is required for the configuration with ALB and ping-pong feature enabled"
	// PingPongWithAlbOnlyMessage At this moment ping-pong feature works with the ALB traffic routing only
	PingPongWithAlbOnlyMessage = "Ping-pong feature works with the ALB traffic routing only"
	// InvalideStepRouteNameNotFoundInManagedRoutes A step has been configured that requires managedRoutes and the route name
	// is missing from managedRoutes
	InvalideStepRouteNameNotFoundInManagedRoutes = "Steps define a route that does not exist in spec.strategy.canary.trafficRouting.managedRoutes"
)

// allowAllPodValidationOptions allows all pod options to be true for the purposes of rollout pod
// spec validation. We allow everything because we don't know what is truly allowed in the cluster
// and rely on ReplicaSet/Pod creation to enforce if these options are truly allowed.
// NOTE: this variable may need to be updated whenever we update our k8s libraries as new options
// are introduced or removed.
var allowAllPodValidationOptions = apivalidation.PodValidationOptions{
	AllowDownwardAPIHugePages:       true,
	AllowInvalidPodDeletionCost:     true,
	AllowIndivisibleHugePagesValues: true,
	AllowWindowsHostProcessField:    true,
	AllowExpandedDNSConfig:          true,
}

func ValidateRollout(rollout *v1alpha1.Rollout) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, ValidateRolloutSpec(rollout, field.NewPath("spec"))...)
	return allErrs
}

// ValidateRolloutSpec checks for a valid spec otherwise returns a list of errors.
func ValidateRolloutSpec(rollout *v1alpha1.Rollout, fldPath *field.Path) field.ErrorList {
	spec := rollout.Spec
	allErrs := field.ErrorList{}

	replicas := defaults.GetReplicasOrDefault(spec.Replicas)
	allErrs = append(allErrs, apivalidation.ValidateNonnegativeField(int64(replicas), fldPath.Child("replicas"))...)

	if spec.WorkloadRef == nil {
		if spec.Selector == nil {
			message := fmt.Sprintf(MissingFieldMessage, ".spec.selector")
			allErrs = append(allErrs, field.Required(fldPath.Child("selector"), message))
		} else {
			allErrs = append(allErrs, unversionedvalidation.ValidateLabelSelector(spec.Selector, fldPath.Child("selector"))...)
			if len(spec.Selector.MatchLabels)+len(spec.Selector.MatchExpressions) == 0 {
				allErrs = append(allErrs, field.Invalid(fldPath.Child("selector"), spec.Selector, "empty selector is invalid for deployment"))
			}
		}
	}

	if !rollout.Spec.TemplateResolvedFromRef && (spec.WorkloadRef != nil && !spec.EmptyTemplate()) {
		// WorkloadRef and template can not be set at the same time for lint plugin
		// During reconciliation, TemplateResolvedFromRef is true and will not reach here
		allErrs = append(allErrs, field.InternalError(fldPath.Child("template"),
			fmt.Errorf("template must be empty for workload reference rollout")))
	}

	selector, err := metav1.LabelSelectorAsSelector(spec.Selector)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("selector"), spec.Selector, "invalid label selector"))
	} else {
		// The upstream K8s validation we are using expects the default values of a PodSpec to be set otherwise throwing a validation error.
		// However, the Rollout does not need to have them set since the ReplicaSet it creates will have the default values set.
		// As a result, the controller sets the default values before validation to prevent the validation errors due to the lack of these default fields. See #576 for more info.
		podTemplate := corev1.PodTemplate{
			Template: *spec.Template.DeepCopy(),
		}
		corev1defaults.SetObjectDefaults_PodTemplate(&podTemplate)
		templateCoreV1 := podTemplate.Template
		// ValidatePodTemplateSpecForReplicaSet function requires PodTemplateSpec from "k8s.io/api/core".
		// We must cast spec.Template from "k8s.io/api/core/v1" to "k8s.io/api/core" in order to use ValidatePodTemplateSpecForReplicaSet.
		data, structConvertErr := json.Marshal(&templateCoreV1)
		if structConvertErr != nil {
			allErrs = append(allErrs, field.InternalError(fldPath.Child("template"), structConvertErr))
			return allErrs
		}
		var template core.PodTemplateSpec
		structConvertErr = json.Unmarshal(data, &template)
		if structConvertErr != nil {
			allErrs = append(allErrs, field.InternalError(fldPath.Child("template"), structConvertErr))
			return allErrs
		}
		template.ObjectMeta = spec.Template.ObjectMeta
		removeSecurityContextPrivileged(&template)

		// Skip validating empty template for rollout resolved from ref
		if rollout.Spec.TemplateResolvedFromRef || spec.WorkloadRef == nil {
			allErrs = append(allErrs, validation.ValidatePodTemplateSpecForReplicaSet(&template, selector, replicas, fldPath.Child("template"), allowAllPodValidationOptions)...)
		}
	}
	allErrs = append(allErrs, apivalidation.ValidateNonnegativeField(int64(spec.MinReadySeconds), fldPath.Child("minReadySeconds"))...)

	revisionHistoryLimit := defaults.GetRevisionHistoryLimitOrDefault(rollout)
	allErrs = append(allErrs, apivalidation.ValidateNonnegativeField(int64(revisionHistoryLimit), fldPath.Child("revisionHistoryLimit"))...)

	progressDeadlineSeconds := defaults.GetProgressDeadlineSecondsOrDefault(rollout)
	allErrs = append(allErrs, apivalidation.ValidateNonnegativeField(int64(progressDeadlineSeconds), fldPath.Child("progressDeadlineSeconds"))...)
	if progressDeadlineSeconds <= spec.MinReadySeconds {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("progressDeadlineSeconds"), progressDeadlineSeconds, "must be greater than minReadySeconds"))
	}

	allErrs = append(allErrs, ValidateRolloutStrategy(rollout, fldPath.Child("strategy"))...)

	return allErrs
}

// removeSecurityContextPrivileged removes the privileged value on containers for the purposes of
// validation. This is necessary because the k8s ValidateSecurityContext library which we reuse,
// calls k8s.io/kubernetes/pkg/capabilities.Get(), which determines the security capabilities at a
// global level. We don't want to call capabilities.Setup(), because it affects it as a global
// level, so instead we remove the privileged setting on any containers so validation ignores it.
// See https://github.com/argoproj/argo-rollouts/issues/796
func removeSecurityContextPrivileged(template *core.PodTemplateSpec) {
	for _, ctrList := range [][]core.Container{template.Spec.Containers, template.Spec.InitContainers} {
		for i, ctr := range ctrList {
			if ctr.SecurityContext != nil && ctr.SecurityContext.Privileged != nil && *ctr.SecurityContext.Privileged {
				ctr.SecurityContext.Privileged = nil
				ctrList[i] = ctr
			}
		}
	}
}

func ValidateRolloutStrategy(rollout *v1alpha1.Rollout, fldPath *field.Path) field.ErrorList {
	strategy := rollout.Spec.Strategy
	allErrs := field.ErrorList{}
	if strategy.BlueGreen == nil && strategy.Canary == nil {
		message := fmt.Sprintf(MissingFieldMessage, ".spec.strategy.canary or .spec.strategy.blueGreen")
		allErrs = append(allErrs, field.Required(fldPath.Child("strategy"), message))
	} else if strategy.BlueGreen != nil && strategy.Canary != nil {
		errVal := fmt.Sprintf("blueGreen: %t canary: %t", rollout.Spec.Strategy.BlueGreen != nil, rollout.Spec.Strategy.Canary != nil)
		allErrs = append(allErrs, field.Invalid(fldPath.Child("strategy"), errVal, InvalidStrategyMessage))
	} else if strategy.BlueGreen != nil {
		allErrs = append(allErrs, ValidateRolloutStrategyBlueGreen(rollout, fldPath)...)
	} else if strategy.Canary != nil {
		allErrs = append(allErrs, ValidateRolloutStrategyCanary(rollout, fldPath)...)
	}
	return allErrs
}

func ValidateRolloutStrategyBlueGreen(rollout *v1alpha1.Rollout, fldPath *field.Path) field.ErrorList {
	blueGreen := rollout.Spec.Strategy.BlueGreen
	allErrs := field.ErrorList{}
	if blueGreen.ActiveService == blueGreen.PreviewService {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("activeService"), blueGreen.ActiveService, DuplicatedServicesBlueGreenMessage))
	}
	revisionHistoryLimit := defaults.GetRevisionHistoryLimitOrDefault(rollout)
	if blueGreen.ScaleDownDelayRevisionLimit != nil && revisionHistoryLimit < *blueGreen.ScaleDownDelayRevisionLimit {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("scaleDownDelayRevisionLimit"), *blueGreen.ScaleDownDelayRevisionLimit, ScaleDownLimitLargerThanRevisionLimit))
	}
	allErrs = append(allErrs, ValidateRolloutStrategyAntiAffinity(blueGreen.AntiAffinity, fldPath.Child("antiAffinity"))...)
	return allErrs
}

// requireCanaryStableServices returns true if the rollout requires canary.stableService and
// canary.canaryService to be defined
func requireCanaryStableServices(rollout *v1alpha1.Rollout) bool {
	canary := rollout.Spec.Strategy.Canary
	if canary.TrafficRouting == nil || (canary.TrafficRouting.Istio != nil && canary.TrafficRouting.Istio.DestinationRule != nil) || (canary.PingPong != nil) {
		return false
	}
	return true
}

func ValidateRolloutStrategyCanary(rollout *v1alpha1.Rollout, fldPath *field.Path) field.ErrorList {
	canary := rollout.Spec.Strategy.Canary
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, invalidMaxSurgeMaxUnavailable(rollout, fldPath.Child("maxSurge"))...)
	if canary.CanaryService != "" && canary.StableService != "" && canary.CanaryService == canary.StableService {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("stableService"), canary.StableService, DuplicatedServicesCanaryMessage))
	}
	if canary.PingPong != nil {
		if canary.TrafficRouting != nil && canary.TrafficRouting.ALB == nil {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("trafficRouting").Child("alb"), canary.TrafficRouting.ALB, PingPongWithAlbOnlyMessage))
		}
		if canary.PingPong.PingService == "" {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("pingPong").Child("pingService"), canary.PingPong.PingService, InvalidPingPongProvidedMessage))
		}
		if canary.PingPong.PongService == "" {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("pingPong").Child("pongService"), canary.PingPong.PongService, InvalidPingPongProvidedMessage))
		}
		if canary.PingPong.PingService == canary.PingPong.PongService {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("pingPong").Child("pingService"), canary.PingPong.PingService, DuplicatedPingPongServicesMessage))
		}
		if canary.TrafficRouting != nil && canary.TrafficRouting.ALB != nil && canary.TrafficRouting.ALB.RootService == "" {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("trafficRouting").Child("alb").Child("rootService"), canary.TrafficRouting.ALB.RootService, MissedAlbRootServiceMessage))
		}
	}
	if requireCanaryStableServices(rollout) {
		if canary.StableService == "" {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("stableService"), canary.StableService, InvalidTrafficRoutingMessage))
		}
		if canary.CanaryService == "" {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("canaryService"), canary.CanaryService, InvalidTrafficRoutingMessage))
		}
	}

	if canary.TrafficRouting == nil {
		if canary.ScaleDownDelaySeconds != nil {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("scaleDownDelaySeconds"), *canary.ScaleDownDelaySeconds, InvalidCanaryScaleDownDelay))
		}
		if canary.DynamicStableScale {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("dynamicStableScale"), canary.DynamicStableScale, InvalidCanaryDynamicStableScale))
		}
	} else {
		if canary.ScaleDownDelaySeconds != nil && canary.DynamicStableScale {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("dynamicStableScale"), canary.DynamicStableScale, InvalidCanaryDynamicStableScaleWithScaleDownDelay))
		}
	}

	for i, step := range canary.Steps {
		stepFldPath := fldPath.Child("steps").Index(i)
		allErrs = append(allErrs, hasMultipleStepsType(step, stepFldPath)...)
		if step.Experiment == nil && step.Pause == nil && step.SetWeight == nil && step.Analysis == nil && step.SetCanaryScale == nil &&
			step.SetHeaderRoute == nil && step.SetMirrorRoute == nil {
			errVal := fmt.Sprintf("step.Experiment: %t step.Pause: %t step.SetWeight: %t step.Analysis: %t step.SetCanaryScale: %t step.SetHeaderRoute: %t step.SetMirrorRoutes: %t",
				step.Experiment == nil, step.Pause == nil, step.SetWeight == nil, step.Analysis == nil, step.SetCanaryScale == nil, step.SetHeaderRoute == nil, step.SetMirrorRoute == nil)
			allErrs = append(allErrs, field.Invalid(stepFldPath, errVal, InvalidStepMessage))
		}
		if step.SetWeight != nil && (*step.SetWeight < 0 || *step.SetWeight > 100) {
			allErrs = append(allErrs, field.Invalid(stepFldPath.Child("setWeight"), *canary.Steps[i].SetWeight, InvalidSetWeightMessage))
		}
		if step.Pause != nil && step.Pause.DurationSeconds() < 0 {
			allErrs = append(allErrs, field.Invalid(stepFldPath.Child("pause").Child("duration"), step.Pause.DurationSeconds(), InvalidDurationMessage))
		}
		if step.SetCanaryScale != nil && canary.TrafficRouting == nil {
			allErrs = append(allErrs, field.Required(fldPath.Child("trafficRouting"), InvalidSetCanaryScaleTrafficPolicy))
		}

		if step.SetHeaderRoute != nil {
			trafficRouting := rollout.Spec.Strategy.Canary.TrafficRouting
			if trafficRouting == nil || (trafficRouting.Istio == nil && trafficRouting.ALB == nil && trafficRouting.Apisix == nil) {
				allErrs = append(allErrs, field.Invalid(stepFldPath.Child("setHeaderRoute"), step.SetHeaderRoute, InvalidSetHeaderRouteTrafficPolicy))
			} else if step.SetHeaderRoute.Match != nil && len(step.SetHeaderRoute.Match) > 0 {
				for j, match := range step.SetHeaderRoute.Match {
					if trafficRouting.ALB != nil {
						matchFld := stepFldPath.Child("setHeaderRoute").Child("match").Index(j)
						allErrs = append(allErrs, hasALBInvalidValues(match.HeaderValue, matchFld)...)
					} else {
						matchFld := stepFldPath.Child("setHeaderRoute").Child("match").Index(j)
						allErrs = append(allErrs, hasMultipleMatchValues(match.HeaderValue, matchFld)...)
					}
				}
			}
		}

		if step.SetMirrorRoute != nil {
			trafficRouting := rollout.Spec.Strategy.Canary.TrafficRouting
			if trafficRouting == nil || trafficRouting.Istio == nil {
				allErrs = append(allErrs, field.Invalid(stepFldPath.Child("setMirrorRoute"), step.SetMirrorRoute, "SetMirrorRoute requires TrafficRouting, supports Istio only"))
			}
			if step.SetMirrorRoute.Match != nil && len(step.SetMirrorRoute.Match) > 0 {
				for j, match := range step.SetMirrorRoute.Match {
					matchFld := stepFldPath.Child("setMirrorRoute").Child("match").Index(j)
					if match.Method != nil {
						allErrs = append(allErrs, hasMultipleMatchValues(match.Method, matchFld)...)
					}
					if match.Path != nil {
						allErrs = append(allErrs, hasMultipleMatchValues(match.Path, matchFld)...)
					}
					if match.Method != nil {
						allErrs = append(allErrs, hasMultipleMatchValues(match.Method, matchFld)...)
					}
				}
			}
		}

		if rollout.Spec.Strategy.Canary.TrafficRouting != nil {
			if step.SetHeaderRoute != nil || step.SetMirrorRoute != nil {
				if rollout.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes == nil {
					message := fmt.Sprintf(MissingFieldMessage, "spec.strategy.canary.trafficRouting.managedRoutes")
					allErrs = append(allErrs, field.Required(fldPath.Child("trafficRouting", "managedRoutes"), message))
				}
			}
		}
		if rollout.Spec.Strategy.Canary.TrafficRouting != nil && rollout.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes != nil {
			if step.SetHeaderRoute != nil {
				allErrs = append(allErrs, ValidateStepRouteFoundInManagedRoute(stepFldPath.Child("setHeaderRoute"), step.SetHeaderRoute.Name, rollout.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes)...)
			}
			if step.SetMirrorRoute != nil {
				allErrs = append(allErrs, ValidateStepRouteFoundInManagedRoute(stepFldPath.Child("setMirrorRoute"), step.SetMirrorRoute.Name, rollout.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes)...)
			}
		}

		analysisRunArgs := make([]v1alpha1.AnalysisRunArgument, 0)
		if step.Experiment != nil {
			for tmplIndex, template := range step.Experiment.Templates {
				if template.Weight != nil {
					if canary.TrafficRouting == nil {
						allErrs = append(allErrs, field.Invalid(stepFldPath.Child("experiment").Child("templates").Index(tmplIndex).Child("weight"), *canary.Steps[i].Experiment.Templates[tmplIndex].Weight, InvalidCanaryExperimentTemplateWeightWithoutTrafficRouting))
					} else if canary.TrafficRouting.ALB == nil && canary.TrafficRouting.SMI == nil && canary.TrafficRouting.Istio == nil {
						allErrs = append(allErrs, field.Invalid(stepFldPath.Child("experiment").Child("templates").Index(tmplIndex).Child("weight"), *canary.Steps[i].Experiment.Templates[tmplIndex].Weight, "Experiment template weight is only available for TrafficRouting with SMI, ALB, and Istio at this time"))
					}
				}
			}
			for _, analysis := range step.Experiment.Analyses {
				for _, arg := range analysis.Args {
					analysisRunArgs = append(analysisRunArgs, arg)
				}
			}
		}
		if step.Analysis != nil {
			for _, arg := range step.Analysis.Args {
				analysisRunArgs = append(analysisRunArgs, arg)
			}
		}

		for _, arg := range analysisRunArgs {
			if arg.ValueFrom != nil {
				if arg.ValueFrom.FieldRef != nil && strings.HasPrefix(arg.ValueFrom.FieldRef.FieldPath, "metadata") {
					_, err := fieldpath.ExtractFieldPathAsString(rollout, arg.ValueFrom.FieldRef.FieldPath)
					if err != nil {
						allErrs = append(allErrs, field.Invalid(stepFldPath.Child("analyses"), analysisRunArgs, InvalidAnalysisArgsMessage))
					}
				}
			}
		}

	}
	allErrs = append(allErrs, ValidateRolloutStrategyAntiAffinity(canary.AntiAffinity, fldPath.Child("antiAffinity"))...)
	return allErrs
}

func ValidateStepRouteFoundInManagedRoute(stepFldPath *field.Path, stepRoutName string, roManagedRoutes []v1alpha1.MangedRoutes) field.ErrorList {
	allErrs := field.ErrorList{}
	found := false
	for _, managedRoute := range roManagedRoutes {
		if stepRoutName == managedRoute.Name {
			found = true
		}
	}
	if !found {
		allErrs = append(allErrs, field.Invalid(stepFldPath, stepRoutName, InvalideStepRouteNameNotFoundInManagedRoutes))
	}
	return allErrs
}

func ValidateRolloutStrategyAntiAffinity(antiAffinity *v1alpha1.AntiAffinity, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if antiAffinity != nil {
		preferred, required := antiAffinity.PreferredDuringSchedulingIgnoredDuringExecution, antiAffinity.RequiredDuringSchedulingIgnoredDuringExecution
		if (preferred == nil && required == nil) || (preferred != nil && required != nil) {
			errVal := fmt.Sprintf("antiAffinity.PreferredDuringSchedulingIgnoredDuringExecution: %t antiAffinity.RequiredDuringSchedulingIgnoredDuringExecution: %t", antiAffinity.PreferredDuringSchedulingIgnoredDuringExecution != nil, antiAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil)
			allErrs = append(allErrs, field.Invalid(fldPath, errVal, InvalidAntiAffinityStrategyMessage))
		}
		if preferred != nil && (preferred.Weight < 1 || preferred.Weight > 100) {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("weight"), preferred.Weight, InvalidAntiAffinityWeightMessage))
		}
	}
	return allErrs
}

func invalidMaxSurgeMaxUnavailable(rollout *v1alpha1.Rollout, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	maxSurge := defaults.GetMaxSurgeOrDefault(rollout)
	maxUnavailable := defaults.GetMaxUnavailableOrDefault(rollout)
	maxSurgeValue := getIntOrPercentValue(*maxSurge)
	maxUnavailableValue := getIntOrPercentValue(*maxUnavailable)
	if maxSurgeValue == 0 && maxUnavailableValue == 0 {
		allErrs = append(allErrs, field.Invalid(fldPath, *rollout.Spec.Strategy.Canary.MaxSurge, InvalidMaxSurgeMaxUnavailable))
	}
	return allErrs
}

func getPercentValue(intOrStringValue intstr.IntOrString) (int, bool) {
	if intOrStringValue.Type != intstr.String {
		return 0, false
	}
	if len(validationutil.IsValidPercent(intOrStringValue.StrVal)) != 0 {
		return 0, false
	}
	value, _ := strconv.Atoi(intOrStringValue.StrVal[:len(intOrStringValue.StrVal)-1])
	return value, true
}

func getIntOrPercentValue(intOrStringValue intstr.IntOrString) int {
	value, isPercent := getPercentValue(intOrStringValue)
	if isPercent {
		return value
	}
	return intOrStringValue.IntValue()
}

func hasMultipleStepsType(s v1alpha1.CanaryStep, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	oneOf := make([]bool, 3)
	oneOf = append(oneOf, s.SetWeight != nil)
	oneOf = append(oneOf, s.Pause != nil)
	oneOf = append(oneOf, s.Experiment != nil)
	oneOf = append(oneOf, s.Analysis != nil)
	hasMultipleStepTypes := false
	for i := range oneOf {
		if oneOf[i] {
			if hasMultipleStepTypes {
				errVal := fmt.Sprintf("step.Experiment: %t step.Pause: %t step.SetWeight: %t step.Analysis: %t", s.Experiment != nil, s.Pause != nil, s.SetWeight != nil, s.Analysis != nil)
				allErrs = append(allErrs, field.Invalid(fldPath, errVal, InvalidStepMessage))
				break
			}
			hasMultipleStepTypes = true
		}
	}
	return allErrs
}

func hasALBInvalidValues(match *v1alpha1.StringMatch, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if match == nil {
		e := field.Invalid(fldPath, match, InvalidStringMatchMissedValuePolicy)
		allErrs = append(allErrs, e)
		return allErrs
	}
	if match.Exact == "" || match.Regex != "" || match.Prefix != "" {
		return append(allErrs, field.Invalid(fldPath, match, InvalidSetHeaderRouteALBValuePolicy))
	}
	return allErrs
}

func hasMultipleMatchValues(match *v1alpha1.StringMatch, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if match == nil {
		e := field.Invalid(fldPath, match, InvalidStringMatchMissedValuePolicy)
		allErrs = append(allErrs, e)
		return allErrs
	}

	var oneOf []bool
	oneOf = append(oneOf, match.Exact != "")
	oneOf = append(oneOf, match.Regex != "")
	oneOf = append(oneOf, match.Prefix != "")
	hasValue := false
	for i := range oneOf {
		if oneOf[i] {
			if hasValue {
				e := field.Invalid(fldPath, match, InvalidStringMatchMultipleValuePolicy)
				allErrs = append(allErrs, e)
				break
			}
			hasValue = true
		}
	}
	if !hasValue {
		e := field.Invalid(fldPath, match, InvalidStringMatchMissedValuePolicy)
		allErrs = append(allErrs, e)
	}
	return allErrs
}
