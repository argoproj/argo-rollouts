package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/argoproj/pkg/kubeclientmetrics"
	smiclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/argoproj/argo-rollouts/controller"
	"github.com/argoproj/argo-rollouts/controller/metrics"
	jobprovider "github.com/argoproj/argo-rollouts/metricproviders/job"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-rollouts/pkg/signals"
	"github.com/argoproj/argo-rollouts/rollout"
	"github.com/argoproj/argo-rollouts/rolloutplugin"
	analysishelper "github.com/argoproj/argo-rollouts/rolloutplugin/analysis"
	pluginPackage "github.com/argoproj/argo-rollouts/rolloutplugin/plugin"
	statefulset "github.com/argoproj/argo-rollouts/rolloutplugin/plugins/statefulset"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	"github.com/argoproj/argo-rollouts/utils/record"
	"github.com/argoproj/argo-rollouts/utils/tolerantinformer"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func newCommand() *cobra.Command {
	var (
		clientConfig                   clientcmd.ClientConfig
		rolloutResyncPeriod            int64
		logLevel                       string
		metricsPort                    int
		healthzPort                    int
		rolloutPluginMetricsPort       int
		rolloutPluginHealthzPort       int
		instanceID                     string
		qps                            float32
		burst                          int
		rolloutThreads                 int
		experimentThreads              int
		analysisThreads                int
		serviceThreads                 int
		ingressThreads                 int
		ephemeralMetadataThreads       int
		ephemeralMetadataPodRetries    int
		targetGroupBindingVersion      string
		albTagKeyResourceID            string
		istioVersion                   string
		trafficSplitVersion            string
		traefikAPIGroup                string
		traefikVersion                 string
		ambassadorVersion              string
		ingressVersion                 string
		appmeshCRDVersion              string
		albIngressClasses              []string
		nginxIngressClasses            []string
		awsVerifyTargetGroup           bool
		namespaced                     bool
		selfServiceNotificationEnabled bool
	)

	electOpts := controller.NewLeaderElectionOptions()

	var command = cobra.Command{
		Use:   "argo-rollouts-combined",
		Short: "Combined controller for Argo Rollouts (standard + RolloutPlugin)",
		RunE: func(c *cobra.Command, args []string) error {
			// Set up logging
			setLogLevel(logLevel)

			// Set up signal handler for graceful shutdown
			ctx := signals.SetupSignalHandlerContext()

			// Set defaults for various components
			defaults.SetVerifyTargetGroup(awsVerifyTargetGroup)
			defaults.SetTargetGroupBindingAPIVersion(targetGroupBindingVersion)
			defaults.SetalbTagKeyResourceID(albTagKeyResourceID)
			defaults.SetIstioAPIVersion(istioVersion)
			defaults.SetAmbassadorAPIVersion(ambassadorVersion)
			defaults.SetSMIAPIVersion(trafficSplitVersion)
			defaults.SetAppMeshCRDVersion(appmeshCRDVersion)
			defaults.SetTraefikAPIGroup(traefikAPIGroup)
			defaults.SetTraefikVersion(traefikVersion)

			// Get Kubernetes config
			config, err := clientConfig.ClientConfig()
			if err != nil {
				return fmt.Errorf("failed to get client config: %w", err)
			}
			config.QPS = qps
			config.Burst = burst

			// Determine namespace
			namespace := metav1.NamespaceAll
			configNS, _, err := clientConfig.Namespace()
			if err != nil {
				return fmt.Errorf("failed to get namespace: %w", err)
			}
			if namespaced {
				namespace = configNS
			}

			log.WithFields(log.Fields{
				"namespace":                namespace,
				"instanceID":               instanceID,
				"metricsPort":              metricsPort,
				"healthzPort":              healthzPort,
				"rolloutPluginMetricsPort": rolloutPluginMetricsPort,
				"rolloutPluginHealthzPort": rolloutPluginHealthzPort,
			}).Info("Starting combined Argo Rollouts controller")

			// Create clientsets
			k8sRequestProvider := &metrics.K8sRequestsCountProvider{}
			kubeclientmetrics.AddMetricsTransportWrapper(config, k8sRequestProvider.IncKubernetesRequest)

			kubeClient, err := kubernetes.NewForConfig(config)
			if err != nil {
				return fmt.Errorf("failed to create kubernetes clientset: %w", err)
			}

			argoprojClient, err := clientset.NewForConfig(config)
			if err != nil {
				return fmt.Errorf("failed to create argoproj clientset: %w", err)
			}

			dynamicClient, err := dynamic.NewForConfig(config)
			if err != nil {
				return fmt.Errorf("failed to create dynamic clientset: %w", err)
			}

			discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
			if err != nil {
				return fmt.Errorf("failed to create discovery client: %w", err)
			}

			smiClient, err := smiclientset.NewForConfig(config)
			if err != nil {
				return fmt.Errorf("failed to create SMI clientset: %w", err)
			}

			// ========================================
			// PART 1: Setup Standard Argo Rollouts Controllers
			// ========================================
			setupLog.Info("Setting up standard Argo Rollouts controllers")

			resyncDuration := time.Duration(rolloutResyncPeriod) * time.Second
			kubeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(
				kubeClient,
				resyncDuration,
				kubeinformers.WithNamespace(namespace))

			instanceIDSelector := controllerutil.InstanceIDRequirement(instanceID)
			instanceIDTweakListFunc := func(options *metav1.ListOptions) {
				options.LabelSelector = instanceIDSelector.String()
			}

			jobKubeClient := kubeClient // For simplicity, using the same client
			jobNs := namespace
			jobInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(
				jobKubeClient,
				resyncDuration,
				kubeinformers.WithNamespace(jobNs),
				kubeinformers.WithTweakListOptions(func(options *metav1.ListOptions) {
					options.LabelSelector = jobprovider.AnalysisRunUIDLabelKey
				}))

			dynamicInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, resyncDuration, namespace, instanceIDTweakListFunc)
			clusterDynamicInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, resyncDuration, metav1.NamespaceAll, instanceIDTweakListFunc)

			_, istioPrimaryDynamicClient := istioutil.GetPrimaryClusterDynamicClient(kubeClient, namespace)
			if istioPrimaryDynamicClient == nil {
				istioPrimaryDynamicClient = dynamicClient
			}
			istioDynamicInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(istioPrimaryDynamicClient, resyncDuration, namespace, nil)

			var notificationConfigNamespace string
			if selfServiceNotificationEnabled {
				notificationConfigNamespace = metav1.NamespaceAll
			} else {
				notificationConfigNamespace = defaults.Namespace()
			}

			notificationSecretInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(
				kubeClient,
				resyncDuration,
				kubeinformers.WithNamespace(notificationConfigNamespace),
				kubeinformers.WithTweakListOptions(func(options *metav1.ListOptions) {
					options.Kind = "Secret"
					options.FieldSelector = fmt.Sprintf("metadata.name=%s", record.NotificationSecret)
				}),
			)

			notificationConfigMapInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(
				kubeClient,
				resyncDuration,
				kubeinformers.WithNamespace(notificationConfigNamespace),
				kubeinformers.WithTweakListOptions(func(options *metav1.ListOptions) {
					options.Kind = "ConfigMap"
					options.FieldSelector = fmt.Sprintf("metadata.name=%s", record.NotificationConfigMap)
				}),
			)

			mode, err := ingressutil.DetermineIngressMode(ingressVersion, kubeClient.DiscoveryClient)
			if err != nil {
				return fmt.Errorf("failed to determine ingress mode: %w", err)
			}
			ingressWrapper, err := ingressutil.NewIngressWrapper(mode, kubeClient, kubeInformerFactory)
			if err != nil {
				return fmt.Errorf("failed to create ingress wrapper: %w", err)
			}

			// Create the standard controller manager
			cm := controller.NewManager(
				namespace,
				kubeClient,
				argoprojClient,
				dynamicClient,
				smiClient,
				discoveryClient,
				kubeInformerFactory.Apps().V1().ReplicaSets(),
				kubeInformerFactory.Core().V1().Services(),
				ingressWrapper,
				jobInformerFactory.Batch().V1().Jobs(),
				tolerantinformer.NewTolerantRolloutInformer(dynamicInformerFactory),
				tolerantinformer.NewTolerantExperimentInformer(dynamicInformerFactory),
				tolerantinformer.NewTolerantAnalysisRunInformer(dynamicInformerFactory),
				tolerantinformer.NewTolerantAnalysisTemplateInformer(dynamicInformerFactory),
				tolerantinformer.NewTolerantClusterAnalysisTemplateInformer(clusterDynamicInformerFactory),
				istioPrimaryDynamicClient,
				istioDynamicInformerFactory.ForResource(istioutil.GetIstioVirtualServiceGVR()).Informer(),
				istioDynamicInformerFactory.ForResource(istioutil.GetIstioDestinationRuleGVR()).Informer(),
				notificationConfigMapInformerFactory,
				notificationSecretInformerFactory,
				resyncDuration,
				instanceID,
				metricsPort,
				healthzPort,
				k8sRequestProvider,
				nginxIngressClasses,
				albIngressClasses,
				dynamicInformerFactory,
				clusterDynamicInformerFactory,
				istioDynamicInformerFactory,
				namespaced,
				kubeInformerFactory,
				jobInformerFactory,
				ephemeralMetadataThreads,
				ephemeralMetadataPodRetries)

			// ========================================
			// PART 2: Setup RolloutPlugin Controller (controller-runtime)
			// ========================================
			setupLog.Info("Setting up RolloutPlugin controller (controller-runtime)")

			// Set up controller-runtime logger
			ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: true})))

			mgrOpts := ctrl.Options{
				Scheme: scheme,
				Metrics: metricsserver.Options{
					BindAddress: fmt.Sprintf(":%d", rolloutPluginMetricsPort),
				},
				HealthProbeBindAddress: fmt.Sprintf(":%d", rolloutPluginHealthzPort),
				LeaderElection:         false, // Using standard controller's leader election
				LeaderElectionID:       "rolloutplugin.argoproj.io",
			}

			if namespace != metav1.NamespaceAll {
				setupLog.Info("RolloutPlugin controller watching specific namespace", "namespace", namespace)
				mgrOpts.Cache.DefaultNamespaces = map[string]cache.Config{
					namespace: {},
				}
			}

			mgr, err := ctrl.NewManager(config, mgrOpts)
			if err != nil {
				return fmt.Errorf("failed to create controller-runtime manager: %w", err)
			}

			// Create plugin manager and register built-in plugins
			pluginManager := rolloutplugin.NewPluginManager()

			logrusCtx := log.WithField("plugin", "statefulset") // TODO Make it generic
			statefulSetPlugin := statefulset.NewPlugin(kubeClient, logrusCtx)
			wrappedPlugin := pluginPackage.NewRolloutPlugin(statefulSetPlugin)

			if err := pluginManager.RegisterPlugin("statefulset", wrappedPlugin); err != nil {
				return fmt.Errorf("failed to register statefulset plugin: %w", err)
			}
			setupLog.Info("Registered StatefulSet plugin")

			// Create analysis helper to enable RolloutPlugin controller to reuse AnalysisRun logic
			// The helper uses listers from the informer-based controller manager
			analysisHelper := analysishelper.NewHelper(
				argoprojClient,
				tolerantinformer.NewTolerantAnalysisRunInformer(dynamicInformerFactory).Lister(),
				tolerantinformer.NewTolerantAnalysisTemplateInformer(dynamicInformerFactory).Lister(),
				tolerantinformer.NewTolerantClusterAnalysisTemplateInformer(clusterDynamicInformerFactory).Lister(),
			)

			// Set up the RolloutPlugin controller
			if err = (&rolloutplugin.RolloutPluginReconciler{
				Client:            mgr.GetClient(),
				Scheme:            mgr.GetScheme(),
				KubeClientset:     kubeClient,
				ArgoProjClientset: argoprojClient,
				DynamicClientset:  dynamicClient,
				PluginManager:     pluginManager,
				AnalysisHelper:    analysisHelper,
			}).SetupWithManager(mgr); err != nil {
				return fmt.Errorf("failed to setup RolloutPlugin controller: %w", err)
			}

			// Add health and readiness checks
			if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
				return fmt.Errorf("failed to setup health check: %w", err)
			}
			if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
				return fmt.Errorf("failed to setup ready check: %w", err)
			}

			// ========================================
			// PART 3: Run Both Controller Types Concurrently
			// ========================================
			var wg sync.WaitGroup

			// Start standard Argo Rollouts controllers
			wg.Add(1)
			go func() {
				defer wg.Done()
				setupLog.Info("Starting standard Argo Rollouts controllers")
				if err := cm.Run(ctx, rolloutThreads, serviceThreads, ingressThreads, experimentThreads, analysisThreads, electOpts); err != nil {
					setupLog.Error(err, "Error running standard controllers")
					os.Exit(1)
				}
				setupLog.Info("Standard Argo Rollouts controllers stopped")
			}()

			// Start controller-runtime manager for RolloutPlugin
			wg.Add(1)
			go func() {
				defer wg.Done()
				setupLog.Info("Starting RolloutPlugin controller (controller-runtime)")
				if err := mgr.Start(ctx); err != nil {
					setupLog.Error(err, "Error running RolloutPlugin controller")
					os.Exit(1)
				}
				setupLog.Info("RolloutPlugin controller stopped")
			}()

			setupLog.Info("All controllers started successfully")

			// Wait for context cancellation
			<-ctx.Done()
			setupLog.Info("Received shutdown signal, waiting for controllers to stop")

			// Wait for all controllers to finish
			wg.Wait()
			setupLog.Info("All controllers stopped gracefully")

			return nil
		},
	}

	// Set up command-line flags
	defaultALBIngressClass := []string{"alb"}
	defaultNGINXIngressClass := []string{"nginx"}

	clientConfig = addKubectlFlagsToCmd(&command)
	command.Flags().Int64Var(&rolloutResyncPeriod, "rollout-resync", controller.DefaultRolloutResyncPeriod, "Time period in seconds for rollouts resync.")
	command.Flags().BoolVar(&namespaced, "namespaced", false, "runs controller in namespaced mode (does not require cluster RBAC)")
	command.Flags().StringVar(&logLevel, "loglevel", "info", "Set the logging level. One of: debug|info|warn|error")
	command.Flags().IntVar(&metricsPort, "metricsPort", controller.DefaultMetricsPort, "Set the port the metrics endpoint should be exposed over (standard controllers)")
	command.Flags().IntVar(&healthzPort, "healthzPort", controller.DefaultHealthzPort, "Set the port the healthz endpoint should be exposed over (standard controllers)")
	command.Flags().IntVar(&rolloutPluginMetricsPort, "rolloutplugin-metrics-port", 8082, "Set the port the metrics endpoint for RolloutPlugin controller")
	command.Flags().IntVar(&rolloutPluginHealthzPort, "rolloutplugin-healthz-port", 8083, "Set the port the healthz endpoint for RolloutPlugin controller")
	command.Flags().StringVar(&instanceID, "instance-id", "", "Indicates which argo rollout objects the controller should operate on")
	command.Flags().Float32Var(&qps, "qps", defaults.DefaultQPS, "Maximum QPS (queries per second) to the K8s API server")
	command.Flags().IntVar(&burst, "burst", defaults.DefaultBurst, "Maximum burst for throttle.")
	command.Flags().IntVar(&rolloutThreads, "rollout-threads", controller.DefaultRolloutThreads, "Set the number of worker threads for the Rollout controller")
	command.Flags().IntVar(&experimentThreads, "experiment-threads", controller.DefaultExperimentThreads, "Set the number of worker threads for the Experiment controller")
	command.Flags().IntVar(&analysisThreads, "analysis-threads", controller.DefaultAnalysisThreads, "Set the number of worker threads for the Analysis controller")
	command.Flags().IntVar(&serviceThreads, "service-threads", controller.DefaultServiceThreads, "Set the number of worker threads for the Service controller")
	command.Flags().IntVar(&ingressThreads, "ingress-threads", controller.DefaultIngressThreads, "Set the number of worker threads for the Ingress controller")
	command.Flags().IntVar(&ephemeralMetadataThreads, "ephemeral-metadata-threads", rollout.DefaultEphemeralMetadataThreads, "Set the number of worker threads for the Ephemeral Metadata reconciler")
	command.Flags().IntVar(&ephemeralMetadataPodRetries, "ephemeral-metadata-update-pod-retries", rollout.DefaultEphemeralMetadataPodRetries, "Set the number of retries to update pod Ephemeral Metadata")
	command.Flags().StringVar(&targetGroupBindingVersion, "aws-target-group-binding-api-version", defaults.DefaultTargetGroupBindingAPIVersion, "Set the default AWS TargetGroupBinding apiVersion")
	command.Flags().StringVar(&albTagKeyResourceID, "alb-tag-key-resource-id", defaults.DefaultAlbTagKeyResourceID, "Set the default AWS LoadBalancer tag key for resource ID")
	command.Flags().StringVar(&istioVersion, "istio-api-version", defaults.DefaultIstioVersion, "Set the default Istio apiVersion")
	command.Flags().StringVar(&ambassadorVersion, "ambassador-api-version", defaults.DefaultAmbassadorVersion, "Set the Ambassador apiVersion")
	command.Flags().StringVar(&trafficSplitVersion, "traffic-split-api-version", defaults.DefaultSMITrafficSplitVersion, "Set the default TrafficSplit apiVersion")
	command.Flags().StringVar(&traefikAPIGroup, "traefik-api-group", defaults.DefaultTraefikAPIGroup, "Set the default Traefik apiGroup")
	command.Flags().StringVar(&traefikVersion, "traefik-api-version", defaults.DefaultTraefikVersion, "Set the default Traefik apiVersion")
	command.Flags().StringVar(&ingressVersion, "ingress-api-version", "", "Set the Ingress apiVersion")
	command.Flags().StringVar(&appmeshCRDVersion, "appmesh-crd-version", defaults.DefaultAppMeshCRDVersion, "Set the default AppMesh CRD Version")
	command.Flags().StringArrayVar(&albIngressClasses, "alb-ingress-classes", defaultALBIngressClass, "Defines all the ingress class annotations that the alb ingress controller operates on")
	command.Flags().StringArrayVar(&nginxIngressClasses, "nginx-ingress-classes", defaultNGINXIngressClass, "Defines all the ingress class annotations that the nginx ingress controller operates on")
	command.Flags().BoolVar(&awsVerifyTargetGroup, "aws-verify-target-group", false, "Verify ALB target group before progressing through steps")
	command.Flags().BoolVar(&electOpts.LeaderElect, "leader-elect", controller.DefaultLeaderElect, "Enable leader election")
	command.Flags().DurationVar(&electOpts.LeaderElectionLeaseDuration, "leader-election-lease-duration", controller.DefaultLeaderElectionLeaseDuration, "Leader election lease duration")
	command.Flags().DurationVar(&electOpts.LeaderElectionRenewDeadline, "leader-election-renew-deadline", controller.DefaultLeaderElectionRenewDeadline, "Leader election renew deadline")
	command.Flags().DurationVar(&electOpts.LeaderElectionRetryPeriod, "leader-election-retry-period", controller.DefaultLeaderElectionRetryPeriod, "Leader election retry period")
	command.Flags().BoolVar(&selfServiceNotificationEnabled, "self-service-notification-enabled", false, "Allows rollouts controller to pull notification config from the namespace")

	return &command
}

func main() {
	if err := newCommand().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func addKubectlFlagsToCmd(cmd *cobra.Command) clientcmd.ClientConfig {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.DefaultClientConfig = &clientcmd.DefaultClientConfig
	overrides := clientcmd.ConfigOverrides{}
	kflags := clientcmd.RecommendedConfigOverrideFlags("")
	cmd.PersistentFlags().StringVar(&loadingRules.ExplicitPath, "kubeconfig", "", "Path to a kube config. Only required if out-of-cluster")
	clientcmd.BindOverrideFlags(&overrides, cmd.PersistentFlags(), kflags)
	return clientcmd.NewInteractiveDeferredLoadingClientConfig(loadingRules, &overrides, os.Stdin)
}

func setLogLevel(logLevel string) {
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		log.Fatal(err)
	}
	log.SetLevel(level)
}
