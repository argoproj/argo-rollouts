package ingress

import (
	"context"
	"errors"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	extensionsv1beta1 "k8s.io/client-go/informers/extensions/v1beta1"
	networkingv1 "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Ingress defines an Ingress resource abstraction used to allow Rollouts to
// work with the newer 'networking' package as well as with the legacy extensions
// package.
type Ingress struct {
	ingress       *v1.Ingress
	legacyIngress *v1beta1.Ingress
	mode          IngressMode
	mux           *sync.Mutex
}

// NewIngress will instantiate and return an Ingress with the given
// Ingress from networking/v1 package
func NewIngress(i *v1.Ingress) *Ingress {
	return &Ingress{
		ingress: i,
		mode:    IngressModeNetworking,
		mux:     &sync.Mutex{},
	}
}

// NewLegacyIngress will instantiate and return an Ingress with the given
// Ingress from extensions/v1beta1 package
func NewLegacyIngress(li *v1beta1.Ingress) *Ingress {
	return &Ingress{
		legacyIngress: li,
		mode:          IngressModeExtensions,
		mux:           &sync.Mutex{},
	}
}

func NewIngressWithAnnotations(mode IngressMode, annotations map[string]string) *Ingress {
	switch mode {
	case IngressModeNetworking:
		i := &v1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: annotations,
			},
		}
		return NewIngress(i)
	case IngressModeExtensions:
		i := &v1beta1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: annotations,
			},
		}
		return NewLegacyIngress(i)
	default:
		return nil
	}
}

func (i *Ingress) GetExtensionsIngress() (*v1beta1.Ingress, error) {
	if i.legacyIngress == nil {
		return nil, errors.New("extensions Ingress is nil in this wrapper")
	}
	return i.legacyIngress, nil
}

func (i *Ingress) GetNetworkingIngress() (*v1.Ingress, error) {
	if i.ingress == nil {
		return nil, errors.New("networking Ingress is nil in this wrapper")
	}
	return i.ingress, nil
}

func (i *Ingress) GetAnnotations() map[string]string {
	i.mux.Lock()
	defer i.mux.Unlock()
	switch i.mode {
	case IngressModeNetworking:
		return i.ingress.GetAnnotations()
	case IngressModeExtensions:
		return i.legacyIngress.GetAnnotations()
	default:
		return make(map[string]string)
	}
}

func (i *Ingress) GetLabels() map[string]string {
	switch i.mode {
	case IngressModeNetworking:
		return i.ingress.GetLabels()
	case IngressModeExtensions:
		return i.legacyIngress.GetLabels()
	default:
		return make(map[string]string)
	}
}

func (i *Ingress) GetObjectMeta() metav1.Object {
	switch i.mode {
	case IngressModeNetworking:
		return i.ingress.GetObjectMeta()
	case IngressModeExtensions:
		return i.legacyIngress.GetObjectMeta()
	default:
		return nil
	}
}

func (i *Ingress) SetAnnotations(annotations map[string]string) {
	i.mux.Lock()
	defer i.mux.Unlock()
	switch i.mode {
	case IngressModeNetworking:
		i.ingress.SetAnnotations(annotations)
	case IngressModeExtensions:
		i.legacyIngress.SetAnnotations(annotations)
	}
}

func (i *Ingress) DeepCopy() *Ingress {
	switch i.mode {
	case IngressModeNetworking:
		ing := i.ingress.DeepCopy()
		return NewIngress(ing)
	case IngressModeExtensions:
		ing := i.legacyIngress.DeepCopy()
		return NewLegacyIngress(ing)
	default:
		return nil
	}
}

func (i *Ingress) GetName() string {
	switch i.mode {
	case IngressModeNetworking:
		return i.ingress.GetName()
	case IngressModeExtensions:
		return i.legacyIngress.GetName()
	default:
		return ""
	}
}

