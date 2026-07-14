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

func NewTolerantExperimentInformer(factory dynamicinformer.DynamicSharedInformerFactory) rolloutinformers.ExperimentInformer {
	delegate := factory.ForResource(v1alpha1.ExperimentGVR)
	newFn := func() *v1alpha1.Experiment { return &v1alpha1.Experiment{} }
	transform := makeTransform(newFn)
	installTransform(delegate.Informer(), transform, "Experiment")
	return &tolerantExperimentInformer{delegate: delegate, transform: transform, newFn: newFn}
}

type tolerantExperimentInformer struct {
	delegate  informers.GenericInformer
	transform cache.TransformFunc
	newFn     func() *v1alpha1.Experiment
}

func (i *tolerantExperimentInformer) Informer() cache.SharedIndexInformer {
	return &transformingInformer{SharedIndexInformer: i.delegate.Informer(), transform: i.transform}
}

func (i *tolerantExperimentInformer) Lister() rolloutlisters.ExperimentLister {
	return &tolerantExperimentLister{indexer: i.delegate.Informer().GetIndexer(), newFn: i.newFn}
}

type tolerantExperimentLister struct {
	indexer cache.Indexer
	newFn   func() *v1alpha1.Experiment
}

func (t *tolerantExperimentLister) List(selector labels.Selector) ([]*v1alpha1.Experiment, error) {
	return listTyped(t.indexer, "", selector, t.newFn)
}

func (t *tolerantExperimentLister) Experiments(namespace string) rolloutlisters.ExperimentNamespaceLister {
	return &tolerantExperimentNamespaceLister{indexer: t.indexer, namespace: namespace, newFn: t.newFn}
}

type tolerantExperimentNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
	newFn     func() *v1alpha1.Experiment
}

func (t *tolerantExperimentNamespaceLister) Get(name string) (*v1alpha1.Experiment, error) {
	return getTyped(t.indexer, v1alpha1.Resource("experiment"), t.namespace, name, t.newFn)
}

func (t *tolerantExperimentNamespaceLister) List(selector labels.Selector) ([]*v1alpha1.Experiment, error) {
	return listTyped(t.indexer, t.namespace, selector, t.newFn)
}
