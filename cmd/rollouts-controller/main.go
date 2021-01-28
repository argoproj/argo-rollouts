package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	smiclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	"github.com/argoproj/argo-rollouts/controller"
	"github.com/argoproj/argo-rollouts/controller/metrics"
	jobprovider "github.com/argoproj/argo-rollouts/metricproviders/job"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-rollouts/pkg/signals"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/alb"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	kubeclientmetrics "github.com/argoproj/argo-rollouts/utils/kubeclientmetrics"
	"github.com/argoproj/argo-rollouts/utils/tolerantinformer"
)

const (
	// CLIName is the name of the CLI
	cliName                    = "argo-rollouts"
	defaultIstioVersion        = "v1alpha3"
	defaultTrafficSplitVersion = "v1alpha1"
)

func newCommand() *cobra.Command {
	var (
		clientConfig        clientcmd.ClientConfig
		rolloutResyncPeriod int64
		logLevel            string
		glogLevel           int
		metricsPort         int
		instanceID          string
		rolloutThreads      int
		experimentThreads   int
		analysisThreads     int
		serviceThreads      int
		ingressThreads      int
		istioVersion        string
		trafficSplitVersion string
		albIngressClasses   []string
		nginxIngressClasses []string
		albVerifyWeight     bool
		namespaced          bool
	)
	var command = cobra.Command{
		Use:   cliName,
		Short: "argo-rollouts is a controller to operate on rollout CRD",
		RunE: func(c *cobra.Command, args []string) error {
			setLogLevel(logLevel)
			formatter := &log.TextFormatter{
				FullTimestamp: true,
			}
			log.SetFormatter(formatter)
			setGLogLevel(glogLevel)

			// set up signals so we handle the first shutdown signal gracefully
			stopCh := signals.SetupSignalHandler()

			alb.SetDefaultVerifyWeight(albVerifyWeight)

			config, err := clientConfig.ClientConfig()
			checkError(err)
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
			rolloutClient, err := clientset.NewForConfig(config)
			checkError(err)
			dynamicClient, err := dynamic.NewForConfig(config)
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
			istioGVR := istioutil.GetIstioGVR(istioVersion)
			// We need three dynamic informer factories:
			// 1. The first is the dynamic informer for rollouts, analysisruns, analysistemplates, experiments
			dynamicInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, resyncDuration, namespace, instanceIDTweakListFunc)
			// 2. The second is for the clusteranalysistemplate. Notice we must instantiate this with
			// metav1.NamespaceAll. The reason why we need a cluster specific dynamic informer factory
			// is to support the mode when the rollout controller is started and only operating against
			// a single namespace (i.e. rollouts-controller --namespace foo).
			clusterDynamicInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, resyncDuration, metav1.NamespaceAll, instanceIDTweakListFunc)
			// 3. We finally need an istio dynamic informer factory which uses different resync
			// period and does not use a tweakListFunc.
			istioDynamicInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, 0, namespace, nil)
			cm := controller.NewManager(
				namespace,
				kubeClient,
				rolloutClient,
				dynamicClient,
				smiClient,
				kubeInformerFactory.Apps().V1().ReplicaSets(),
				kubeInformerFactory.Core().V1().Services(),
				kubeInformerFactory.Extensions().V1beta1().Ingresses(),
				jobInformerFactory.Batch().V1().Jobs(),
				tolerantinformer.NewTolerantRolloutInformer(dynamicInformerFactory),
				tolerantinformer.NewTolerantExperimentInformer(dynamicInformerFactory),
				tolerantinformer.NewTolerantAnalysisRunInformer(dynamicInformerFactory),
				tolerantinformer.NewTolerantAnalysisTemplateInformer(dynamicInformerFactory),
				tolerantinformer.NewTolerantClusterAnalysisTemplateInformer(clusterDynamicInformerFactory),
				istioDynamicInformerFactory.ForResource(istioGVR).Informer(),
				resyncDuration,
				instanceID,
				metricsPort,
				k8sRequestProvider,
				istioVersion,
				trafficSplitVersion,
				nginxIngressClasses,
				albIngressClasses)
			// notice that there is no need to run Start methods in a separate goroutine. (i.e. go kubeInformerFactory.Start(stopCh)
			// Start method is non-blocking and runs all registered informers in a dedicated goroutine.
			dynamicInformerFactory.Start(stopCh)
			if !namespaced {
				clusterDynamicInformerFactory.Start(stopCh)
			}
			kubeInformerFactory.Start(stopCh)
			jobInformerFactory.Start(stopCh)

			// Check if Istio installed on cluster before starting dynamicInformerFactory
			if istioutil.DoesIstioExist(dynamicClient, namespace, istioVersion) {
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
	command.Flags().IntVar(&glogLevel, "gloglevel", 0, "Set the glog logging level")
	command.Flags().IntVar(&metricsPort, "metricsport", controller.DefaultMetricsPort, "Set the port the metrics endpoint should be exposed over")
	command.Flags().StringVar(&instanceID, "instance-id", "", "Indicates which argo rollout objects the controller should operate on")
	command.Flags().IntVar(&rolloutThreads, "rollout-threads", controller.DefaultRolloutThreads, "Set the number of worker threads for the Rollout controller")
	command.Flags().IntVar(&experimentThreads, "experiment-threads", controller.DefaultExperimentThreads, "Set the number of worker threads for the Experiment controller")
	command.Flags().IntVar(&analysisThreads, "analysis-threads", controller.DefaultAnalysisThreads, "Set the number of worker threads for the Experiment controller")
	command.Flags().IntVar(&serviceThreads, "service-threads", controller.DefaultServiceThreads, "Set the number of worker threads for the Service controller")
	command.Flags().IntVar(&ingressThreads, "ingress-threads", controller.DefaultIngressThreads, "Set the number of worker threads for the Ingress controller")
	command.Flags().StringVar(&istioVersion, "istio-api-version", defaultIstioVersion, "Set the default Istio apiVersion that controller should look when manipulating VirtualServices.")
	command.Flags().StringVar(&trafficSplitVersion, "traffic-split-api-version", defaultTrafficSplitVersion, "Set the default TrafficSplit apiVersion that controller uses when creating TrafficSplits.")
	command.Flags().StringArrayVar(&albIngressClasses, "alb-ingress-classes", defaultALBIngressClass, "Defines all the ingress class annotations that the alb ingress controller operates on. Defaults to alb")
	command.Flags().StringArrayVar(&nginxIngressClasses, "nginx-ingress-classes", defaultNGINXIngressClass, "Defines all the ingress class annotations that the nginx ingress controller operates on. Defaults to nginx")
	command.Flags().BoolVar(&albVerifyWeight, "alb-verify-weight", false, "Verify ALB target group weights before progressing through steps (requires AWS privileges)")
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

// setGLogLevel set the glog level for the k8s go-client
func setGLogLevel(glogLevel int) {
	klog.InitFlags(nil)
	_ = flag.Set("logtostderr", "true")
	_ = flag.Set("v", strconv.Itoa(glogLevel))
}

func checkError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
