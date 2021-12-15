package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	notificationapi "github.com/argoproj/notifications-engine/pkg/api"
	notificationcontroller "github.com/argoproj/notifications-engine/pkg/controller"

	"github.com/pkg/errors"
	smiclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	batchinformers "k8s.io/client-go/informers/batch/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/workqueue"

	"github.com/argoproj/argo-rollouts/analysis"
	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/experiments"
	"github.com/argoproj/argo-rollouts/ingress"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	rolloutscheme "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/scheme"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout"
	"github.com/argoproj/argo-rollouts/service"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	"github.com/argoproj/argo-rollouts/utils/queue"
	"github.com/argoproj/argo-rollouts/utils/record"
)

const (
	// DefaultRolloutResyncPeriod is the default time in seconds for rollout resync period
	DefaultRolloutResyncPeriod = 15 * 60

	// DefaultHealthzPort is the default port to check controller's health
	DefaultHealthzPort = 8080

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

	// DefaultLeaderElect is the default true leader election should be enabled
	DefaultLeaderElect = true

	// DefaultLeaderElectionLeaseDuration is the default time in seconds that non-leader candidates will wait to force acquire leadership
	DefaultLeaderElectionLeaseDuration = 15 * time.Second

	// DefaultLeaderElectionRenewDeadline is the default time in seconds that the acting master will retry refreshing leadership before giving up
	DefaultLeaderElectionRenewDeadline = 10 * time.Second

	// DefaultLeaderElectionRetryPeriod is the default time in seconds that the leader election clients should wait between tries of actions
	DefaultLeaderElectionRetryPeriod = 2 * time.Second

	defaultLeaderElectionLeaseLockName = "argo-rollouts-controller-lock"
	listenAddr                         = "0.0.0.0:%d"
)

type LeaderElectionOptions struct {
	LeaderElect                 bool
	LeaderElectionNamespace     string
	LeaderElectionLeaseDuration time.Duration
	LeaderElectionRenewDeadline time.Duration
	LeaderElectionRetryPeriod   time.Duration
}

func NewLeaderElectionOptions() *LeaderElectionOptions {
	return &LeaderElectionOptions{
		LeaderElect:                 DefaultLeaderElect,
		LeaderElectionNamespace:     defaults.Namespace(),
		LeaderElectionLeaseDuration: DefaultLeaderElectionLeaseDuration,
		LeaderElectionRenewDeadline: DefaultLeaderElectionRenewDeadline,
		LeaderElectionRetryPeriod:   DefaultLeaderElectionRetryPeriod,
	}
}

// Manager is the controller implementation for Argo-Rollout resources
type Manager struct {
	metricsServer           *metrics.MetricsServer
	secondaryMetricsServer  *metrics.MetricsServer
	healthzServer           *http.Server
	rolloutController       *rollout.Controller
	experimentController    *experiments.Controller
	analysisController      *analysis.Controller
	serviceController       *service.Controller
	ingressController       *ingress.Controller
	notificationsController notificationcontroller.NotificationController

	rolloutSynced                 cache.InformerSynced
	experimentSynced              cache.InformerSynced
	analysisRunSynced             cache.InformerSynced
	analysisTemplateSynced        cache.InformerSynced
	clusterAnalysisTemplateSynced cache.InformerSynced
	serviceSynced                 cache.InformerSynced
	ingressSynced                 cache.InformerSynced
	jobSynced                     cache.InformerSynced
	replicasSetSynced             cache.InformerSynced
	configMapSynced               cache.InformerSynced
	secretSynced                  cache.InformerSynced

	rolloutWorkqueue     workqueue.RateLimitingInterface
	serviceWorkqueue     workqueue.RateLimitingInterface
	ingressWorkqueue     workqueue.RateLimitingInterface
	experimentWorkqueue  workqueue.RateLimitingInterface
	analysisRunWorkqueue workqueue.RateLimitingInterface

	refResolver rollout.TemplateRefResolver

	kubeClientSet kubernetes.Interface

	namespace string
}

