package abort

import (
	"fmt"

	"github.com/spf13/cobra"
	types "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
)

const (
	example = `
  # Abort a rollout
  %[1]s abort guestbook
`
)

const (
	abortPatch = `{"status":{"abort":true}}`
)

// NewCmdAbort returns a new instance of an `rollouts abort` command
func NewCmdAbort(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:          "abort ROLLOUT",
		Short:        "Abort a rollout",
		Example:      o.Example(example),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return o.UsageErr(c)
			}
			ns := o.Namespace()
			rolloutIf := o.RolloutsClientset().ArgoprojV1alpha1().Rollouts(ns)
			for _, name := range args {
				ro, err := rolloutIf.Patch(name, types.MergePatchType, []byte(abortPatch))
				if err != nil {
					return err
				}
				fmt.Fprintf(o.Out, "rollout '%s' aborted\n", ro.Name)
			}
			return nil
		},
	}
	o.AddKubectlFlags(cmd)
	return cmd
}
