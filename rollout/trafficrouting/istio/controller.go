package istio

import (
	"context"
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamiclister"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/slice"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	roclientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/queue"
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"
)

const (
	// names for the rollout indexer
	virtualServiceIndexName  = "byVirtualService"
	destinationRuleIndexName = "byDestinationRule"

	// default number of workers to handle DestinationRule update events
	destinationRuleWorkers = 10
)

type IstioControllerConfig struct {
	ArgoprojClientSet       roclientset.Interface
	DynamicClientSet        dynamic.Interface
	EnqueueRollout          func(ro interface{})
	RolloutsInformer        informers.RolloutInformer
	VirtualServiceInformer  cache.SharedIndexInformer
	DestinationRuleInformer cache.SharedIndexInformer
}

type IstioController struct {
	IstioControllerConfig
	VirtualServiceLister  dynamiclister.Lister
	DestinationRuleLister dynamiclister.Lister

	destinationRuleWorkqueue workqueue.RateLimitingInterface
}

func NewIstioController(cfg IstioControllerConfig) *IstioController {
	c := IstioController{
		IstioControllerConfig:    cfg,
		destinationRuleWorkqueue: workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "DestinationRules"),
		VirtualServiceLister:     dynamiclister.New(cfg.VirtualServiceInformer.GetIndexer(), istioutil.GetIstioVirtualServiceGVR()),
		DestinationRuleLister:    dynamiclister.New(cfg.DestinationRuleInformer.GetIndexer(), istioutil.GetIstioDestinationRuleGVR()),
	}

	// Add a Rollout index against referenced VirtualServices and DestinationRules
	util.CheckErr(cfg.RolloutsInformer.Informer().AddIndexers(cache.Indexers{
		virtualServiceIndexName: func(obj interface{}) (strings []string, e error) {
			if ro := unstructuredutil.ObjectToRollout(obj); ro != nil {
				return istioutil.GetRolloutVirtualServiceKeys(ro), nil
			}
			return
		},
	}))
	util.CheckErr(cfg.RolloutsInformer.Informer().AddIndexers(cache.Indexers{
		destinationRuleIndexName: func(obj interface{}) (strings []string, e error) {
			if ro := unstructuredutil.ObjectToRollout(obj); ro != nil {
				return istioutil.GetRolloutDesinationRuleKeys(ro), nil
			}
			return
		},
	}))

	// When a VirtualService changes, simply enqueue the referencing rollout
	c.VirtualServiceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.EnqueueRolloutFromIstioVirtualService(obj)
		},
		// TODO: DeepEquals on httpRoutes
		UpdateFunc: func(old, new interface{}) {
			c.EnqueueRolloutFromIstioVirtualService(new)
		},
		DeleteFunc: func(obj interface{}) {
			c.EnqueueRolloutFromIstioVirtualService(obj)
		},
	})

	// When a DestinationRule changes, enqueue the DestinationRule for processing
	c.DestinationRuleInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.EnqueueDestinationRule(obj)
		},
		UpdateFunc: func(old, new interface{}) {
			c.EnqueueDestinationRule(new)
		},
		DeleteFunc: func(obj interface{}) {
			c.EnqueueDestinationRule(obj)
		},
	})
	return &c
}

// Run starts the Istio informers. If Istio is not installed, will periodically check for presence
// of Istio, then start informers once detected. This allows Argo Rollouts to be installed in any
// order during cluster bootstrapping.
func (c *IstioController) Run(ctx context.Context) {
	ns := defaults.Namespace()
	waitForIstioInstall := !istioutil.DoesIstioExist(c.DynamicClientSet, ns)
	if waitForIstioInstall {
		ticker := time.NewTicker(10 * time.Minute)
		for !istioutil.DoesIstioExist(c.DynamicClientSet, ns) {
			// Should only execute if Istio is not installed on cluster
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
			}
		}
		ticker.Stop()
		log.Info("Istio install detected. Starting informers")
		go c.VirtualServiceInformer.Run(ctx.Done())
		go c.DestinationRuleInformer.Run(ctx.Done())
	} else {
		log.Info("Istio detected")
	}

	cache.WaitForCacheSync(ctx.Done(), c.VirtualServiceInformer.HasSynced, c.DestinationRuleInformer.HasSynced)

	log.Info("Starting istio workers")
	wg := sync.WaitGroup{}
	for i := 0; i < destinationRuleWorkers; i++ {
		wg.Add(1)
		go wait.Until(func() {
			controllerutil.RunWorker(ctx, c.destinationRuleWorkqueue, "destinationrule", c.syncDestinationRule, nil)
			wg.Done()
			log.Debug("Istio worker has stopped")
		}, time.Second, ctx.Done())
	}
	log.Infof("Istio workers (%d) started", destinationRuleWorkers)

	<-ctx.Done()
	wg.Wait()
	log.Info("All istio workers have stopped")
}

// EnqueueDestinationRule examines a VirtualService, finds the Rollout referencing
// that VirtualService, and enqueues the corresponding Rollout for reconciliation
func (c *IstioController) EnqueueDestinationRule(obj interface{}) {
	controllerutil.EnqueueRateLimited(obj, c.destinationRuleWorkqueue)
}

