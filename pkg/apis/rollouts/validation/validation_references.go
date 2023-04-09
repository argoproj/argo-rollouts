package validation

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/strings/slices"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/ambassador"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/appmesh"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/istio"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	serviceutil "github.com/argoproj/argo-rollouts/utils/service"
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
	PingService    ServiceType = "PingService"
	PongService    ServiceType = "PongService"
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
	AppMeshResources          []unstructured.Unstructured
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
	for _, appmeshRes := range referencedResources.AppMeshResources {
		allErrs = append(allErrs, ValidateAppMeshResource(appmeshRes)...)
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

	// Verify the service selector labels matches rollout's, except for DefaultRolloutUniqueLabelKey
	for svcLabelKey, svcLabelValue := range service.Spec.Selector {
		if svcLabelKey == v1alpha1.DefaultRolloutUniqueLabelKey {
			continue
		}
		if v, ok := rollout.Spec.Template.Labels[svcLabelKey]; !ok || v != svcLabelValue {
			msg := fmt.Sprintf("Service %q has unmatch label %q in rollout", service.Name, svcLabelKey)
			allErrs = append(allErrs, field.Invalid(fldPath, service.Name, msg))
		}
	}

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
	_, err := analysisutil.NewAnalysisRunFromTemplates(templates.AnalysisTemplates, templates.ClusterAnalysisTemplates, buildAnalysisArgs(templates.Args, rollout), []v1alpha1.DryRun{}, []v1alpha1.MeasurementRetention{}, "", "", "")
	if err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath, value, err.Error()))
		return allErrs
	}

	if rollout.Spec.Strategy.Canary != nil {
		for _, step := range rollout.Spec.Strategy.Canary.Steps {
			if step.Analysis != nil {
				_, err := analysisutil.NewAnalysisRunFromTemplates(templates.AnalysisTemplates, templates.ClusterAnalysisTemplates, buildAnalysisArgs(templates.Args, rollout), step.Analysis.DryRun, step.Analysis.MeasurementRetention, "", "", "")
				if err != nil {
					allErrs = append(allErrs, field.Invalid(fldPath, value, err.Error()))
					return allErrs
				}
			}
		}
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
	canary := rollout.Spec.Strategy.Canary

	if canary.TrafficRouting.Nginx != nil {
		return validateNginxIngress(canary, ingress, fldPath)
	} else if canary.TrafficRouting.ALB != nil {
		return validateAlbIngress(canary, ingress, fldPath)
	} else {
		return allErrs
	}
}

func validateNginxIngress(canary *v1alpha1.CanaryStrategy, ingress *ingressutil.Ingress, fldPath *field.Path) field.ErrorList {
	stableIngresses := canary.TrafficRouting.Nginx.StableIngresses
	allErrs := field.ErrorList{}
	// If there are additional stable Nginx ingresses, and one of them is being validated,
	// use that ingress name.
	if stableIngresses != nil && slices.Contains(stableIngresses, ingress.GetName()) {
		fldPath = fldPath.Child("nginx").Child("stableIngresses")
		serviceName := canary.StableService
		ingressName := ingress.GetName()
		return reportErrors(ingress, serviceName, ingressName, fldPath, allErrs)
	} else {
		fldPath = fldPath.Child("nginx").Child("stableIngress")
		serviceName := canary.StableService
		ingressName := canary.TrafficRouting.Nginx.StableIngress
		return reportErrors(ingress, serviceName, ingressName, fldPath, allErrs)
	}
}

func validateAlbIngress(canary *v1alpha1.CanaryStrategy, ingress *ingressutil.Ingress, fldPath *field.Path) field.ErrorList {
	ingresses := canary.TrafficRouting.ALB.Ingresses
	allErrs := field.ErrorList{}
	// If there are multiple ALB ingresses, and one of them is being validated,
	// use that ingress name.
	if ingresses != nil && slices.Contains(ingresses, ingress.GetName()) {
		fldPath = fldPath.Child("alb").Child("ingresses")
		serviceName := canary.StableService
		ingressName := ingress.GetName()
		if canary.TrafficRouting.ALB.RootService != "" {
			serviceName = canary.TrafficRouting.ALB.RootService
		}
		return reportErrors(ingress, serviceName, ingressName, fldPath, allErrs)
	} else {
		fldPath = fldPath.Child("alb").Child("ingress")
		serviceName := canary.StableService
		ingressName := canary.TrafficRouting.ALB.Ingress
		if canary.TrafficRouting.ALB.RootService != "" {
			serviceName = canary.TrafficRouting.ALB.RootService
		}
		return reportErrors(ingress, serviceName, ingressName, fldPath, allErrs)
	}
}

