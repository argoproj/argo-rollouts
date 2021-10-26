package validation

import (
	"fmt"

	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	serviceutil "github.com/argoproj/argo-rollouts/utils/service"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	corev1 "k8s.io/api/core/v1"
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

type AnalysisTemplatesWithType struct {
	AnalysisTemplates        []*v1alpha1.AnalysisTemplate
	ClusterAnalysisTemplates []*v1alpha1.ClusterAnalysisTemplate
	TemplateType             AnalysisTemplateType
	// CanaryStepIndex only used for InlineAnalysis
	CanaryStepIndex int
	Args            []v1alpha1.AnalysisRunArgument
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
	AnalysisTemplatesWithType []AnalysisTemplatesWithType
	Ingresses                 []ingressutil.Ingress
	ServiceWithType           []ServiceWithType
	VirtualServices           []unstructured.Unstructured
	AmbassadorMappings        []unstructured.Unstructured
}

func ValidateRolloutReferencedResources(rollout *v1alpha1.Rollout, referencedResources ReferencedResources) field.ErrorList {
	allErrs := field.ErrorList{}
	for _, service := range referencedResources.ServiceWithType {
		allErrs = append(allErrs, ValidateService(service, rollout)...)
	}
	for _, templates := range referencedResources.AnalysisTemplatesWithType {
		allErrs = append(allErrs, ValidateAnalysisTemplatesWithType(rollout, templates)...)
	}
	for _, ingress := range referencedResources.Ingresses {
		allErrs = append(allErrs, ValidateIngress(rollout, &ingress)...)
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

func ValidateAnalysisTemplatesWithType(rollout *v1alpha1.Rollout, templates AnalysisTemplatesWithType) field.ErrorList {
	allErrs := field.ErrorList{}
	fldPath := GetAnalysisTemplateWithTypeFieldPath(templates.TemplateType, templates.CanaryStepIndex)
	if fldPath == nil {
		return allErrs
	}

	templateNames := GetAnalysisTemplateNames(templates)
	value := fmt.Sprintf("templateNames: %s", templateNames)
	_, err := analysisutil.NewAnalysisRunFromTemplates(templates.AnalysisTemplates, templates.ClusterAnalysisTemplates, buildAnalysisArgs(templates.Args, rollout), "", "", "")
	if err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath, value, err.Error()))
		return allErrs
	}

	for _, template := range templates.AnalysisTemplates {
		allErrs = append(allErrs, ValidateAnalysisTemplateWithType(rollout, template, nil, templates.TemplateType, fldPath)...)
	}
	for _, clusterTemplate := range templates.ClusterAnalysisTemplates {
		allErrs = append(allErrs, ValidateAnalysisTemplateWithType(rollout, nil, clusterTemplate, templates.TemplateType, fldPath)...)
	}
	return allErrs
}

