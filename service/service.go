package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubernetes/cmd/kubeadm/app/util"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	serviceutil "github.com/argoproj/argo-rollouts/utils/service"
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"
)

const (
	// serviceIndexName is the index by which Service resources are cached
	serviceIndexName    = "byService"
	removeSelectorPatch = `{"metadata": {
		"annotations": {
			"` + v1alpha1.ManagedByRolloutsKey + `": null
		}
	}
}`
	removeSelectorAndManagedByPatch = `{
	"metadata": {
		"annotations": {
			"` + v1alpha1.ManagedByRolloutsKey + `": null
		}
	},
	"spec": {
		"selector": {
			"` + v1alpha1.DefaultRolloutUniqueLabelKey + `": null
		}
	}
}`
)

// ControllerConfig describes the data required to instantiate a new service controller
type ControllerConfig struct {
	Kubeclientset     kubernetes.Interface
	Argoprojclientset clientset.Interface

	RolloutsInformer informers.RolloutInformer
	ServicesInformer coreinformers.ServiceInformer

	RolloutWorkqueue workqueue.RateLimitingInterface
	ServiceWorkqueue workqueue.RateLimitingInterface

	ResyncPeriod time.Duration

	MetricsServer *metrics.MetricsServer
}

// Controller describes a service controller
type Controller struct {
	kubeclientset     kubernetes.Interface
	argoprojclientset clientset.Interface
	rolloutsIndexer   cache.Indexer
	rolloutSynced     cache.InformerSynced
	servicesLister    v1.ServiceLister
	serviceSynced     cache.InformerSynced
	rolloutWorkqueue  workqueue.RateLimitingInterface
	serviceWorkqueue  workqueue.RateLimitingInterface
	resyncPeriod      time.Duration

	metricServer   *metrics.MetricsServer
	enqueueRollout func(obj interface{})
}

// NewController returns a new service controller
func NewController(cfg ControllerConfig) *Controller {

	controller := &Controller{
		kubeclientset:     cfg.Kubeclientset,
		argoprojclientset: cfg.Argoprojclientset,
		rolloutsIndexer:   cfg.RolloutsInformer.Informer().GetIndexer(),
		rolloutSynced:     cfg.RolloutsInformer.Informer().HasSynced,
		servicesLister:    cfg.ServicesInformer.Lister(),
		serviceSynced:     cfg.ServicesInformer.Informer().HasSynced,

		rolloutWorkqueue: cfg.RolloutWorkqueue,
		serviceWorkqueue: cfg.ServiceWorkqueue,
		resyncPeriod:     cfg.ResyncPeriod,
		metricServer:     cfg.MetricsServer,
	}

	util.CheckErr(cfg.RolloutsInformer.Informer().AddIndexers(cache.Indexers{
		serviceIndexName: func(obj interface{}) (strings []string, e error) {
			if ro := unstructuredutil.ObjectToRollout(obj); ro != nil {
				return serviceutil.GetRolloutServiceKeys(ro), nil
			}
			return []string{}, nil
		},
	}))

	cfg.ServicesInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controllerutil.Enqueue(obj, cfg.ServiceWorkqueue)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			controllerutil.Enqueue(newObj, cfg.ServiceWorkqueue)
		},
		DeleteFunc: func(obj interface{}) {
			controllerutil.Enqueue(obj, cfg.ServiceWorkqueue)
		},
	})
	controller.enqueueRollout = func(obj interface{}) {
		controllerutil.EnqueueRateLimited(obj, cfg.RolloutWorkqueue)
	}

	return controller
}

// Run starts the controller threads
func (c *Controller) Run(ctx context.Context, threadiness int) error {
	log.Info("Starting Service workers")
	wg := sync.WaitGroup{}
	for i := 0; i < threadiness; i++ {
		wg.Add(1)
		go wait.Until(func() {
			controllerutil.RunWorker(ctx, c.serviceWorkqueue, logutil.ServiceKey, c.syncService, c.metricServer)
			log.Debug("Service worker has stopped")
			wg.Done()
		}, time.Second, ctx.Done())
	}

	log.Info("Started Service workers")
	<-ctx.Done()
	wg.Wait()
	log.Info("All service workers have stopped")

	return nil
}

// syncService detects a change to a Service which is managed by a Rollout, and enqueues the
// related rollout for reconciliation. If no rollout is referencing the Service, then removes
// any injected fields in the service (e.g. rollouts-pod-template-hash and managed-by annotation)
func (c *Controller) syncService(ctx context.Context, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	svc, err := c.servicesLister.Services(namespace).Get(name)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	// Return early if the svc does not have a hash selector
	if _, hasHashSelector := svc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]; !hasHashSelector {
		return nil
	}

	// Handles case where the controller is not watching all Rollouts in the cluster due to instance-ids by making an
	// API call to get Rollout and confirm it references the service.
	rolloutName, hasManagedBy := serviceutil.HasManagedByAnnotation(svc)
	logCtx := log.WithField(logutil.ServiceKey, svc.Name).WithField(logutil.NamespaceKey, svc.Namespace)
	if rolloutName != "" {
		logCtx = logCtx.WithField(logutil.RolloutKey, rolloutName)
	}
	logCtx.Infof("syncing service")

	if hasManagedBy {
		rollout, err := c.argoprojclientset.ArgoprojV1alpha1().Rollouts(svc.Namespace).Get(ctx, rolloutName, metav1.GetOptions{})
		if err == nil {
			if serviceutil.CheckRolloutForService(rollout, svc) {
				c.enqueueRollout(rollout)
				return nil
			}
		}
	} else {
		// Checks if a service without a managed-by but has a hash selector doesn't have any rollouts reference it. If
		// not, the controller removes the hash selector. This protects against case where users upgrade from a version
		// of Argo Rollouts without managed-by. Otherwise, the has selector would just be removed.
		rollouts, err := c.getRolloutsByService(svc.Namespace, svc.Name)
		if err == nil {
			for i := range rollouts {
				if serviceutil.CheckRolloutForService(rollouts[i], svc) {
					c.enqueueRollout(rollouts[i])
					return nil
				}
			}
		}
	}

	patch := generateRemovePatch(svc)
	if patch != "" {
		_, err = c.kubeclientset.CoreV1().Services(svc.Namespace).Patch(ctx, svc.Name, patchtypes.MergePatchType, []byte(patch), metav1.PatchOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		logCtx.Infof("cleaned service")
	}
	return nil
}

// generateRemovePatch generates a patch which clears injected fields the controller may have injected
// against the Service
func generateRemovePatch(svc *corev1.Service) string {
	if _, ok := svc.Annotations[v1alpha1.ManagedByRolloutsKey]; ok {
		return removeSelectorAndManagedByPatch
	}
	if _, ok := svc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]; ok {
		return removeSelectorPatch
	}
	return ""
}

// getRolloutsByService returns all rollouts which are referencing specified service
func (c *Controller) getRolloutsByService(namespace string, serviceName string) ([]*v1alpha1.Rollout, error) {
	objs, err := c.rolloutsIndexer.ByIndex(serviceIndexName, fmt.Sprintf("%s/%s", namespace, serviceName))
	if err != nil {
		return nil, err
	}
	var rollouts []*v1alpha1.Rollout
	for _, obj := range objs {
		if ro := unstructuredutil.ObjectToRollout(obj); ro != nil {
			rollouts = append(rollouts, ro)
		}
	}
	return rollouts, nil
}
