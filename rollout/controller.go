package rollout

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	register "github.com/argoproj/argo-rollouts/pkg/apis/rollouts"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	listers "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	serviceutil "github.com/argoproj/argo-rollouts/utils/service"
)

// RolloutController is the controller implementation for Rollout resources
type RolloutController struct {
	// rsControl is used for adopting/releasing replica sets.
	replicaSetControl controller.RSControlInterface

	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// argoprojclientset is a clientset for our own API group
	argoprojclientset clientset.Interface

	replicaSetLister  appslisters.ReplicaSetLister
	replicaSetSynced  cache.InformerSynced
	rolloutsLister    listers.RolloutLister
	rolloutsSynced    cache.InformerSynced
	rolloutsIndexer   cache.Indexer
	servicesLister    v1.ServiceLister
	experimentsLister listers.ExperimentLister
	metricsServer     *metrics.MetricsServer

	// used for unit testing
	enqueueRollout      func(obj interface{})
	enqueueRolloutAfter func(obj interface{}, duration time.Duration)

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	rolloutWorkqueue workqueue.RateLimitingInterface
	serviceWorkqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder     record.EventRecorder
	resyncPeriod time.Duration
}

// NewRolloutController returns a new rollout controller
func NewRolloutController(
	kubeclientset kubernetes.Interface,
	argoprojclientset clientset.Interface,
	experimentInformer informers.ExperimentInformer,
	replicaSetInformer appsinformers.ReplicaSetInformer,
	servicesInformer coreinformers.ServiceInformer,
	rolloutsInformer informers.RolloutInformer,
	resyncPeriod time.Duration,
	rolloutWorkQueue workqueue.RateLimitingInterface,
	serviceWorkQueue workqueue.RateLimitingInterface,
	metricsServer *metrics.MetricsServer,
	recorder record.EventRecorder) *RolloutController {

	replicaSetControl := controller.RealRSControl{
		KubeClient: kubeclientset,
		Recorder:   recorder,
	}

	controller := &RolloutController{
		kubeclientset:     kubeclientset,
		argoprojclientset: argoprojclientset,
		replicaSetControl: replicaSetControl,
		replicaSetLister:  replicaSetInformer.Lister(),
		replicaSetSynced:  replicaSetInformer.Informer().HasSynced,
		rolloutsIndexer:   rolloutsInformer.Informer().GetIndexer(),
		rolloutsLister:    rolloutsInformer.Lister(),
		rolloutsSynced:    rolloutsInformer.Informer().HasSynced,
		rolloutWorkqueue:  rolloutWorkQueue,
		serviceWorkqueue:  serviceWorkQueue,
		servicesLister:    servicesInformer.Lister(),
		experimentsLister: experimentInformer.Lister(),
		recorder:          recorder,
		resyncPeriod:      resyncPeriod,
		metricsServer:     metricsServer,
	}
	controller.enqueueRollout = func(obj interface{}) {
		controllerutil.EnqueueRateLimited(obj, rolloutWorkQueue)
	}
	controller.enqueueRolloutAfter = func(obj interface{}, duration time.Duration) {
		controllerutil.EnqueueAfter(obj, duration, rolloutWorkQueue)
	}
	log.Info("Setting up event handlers")
	// Set up an event handler for when rollout resources change
	rolloutsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueRollout,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueRollout(new)
		},
		DeleteFunc: func(obj interface{}) {
			if r, ok := obj.(*v1alpha1.Rollout); ok {
				for _, s := range serviceutil.GetRolloutServiceKeys(r) {
					controller.serviceWorkqueue.AddRateLimited(s)
				}
			}
		},
	})

	replicaSetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controllerutil.EnqueueParentObject(obj, register.RolloutKind, controller.rolloutsLister, controller.enqueueRollout)
		},
		UpdateFunc: func(old, new interface{}) {
			newRS := new.(*appsv1.ReplicaSet)
			oldRS := old.(*appsv1.ReplicaSet)
			if newRS.ResourceVersion == oldRS.ResourceVersion {
				// Periodic resync will send update events for all known replicas.
				// Two different versions of the same Replica will always have different RVs.
				return
			}
			controllerutil.EnqueueParentObject(new, register.RolloutKind, controller.rolloutsLister, controller.enqueueRollout)
		},
		DeleteFunc: func(obj interface{}) {
			controllerutil.EnqueueParentObject(obj, register.RolloutKind, controller.rolloutsLister, controller.enqueueRollout)
		},
	})
	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *RolloutController) Run(threadiness int, stopCh <-chan struct{}) error {
	log.Info("Starting Rollout workers")
	for i := 0; i < threadiness; i++ {
		go wait.Until(func() {
			controllerutil.RunWorker(c.rolloutWorkqueue, logutil.RolloutKey, c.syncHandler, c.metricsServer)
		}, time.Second, stopCh)
	}
	log.Info("Started Rollout workers")
	<-stopCh
	log.Info("Shutting down workers")

	return nil
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Rollout resource
// with the current status of the resource.
func (c *RolloutController) syncHandler(key string) error {
	startTime := time.Now()
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	log.WithField(logutil.RolloutKey, name).WithField(logutil.NamespaceKey, namespace).Infof("Started syncing rollout at (%v)", startTime)
	rollout, err := c.rolloutsLister.Rollouts(namespace).Get(name)
	if k8serrors.IsNotFound(err) {
		log.WithField(logutil.RolloutKey, name).WithField(logutil.NamespaceKey, namespace).Info("Rollout has been deleted")
		return nil
	}
	if err != nil {
		return err
	}

	// Remarshal the rollout to normalize all fields so that when we calculate hashes against the
	// rollout spec and pod template spec, the hash will be consistent. See issue #70
	// This also returns a copy of the rollout to prevent mutation of the informer cache.
	r := remarshalRollout(rollout)
	logCtx := logutil.WithRollout(r)

	if r.ObjectMeta.DeletionTimestamp != nil {
		logCtx.Info("No reconciliation as rollout marked for deletion")
		return nil
	}

	// In order to work with HPA, the rollout.Spec.Replica field cannot be nil. As a result, the controller will update
	// the rollout to have the replicas field set to the default value. see https://github.com/argoproj/argo-rollouts/issues/119
	if rollout.Spec.Replicas == nil {
		logCtx.Info("Setting .Spec.Replica to 1 from nil")
		r.Spec.Replicas = pointer.Int32Ptr(defaults.DefaultReplicas)
		_, err := c.argoprojclientset.ArgoprojV1alpha1().Rollouts(r.Namespace).Update(r)
		return err

	}
	defer func() {
		duration := time.Since(startTime)
		c.metricsServer.IncReconcile(r, duration)
		logCtx.WithField("time_ms", duration.Seconds()*1e3).Info("Reconciliation completed")
	}()

	prevCond := conditions.GetRolloutCondition(rollout.Status, v1alpha1.InvalidSpec)
	invalidSpecCond := conditions.VerifyRolloutSpec(r, prevCond)
	if invalidSpecCond != nil {
		logutil.WithRollout(r).Error("Spec submitted is invalid")
		generation := conditions.ComputeGenerationHash(r.Spec)
		if r.Status.ObservedGeneration != generation || !reflect.DeepEqual(invalidSpecCond, prevCond) {
			newStatus := r.Status.DeepCopy()
			newStatus.ObservedGeneration = generation
			// SetRolloutCondition only updates the condition when the status and/or reason changes, but
			// the controller should update the invalidSpec if there is a change in why the spec is invalid
			if prevCond != nil && prevCond.Message != invalidSpecCond.Message {
				conditions.RemoveRolloutCondition(newStatus, v1alpha1.InvalidSpec)
			}
			conditions.SetRolloutCondition(newStatus, *invalidSpecCond)
			err := c.persistRolloutStatus(r, newStatus, nil)
			if err != nil {
				return err
			}
		}
		return nil
	}

	err = c.checkPausedConditions(r)
	if err != nil {
		return err
	}

	// List ReplicaSets owned by this Rollout, while reconciling ControllerRef
	// through adoption/orphaning.
	rsList, err := c.getReplicaSetsForRollouts(r)
	if err != nil {
		return err
	}

	scalingEvent, err := c.isScalingEvent(r, rsList)
	if err != nil {
		return err
	}
	if scalingEvent {
		return c.syncScalingEvent(r, rsList)
	}
	if rollout.Spec.Strategy.BlueGreenStrategy != nil {
		return c.rolloutBlueGreen(r, rsList)
	}
	if rollout.Spec.Strategy.CanaryStrategy != nil {
		return c.rolloutCanary(r, rsList)
	}
	return fmt.Errorf("no rollout strategy selected")
}

func remarshalRollout(r *v1alpha1.Rollout) *v1alpha1.Rollout {
	rolloutBytes, err := json.Marshal(r)
	if err != nil {
		panic(err)
	}
	var remarshalled v1alpha1.Rollout
	err = json.Unmarshal(rolloutBytes, &remarshalled)
	if err != nil {
		panic(err)
	}
	return &remarshalled
}
