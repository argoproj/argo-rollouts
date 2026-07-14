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
	transform := makeTransform(func() *v1alpha1.Experiment { return &v1alpha1.Experiment{} })
	installTransform(delegate.Informer(), transform, "Experiment")
	return &tolerantExperimentInformer{delegate: delegate, transform: transform}
}

type tolerantExperimentInformer struct {
	delegate  informers.GenericInformer
	transform cache.TransformFunc
}

func (i *tolerantExperimentInformer) Informer() cache.SharedIndexInformer {
	return &transformingInformer{SharedIndexInformer: i.delegate.Informer(), transform: i.transform}
}

func (i *tolerantExperimentInformer) Lister() rolloutlisters.ExperimentLister {
	return &tolerantExperimentLister{indexer: i.delegate.Informer().GetIndexer()}
}

type tolerantExperimentLister struct {
	indexer cache.Indexer
}

func (t *tolerantExperimentLister) List(selector labels.Selector) ([]*v1alpha1.Experiment, error) {
	return listTyped(t.indexer, "", selector,
		func() *v1alpha1.Experiment { return &v1alpha1.Experiment{} },
		func(ex *v1alpha1.Experiment) *v1alpha1.Experiment { return ex.DeepCopy() })
}

func (t *tolerantExperimentLister) Experiments(namespace string) rolloutlisters.ExperimentNamespaceLister {
	return &tolerantExperimentNamespaceLister{indexer: t.indexer, namespace: namespace}
}

type tolerantExperimentNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

func (t *tolerantExperimentNamespaceLister) Get(name string) (*v1alpha1.Experiment, error) {
	return getTyped(t.indexer, v1alpha1.Resource("experiment"), t.namespace, name,
		func() *v1alpha1.Experiment { return &v1alpha1.Experiment{} },
		func(ex *v1alpha1.Experiment) *v1alpha1.Experiment { return ex.DeepCopy() })
}

func (t *tolerantExperimentNamespaceLister) List(selector labels.Selector) ([]*v1alpha1.Experiment, error) {
	return listTyped(t.indexer, t.namespace, selector,
		func() *v1alpha1.Experiment { return &v1alpha1.Experiment{} },
		func(ex *v1alpha1.Experiment) *v1alpha1.Experiment { return ex.DeepCopy() })
}
