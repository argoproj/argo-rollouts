package istio

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

func DoesIstioExist(dynamicClient dynamic.Interface, namespace string, version string) bool {
	_, err := dynamicClient.Resource(GetIstioGVR(version)).Namespace(namespace).List(metav1.ListOptions{Limit:1})
	if err != nil {
		return false
	}
	return true
}

func GetIstioGVR(version string) schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "networking.istio.io",
		Version:  version,
		Resource: "virtualservices",
	}
}
