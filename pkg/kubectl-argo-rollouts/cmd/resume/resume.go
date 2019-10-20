package resume

import (
	"fmt"

	"github.com/spf13/cobra"
	types "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
)

const (
	example = `
  # Resume a rollout
  %[1]s resume guestbook
`
)

// NewCmdResume returns a new instance of an `rollouts resume` command
func NewCmdResume(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:          "resume ROLLOUT",
		Short:        "Resume a rollout",
		Example:      o.Example(example),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return o.UsageErr(c)
			}
			rolloutIf := o.RolloutsClientset().ArgoprojV1alpha1().Rollouts(o.Namespace())
			for _, name := range args {
				ro, err := rolloutIf.Patch(name, types.MergePatchType, []byte(`{"status":{"pauseConditions":null}}`))
				if err != nil {
					return err
				}
				fmt.Fprintf(o.Out, "rollout '%s' resumed\n", ro.Name)
			}
			return nil
		},
	}
	o.AddKubectlFlags(cmd)
	return cmd
}
