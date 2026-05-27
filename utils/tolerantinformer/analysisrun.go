package tolerantinformer

import (
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
	return rolloutlisters.NewAnalysisRunLister(i.delegate.Informer().GetIndexer())
}
