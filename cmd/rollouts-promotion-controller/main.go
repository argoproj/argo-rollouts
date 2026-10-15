package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/argoproj/argo-rollouts/controller"
	"github.com/argoproj/argo-rollouts/controller/promotion"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutclient "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	rolloutinformers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	"github.com/argoproj/argo-rollouts/pkg/signals"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/errors"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/version"
)

const (
	cliName    = "rollouts-promotion-controller"
	jsonFormat = "json"
	textFormat = "text"
)

func newCommand() *cobra.Command {
	var (
		clientConfig       clientcmd.ClientConfig
		namespace          string
		resyncPeriod       int64
		workers            int
		qps                float32
		burst              int
		logLevel           string
		logFormat          string
		klogLevel          int
		healthzPort        int
		printVersion       bool
		metricsPlaceholder int
		leaderElect        bool
		leaderNamespace    string
		leaderLockName     string
		leaderLease        time.Duration
		leaderRenew        time.Duration
		leaderRetry        time.Duration
	)

	command := cobra.Command{
		Use:   cliName,
		Short: "rollouts-promotion-controller promotes Argo Rollouts from RolloutPromotion CRs",
		RunE: func(c *cobra.Command, args []string) error {
			if printVersion {
				fmt.Println(version.GetVersion())
				return nil
			}

			setLogLevel(logLevel)
			if logFormat != "" {
				log.SetFormatter(createFormatter(logFormat))
			}
			logutil.SetKLogLevel(klogLevel)

			ctx := signals.SetupSignalHandlerContext()
			config, err := clientConfig.ClientConfig()
			errors.CheckError(err)
			config.QPS = qps
			config.Burst = burst

			watchNamespace := namespace
			if watchNamespace == "" {
				watchNamespace = metav1.NamespaceAll
			}

			kubeClient, err := kubernetes.NewForConfig(config)
			errors.CheckError(err)
			dynamicClient, err := dynamic.NewForConfig(config)
			errors.CheckError(err)
			rolloutsClient, err := rolloutclient.NewForConfig(config)
			errors.CheckError(err)

			if healthzPort > 0 {
				healthzServer := controller.NewHealthzServer(fmt.Sprintf("0.0.0.0:%d", healthzPort))
				go func() {
					if err := healthzServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
						log.WithError(err).Error("healthz server stopped")
					}
				}()
				go func() {
					<-ctx.Done()
					shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					_ = healthzServer.Shutdown(shutdownCtx)
				}()
			}

			resyncDuration := time.Duration(resyncPeriod) * time.Second
			factory := rolloutinformers.NewSharedInformerFactoryWithOptions(rolloutsClient, resyncDuration, rolloutinformers.WithNamespace(watchNamespace))
			informer := factory.Argoproj().V1alpha1().RolloutPromotions().Informer()
			pc := promotion.NewController(watchNamespace, kubeClient, dynamicClient, rolloutsClient, informer)

			log.WithFields(log.Fields{
				"version":     version.GetVersion(),
				"namespace":   watchNamespace,
				"workers":     workers,
				"healthzPort": healthzPort,
			}).Info("RolloutPromotion controller starting")

			if !leaderElect {
				log.Warn("Leader election is disabled; only one promotion-controller replica is safe")
				return pc.Run(ctx, workers)
			}
			if leaderNamespace == "" {
				return fmt.Errorf("--leader-election-namespace is required when leader election is enabled")
			}
			identity, err := os.Hostname()
			if err != nil {
				return err
			}
			identity = identity + "_" + string(uuid.NewUUID())
			leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
				Lock: &resourcelock.LeaseLock{
					LeaseMeta: metav1.ObjectMeta{Name: leaderLockName, Namespace: leaderNamespace},
					Client:    kubeClient.CoordinationV1(),
					LockConfig: resourcelock.ResourceLockConfig{
						Identity: identity,
					},
				},
				LeaseDuration:   leaderLease,
				RenewDeadline:   leaderRenew,
				RetryPeriod:     leaderRetry,
				ReleaseOnCancel: true,
				Name:            "rollouts-promotion-controller",
				Callbacks: leaderelection.LeaderCallbacks{
					OnStartedLeading: func(leaderCtx context.Context) {
						log.WithField("identity", identity).Info("promotion-controller became leader")
						if err := pc.Run(leaderCtx, workers); err != nil {
							log.WithError(err).Error("promotion-controller stopped with an error")
						}
					},
					OnStoppedLeading: func() {
						log.WithField("identity", identity).Warn("promotion-controller stopped leading")
					},
					OnNewLeader: func(newIdentity string) {
						log.WithField("identity", newIdentity).Info("promotion-controller leader elected")
					},
				},
			})
			return nil
		},
	}

	clientConfig = addKubectlFlagsToCmd(&command)
	command.AddCommand(newTrafficRollbackRequestCommand(clientConfig))
	command.Flags().StringVar(&namespace, "namespace", "", "Namespace to watch. Empty means all namespaces.")
	command.Flags().Int64Var(&resyncPeriod, "resync", controller.DefaultRolloutResyncPeriod, "Informer resync period in seconds.")
	command.Flags().IntVar(&workers, "workers", 2, "Number of RolloutPromotion workers.")
	command.Flags().Float32Var(&qps, "qps", defaults.DefaultQPS, "Maximum QPS to the Kubernetes API server.")
	command.Flags().IntVar(&burst, "burst", defaults.DefaultBurst, "Maximum Kubernetes API burst.")
	command.Flags().StringVar(&logLevel, "loglevel", "info", "Set the logging level. One of: debug|info|warn|error.")
	command.Flags().StringVar(&logFormat, "logformat", "", "Set the logging format. One of: text|json.")
	command.Flags().IntVar(&klogLevel, "kloglevel", 0, "Set the klog logging level.")
	command.Flags().IntVar(&healthzPort, "healthzPort", controller.DefaultHealthzPort+1, "Set the port for the health endpoint. Use 0 to disable.")
	command.Flags().IntVar(&metricsPlaceholder, "metricsPort", 0, "Reserved for future metrics support.")
	command.Flags().BoolVar(&printVersion, "version", false, "Print version.")
	command.Flags().BoolVar(&leaderElect, "leader-elect", true, "Enable leader election for active/passive high availability.")
	command.Flags().StringVar(&leaderNamespace, "leader-election-namespace", defaults.Namespace(), "Namespace containing the leader election Lease.")
	command.Flags().StringVar(&leaderLockName, "leader-election-lock-name", "argo-rollouts-promotion-controller-lock", "Name of the leader election Lease.")
	command.Flags().DurationVar(&leaderLease, "leader-election-lease-duration", controller.DefaultLeaderElectionLeaseDuration, "Leader election lease duration.")
	command.Flags().DurationVar(&leaderRenew, "leader-election-renew-deadline", controller.DefaultLeaderElectionRenewDeadline, "Leader election renew deadline.")
	command.Flags().DurationVar(&leaderRetry, "leader-election-retry-period", controller.DefaultLeaderElectionRetryPeriod, "Leader election retry period.")
	_ = metricsPlaceholder
	return &command
}

