package status

import (
	"context"
	"fmt"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/signals"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	completionutil "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/util/completion"
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
			signals.SetupSignalHandler(cancel)
			controller.Start(ctx)

			ri, err := controller.GetRolloutInfo()
			if err != nil {
				return err
			}

			if !statusOptions.Watch {
				if ri.Status == "Healthy" || ri.Status == "Degraded" {
					fmt.Fprintln(o.Out, ri.Status)
				} else {
					fmt.Fprintf(o.Out, "%s - %s\n", ri.Status, ri.Message)
				}
			} else {
				rolloutUpdates := make(chan *rollout.RolloutInfo)
				controller.RegisterCallback(func(roInfo *rollout.RolloutInfo) {
					rolloutUpdates <- roInfo
				})
				go controller.Run(ctx)
				statusOptions.WatchStatus(ctx.Done(), rolloutUpdates)
				defer close(rolloutUpdates)

				// the final rollout info after timeout or reach Healthy or Degraded status
				ri, err = controller.GetRolloutInfo()
				if err != nil {
					return err
				}
			}

			if ri.Status == "Degraded" {
				return fmt.Errorf("The rollout is in a degraded state with message: %s", ri.Message)
			} else if ri.Status != "Healthy" && statusOptions.Watch {
				return fmt.Errorf("Rollout status watch exceeded timeout")
			}

			return nil
		},
		ValidArgsFunction: completionutil.RolloutNameCompletionFunc(o),
	}
	cmd.Flags().BoolVarP(&statusOptions.Watch, "watch", "w", true, "Watch the status of the rollout until it's done")
	cmd.Flags().DurationVarP(&statusOptions.Timeout, "timeout", "t", time.Duration(0), "The length of time to watch before giving up. Non-zero values should contain a corresponding time unit (e.g. 1s, 2m, 3h). Zero means wait forever")
	return cmd
}

func (o *StatusOptions) WatchStatus(stopCh <-chan struct{}, rolloutUpdates <-chan *rollout.RolloutInfo) string {
	timeout := make(chan bool)
	var roInfo *rollout.RolloutInfo
	var prevMessage string

	if o.Timeout != 0 {
		go func() {
			time.Sleep(o.Timeout)
			timeout <- true
		}()
	}

	printStatus := func(roInfo rollout.RolloutInfo) {
		message := roInfo.Status
		if roInfo.Message != "" {
			message = fmt.Sprintf("%s - %s", roInfo.Status, roInfo.Message)
		}
		if message != prevMessage {
			fmt.Fprintln(o.Out, message)
			prevMessage = message
		}
	}

	for {
		select {
		case roInfo = <-rolloutUpdates:
			if roInfo != nil {
				printStatus(*roInfo)
				if roInfo.Status == "Healthy" || roInfo.Status == "Degraded" {
					return roInfo.Status
				}
			}
		case <-stopCh:
			return ""
		case <-timeout:
			return ""
		}
	}
}
