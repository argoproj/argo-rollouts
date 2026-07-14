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
	transform := makeTransform(func() *v1alpha1.AnalysisTemplate { return &v1alpha1.AnalysisTemplate{} })
	installTransform(delegate.Informer(), transform, "AnalysisTemplate")
	return &tolerantAnalysisTemplateInformer{delegate: delegate, transform: transform}
}

type tolerantAnalysisTemplateInformer struct {
	delegate  informers.GenericInformer
	transform cache.TransformFunc
}

func (i *tolerantAnalysisTemplateInformer) Informer() cache.SharedIndexInformer {
	return &transformingInformer{SharedIndexInformer: i.delegate.Informer(), transform: i.transform}
}

func (i *tolerantAnalysisTemplateInformer) Lister() rolloutlisters.AnalysisTemplateLister {
	return &tolerantAnalysisTemplateLister{indexer: i.delegate.Informer().GetIndexer()}
}

type tolerantAnalysisTemplateLister struct {
	indexer cache.Indexer
}

func (t *tolerantAnalysisTemplateLister) List(selector labels.Selector) ([]*v1alpha1.AnalysisTemplate, error) {
	return listTyped(t.indexer, "", selector,
		func() *v1alpha1.AnalysisTemplate { return &v1alpha1.AnalysisTemplate{} },
		func(at *v1alpha1.AnalysisTemplate) *v1alpha1.AnalysisTemplate { return at.DeepCopy() })
}

func (t *tolerantAnalysisTemplateLister) AnalysisTemplates(namespace string) rolloutlisters.AnalysisTemplateNamespaceLister {
	return &tolerantAnalysisTemplateNamespaceLister{indexer: t.indexer, namespace: namespace}
}

type tolerantAnalysisTemplateNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

func (t *tolerantAnalysisTemplateNamespaceLister) Get(name string) (*v1alpha1.AnalysisTemplate, error) {
	return getTyped(t.indexer, v1alpha1.Resource("analysistemplate"), t.namespace, name,
		func() *v1alpha1.AnalysisTemplate { return &v1alpha1.AnalysisTemplate{} },
		func(at *v1alpha1.AnalysisTemplate) *v1alpha1.AnalysisTemplate { return at.DeepCopy() })
}

func (t *tolerantAnalysisTemplateNamespaceLister) List(selector labels.Selector) ([]*v1alpha1.AnalysisTemplate, error) {
	return listTyped(t.indexer, t.namespace, selector,
		func() *v1alpha1.AnalysisTemplate { return &v1alpha1.AnalysisTemplate{} },
		func(at *v1alpha1.AnalysisTemplate) *v1alpha1.AnalysisTemplate { return at.DeepCopy() })
}
