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
	transform := makeTransform(func() *v1alpha1.AnalysisRun { return &v1alpha1.AnalysisRun{} })
	installTransform(delegate.Informer(), transform, "AnalysisRun")
	return &tolerantAnalysisRunInformer{delegate: delegate, transform: transform}
}

type tolerantAnalysisRunInformer struct {
	delegate  informers.GenericInformer
	transform cache.TransformFunc
}

func (i *tolerantAnalysisRunInformer) Informer() cache.SharedIndexInformer {
	return &transformingInformer{SharedIndexInformer: i.delegate.Informer(), transform: i.transform}
}

func (i *tolerantAnalysisRunInformer) Lister() rolloutlisters.AnalysisRunLister {
	return &tolerantAnalysisRunLister{indexer: i.delegate.Informer().GetIndexer()}
}

// tolerantAnalysisRunLister lists from the indexer and deep-copies each result so
// callers can safely mutate without corrupting the shared cache. Objects are
// coerced from either the typed form (SetTransform path) or *unstructured.Unstructured
// (direct store writes that bypass SetTransform).
type tolerantAnalysisRunLister struct {
	indexer cache.Indexer
}

func (t *tolerantAnalysisRunLister) List(selector labels.Selector) ([]*v1alpha1.AnalysisRun, error) {
	return listTyped(t.indexer, "", selector,
		func() *v1alpha1.AnalysisRun { return &v1alpha1.AnalysisRun{} },
		func(ar *v1alpha1.AnalysisRun) *v1alpha1.AnalysisRun { return ar.DeepCopy() })
}

func (t *tolerantAnalysisRunLister) AnalysisRuns(namespace string) rolloutlisters.AnalysisRunNamespaceLister {
	return &tolerantAnalysisRunNamespaceLister{indexer: t.indexer, namespace: namespace}
}

type tolerantAnalysisRunNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

func (t *tolerantAnalysisRunNamespaceLister) Get(name string) (*v1alpha1.AnalysisRun, error) {
	return getTyped(t.indexer, v1alpha1.Resource("analysisrun"), t.namespace, name,
		func() *v1alpha1.AnalysisRun { return &v1alpha1.AnalysisRun{} },
		func(ar *v1alpha1.AnalysisRun) *v1alpha1.AnalysisRun { return ar.DeepCopy() })
}

func (t *tolerantAnalysisRunNamespaceLister) List(selector labels.Selector) ([]*v1alpha1.AnalysisRun, error) {
	return listTyped(t.indexer, t.namespace, selector,
		func() *v1alpha1.AnalysisRun { return &v1alpha1.AnalysisRun{} },
		func(ar *v1alpha1.AnalysisRun) *v1alpha1.AnalysisRun { return ar.DeepCopy() })
}
