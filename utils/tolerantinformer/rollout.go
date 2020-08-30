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

func NewTolerantRolloutInformer(factory dynamicinformer.DynamicSharedInformerFactory) rolloutinformers.RolloutInformer {
	return &tolerantRolloutInformer{
		delegate: factory.ForResource(v1alpha1.RolloutGVR),
	}
}

type tolerantRolloutInformer struct {
	delegate informers.GenericInformer
}

func (i *tolerantRolloutInformer) Informer() cache.SharedIndexInformer {
	return i.delegate.Informer()
}

func (i *tolerantRolloutInformer) Lister() rolloutlisters.RolloutLister {
	return &tolerantRolloutLister{
		delegate: i.delegate.Lister(),
	}
}

type tolerantRolloutLister struct {
	delegate cache.GenericLister
}

func (t *tolerantRolloutLister) List(selector labels.Selector) ([]*v1alpha1.Rollout, error) {
	objects, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	return convertObjectsToRollouts(objects)
}

func (t *tolerantRolloutLister) Rollouts(namespace string) rolloutlisters.RolloutNamespaceLister {
	return &tolerantRolloutNamespaceLister{
		delegate: t.delegate.ByNamespace(namespace),
	}
}

type tolerantRolloutNamespaceLister struct {
	delegate cache.GenericNamespaceLister
}

func (t *tolerantRolloutNamespaceLister) Get(name string) (*v1alpha1.Rollout, error) {
	object, err := t.delegate.Get(name)
	if err != nil {
		return nil, err
	}
	v := &v1alpha1.Rollout{}
	err = convertObject(object, v)
	return v, err
}

func (t *tolerantRolloutNamespaceLister) List(selector labels.Selector) ([]*v1alpha1.Rollout, error) {
	objects, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	return convertObjectsToRollouts(objects)
}

func convertObjectsToRollouts(objects []runtime.Object) ([]*v1alpha1.Rollout, error) {
	var firstErr error
	vs := make([]*v1alpha1.Rollout, len(objects))
	for i, obj := range objects {
		vs[i] = &v1alpha1.Rollout{}
		err := convertObject(obj, vs[i])
		if err != nil && firstErr != nil {
			firstErr = err
		}
	}
	return vs, firstErr
}
