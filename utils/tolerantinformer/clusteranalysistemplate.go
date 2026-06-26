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
	installTransform(delegate.Informer(),
		makeTransform(func() *v1alpha1.ClusterAnalysisTemplate { return &v1alpha1.ClusterAnalysisTemplate{} }),
		"ClusterAnalysisTemplate")
	return &tolerantClusterAnalysisTemplateInformer{delegate: delegate}
}

type tolerantClusterAnalysisTemplateInformer struct {
	delegate informers.GenericInformer
}

func (i *tolerantClusterAnalysisTemplateInformer) Informer() cache.SharedIndexInformer {
	return i.delegate.Informer()
}

func (i *tolerantClusterAnalysisTemplateInformer) Lister() rolloutlisters.ClusterAnalysisTemplateLister {
	return &tolerantClusterAnalysisTemplateLister{
		delegate: rolloutlisters.NewClusterAnalysisTemplateLister(i.delegate.Informer().GetIndexer()),
	}
}

type tolerantClusterAnalysisTemplateLister struct {
	delegate rolloutlisters.ClusterAnalysisTemplateLister
}

func (t *tolerantClusterAnalysisTemplateLister) List(selector labels.Selector) ([]*v1alpha1.ClusterAnalysisTemplate, error) {
	items, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	out := make([]*v1alpha1.ClusterAnalysisTemplate, len(items))
	for i, cat := range items {
		out[i] = cat.DeepCopy()
	}
	return out, nil
}

func (t *tolerantClusterAnalysisTemplateLister) Get(name string) (*v1alpha1.ClusterAnalysisTemplate, error) {
	cat, err := t.delegate.Get(name)
	if err != nil {
		return nil, err
	}
	return cat.DeepCopy(), nil
}
