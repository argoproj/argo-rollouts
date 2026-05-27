package tolerantinformer

import (
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutinformers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	rolloutlisters "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
)

func NewTolerantRolloutInformer(factory dynamicinformer.DynamicSharedInformerFactory) rolloutinformers.RolloutInformer {
	delegate := factory.ForResource(v1alpha1.RolloutGVR)
	installTransform(delegate.Informer(),
		makeTransform(func() *v1alpha1.Rollout { return &v1alpha1.Rollout{} }),
		"Rollout")
	return &tolerantRolloutInformer{delegate: delegate}
}

type tolerantRolloutInformer struct {
	delegate informers.GenericInformer
}

func (i *tolerantRolloutInformer) Informer() cache.SharedIndexInformer {
	return i.delegate.Informer()
}

func (i *tolerantRolloutInformer) Lister() rolloutlisters.RolloutLister {
	return rolloutlisters.NewRolloutLister(i.delegate.Informer().GetIndexer())
}
