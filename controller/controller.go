package controller

import (
	"fmt"
	"time"

	"github.com/pkg/errors"
	smiclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	batchinformers "k8s.io/client-go/informers/batch/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	extensionsinformers "k8s.io/client-go/informers/extensions/v1beta1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"github.com/argoproj/argo-rollouts/analysis"
	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/experiments"
	"github.com/argoproj/argo-rollouts/ingress"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	rolloutscheme "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/scheme"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout"
	"github.com/argoproj/argo-rollouts/service"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
)

const controllerAgentName = "rollouts-controller"

const (
	// DefaultRolloutResyncPeriod is the default time in seconds for rollout resync period
	DefaultRolloutResyncPeriod = 15 * 60

	// DefaultMetricsPort is the default port to expose the metrics endpoint
	DefaultMetricsPort = 8090

	// DefaultRolloutThreads is the default number of rollout worker threads to start with the controller
	DefaultRolloutThreads = 10

	// DefaultExperimentThreads is the default number of experiment worker threads to start with the controller
	DefaultExperimentThreads = 10

	// DefaultAnalysisThreads is the default number of analysis worker threads to start with the controller
	DefaultAnalysisThreads = 30

	// DefaultServiceThreads is the default number of service worker threads to start with the controller
	DefaultServiceThreads = 10

	// DefaultIngressThreads is the default number of ingress worker threads to start with the controller
	DefaultIngressThreads = 10
)

// Manager is the controller implementation for Argo-Rollout resources
type Manager struct {
	metricsServer        *metrics.MetricsServer
	rolloutController    *rollout.Controller
	experimentController *experiments.Controller
	analysisController   *analysis.Controller
	serviceController    *service.Controller
	ingressController    *ingress.Controller

	rolloutSynced                 cache.InformerSynced
	experimentSynced              cache.InformerSynced
	analysisRunSynced             cache.InformerSynced
	analysisTemplateSynced        cache.InformerSynced
	clusterAnalysisTemplateSynced cache.InformerSynced
	serviceSynced                 cache.InformerSynced
	ingressSynced                 cache.InformerSynced
	jobSynced                     cache.InformerSynced
	replicasSetSynced             cache.InformerSynced
	istioVirtualServiceSynced     cache.InformerSynced

	rolloutWorkqueue     workqueue.RateLimitingInterface
	serviceWorkqueue     workqueue.RateLimitingInterface
	ingressWorkqueue     workqueue.RateLimitingInterface
	experimentWorkqueue  workqueue.RateLimitingInterface
	analysisRunWorkqueue workqueue.RateLimitingInterface

	defaultIstioVersion        string
	defaultTrafficSplitVersion string

	dynamicClientSet dynamic.Interface

	namespace string
}

