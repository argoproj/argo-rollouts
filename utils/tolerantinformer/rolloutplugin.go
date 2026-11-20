package tolerantinformer

import (
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutinformers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	rolloutlisters "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
)

func NewTolerantRolloutPluginInformer(factory dynamicinformer.DynamicSharedInformerFactory) rolloutinformers.RolloutPluginInformer {
	delegate := factory.ForResource(v1alpha1.RolloutPluginGVR)
	newFn := func() *v1alpha1.RolloutPlugin { return &v1alpha1.RolloutPlugin{} }
	transform := makeTransform(newFn)
	installTransform(delegate.Informer(), transform, "RolloutPlugin")
	return &tolerantRolloutPluginInformer{
		delegate:  delegate,
		transform: transform,
		newFn:     newFn,
	}
}

type tolerantRolloutPluginInformer struct {
	delegate  informers.GenericInformer
	transform cache.TransformFunc
	newFn     func() *v1alpha1.RolloutPlugin
}

func (i *tolerantRolloutPluginInformer) Informer() cache.SharedIndexInformer {
	return &transformingInformer{SharedIndexInformer: i.delegate.Informer(), transform: i.transform}
}

func (i *tolerantRolloutPluginInformer) Lister() rolloutlisters.RolloutPluginLister {
	return &tolerantRolloutPluginLister{indexer: i.delegate.Informer().GetIndexer(), newFn: i.newFn}
}

type tolerantRolloutPluginLister struct {
	indexer cache.Indexer
	newFn   func() *v1alpha1.RolloutPlugin
}

func (t *tolerantRolloutPluginLister) List(selector labels.Selector) ([]*v1alpha1.RolloutPlugin, error) {
	return listTyped(t.indexer, "", selector, t.newFn)
}

func (t *tolerantRolloutPluginLister) RolloutPlugins(namespace string) rolloutlisters.RolloutPluginNamespaceLister {
	return &tolerantRolloutPluginNamespaceLister{indexer: t.indexer, namespace: namespace, newFn: t.newFn}
}

type tolerantRolloutPluginNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
	newFn     func() *v1alpha1.RolloutPlugin
}

func (t *tolerantRolloutPluginNamespaceLister) Get(name string) (*v1alpha1.RolloutPlugin, error) {
	return getTyped(t.indexer, v1alpha1.Resource("rolloutplugin"), t.namespace, name, t.newFn)
}

func (t *tolerantRolloutPluginNamespaceLister) List(selector labels.Selector) ([]*v1alpha1.RolloutPlugin, error) {
	return listTyped(t.indexer, t.namespace, selector, t.newFn)
}