func (i *Ingress) GetNamespace() string {
	switch i.mode {
	case IngressModeNetworking:
		return i.ingress.GetNamespace()
	case IngressModeExtensions:
		return i.legacyIngress.GetNamespace()
	default:
		return ""
	}
}

func (i *Ingress) GetLoadBalancerStatus() corev1.LoadBalancerStatus {
	switch i.mode {
	case IngressModeNetworking:
		return i.ingress.Status.LoadBalancer
	case IngressModeExtensions:
		return i.legacyIngress.Status.LoadBalancer
	default:
		return corev1.LoadBalancerStatus{}
	}
}

func (i *Ingress) Mode() IngressMode {
	return i.mode
}

// IngressWrap wraps the two ingress informers provided by the client-go. This is used
// to centralize the ingress informer operations to allow Rollouts to interact with
// both versions.
type IngressWrap struct {
	client                kubernetes.Interface
	mode                  IngressMode
	ingressInformer       networkingv1.IngressInformer
	legacyIngressInformer extensionsv1beta1.IngressInformer
}

type IngressMode int

const (
	IngressModeExtensions IngressMode = iota + 1 // start iota with 1 to avoid having this as default value
	IngressModeNetworking
)

func NewIngressWrapper(mode IngressMode, client kubernetes.Interface, informerFactory informers.SharedInformerFactory) (*IngressWrap, error) {
	var ingressInformer networkingv1.IngressInformer
	var legacyIngressInformer extensionsv1beta1.IngressInformer
	switch mode {
	case IngressModeNetworking:
		ingressInformer = informerFactory.Networking().V1().Ingresses()
	case IngressModeExtensions:
		legacyIngressInformer = informerFactory.Extensions().V1beta1().Ingresses()
	default:
		return nil, errors.New("error creating ingress wrapper: undefined ingress mode")
	}
	return &IngressWrap{
		client:                client,
		mode:                  mode,
		ingressInformer:       ingressInformer,
		legacyIngressInformer: legacyIngressInformer,
	}, nil
}

func (w *IngressWrap) Informer() cache.SharedIndexInformer {
	switch w.mode {
	case IngressModeNetworking:
		return w.ingressInformer.Informer()
	case IngressModeExtensions:
		return w.legacyIngressInformer.Informer()
	default:
		return nil
	}
}

func (w *IngressWrap) Patch(ctx context.Context, namespace, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*Ingress, error) {
	switch w.mode {
	case IngressModeNetworking:
		return w.patch(ctx, namespace, name, pt, data, opts, subresources...)
	case IngressModeExtensions:
		return w.patchLegacy(ctx, namespace, name, pt, data, opts, subresources...)
	default:
		return nil, errors.New("ingress patch error: undefined ingress mode")
	}
}

func (w *IngressWrap) patch(ctx context.Context, namespace, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*Ingress, error) {
	i, err := w.client.NetworkingV1().Ingresses(namespace).Patch(ctx, name, pt, data, opts, subresources...)
	if err != nil {
		return nil, err
	}
	return NewIngress(i), nil
}

func (w *IngressWrap) patchLegacy(ctx context.Context, namespace, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*Ingress, error) {
	li, err := w.client.ExtensionsV1beta1().Ingresses(namespace).Patch(ctx, name, pt, data, opts, subresources...)
	if err != nil {
		return nil, err
	}
	return NewLegacyIngress(li), nil
}

func (w *IngressWrap) Update(ctx context.Context, namespace string, ingress *Ingress) (*Ingress, error) {
	switch w.mode {
	case IngressModeNetworking:
		return w.update(ctx, namespace, ingress)
	case IngressModeExtensions:
		return w.legacyUpdate(ctx, namespace, ingress)
	default:
		return nil, errors.New("error updating ingress: undefined ingress mode")
	}
}

