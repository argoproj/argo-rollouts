package experiments

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	informersv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
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
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/diff"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"
)

// Controller is the controller implementation for Experiment resources
type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// experimentsclientset is a clientset for our own API group
	argoProjClientset clientset.Interface

	// rsControl is used for adopting/releasing replica sets.
	replicaSetControl controller.RSControlInterface

	replicaSetLister              appslisters.ReplicaSetLister
	experimentsLister             listers.ExperimentLister
	analysisTemplateLister        listers.AnalysisTemplateLister
	clusterAnalysisTemplateLister listers.ClusterAnalysisTemplateLister
	analysisRunLister             listers.AnalysisRunLister
	serviceLister                 listersv1.ServiceLister

	replicaSetSynced              cache.InformerSynced
	experimentSynced              cache.InformerSynced
	analysisTemplateSynced        cache.InformerSynced
	clusterAnalysisTemplateSynced cache.InformerSynced
	analysisRunSynced             cache.InformerSynced

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

// ControllerConfig describes the data required to instantiate a new analysis controller
type ControllerConfig struct {
	KubeClientSet                   kubernetes.Interface
	ArgoProjClientset               clientset.Interface
	ReplicaSetInformer              appsinformers.ReplicaSetInformer
	ExperimentsInformer             informers.ExperimentInformer
	AnalysisRunInformer             informers.AnalysisRunInformer
	AnalysisTemplateInformer        informers.AnalysisTemplateInformer
	ClusterAnalysisTemplateInformer informers.ClusterAnalysisTemplateInformer
	ServiceInformer                 informersv1.ServiceInformer
	ResyncPeriod                    time.Duration
	RolloutWorkQueue                workqueue.RateLimitingInterface
	ExperimentWorkQueue             workqueue.RateLimitingInterface
	MetricsServer                   *metrics.MetricsServer
	Recorder                        record.EventRecorder
}

// NewController returns a new experiment controller
func NewController(cfg ControllerConfig) *Controller {

	replicaSetControl := controller.RealRSControl{
		KubeClient: cfg.KubeClientSet,
		Recorder:   cfg.Recorder.K8sRecorder(),
	}

	controller := &Controller{
		kubeclientset:                 cfg.KubeClientSet,
		argoProjClientset:             cfg.ArgoProjClientset,
		replicaSetControl:             replicaSetControl,
		replicaSetLister:              cfg.ReplicaSetInformer.Lister(),
		experimentsLister:             cfg.ExperimentsInformer.Lister(),
		analysisTemplateLister:        cfg.AnalysisTemplateInformer.Lister(),
		clusterAnalysisTemplateLister: cfg.ClusterAnalysisTemplateInformer.Lister(),
		analysisRunLister:             cfg.AnalysisRunInformer.Lister(),
		serviceLister:                 cfg.ServiceInformer.Lister(),
		metricsServer:                 cfg.MetricsServer,
		rolloutWorkqueue:              cfg.RolloutWorkQueue,
		experimentWorkqueue:           cfg.ExperimentWorkQueue,

		replicaSetSynced:              cfg.ReplicaSetInformer.Informer().HasSynced,
		experimentSynced:              cfg.ExperimentsInformer.Informer().HasSynced,
		analysisRunSynced:             cfg.AnalysisRunInformer.Informer().HasSynced,
		analysisTemplateSynced:        cfg.AnalysisTemplateInformer.Informer().HasSynced,
		clusterAnalysisTemplateSynced: cfg.ClusterAnalysisTemplateInformer.Informer().HasSynced,
		recorder:                      cfg.Recorder,
		resyncPeriod:                  cfg.ResyncPeriod,
	}

	controller.enqueueExperiment = func(obj interface{}) {
		controllerutil.Enqueue(obj, cfg.ExperimentWorkQueue)
	}
	controller.enqueueExperimentAfter = func(obj interface{}, duration time.Duration) {
		controllerutil.EnqueueAfter(obj, duration, cfg.ExperimentWorkQueue)
	}

	log.Info("Setting up experiments event handlers")
	// Set up an event handler for when experiment resources change
	cfg.ExperimentsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueExperiment,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueExperiment(new)
		},
		DeleteFunc: controller.enqueueExperiment,
	})

	cfg.ExperimentsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			enqueueRollout := func(obj interface{}) {
				controllerutil.Enqueue(obj, cfg.RolloutWorkQueue)
			}
			controllerutil.EnqueueParentObject(obj, register.RolloutKind, enqueueRollout)
		},
		UpdateFunc: func(old, new interface{}) {
			oldAcc, err := meta.Accessor(old)
			if err != nil {
				return
			}
			newAcc, err := meta.Accessor(new)
			if err != nil {
				return
			}
			if oldAcc.GetResourceVersion() == newAcc.GetResourceVersion() {
				// Periodic resync will send update events for all known replicas.
				// Two different versions of the same Replica will always have different RVs.
				return
			}
			enqueueRollout := func(obj interface{}) {
				controllerutil.Enqueue(obj, cfg.RolloutWorkQueue)
			}
			controllerutil.EnqueueParentObject(new, register.RolloutKind, enqueueRollout)
		},
		DeleteFunc: func(obj interface{}) {
			enqueueRollout := func(obj interface{}) {
				controllerutil.Enqueue(obj, cfg.RolloutWorkQueue)
			}
			controllerutil.EnqueueParentObject(obj, register.RolloutKind, enqueueRollout)
			if ex := unstructuredutil.ObjectToExperiment(obj); ex != nil {
				logCtx := logutil.WithExperiment(ex)
				logCtx.Info("experiment deleted")
				controller.metricsServer.Remove(ex.Namespace, ex.Name, logutil.ExperimentKey)
			}
		},
	})

	cfg.ReplicaSetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
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
			if defaults.GetReplicasOrDefault(newRS.Spec.Replicas) == defaults.GetReplicasOrDefault(oldRS.Spec.Replicas) &&
				newRS.Status.Replicas == oldRS.Status.Replicas &&
				newRS.Status.ReadyReplicas == oldRS.Status.ReadyReplicas &&
				newRS.Status.AvailableReplicas == oldRS.Status.AvailableReplicas {
				// we only care about changes to replicaset's replica counters. ignore everything else
				return
			}
			controllerutil.EnqueueParentObject(new, register.ExperimentKind, controller.enqueueExperiment)
		},
		DeleteFunc: func(obj interface{}) {
			controllerutil.EnqueueParentObject(obj, register.ExperimentKind, controller.enqueueExperiment)
		},
	})

	cfg.AnalysisRunInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controller.enqueueIfCompleted(obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			controller.enqueueIfCompleted(newObj)
		},
		DeleteFunc: func(obj interface{}) {
			controller.enqueueIfCompleted(obj)
		},
	})
	return controller
}

