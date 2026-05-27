package tolerantinformer

import (
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutinformers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	rolloutlisters "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
)

func NewTolerantExperimentInformer(factory dynamicinformer.DynamicSharedInformerFactory) rolloutinformers.ExperimentInformer {
	delegate := factory.ForResource(v1alpha1.ExperimentGVR)
	installTransform(delegate.Informer(),
		makeTransform(func() *v1alpha1.Experiment { return &v1alpha1.Experiment{} }),
		"Experiment")
	return &tolerantExperimentInformer{delegate: delegate}
}

type tolerantExperimentInformer struct {
	delegate informers.GenericInformer
}

func (i *tolerantExperimentInformer) Informer() cache.SharedIndexInformer {
	return i.delegate.Informer()
}

func (i *tolerantExperimentInformer) Lister() rolloutlisters.ExperimentLister {
	return rolloutlisters.NewExperimentLister(i.delegate.Informer().GetIndexer())
}
