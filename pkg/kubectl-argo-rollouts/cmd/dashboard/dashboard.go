package dashboard

import (
	"context"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/argoproj/argo-rollouts/server"
)

var (
	dashBoardExample = `
	# Start UI dashboard
	%[1]s dashboard

	# Start UI dashboard on a specific port
	%[1]s dashboard --port 8080`
)

func NewCmdDashboard(o *options.ArgoRolloutsOptions) *cobra.Command {
	var rootPath string
	var port int
	var cmd = &cobra.Command{
		Use:     "dashboard",
		Short:   "Start UI dashboard",
		Example: o.Example(dashBoardExample),
		RunE: func(c *cobra.Command, args []string) error {
			namespace := o.Namespace()
			kubeclientset := o.KubeClientset()
			rolloutclientset := o.RolloutsClientset()
			cacheTTLEnv := os.Getenv("KUBECTL_CACHE_TTL")
			var cacheTTL time.Duration
			if cacheTTLEnv != "" {
				parsedTTL, err := time.ParseDuration(cacheTTLEnv)
				if err != nil {
					log.Errorf("Invalid value for KUBECTL_CACHE_TTL: %v", err)
				} else {
					cacheTTL = parsedTTL
					log.Infof("Using cache TTL from environment variable: %s", cacheTTLEnv)
				}
			}

			opts := server.ServerOptions{
				Namespace:         namespace,
				KubeClientset:     kubeclientset,
				RolloutsClientset: rolloutclientset,
				DynamicClientset:  o.DynamicClientset(),
				RootPath:          rootPath,
				CacheTTL:          cacheTTL,
			}

			for {
				ctx := context.Background()
				ctx, cancel := context.WithCancel(ctx)
				argorollouts := server.NewServer(opts)
				argorollouts.Run(ctx, port, true)
				cancel()
			}
		},
	}
	cmd.Flags().StringVar(&rootPath, "root-path", "rollouts", "changes the root path of the dashboard")
	cmd.Flags().IntVarP(&port, "port", "p", 3100, "port to listen on")

	return cmd
}
