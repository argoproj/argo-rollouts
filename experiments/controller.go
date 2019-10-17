package experiments

import (
	"time"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	patchtypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	register "github.com/argoproj/argo-rollouts/pkg/apis/rollouts"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	listers "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	"github.com/argoproj/argo-rollouts/utils/diff"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

// ExperimentController is the controller implementation for Experiment resources
type ExperimentController struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// experimentsclientset is a clientset for our own API group
	argoProjClientset clientset.Interface

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
	argoProjClientset clientset.Interface,
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
		argoProjClientset:   argoProjClientset,
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

	controller.enqueueExperiment = func(obj interface{}) {
		controllerutil.Enqueue(obj, experimentWorkQueue)
	}
	controller.enqueueExperimentAfter = func(obj interface{}, duration time.Duration) {
		controllerutil.EnqueueAfter(obj, duration, experimentWorkQueue)
	}

	log.Info("Setting up experiments event handlers")
	// Set up an event handler for when experiment resources change
	experimentsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueExperiment,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueExperiment(new)
		},
		DeleteFunc: controller.enqueueExperiment,
	})

	experimentsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			enqueueRollout := func(obj interface{}) {
				controllerutil.Enqueue(obj, rolloutWorkQueue)
			}
			controllerutil.EnqueueParentObject(obj, register.RolloutKind, enqueueRollout)
		},
		UpdateFunc: func(old, new interface{}) {
			newExperiment := new.(*v1alpha1.Experiment)
			oldExperiment := old.(*v1alpha1.Experiment)
			if newExperiment.ResourceVersion == oldExperiment.ResourceVersion {
				// Periodic resync will send update events for all known replicas.
				// Two different versions of the same Replica will always have different RVs.
				return
			}
			enqueueRollout := func(obj interface{}) {
				controllerutil.Enqueue(obj, rolloutWorkQueue)
			}
			controllerutil.EnqueueParentObject(new, register.RolloutKind, enqueueRollout)
		},
		DeleteFunc: func(obj interface{}) {
			enqueueRollout := func(obj interface{}) {
				controllerutil.Enqueue(obj, rolloutWorkQueue)
			}
			controllerutil.EnqueueParentObject(obj, register.RolloutKind, enqueueRollout)
		},
	})

	replicaSetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controllerutil.EnqueueParentObject(obj, register.ExperimentKind, controller.enqueueExperiment)
		},
		UpdateFunc: func(old, new interface{}) {
			newRS := new.(*appsv1.ReplicaSet)
			oldRS := old.(*appsv1.ReplicaSet)
			if newRS.ResourceVersion == oldRS.ResourceVersion {
				// Periodic resync will send update events for all known replicas.
				// Two different versions of the same Replica will always have different RVs.
				return
			}
			controllerutil.EnqueueParentObject(new, register.ExperimentKind, controller.enqueueExperiment)
		},
		DeleteFunc: func(obj interface{}) {
			controllerutil.EnqueueParentObject(obj, register.ExperimentKind, controller.enqueueExperiment)
		},
	})
	return controller
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
	logCtx := log.WithField(logutil.ExperimentKey, name).WithField(logutil.NamespaceKey, namespace)
	logCtx.Infof("Started syncing Experiment at (%v)", startTime)
	experiment, err := ec.experimentsLister.Experiments(namespace).Get(name)
	if k8serrors.IsNotFound(err) {
		logCtx.Info("Experiment has been deleted")
		return nil
	}
	if err != nil {
		return err
	}

	defer func() {
		duration := time.Since(startTime)
		//TODO(dthomson) Add metrics for experiments
		//ec.metricsServer.IncReconcile(r, duration)
		logCtx.WithField("time_ms", duration.Seconds()*1e3).Info("Reconciliation completed")
	}()

	if experiment.DeletionTimestamp != nil {
		logCtx.Info("No reconciliation as experiment marked for deletion")
		return nil
	}

	prevCond := conditions.GetExperimentCondition(experiment.Status, v1alpha1.InvalidExperimentSpec)
	invalidSpecCond := conditions.VerifyExperimentSpec(experiment, prevCond)
	if invalidSpecCond != nil {
		logCtx.Error("Spec submitted is invalid")
		newStatus := experiment.Status.DeepCopy()
		// SetExperimentCondition only updates the condition when the status and/or reason changes, but
		// the controller should update the invalidSpec if there is a change in why the spec is invalid
		if prevCond != nil && prevCond.Message != invalidSpecCond.Message {
			conditions.RemoveExperimentCondition(newStatus, v1alpha1.InvalidExperimentSpec)
		}
		conditions.SetExperimentCondition(newStatus, *invalidSpecCond)
		return ec.persistExperimentStatus(experiment, newStatus)
	}

	// List ReplicaSets owned by this Experiment, while reconciling ControllerRef
	// through adoption/orphaning.
	templateRSs, err := ec.getReplicaSetsForExperiment(experiment)
	if err != nil {
		return err
	}

	exCtx := experimentContext{
		log:                    logCtx,
		ex:                     experiment,
		templateRSs:            templateRSs,
		kubeclientset:          ec.kubeclientset,
		argoProjClientset:      ec.argoProjClientset,
		replicaSetLister:       ec.replicaSetLister,
		recorder:               ec.recorder,
		enqueueExperimentAfter: ec.enqueueExperimentAfter,
	}

	newStatus, err := exCtx.reconcile()
	if err != nil {
		return err
	}
	return ec.persistExperimentStatus(experiment, newStatus)
}

func (ec *ExperimentController) persistExperimentStatus(orig *v1alpha1.Experiment, newStatus *v1alpha1.ExperimentStatus) error {
	logCtx := logutil.WithExperiment(orig)
	patch, modified, err := diff.CreateTwoWayMergePatch(
		&v1alpha1.Experiment{
			Status: orig.Status,
		},
		&v1alpha1.Experiment{
			Status: *newStatus,
		}, v1alpha1.Experiment{})
	if err != nil {
		logCtx.Errorf("Error constructing app status patch: %v", err)
		return err
	}
	if !modified {
		logCtx.Info("No status changes. Skipping patch")
		return nil
	}
	logCtx.Debugf("Experiment Patch: %s", patch)
	_, err = ec.argoProjClientset.ArgoprojV1alpha1().Experiments(orig.Namespace).Patch(orig.Name, patchtypes.MergePatchType, patch)
	if err != nil {
		logCtx.Warningf("Error updating experiment: %v", err)
		return err
	}
	logCtx.Info("Patch status successfully")
	return nil
}
