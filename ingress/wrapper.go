package ingress

import (
	"context"
	"sync"

	"k8s.io/api/extensions/v1beta1"
	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	mux           sync.Mutex
}

func NewIngress(i *v1.Ingress) *Ingress {
	return &Ingress{
		ingress: i,
		mode:    IngressModeNetworking,
	}
}

func NewLegacyIngress(li *v1beta1.Ingress) *Ingress {
	return &Ingress{
		legacyIngress: li,
		mode:          IngressModeExtensions,
	}
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
	IngressModeExtensions IngressMode = iota
	IngressModeNetworking
)

func NewIngressWrapper(mode IngressMode, client kubernetes.Interface, informerFactory informers.SharedInformerFactory) *IngressWrap {
	var ingressInformer networkingv1.IngressInformer
	var legacyIngressInformer extensionsv1beta1.IngressInformer
	if mode == IngressModeNetworking {
		ingressInformer = informerFactory.Networking().V1().Ingresses()
	} else {
		legacyIngressInformer = informerFactory.Extensions().V1beta1().Ingresses()
	}
	return &IngressWrap{
		client:                client,
		mode:                  mode,
		ingressInformer:       ingressInformer,
		legacyIngressInformer: legacyIngressInformer,
	}
}

func (w *IngressWrap) Informer() cache.SharedIndexInformer {
	if w.legacyIngressInformer != nil {
		return w.legacyIngressInformer.Informer()
	}
	return w.ingressInformer.Informer()
}

func (w *IngressWrap) Update(ctx context.Context, namespace string, ingress *Ingress) (*Ingress, error) {
	if w.mode == IngressModeNetworking {
		return w.update(ctx, namespace, ingress)
	}
	return w.legacyUpdate(ctx, namespace, ingress)
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

func (w *IngressWrap) Get(namespace, name string) (*Ingress, error) {
	if w.legacyIngressInformer != nil {
		return w.getLegacy(namespace, name)
	}
	return w.get(namespace, name)
}

func (w *IngressWrap) get(namespace, name string) (*Ingress, error) {
	ing, err := w.ingressInformer.Lister().Ingresses(namespace).Get(name)
	if err != nil {
		return nil, err
	}
	return NewIngress(ing), nil
}
func (w *IngressWrap) getLegacy(namespace, name string) (*Ingress, error) {
	li, err := w.legacyIngressInformer.Lister().Ingresses(namespace).Get(name)
	if err != nil {
		return nil, err
	}
	return NewLegacyIngress(li), nil
}
