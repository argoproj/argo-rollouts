package dashboard

import (
	"context"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/argoproj/argo-rollouts/server"
	"github.com/spf13/cobra"
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

			opts := server.ServerOptions{
				Namespace:         namespace,
				KubeClientset:     kubeclientset,
				RolloutsClientset: rolloutclientset,
				DynamicClientset:  o.DynamicClientset(),
				RootPath:          rootPath,
        DisableAuth:       true,
			}

			for {
				ctx := context.Background()
				ctx, cancel := context.WithCancel(ctx)
				argorollouts := server.NewServer(ctx, opts)
				argorollouts.Run(ctx, port, true)
				cancel()
			}
		},
	}
	cmd.Flags().StringVar(&rootPath, "root-path", "rollouts", "changes the root path of the dashboard")
	cmd.Flags().IntVarP(&port, "port", "p", 3100, "port to listen on")

	return cmd
}
