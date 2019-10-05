package job

import (
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	batchinformers "k8s.io/client-go/informers/batch/v1"
	"k8s.io/client-go/kubernetes"
	batchv1 "k8s.io/client-go/listers/batch/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

const (
	AnalysisRunNameKey = "analysisruns.argoproj.io/name"
)

type JobController struct {
	kubeclientset        kubernetes.Interface
	jobLister            batchv1.JobLister
	jobSynced            cache.InformerSynced
	analysisRunWorkqueue workqueue.RateLimitingInterface
	jobWorkqueue         workqueue.RateLimitingInterface
	resyncPeriod         time.Duration
	metricServer         *metrics.MetricsServer
	enqueueAnalysisRun   func(obj interface{})
}

// NewJobController returns a new job controller
func NewJobController(
	kubeclientset kubernetes.Interface,
	jobInformer batchinformers.JobInformer,
	resyncPeriod time.Duration,
	analysisRunWorkqueue workqueue.RateLimitingInterface,
	jobWorkqueue workqueue.RateLimitingInterface,
	metricServer *metrics.MetricsServer,
) *JobController {

	controller := &JobController{
		kubeclientset: kubeclientset,
		//jobInformer:          jobInformer.Informer().GetIndexer(),
		jobSynced:            jobInformer.Informer().HasSynced,
		analysisRunWorkqueue: analysisRunWorkqueue,
		jobWorkqueue:         jobWorkqueue,
		resyncPeriod:         resyncPeriod,
		metricServer:         metricServer,
	}

	jobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controllerutil.Enqueue(obj, jobWorkqueue)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			controllerutil.Enqueue(newObj, jobWorkqueue)
		},
		DeleteFunc: func(obj interface{}) {
			controllerutil.Enqueue(obj, jobWorkqueue)
		},
	})
	controller.enqueueAnalysisRun = func(obj interface{}) {
		controllerutil.EnqueueRateLimited(obj, analysisRunWorkqueue)
	}

	return controller
}

func (c *JobController) Run(threadiness int, stopCh <-chan struct{}) error {
	log.Info("Starting job workers")
	for i := 0; i < threadiness; i++ {
		go wait.Until(func() {
			controllerutil.RunWorker(c.jobWorkqueue, logutil.JobKey, c.syncJob, c.metricServer)
		}, time.Second, stopCh)
	}

	log.Info("Started job workers")
	<-stopCh
	log.Info("Shutting job workers")

	return nil
}

func (c *JobController) syncJob(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	job, err := c.jobLister.Jobs(namespace).Get(name)
	if errors.IsNotFound(err) {
		log.Infof("Job %v has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	// TODO: check if job is completed
	if job.ObjectMeta.Labels != nil {
		if analysisRunName := job.ObjectMeta.Labels[AnalysisRunNameKey]; analysisRunName != "" {
			c.enqueueAnalysisRun(namespace + "/" + analysisRunName)
		}
	}
	return nil
}
