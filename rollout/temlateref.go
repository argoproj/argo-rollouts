package rollout

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	templateRefIndexName = "byTemplateRef"
)

type knownKindInfo struct {
	TemplatePath []string
	SelectorPath []string
}

var (
	infoByGroupKind = map[schema.GroupKind]knownKindInfo{
		{Kind: "PodTemplate"}: {
			TemplatePath: []string{"template"},
		},
		{Group: "apps", Kind: "Deployment"}: {
			TemplatePath: []string{"spec", "template"}, SelectorPath: []string{"spec", "selector"},
		},
		{Group: "apps", Kind: "ReplicaSet"}: {
			TemplatePath: []string{"spec", "template"}, SelectorPath: []string{"spec", "selector"},
		},
	}
)

type informerBasedTemplateResolver struct {
	namespace              string
	informerResyncDuration time.Duration
	informerSyncTimeout    time.Duration
	informersLock          sync.Mutex
	informers              map[schema.GroupVersionKind]func() (informers.GenericInformer, error)
	dynamicClient          dynamic.Interface
	discoClient            discovery.DiscoveryInterface
	ctx                    context.Context
	cancelContext          context.CancelFunc
	rolloutWorkQueue       workqueue.Interface
	rolloutsInformer       cache.SharedIndexInformer
}

// NewInformerBasedWorkloadRefResolver create new instance of workload ref resolver.
func NewInformerBasedWorkloadRefResolver(
	namespace string,
	dynamicClient dynamic.Interface,
	discoClient discovery.DiscoveryInterface,
	rolloutWorkQueue workqueue.Interface,
	rolloutsInformer cache.SharedIndexInformer,
) *informerBasedTemplateResolver {
	ctx, cancelContext := context.WithCancel(context.TODO())
	err := rolloutsInformer.AddIndexers(cache.Indexers{
		templateRefIndexName: func(obj interface{}) ([]string, error) {
			if ro := unstructuredutil.ObjectToRollout(obj); ro != nil && ro.Spec.WorkloadRef != nil {
				return []string{refKey(*ro.Spec.WorkloadRef, ro.Namespace)}, nil
			}
			return nil, nil
		},
	})
	if err != nil {
		panic(err)
	}
	return &informerBasedTemplateResolver{
		informers:              map[schema.GroupVersionKind]func() (informers.GenericInformer, error){},
		namespace:              namespace,
		ctx:                    ctx,
		cancelContext:          cancelContext,
		informerResyncDuration: time.Minute * 5,
		informerSyncTimeout:    time.Minute,
		dynamicClient:          dynamicClient,
		discoClient:            discoClient,
		rolloutWorkQueue:       rolloutWorkQueue,
		rolloutsInformer:       rolloutsInformer,
	}
}

func refKey(ref v1alpha1.ObjectRef, namespace string) string {
	return fmt.Sprintf("%s/%s/%s/%s", ref.APIVersion, ref.Kind, namespace, ref.Name)
}

// Stop stops all started informers
func (r *informerBasedTemplateResolver) Stop() {
	r.informersLock.Lock()
	defer r.informersLock.Unlock()
	if r.cancelContext != nil {
		r.cancelContext()
	}
	ctx, cancelContext := context.WithCancel(context.TODO())
	r.ctx = ctx
	r.cancelContext = cancelContext
}

func remashalMap(objMap map[string]interface{}, res interface{}) error {
	data, err := json.Marshal(objMap)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, res)
}

