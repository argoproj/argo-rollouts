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
	installTransform(delegate.Informer(),
		makeTransform(func() *v1alpha1.AnalysisRun { return &v1alpha1.AnalysisRun{} }),
		"AnalysisRun")
	return &tolerantAnalysisRunInformer{delegate: delegate}
}

type tolerantAnalysisRunInformer struct {
	delegate informers.GenericInformer
}

func (i *tolerantAnalysisRunInformer) Informer() cache.SharedIndexInformer {
	return i.delegate.Informer()
}

func (i *tolerantAnalysisRunInformer) Lister() rolloutlisters.AnalysisRunLister {
	return &tolerantAnalysisRunLister{
		delegate: rolloutlisters.NewAnalysisRunLister(i.delegate.Informer().GetIndexer()),
	}
}

// tolerantAnalysisRunLister wraps the generated typed lister and deep-copies each
// returned object so callers can safely mutate the result without corrupting the
// shared informer cache. The SetTransform conversion still runs only once per
// object change, so the dominant cost (unstructured→typed reflection) is paid
// once; the per-call cost here is a fast generated DeepCopy.
type tolerantAnalysisRunLister struct {
	delegate rolloutlisters.AnalysisRunLister
}

func (t *tolerantAnalysisRunLister) List(selector labels.Selector) ([]*v1alpha1.AnalysisRun, error) {
	items, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	out := make([]*v1alpha1.AnalysisRun, len(items))
	for i, ar := range items {
		out[i] = ar.DeepCopy()
	}
	return out, nil
}

func (t *tolerantAnalysisRunLister) AnalysisRuns(namespace string) rolloutlisters.AnalysisRunNamespaceLister {
	return &tolerantAnalysisRunNamespaceLister{
		delegate: t.delegate.AnalysisRuns(namespace),
	}
}

type tolerantAnalysisRunNamespaceLister struct {
	delegate rolloutlisters.AnalysisRunNamespaceLister
}

func (t *tolerantAnalysisRunNamespaceLister) Get(name string) (*v1alpha1.AnalysisRun, error) {
	ar, err := t.delegate.Get(name)
	if err != nil {
		return nil, err
	}
	return ar.DeepCopy(), nil
}

func (t *tolerantAnalysisRunNamespaceLister) List(selector labels.Selector) ([]*v1alpha1.AnalysisRun, error) {
	items, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	out := make([]*v1alpha1.AnalysisRun, len(items))
	for i, ar := range items {
		out[i] = ar.DeepCopy()
	}
	return out, nil
}