// EnqueueRolloutFromIstioVirtualService examines a VirtualService, finds the Rollout referencing
// that VirtualService, and enqueues the corresponding Rollout for reconciliation
func (c *IstioController) EnqueueRolloutFromIstioVirtualService(vsvc interface{}) {
	acc, err := meta.Accessor(vsvc)
	if err != nil {
		log.Errorf("Error processing istio VirtualService from watch: %v: %v", err, vsvc)
		return
	}
	rolloutToEnqueue, err := c.RolloutsInformer.Informer().GetIndexer().ByIndex(virtualServiceIndexName, fmt.Sprintf("%s/%s", acc.GetNamespace(), acc.GetName()))
	if err != nil {
		log.Errorf("Cannot process indexer: %s", err.Error())
		return
	}
	for i := range rolloutToEnqueue {
		c.EnqueueRollout(rolloutToEnqueue[i])
	}
}

func (c *IstioController) GetReferencedVirtualServices(ro *v1alpha1.Rollout) (*[]unstructured.Unstructured, error) {
	var fldPath *field.Path
	ctx := context.TODO()
	virtualServices := []unstructured.Unstructured{}
	if ro.Spec.Strategy.Canary != nil {
		canary := ro.Spec.Strategy.Canary
		if canary.TrafficRouting != nil && canary.TrafficRouting.Istio != nil {
			var vsvc *unstructured.Unstructured
			var err error
			var vsvcs []v1alpha1.IstioVirtualService

			if istioutil.MultipleVirtualServiceConfigured(ro) {
				vsvcs = canary.TrafficRouting.Istio.VirtualServices
				fldPath = field.NewPath("spec", "strategy", "canary", "trafficRouting", "istio", "virtualServices", "name")
			} else {
				vsvcs = []v1alpha1.IstioVirtualService{*canary.TrafficRouting.Istio.VirtualService}
				fldPath = field.NewPath("spec", "strategy", "canary", "trafficRouting", "istio", "virtualService", "name")
			}

			for _, eachVsvc := range vsvcs {
				vsvcNamespace, vsvcName := istioutil.GetVirtualServiceNamespaceName(eachVsvc.Name)
				if vsvcNamespace == "" {
					vsvcNamespace = ro.Namespace
				}
				if c.VirtualServiceInformer.HasSynced() {
					vsvc, err = c.VirtualServiceLister.Namespace(vsvcNamespace).Get(vsvcName)
				} else {
					vsvc, err = c.DynamicClientSet.Resource(istioutil.GetIstioVirtualServiceGVR()).Namespace(vsvcNamespace).Get(ctx, vsvcName, metav1.GetOptions{})
				}

				if k8serrors.IsNotFound(err) {
					return nil, field.Invalid(fldPath, vsvcName, err.Error())
				}
				if err != nil {
					return nil, err
				}

				virtualServices = append(virtualServices, *vsvc)
			}
		}
	}
	return &virtualServices, nil
}

// syncDestinationRule examines a DestinationRule, finds the Rollout which is managing that
// DestinationRule, and enqueues it for reconciliation. If no Rollout is managing the
// DestinationRule, it removes any injected labels/annotations in that DestinationRule (i.e. the
// `rollouts-pod-template-hash` label and the managed-by annotation. This handles the case when a
// Rollout has either been deleted, or modified such that it is longer referencing the
// DestinationRule.
func (c *IstioController) syncDestinationRule(ctx context.Context, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	dRuleUn, err := c.DestinationRuleLister.Namespace(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	managingRolloutName := getManagingRolloutName(dRuleUn)
	if managingRolloutName == "" {
		// Ignore DestinationRules not managed by a Rollout
		return nil
	}

	logCtx := log.WithField(logutil.RolloutKey, managingRolloutName).WithField(logutil.NamespaceKey, namespace).WithField("destinationrule", name)

	cleanDestRule := false
	ro, err := c.ArgoprojClientSet.ArgoprojV1alpha1().Rollouts(namespace).Get(context.TODO(), managingRolloutName, metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
		logCtx.Infof("cleaning destinationrule: rollout does not exist")
		cleanDestRule = true
	} else {
		if !slice.ContainsString(istioutil.GetRolloutDesinationRuleKeys(ro), key, nil) {
			logCtx.Infof("cleaning destinationrule: rollout no longer references rule")
			cleanDestRule = true
		}
	}

	if cleanDestRule {
		// remove any fields we may have injected into a DestinationRule
		origBytes, dRule, dRuleNew, err := unstructuredToDestinationRules(dRuleUn)
		if err != nil {
			return err
		}
		if dRuleNew.Annotations != nil {
			delete(dRuleNew.Annotations, v1alpha1.ManagedByRolloutsKey)
		}
		for i, subset := range dRuleNew.Spec.Subsets {
			if _, exists := subset.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]; exists {
				delete(subset.Labels, v1alpha1.DefaultRolloutUniqueLabelKey)
				dRuleNew.Spec.Subsets[i] = subset
			}
		}
		dRuleClient := c.DynamicClientSet.Resource(istioutil.GetIstioDestinationRuleGVR()).Namespace(dRule.Namespace)
		modified, err := updateDestinationRule(context.TODO(), dRuleClient, origBytes, dRule, dRuleNew)
		if err != nil {
			return err
		}
		if modified {
			logCtx.Infof("cleaned destination rule")
			return nil
		}
	}

	// destination rule changed, re-reconcile rollout
	c.EnqueueRollout(namespace + "/" + managingRolloutName)
	return nil
}

// getManagingRollout returns the name of the rollout managing this DestinationRule or empty string if none
func getManagingRolloutName(un *unstructured.Unstructured) string {
	annots := un.GetAnnotations()
	if annots == nil {
		return ""
	}
	return annots[v1alpha1.ManagedByRolloutsKey]
}

func (c *IstioController) ShutDownWithDrain() {
	c.destinationRuleWorkqueue.ShutDownWithDrain()
}
