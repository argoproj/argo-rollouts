package main

import (
	"fmt"
	"os"
	"time"

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
	"github.com/argoproj/pkg/kubeclientmetrics"
)

const (
	// CLIName is the name of the CLI
	cliName = "argo-rollouts"
)

func newCommand() *cobra.Command {
	var (
		clientConfig         clientcmd.ClientConfig
		rolloutResyncPeriod  int64
		logLevel             string
		klogLevel            int
		metricsPort          int
		instanceID           string
		rolloutThreads       int
		experimentThreads    int
		analysisThreads      int
		serviceThreads       int
		ingressThreads       int
		istioVersion         string
		trafficSplitVersion  string
		ambassadorVersion    string
		ingressVersion       string
		albIngressClasses    []string
		nginxIngressClasses  []string
		awsVerifyTargetGroup bool
		namespaced           bool
		printVersion         bool
	)
	var command = cobra.Command{
		Use:   cliName,
		Short: "argo-rollouts is a controller to operate on rollout CRD",
		RunE: func(c *cobra.Command, args []string) error {
			if printVersion {
				fmt.Println(version.GetVersion())
				return nil
			}
			setLogLevel(logLevel)
			formatter := &log.TextFormatter{
				FullTimestamp: true,
			}
			log.SetFormatter(formatter)
			logutil.SetKLogLevel(klogLevel)
			log.WithField("version", version.GetVersion()).Info("Argo Rollouts starting")

			// set up signals so we handle the first shutdown signal gracefully
			stopCh := signals.SetupSignalHandler()

			defaults.SetVerifyTargetGroup(awsVerifyTargetGroup)
			defaults.SetIstioAPIVersion(istioVersion)
			defaults.SetAmbassadorAPIVersion(ambassadorVersion)
			defaults.SetSMIAPIVersion(trafficSplitVersion)

			config, err := clientConfig.ClientConfig()
			checkError(err)
			namespace := metav1.NamespaceAll
			configNS, _, err := clientConfig.Namespace()
			checkError(err)
			if namespaced {
				namespace = configNS
				log.Infof("Using namespace %s", namespace)
			}

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

			controllerNamespaceInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(
				kubeClient,
				resyncDuration,
				kubeinformers.WithNamespace(defaults.Namespace()))
			configMapInformer := controllerNamespaceInformerFactory.Core().V1().ConfigMaps()
			secretInformer := controllerNamespaceInformerFactory.Core().V1().Secrets()

			k8sRequestProvider := &metrics.K8sRequestsCountProvider{}
			kubeclientmetrics.AddMetricsTransportWrapper(config, k8sRequestProvider.IncKubernetesRequest)
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
				configMapInformer,
				secretInformer,
				resyncDuration,
				instanceID,
				metricsPort,
				k8sRequestProvider,
				nginxIngressClasses,
				albIngressClasses)
			// notice that there is no need to run Start methods in a separate goroutine. (i.e. go kubeInformerFactory.Start(stopCh)
			// Start method is non-blocking and runs all registered informers in a dedicated goroutine.
			dynamicInformerFactory.Start(stopCh)
			if !namespaced {
				clusterDynamicInformerFactory.Start(stopCh)
			}
			kubeInformerFactory.Start(stopCh)
			controllerNamespaceInformerFactory.Start(stopCh)
			jobInformerFactory.Start(stopCh)

			// Check if Istio installed on cluster before starting dynamicInformerFactory
			if istioutil.DoesIstioExist(istioPrimaryDynamicClient, namespace) {
				istioDynamicInformerFactory.Start(stopCh)
			}

			if err = cm.Run(rolloutThreads, serviceThreads, ingressThreads, experimentThreads, analysisThreads, stopCh); err != nil {
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
	command.Flags().IntVar(&klogLevel, "kloglevel", 0, "Set the klog logging level")
	command.Flags().IntVar(&metricsPort, "metricsport", controller.DefaultMetricsPort, "Set the port the metrics endpoint should be exposed over")
	command.Flags().StringVar(&instanceID, "instance-id", "", "Indicates which argo rollout objects the controller should operate on")
	command.Flags().IntVar(&rolloutThreads, "rollout-threads", controller.DefaultRolloutThreads, "Set the number of worker threads for the Rollout controller")
	command.Flags().IntVar(&experimentThreads, "experiment-threads", controller.DefaultExperimentThreads, "Set the number of worker threads for the Experiment controller")
	command.Flags().IntVar(&analysisThreads, "analysis-threads", controller.DefaultAnalysisThreads, "Set the number of worker threads for the Experiment controller")
	command.Flags().IntVar(&serviceThreads, "service-threads", controller.DefaultServiceThreads, "Set the number of worker threads for the Service controller")
	command.Flags().IntVar(&ingressThreads, "ingress-threads", controller.DefaultIngressThreads, "Set the number of worker threads for the Ingress controller")
	command.Flags().StringVar(&istioVersion, "istio-api-version", defaults.DefaultIstioVersion, "Set the default Istio apiVersion that controller should look when manipulating VirtualServices.")
	command.Flags().StringVar(&ambassadorVersion, "ambassador-api-version", defaults.DefaultAmbassadorVersion, "Set the Ambassador apiVersion that controller should look when manipulating Ambassador Mappings.")
	command.Flags().StringVar(&trafficSplitVersion, "traffic-split-api-version", defaults.DefaultSMITrafficSplitVersion, "Set the default TrafficSplit apiVersion that controller uses when creating TrafficSplits.")
	command.Flags().StringVar(&ingressVersion, "ingress-api-version", "", "Set the Ingress apiVersion that the controller should use.")
	command.Flags().StringArrayVar(&albIngressClasses, "alb-ingress-classes", defaultALBIngressClass, "Defines all the ingress class annotations that the alb ingress controller operates on. Defaults to alb")
	command.Flags().StringArrayVar(&nginxIngressClasses, "nginx-ingress-classes", defaultNGINXIngressClass, "Defines all the ingress class annotations that the nginx ingress controller operates on. Defaults to nginx")
	command.Flags().BoolVar(&awsVerifyTargetGroup, "alb-verify-weight", false, "Verify ALB target group weights before progressing through steps (requires AWS privileges)")
	command.Flags().MarkDeprecated("alb-verify-weight", "Use --aws-verify-target-group instead")
	command.Flags().BoolVar(&awsVerifyTargetGroup, "aws-verify-target-group", false, "Verify ALB target group before progressing through steps (requires AWS privileges)")
	command.Flags().BoolVar(&printVersion, "version", false, "Print version")
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

func checkError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
