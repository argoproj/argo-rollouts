package analysis

import (
	"context"
	"sync"
	"time"

	"github.com/argoproj/argo-rollouts/metric"
	jobProvider "github.com/argoproj/argo-rollouts/metricproviders/job"
	"github.com/aws/smithy-go/ptr"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"

	log "github.com/sirupsen/logrus"
	batchv1 "k8s.io/api/batch/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	batchinformers "k8s.io/client-go/informers/batch/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/metricproviders"
	register "github.com/argoproj/argo-rollouts/pkg/apis/rollouts"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	listers "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

var (
	analysisRunGVK = v1alpha1.SchemeGroupVersion.WithKind("AnalysisRun")
)

// Controller is the controller implementation for Analysis resources
type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// analysisclientset is a clientset for our own API group
	argoProjClientset clientset.Interface

	analysisRunLister listers.AnalysisRunLister

	analysisRunSynced cache.InformerSynced

	jobInformer batchinformers.JobInformer

	metricsServer *metrics.MetricsServer

	newProvider func(logCtx log.Entry, metric v1alpha1.Metric) (metric.Provider, error)

	// used for unit testing
	enqueueAnalysis      func(obj any)
	enqueueAnalysisAfter func(obj any, duration time.Duration)

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	analysisRunWorkQueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder     record.EventRecorder
	resyncPeriod time.Duration
}

// ControllerConfig describes the data required to instantiate a new analysis controller
type ControllerConfig struct {
	KubeClientSet        kubernetes.Interface
	ArgoProjClientset    clientset.Interface
	AnalysisRunInformer  informers.AnalysisRunInformer
	JobInformer          batchinformers.JobInformer
	ResyncPeriod         time.Duration
	AnalysisRunWorkQueue workqueue.RateLimitingInterface
	MetricsServer        *metrics.MetricsServer
	Recorder             record.EventRecorder
}

// NewController returns a new analysis controller
func NewController(cfg ControllerConfig) *Controller {

	controller := &Controller{
		kubeclientset:        cfg.KubeClientSet,
		argoProjClientset:    cfg.ArgoProjClientset,
		analysisRunLister:    cfg.AnalysisRunInformer.Lister(),
		metricsServer:        cfg.MetricsServer,
		analysisRunWorkQueue: cfg.AnalysisRunWorkQueue,
		jobInformer:          cfg.JobInformer,
		analysisRunSynced:    cfg.AnalysisRunInformer.Informer().HasSynced,
		recorder:             cfg.Recorder,
		resyncPeriod:         cfg.ResyncPeriod,
	}

	controller.enqueueAnalysis = func(obj any) {
		controllerutil.Enqueue(obj, cfg.AnalysisRunWorkQueue)
	}
	controller.enqueueAnalysisAfter = func(obj any, duration time.Duration) {
		controllerutil.EnqueueAfter(obj, duration, cfg.AnalysisRunWorkQueue)
	}

	providerFactory := metricproviders.ProviderFactory{
		KubeClient: controller.kubeclientset,
		JobLister:  cfg.JobInformer.Lister(),
	}
	controller.newProvider = providerFactory.NewProvider

	cfg.JobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			controller.enqueueJobIfCompleted(obj)
		},
		UpdateFunc: func(oldObj, newObj any) {
			controller.enqueueJobIfCompleted(newObj)
		},
		DeleteFunc: func(obj any) {
			controller.enqueueJobIfCompleted(obj)
		},
	})

	log.Info("Setting up analysis event handlers")
	// Set up an event handler for when analysis resources change
	cfg.AnalysisRunInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueAnalysis,
		UpdateFunc: func(old, new any) {
			controller.enqueueAnalysis(new)
		},
		DeleteFunc: func(obj any) {
			controller.enqueueAnalysis(obj)
			if ar := unstructuredutil.ObjectToAnalysisRun(obj); ar != nil {
				logCtx := logutil.WithAnalysisRun(ar)
				logCtx.Info("analysis run deleted")
				controller.metricsServer.Remove(ar.Namespace, ar.Name, logutil.AnalysisRunKey)
			}
		},
	})
	return controller
}

