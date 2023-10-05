package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/argoproj/argo-rollouts/utils/plugin"

	istioutil "github.com/argoproj/argo-rollouts/utils/istio"

	rolloutsConfig "github.com/argoproj/argo-rollouts/utils/config"
	goPlugin "github.com/hashicorp/go-plugin"

	"k8s.io/apimachinery/pkg/util/wait"

	"k8s.io/client-go/dynamic/dynamicinformer"
	kubeinformers "k8s.io/client-go/informers"

	notificationapi "github.com/argoproj/notifications-engine/pkg/api"
	notificationcontroller "github.com/argoproj/notifications-engine/pkg/controller"

	"github.com/pkg/errors"
	smiclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/uuid"
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
	wg                      *sync.WaitGroup
	metricsServer           *metrics.MetricsServer
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

	dynamicInformerFactory               dynamicinformer.DynamicSharedInformerFactory
	clusterDynamicInformerFactory        dynamicinformer.DynamicSharedInformerFactory
	istioDynamicInformerFactory          dynamicinformer.DynamicSharedInformerFactory
	namespaced                           bool
	kubeInformerFactory                  kubeinformers.SharedInformerFactory
	notificationConfigMapInformerFactory kubeinformers.SharedInformerFactory
	notificationSecretInformerFactory    kubeinformers.SharedInformerFactory
	jobInformerFactory                   kubeinformers.SharedInformerFactory
	istioPrimaryDynamicClient            dynamic.Interface
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
	notificationConfigMapInformerFactory kubeinformers.SharedInformerFactory,
	notificationSecretInformerFactory kubeinformers.SharedInformerFactory,
	resyncPeriod time.Duration,
	instanceID string,
	metricsPort int,
	healthzPort int,
	k8sRequestProvider *metrics.K8sRequestsCountProvider,
	nginxIngressClasses []string,
	albIngressClasses []string,
	dynamicInformerFactory dynamicinformer.DynamicSharedInformerFactory,
	clusterDynamicInformerFactory dynamicinformer.DynamicSharedInformerFactory,
	istioDynamicInformerFactory dynamicinformer.DynamicSharedInformerFactory,
	namespaced bool,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	jobInformerFactory kubeinformers.SharedInformerFactory,
) *Manager {
	runtime.Must(rolloutscheme.AddToScheme(scheme.Scheme))
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
	})

	healthzServer := NewHealthzServer(fmt.Sprintf(listenAddr, healthzPort))
	rolloutWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
	experimentWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Experiments")
	analysisRunWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "AnalysisRuns")
	serviceWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Services")
	ingressWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Ingresses")

	refResolver := rollout.NewInformerBasedWorkloadRefResolver(namespace, dynamicclientset, discoveryClient, argoprojclientset, rolloutsInformer.Informer())
	apiFactory := notificationapi.NewFactory(record.NewAPIFactorySettings(), defaults.Namespace(), notificationSecretInformerFactory.Core().V1().Secrets().Informer(), notificationConfigMapInformerFactory.Core().V1().ConfigMaps().Informer())
	recorder := record.NewEventRecorder(kubeclientset, metrics.MetricRolloutEventsTotal, metrics.MetricNotificationFailedTotal, metrics.MetricNotificationSuccessTotal, metrics.MetricNotificationSend, apiFactory)
	notificationsController := notificationcontroller.NewControllerWithNamespaceSupport(dynamicclientset.Resource(v1alpha1.RolloutGVR), rolloutsInformer.Informer(), apiFactory,
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
		wg:                                   &sync.WaitGroup{},
		metricsServer:                        metricsServer,
		healthzServer:                        healthzServer,
		rolloutSynced:                        rolloutsInformer.Informer().HasSynced,
		serviceSynced:                        servicesInformer.Informer().HasSynced,
		ingressSynced:                        ingressWrap.HasSynced,
		jobSynced:                            jobInformer.Informer().HasSynced,
		experimentSynced:                     experimentsInformer.Informer().HasSynced,
		analysisRunSynced:                    analysisRunInformer.Informer().HasSynced,
		analysisTemplateSynced:               analysisTemplateInformer.Informer().HasSynced,
		clusterAnalysisTemplateSynced:        clusterAnalysisTemplateInformer.Informer().HasSynced,
		replicasSetSynced:                    replicaSetInformer.Informer().HasSynced,
		configMapSynced:                      notificationConfigMapInformerFactory.Core().V1().ConfigMaps().Informer().HasSynced,
		secretSynced:                         notificationSecretInformerFactory.Core().V1().Secrets().Informer().HasSynced,
		rolloutWorkqueue:                     rolloutWorkqueue,
		experimentWorkqueue:                  experimentWorkqueue,
		analysisRunWorkqueue:                 analysisRunWorkqueue,
		serviceWorkqueue:                     serviceWorkqueue,
		ingressWorkqueue:                     ingressWorkqueue,
		rolloutController:                    rolloutController,
		serviceController:                    serviceController,
		ingressController:                    ingressController,
		experimentController:                 experimentController,
		analysisController:                   analysisController,
		notificationsController:              notificationsController,
		refResolver:                          refResolver,
		namespace:                            namespace,
		kubeClientSet:                        kubeclientset,
		dynamicInformerFactory:               dynamicInformerFactory,
		clusterDynamicInformerFactory:        clusterDynamicInformerFactory,
		istioDynamicInformerFactory:          istioDynamicInformerFactory,
		namespaced:                           namespaced,
		kubeInformerFactory:                  kubeInformerFactory,
		jobInformerFactory:                   jobInformerFactory,
		istioPrimaryDynamicClient:            istioPrimaryDynamicClient,
		notificationConfigMapInformerFactory: notificationConfigMapInformerFactory,
		notificationSecretInformerFactory:    notificationSecretInformerFactory,
	}

	_, err := rolloutsConfig.InitializeConfig(kubeclientset, defaults.DefaultRolloutsConfigMapName)
	if err != nil {
		log.Fatalf("Failed to init config: %v", err)
	}

	err = plugin.DownloadPlugins(plugin.FileDownloaderImpl{})
	if err != nil {
		log.Fatalf("Failed to download plugins: %v", err)
	}

	return cm
}