// NewManager returns a new manager to manage all the controllers
func NewManager(
	namespace string,
	kubeclientset kubernetes.Interface,
	argoprojclientset clientset.Interface,
	dynamicclientset dynamic.Interface,
	smiclientset smiclientset.Interface,
	discoveryClient discovery.DiscoveryInterface,
	replicaSetInformer appsinformers.ReplicaSetInformer,
	servicesInformer coreinformers.ServiceInformer,
	ingressWrap *ingressutil.IngressWrap,
	jobInformer batchinformers.JobInformer,
	rolloutsInformer informers.RolloutInformer,
	experimentsInformer informers.ExperimentInformer,
	analysisRunInformer informers.AnalysisRunInformer,
	analysisTemplateInformer informers.AnalysisTemplateInformer,
	clusterAnalysisTemplateInformer informers.ClusterAnalysisTemplateInformer,
	istioPrimaryDynamicClient dynamic.Interface,
	istioVirtualServiceInformer cache.SharedIndexInformer,
	istioDestinationRuleInformer cache.SharedIndexInformer,
	configMapInformer coreinformers.ConfigMapInformer,
	secretInformer coreinformers.SecretInformer,
	resyncPeriod time.Duration,
	instanceID string,
	metricsPort int,
	healthzPort int,
	k8sRequestProvider *metrics.K8sRequestsCountProvider,
	nginxIngressClasses []string,
	albIngressClasses []string,
) *Manager {

	utilruntime.Must(rolloutscheme.AddToScheme(scheme.Scheme))
	log.Info("Creating event broadcaster")

	metricsAddr := fmt.Sprintf(listenAddr, metricsPort)
	metricsServer := metrics.NewMetricsServer(metrics.ServerConfig{
		Addr:                          metricsAddr,
		RolloutLister:                 rolloutsInformer.Lister(),
		AnalysisRunLister:             analysisRunInformer.Lister(),
		AnalysisTemplateLister:        analysisTemplateInformer.Lister(),
		ClusterAnalysisTemplateLister: clusterAnalysisTemplateInformer.Lister(),
		ExperimentLister:              experimentsInformer.Lister(),
		K8SRequestProvider:            k8sRequestProvider,
	}, true)

	healthzServer := NewHealthzServer(fmt.Sprintf(listenAddr, healthzPort))

	rolloutWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
	experimentWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Experiments")
	analysisRunWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "AnalysisRuns")
	serviceWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Services")
	ingressWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Ingresses")

	refResolver := rollout.NewInformerBasedWorkloadRefResolver(namespace, dynamicclientset, discoveryClient, argoprojclientset, rolloutsInformer.Informer())
	apiFactory := notificationapi.NewFactory(record.NewAPIFactorySettings(), defaults.Namespace(), secretInformer.Informer(), configMapInformer.Informer())
	recorder := record.NewEventRecorder(kubeclientset, metrics.MetricRolloutEventsTotal, apiFactory)
	notificationsController := notificationcontroller.NewController(dynamicclientset.Resource(v1alpha1.RolloutGVR), rolloutsInformer.Informer(), apiFactory,
		notificationcontroller.WithToUnstructured(func(obj metav1.Object) (*unstructured.Unstructured, error) {
			data, err := json.Marshal(obj)
			if err != nil {
				return nil, err
			}
			res := &unstructured.Unstructured{}
			err = json.Unmarshal(data, res)
			if err != nil {
				return nil, err
			}
			return res, nil
		}),
	)

	rolloutController := rollout.NewController(rollout.ControllerConfig{
		Namespace:                       namespace,
		KubeClientSet:                   kubeclientset,
		ArgoProjClientset:               argoprojclientset,
		DynamicClientSet:                dynamicclientset,
		RefResolver:                     refResolver,
		SmiClientSet:                    smiclientset,
		ExperimentInformer:              experimentsInformer,
		AnalysisRunInformer:             analysisRunInformer,
		AnalysisTemplateInformer:        analysisTemplateInformer,
		ClusterAnalysisTemplateInformer: clusterAnalysisTemplateInformer,
		IstioPrimaryDynamicClient:       istioPrimaryDynamicClient,
		IstioVirtualServiceInformer:     istioVirtualServiceInformer,
		IstioDestinationRuleInformer:    istioDestinationRuleInformer,
		ReplicaSetInformer:              replicaSetInformer,
		ServicesInformer:                servicesInformer,
		IngressWrapper:                  ingressWrap,
		RolloutsInformer:                rolloutsInformer,
		ResyncPeriod:                    resyncPeriod,
		RolloutWorkQueue:                rolloutWorkqueue,
		ServiceWorkQueue:                serviceWorkqueue,
		IngressWorkQueue:                ingressWorkqueue,
		MetricsServer:                   metricsServer,
		Recorder:                        recorder,
	})

	experimentController := experiments.NewController(experiments.ControllerConfig{
		KubeClientSet:                   kubeclientset,
		ArgoProjClientset:               argoprojclientset,
		ReplicaSetInformer:              replicaSetInformer,
		ExperimentsInformer:             experimentsInformer,
		AnalysisRunInformer:             analysisRunInformer,
		AnalysisTemplateInformer:        analysisTemplateInformer,
		ClusterAnalysisTemplateInformer: clusterAnalysisTemplateInformer,
		ServiceInformer:                 servicesInformer,
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
		IngressWrap:      ingressWrap,
		IngressWorkQueue: ingressWorkqueue,

		RolloutsInformer: rolloutsInformer,
		RolloutWorkQueue: rolloutWorkqueue,

		MetricsServer: metricsServer,

		ALBClasses:   albIngressClasses,
		NGINXClasses: nginxIngressClasses,
	})

	cm := &Manager{
		metricsServer:                 metricsServer,
		healthzServer:                 healthzServer,
		rolloutSynced:                 rolloutsInformer.Informer().HasSynced,
		serviceSynced:                 servicesInformer.Informer().HasSynced,
		ingressSynced:                 ingressWrap.HasSynced,
		jobSynced:                     jobInformer.Informer().HasSynced,
		experimentSynced:              experimentsInformer.Informer().HasSynced,
		analysisRunSynced:             analysisRunInformer.Informer().HasSynced,
		analysisTemplateSynced:        analysisTemplateInformer.Informer().HasSynced,
		clusterAnalysisTemplateSynced: clusterAnalysisTemplateInformer.Informer().HasSynced,
		replicasSetSynced:             replicaSetInformer.Informer().HasSynced,
		configMapSynced:               configMapInformer.Informer().HasSynced,
		secretSynced:                  secretInformer.Informer().HasSynced,
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
		notificationsController:       notificationsController,
		refResolver:                   refResolver,
		namespace:                     namespace,
		kubeClientSet:                 kubeclientset,
	}

	return cm
}

