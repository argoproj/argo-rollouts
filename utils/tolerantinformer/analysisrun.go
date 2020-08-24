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

func NewTolerantAnalysisRunInformer(factory dynamicinformer.DynamicSharedInformerFactory) rolloutinformers.AnalysisRunInformer {
	return &tolerantAnalysisRunInformer{
		delegate: factory.ForResource(v1alpha1.AnalysisRunGVR),
	}
}

type tolerantAnalysisRunInformer struct {
	delegate informers.GenericInformer
}

func (i *tolerantAnalysisRunInformer) Informer() cache.SharedIndexInformer {
	return i.delegate.Informer()
}

func (i *tolerantAnalysisRunInformer) Lister() rolloutlisters.AnalysisRunLister {
	return &tolerantAnalysisRunLister{
		delegate: i.delegate.Lister(),
	}
}

type tolerantAnalysisRunLister struct {
	delegate cache.GenericLister
}

func (t *tolerantAnalysisRunLister) List(selector labels.Selector) ([]*v1alpha1.AnalysisRun, error) {
	objects, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	return convertObjectsToAnalysisRuns(objects)
}

func (t *tolerantAnalysisRunLister) AnalysisRuns(namespace string) rolloutlisters.AnalysisRunNamespaceLister {
	return &tolerantAnalysisRunNamespaceLister{
		delegate: t.delegate.ByNamespace(namespace),
	}
}

type tolerantAnalysisRunNamespaceLister struct {
	delegate cache.GenericNamespaceLister
}

func (t *tolerantAnalysisRunNamespaceLister) Get(name string) (*v1alpha1.AnalysisRun, error) {
	object, err := t.delegate.Get(name)
	if err != nil {
		return nil, err
	}
	v := &v1alpha1.AnalysisRun{}
	err = convertObject(object, v)
	return v, err
}

func (t *tolerantAnalysisRunNamespaceLister) List(selector labels.Selector) ([]*v1alpha1.AnalysisRun, error) {
	objects, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	return convertObjectsToAnalysisRuns(objects)
}

func convertObjectsToAnalysisRuns(objects []runtime.Object) ([]*v1alpha1.AnalysisRun, error) {
	var firstErr error
	vs := make([]*v1alpha1.AnalysisRun, len(objects))
	for i, obj := range objects {
		vs[i] = &v1alpha1.AnalysisRun{}
		err := convertObject(obj, vs[i])
		if err != nil && firstErr != nil {
			firstErr = err
		}
	}
	return vs, firstErr
}
