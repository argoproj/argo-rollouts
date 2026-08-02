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

func NewTolerantClusterAnalysisTemplateInformer(factory dynamicinformer.DynamicSharedInformerFactory) rolloutinformers.ClusterAnalysisTemplateInformer {
	delegate := factory.ForResource(v1alpha1.ClusterAnalysisTemplateGVR)
	newFn := func() *v1alpha1.ClusterAnalysisTemplate { return &v1alpha1.ClusterAnalysisTemplate{} }
	transform := makeTransform(newFn)
	installTransform(delegate.Informer(), transform, "ClusterAnalysisTemplate")
	return &tolerantClusterAnalysisTemplateInformer{delegate: delegate, transform: transform, newFn: newFn}
}

type tolerantClusterAnalysisTemplateInformer struct {
	delegate  informers.GenericInformer
	transform cache.TransformFunc
	newFn     func() *v1alpha1.ClusterAnalysisTemplate
}

func (i *tolerantClusterAnalysisTemplateInformer) Informer() cache.SharedIndexInformer {
	return &transformingInformer{SharedIndexInformer: i.delegate.Informer(), transform: i.transform}
}

func (i *tolerantClusterAnalysisTemplateInformer) Lister() rolloutlisters.ClusterAnalysisTemplateLister {
	return &tolerantClusterAnalysisTemplateLister{indexer: i.delegate.Informer().GetIndexer(), newFn: i.newFn}
}

type tolerantClusterAnalysisTemplateLister struct {
	indexer cache.Indexer
	newFn   func() *v1alpha1.ClusterAnalysisTemplate
}

func (t *tolerantClusterAnalysisTemplateLister) List(selector labels.Selector) ([]*v1alpha1.ClusterAnalysisTemplate, error) {
	return listTyped(t.indexer, "", selector, t.newFn)
}

func (t *tolerantClusterAnalysisTemplateLister) Get(name string) (*v1alpha1.ClusterAnalysisTemplate, error) {
	return getTyped(t.indexer, v1alpha1.Resource("clusteranalysistemplate"), "", name, t.newFn)
}
