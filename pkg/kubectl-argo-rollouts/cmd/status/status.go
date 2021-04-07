package status

import (
	"context"
	"fmt"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/viewcontroller"
	"github.com/spf13/cobra"
)

const (
	statusLong = `Watch rollout until it finishes or the timeout is exceeded. Returns success if
the rollout is healthy upon completion and an error otherwise.`
	statusExample = `
	# Watch the rollout until it succeeds
	%[1]s status guestbook

	# Watch the rollout until it succeeds, fail if it takes more than 60 seconds
	%[1]s status --timeout 60 guestbook
	`
)

type StatusOptions struct {
	Watch   bool
	Timeout int64

	options.ArgoRolloutsOptions
}

// NewCmdStatus returns a new instance of a `rollouts status` command
func NewCmdStatus(o *options.ArgoRolloutsOptions) *cobra.Command {
	statusOptions := StatusOptions{
		ArgoRolloutsOptions: *o,
	}

	var cmd = &cobra.Command{
		Use:          "status ROLLOUT_NAME",
		Short:        "Show the status of a rollout",
		Long:         statusLong,
		Example:      o.Example(statusExample),
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
				fmt.Fprintln(o.Out, ri.Status)
			} else {
				rolloutUpdates := make(chan *rollout.RolloutInfo)
				defer close(rolloutUpdates)
				controller.RegisterCallback(func(roInfo *rollout.RolloutInfo) {
					rolloutUpdates <- roInfo
				})
				go statusOptions.WatchStatus(ctx.Done(), cancel, statusOptions.Timeout, rolloutUpdates)
				controller.Run(ctx)

				finalRi, err := controller.GetRolloutInfo()
				if err != nil {
					return err
				}

				if finalRi.Status == "Degraded" {
					return fmt.Errorf("The rollout is in a degraded state with message: %s", finalRi.Message)
				} else if finalRi.Status != "Healthy" {
					return fmt.Errorf("Rollout progress exceeded timeout")
				}
			}

			return nil
		},
	}
	cmd.Flags().BoolVarP(&statusOptions.Watch, "watch", "w", true, "Watch the status of the rollout until it's done")
	cmd.Flags().Int64VarP(&statusOptions.Timeout, "timeout", "t", 0, "The length of time in seconds to watch before giving up, zero means wait forever")
	return cmd
}

func (o *StatusOptions) WatchStatus(stopCh <-chan struct{}, cancelFunc context.CancelFunc, timeoutSeconds int64, rolloutUpdates chan *rollout.RolloutInfo) {
	timeout := make(chan bool)
	var roInfo *rollout.RolloutInfo
	var preventFlicker time.Time

	if timeoutSeconds != 0 {
		go func() {
			time.Sleep(time.Duration(timeoutSeconds) * time.Second)
			timeout <- true
		}()
	}

	for {
		select {
		case roInfo = <-rolloutUpdates:
			if roInfo != nil && roInfo.Status == "Healthy" || roInfo.Status == "Degraded" {
				fmt.Fprintln(o.Out, roInfo.Status)
				cancelFunc()
				return
			}
			if roInfo != nil && time.Now().After(preventFlicker.Add(200*time.Millisecond)) {
				fmt.Fprintf(o.Out, "%s - %s\n", roInfo.Status, roInfo.Message)
				preventFlicker = time.Now()
			}
		case <-stopCh:
			return
		case <-timeout:
			cancelFunc()
			return
		}
	}
}
