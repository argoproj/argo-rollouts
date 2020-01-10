package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	"github.com/argoproj/argo-rollouts/controller"
	jobprovider "github.com/argoproj/argo-rollouts/metricproviders/job"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	"github.com/argoproj/argo-rollouts/pkg/signals"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
)

const (
	// CLIName is the name of the CLI
	cliName = "argo-rollouts"

	defaultIstioVersion = "v1alpha3"
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
		istioVersion        string
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

			// cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
			config, err := clientConfig.ClientConfig()
			checkError(err)
			namespace := metav1.NamespaceAll
			configNS, modified, err := clientConfig.Namespace()
			checkError(err)
			if modified {
				namespace = configNS
				log.Infof("Using namespace %s", namespace)
			}

			kubeClient, err := kubernetes.NewForConfig(config)
			checkError(err)
			rolloutClient, err := clientset.NewForConfig(config)
			checkError(err)
			dynamicClient, err := dynamic.NewForConfig(config)
			checkError(err)
			resyncDuration := time.Duration(rolloutResyncPeriod) * time.Second
			kubeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(
				kubeClient,
				resyncDuration,
				kubeinformers.WithNamespace(namespace))
			instanceIDSelector := controllerutil.InstanceIDRequirement(instanceID)
			argoRolloutsInformerFactory := informers.NewSharedInformerFactoryWithOptions(
				rolloutClient,
				resyncDuration,
				informers.WithNamespace(namespace),
				informers.WithTweakListOptions(func(options *metav1.ListOptions) {
					options.LabelSelector = instanceIDSelector.String()
				}))
			jobInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(
				kubeClient,
				resyncDuration,
				kubeinformers.WithNamespace(namespace),
				kubeinformers.WithTweakListOptions(func(options *metav1.ListOptions) {
					options.LabelSelector = jobprovider.AnalysisRunUIDLabelKey
				}))
			cm := controller.NewManager(
				namespace,
				kubeClient,
				rolloutClient,
				dynamicClient,
				kubeInformerFactory.Apps().V1().ReplicaSets(),
				kubeInformerFactory.Core().V1().Services(),
				jobInformerFactory.Batch().V1().Jobs(),
				argoRolloutsInformerFactory.Argoproj().V1alpha1().Rollouts(),
				argoRolloutsInformerFactory.Argoproj().V1alpha1().Experiments(),
				argoRolloutsInformerFactory.Argoproj().V1alpha1().AnalysisRuns(),
				argoRolloutsInformerFactory.Argoproj().V1alpha1().AnalysisTemplates(),
				resyncDuration,
				instanceID,
				metricsPort,
				defaultIstioVersion)

			// notice that there is no need to run Start methods in a separate goroutine. (i.e. go kubeInformerFactory.Start(stopCh)
			// Start method is non-blocking and runs all registered informers in a dedicated goroutine.
			kubeInformerFactory.Start(stopCh)
			argoRolloutsInformerFactory.Start(stopCh)
			jobInformerFactory.Start(stopCh)

			if err = cm.Run(rolloutThreads, serviceThreads, experimentThreads, analysisThreads, stopCh); err != nil {
				log.Fatalf("Error running controller: %s", err.Error())
			}
			return nil
		},
	}
	clientConfig = addKubectlFlagsToCmd(&command)
	command.Flags().Int64Var(&rolloutResyncPeriod, "rollout-resync", controller.DefaultRolloutResyncPeriod, "Time period in seconds for rollouts resync.")
	command.Flags().StringVar(&logLevel, "loglevel", "info", "Set the logging level. One of: debug|info|warn|error")
	command.Flags().IntVar(&glogLevel, "gloglevel", 0, "Set the glog logging level")
	command.Flags().IntVar(&metricsPort, "metricsport", controller.DefaultMetricsPort, "Set the port the metrics endpoint should be exposed over")
	command.Flags().StringVar(&instanceID, "instance-id", "", "Indicates which argo rollout objects the controller should operate on")
	command.Flags().IntVar(&rolloutThreads, "rollout-threads", controller.DefaultRolloutThreads, "Set the number of worker threads for the Rollout controller")
	command.Flags().IntVar(&experimentThreads, "experiment-threads", controller.DefaultExperimentThreads, "Set the number of worker threads for the Experiment controller")
	command.Flags().IntVar(&analysisThreads, "analysis-threads", controller.DefaultAnalysisThreads, "Set the number of worker threads for the Experiment controller")
	command.Flags().IntVar(&serviceThreads, "service-threads", controller.DefaultServiceThreads, "Set the number of worker threads for the Service controller")
	command.Flags().StringVar(&istioVersion, "istio-api-version", defaultIstioVersion, "Set the default Istio apiVersion that controller should look when manipulating VirtualServices.")
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
