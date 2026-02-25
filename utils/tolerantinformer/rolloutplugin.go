package tolerantinformer

import (
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutinformers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	rolloutlisters "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
)

func NewTolerantRolloutPluginInformer(factory dynamicinformer.DynamicSharedInformerFactory) rolloutinformers.RolloutPluginInformer {
	return &tolerantRolloutPluginInformer{
		delegate: factory.ForResource(v1alpha1.RolloutPluginGVR),
	}
}

type tolerantRolloutPluginInformer struct {
	delegate informers.GenericInformer
}

func (i *tolerantRolloutPluginInformer) Informer() cache.SharedIndexInformer {
	return i.delegate.Informer()
}

func (i *tolerantRolloutPluginInformer) Lister() rolloutlisters.RolloutPluginLister {
	return &tolerantRolloutPluginLister{
		delegate: i.delegate.Lister(),
	}
}

type tolerantRolloutPluginLister struct {
	delegate cache.GenericLister
}

func (t *tolerantRolloutPluginLister) List(selector labels.Selector) ([]*v1alpha1.RolloutPlugin, error) {
	objects, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	return convertObjectsToRolloutPlugins(objects)
}

func (t *tolerantRolloutPluginLister) RolloutPlugins(namespace string) rolloutlisters.RolloutPluginNamespaceLister {
	return &tolerantRolloutPluginNamespaceLister{
		delegate: t.delegate.ByNamespace(namespace),
	}
}

type tolerantRolloutPluginNamespaceLister struct {
	delegate cache.GenericNamespaceLister
}

func (t *tolerantRolloutPluginNamespaceLister) Get(name string) (*v1alpha1.RolloutPlugin, error) {
	object, err := t.delegate.Get(name)
	if err != nil {
		return nil, err
	}
	v := &v1alpha1.RolloutPlugin{}
	err = convertObject(object, v)
	return v, err
}

func (t *tolerantRolloutPluginNamespaceLister) List(selector labels.Selector) ([]*v1alpha1.RolloutPlugin, error) {
	objects, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	return convertObjectsToRolloutPlugins(objects)
}

func convertObjectsToRolloutPlugins(objects []runtime.Object) ([]*v1alpha1.RolloutPlugin, error) {
	var firstErr error
	vs := make([]*v1alpha1.RolloutPlugin, len(objects))
	for i, obj := range objects {
		vs[i] = &v1alpha1.RolloutPlugin{}
		err := convertObject(obj, vs[i])
		if err != nil && firstErr != nil {
			firstErr = err
		}
	}
	return vs, firstErr
}
