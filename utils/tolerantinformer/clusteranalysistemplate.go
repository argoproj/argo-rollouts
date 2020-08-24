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

func NewTolerantClusterAnalysisTemplateInformer(factory dynamicinformer.DynamicSharedInformerFactory) rolloutinformers.ClusterAnalysisTemplateInformer {
	return &tolerantClusterAnalysisTemplateInformer{
		delegate: factory.ForResource(v1alpha1.ClusterAnalysisTemplateGVR),
	}
}

type tolerantClusterAnalysisTemplateInformer struct {
	delegate informers.GenericInformer
}

func (i *tolerantClusterAnalysisTemplateInformer) Informer() cache.SharedIndexInformer {
	return i.delegate.Informer()
}

func (i *tolerantClusterAnalysisTemplateInformer) Lister() rolloutlisters.ClusterAnalysisTemplateLister {
	return &tolerantClusterAnalysisTemplateLister{
		delegate: i.delegate.Lister(),
	}
}

type tolerantClusterAnalysisTemplateLister struct {
	delegate cache.GenericLister
}

func (t *tolerantClusterAnalysisTemplateLister) List(selector labels.Selector) ([]*v1alpha1.ClusterAnalysisTemplate, error) {
	objects, err := t.delegate.List(selector)
	if err != nil {
		return nil, err
	}
	return convertObjectsToClusterAnalysisTemplates(objects)
}

func (t *tolerantClusterAnalysisTemplateLister) Get(name string) (*v1alpha1.ClusterAnalysisTemplate, error) {
	object, err := t.delegate.Get(name)
	if err != nil {
		return nil, err
	}
	v := &v1alpha1.ClusterAnalysisTemplate{}
	err = convertObject(object, v)
	return v, err
}

func convertObjectsToClusterAnalysisTemplates(objects []runtime.Object) ([]*v1alpha1.ClusterAnalysisTemplate, error) {
	var firstErr error
	vs := make([]*v1alpha1.ClusterAnalysisTemplate, len(objects))
	for i, obj := range objects {
		vs[i] = &v1alpha1.ClusterAnalysisTemplate{}
		err := convertObject(obj, vs[i])
		if err != nil && firstErr != nil {
			firstErr = err
		}
	}
	return vs, firstErr
}
