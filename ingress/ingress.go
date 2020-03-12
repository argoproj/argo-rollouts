package ingress

import (
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	extensionsinformers "k8s.io/client-go/informers/extensions/v1beta1"
	extentionslisters "k8s.io/client-go/listers/extensions/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubernetes/cmd/kubeadm/app/util"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

const (
	// ingressIndexName is the index by which Ingress resources are cached
	ingressIndexName = "byIngress"
)

// ControllerConfig describes the data required to instantiate a new ingress controller
type ControllerConfig struct {
	IngressInformer  extensionsinformers.IngressInformer
	IngressWorkQueue workqueue.RateLimitingInterface

	RolloutsInformer informers.RolloutInformer
	RolloutWorkQueue workqueue.RateLimitingInterface

	MetricsServer *metrics.MetricsServer
}

// Controller describes an ingress controller
type Controller struct {
	rolloutsIndexer  cache.Indexer
	ingressLister    extentionslisters.IngressLister
	ingressWorkqueue workqueue.RateLimitingInterface

	metricServer   *metrics.MetricsServer
	enqueueRollout func(obj interface{})
}

// NewController returns a new ingress controller
func NewController(cfg ControllerConfig) *Controller {

	controller := &Controller{
		rolloutsIndexer: cfg.RolloutsInformer.Informer().GetIndexer(),
		ingressLister:   cfg.IngressInformer.Lister(),

		ingressWorkqueue: cfg.IngressWorkQueue,
		metricServer:     cfg.MetricsServer,
	}

	util.CheckErr(cfg.RolloutsInformer.Informer().AddIndexers(cache.Indexers{
		ingressIndexName: func(obj interface{}) ([]string, error) {
			if rollout, ok := obj.(*v1alpha1.Rollout); ok {
				return ingressutil.GetRolloutIngressKeys(rollout), nil
			}
			return []string{}, nil
		},
	}))

	cfg.IngressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controllerutil.Enqueue(obj, cfg.IngressWorkQueue)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			controllerutil.Enqueue(newObj, cfg.IngressWorkQueue)
		},
		DeleteFunc: func(obj interface{}) {
			controllerutil.Enqueue(obj, cfg.IngressWorkQueue)
		},
	})
	controller.enqueueRollout = func(obj interface{}) {
		controllerutil.EnqueueRateLimited(obj, cfg.RolloutWorkQueue)
	}

	return controller
}

// Run starts the controller threads
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	log.Info("Starting Ingress workers")
	for i := 0; i < threadiness; i++ {
		go wait.Until(func() {
			controllerutil.RunWorker(c.ingressWorkqueue, logutil.IngressKey, c.syncIngress, c.metricServer)
		}, time.Second, stopCh)
	}

	log.Info("Started Ingress workers")
	<-stopCh
	log.Info("Shutting down Ingress workers")

	return nil
}

// syncIngress queues all rollouts referencing the Ingress for reconciliation
func (c *Controller) syncIngress(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	_, err = c.ingressLister.Ingresses(namespace).Get(name)
	if err != nil {
		if !errors.IsNotFound(err) {
			// Unknown error occurred
			return err
		}

		if !strings.HasSuffix(name, ingressutil.CanaryIngressSuffix) {
			// a primary ingress was deleted, simply ignore the event
			log.WithField(logutil.IngressKey, key).Warn("primary ingress has been deleted")
			return nil
		}
	}

	if rollouts, err := c.getRolloutsByIngress(namespace, name); err == nil {
		for i := range rollouts {
			// reconciling the Rollout will ensure the canaryIngress is updated or created
			c.enqueueRollout(rollouts[i])
		}
	}
	return nil
}

// getRolloutsByIngress returns all rollouts which are referencing specified ingress
func (c *Controller) getRolloutsByIngress(namespace string, ingressName string) ([]*v1alpha1.Rollout, error) {
	objs, err := c.rolloutsIndexer.ByIndex(ingressIndexName, fmt.Sprintf("%s/%s", namespace, ingressName))
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
