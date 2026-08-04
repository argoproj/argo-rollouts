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

func NewTolerantRolloutInformer(factory dynamicinformer.DynamicSharedInformerFactory) rolloutinformers.RolloutInformer {
	delegate := factory.ForResource(v1alpha1.RolloutGVR)
	newFn := func() *v1alpha1.Rollout { return &v1alpha1.Rollout{} }
	transform := makeTransform(newFn)
	installTransform(delegate.Informer(), transform, "Rollout")
	return &tolerantRolloutInformer{delegate: delegate, transform: transform, newFn: newFn}
}

type tolerantRolloutInformer struct {
	delegate  informers.GenericInformer
	transform cache.TransformFunc
	newFn     func() *v1alpha1.Rollout
}

func (i *tolerantRolloutInformer) Informer() cache.SharedIndexInformer {
	return &transformingInformer{SharedIndexInformer: i.delegate.Informer(), transform: i.transform}
}

func (i *tolerantRolloutInformer) Lister() rolloutlisters.RolloutLister {
	return &tolerantRolloutLister{indexer: i.delegate.Informer().GetIndexer(), newFn: i.newFn}
}

type tolerantRolloutLister struct {
	indexer cache.Indexer
	newFn   func() *v1alpha1.Rollout
}

func (t *tolerantRolloutLister) List(selector labels.Selector) ([]*v1alpha1.Rollout, error) {
	return listTyped(t.indexer, "", selector, t.newFn)
}

func (t *tolerantRolloutLister) Rollouts(namespace string) rolloutlisters.RolloutNamespaceLister {
	return &tolerantRolloutNamespaceLister{indexer: t.indexer, namespace: namespace, newFn: t.newFn}
}

type tolerantRolloutNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
	newFn     func() *v1alpha1.Rollout
}

func (t *tolerantRolloutNamespaceLister) Get(name string) (*v1alpha1.Rollout, error) {
	return getTyped(t.indexer, v1alpha1.Resource("rollout"), t.namespace, name, t.newFn)
}

func (t *tolerantRolloutNamespaceLister) List(selector labels.Selector) ([]*v1alpha1.Rollout, error) {
	return listTyped(t.indexer, t.namespace, selector, t.newFn)
}
