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
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"
)

const (
	// ingressIndexName is the index by which Ingress resources are cached
	ingressIndexName = "byIngress"
)

// ControllerConfig describes the data required to instantiate a new ingress controller
type ControllerConfig struct {
	Client           kubernetes.Interface
	IngressInformer  extensionsinformers.IngressInformer
	IngressWorkQueue workqueue.RateLimitingInterface

	RolloutsInformer informers.RolloutInformer
	RolloutWorkQueue workqueue.RateLimitingInterface

	MetricsServer *metrics.MetricsServer
	ALBClasses    []string
	NGINXClasses  []string
}

// Controller describes an ingress controller
type Controller struct {
	client           kubernetes.Interface
	rolloutsIndexer  cache.Indexer
	ingressLister    extentionslisters.IngressLister
	ingressWorkqueue workqueue.RateLimitingInterface

	metricServer   *metrics.MetricsServer
	enqueueRollout func(obj interface{})
	albClasses     []string
	nginxClasses   []string
}

// NewController returns a new ingress controller
func NewController(cfg ControllerConfig) *Controller {

	controller := &Controller{
		client:          cfg.Client,
		rolloutsIndexer: cfg.RolloutsInformer.Informer().GetIndexer(),
		ingressLister:   cfg.IngressInformer.Lister(),

		ingressWorkqueue: cfg.IngressWorkQueue,
		metricServer:     cfg.MetricsServer,
		albClasses:       cfg.ALBClasses,
		nginxClasses:     cfg.NGINXClasses,
	}

	util.CheckErr(cfg.RolloutsInformer.Informer().AddIndexers(cache.Indexers{
		ingressIndexName: func(obj interface{}) ([]string, error) {
			if ro := unstructuredutil.ObjectToRollout(obj); ro != nil {
				return ingressutil.GetRolloutIngressKeys(ro), nil
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
	ingress, err := c.ingressLister.Ingresses(namespace).Get(name)
	if err != nil {
		if !errors.IsNotFound(err) {
			// Unknown error occurred
			return err
		}

		if !strings.HasSuffix(name, ingressutil.CanaryIngressSuffix) {
			// a primary ingress was deleted, simply ignore the event
			log.WithField(logutil.IngressKey, key).Warn("primary ingress has been deleted")
		}
		return nil
	}
	rollouts, err := c.getRolloutsByIngress(ingress.Namespace, ingress.Name)
	if err != nil {
		return nil
	}
	// An ingress without annotations cannot be a alb or nginx ingress
	if ingress.Annotations == nil {
		return nil
	}
	class := ingress.Annotations["kubernetes.io/ingress.class"]
	switch {
	case hasClass(c.albClasses, class):
		return c.syncALBIngress(ingress, rollouts)
	case hasClass(c.nginxClasses, class):
		return c.syncNginxIngress(name, namespace, rollouts)
	default:
		return nil
	}
}

func hasClass(classes []string, class string) bool {
	for _, str := range classes {
		if str == class {
			return true
		}
	}
	return false
}

func (c *Controller) syncNginxIngress(name, namespace string, rollouts []*v1alpha1.Rollout) error {
	for i := range rollouts {
		// reconciling the Rollout will ensure the canaryIngress is updated or created
		c.enqueueRollout(rollouts[i])
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
	for _, obj := range objs {
		if ro := unstructuredutil.ObjectToRollout(obj); ro != nil {
			rollouts = append(rollouts, ro)
		}
	}
	return rollouts, nil
}
