package dashboard

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/argoproj/argo-rollouts/server"
)

var (
	dashBoardExample = `
	# Start UI dashboard
	%[1]s dashboard

	# Start UI dashboard on a specific port
	%[1]s dashboard --port 8080

	# Start UI dashboard with client auth mode (requires bearer token)
	%[1]s dashboard --auth-mode client`
)

func NewCmdDashboard(o *options.ArgoRolloutsOptions) *cobra.Command {
	var rootPath string
	var port int
	var authMode string
	var cmd = &cobra.Command{
		Use:     "dashboard",
		Short:   "Start UI dashboard",
		Example: o.Example(dashBoardExample),
		RunE: func(c *cobra.Command, args []string) error {
			if authMode != server.AuthModeServer && authMode != server.AuthModeClient {
				return fmt.Errorf("invalid auth mode %q: must be %q or %q", authMode, server.AuthModeServer, server.AuthModeClient)
			}

			namespace := o.Namespace()
			kubeclientset := o.KubeClientset()
			rolloutclientset := o.RolloutsClientset()

			opts := server.ServerOptions{
				Namespace:         namespace,
				KubeClientset:     kubeclientset,
				RolloutsClientset: rolloutclientset,
				DynamicClientset:  o.DynamicClientset(),
				RootPath:          rootPath,
				AuthMode:          authMode,
			}

			if authMode == server.AuthModeClient {
				restConfig, err := o.RESTClientGetter.ToRESTConfig()
				if err != nil {
					return fmt.Errorf("failed to get REST config: %w", err)
				}
				opts.RESTConfig = restConfig
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
	cmd.Flags().StringVar(&authMode, "auth-mode", server.AuthModeServer, `authentication mode: "server" (default, uses server credentials) or "client" (requires bearer token from users)`)

	return cmd
}
