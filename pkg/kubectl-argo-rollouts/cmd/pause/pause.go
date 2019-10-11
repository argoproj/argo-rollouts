package pause

import (
	"fmt"

	"github.com/spf13/cobra"
	types "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
)

const (
	example = `
  # Pause a rollout
  %[1]s pause guestbook
`
)

// NewCmdPause returns a new instance of an `rollouts pause` command
func NewCmdPause(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:          "pause ROLLOUT",
		Short:        "Pause a rollout",
		Example:      o.Example(example),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return o.UsageErr(c)
			}
			ns := o.Namespace()
			rolloutIf := o.RolloutsClientset().ArgoprojV1alpha1().Rollouts(ns)
			for _, name := range args {
				ro, err := rolloutIf.Patch(name, types.MergePatchType, []byte(`{"spec":{"paused":true}}`))
				if err != nil {
					return err
				}
				fmt.Fprintf(o.Out, "rollout '%s' paused\n", ro.Name)
			}
			return nil
		},
	}
	o.AddKubectlFlags(cmd)
	return cmd
}
