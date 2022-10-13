package ingress

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
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
	IngressWrap      *ingressutil.IngressWrap
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
	ingressWrapper   IngressWrapper
	ingressWorkqueue workqueue.RateLimitingInterface

	metricServer   *metrics.MetricsServer
	enqueueRollout func(obj interface{})
	albClasses     []string
	nginxClasses   []string
}

type IngressWrapper interface {
	GetCached(namespace, name string) (*ingressutil.Ingress, error)
	Update(ctx context.Context, namespace string, ingress *ingressutil.Ingress) (*ingressutil.Ingress, error)
}

// NewController returns a new ingress controller
func NewController(cfg ControllerConfig) *Controller {

	controller := &Controller{
		client:          cfg.Client,
		rolloutsIndexer: cfg.RolloutsInformer.Informer().GetIndexer(),
		ingressWrapper:  cfg.IngressWrap,

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

	cfg.IngressWrap.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
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
func (c *Controller) Run(ctx context.Context, threadiness int) error {
	log.Info("Starting Ingress workers")
	wg := sync.WaitGroup{}
	for i := 0; i < threadiness; i++ {
		wg.Add(1)
		go wait.Until(func() {
			controllerutil.RunWorker(ctx, c.ingressWorkqueue, logutil.IngressKey, c.syncIngress, c.metricServer)
			wg.Done()
			log.Debug("Ingress worker has stopped")
		}, time.Second, ctx.Done())
	}

	log.Info("Started Ingress workers")
	<-ctx.Done()
	wg.Wait()
	log.Info("All ingress workers have stopped")

	return nil
}

// syncIngress queues all rollouts referencing the Ingress for reconciliation
func (c *Controller) syncIngress(ctx context.Context, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	ingress, err := c.ingressWrapper.GetCached(namespace, name)
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
	rollouts, err := c.getRolloutsByIngress(ingress.GetNamespace(), ingress.GetName())
	if err != nil {
		return nil
	}
	class := ingress.GetClass()
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
