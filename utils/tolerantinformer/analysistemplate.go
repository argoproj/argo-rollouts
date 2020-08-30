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

func NewTolerantAnalysisTemplateInformer(factory dynamicinformer.DynamicSharedInformerFactory) rolloutinformers.AnalysisTemplateInformer {
	return &tolerantAnalysisTemplateInformer{
		delegate: factory.ForResource(v1alpha1.AnalysisTemplateGVR),
	}
}

type tolerantAnalysisTemplateInformer struct {
	delegate informers.GenericInformer
}

func (i *tolerantAnalysisTemplateInformer) Informer() cache.SharedIndexInformer {
	return i.delegate.Informer()
}

func (i *tolerantAnalysisTemplateInformer) Lister() rolloutlisters.AnalysisTemplateLister {
	return &tolerantAnalysisTemplateLister{
		delegate: i.delegate.Lister(),
	}
}

type tolerantAnalysisTemplateLister struct {
	delegate cache.GenericLister
}

func (t *tolerantAnalysisTemplateLister) List(selector labels.Selector) ([]*v1alpha1.AnalysisTemplate, error) {
	objects, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	return convertObjectsToAnalysisTemplates(objects)
}

func (t *tolerantAnalysisTemplateLister) AnalysisTemplates(namespace string) rolloutlisters.AnalysisTemplateNamespaceLister {
	return &tolerantAnalysisTemplateNamespaceLister{
		delegate: t.delegate.ByNamespace(namespace),
	}
}

type tolerantAnalysisTemplateNamespaceLister struct {
	delegate cache.GenericNamespaceLister
}

func (t *tolerantAnalysisTemplateNamespaceLister) Get(name string) (*v1alpha1.AnalysisTemplate, error) {
	object, err := t.delegate.Get(name)
	if err != nil {
		return nil, err
	}
	v := &v1alpha1.AnalysisTemplate{}
	err = convertObject(object, v)
	return v, err
}

func (t *tolerantAnalysisTemplateNamespaceLister) List(selector labels.Selector) ([]*v1alpha1.AnalysisTemplate, error) {
	objects, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	return convertObjectsToAnalysisTemplates(objects)
}

func convertObjectsToAnalysisTemplates(objects []runtime.Object) ([]*v1alpha1.AnalysisTemplate, error) {
	var firstErr error
	vs := make([]*v1alpha1.AnalysisTemplate, len(objects))
	for i, obj := range objects {
		vs[i] = &v1alpha1.AnalysisTemplate{}
		err := convertObject(obj, vs[i])
		if err != nil && firstErr != nil {
			firstErr = err
		}
	}
	return vs, firstErr
}
