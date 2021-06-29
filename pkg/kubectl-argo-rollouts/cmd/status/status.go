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
	%[1]s status --timeout 60s guestbook
	`
)

type StatusOptions struct {
	Watch   bool
	Timeout time.Duration

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
				go controller.Run(ctx)
				statusOptions.WatchStatus(ctx.Done(), rolloutUpdates)

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
	cmd.Flags().DurationVarP(&statusOptions.Timeout, "timeout", "t", time.Duration(0), "The length of time to watch before giving up. Non-zero values should contain a corresponding time unit (e.g. 1s, 2m, 3h). Zero means wait forever")
	return cmd
}

func (o *StatusOptions) WatchStatus(stopCh <-chan struct{}, rolloutUpdates chan *rollout.RolloutInfo) string {
	timeout := make(chan bool)
	var roInfo *rollout.RolloutInfo
	var preventFlicker time.Time

	if o.Timeout != 0 {
		go func() {
			time.Sleep(o.Timeout)
			timeout <- true
		}()
	}

	for {
		select {
		case roInfo = <-rolloutUpdates:
			if roInfo != nil && roInfo.Status == "Healthy" || roInfo.Status == "Degraded" {
				fmt.Fprintln(o.Out, roInfo.Status)
				return roInfo.Status
			}
			if roInfo != nil && time.Now().After(preventFlicker.Add(200*time.Millisecond)) {
				fmt.Fprintf(o.Out, "%s - %s\n", roInfo.Status, roInfo.Message)
				preventFlicker = time.Now()
			}
		case <-stopCh:
			return ""
		case <-timeout:
			return ""
		}
	}
}
