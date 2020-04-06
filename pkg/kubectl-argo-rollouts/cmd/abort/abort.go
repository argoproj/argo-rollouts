package abort

import (
	"fmt"

	"github.com/spf13/cobra"
	types "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
)

const (
	abortExample = `
  # Abort a rollout
  %[1]s abort guestbook`

	abortUsage = `This command stops progressing the current rollout and reverts all steps. The previous ReplicaSet will be active.

Note the 'spec.template' still represents the new rollout version. If the Rollout leaves the aborted state, it will try to go to the new version. 
Updating the 'spec.template' back to the previous version will fully revert the rollout.`
)

const (
	abortPatch = `{"status":{"abort":true}}`
)

// NewCmdAbort returns a new instance of an `rollouts abort` command
func NewCmdAbort(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:          "abort ROLLOUT_NAME",
		Short:        "Abort a rollout",
		Long:         abortUsage,
		Example:      o.Example(abortExample),
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
	return cmd
}