func ValidateAnalysisTemplateWithType(rollout *v1alpha1.Rollout, template *v1alpha1.AnalysisTemplate, clusterTemplate *v1alpha1.ClusterAnalysisTemplate, templateType AnalysisTemplateType, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	var templateSpec v1alpha1.AnalysisTemplateSpec
	var templateName string

	if clusterTemplate != nil {
		templateName, templateSpec = clusterTemplate.Name, clusterTemplate.Spec
	} else if template != nil {
		templateName, templateSpec = template.Name, template.Spec
	}

	if templateType != BackgroundAnalysis {
		setArgValuePlaceHolder(templateSpec.Args)
		resolvedMetrics, err := validateAnalysisMetrics(templateSpec.Metrics, templateSpec.Args)
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
	} else if templateType == BackgroundAnalysis && len(templateSpec.Args) > 0 {
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

func ValidateIngress(rollout *v1alpha1.Rollout, ingress *ingressutil.Ingress) field.ErrorList {
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
	if !ingressutil.HasRuleWithService(ingress, serviceName) {
		msg := fmt.Sprintf("ingress `%s` has no rules using service %s backend", ingress.GetName(), serviceName)
		allErrs = append(allErrs, field.Invalid(fldPath, ingressName, msg))
	}
	return allErrs
}

// ValidateRolloutVirtualServicesConfig checks either VirtualService or VirtualServices configured
// It returns an error if both VirtualService and VirtualServices are configured.
// Also, returns an error if both are not configured.
func ValidateRolloutVirtualServicesConfig(r *v1alpha1.Rollout) error {
	var fldPath *field.Path
	fldPath = field.NewPath("spec", "strategy", "canary", "trafficRouting", "istio")
	errorString := "either VirtualService or VirtualServices must be configured"

	if r.Spec.Strategy.Canary != nil {
		canary := r.Spec.Strategy.Canary
		if canary.TrafficRouting != nil && canary.TrafficRouting.Istio != nil {
			if istioutil.MultipleVirtualServiceConfigured(r) {
				if r.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService != nil {
					return field.InternalError(fldPath, fmt.Errorf(errorString))
				}
			} else {
				if r.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService == nil {
					return field.InternalError(fldPath, fmt.Errorf(errorString))
				}
			}
		}
	}
	return nil
}

func ValidateVirtualService(rollout *v1alpha1.Rollout, obj unstructured.Unstructured) field.ErrorList {
	var fldPath *field.Path
	var virtualServices []v1alpha1.IstioVirtualService
	allErrs := field.ErrorList{}
	newObj := obj.DeepCopy()

	if istioutil.MultipleVirtualServiceConfigured(rollout) {
		fldPath = field.NewPath("spec", "strategy", "canary", "trafficRouting", "istio", "virtualServices", "name")
		virtualServices = rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualServices
	} else {
		fldPath = field.NewPath("spec", "strategy", "canary", "trafficRouting", "istio", "virtualService", "name")
		virtualServices = []v1alpha1.IstioVirtualService{*rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService}
	}

	virtualServiceRecordName := obj.GetName()

	for _, virtualService := range virtualServices {
		name := virtualService.Name
		_, vsvcName := istioutil.GetVirtualServiceNamespaceName(name)
		if vsvcName == virtualServiceRecordName {
			httpRoutesI, errHttp := istio.GetHttpRoutesI(newObj)
			tlsRoutesI, errTls := istio.GetTlsRoutesI(newObj)
			// None of the HTTP/TLS routes exist.
			if errHttp != nil && errTls != nil {
				msg := fmt.Sprintf("Unable to get HTTP and/or TLS routes for Istio VirtualService")
				allErrs = append(allErrs, field.Invalid(fldPath, vsvcName, msg))
			}
			// Validate HTTP Routes
			if errHttp == nil {
				httpRoutes, err := istio.GetHttpRoutes(newObj, httpRoutesI)
				if err != nil {
					msg := fmt.Sprintf("Unable to get HTTP routes for Istio VirtualService")
					allErrs = append(allErrs, field.Invalid(fldPath, vsvcName, msg))
				}
				err = istio.ValidateHTTPRoutes(rollout, virtualService.Routes, httpRoutes)
				if err != nil {
					msg := fmt.Sprintf("Istio VirtualService has invalid HTTP routes. Error: %s", err.Error())
					allErrs = append(allErrs, field.Invalid(fldPath, vsvcName, msg))
				}
			}
			// Validate TLS Routes
			if errTls == nil {
				tlsRoutes, err := istio.GetTlsRoutes(newObj, tlsRoutesI)
				if err != nil {
					msg := fmt.Sprintf("Unable to get TLS routes for Istio VirtualService")
					allErrs = append(allErrs, field.Invalid(fldPath, vsvcName, msg))
				}
				err = istio.ValidateTlsRoutes(rollout, virtualService.TLSRoutes, tlsRoutes)
				if err != nil {
					msg := fmt.Sprintf("Istio VirtualService has invalid TLS routes. Error: %s", err.Error())
					allErrs = append(allErrs, field.Invalid(fldPath, vsvcName, msg))
				}
			}
			break
		}
	}
	// Return all errors
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

func GetAnalysisTemplateNames(templates AnalysisTemplatesWithType) []string {
	templateNames := make([]string, 0)
	for _, template := range templates.AnalysisTemplates {
		templateNames = append(templateNames, template.Name)
	}
	for _, clusterTemplate := range templates.ClusterAnalysisTemplates {
		templateNames = append(templateNames, clusterTemplate.Name)
	}
	return templateNames
}

func GetAnalysisTemplateWithTypeFieldPath(templateType AnalysisTemplateType, canaryStepIndex int) *field.Path {
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
	return fldPath
}

func buildAnalysisArgs(args []v1alpha1.AnalysisRunArgument, r *v1alpha1.Rollout) []v1alpha1.Argument {
	stableRSDummy := appsv1.ReplicaSet{
		ObjectMeta: v1.ObjectMeta{
			Labels: map[string]string{
				v1alpha1.DefaultRolloutUniqueLabelKey: "dummy-stable-hash",
			},
		},
	}
	newRSDummy := appsv1.ReplicaSet{
		ObjectMeta: v1.ObjectMeta{
			Labels: map[string]string{
				v1alpha1.DefaultRolloutUniqueLabelKey: "dummy-new-hash",
			},
		},
	}
	return analysisutil.BuildArgumentsForRolloutAnalysisRun(args, &stableRSDummy, &newRSDummy, r)
}

// validateAnalysisMetrics validates the metrics of an Analysis object
func validateAnalysisMetrics(metrics []v1alpha1.Metric, args []v1alpha1.Argument) ([]v1alpha1.Metric, error) {
	for i, arg := range args {
		if arg.ValueFrom != nil {
			if arg.Value != nil {
				return nil, fmt.Errorf("arg '%s' has both Value and ValueFrom fields", arg.Name)
			}
			argVal := "dummy-value"
			args[i].Value = &argVal
		}
	}

	for i, metric := range metrics {
		resolvedMetric, err := analysisutil.ResolveMetricArgs(metric, args)
		if err != nil {
			return nil, err
		}
		metrics[i] = *resolvedMetric
	}
	return metrics, nil
}
