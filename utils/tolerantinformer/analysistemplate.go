package tolerantinformer

import (
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
	return rolloutlisters.NewAnalysisTemplateLister(i.delegate.Informer().GetIndexer())
}
