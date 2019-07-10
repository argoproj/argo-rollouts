package experiments

import (
	"time"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"

	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubernetes/pkg/controller"

	"fmt"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	listers "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
)

// ExperimentController is the controller implementation for Experiment resources
type ExperimentController struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// experimentsclientset is a clientset for our own API group
	arogProjClientset clientset.Interface

	// rsControl is used for adopting/releasing replica sets.
	replicaSetControl controller.RSControlInterface

	replicaSetLister  appslisters.ReplicaSetLister
	rolloutsLister    listers.RolloutLister
	experimentsLister listers.ExperimentLister

	replicaSetSynced cache.InformerSynced
	experimentSynced cache.InformerSynced
	rolloutSynced    cache.InformerSynced

	metricsServer *metrics.MetricsServer

	// used for unit testing
	enqueueExperiment      func(obj interface{})
	enqueueExperimentAfter func(obj interface{}, duration time.Duration)

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	rolloutWorkqueue    workqueue.RateLimitingInterface
	experimentWorkqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder     record.EventRecorder
	resyncPeriod time.Duration
}

// NewExperimentController returns a new experiment controller
func NewExperimentController(
	kubeclientset kubernetes.Interface,
	arogProjClientset clientset.Interface,
	replicaSetInformer appsinformers.ReplicaSetInformer,
	rolloutsInformer informers.RolloutInformer,
	experimentsInformer informers.ExperimentInformer,
	resyncPeriod time.Duration,
	rolloutWorkQueue workqueue.RateLimitingInterface,
	experimentWorkQueue workqueue.RateLimitingInterface,
	metricsServer *metrics.MetricsServer,
	recorder record.EventRecorder) *ExperimentController {

	replicaSetControl := controller.RealRSControl{
		KubeClient: kubeclientset,
		Recorder:   recorder,
	}

	controller := &ExperimentController{
		kubeclientset:       kubeclientset,
		arogProjClientset:   arogProjClientset,
		replicaSetControl:   replicaSetControl,
		replicaSetLister:    replicaSetInformer.Lister(),
		rolloutsLister:      rolloutsInformer.Lister(),
		experimentsLister:   experimentsInformer.Lister(),
		metricsServer:       metricsServer,
		rolloutWorkqueue:    rolloutWorkQueue,
		experimentWorkqueue: experimentWorkQueue,

		replicaSetSynced: replicaSetInformer.Informer().HasSynced,
		experimentSynced: experimentsInformer.Informer().HasSynced,
		rolloutSynced:    rolloutsInformer.Informer().HasSynced,
		recorder:         recorder,
		resyncPeriod:     resyncPeriod,
	}

	controller.enqueueExperiment = controller.enqueue
	controller.enqueueExperimentAfter = controller.enqueueAfter

	log.Info("Setting up experiments event handlers")
	// Set up an event handler for when experiment resources change
	experimentsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueExperiment,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueExperiment(new)
		},
		DeleteFunc: func(obj interface{}) {
			controller.enqueueExperiment(obj)
		},
	})

	rolloutsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueRolloutFromExperiment,
		UpdateFunc: func(old, new interface{}) {
			newRollout := new.(*v1alpha1.Rollout)
			oldRollout := old.(*v1alpha1.Rollout)
			if newRollout.ResourceVersion == oldRollout.ResourceVersion {
				// Periodic resync will send update events for all known replicas.
				// Two different versions of the same Replica will always have different RVs.
				return
			}
			controller.enqueueRolloutFromExperiment(new)
		},
		DeleteFunc: controller.enqueueRolloutFromExperiment,
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
	return controller
}

func (ec *ExperimentController) handleObject(obj interface{}) {
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
		// If this object is not owned by a experiment, we should not do anything more
		// with it.
		if ownerRef.Kind != "Experiment" {
			return
		}

		experiment, err := ec.experimentsLister.Experiments(object.GetNamespace()).Get(ownerRef.Name)
		if err != nil {
			log.Infof("ignoring orphaned object '%s' of experiment '%s'", object.GetSelfLink(), ownerRef.Name)
			return
		}

		controllerutil.Enqueue(experiment, ec.experimentWorkqueue)
		return
	}
}

func (ec *ExperimentController) enqueue(obj interface{}) {
	controllerutil.Enqueue(obj, ec.experimentWorkqueue)

}

func (ec *ExperimentController) enqueueAfter(obj interface{}, duration time.Duration) {
	controllerutil.EnqueueAfter(obj, duration, ec.experimentWorkqueue)

}

func (ec *ExperimentController) enqueueRolloutFromExperiment(obj interface{}) {
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

		rollout, err := ec.rolloutsLister.Rollouts(object.GetNamespace()).Get(ownerRef.Name)
		if err != nil {
			log.Infof("ignoring orphaned object '%s' of rollout '%s'", object.GetSelfLink(), ownerRef.Name)
			return
		}

		controllerutil.Enqueue(rollout, ec.rolloutWorkqueue)
		return
	}
}

func (ec *ExperimentController) Run(threadiness int, stopCh <-chan struct{}) error {
	log.Info("Starting Experiment workers")
	for i := 0; i < threadiness; i++ {
		go wait.Until(func() {
			controllerutil.RunWorker(ec.experimentWorkqueue, logutil.ExperimentKey, ec.syncHandler, ec.metricsServer)
		}, time.Second, stopCh)
	}
	log.Info("Started Experiment workers")
	<-stopCh
	log.Info("Shutting down experiment workers")

	return nil
}

func (ec *ExperimentController) syncHandler(key string) error {
	startTime := time.Now()
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	log.WithField(logutil.ExperimentKey, name).WithField(logutil.NamespaceKey, namespace).Infof("Started syncing Experiment at (%v)", startTime)
	experiment, err := ec.experimentsLister.Experiments(namespace).Get(name)
	if k8serrors.IsNotFound(err) {
		log.WithField(logutil.ExperimentKey, name).WithField(logutil.NamespaceKey, namespace).Info("Experiment has been deleted")
		return nil
	}
	if err != nil {
		return err
	}

	defer func() {
		duration := time.Since(startTime)
		//ec.metricsServer.IncReconcile(r, duration)
		logCtx := logutil.WithExperiment(experiment).WithField("time_ms", duration.Seconds()*1e3)
		logCtx.Info("Reconciliation completed")
	}()
	return nil
}