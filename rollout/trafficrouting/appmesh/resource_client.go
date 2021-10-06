package appmesh

import (
	"context"
	"errors"

	appmeshutil "github.com/argoproj/argo-rollouts/utils/appmesh"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

type ResourceClient struct {
	client dynamic.Interface
}

func NewResourceClient(client dynamic.Interface) *ResourceClient {
	return &ResourceClient{
		client: client,
	}
}

func (rc *ResourceClient) GetVirtualServiceCR(ctx context.Context, namespace string, name string) (*unstructured.Unstructured, error) {
	return rc.client.Resource(appmeshutil.GetAppMeshVirtualServiceGVR()).
		Namespace(namespace).
		Get(ctx, name, metav1.GetOptions{})
}

func (rc *ResourceClient) GetVirtualRouterCR(ctx context.Context, namespace string, name string) (*unstructured.Unstructured, error) {
	return rc.client.Resource(appmeshutil.GetAppMeshVirtualRouterGVR()).
		Namespace(namespace).
		Get(ctx, name, metav1.GetOptions{})
}

func (rc *ResourceClient) GetVirtualNodeCR(ctx context.Context, namespace string, name string) (*unstructured.Unstructured, error) {
	return rc.client.Resource(appmeshutil.GetAppMeshVirtualNodeGVR()).
		Namespace(namespace).
		Get(ctx, name, metav1.GetOptions{})
}

func (rc *ResourceClient) UpdateVirtualRouterCR(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	client := rc.client.Resource(appmeshutil.GetAppMeshVirtualRouterGVR()).Namespace(obj.GetNamespace())
	return client.Update(ctx, obj, metav1.UpdateOptions{})
}

func (rc *ResourceClient) UpdateVirtualNodeCR(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	client := rc.client.Resource(appmeshutil.GetAppMeshVirtualNodeGVR()).Namespace(obj.GetNamespace())
	return client.Update(ctx, obj, metav1.UpdateOptions{})
}

func (rc *ResourceClient) GetVirtualRouterCRForVirtualService(ctx context.Context, uVsvc *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	virtualRouterRefMap, found, err := unstructured.NestedMap(uVsvc.Object, "spec", "provider", "virtualRouter", "virtualRouterRef")
	if !found {
		return nil, errors.New(ErrVirtualServiceNotUsingVirtualRouter)
	}
	if err != nil {
		return nil, err
	}
	namespace := defaultIfEmpty(virtualRouterRefMap["namespace"], uVsvc.GetNamespace())
	name := virtualRouterRefMap["name"].(string)
	return rc.GetVirtualRouterCR(ctx, namespace, name)
}

func defaultIfEmpty(strI interface{}, defaultStr string) string {
	if strI == nil {
		return defaultStr
	} else {
		str, _ := strI.(string)
		if str == "" {
			return defaultStr
		}
		return str
	}
}
