package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/argoproj/argo-rollouts/utils/record"

	"github.com/argoproj/pkg/kubeclientmetrics"
	smiclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/argoproj/argo-rollouts/controller"
	"github.com/argoproj/argo-rollouts/controller/metrics"
	jobprovider "github.com/argoproj/argo-rollouts/metricproviders/job"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-rollouts/pkg/signals"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/tolerantinformer"
	"github.com/argoproj/argo-rollouts/utils/version"
)

const (
	// CLIName is the name of the CLI
	cliName    = "argo-rollouts"
	jsonFormat = "json"
	textFormat = "text"
)

func newCommand() *cobra.Command {
	var (
		clientConfig                   clientcmd.ClientConfig
		rolloutResyncPeriod            int64
		logLevel                       string
		logFormat                      string
		klogLevel                      int
		metricsPort                    int
		healthzPort                    int
		instanceID                     string
		qps                            float32
		burst                          int
		rolloutThreads                 int
		experimentThreads              int
		analysisThreads                int
		serviceThreads                 int
		ingressThreads                 int
		istioVersion                   string
		trafficSplitVersion            string
		ambassadorVersion              string
		ingressVersion                 string
		appmeshCRDVersion              string
		albIngressClasses              []string
		nginxIngressClasses            []string
		awsVerifyTargetGroup           bool
		namespaced                     bool
		printVersion                   bool
		selfServiceNotificationEnabled bool
	)
	electOpts := controller.NewLeaderElectionOptions()
	var command = cobra.Command{
		Use:   cliName,
		Short: "argo-rollouts is a controller to operate on rollout CRD",
		RunE: func(c *cobra.Command, args []string) error {
			if printVersion {
				fmt.Println(version.GetVersion())
				return nil
			}
			setLogLevel(logLevel)
			if logFormat != "" {
				log.SetFormatter(createFormatter(logFormat))
			}
			logutil.SetKLogLogger(log.New())
			logutil.SetKLogLevel(klogLevel)
			log.WithField("version", version.GetVersion()).Info("Argo Rollouts starting")

			// set up signals so we handle the first shutdown signal gracefully
			ctx := signals.SetupSignalHandlerContext()

			defaults.SetVerifyTargetGroup(awsVerifyTargetGroup)
			defaults.SetIstioAPIVersion(istioVersion)
			defaults.SetAmbassadorAPIVersion(ambassadorVersion)
			defaults.SetSMIAPIVersion(trafficSplitVersion)
			defaults.SetAppMeshCRDVersion(appmeshCRDVersion)

			config, err := clientConfig.ClientConfig()
			checkError(err)
			config.QPS = qps
			config.Burst = burst
			namespace := metav1.NamespaceAll
			configNS, _, err := clientConfig.Namespace()
			checkError(err)
			if namespaced {
				namespace = configNS
				log.Infof("Using namespace %s", namespace)
			}

			k8sRequestProvider := &metrics.K8sRequestsCountProvider{}
			kubeclientmetrics.AddMetricsTransportWrapper(config, k8sRequestProvider.IncKubernetesRequest)

			kubeClient, err := kubernetes.NewForConfig(config)
			checkError(err)
			argoprojClient, err := clientset.NewForConfig(config)
			checkError(err)
			dynamicClient, err := dynamic.NewForConfig(config)
			checkError(err)
			discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
			checkError(err)
			smiClient, err := smiclientset.NewForConfig(config)
			resyncDuration := time.Duration(rolloutResyncPeriod) * time.Second
			kubeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(
				kubeClient,
				resyncDuration,
				kubeinformers.WithNamespace(namespace))
			instanceIDSelector := controllerutil.InstanceIDRequirement(instanceID)
			instanceIDTweakListFunc := func(options *metav1.ListOptions) {
				options.LabelSelector = instanceIDSelector.String()
			}
			jobInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(
				kubeClient,
				resyncDuration,
				kubeinformers.WithNamespace(namespace),
				kubeinformers.WithTweakListOptions(func(options *metav1.ListOptions) {
					options.LabelSelector = jobprovider.AnalysisRunUIDLabelKey
				}))
			// We need three dynamic informer factories:
			// 1. The first is the dynamic informer for rollouts, analysisruns, analysistemplates, experiments
			dynamicInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, resyncDuration, namespace, instanceIDTweakListFunc)
			// 2. The second is for the clusteranalysistemplate. Notice we must instantiate this with
			// metav1.NamespaceAll. The reason why we need a cluster specific dynamic informer factory
			// is to support the mode when the rollout controller is started and only operating against
			// a single namespace (i.e. rollouts-controller --namespace foo).
			clusterDynamicInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, resyncDuration, metav1.NamespaceAll, instanceIDTweakListFunc)
			// 3. We finally need an istio dynamic informer factory which does not use a tweakListFunc.
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
			checkError(err)
			ingressWrapper, err := ingressutil.NewIngressWrapper(mode, kubeClient, kubeInformerFactory)
			checkError(err)

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
				jobInformerFactory)

			if err = cm.Run(ctx, rolloutThreads, serviceThreads, ingressThreads, experimentThreads, analysisThreads, electOpts); err != nil {
				log.Fatalf("Error running controller: %s", err.Error())
			}
			return nil
		},
	}

	defaultALBIngressClass := []string{"alb"}
	defaultNGINXIngressClass := []string{"nginx"}

	clientConfig = addKubectlFlagsToCmd(&command)
	command.Flags().Int64Var(&rolloutResyncPeriod, "rollout-resync", controller.DefaultRolloutResyncPeriod, "Time period in seconds for rollouts resync.")
	command.Flags().BoolVar(&namespaced, "namespaced", false, "runs controller in namespaced mode (does not require cluster RBAC)")
	command.Flags().StringVar(&logLevel, "loglevel", "info", "Set the logging level. One of: debug|info|warn|error")
	command.Flags().StringVar(&logFormat, "logformat", "", "Set the logging format. One of: text|json")
	command.Flags().IntVar(&klogLevel, "kloglevel", 0, "Set the klog logging level")
	command.Flags().IntVar(&metricsPort, "metricsport", controller.DefaultMetricsPort, "Set the port the metrics endpoint should be exposed over")
	command.Flags().IntVar(&healthzPort, "healthzPort", controller.DefaultHealthzPort, "Set the port the healthz endpoint should be exposed over")
	command.Flags().StringVar(&instanceID, "instance-id", "", "Indicates which argo rollout objects the controller should operate on")
	command.Flags().Float32Var(&qps, "qps", defaults.DefaultQPS, "Maximum QPS (queries per second) to the K8s API server")
	command.Flags().IntVar(&burst, "burst", defaults.DefaultBurst, "Maximum burst for throttle.")
	command.Flags().IntVar(&rolloutThreads, "rollout-threads", controller.DefaultRolloutThreads, "Set the number of worker threads for the Rollout controller")
	command.Flags().IntVar(&experimentThreads, "experiment-threads", controller.DefaultExperimentThreads, "Set the number of worker threads for the Experiment controller")
	command.Flags().IntVar(&analysisThreads, "analysis-threads", controller.DefaultAnalysisThreads, "Set the number of worker threads for the Experiment controller")
	command.Flags().IntVar(&serviceThreads, "service-threads", controller.DefaultServiceThreads, "Set the number of worker threads for the Service controller")
	command.Flags().IntVar(&ingressThreads, "ingress-threads", controller.DefaultIngressThreads, "Set the number of worker threads for the Ingress controller")
	command.Flags().StringVar(&istioVersion, "istio-api-version", defaults.DefaultIstioVersion, "Set the default Istio apiVersion that controller should look when manipulating VirtualServices.")
	command.Flags().StringVar(&ambassadorVersion, "ambassador-api-version", defaults.DefaultAmbassadorVersion, "Set the Ambassador apiVersion that controller should look when manipulating Ambassador Mappings.")
	command.Flags().StringVar(&trafficSplitVersion, "traffic-split-api-version", defaults.DefaultSMITrafficSplitVersion, "Set the default TrafficSplit apiVersion that controller uses when creating TrafficSplits.")
	command.Flags().StringVar(&ingressVersion, "ingress-api-version", "", "Set the Ingress apiVersion that the controller should use.")
	command.Flags().StringVar(&appmeshCRDVersion, "appmesh-crd-version", defaults.DefaultAppMeshCRDVersion, "Set the default AppMesh CRD Version that controller uses when manipulating resources.")
	command.Flags().StringArrayVar(&albIngressClasses, "alb-ingress-classes", defaultALBIngressClass, "Defines all the ingress class annotations that the alb ingress controller operates on. Defaults to alb")
	command.Flags().StringArrayVar(&nginxIngressClasses, "nginx-ingress-classes", defaultNGINXIngressClass, "Defines all the ingress class annotations that the nginx ingress controller operates on. Defaults to nginx")
	command.Flags().BoolVar(&awsVerifyTargetGroup, "alb-verify-weight", false, "Verify ALB target group weights before progressing through steps (requires AWS privileges)")
	command.Flags().MarkDeprecated("alb-verify-weight", "Use --aws-verify-target-group instead")
	command.Flags().BoolVar(&awsVerifyTargetGroup, "aws-verify-target-group", false, "Verify ALB target group before progressing through steps (requires AWS privileges)")
	command.Flags().BoolVar(&printVersion, "version", false, "Print version")
	command.Flags().BoolVar(&electOpts.LeaderElect, "leader-elect", controller.DefaultLeaderElect, "If true, controller will perform leader election between instances to ensure no more than one instance of controller operates at a time")
	command.Flags().DurationVar(&electOpts.LeaderElectionLeaseDuration, "leader-election-lease-duration", controller.DefaultLeaderElectionLeaseDuration, "The duration that non-leader candidates will wait after observing a leadership renewal until attempting to acquire leadership of a led but unrenewed leader slot. This is effectively the maximum duration that a leader can be stopped before it is replaced by another candidate. This is only applicable if leader election is enabled.")
	command.Flags().DurationVar(&electOpts.LeaderElectionRenewDeadline, "leader-election-renew-deadline", controller.DefaultLeaderElectionRenewDeadline, "The interval between attempts by the acting master to renew a leadership slot before it stops leading. This must be less than or equal to the lease duration. This is only applicable if leader election is enabled.")
	command.Flags().DurationVar(&electOpts.LeaderElectionRetryPeriod, "leader-election-retry-period", controller.DefaultLeaderElectionRetryPeriod, "The duration the clients should wait between attempting acquisition and renewal of a leadership. This is only applicable if leader election is enabled.")
	command.Flags().BoolVar(&selfServiceNotificationEnabled, "self-service-notification-enabled", false, "Allows rollouts controller to pull notification config from the namespace that the rollout resource is in. This is useful for self-service notification.")
	return &command
}

func main() {
	if err := newCommand().Execute(); err != nil {
		fmt.Println(err)
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

// setLogLevel parses and sets a logrus log level
func setLogLevel(logLevel string) {
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		log.Fatal(err)
	}
	log.SetLevel(level)
}

func createFormatter(logFormat string) log.Formatter {
	var formatType log.Formatter
	switch strings.ToLower(logFormat) {
	case jsonFormat:
		formatType = &log.JSONFormatter{}
	case textFormat:
		formatType = &log.TextFormatter{
			FullTimestamp: true,
		}
	default:
		log.Infof("Unknown format: %s. Using text logformat", logFormat)
		formatType = &log.TextFormatter{
			FullTimestamp: true,
		}
	}

	return formatType
}

func checkError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
