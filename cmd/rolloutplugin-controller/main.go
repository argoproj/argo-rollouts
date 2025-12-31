package main

import (
	"flag"

	"github.com/go-logr/logr"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-rollouts/rolloutplugin"
	pluginPackage "github.com/argoproj/argo-rollouts/rolloutplugin/plugin"
	"github.com/argoproj/argo-rollouts/rolloutplugin/plugins/statefulset"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	// Set controller-runtime logger to a null logger to suppress the warning
	// We use logrus for our own logging
	ctrl.SetLogger(logr.New(ctrllog.NullLogSink{}))

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var namespace string
	var logLevel string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&namespace, "namespace", "", "The namespace to watch. If empty, watches all namespaces.")
	flag.StringVar(&logLevel, "loglevel", "info", "Set the logging level. One of: debug, info, warn, error")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	flag.Parse()

	// Set up logrus log level
	setLogLevel(logLevel)

	// Set up manager options
	mgrOpts := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "rolloutplugin.argoproj.io",
	}

	// If a specific namespace is provided, configure the cache to watch only that namespace
	if namespace != "" {
		log.WithField("namespace", namespace).Info("Watching specific namespace")
		mgrOpts.Cache.DefaultNamespaces = map[string]cache.Config{
			namespace: {},
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOpts)
	if err != nil {
		log.WithError(err).Fatal("Unable to start manager")
	}

	// Create additional clientsets
	config := ctrl.GetConfigOrDie()

	kubeClientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.WithError(err).Fatal("Unable to create kubernetes clientset")
	}

	argoProjClientset, err := clientset.NewForConfig(config)
	if err != nil {
		log.WithError(err).Fatal("Unable to create argoproj clientset")
	}

	dynamicClientset, err := dynamic.NewForConfig(config)
	if err != nil {
		log.WithError(err).Fatal("Unable to create dynamic clientset")
	}

	// Create plugin manager
	pluginManager := rolloutplugin.NewPluginManager()

	// Register built-in plugins (similar to metric providers pattern)
	// StatefulSet is a built-in plugin for native Kubernetes kind
	logrusCtx := log.WithField("plugin", "statefulset")
	statefulSetPlugin := statefulset.NewPlugin(kubeClientset, logrusCtx)
	wrappedPlugin := pluginPackage.NewRolloutPlugin(statefulSetPlugin)

	if err := pluginManager.RegisterPlugin("statefulset", wrappedPlugin); err != nil {
		log.WithError(err).Fatal("Unable to register statefulset plugin")
	}
	log.Info("Registered built-in StatefulSet plugin")

	// Set up the controller
	if err = (&rolloutplugin.RolloutPluginReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		KubeClientset:     kubeClientset,
		ArgoProjClientset: argoProjClientset,
		DynamicClientset:  dynamicClientset,
		PluginManager:     pluginManager,
	}).SetupWithManager(mgr); err != nil {
		log.WithError(err).WithField("controller", "RolloutPlugin").Fatal("Unable to create controller")
	}

	// Add health and readiness checks
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.WithError(err).Fatal("Unable to set up health check")
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.WithError(err).Fatal("Unable to set up ready check")
	}

	log.Info("Starting RolloutPlugin controller manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.WithError(err).Fatal("Problem running manager")
	}
}

// setLogLevel sets the logging level based on the provided string
func setLogLevel(logLevel string) {
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		log.WithField("level", logLevel).Warn("Invalid log level, defaulting to info")
		level = log.InfoLevel
	}
	log.SetLevel(level)
}