// NewManager returns a new manager to manage all the controllers
func NewManager(
	namespace string,
	kubeclientset kubernetes.Interface,
	argoprojclientset clientset.Interface,
	dynamicclientset dynamic.Interface,
	smiclientset smiclientset.Interface,
	replicaSetInformer appsinformers.ReplicaSetInformer,
	servicesInformer coreinformers.ServiceInformer,
	ingressesInformer extensionsinformers.IngressInformer,
	jobInformer batchinformers.JobInformer,
	rolloutsInformer informers.RolloutInformer,
	experimentsInformer informers.ExperimentInformer,
	analysisRunInformer informers.AnalysisRunInformer,
	analysisTemplateInformer informers.AnalysisTemplateInformer,
	clusterAnalysisTemplateInformer informers.ClusterAnalysisTemplateInformer,
	istioVirtualServiceInformer cache.SharedIndexInformer,
	resyncPeriod time.Duration,
	instanceID string,
	metricsPort int,
	k8sRequestProvider *metrics.K8sRequestsCountProvider,
	defaultIstioVersion string,
	defaultTrafficSplitVersion string,
	nginxIngressClasses []string,
	albIngressClasses []string,
) *Manager {

	utilruntime.Must(rolloutscheme.AddToScheme(scheme.Scheme))
	log.Info("Creating event broadcaster")

	// Create event broadcaster
	// Add argo-rollouts custom resources to the default Kubernetes Scheme so Events can be
	// logged for argo-rollouts types.
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(log.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})
	metricsAddr := fmt.Sprintf("0.0.0.0:%d", metricsPort)
	metricsServer := metrics.NewMetricsServer(metrics.ServerConfig{
		Addr:               metricsAddr,
		RolloutLister:      rolloutsInformer.Lister(),
		AnalysisRunLister:  analysisRunInformer.Lister(),
		ExperimentLister:   experimentsInformer.Lister(),
		K8SRequestProvider: k8sRequestProvider,
	})

	rolloutWorkqueue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Rollouts")
	experimentWorkqueue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Experiments")
	analysisRunWorkqueue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AnalysisRuns")
	serviceWorkqueue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Services")
	ingressWorkqueue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Ingresses")

	rolloutController := rollout.NewController(rollout.ControllerConfig{
		Namespace:                       namespace,
		KubeClientSet:                   kubeclientset,
		ArgoProjClientset:               argoprojclientset,
		DynamicClientSet:                dynamicclientset,
		SmiClientSet:                    smiclientset,
		ExperimentInformer:              experimentsInformer,
		AnalysisRunInformer:             analysisRunInformer,
		AnalysisTemplateInformer:        analysisTemplateInformer,
		ClusterAnalysisTemplateInformer: clusterAnalysisTemplateInformer,
		IstioVirtualServiceInformer:     istioVirtualServiceInformer,
		ReplicaSetInformer:              replicaSetInformer,
		ServicesInformer:                servicesInformer,
		IngressInformer:                 ingressesInformer,
		RolloutsInformer:                rolloutsInformer,
		ResyncPeriod:                    resyncPeriod,
		RolloutWorkQueue:                rolloutWorkqueue,
		ServiceWorkQueue:                serviceWorkqueue,
		IngressWorkQueue:                ingressWorkqueue,
		MetricsServer:                   metricsServer,
		Recorder:                        recorder,
		DefaultIstioVersion:             defaultIstioVersion,
		DefaultTrafficSplitVersion:      defaultTrafficSplitVersion,
	})

	experimentController := experiments.NewController(experiments.ControllerConfig{
		KubeClientSet:                   kubeclientset,
		ArgoProjClientset:               argoprojclientset,
		ReplicaSetInformer:              replicaSetInformer,
		ExperimentsInformer:             experimentsInformer,
		AnalysisRunInformer:             analysisRunInformer,
		AnalysisTemplateInformer:        analysisTemplateInformer,
		ClusterAnalysisTemplateInformer: clusterAnalysisTemplateInformer,
		ResyncPeriod:                    resyncPeriod,
		RolloutWorkQueue:                rolloutWorkqueue,
		ExperimentWorkQueue:             experimentWorkqueue,
		MetricsServer:                   metricsServer,
		Recorder:                        recorder,
	})

	analysisController := analysis.NewController(analysis.ControllerConfig{
		KubeClientSet:        kubeclientset,
		ArgoProjClientset:    argoprojclientset,
		AnalysisRunInformer:  analysisRunInformer,
		JobInformer:          jobInformer,
		ResyncPeriod:         resyncPeriod,
		AnalysisRunWorkQueue: analysisRunWorkqueue,
		MetricsServer:        metricsServer,
		Recorder:             recorder,
	})

	serviceController := service.NewController(service.ControllerConfig{
		Kubeclientset:     kubeclientset,
		Argoprojclientset: argoprojclientset,
		RolloutsInformer:  rolloutsInformer,
		ServicesInformer:  servicesInformer,
		RolloutWorkqueue:  rolloutWorkqueue,
		ServiceWorkqueue:  serviceWorkqueue,
		ResyncPeriod:      resyncPeriod,
		MetricsServer:     metricsServer,
	})

	ingressController := ingress.NewController(ingress.ControllerConfig{
		Client:           kubeclientset,
		IngressInformer:  ingressesInformer,
		IngressWorkQueue: ingressWorkqueue,

		RolloutsInformer: rolloutsInformer,
		RolloutWorkQueue: rolloutWorkqueue,

		MetricsServer: metricsServer,

		ALBClasses:   albIngressClasses,
		NGINXClasses: nginxIngressClasses,
	})

	cm := &Manager{
		metricsServer:                 metricsServer,
		rolloutSynced:                 rolloutsInformer.Informer().HasSynced,
		serviceSynced:                 servicesInformer.Informer().HasSynced,
		ingressSynced:                 ingressesInformer.Informer().HasSynced,
		jobSynced:                     jobInformer.Informer().HasSynced,
		experimentSynced:              experimentsInformer.Informer().HasSynced,
		analysisRunSynced:             analysisRunInformer.Informer().HasSynced,
		analysisTemplateSynced:        analysisTemplateInformer.Informer().HasSynced,
		clusterAnalysisTemplateSynced: clusterAnalysisTemplateInformer.Informer().HasSynced,
		replicasSetSynced:             replicaSetInformer.Informer().HasSynced,
		istioVirtualServiceSynced:     istioVirtualServiceInformer.HasSynced,
		rolloutWorkqueue:              rolloutWorkqueue,
		experimentWorkqueue:           experimentWorkqueue,
		analysisRunWorkqueue:          analysisRunWorkqueue,
		serviceWorkqueue:              serviceWorkqueue,
		ingressWorkqueue:              ingressWorkqueue,
		rolloutController:             rolloutController,
		serviceController:             serviceController,
		ingressController:             ingressController,
		experimentController:          experimentController,
		analysisController:            analysisController,
		defaultIstioVersion:           defaultIstioVersion,
		defaultTrafficSplitVersion:    defaultTrafficSplitVersion,
		dynamicClientSet:              dynamicclientset,
		namespace:                     namespace,
	}

	return cm
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Manager) Run(rolloutThreadiness, serviceThreadiness, ingressThreadiness, experimentThreadiness, analysisThreadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.serviceWorkqueue.ShutDown()
	defer c.ingressWorkqueue.ShutDown()
	defer c.rolloutWorkqueue.ShutDown()
	defer c.experimentWorkqueue.ShutDown()
	defer c.analysisRunWorkqueue.ShutDown()
	// Wait for the caches to be synced before starting workers
	log.Info("Waiting for controller's informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.serviceSynced, c.ingressSynced, c.jobSynced, c.rolloutSynced, c.experimentSynced, c.analysisRunSynced, c.analysisTemplateSynced, c.replicasSetSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}
	// only wait for cluster scoped informers to sync if we are running in cluster-wide mode
	if c.namespace == metav1.NamespaceAll {
		if ok := cache.WaitForCacheSync(stopCh, c.clusterAnalysisTemplateSynced); !ok {
			return fmt.Errorf("failed to wait for cluster-scoped caches to sync")
		}
	}
	// Check if Istio exists
	if istioutil.DoesIstioExist(c.dynamicClientSet, c.namespace, c.defaultIstioVersion) {
		// Wait for Istio cache to sync before starting workers
		if ok := cache.WaitForCacheSync(stopCh, c.istioVirtualServiceSynced); !ok {
			return fmt.Errorf("failed to wait for istio virtualService cache to sync")
		}
	}

	// Start the informer factories to begin populating the informer caches
	log.Info("Starting Controllers")
	go wait.Until(func() { c.rolloutController.Run(rolloutThreadiness, stopCh) }, time.Second, stopCh)
	go wait.Until(func() { c.serviceController.Run(serviceThreadiness, stopCh) }, time.Second, stopCh)
	go wait.Until(func() { c.ingressController.Run(ingressThreadiness, stopCh) }, time.Second, stopCh)
	go wait.Until(func() { c.experimentController.Run(experimentThreadiness, stopCh) }, time.Second, stopCh)
	go wait.Until(func() { c.analysisController.Run(analysisThreadiness, stopCh) }, time.Second, stopCh)
	log.Info("Started controller")

	go func() {
		log.Infof("Starting Metric Server at %s", c.metricsServer.Addr)
		err := c.metricsServer.ListenAndServe()
		if err != nil {
			err = errors.Wrap(err, "Starting Metric Server")
			log.Fatal(err)
		}
	}()
	<-stopCh
	log.Info("Shutting down workers")

	return nil
}
