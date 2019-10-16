package cmd

import (
	"github.com/spf13/cobra"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/list"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/pause"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/resume"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/version"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
)

const (
	example = `
  # Pause the guestbook rollout
  %[1]s pause guestbook

  # Resume the guestbook rollout
  %[1]s resume guestbook
`
)

func NewCmdArgoRollouts(o *options.ArgoRolloutsOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "kubectl-argo-rollouts COMMAND",
		Short:             "Manage argo rollouts",
		Example:           o.Example(example),
		SilenceUsage:      true,
		PersistentPreRunE: o.PersistentPreRunE,
		RunE: func(c *cobra.Command, args []string) error {
			return o.UsageErr(c)
		},
	}
	cmd.AddCommand(list.NewCmdList(o))
	cmd.AddCommand(pause.NewCmdPause(o))
	cmd.AddCommand(resume.NewCmdResume(o))
	cmd.AddCommand(version.NewCmdVersion(o))
	return cmd
}
