package istio

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
)

func DoesIstioExist(dynamicClient dynamic.Interface, namespace string) bool {
	_, err := dynamicClient.Resource(GetIstioVirtualServiceGVR()).Namespace(namespace).List(context.TODO(), metav1.ListOptions{Limit: 1})
	if err != nil {
		return false
	}
	return true
}

func GetIstioVirtualServiceGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "networking.istio.io",
		Version:  defaults.GetIstioAPIVersion(),
		Resource: "virtualservices",
	}
}

func GetIstioDestinationRuleGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "networking.istio.io",
		Version:  defaults.GetIstioAPIVersion(),
		Resource: "destinationrules",
	}
}

func GetVirtualServiceNamespaceName(vsv string) (string, string) {
	namespace := ""
	name := ""

	fields := strings.Split(vsv, ".")
	if len(fields) >= 2 {
		name = fields[0]
		namespace = fields[1]
	} else if len(fields) == 1 {
		name = fields[0]
	}

	return namespace, name
}

func MultipleVirtualServiceConfigured(rollout *v1alpha1.Rollout) bool {
	return rollout.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualServices != nil
}

// GetRolloutVirtualServiceKeys gets the referenced VirtualService and its namespace from a Rollout
func GetRolloutVirtualServiceKeys(ro *v1alpha1.Rollout) []string {
	var virtualServices []v1alpha1.IstioVirtualService
	var virtualServiceKeys []string
	canary := ro.Spec.Strategy.Canary

	if canary == nil || canary.TrafficRouting == nil || canary.TrafficRouting.Istio == nil ||
		(canary.TrafficRouting.Istio.VirtualServices == nil && canary.TrafficRouting.Istio.VirtualService == nil) ||
		(canary.TrafficRouting.Istio.VirtualServices != nil && canary.TrafficRouting.Istio.VirtualService != nil) {
		return []string{}
	}

	if MultipleVirtualServiceConfigured(ro) {
		virtualServices = ro.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualServices
	} else {
		virtualServices = []v1alpha1.IstioVirtualService{*ro.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService}
	}

	for _, virtualService := range virtualServices {
		namespace, name := GetVirtualServiceNamespaceName(virtualService.Name)
		if namespace == "" {
			namespace = ro.Namespace
		}
		if name == "" {
			continue
		}

		virtualServiceKeys = append(virtualServiceKeys, fmt.Sprintf("%s/%s", namespace, name))
	}

	return virtualServiceKeys
}

// GetRolloutDesinationRuleKeys gets the referenced DestinationRule and its namespace from a Rollout
func GetRolloutDesinationRuleKeys(ro *v1alpha1.Rollout) []string {
	canary := ro.Spec.Strategy.Canary
	if canary == nil || canary.TrafficRouting == nil || canary.TrafficRouting.Istio == nil || canary.TrafficRouting.Istio.DestinationRule == nil || canary.TrafficRouting.Istio.DestinationRule.Name == "" {
		return []string{}
	}
	return []string{fmt.Sprintf("%s/%s", ro.Namespace, canary.TrafficRouting.Istio.DestinationRule.Name)}
}
