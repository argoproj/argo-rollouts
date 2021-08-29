package set

import (
	"github.com/spf13/cobra"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
)

const (
	setExample = `
  # Set rollout image
  %[1]s set image my-rollout demo=argoproj/rollouts-demo:yellow`
)

// NewCmdSet returns a new instance of an `rollouts set` command
func NewCmdSet(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:          "set COMMAND",
		Short:        "Update various values on resources",
		Long:         "This command consists of multiple subcommands which can be used to update rollout resources.",
		Example:      o.Example(setExample),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			return o.UsageErr(c)
		},
	}
	cmd.AddCommand(NewCmdSetImage(o))
	return cmd
}