func reportErrors(ingress *ingressutil.Ingress, serviceName, ingressName string, fldPath *field.Path, allErrs field.ErrorList) field.ErrorList {
	if !ingressutil.HasRuleWithService(ingress, serviceName) {
		msg := fmt.Sprintf("ingress `%s` has no rules using service %s backend", ingress.GetName(), serviceName)
		allErrs = append(allErrs, field.Invalid(fldPath, ingressName, msg))
	}
	return allErrs
}

// Validates that only one or the other of the two fields
// (StableIngress, StableIngresses) is defined on the Nginx struct
func ValidateRolloutNginxIngressesConfig(r *v1alpha1.Rollout) error {
	fldPath := field.NewPath("spec", "strategy", "canary", "trafficRouting", "nginx")
	var err error

	// If the traffic strategy isn't canary -> Nginx, no need to validate
	// fields on Nginx struct.
	if r.Spec.Strategy.Canary == nil ||
		r.Spec.Strategy.Canary.TrafficRouting == nil ||
		r.Spec.Strategy.Canary.TrafficRouting.Nginx == nil {
		return nil
	}

	// If both StableIngress and StableIngresses are configured or if neither are configured,
	// that's an error. It must be one or the other.
	if ingressutil.MultipleNginxIngressesConfigured(r) && ingressutil.SingleNginxIngressConfigured(r) {
		err = field.InternalError(fldPath, fmt.Errorf("Either StableIngress or StableIngresses must be configured. Both are configured."))
	} else if !(ingressutil.MultipleNginxIngressesConfigured(r) || ingressutil.SingleNginxIngressConfigured(r)) {
		err = field.InternalError(fldPath, fmt.Errorf("Either StableIngress or StableIngresses must be configured. Neither are configured."))
	}

	return err
}

// ValidateRolloutAlbIngressesConfig checks that only one or the other of the two fields
// (Ingress, Ingresses) is defined on the ALB struct
func ValidateRolloutAlbIngressesConfig(r *v1alpha1.Rollout) error {
	fldPath := field.NewPath("spec", "strategy", "canary", "trafficRouting", "alb")
	var err error

	// If the traffic strategy isn't canary -> ALB, no need to validate
	// fields on ALB struct.
	if r.Spec.Strategy.Canary == nil ||
		r.Spec.Strategy.Canary.TrafficRouting == nil ||
		r.Spec.Strategy.Canary.TrafficRouting.ALB == nil {
		return nil
	}

	// If both Ingress and Ingresses are configured or if neither are configured,
	// that's an error. It must be one or the other.
	if ingressutil.MultipleAlbIngressesConfigured(r) && ingressutil.SingleAlbIngressConfigured(r) {
		err = field.InternalError(fldPath, fmt.Errorf("Either Ingress or Ingresses must be configured. Both are configured."))
	} else if !(ingressutil.MultipleAlbIngressesConfigured(r) || ingressutil.SingleAlbIngressConfigured(r)) {
		err = field.InternalError(fldPath, fmt.Errorf("Either Ingress or Ingresses must be configured. Neither are configured."))
	}

	return err
}

