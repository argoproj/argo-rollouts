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

func NewTolerantAnalysisTemplateInformer(factory dynamicinformer.DynamicSharedInformerFactory) rolloutinformers.AnalysisTemplateInformer {
	delegate := factory.ForResource(v1alpha1.AnalysisTemplateGVR)
	installTransform(delegate.Informer(),
		makeTransform(func() *v1alpha1.AnalysisTemplate { return &v1alpha1.AnalysisTemplate{} }),
		"AnalysisTemplate")
	return &tolerantAnalysisTemplateInformer{delegate: delegate}
}

type tolerantAnalysisTemplateInformer struct {
	delegate informers.GenericInformer
}

func (i *tolerantAnalysisTemplateInformer) Informer() cache.SharedIndexInformer {
	return i.delegate.Informer()
}

func (i *tolerantAnalysisTemplateInformer) Lister() rolloutlisters.AnalysisTemplateLister {
	return &tolerantAnalysisTemplateLister{
		delegate: rolloutlisters.NewAnalysisTemplateLister(i.delegate.Informer().GetIndexer()),
	}
}

type tolerantAnalysisTemplateLister struct {
	delegate rolloutlisters.AnalysisTemplateLister
}

func (t *tolerantAnalysisTemplateLister) List(selector labels.Selector) ([]*v1alpha1.AnalysisTemplate, error) {
	items, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	out := make([]*v1alpha1.AnalysisTemplate, len(items))
	for i, at := range items {
		out[i] = at.DeepCopy()
	}
	return out, nil
}

func (t *tolerantAnalysisTemplateLister) AnalysisTemplates(namespace string) rolloutlisters.AnalysisTemplateNamespaceLister {
	return &tolerantAnalysisTemplateNamespaceLister{
		delegate: t.delegate.AnalysisTemplates(namespace),
	}
}

type tolerantAnalysisTemplateNamespaceLister struct {
	delegate rolloutlisters.AnalysisTemplateNamespaceLister
}

func (t *tolerantAnalysisTemplateNamespaceLister) Get(name string) (*v1alpha1.AnalysisTemplate, error) {
	at, err := t.delegate.Get(name)
	if err != nil {
		return nil, err
	}
	return at.DeepCopy(), nil
}

func (t *tolerantAnalysisTemplateNamespaceLister) List(selector labels.Selector) ([]*v1alpha1.AnalysisTemplate, error) {
	items, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	out := make([]*v1alpha1.AnalysisTemplate, len(items))
	for i, at := range items {
		out[i] = at.DeepCopy()
	}
	return out, nil
}
