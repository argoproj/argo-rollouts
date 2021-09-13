package cmd

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/abort"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/completion"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/create"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/dashboard"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/get"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/lint"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/list"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/pause"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/promote"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/restart"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/retry"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/set"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/status"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/terminate"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/undo"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/version"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/argoproj/argo-rollouts/utils/record"
	notificationcmd "github.com/argoproj/notifications-engine/pkg/cmd"

	"github.com/spf13/cobra"
)

const (
	example = `
  # Get guestbook rollout and watch progress
  %[1]s get rollout guestbook -w

  # Pause the guestbook rollout
  %[1]s pause guestbook

  # Promote the guestbook rollout
  %[1]s promote guestbook

  # Abort the guestbook rollout
  %[1]s abort guestbook

  # Retry the guestbook rollout
  %[1]s retry guestbook`
)

// NewCmdArgoRollouts returns new instance of rollouts command.
func NewCmdArgoRollouts(o *options.ArgoRolloutsOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "kubectl-argo-rollouts COMMAND",
		Short:             "Manage argo rollouts",
		Long:              "This command consists of multiple subcommands which can be used to manage Argo Rollouts.",
		Example:           o.Example(example),
		SilenceUsage:      true,
		PersistentPreRunE: o.PersistentPreRunE,
		RunE: func(c *cobra.Command, args []string) error {
			return o.UsageErr(c)
		},
	}
	o.AddKubectlFlags(cmd)
	cmd.AddCommand(create.NewCmdCreate(o))
	cmd.AddCommand(get.NewCmdGet(o))
	cmd.AddCommand(lint.NewCmdLint(o))
	cmd.AddCommand(list.NewCmdList(o))
	cmd.AddCommand(pause.NewCmdPause(o))
	cmd.AddCommand(promote.NewCmdPromote(o))
	cmd.AddCommand(restart.NewCmdRestart(o))
	cmd.AddCommand(version.NewCmdVersion(o))
	cmd.AddCommand(abort.NewCmdAbort(o))
	cmd.AddCommand(retry.NewCmdRetry(o))
	cmd.AddCommand(terminate.NewCmdTerminate(o))
	cmd.AddCommand(set.NewCmdSet(o))
	cmd.AddCommand(undo.NewCmdUndo(o))
	cmd.AddCommand(dashboard.NewCmdDashboard(o))
	cmd.AddCommand(status.NewCmdStatus(o))
	cmd.AddCommand(notificationcmd.NewToolsCommand("notifications", "kubectl argo rollouts notifications", v1alpha1.RolloutGVR, record.NewAPIFactorySettings()))
	cmd.AddCommand(completion.NewCmdCompletion(o))

	return cmd
}
