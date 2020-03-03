package ingress

import (
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	extensionsinformers "k8s.io/client-go/informers/extensions/v1beta1"
	"k8s.io/client-go/kubernetes"
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
	ingressIndexName = "byIngress"
)

type IngressController struct {
	kubeclientset    kubernetes.Interface
	rolloutsIndexer  cache.Indexer
	rolloutSynced    cache.InformerSynced
	ingressLister    extentionslisters.IngressLister
	ingressSynced    cache.InformerSynced
	rolloutWorkqueue workqueue.RateLimitingInterface
	ingressWorkqueue workqueue.RateLimitingInterface
	resyncPeriod     time.Duration

	metricServer   *metrics.MetricsServer
	enqueueRollout func(obj interface{})
}

// NewIngressController returns a new ingress controller
func NewIngressController(
	kubeclientset kubernetes.Interface,
	ingressInformer extensionsinformers.IngressInformer,
	rolloutsInformer informers.RolloutInformer,
	resyncPeriod time.Duration,
	rolloutWorkQueue workqueue.RateLimitingInterface,
	ingressWorkQueue workqueue.RateLimitingInterface,
	metricServer *metrics.MetricsServer) *IngressController {

	controller := &IngressController{
		kubeclientset:   kubeclientset,
		rolloutsIndexer: rolloutsInformer.Informer().GetIndexer(),
		rolloutSynced:   rolloutsInformer.Informer().HasSynced,
		ingressLister:   ingressInformer.Lister(),
		ingressSynced:   ingressInformer.Informer().HasSynced,

		rolloutWorkqueue: rolloutWorkQueue,
		ingressWorkqueue: ingressWorkQueue,
		resyncPeriod:     resyncPeriod,
		metricServer:     metricServer,
	}

	util.CheckErr(rolloutsInformer.Informer().AddIndexers(cache.Indexers{
		ingressIndexName: func(obj interface{}) (strings []string, e error) {
			if rollout, ok := obj.(*v1alpha1.Rollout); ok {
				return ingressutil.GetRolloutIngressKeys(rollout), nil
			}
			return []string{}, nil
		},
	}))

	ingressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controllerutil.Enqueue(obj, ingressWorkQueue)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			controllerutil.Enqueue(newObj, ingressWorkQueue)
		},
		DeleteFunc: func(obj interface{}) {
			controllerutil.Enqueue(obj, ingressWorkQueue)
		},
	})
	controller.enqueueRollout = func(obj interface{}) {
		controllerutil.EnqueueRateLimited(obj, rolloutWorkQueue)
	}

	return controller
}

func (c *IngressController) Run(threadiness int, stopCh <-chan struct{}) error {
	log.Info("Starting Ingress workers")
	for i := 0; i < threadiness; i++ {
		go wait.Until(func() {
			controllerutil.RunWorker(c.ingressWorkqueue, logutil.ServiceKey, c.syncIngress, c.metricServer)
		}, time.Second, stopCh)
	}

	log.Info("Started Ingress workers")
	<-stopCh
	log.Info("Shutting down Ingress workers")

	return nil
}

func (c *IngressController) syncIngress(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	_, err = c.ingressLister.Ingresses(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			if !strings.HasSuffix(name, "-canary") {
				// a primary ingress was deleted, simply ignore the event
				log.WithField(logutil.IngressKey, key).Infof("Primary ingress %v has been deleted", key)
				return nil
			}
		} else {
			// Other unknown error occurred
			return err
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
func (c *IngressController) getRolloutsByIngress(namespace string, ingressName string) ([]*v1alpha1.Rollout, error) {
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