func (c *Controller) Run(ctx context.Context, threadiness int) error {
	log.Info("Starting analysis workers")
	wg := sync.WaitGroup{}
	for i := 0; i < threadiness; i++ {
		wg.Add(1)
		go wait.Until(func() {
			controllerutil.RunWorker(ctx, c.analysisRunWorkQueue, logutil.AnalysisRunKey, c.syncHandler, c.metricsServer)
			log.Debug("Analysis worker has stopped")
			wg.Done()
		}, time.Second, ctx.Done())
	}
	log.Infof("Started %d analysis workers", threadiness)
	<-ctx.Done()
	wg.Wait()
	log.Info("All analysis workers have stopped")

	return nil
}

func (c *Controller) syncHandler(ctx context.Context, key string) error {
	startTime := timeutil.Now()
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	log.WithField(logutil.AnalysisRunKey, name).WithField(logutil.NamespaceKey, namespace).Infof("Started syncing Analysis at (%v)", startTime)
	run, err := c.analysisRunLister.AnalysisRuns(namespace).Get(name)
	if k8serrors.IsNotFound(err) {
		log.WithField(logutil.AnalysisRunKey, name).WithField(logutil.NamespaceKey, namespace).Info("Analysis has been deleted")
		return nil
	}
	if err != nil {
		return err
	}

	defer func() {
		duration := time.Since(startTime)
		c.metricsServer.IncAnalysisRunReconcile(run, duration)
		logCtx := logutil.WithAnalysisRun(run).WithField("time_ms", duration.Seconds()*1e3)
		logCtx.Info("Reconciliation completed")
	}()

	if run.DeletionTimestamp != nil {
		logutil.WithAnalysisRun(run).Info("No reconciliation as analysis marked for deletion")
		return nil
	}

	newRun := c.reconcileAnalysisRun(run)
	return c.persistAnalysisRunStatus(run, newRun.Status)
}

func (c *Controller) jobParentReference(obj any) (*v1.OwnerReference, string) {
	job, ok := obj.(*batchv1.Job)
	if !ok {
		return nil, ""
	}
	// if it has owner reference, return it as is
	ownerRef := v1.GetControllerOf(job)
	// else if it's missing owner reference check if analysis run uid is set and
	// if it is there use labels/annotations to create owner reference
	if ownerRef == nil && job.Labels[jobProvider.AnalysisRunUIDLabelKey] != "" {
		ownerRef = &v1.OwnerReference{
			APIVersion:         analysisRunGVK.GroupVersion().String(),
			Kind:               analysisRunGVK.Kind,
			Name:               job.Annotations[jobProvider.AnalysisRunNameAnnotationKey],
			UID:                types.UID(job.Labels[jobProvider.AnalysisRunUIDLabelKey]),
			BlockOwnerDeletion: ptr.Bool(true),
			Controller:         ptr.Bool(true),
		}
	}
	ns := job.GetNamespace()
	if job.Annotations != nil {
		if job.Annotations[jobProvider.AnalysisRunNamespaceAnnotationKey] != "" {
			ns = job.Annotations[jobProvider.AnalysisRunNamespaceAnnotationKey]
		}
	}
	return ownerRef, ns
}

func (c *Controller) enqueueJobIfCompleted(obj any) {
	job, ok := obj.(*batchv1.Job)
	if !ok {
		return
	}
	for _, condition := range job.Status.Conditions {
		switch condition.Type {
		case batchv1.JobFailed, batchv1.JobComplete:
			controllerutil.EnqueueParentObject(job, register.AnalysisRunKind, c.enqueueAnalysis, c.jobParentReference)
			return
		}
	}
}
