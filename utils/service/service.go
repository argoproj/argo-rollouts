package service

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func GetRolloutSelectorLabel(svc *corev1.Service) string {
	if svc == nil || svc.Spec.Selector == nil {
		return ""
	}
	return svc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
}

// GetRolloutServiceKeys returns services keys (namespace/serviceName) which are referenced by specified rollout
func GetRolloutServiceKeys(rollout *v1alpha1.Rollout) []string {
	servicesSet := make(map[string]bool)
	if rollout.Spec.Strategy.BlueGreen != nil {
		if rollout.Spec.Strategy.BlueGreen.ActiveService != "" {
			servicesSet[fmt.Sprintf("%s/%s", rollout.Namespace, rollout.Spec.Strategy.BlueGreen.ActiveService)] = true
		}
		if rollout.Spec.Strategy.BlueGreen.PreviewService != "" {
			servicesSet[fmt.Sprintf("%s/%s", rollout.Namespace, rollout.Spec.Strategy.BlueGreen.PreviewService)] = true
		}
	} else if rollout.Spec.Strategy.Canary != nil {
		if rollout.Spec.Strategy.Canary.CanaryService != "" {
			servicesSet[fmt.Sprintf("%s/%s", rollout.Namespace, rollout.Spec.Strategy.Canary.CanaryService)] = true
		}
		if rollout.Spec.Strategy.Canary.StableService != "" {
			servicesSet[fmt.Sprintf("%s/%s", rollout.Namespace, rollout.Spec.Strategy.Canary.StableService)] = true
		}
	}
	var services []string
	for svc := range servicesSet {
		services = append(services, svc)
	}
	return services
}

func HasManagedByAnnotation(service *corev1.Service) (string, bool) {
	if service.Annotations == nil {
		return "", false
	}
	annotation, exists := service.Annotations[v1alpha1.ManagedByRolloutsKey]
	return annotation, exists
}

// CheckRolloutForService Checks to if the Rollout references that service
func CheckRolloutForService(rollout *v1alpha1.Rollout, svc *corev1.Service) bool {
	for _, service := range GetRolloutServiceKeys(rollout) {
		if service == fmt.Sprintf("%s/%s", svc.Namespace, svc.Name) {
			return true
		}
	}
	return false
}
