package pause

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	completionutil "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/util/completion"
)

const (
	pauseExample = `
  # Pause a rollout
  %[1]s pause guestbook`
)

// NewCmdPause returns a new instance of an `rollouts pause` command
func NewCmdPause(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:          "pause ROLLOUT_NAME",
		Short:        "Pause a rollout",
		Long:         "Set the rollout paused state to 'true'",
		Example:      o.Example(pauseExample),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return o.UsageErr(c)
			}
			ns := o.Namespace()
			rolloutIf := o.RolloutsClientset().ArgoprojV1alpha1().Rollouts(ns)
			for _, name := range args {
				ro, err := rolloutIf.Patch(context.TODO(), name, types.MergePatchType, []byte(`{"spec":{"paused":true}}`), metav1.PatchOptions{})
				if err != nil {
					return err
				}
				fmt.Fprintf(o.Out, "rollout '%s' paused\n", ro.Name)
			}
			return nil
		},
		ValidArgsFunction: completionutil.RolloutNameCompletionFunc(o),
	}
	return cmd
}