// Run will sync informer caches and start controllers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// controllers to finish processing their current work items.
func (c *Manager) Run(rolloutThreadiness, serviceThreadiness, ingressThreadiness, experimentThreadiness, analysisThreadiness int, electOpts *LeaderElectionOptions, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.serviceWorkqueue.ShutDown()
	defer c.ingressWorkqueue.ShutDown()
	defer c.rolloutWorkqueue.ShutDown()
	defer c.experimentWorkqueue.ShutDown()
	defer c.analysisRunWorkqueue.ShutDown()

	// Wait for the caches to be synced before starting workers
	log.Info("Waiting for controller's informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.serviceSynced, c.ingressSynced, c.jobSynced, c.rolloutSynced, c.experimentSynced, c.analysisRunSynced, c.analysisTemplateSynced, c.replicasSetSynced, c.configMapSynced, c.secretSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}
	// only wait for cluster scoped informers to sync if we are running in cluster-wide mode
	if c.namespace == metav1.NamespaceAll {
		if ok := cache.WaitForCacheSync(stopCh, c.clusterAnalysisTemplateSynced); !ok {
			return fmt.Errorf("failed to wait for cluster-scoped caches to sync")
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if !electOpts.LeaderElect {
		log.Info("Leader election is turned off. Running in single-instance mode")
		go c.startLeading(ctx, rolloutThreadiness, serviceThreadiness, ingressThreadiness, experimentThreadiness, analysisThreadiness)
	} else {
		// id used to distinguish between multiple controller manager instances
		id, err := os.Hostname()
		if err != nil {
			log.Fatalf("Error getting hostname for leader election %v", err)
		}

		if electOpts.LeaderElectionNamespace == "" {
			log.Fatalf("Error LeaderElectionNamespace is empty")
		}

		// add a uniquifier so that two processes on the same host don't accidentally both become active
		id = id + "_" + string(uuid.NewUUID())
		log.Infof("Leaderelection get id %s", id)
		go leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
			Lock: &resourcelock.LeaseLock{
				LeaseMeta: metav1.ObjectMeta{Name: defaultLeaderElectionLeaseLockName, Namespace: electOpts.LeaderElectionNamespace}, Client: c.kubeClientSet.CoordinationV1(),
				LockConfig: resourcelock.ResourceLockConfig{Identity: id},
			},
			ReleaseOnCancel: true,
			LeaseDuration:   electOpts.LeaderElectionLeaseDuration,
			RenewDeadline:   electOpts.LeaderElectionRenewDeadline,
			RetryPeriod:     electOpts.LeaderElectionRetryPeriod,
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: func(ctx context.Context) {
					if c.secondaryMetricsServer != nil {
						log.Warnln("Shutdown Secondary Metrics Server")
						c.secondaryMetricsServer.Shutdown(ctx)
					}
					c.startLeading(ctx, rolloutThreadiness, serviceThreadiness, ingressThreadiness, experimentThreadiness, analysisThreadiness)
				},
				OnStoppedLeading: func() {
					log.Infof("Stopped leading controller: %s", id)
					return
				},
				OnNewLeader: func(identity string) {
					if identity == id {
						return
					}
					log.Infof("New leader elected: %s", identity)

					if c.secondaryMetricsServer != nil {
						log.Warn("Secondary metrics server already started")
						return
					}

					log.Infof("Starting Secondary Metric Server at %s", c.metricsServer.Addr)
					c.secondaryMetricsServer = metrics.NewMetricsServer(metrics.ServerConfig{
						Addr: c.metricsServer.Addr,
					}, false)
					err = c.secondaryMetricsServer.ListenAndServe()
					if err != nil {
						err = errors.Wrap(err, "Starting Secondary Metric Server")
						log.Warn(err)
					}
				},
			},
		})
	}

	go func() {
		log.Infof("Starting Healthz Server at %s", c.healthzServer.Addr)
		err := c.healthzServer.ListenAndServe()
		if err != nil {
			err = errors.Wrap(err, "Starting Healthz Server")
			log.Error(err)
		}
	}()

	<-stopCh
	log.Info("Shutting down workers")

	return nil
}

