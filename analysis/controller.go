package analysis

import (
	"time"

	log "github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	listers "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/providers"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

// AnalysisController is the controller implementation for Analysis resources
type AnalysisController struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// analysisclientset is a clientset for our own API group
	argoProjClientset clientset.Interface

	analysisRunLister listers.AnalysisRunLister

	analysisRunSynced cache.InformerSynced

	metricsServer *metrics.MetricsServer

	newProvider func(logCtx log.Entry, metric v1alpha1.Metric) (providers.Provider, error)

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

// NewAnalysisController returns a new analysis controller
func NewAnalysisController(
	kubeclientset kubernetes.Interface,
	argoProjClientset clientset.Interface,
	analysisRunInformer informers.AnalysisRunInformer,
	resyncPeriod time.Duration,
	analysisRunWorkQueue workqueue.RateLimitingInterface,
	metricsServer *metrics.MetricsServer,
	recorder record.EventRecorder) *AnalysisController {

	controller := &AnalysisController{
		kubeclientset:        kubeclientset,
		argoProjClientset:    argoProjClientset,
		analysisRunLister:    analysisRunInformer.Lister(),
		metricsServer:        metricsServer,
		analysisRunWorkQueue: analysisRunWorkQueue,
		analysisRunSynced:    analysisRunInformer.Informer().HasSynced,
		recorder:             recorder,
		resyncPeriod:         resyncPeriod,
	}

	controller.enqueueAnalysis = func(obj interface{}) {
		controllerutil.Enqueue(obj, analysisRunWorkQueue)
	}
	controller.enqueueAnalysisAfter = func(obj interface{}, duration time.Duration) {
		controllerutil.EnqueueAfter(obj, duration, analysisRunWorkQueue)
	}

	controller.newProvider = providers.NewProvider

	log.Info("Setting up analysis event handlers")
	// Set up an event handler for when analysis resources change
	analysisRunInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueAnalysis,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueAnalysis(new)
		},
		DeleteFunc: controller.enqueueAnalysis,
	})
	return controller
}

func (c *AnalysisController) Run(threadiness int, stopCh <-chan struct{}) error {
	log.Info("Starting analysis workers")
	for i := 0; i < threadiness; i++ {
		go wait.Until(func() {
			controllerutil.RunWorker(c.analysisRunWorkQueue, logutil.AnalysisRunKey, c.syncHandler, c.metricsServer)
		}, time.Second, stopCh)
	}
	log.Infof("Started %d analysis workers", threadiness)
	<-stopCh
	log.Info("Shutting down analysis workers")

	return nil
}

func (c *AnalysisController) syncHandler(key string) error {
	startTime := time.Now()
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
		//TODO(jessesuen) Add metrics for analysis
		//arc.metricsServer.IncReconcile(r, duration)
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
