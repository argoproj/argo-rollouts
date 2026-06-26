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
	return &tolerantRolloutLister{
		delegate: rolloutlisters.NewRolloutLister(i.delegate.Informer().GetIndexer()),
	}
}

type tolerantRolloutLister struct {
	delegate rolloutlisters.RolloutLister
}

func (t *tolerantRolloutLister) List(selector labels.Selector) ([]*v1alpha1.Rollout, error) {
	items, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	out := make([]*v1alpha1.Rollout, len(items))
	for i, ro := range items {
		out[i] = ro.DeepCopy()
	}
	return out, nil
}

func (t *tolerantRolloutLister) Rollouts(namespace string) rolloutlisters.RolloutNamespaceLister {
	return &tolerantRolloutNamespaceLister{
		delegate: t.delegate.Rollouts(namespace),
	}
}

type tolerantRolloutNamespaceLister struct {
	delegate rolloutlisters.RolloutNamespaceLister
}

func (t *tolerantRolloutNamespaceLister) Get(name string) (*v1alpha1.Rollout, error) {
	ro, err := t.delegate.Get(name)
	if err != nil {
		return nil, err
	}
	return ro.DeepCopy(), nil
}

func (t *tolerantRolloutNamespaceLister) List(selector labels.Selector) ([]*v1alpha1.Rollout, error) {
	items, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	out := make([]*v1alpha1.Rollout, len(items))
	for i, ro := range items {
		out[i] = ro.DeepCopy()
	}
	return out, nil
}