// Run will sync informer caches and start controllers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// controllers to finish processing their current work items.
func (c *Manager) Run(ctx context.Context, rolloutThreadiness, serviceThreadiness, ingressThreadiness, experimentThreadiness, analysisThreadiness int, electOpts *LeaderElectionOptions) error {
	defer runtime.HandleCrash()
	defer func() {
		log.Infof("Exiting Main Run function")
	}()

	go func() {
		log.Infof("Starting Healthz Server at %s", c.healthzServer.Addr)
		err := c.healthzServer.ListenAndServe()
		if err != nil {
			err = errors.Wrap(err, "Healthz Server Error")
			log.Error(err)
		}
	}()

	go func() {
		log.Infof("Starting Metric Server at %s", c.metricsServer.Addr)
		if err := c.metricsServer.ListenAndServe(); err != nil {
			log.Error(errors.Wrap(err, "Metric Server Error"))
		}
	}()

	if !electOpts.LeaderElect {
		log.Info("Leader election is turned off. Running in single-instance mode")
		go c.startLeading(ctx, rolloutThreadiness, serviceThreadiness, ingressThreadiness, experimentThreadiness, analysisThreadiness)
		<-ctx.Done()
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
		leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
			Lock: &resourcelock.LeaseLock{
				LeaseMeta: metav1.ObjectMeta{Name: defaultLeaderElectionLeaseLockName, Namespace: electOpts.LeaderElectionNamespace}, Client: c.kubeClientSet.CoordinationV1(),
				LockConfig: resourcelock.ResourceLockConfig{Identity: id},
			},
			ReleaseOnCancel: false, // We can not set this to true because our context is sent on sig which means our code
			// is still running prior to calling cancel. We would need to shut down and then call cancel in order to set this to true.
			LeaseDuration: electOpts.LeaderElectionLeaseDuration,
			RenewDeadline: electOpts.LeaderElectionRenewDeadline,
			RetryPeriod:   electOpts.LeaderElectionRetryPeriod,
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: func(ctx context.Context) {
					log.Infof("I am the new leader: %s", id)
					c.startLeading(ctx, rolloutThreadiness, serviceThreadiness, ingressThreadiness, experimentThreadiness, analysisThreadiness)
				},
				OnStoppedLeading: func() {
					log.Infof("OnStoppedLeading called, shutting down: %s, context err: %s", id, ctx.Err())
				},
				OnNewLeader: func(identity string) {
					log.Infof("New leader elected: %s", identity)
				},
			},
		})
	}
	log.Info("Shutting down workers")
	goPlugin.CleanupClients()

	c.serviceWorkqueue.ShutDownWithDrain()
	c.ingressWorkqueue.ShutDownWithDrain()
	c.rolloutWorkqueue.ShutDownWithDrain()
	c.experimentWorkqueue.ShutDownWithDrain()
	c.analysisRunWorkqueue.ShutDownWithDrain()
	c.analysisRunWorkqueue.ShutDownWithDrain()

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Second) // give max of 10 seconds for http servers to shut down
	defer cancel()
	c.healthzServer.Shutdown(ctxWithTimeout)
	c.metricsServer.Shutdown(ctxWithTimeout)

	c.wg.Wait()

	return nil
}

