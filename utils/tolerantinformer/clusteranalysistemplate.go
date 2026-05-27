package tolerantinformer

import (
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
	return rolloutlisters.NewClusterAnalysisTemplateLister(i.delegate.Informer().GetIndexer())
}
