package validation

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/kubernetes/pkg/apis/networking"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/istio"
)
// Controller will validate references in reconciliation

type ReferencedResources struct {
	Ingresses []networking.Ingress
	Services []v1.Service
	VirtualServices []v1alpha1.IstioVirtualService
	AnalysisTemplates []v1alpha1.AnalysisTemplate
}

func ValidateRolloutReferencedResources(rollout *v1alpha1.Rollout, referencedResources ReferencedResources) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, ValidateIngresses(referencedResources.Ingresses)...)
	allErrs = append(allErrs, ValidateServices(referencedResources.Services)...)
	allErrs = append(allErrs, ValidateVirtualServices(rollout, referencedResources.VirtualServices)...)
	allErrs = append(allErrs, ValidateAnalysisTemplates(referencedResources.AnalysisTemplates)...)
	return allErrs
}

func ValidateIngresses(ingresses []networking.Ingress) field.ErrorList {
	allErrs := field.ErrorList{}
	for _, ingress := range ingresses {
		allErrs = append(allErrs, ValidateIngress(ingress)...)
	}
	return allErrs
}

func ValidateServices(services []v1.Service) field.ErrorList {
	allErrs := field.ErrorList{}
	for _, service := range services {
		allErrs = append(allErrs, ValidateService(service)...)
	}
	return allErrs
}

func ValidateVirtualServices(rollout *v1alpha1.Rollout, virtualServices []v1alpha1.IstioVirtualService) field.ErrorList {
	allErrs := field.ErrorList{}
	for _, virtualService := range virtualServices {
		allErrs = append(allErrs, ValidateVirtualService(rollout, virtualService)...)
	}
	return allErrs
}

func ValidateAnalysisTemplates(analysisTempates []v1alpha1.AnalysisTemplate) field.ErrorList {
	allErrs := field.ErrorList{}
	for _, analysisTemplate := range analysisTempates {
		allErrs = append(allErrs, ValidateAnalysisTemplate(analysisTemplate)...)
	}
	return allErrs
}

// ALB or Nginx
func ValidateIngress(ingress networking.Ingress) field.ErrorList {
	return nil
}

// TODO: what checks?
func ValidateService(service v1.Service) field.ErrorList {
	return nil
}

func ValidateVirtualService(rollout *v1alpha1.Rollout, virtualService v1alpha1.IstioVirtualService) field.ErrorList {
	allErrs := field.ErrorList{}
	// TODO: types.go for istio vsvc?
	//httpRoutesI := virtualService.Routes
	//if !notFound {
	//	return nil, false, fmt.Errorf(".spec.http is not defined")
	//}
	//if err != nil {
	//	return nil, false, err
	//}
	//routeBytes, err := json.Marshal(httpRoutesI)
	//if err != nil {
	//	return nil, false, err
	//}
	//
	//var httpRoutes []HttpRoute
	//err = json.Unmarshal(routeBytes, &httpRoutes)
	//if err != nil {
	//	return nil, false, err
	//}
	validateHTTPRoutes(rollout, virtualService.Routes)
	return nil
}

func validateHTTPRoutes(r *v1alpha1.Rollout, httpRoutes []istio.HttpRoute) error {
	routes := r.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Routes
	stableSvc := r.Spec.Strategy.Canary.StableService
	canarySvc := r.Spec.Strategy.Canary.CanaryService

	routesPatched := map[string]bool{}
	for _, route := range routes {
		routesPatched[route] = false
	}

	for _, route := range httpRoutes {
		// check if the httpRoute is in the list of routes from the rollout
		if _, ok := routesPatched[route.Name]; ok {
			routesPatched[route.Name] = true
			err := validateHosts(route, stableSvc, canarySvc)
			if err != nil {
				return err
			}
		}
	}

	for i := range routesPatched {
		if !routesPatched[i] {
			return fmt.Errorf("Route '%s' is not found", i)
		}
	}

	return nil
}

// validateHosts ensures there are two destinations within a route and their hosts are the stable and canary service
func validateHosts(hr istio.HttpRoute, stableSvc, canarySvc string) error {
	if len(hr.Route) != 2 {
		return fmt.Errorf("Route '%s' does not have exactly two routes", hr.Name)
	}
	hasStableSvc := false
	hasCanarySvc := false
	for _, r := range hr.Route {
		if r.Destination.Host == stableSvc {
			hasStableSvc = true
		}
		if r.Destination.Host == canarySvc {
			hasCanarySvc = true
		}
	}
	if !hasCanarySvc {
		return fmt.Errorf("Canary Service '%s' not found in route", canarySvc)
	}
	if !hasStableSvc {
		return fmt.Errorf("Stable Service '%s' not found in route", stableSvc)
	}
	return nil
}

// Must run deterministically
func ValidateAnalysisTemplate(analysisTempate v1alpha1.AnalysisTemplate) field.ErrorList {
	allErrs := field.ErrorList{}
	for _, metric := range analysisTempate.Spec.Metrics {
		effectiveCount := metric.EffectiveCount()
		if effectiveCount == nil {
			allErrs = append(allErrs, nil) // "Metric metric.Name in analysisTemplate analysisTemplate.name runs indefinitely"
		}
	}
	return allErrs
}