// Run starts the controller threads
func (ec *Controller) Run(ctx context.Context, threadiness int) error {
	log.Info("Starting Experiment workers")
	wg := sync.WaitGroup{}
	for i := 0; i < threadiness; i++ {
		wg.Add(1)
		go wait.Until(func() {
			controllerutil.RunWorker(ctx, ec.experimentWorkqueue, logutil.ExperimentKey, ec.syncHandler, ec.metricsServer)
			log.Debug("Experiment worker has stopped")
			wg.Done()
		}, time.Second, ctx.Done())
	}
	log.Info("Started Experiment workers")
	<-ctx.Done()
	wg.Wait()
	log.Info("All experiment workers have stopped")

	return nil
}

func (ec *Controller) syncHandler(ctx context.Context, key string) error {
	startTime := timeutil.Now()
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
		ec.metricsServer.IncExperimentReconcile(experiment, duration)
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

	templateServices, err := ec.getServicesForExperiment(experiment)
	if err != nil {
		return err
	}

	exCtx := newExperimentContext(
		experiment,
		templateRSs,
		templateServices,
		ec.kubeclientset,
		ec.argoProjClientset,
		ec.replicaSetLister,
		ec.analysisTemplateLister,
		ec.clusterAnalysisTemplateLister,
		ec.analysisRunLister,
		ec.serviceLister,
		ec.recorder,
		ec.resyncPeriod,
		ec.enqueueExperimentAfter,
	)

	newStatus := exCtx.reconcile()
	return ec.persistExperimentStatus(experiment, newStatus)
}

func (ec *Controller) persistExperimentStatus(orig *v1alpha1.Experiment, newStatus *v1alpha1.ExperimentStatus) error {
	ctx := context.TODO()
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
	_, err = ec.argoProjClientset.ArgoprojV1alpha1().Experiments(orig.Namespace).Patch(ctx, orig.Name, patchtypes.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		logCtx.Warningf("Error updating experiment: %v", err)
		return err
	}
	logCtx.Info("Patch status successfully")
	return nil
}

// enqueueIfCompleted conditionally enqueues the AnalysisRun's Experiment if the run is complete
func (ec *Controller) enqueueIfCompleted(obj interface{}) {
	run := unstructuredutil.ObjectToAnalysisRun(obj)
	if run == nil {
		return
	}
	if run.Status.Phase.Completed() {
		controllerutil.EnqueueParentObject(run, register.ExperimentKind, ec.enqueueExperiment)
	}
}
