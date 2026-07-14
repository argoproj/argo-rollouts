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
	transform := makeTransform(func() *v1alpha1.Rollout { return &v1alpha1.Rollout{} })
	installTransform(delegate.Informer(), transform, "Rollout")
	return &tolerantRolloutInformer{delegate: delegate, transform: transform}
}

type tolerantRolloutInformer struct {
	delegate  informers.GenericInformer
	transform cache.TransformFunc
}

func (i *tolerantRolloutInformer) Informer() cache.SharedIndexInformer {
	return &transformingInformer{SharedIndexInformer: i.delegate.Informer(), transform: i.transform}
}

func (i *tolerantRolloutInformer) Lister() rolloutlisters.RolloutLister {
	return &tolerantRolloutLister{indexer: i.delegate.Informer().GetIndexer()}
}

type tolerantRolloutLister struct {
	indexer cache.Indexer
}

func (t *tolerantRolloutLister) List(selector labels.Selector) ([]*v1alpha1.Rollout, error) {
	return listTyped(t.indexer, "", selector,
		func() *v1alpha1.Rollout { return &v1alpha1.Rollout{} },
		func(ro *v1alpha1.Rollout) *v1alpha1.Rollout { return ro.DeepCopy() })
}

func (t *tolerantRolloutLister) Rollouts(namespace string) rolloutlisters.RolloutNamespaceLister {
	return &tolerantRolloutNamespaceLister{indexer: t.indexer, namespace: namespace}
}

type tolerantRolloutNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

func (t *tolerantRolloutNamespaceLister) Get(name string) (*v1alpha1.Rollout, error) {
	return getTyped(t.indexer, v1alpha1.Resource("rollout"), t.namespace, name,
		func() *v1alpha1.Rollout { return &v1alpha1.Rollout{} },
		func(ro *v1alpha1.Rollout) *v1alpha1.Rollout { return ro.DeepCopy() })
}

func (t *tolerantRolloutNamespaceLister) List(selector labels.Selector) ([]*v1alpha1.Rollout, error) {
	return listTyped(t.indexer, t.namespace, selector,
		func() *v1alpha1.Rollout { return &v1alpha1.Rollout{} },
		func(ro *v1alpha1.Rollout) *v1alpha1.Rollout { return ro.DeepCopy() })
}
