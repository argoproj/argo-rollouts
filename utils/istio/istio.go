package istio

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
)

var (
	istioAPIVersion = defaults.DefaultIstioVersion
)

func SetIstioAPIVersion(apiVersion string) {
	istioAPIVersion = apiVersion
}

func GetIstioAPIVersion() string {
	return istioAPIVersion
}

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
		Version:  istioAPIVersion,
		Resource: "virtualservices",
	}
}

func GetIstioDestinationRuleGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "networking.istio.io",
		Version:  istioAPIVersion,
		Resource: "destinationrules",
	}
}

// GetRolloutVirtualServiceKeys gets the referenced VirtualService and its namespace from a Rollout
func GetRolloutVirtualServiceKeys(ro *v1alpha1.Rollout) []string {
	canary := ro.Spec.Strategy.Canary
	if canary == nil || canary.TrafficRouting == nil || canary.TrafficRouting.Istio == nil || canary.TrafficRouting.Istio.VirtualService.Name == "" {
		return []string{}
	}
	return []string{fmt.Sprintf("%s/%s", ro.Namespace, canary.TrafficRouting.Istio.VirtualService.Name)}
}

// GetRolloutDesinationRuleKeys gets the referenced DestinationRule and its namespace from a Rollout
func GetRolloutDesinationRuleKeys(ro *v1alpha1.Rollout) []string {
	canary := ro.Spec.Strategy.Canary
	if canary == nil || canary.TrafficRouting == nil || canary.TrafficRouting.Istio == nil || canary.TrafficRouting.Istio.DestinationRule == nil || canary.TrafficRouting.Istio.DestinationRule.Name == "" {
		return []string{}
	}
	return []string{fmt.Sprintf("%s/%s", ro.Namespace, canary.TrafficRouting.Istio.DestinationRule.Name)}
}
