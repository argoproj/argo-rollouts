package mocks

import (
	"context"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"

	argoRecord "github.com/argoproj/argo-rollouts/utils/record"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/record"
)

type FakeDynamicClient struct{}

type FakeTraefikServiceClient struct {
	IsClientGetTraefikServiceError   bool
	IsGetErrorTraefikServiceManifest bool

	IsGetMirrorTraefikService                  bool
	IsClientGetErrorMirrorTraefikService       bool
	IsGetErrorMirrorTraefikServiceManifest     bool
	IsGetMirrorTraefikServiceWithNotFoundField bool

	IsClientUpdateError bool
}

type FakeIngressRouteClient struct{}

type FakeServiceClient struct{}

type FakeService struct {
	Weight int
}

type FakeRecorder struct{}

var (
	TraefikServiceObj      *unstructured.Unstructured
	ErrorTraefikServiceObj *unstructured.Unstructured

	MirrorTraefikServiceObj               *unstructured.Unstructured
	ErrorMirrorTraefikServiceObj          *unstructured.Unstructured
	MirrorTraefikServiceWithNotFoundField *unstructured.Unstructured
)

func (f *FakeRecorder) Eventf(object runtime.Object, opts argoRecord.EventOptions, messageFmt string, args ...interface{}) {
}

func (f *FakeRecorder) Warnf(object runtime.Object, opts argoRecord.EventOptions, messageFmt string, args ...interface{}) {
}

func (f *FakeRecorder) K8sRecorder() record.EventRecorder {
	return nil
}

func (f *FakeTraefikServiceClient) Create(ctx context.Context, obj *unstructured.Unstructured, options metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}
func (f *FakeIngressRouteClient) Create(ctx context.Context, obj *unstructured.Unstructured, options metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *FakeTraefikServiceClient) Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	if f.IsGetMirrorTraefikService {
		if MirrorTraefikServiceObj.GetName() == name {
			return MirrorTraefikServiceObj, nil
		}
		return nil, errors.New("Mirror traefik service not found")
	}
	if f.IsClientGetErrorMirrorTraefikService {
		return nil, errors.New("Error get mirror traefik service")
	}
	if f.IsGetErrorMirrorTraefikServiceManifest {
		return ErrorMirrorTraefikServiceObj, nil
	}
	if f.IsGetMirrorTraefikServiceWithNotFoundField {
		return MirrorTraefikServiceWithNotFoundField, nil
	}

	if f.IsClientGetTraefikServiceError {
		return TraefikServiceObj, errors.New("Traefik get error")
	}
	if f.IsGetErrorTraefikServiceManifest {
		return ErrorTraefikServiceObj, nil
	}
	return TraefikServiceObj, nil
}
func (f *FakeIngressRouteClient) Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}
func (f *FakeServiceClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1.Service, error) {
	return nil, nil
}

func (f *FakeTraefikServiceClient) Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	if f.IsClientUpdateError {
		return obj, errors.New("Traefik update error")
	}
	return obj, nil
}
func (f *FakeIngressRouteClient) Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *FakeTraefikServiceClient) UpdateStatus(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *FakeTraefikServiceClient) Delete(ctx context.Context, name string, options metav1.DeleteOptions, subresources ...string) error {
	return nil
}
func (f *FakeIngressRouteClient) Delete(ctx context.Context, name string, options metav1.DeleteOptions, subresources ...string) error {
	return nil
}

func (f *FakeTraefikServiceClient) DeleteCollection(ctx context.Context, options metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	return nil
}

func (f *FakeTraefikServiceClient) List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return nil, nil
}

func (f *FakeTraefikServiceClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}

func (f *FakeTraefikServiceClient) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, options metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *FakeTraefikServiceClient) Namespace(string) dynamic.ResourceInterface {
	return f
}

func (f *FakeDynamicClient) Resource(schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return &FakeTraefikServiceClient{}
}

func (f *FakeTraefikServiceClient) Apply(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *FakeTraefikServiceClient) ApplyStatus(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}
