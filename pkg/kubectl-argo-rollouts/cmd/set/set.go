package set

import (
	"github.com/spf13/cobra"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
)

const (
	example = `
  # Set rollout image
  %[1]s set image my-rollout argoproj/rollouts-demo:yellow
`
)

// NewCmdSet returns a new instance of an `rollouts set` command
func NewCmdSet(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:          "set COMMAND",
		Short:        "Update various values on resources",
		Example:      o.Example(example),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			return o.UsageErr(c)
		},
	}
	o.AddKubectlFlags(cmd)
	cmd.AddCommand(NewCmdSetImage(o))
	return cmd
}
