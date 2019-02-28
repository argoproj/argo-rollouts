package controller

import (
	"fmt"
	"reflect"
	"time"

	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	appslisters "k8s.io/client-go/listers/apps/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	rolloutscheme "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/scheme"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	listers "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

const controllerAgentName = "rollouts-controller"

const (
	// SuccessSynced is used as part of the Event 'reason' when a Rollout is synced
	SuccessSynced = "Synced"
	// ErrResourceExists is used as part of the Event 'reason' when a Rollout fails
	// to sync due to a Replica of the same name already existing.
	ErrResourceExists = "ErrResourceExists"

	// MessageResourceExists is the message used for Events when a resource
	// fails to sync due to a Replica already existing
	MessageResourceExists = "Resource %q already exists and is not managed by Rollout"
	// MessageResourceSynced is the message used for an Event fired when a Rollout
	// is synced successfully
	MessageResourceSynced = "Rollout synced successfully"

	// DefaultRolloutResyncPeriod Default time in seconds for rollout resync period
	DefaultRolloutResyncPeriod = 30
)

// Controller is the controller implementation for Rollout resources
type Controller struct {
	// rsControl is used for adopting/releasing replica sets.
	replicaSetControl controller.RSControlInterface

	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// rolloutsclientset is a clientset for our own API group
	rolloutsclientset clientset.Interface

	replicaSetLister appslisters.ReplicaSetLister
	replicaSetSynced cache.InformerSynced
	serviceLister    corelisters.ServiceLister
	serviceSynced    cache.InformerSynced
	rolloutsLister   listers.RolloutLister
	rolloutsSynced   cache.InformerSynced

	// used for unit testing
	enqueueRollout      func(obj interface{})
	enqueueRolloutAfter func(obj interface{}, duration time.Duration)

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder     record.EventRecorder
	resyncPeriod time.Duration
}

// NewController returns a new rollout controller
func NewController(
	kubeclientset kubernetes.Interface,
	rolloutsclientset clientset.Interface,
	replicaSetInformer appsinformers.ReplicaSetInformer,
	serviceInformer coreinformers.ServiceInformer,
	rolloutsInformer informers.RolloutInformer,
	resyncPeriod time.Duration) *Controller {

	// Create event broadcaster
	// Add rollouts-controller types to the default Kubernetes Scheme so Events can be
	// logged for argo-rollouts types.
	utilruntime.Must(rolloutscheme.AddToScheme(scheme.Scheme))
	log.Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})
	replicaSetControl := controller.RealRSControl{
		KubeClient: kubeclientset,
		Recorder:   recorder,
	}

	controller := &Controller{
		kubeclientset:     kubeclientset,
		rolloutsclientset: rolloutsclientset,
		replicaSetControl: replicaSetControl,
		replicaSetLister:  replicaSetInformer.Lister(),
		replicaSetSynced:  replicaSetInformer.Informer().HasSynced,
		serviceLister:     serviceInformer.Lister(),
		serviceSynced:     serviceInformer.Informer().HasSynced,
		rolloutsLister:    rolloutsInformer.Lister(),
		rolloutsSynced:    rolloutsInformer.Informer().HasSynced,
		workqueue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Rollouts"),
		recorder:          recorder,
		resyncPeriod:      resyncPeriod,
	}
	controller.enqueueRollout = controller.enqueueRateLimited
	controller.enqueueRolloutAfter = controller.enqueueAfter

	log.Info("Setting up event handlers")
	// Set up an event handler for when rollout resources change
	rolloutsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueRollout,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueRollout(new)
		},
	})
	replicaSetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.handleObject,
		UpdateFunc: func(old, new interface{}) {
			newRS := new.(*appsv1.ReplicaSet)
			oldRS := old.(*appsv1.ReplicaSet)
			if newRS.ResourceVersion == oldRS.ResourceVersion {
				// Periodic resync will send update events for all known replicas.
				// Two different versions of the same Replica will always have different RVs.
				return
			}
			controller.handleObject(new)
		},
		DeleteFunc: controller.handleObject,
	})

	serviceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.handleService,
		UpdateFunc: controller.updateService,
		DeleteFunc: controller.handleService,
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	log.Info("Starting Rollout controller")

	// Wait for the caches to be synced before starting workers
	log.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.replicaSetSynced, c.rolloutsSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	log.Info("Starting workers")
	// Launch two workers to process Rollout resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	log.Info("Started workers")
	<-stopCh
	log.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// Rollout resource to be synced.
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		log.WithField(logutil.RolloutKey, key).Infof("Successfully synced")
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Rollout resource
// with the current status of the resource.
func (c *Controller) syncHandler(key string) error {
	startTime := time.Now()
	log.WithField(logutil.RolloutKey, key).Infof("Started syncing rollout at (%v)", startTime)
	defer func() {
		log.WithField(logutil.RolloutKey, key).Infof("Finished syncing rollout (%v)", time.Since(startTime))
	}()

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	rollout, err := c.rolloutsLister.Rollouts(namespace).Get(name)
	if errors.IsNotFound(err) {
		log.WithField(logutil.RolloutKey, key).Infof("Rollout %v has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	// Deep-copy otherwise we are mutating our cache.
	r := rollout.DeepCopy()

	prevCond := conditions.GetRolloutCondition(rollout.Status, v1alpha1.InvalidSpec)
	invalidSpecCond := conditions.VerifyRolloutSpec(r, prevCond)
	if invalidSpecCond != nil {
		logutil.WithRollout(r).Error("Error: Spec submitted is invalid")
		generation := conditions.ComputeGenerationHash(r.Spec)
		if r.Status.ObservedGeneration != generation || !reflect.DeepEqual(invalidSpecCond, prevCond) {
			newStatus := r.Status
			newStatus.ObservedGeneration = generation
			conditions.SetRolloutCondition(&newStatus, *invalidSpecCond)
			err := c.persistRolloutStatus(r, &newStatus, r.Spec.Pause)
			if err != nil {
				return err
			}
		}
		return nil
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
		return c.sync(r, rsList)
	}
	if rollout.Spec.Strategy.BlueGreenStrategy != nil {
		return c.rolloutBlueGreen(r, rsList)
	}
	if rollout.Spec.Strategy.CanaryStrategy != nil {
		return c.rolloutCanary(r, rsList)
	}
	return fmt.Errorf("no rollout strategy selected")
}

func (c *Controller) enqueue(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}

func (c *Controller) enqueueRateLimited(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	c.workqueue.AddRateLimited(key)
}

func (c *Controller) enqueueAfter(obj interface{}, duration time.Duration) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	c.workqueue.AddAfter(key, duration)
}

// handleObject will take any resource implementing metav1.Object and attempt
// to find the Rollout resource that 'owns' it. It does this by looking at the
// objects metadata.ownerReferences field for an appropriate OwnerReference.
// It then enqueues that Rollout resource to be processed. If the object does not
// have an appropriate OwnerReference, it will simply be skipped.
func (c *Controller) handleObject(obj interface{}) {
	var object metav1.Object
	var ok bool
	if object, ok = obj.(metav1.Object); !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			runtime.HandleError(fmt.Errorf("error decoding object, invalid type"))
			return
		}
		object, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			runtime.HandleError(fmt.Errorf("error decoding object tombstone, invalid type"))
			return
		}
		log.Infof("Recovered deleted object '%s' from tombstone", object.GetName())
	}
	log.Infof("Processing object: %s", object.GetName())
	if ownerRef := metav1.GetControllerOf(object); ownerRef != nil {
		// If this object is not owned by a Rollout, we should not do anything more
		// with it.
		if ownerRef.Kind != "Rollout" {
			return
		}

		rollout, err := c.rolloutsLister.Rollouts(object.GetNamespace()).Get(ownerRef.Name)
		if err != nil {
			log.Infof("ignoring orphaned object '%s' of rollout '%s'", object.GetSelfLink(), ownerRef.Name)
			return
		}

		c.enqueueRollout(rollout)
		return
	}
}