// ValidateRolloutVirtualServicesConfig checks either VirtualService or VirtualServices configured
// It returns an error if both VirtualService and VirtualServices are configured.
// Also, returns an error if both are not configured.
func ValidateRolloutVirtualServicesConfig(r *v1alpha1.Rollout) error {
	fldPath := field.NewPath("spec", "strategy", "canary", "trafficRouting", "istio")
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

	if rollout.Spec.Strategy.Canary == nil ||
		rollout.Spec.Strategy.Canary.TrafficRouting == nil ||
		rollout.Spec.Strategy.Canary.TrafficRouting.Istio == nil {

		msg := "Rollout object is not configured with Istio traffic routing"
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "istio"), rollout.Name, msg))
		return allErrs
	}

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
			tcpRoutesI, errTcp := istio.GetTcpRoutesI(newObj)
			// None of the HTTP/TLS routes exist.
			if errHttp != nil && errTls != nil && errTcp != nil {
				msg := "Unable to get any of the HTTP, TCP or TLS routes for the Istio VirtualService"
				allErrs = append(allErrs, field.Invalid(fldPath, vsvcName, msg))
			}
			// Validate HTTP Routes
			if errHttp == nil {
				httpRoutes, err := istio.GetHttpRoutes(httpRoutesI)
				if err != nil {
					msg := "Unable to get HTTP routes for Istio VirtualService"
					allErrs = append(allErrs, field.Invalid(fldPath, vsvcName, msg))
				} else {
					err = istio.ValidateHTTPRoutes(rollout, virtualService.Routes, httpRoutes)
					if err != nil {
						msg := fmt.Sprintf("Istio VirtualService has invalid HTTP routes. Error: %s", err.Error())
						allErrs = append(allErrs, field.Invalid(fldPath, vsvcName, msg))
					}
				}
			}
			// Validate TLS Routes
			if errTls == nil {
				tlsRoutes, err := istio.GetTlsRoutes(newObj, tlsRoutesI)
				if err != nil {
					msg := "Unable to get TLS routes for Istio VirtualService"
					allErrs = append(allErrs, field.Invalid(fldPath, vsvcName, msg))
				} else {
					err = istio.ValidateTlsRoutes(rollout, virtualService.TLSRoutes, tlsRoutes)
					if err != nil {
						msg := fmt.Sprintf("Istio VirtualService has invalid TLS routes. Error: %s", err.Error())
						allErrs = append(allErrs, field.Invalid(fldPath, vsvcName, msg))
					}
				}
			}
			// Validate TCP Routes
			if errTcp == nil {
				tcpRoutes, err := istio.GetTcpRoutes(newObj, tcpRoutesI)
				if err != nil {
					msg := "Unable to get TCP routes for Istio VirtualService"
					allErrs = append(allErrs, field.Invalid(fldPath, vsvcName, msg))
				} else {
					err = istio.ValidateTcpRoutes(rollout, virtualService.TCPRoutes, tcpRoutes)
					if err != nil {
						msg := fmt.Sprintf("Istio VirtualService has invalid TCP routes. Error: %s", err.Error())
						allErrs = append(allErrs, field.Invalid(fldPath, vsvcName, msg))
					}
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

func ValidateAppMeshResource(obj unstructured.Unstructured) field.ErrorList {
	if obj.GetKind() != "VirtualRouter" {
		fldPath := field.NewPath("kind")
		msg := fmt.Sprintf("Expected object kind to be VirtualRouter but is %s", obj.GetKind())
		return field.ErrorList{field.Invalid(fldPath, obj.GetKind(), msg)}
	}

	err := ValidateAppMeshVirtualRouter(&obj)
	if err != nil {
		return field.ErrorList{err}
	}
	return field.ErrorList{}
}

func ValidateAppMeshVirtualRouter(vrouter *unstructured.Unstructured) *field.Error {
	routesFldPath := field.NewPath("spec", "routes")
	allRoutesI, found, err := unstructured.NestedSlice(vrouter.Object, "spec", "routes")
	if !found || err != nil || len(allRoutesI) == 0 {
		msg := fmt.Sprintf("No routes defined for AppMesh virtual-router %s", vrouter.GetName())
		return field.Invalid(routesFldPath, vrouter.GetName(), msg)
	}
	for idx, routeI := range allRoutesI {
		routeFldPath := routesFldPath.Index(idx)
		route, ok := routeI.(map[string]interface{})
		if !ok {
			msg := fmt.Sprintf("Invalid route was found for AppMesh virtual-router %s at index %d", vrouter.GetName(), idx)
			return field.Invalid(routeFldPath, vrouter.GetName(), msg)
		}

		routeName := route["name"]
		routeRule, routeType, err := appmesh.GetRouteRule(route)
		if err != nil {
			msg := fmt.Sprintf("Error getting route details for AppMesh virtual-router %s and route %s. Error: %s", vrouter.GetName(), routeName, err.Error())
			return field.Invalid(routeFldPath, vrouter.GetName(), msg)
		}

		weightedTargetsFldPath := routeFldPath.Child(routeType).Child("action").Child("weightedTargets")
		weightedTargets, found, err := unstructured.NestedSlice(routeRule, "action", "weightedTargets")
		if !found || err != nil {
			msg := fmt.Sprintf("Invalid route action found for AppMesh virtual-router %s and route %s", vrouter.GetName(), routeName)
			return field.Invalid(weightedTargetsFldPath, vrouter.GetName(), msg)
		}

		if len(weightedTargets) != 2 {
			msg := fmt.Sprintf("Invalid number of weightedTargets (%d) for AppMesh virtual-router %s and route %s, expected 2", len(weightedTargets), vrouter.GetName(), routeName)
			return field.Invalid(weightedTargetsFldPath, vrouter.GetName(), msg)
		}
	}
	return nil
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
	case PingService:
		fldPath = fldPath.Child("canary", "pingPong", "pingService")
	case PongService:
		fldPath = fldPath.Child("canary", "pingPong", "pongService")
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
	res, _ := analysisutil.BuildArgumentsForRolloutAnalysisRun(args, &stableRSDummy, &newRSDummy, r)
	return res
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
