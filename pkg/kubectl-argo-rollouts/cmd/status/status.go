package status

import (
	"context"
	"fmt"
	"time"

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
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			controller.Start(ctx)

			ri, err := controller.GetRolloutInfo()
			if err != nil {
				return err
			}

			if !statusOptions.Watch {
				fmt.Println(ri.Status)
			} else {
				rolloutUpdates := make(chan *info.RolloutInfo)
				defer close(rolloutUpdates)
				controller.RegisterCallback(func(roInfo *info.RolloutInfo) {
					rolloutUpdates <- roInfo
				})
				go statusOptions.WatchStatus(ctx.Done(), cancel, rolloutUpdates)
				controller.Run(ctx)
			}

			return nil
		},
	}
	cmd.Flags().BoolVarP(&statusOptions.Watch, "watch", "w", false, "Watch the status of the rollout until it's done")
	return cmd
}

func (o *StatusOptions) WatchStatus(stopCh <-chan struct{}, cancelFunc context.CancelFunc, rolloutUpdates chan *info.RolloutInfo) {
	ticker := time.NewTicker(time.Second)
	var roInfo *info.RolloutInfo
	var preventFlicker time.Time

	for {
		select {
		case roInfo = <-rolloutUpdates:
		case <-ticker.C:
		case <-stopCh:
			return
		}
		if roInfo != nil && time.Now().After(preventFlicker.Add(200*time.Millisecond)) {
			fmt.Printf("%s - %s\n", roInfo.Status, roInfo.Message)

			if roInfo.Status == "Healthy" || roInfo.Status == "Degraded" {
				cancelFunc()
				return
			}

			preventFlicker = time.Now()
		}
	}
}
