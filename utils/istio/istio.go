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

	// Strip the well-known cluster.local DNS suffix before parsing so that a
	// fully-qualified reference still resolves to the right name/namespace.
	vsv = strings.TrimSuffix(vsv, ".cluster.local")

	fields := strings.Split(vsv, ".")
	if len(fields) >= 2 {
		// Istio uses the "name.namespace" shorthand and a Kubernetes namespace
		// cannot contain a period, so the last segment is the namespace and
		// everything before it is the VirtualService name. This keeps names
		// that legitimately contain periods intact (see issue #4709).
		name = strings.Join(fields[:len(fields)-1], ".")
		namespace = fields[len(fields)-1]
	} else if len(fields) == 1 {
		name = fields[0]
	}

	return namespace, name
}

// GetVirtualServiceRefNamespaceName returns the namespace and name of the VirtualService a
// reference points to. A namespace given in the name ("name.namespace") takes precedence over
// the namespace field, and the Rollout namespace is used when neither is set.
func GetVirtualServiceRefNamespaceName(vsvc v1alpha1.IstioVirtualService, rolloutNamespace string) (string, string) {
	namespace, name := GetVirtualServiceNamespaceName(vsvc.Name)
	if namespace == "" {
		namespace = vsvc.Namespace
	}
	if namespace == "" {
		namespace = rolloutNamespace
	}
	return namespace, name
}

// GetDestinationRuleNamespace returns the namespace of the referenced DestinationRule, which
// defaults to the Rollout namespace.
func GetDestinationRuleNamespace(dRule *v1alpha1.IstioDestinationRule, rolloutNamespace string) string {
	if dRule != nil && dRule.Namespace != "" {
		return dRule.Namespace
	}
	return rolloutNamespace
}

// GetDestinationRuleManagedByValue returns the managed-by annotation value for a DestinationRule
// in dRuleNamespace: the Rollout name, prefixed with the Rollout namespace when the
// DestinationRule is in another namespace.
func GetDestinationRuleManagedByValue(ro *v1alpha1.Rollout, dRuleNamespace string) string {
	if dRuleNamespace == ro.Namespace {
		return ro.Name
	}
	return fmt.Sprintf("%s/%s", ro.Namespace, ro.Name)
}

// ParseDestinationRuleManagedByValue returns the namespace and name of the Rollout named by a
// DestinationRule's managed-by annotation. A plain name refers to a Rollout in the
// DestinationRule's own namespace.
func ParseDestinationRuleManagedByValue(value, dRuleNamespace string) (string, string) {
	if namespace, name, ok := strings.Cut(value, "/"); ok {
		return namespace, name
	}
	return dRuleNamespace, value
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
		namespace, name := GetVirtualServiceRefNamespaceName(virtualService, ro.Namespace)
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
	dRule := canary.TrafficRouting.Istio.DestinationRule
	return []string{fmt.Sprintf("%s/%s", GetDestinationRuleNamespace(dRule, ro.Namespace), dRule.Name)}
}
