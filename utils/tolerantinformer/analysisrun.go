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

func NewTolerantAnalysisRunInformer(factory dynamicinformer.DynamicSharedInformerFactory) rolloutinformers.AnalysisRunInformer {
	delegate := factory.ForResource(v1alpha1.AnalysisRunGVR)
	newFn := func() *v1alpha1.AnalysisRun { return &v1alpha1.AnalysisRun{} }
	transform := makeTransform(newFn)
	installTransform(delegate.Informer(), transform, "AnalysisRun")
	return &tolerantAnalysisRunInformer{delegate: delegate, transform: transform, newFn: newFn}
}

type tolerantAnalysisRunInformer struct {
	delegate  informers.GenericInformer
	transform cache.TransformFunc
	newFn     func() *v1alpha1.AnalysisRun
}

func (i *tolerantAnalysisRunInformer) Informer() cache.SharedIndexInformer {
	return &transformingInformer{SharedIndexInformer: i.delegate.Informer(), transform: i.transform}
}

func (i *tolerantAnalysisRunInformer) Lister() rolloutlisters.AnalysisRunLister {
	return &tolerantAnalysisRunLister{indexer: i.delegate.Informer().GetIndexer(), newFn: i.newFn}
}

// tolerantAnalysisRunLister lists from the indexer and deep-copies each result so
// callers can safely mutate without corrupting the shared cache. Objects are
// coerced from either the typed form (SetTransform path) or *unstructured.Unstructured
// (direct store writes that bypass SetTransform).
type tolerantAnalysisRunLister struct {
	indexer cache.Indexer
	newFn   func() *v1alpha1.AnalysisRun
}

func (t *tolerantAnalysisRunLister) List(selector labels.Selector) ([]*v1alpha1.AnalysisRun, error) {
	return listTyped(t.indexer, "", selector, t.newFn)
}

func (t *tolerantAnalysisRunLister) AnalysisRuns(namespace string) rolloutlisters.AnalysisRunNamespaceLister {
	return &tolerantAnalysisRunNamespaceLister{indexer: t.indexer, namespace: namespace, newFn: t.newFn}
}

type tolerantAnalysisRunNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
	newFn     func() *v1alpha1.AnalysisRun
}

func (t *tolerantAnalysisRunNamespaceLister) Get(name string) (*v1alpha1.AnalysisRun, error) {
	return getTyped(t.indexer, v1alpha1.Resource("analysisrun"), t.namespace, name, t.newFn)
}

func (t *tolerantAnalysisRunNamespaceLister) List(selector labels.Selector) ([]*v1alpha1.AnalysisRun, error) {
	return listTyped(t.indexer, t.namespace, selector, t.newFn)
}