func newTrafficRollbackRequestCommand(clientConfig clientcmd.ClientConfig) *cobra.Command {
	var namespace, name, requestID string
	var waitForCompletion bool
	var timeout time.Duration
	command := &cobra.Command{
		Use:   "request-traffic-rollback",
		Short: "Request restoration of the latest controller-owned traffic snapshot",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(namespace) == "" || strings.TrimSpace(name) == "" {
				return fmt.Errorf("--namespace and --name are required")
			}
			if strings.TrimSpace(requestID) == "" {
				requestID = strings.TrimSpace(os.Getenv("POD_UID"))
			}
			if requestID == "" {
				return fmt.Errorf("--request-id or POD_UID is required")
			}
			config, err := clientConfig.ClientConfig()
			if err != nil {
				return err
			}
			client, err := rolloutclient.NewForConfig(config)
			if err != nil {
				return err
			}
			patch, err := json.Marshal(map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": map[string]string{annotations.TrafficRollbackRequestAnnotation: requestID},
				},
			})
			if err != nil {
				return err
			}
			promotions := client.ArgoprojV1alpha1().RolloutPromotions(namespace)
			if _, err = promotions.Patch(cmd.Context(), name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
				return err
			}
			if !waitForCompletion {
				return nil
			}
			return wait.PollUntilContextTimeout(cmd.Context(), time.Second, timeout, true, func(ctx context.Context) (bool, error) {
				request, err := promotions.Get(ctx, name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				if request.Status.LastTrafficRollbackRequest != requestID {
					return false, nil
				}
				if request.Status.Phase == v1alpha1.RolloutPromotionPhaseFailed {
					return false, fmt.Errorf("traffic rollback request %s failed", requestID)
				}
				return request.Status.Phase == v1alpha1.RolloutPromotionPhaseSucceeded, nil
			})
		},
	}
	command.Flags().StringVar(&namespace, "namespace", "", "RolloutPromotion namespace")
	command.Flags().StringVar(&name, "name", "", "RolloutPromotion name")
	command.Flags().StringVar(&requestID, "request-id", "", "Opaque idempotency token; defaults to POD_UID")
	command.Flags().BoolVar(&waitForCompletion, "wait", true, "Wait for the traffic rollback to complete")
	command.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "Maximum wait duration")
	return command
}

func addKubectlFlagsToCmd(cmd *cobra.Command) clientcmd.ClientConfig {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.DefaultClientConfig = &clientcmd.DefaultClientConfig
	overrides := clientcmd.ConfigOverrides{}
	kflags := clientcmd.RecommendedConfigOverrideFlags("")
	cmd.PersistentFlags().StringVar(&loadingRules.ExplicitPath, "kubeconfig", "", "Path to a kube config. Only required if out-of-cluster.")
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

func createFormatter(logFormat string) log.Formatter {
	switch strings.ToLower(logFormat) {
	case jsonFormat:
		return &log.JSONFormatter{}
	case textFormat:
		return &log.TextFormatter{FullTimestamp: true}
	default:
		log.Infof("Unknown format: %s. Using text logformat", logFormat)
		return &log.TextFormatter{FullTimestamp: true}
	}
}

func main() {
	if err := newCommand().Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
