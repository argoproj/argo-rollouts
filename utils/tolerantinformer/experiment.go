package tolerantinformer

import (
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutinformers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	rolloutlisters "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
)

func NewTolerantExperimentInformer(factory dynamicinformer.DynamicSharedInformerFactory) rolloutinformers.ExperimentInformer {
	return &tolerantExperimentInformer{
		delegate: factory.ForResource(v1alpha1.ExperimentGVR),
	}
}

type tolerantExperimentInformer struct {
	delegate informers.GenericInformer
}

func (i *tolerantExperimentInformer) Informer() cache.SharedIndexInformer {
	return i.delegate.Informer()
}

func (i *tolerantExperimentInformer) Lister() rolloutlisters.ExperimentLister {
	return &tolerantExperimentLister{
		delegate: i.delegate.Lister(),
	}
}

type tolerantExperimentLister struct {
	delegate cache.GenericLister
}

func (t *tolerantExperimentLister) List(selector labels.Selector) ([]*v1alpha1.Experiment, error) {
	objects, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	return convertObjectsToExperiments(objects)
}

func (t *tolerantExperimentLister) Experiments(namespace string) rolloutlisters.ExperimentNamespaceLister {
	return &tolerantExperimentNamespaceLister{
		delegate: t.delegate.ByNamespace(namespace),
	}
}

type tolerantExperimentNamespaceLister struct {
	delegate cache.GenericNamespaceLister
}

func (t *tolerantExperimentNamespaceLister) Get(name string) (*v1alpha1.Experiment, error) {
	object, err := t.delegate.Get(name)
	if err != nil {
		return nil, err
	}
	v := &v1alpha1.Experiment{}
	err = convertObject(object, v)
	return v, err
}

func (t *tolerantExperimentNamespaceLister) List(selector labels.Selector) ([]*v1alpha1.Experiment, error) {
	objects, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	return convertObjectsToExperiments(objects)
}

func convertObjectsToExperiments(objects []runtime.Object) ([]*v1alpha1.Experiment, error) {
	var firstErr error
	vs := make([]*v1alpha1.Experiment, len(objects))
	for i, obj := range objects {
		vs[i] = &v1alpha1.Experiment{}
		err := convertObject(obj, vs[i])
		if err != nil && firstErr != nil {
			firstErr = err
		}
	}
	return vs, firstErr
}
