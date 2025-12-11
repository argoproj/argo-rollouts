package main

import (
	"flag"
	"os"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-rollouts/rolloutplugin"
	pluginPackage "github.com/argoproj/argo-rollouts/rolloutplugin/plugin"
	"github.com/argoproj/argo-rollouts/rolloutplugin/plugins/statefulset"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var namespace string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&namespace, "namespace", "", "The namespace to watch. If empty, watches all namespaces.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

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
		setupLog.Info("Watching specific namespace", "namespace", namespace)
		mgrOpts.Cache.DefaultNamespaces = map[string]cache.Config{
			namespace: {},
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOpts)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Create additional clientsets
	config := ctrl.GetConfigOrDie()

	kubeClientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		setupLog.Error(err, "unable to create kubernetes clientset")
		os.Exit(1)
	}

	argoProjClientset, err := clientset.NewForConfig(config)
	if err != nil {
		setupLog.Error(err, "unable to create argoproj clientset")
		os.Exit(1)
	}

	dynamicClientset, err := dynamic.NewForConfig(config)
	if err != nil {
		setupLog.Error(err, "unable to create dynamic clientset")
		os.Exit(1)
	}

	// Create plugin manager
	pluginManager := rolloutplugin.NewPluginManager()

	// Register built-in plugins
	// For StatefulSet plugin, we can either use the direct plugin or the RPC wrapper
	// Using direct plugin for built-in mode (no separate executable needed)
	logrusCtx := log.WithField("plugin", "statefulset")
	statefulSetPlugin := statefulset.NewPlugin(kubeClientset, logrusCtx)

	// Wrap the plugin with RPC interface for consistency
	wrappedPlugin := pluginPackage.NewRolloutPlugin(statefulSetPlugin)

	if err := pluginManager.RegisterPlugin("statefulset", wrappedPlugin); err != nil {
		setupLog.Error(err, "unable to register statefulset plugin")
		os.Exit(1)
	}
	setupLog.Info("Registered StatefulSet plugin")

	// Set up the controller
	if err = (&rolloutplugin.RolloutPluginReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		KubeClientset:     kubeClientset,
		ArgoProjClientset: argoProjClientset,
		DynamicClientset:  dynamicClientset,
		PluginManager:     pluginManager,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "RolloutPlugin")
		os.Exit(1)
	}

	// Add health and readiness checks
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
