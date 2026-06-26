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
	return &tolerantExperimentLister{
		delegate: rolloutlisters.NewExperimentLister(i.delegate.Informer().GetIndexer()),
	}
}

type tolerantExperimentLister struct {
	delegate rolloutlisters.ExperimentLister
}

func (t *tolerantExperimentLister) List(selector labels.Selector) ([]*v1alpha1.Experiment, error) {
	items, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	out := make([]*v1alpha1.Experiment, len(items))
	for i, ex := range items {
		out[i] = ex.DeepCopy()
	}
	return out, nil
}

func (t *tolerantExperimentLister) Experiments(namespace string) rolloutlisters.ExperimentNamespaceLister {
	return &tolerantExperimentNamespaceLister{
		delegate: t.delegate.Experiments(namespace),
	}
}

type tolerantExperimentNamespaceLister struct {
	delegate rolloutlisters.ExperimentNamespaceLister
}

func (t *tolerantExperimentNamespaceLister) Get(name string) (*v1alpha1.Experiment, error) {
	ex, err := t.delegate.Get(name)
	if err != nil {
		return nil, err
	}
	return ex.DeepCopy(), nil
}

func (t *tolerantExperimentNamespaceLister) List(selector labels.Selector) ([]*v1alpha1.Experiment, error) {
	items, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	out := make([]*v1alpha1.Experiment, len(items))
	for i, ex := range items {
		out[i] = ex.DeepCopy()
	}
	return out, nil
}
