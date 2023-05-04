package mocks

import (
	"context"

	"github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"

	argoRecord "github.com/argoproj/argo-rollouts/utils/record"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/record"
)

type FakeDynamicClient struct {
	IsListError bool
}

type FakeClient struct {
	IsGetError                     bool
	IsGetErrorManifest             bool
	UpdateError                    bool
	IsListError                    bool
	IsGetNotFoundError             bool
	IsGetManagedRouteError         bool
	IsDuplicateSetHeaderRouteError bool
	IsDeleteError                  bool
	IsCreateError                  bool
	DeleteName                     string
	UpdatedObj                     *unstructured.Unstructured
	CreatedObj                     *unstructured.Unstructured
}

type FakeRecorder struct{}

var (
	ApisixRouteObj                   *unstructured.Unstructured
	SetHeaderApisixRouteObj          *unstructured.Unstructured
	DuplicateSetHeaderApisixRouteObj *unstructured.Unstructured
	ErrorApisixRouteObj              *unstructured.Unstructured
)

func (f *FakeRecorder) Eventf(object runtime.Object, opts argoRecord.EventOptions, messageFmt string, args ...interface{}) {
}

func (f *FakeRecorder) Warnf(object runtime.Object, opts argoRecord.EventOptions, messageFmt string, args ...interface{}) {
}

func (f *FakeRecorder) K8sRecorder() record.EventRecorder {
	return nil
}

func (f *FakeClient) Create(ctx context.Context, obj *unstructured.Unstructured, options metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	if f.IsCreateError {
		return nil, errors.New("create apisix route error!")
	}
	f.CreatedObj = obj
	return nil, nil
}

func (f *FakeClient) Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	if name == "mocks-apisix-route" {
		if f.IsGetError {
			return ApisixRouteObj, errors.New("Apisix get error")
		}
		if f.IsGetErrorManifest {
			return ErrorApisixRouteObj, nil
		}
		return ApisixRouteObj, nil
	} else if name == "set-header" {
		if f.IsGetNotFoundError {
			return nil, k8serrors.NewNotFound(schema.GroupResource{}, "set-header")
		}
		if f.IsGetManagedRouteError {
			return nil, errors.New("")
		}
		if f.IsDuplicateSetHeaderRouteError {
			return DuplicateSetHeaderApisixRouteObj, nil
		}
		return SetHeaderApisixRouteObj, nil
	}
	return nil, nil
}

func (f *FakeClient) Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	f.UpdatedObj = obj
	if f.UpdateError {
		return obj, errors.New("Apisix update error")
	}
	return obj, nil
}

func (f *FakeClient) UpdateStatus(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *FakeClient) Delete(ctx context.Context, name string, options metav1.DeleteOptions, subresources ...string) error {
	if f.IsDeleteError {
		return errors.New("delete apisixroute error!")
	}
	f.DeleteName = name
	return nil
}

func (f *FakeClient) DeleteCollection(ctx context.Context, options metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	return nil
}

func (f *FakeClient) List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	if f.IsListError {
		return nil, errors.New("Apisix list error")
	}
	return nil, nil
}

func (f *FakeClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}

func (f *FakeClient) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, options metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *FakeClient) Namespace(string) dynamic.ResourceInterface {
	return f
}

func (f *FakeDynamicClient) Resource(schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return &FakeClient{IsListError: f.IsListError}
}

func (f *FakeClient) Apply(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *FakeClient) ApplyStatus(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}
