package analysis

import (
	"context"
	"sync"
	"time"

	"github.com/argoproj/argo-rollouts/metric"

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
	enqueueAnalysis      func(obj interface{})
	enqueueAnalysisAfter func(obj interface{}, duration time.Duration)

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

	controller.enqueueAnalysis = func(obj interface{}) {
		controllerutil.Enqueue(obj, cfg.AnalysisRunWorkQueue)
	}
	controller.enqueueAnalysisAfter = func(obj interface{}, duration time.Duration) {
		controllerutil.EnqueueAfter(obj, duration, cfg.AnalysisRunWorkQueue)
	}

	providerFactory := metricproviders.ProviderFactory{
		KubeClient: controller.kubeclientset,
		JobLister:  cfg.JobInformer.Lister(),
	}
	controller.newProvider = providerFactory.NewProvider

	cfg.JobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
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

	log.Info("Setting up analysis event handlers")
	// Set up an event handler for when analysis resources change
	cfg.AnalysisRunInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueAnalysis,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueAnalysis(new)
		},
		DeleteFunc: func(obj interface{}) {
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

func (c *Controller) enqueueIfCompleted(obj interface{}) {
	job, ok := obj.(*batchv1.Job)
	if !ok {
		return
	}
	for _, condition := range job.Status.Conditions {
		switch condition.Type {
		case batchv1.JobFailed, batchv1.JobComplete:
			controllerutil.EnqueueParentObject(job, register.AnalysisRunKind, c.enqueueAnalysis)
			return
		}
	}
}