func (c *Manager) startLeading(ctx context.Context, rolloutThreadiness, serviceThreadiness, ingressThreadiness, experimentThreadiness, analysisThreadiness int) {
	defer runtime.HandleCrash()
	// Start the informer factories to begin populating the informer caches
	log.Info("Starting Controllers")

	c.notificationConfigMapInformerFactory.Start(ctx.Done())
	c.notificationSecretInformerFactory.Start(ctx.Done())
	if ok := cache.WaitForCacheSync(ctx.Done(), c.configMapSynced, c.secretSynced); !ok {
		log.Fatalf("failed to wait for configmap/secret caches to sync, exiting")
	}

	// notice that there is no need to run Start methods in a separate goroutine. (i.e. go kubeInformerFactory.Start(stopCh)
	// Start method is non-blocking and runs all registered informers in a dedicated goroutine.
	c.dynamicInformerFactory.Start(ctx.Done())
	if !c.namespaced {
		c.clusterDynamicInformerFactory.Start(ctx.Done())
	}
	c.kubeInformerFactory.Start(ctx.Done())

	c.jobInformerFactory.Start(ctx.Done())

	// Check if Istio installed on cluster before starting dynamicInformerFactory
	if istioutil.DoesIstioExist(c.istioPrimaryDynamicClient, c.namespace) {
		c.istioDynamicInformerFactory.Start(ctx.Done())
	}

	// Wait for the caches to be synced before starting workers
	log.Info("Waiting for controller's informer caches to sync")
	if ok := cache.WaitForCacheSync(ctx.Done(), c.serviceSynced, c.ingressSynced, c.jobSynced, c.rolloutSynced, c.experimentSynced, c.analysisRunSynced, c.analysisTemplateSynced, c.replicasSetSynced, c.configMapSynced, c.secretSynced); !ok {
		log.Fatalf("failed to wait for caches to sync, exiting")
	}
	// only wait for cluster scoped informers to sync if we are running in cluster-wide mode
	if c.namespace == metav1.NamespaceAll {
		if ok := cache.WaitForCacheSync(ctx.Done(), c.clusterAnalysisTemplateSynced); !ok {
			log.Fatalf("failed to wait for cluster-scoped caches to sync, exiting")
		}
	}

	go wait.Until(func() { c.wg.Add(1); c.rolloutController.Run(ctx, rolloutThreadiness); c.wg.Done() }, time.Second, ctx.Done())
	go wait.Until(func() { c.wg.Add(1); c.serviceController.Run(ctx, serviceThreadiness); c.wg.Done() }, time.Second, ctx.Done())
	go wait.Until(func() { c.wg.Add(1); c.ingressController.Run(ctx, ingressThreadiness); c.wg.Done() }, time.Second, ctx.Done())
	go wait.Until(func() { c.wg.Add(1); c.experimentController.Run(ctx, experimentThreadiness); c.wg.Done() }, time.Second, ctx.Done())
	go wait.Until(func() { c.wg.Add(1); c.analysisController.Run(ctx, analysisThreadiness); c.wg.Done() }, time.Second, ctx.Done())
	go wait.Until(func() { c.wg.Add(1); c.notificationsController.Run(rolloutThreadiness, ctx.Done()); c.wg.Done() }, time.Second, ctx.Done())

	log.Info("Started controller")
}