func (w *IngressWrap) update(ctx context.Context, namespace string, ingress *Ingress) (*Ingress, error) {
	i, err := w.client.NetworkingV1().Ingresses(namespace).Update(ctx, ingress.ingress, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	return NewIngress(i), nil
}

func (w *IngressWrap) legacyUpdate(ctx context.Context, namespace string, ingress *Ingress) (*Ingress, error) {
	li, err := w.client.ExtensionsV1beta1().Ingresses(namespace).Update(ctx, ingress.legacyIngress, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	return NewLegacyIngress(li), nil
}
func (w *IngressWrap) Get(ctx context.Context, namespace, name string, opts metav1.GetOptions) (*Ingress, error) {
	switch w.mode {
	case IngressModeNetworking:
		return w.get(ctx, namespace, name, opts)
	case IngressModeExtensions:
		return w.getLegacy(ctx, namespace, name, opts)
	default:
		return nil, errors.New("error running IngressWrap.Get: undefined ingress mode")
	}
}

func (w *IngressWrap) get(ctx context.Context, namespace, name string, opts metav1.GetOptions) (*Ingress, error) {
	ing, err := w.client.NetworkingV1().Ingresses(namespace).Get(ctx, name, opts)
	if err != nil {
		return nil, err
	}
	return NewIngress(ing), nil
}

func (w *IngressWrap) getLegacy(ctx context.Context, namespace, name string, opts metav1.GetOptions) (*Ingress, error) {
	ing, err := w.client.ExtensionsV1beta1().Ingresses(namespace).Get(ctx, name, opts)
	if err != nil {
		return nil, err
	}
	return NewLegacyIngress(ing), nil
}

func (w *IngressWrap) GetCached(namespace, name string) (*Ingress, error) {
	switch w.mode {
	case IngressModeNetworking:
		return w.getCached(namespace, name)
	case IngressModeExtensions:
		return w.getCachedLegacy(namespace, name)
	default:
		return nil, errors.New("error running IngressWrap.GetCached: undefined ingress mode")
	}
}

func (w *IngressWrap) getCached(namespace, name string) (*Ingress, error) {
	ing, err := w.ingressInformer.Lister().Ingresses(namespace).Get(name)
	if err != nil {
		return nil, err
	}
	return NewIngress(ing), nil
}
func (w *IngressWrap) getCachedLegacy(namespace, name string) (*Ingress, error) {
	li, err := w.legacyIngressInformer.Lister().Ingresses(namespace).Get(name)
	if err != nil {
		return nil, err
	}
	return NewLegacyIngress(li), nil
}

func (w *IngressWrap) Create(ctx context.Context, namespace string, ingress *Ingress, opts metav1.CreateOptions) (*Ingress, error) {
	switch w.mode {
	case IngressModeNetworking:
		return w.create(ctx, namespace, ingress.ingress, opts)
	case IngressModeExtensions:
		return w.createLegacy(ctx, namespace, ingress.legacyIngress, opts)
	default:
		return nil, errors.New("error creating ingress: undefined ingress mode")
	}
}

func (w *IngressWrap) create(ctx context.Context, namespace string, ingress *v1.Ingress, opts metav1.CreateOptions) (*Ingress, error) {
	i, err := w.client.NetworkingV1().Ingresses(namespace).Create(ctx, ingress, opts)
	if err != nil {
		return nil, err
	}
	return NewIngress(i), nil
}

func (w *IngressWrap) createLegacy(ctx context.Context, namespace string, ingress *v1beta1.Ingress, opts metav1.CreateOptions) (*Ingress, error) {
	li, err := w.client.ExtensionsV1beta1().Ingresses(namespace).Create(ctx, ingress, opts)
	if err != nil {
		return nil, err
	}
	return NewLegacyIngress(li), nil
}

func (w *IngressWrap) HasSynced() bool {
	switch w.mode {
	case IngressModeNetworking:
		return w.ingressInformer.Informer().HasSynced()
	case IngressModeExtensions:
		return w.legacyIngressInformer.Informer().HasSynced()
	default:
		return false
	}
}
