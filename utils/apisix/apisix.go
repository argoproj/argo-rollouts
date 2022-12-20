package apisix

import (
	"context"
	"strings"

	"github.com/argoproj/argo-rollouts/utils/defaults"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const apisixRoutes = "apisixroutes"

var (
	apiGroupToResource = map[string]string{
		defaults.DefaultApisixAPIGroup: apisixRoutes,
	}
)

func DoesApisixExist(dynamicClient dynamic.Interface, namespace string) bool {
	_, err := dynamicClient.Resource(GetMappingGVR()).Namespace(namespace).List(context.TODO(), metav1.ListOptions{Limit: 1})
	if err != nil {
		return false
	}
	return true
}

func NewDynamicClient(di dynamic.Interface, namespace string) dynamic.ResourceInterface {
	return di.Resource(GetMappingGVR()).Namespace(namespace)
}

func GetMappingGVR() schema.GroupVersionResource {
	group := defaults.DefaultApisixAPIGroup
	parts := strings.Split(defaults.DefaultApisixVersion, "/")
	version := parts[len(parts)-1]
	resourceName := apiGroupToResource[group]
	return schema.GroupVersionResource{
		Group:    group,
		Version:  version,
		Resource: resourceName,
	}
}
