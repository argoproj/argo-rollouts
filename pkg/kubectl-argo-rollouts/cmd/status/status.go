package status

import (
	"context"
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/viewcontroller"
	"github.com/spf13/cobra"
)

const (
	statusExample     = ``
	statusUsage       = ``
	statusUsageCommon = ``
)

type StatusOptions struct {
	Watch bool

	options.ArgoRolloutsOptions
}

// NewCmdStatus returns a new instance of an `rollouts status` command
func NewCmdStatus(o *options.ArgoRolloutsOptions) *cobra.Command {
	statusOptions := StatusOptions{
		ArgoRolloutsOptions: *o,
	}

	var cmd = &cobra.Command{
		Use:          "status ROLLOUT_NAME",
		Short:        "",
		Long:         "",
		Example:      "",
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 1 {
				return o.UsageErr(c)
			}
			name := args[0]
			controller := viewcontroller.NewRolloutViewController(o.Namespace(), name, statusOptions.KubeClientset(), statusOptions.RolloutsClientset())
			ctx := context.Background()
			controller.Start(ctx)

			ri, err := controller.GetRolloutInfo()
			if err != nil {
				return err
			}

			statusOptions.PrintStatus(ri)

			return nil
		},
	}
	cmd.Flags().BoolVarP(&statusOptions.Watch, "watch", "w", false, "Watch the status of the rollout until it's done")
	return cmd
}

func (o *StatusOptions) PrintStatus(roInfo *info.RolloutInfo) {
	fmt.Println(roInfo.Status)
}
