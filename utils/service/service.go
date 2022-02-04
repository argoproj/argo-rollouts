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
	if rollout == nil {
		return nil
	}
	var services []string
	if rollout.Spec.Strategy.BlueGreen != nil {
		if rollout.Spec.Strategy.BlueGreen.ActiveService != "" {
			services = append(services, fmt.Sprintf("%s/%s", rollout.Namespace, rollout.Spec.Strategy.BlueGreen.ActiveService))
		}
		if rollout.Spec.Strategy.BlueGreen.PreviewService != "" {
			services = append(services, fmt.Sprintf("%s/%s", rollout.Namespace, rollout.Spec.Strategy.BlueGreen.PreviewService))
		}
	} else if rollout.Spec.Strategy.Canary != nil {
		if rollout.Spec.Strategy.Canary.CanaryService != "" {
			services = append(services, fmt.Sprintf("%s/%s", rollout.Namespace, rollout.Spec.Strategy.Canary.CanaryService))
		}
		if rollout.Spec.Strategy.Canary.StableService != "" {
			services = append(services, fmt.Sprintf("%s/%s", rollout.Namespace, rollout.Spec.Strategy.Canary.StableService))
		}
		if rollout.Spec.Strategy.Canary.PingPong != nil && rollout.Spec.Strategy.Canary.PingPong.PingService != "" {
			services = append(services, fmt.Sprintf("%s/%s", rollout.Namespace, rollout.Spec.Strategy.Canary.PingPong.PingService))
		}
		if rollout.Spec.Strategy.Canary.PingPong != nil && rollout.Spec.Strategy.Canary.PingPong.PongService != "" {
			services = append(services, fmt.Sprintf("%s/%s", rollout.Namespace, rollout.Spec.Strategy.Canary.PingPong.PongService))
		}
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