// Resolve verifies if given rollout has template reference and resolves pod template
func (r *informerBasedTemplateResolver) Resolve(rollout *v1alpha1.Rollout) error {
	if rollout.Spec.WorkloadRef == nil {
		return nil
	}

	gvk := schema.FromAPIVersionAndKind(rollout.Spec.WorkloadRef.APIVersion, rollout.Spec.WorkloadRef.Kind)

	info, ok := infoByGroupKind[gvk.GroupKind()]
	if !ok {
		return fmt.Errorf("workload of type %s/%s is not supported", gvk.Group, gvk.Kind)
	}

	informer, err := r.getInformer(gvk)
	if err != nil {
		return err
	}
	obj, err := informer.Lister().Get(fmt.Sprintf("%s/%s", rollout.Namespace, rollout.Spec.WorkloadRef.Name))
	if err != nil {
		return err
	}
	un, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("informer for %v must have unstructured object but had %v", gvk, obj)
	}

	if podTemplateSpecMap, ok, _ := unstructured.NestedMap(un.Object, info.TemplatePath...); ok {
		var template corev1.PodTemplateSpec
		if err := remashalMap(podTemplateSpecMap, &template); err != nil {
			return err
		}

		rollout.Spec.SetResolvedTemplate(template)
	}

	if rollout.Spec.Selector == nil && info.SelectorPath != nil {
		if selectorMap, ok, _ := unstructured.NestedMap(un.Object, info.SelectorPath...); ok {
			var selector v1.LabelSelector
			if err := remashalMap(selectorMap, &selector); err != nil {
				return err
			}
			rollout.Spec.SetResolvedSelector(&selector)
		}
	}

	return nil
}

// newInformerForGVK create an informer for a given group version kind
func (r *informerBasedTemplateResolver) newInformerForGVK(gvk schema.GroupVersionKind) (informers.GenericInformer, error) {
	resources, err := r.discoClient.ServerResourcesForGroupVersion(gvk.GroupVersion().String())
	if err != nil {
		return nil, err
	}
	var apiResource *v1.APIResource
	for _, r := range resources.APIResources {
		if r.Kind == gvk.Kind {
			apiResource = &r
			break
		}
	}
	if apiResource == nil {
		return nil, errors.NewNotFound(schema.GroupResource{Group: gvk.Group, Resource: gvk.Kind}, "")
	}
	informer := dynamicinformer.NewFilteredDynamicInformer(
		r.dynamicClient,
		schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: apiResource.Name},
		r.namespace,
		r.informerResyncDuration,
		cache.Indexers{},
		nil)
	informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			r.requeueReferencedRollouts(obj, gvk)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			r.requeueReferencedRollouts(newObj, gvk)
		},
		DeleteFunc: func(obj interface{}) {
			r.requeueReferencedRollouts(obj, gvk)
		},
	})
	return informer, nil

}

// requeueReferencedRollouts re-queues all rollouts referenced by given object
func (r *informerBasedTemplateResolver) requeueReferencedRollouts(obj interface{}, gvk schema.GroupVersionKind) {
	roMeta, err := meta.Accessor(obj)
	if err != nil {
		return
	}
	rollouts, err := r.rolloutsInformer.GetIndexer().ByIndex(templateRefIndexName, refKey(v1alpha1.ObjectRef{
		Kind:       gvk.Kind,
		APIVersion: gvk.GroupVersion().String(),
		Name:       roMeta.GetName(),
	}, roMeta.GetNamespace()))
	if err != nil {
		return
	}
	for _, ro := range rollouts {
		if key, err := cache.MetaNamespaceKeyFunc(ro); err == nil {
			r.rolloutWorkQueue.Add(key)
		}
	}
}

// getInformer on-demand creates and informer that watches all resources of a given group version kind
func (r *informerBasedTemplateResolver) getInformer(gvk schema.GroupVersionKind) (informers.GenericInformer, error) {
	r.informersLock.Lock()
	getInformer, ok := r.informers[gvk]
	if !ok {
		var initLock sync.Mutex
		initialized := false
		var informer informers.GenericInformer
		getInformer = func() (informers.GenericInformer, error) {
			initLock.Lock()
			defer initLock.Unlock()
			if !initialized {
				if i, err := r.newInformerForGVK(gvk); err != nil {
					return nil, err
				} else {
					informer = i
				}
				go informer.Informer().Run(r.ctx.Done())
				ctx, cancel := context.WithTimeout(r.ctx, r.informerSyncTimeout)
				defer cancel()
				if !cache.WaitForCacheSync(ctx.Done(), informer.Informer().HasSynced) {
					return nil, fmt.Errorf("failed to sync informer for %v", gvk)
				}
				initialized = true
			}

			return informer, nil
		}
		r.informers[gvk] = getInformer
	}
	r.informersLock.Unlock()

	informer, err := getInformer()
	if err != nil {
		return nil, err
	}
	return informer, nil
}
