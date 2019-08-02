package service

import (
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
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
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	serviceutil "github.com/argoproj/argo-rollouts/utils/service"
)

const (
	serviceIndexName    = "byService"
	removeSelectorPatch = `[{ "op": "remove", "path": "/spec/selector/%s" }]`
)

type ServiceController struct {
	kubeclientset    kubernetes.Interface
	rolloutsIndexer  cache.Indexer
	rolloutSynced    cache.InformerSynced
	servicesLister   v1.ServiceLister
	serviceSynced    cache.InformerSynced
	rolloutWorkqueue workqueue.RateLimitingInterface
	serviceWorkqueue workqueue.RateLimitingInterface
	resyncPeriod     time.Duration

	metricServer   *metrics.MetricsServer
	enqueueRollout func(obj interface{})
}

// NewServiceController returns a new service controller
func NewServiceController(
	kubeclientset kubernetes.Interface,
	servicesInformer coreinformers.ServiceInformer,
	rolloutsInformer informers.RolloutInformer,
	resyncPeriod time.Duration,
	rolloutWorkQueue workqueue.RateLimitingInterface,
	serviceWorkQueue workqueue.RateLimitingInterface,
	metricServer *metrics.MetricsServer) *ServiceController {

	controller := &ServiceController{
		kubeclientset:   kubeclientset,
		rolloutsIndexer: rolloutsInformer.Informer().GetIndexer(),
		rolloutSynced:   rolloutsInformer.Informer().HasSynced,
		servicesLister:  servicesInformer.Lister(),
		serviceSynced:   servicesInformer.Informer().HasSynced,

		rolloutWorkqueue: rolloutWorkQueue,
		serviceWorkqueue: serviceWorkQueue,
		resyncPeriod:     resyncPeriod,
		metricServer:     metricServer,
	}

	util.CheckErr(rolloutsInformer.Informer().AddIndexers(cache.Indexers{
		serviceIndexName: func(obj interface{}) (strings []string, e error) {
			if rollout, ok := obj.(*v1alpha1.Rollout); ok {
				return serviceutil.GetRolloutServiceKeys(rollout), nil
			}
			return []string{}, nil
		},
	}))

	servicesInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controllerutil.Enqueue(obj, serviceWorkQueue)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			controllerutil.Enqueue(newObj, serviceWorkQueue)
		},
		DeleteFunc: func(obj interface{}) {
			controllerutil.Enqueue(obj, serviceWorkQueue)
		},
	})
	controller.enqueueRollout = func(obj interface{}) {
		controllerutil.EnqueueRateLimited(obj, rolloutWorkQueue)
	}

	return controller
}

func (c *ServiceController) Run(threadiness int, stopCh <-chan struct{}) error {
	log.Info("Starting Service workers")
	for i := 0; i < threadiness; i++ {
		go wait.Until(func() {
			controllerutil.RunWorker(c.serviceWorkqueue, logutil.RolloutKey, c.syncService, c.metricServer)
		}, time.Second, stopCh)
	}

	log.Info("Started Service workers")
	<-stopCh
	log.Info("Shutting down workers")

	return nil
}

func (c *ServiceController) syncService(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	svc, err := c.servicesLister.Services(namespace).Get(name)
	if errors.IsNotFound(err) {
		log.WithField(logutil.ServiceKey, key).Infof("Service %v has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	if rollouts, err := c.getRolloutsByService(svc.Namespace, svc.Name); err == nil {
		for i := range rollouts {
			c.enqueueRollout(rollouts[i])
		}

		if _, hasHashSelector := svc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]; hasHashSelector && len(rollouts) == 0 {
			updatedSvc := svc.DeepCopy()
			delete(updatedSvc.Spec.Selector, v1alpha1.DefaultRolloutUniqueLabelKey)
			patch := fmt.Sprintf(removeSelectorPatch, v1alpha1.DefaultRolloutUniqueLabelKey)
			_, err := c.kubeclientset.CoreV1().Services(updatedSvc.Namespace).Patch(updatedSvc.Name, patchtypes.JSONPatchType, []byte(patch))
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}
	}
	return nil
}

// getRolloutsByService returns all rollouts which are referencing specified service
func (c *ServiceController) getRolloutsByService(namespace string, serviceName string) ([]*v1alpha1.Rollout, error) {
	objs, err := c.rolloutsIndexer.ByIndex(serviceIndexName, fmt.Sprintf("%s/%s", namespace, serviceName))
	if err != nil {
		return nil, err
	}
	var rollouts []*v1alpha1.Rollout
	for i := range objs {
		if r, ok := objs[i].(*v1alpha1.Rollout); ok {
			rollouts = append(rollouts, r)
		}
	}
	return rollouts, nil
}