func (c *Manager) startLeading(ctx context.Context, rolloutThreadiness, serviceThreadiness, ingressThreadiness, experimentThreadiness, analysisThreadiness int) {
	defer runtime.HandleCrash()
	// Start the informer factories to begin populating the informer caches
	log.Info("Starting Controllers")
	go wait.Until(func() { c.rolloutController.Run(rolloutThreadiness, ctx.Done()) }, time.Second, ctx.Done())
	go wait.Until(func() { c.serviceController.Run(serviceThreadiness, ctx.Done()) }, time.Second, ctx.Done())
	go wait.Until(func() { c.ingressController.Run(ingressThreadiness, ctx.Done()) }, time.Second, ctx.Done())
	go wait.Until(func() { c.experimentController.Run(experimentThreadiness, ctx.Done()) }, time.Second, ctx.Done())
	go wait.Until(func() { c.analysisController.Run(analysisThreadiness, ctx.Done()) }, time.Second, ctx.Done())
	go wait.Until(func() { c.notificationsController.Run(rolloutThreadiness, ctx.Done()) }, time.Second, ctx.Done())

	go func() {
		log.Infof("Starting Metric Server at %s", c.metricsServer.Addr)
		if err := c.metricsServer.ListenAndServe(); err != nil {
			log.Error(errors.Wrap(err, "Starting Metric Server"))
		}
	}()

	log.Info("Started controller")
}
