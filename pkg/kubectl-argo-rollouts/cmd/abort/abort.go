package abort

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/typed/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	completionutil "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/util/completion"
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
				ro, err := AbortRollout(rolloutIf, name)
				if err != nil {
					return err
				}
				fmt.Fprintf(o.Out, "rollout '%s' aborted\n", ro.Name)
			}
			return nil
		},
		ValidArgsFunction: completionutil.RolloutNameCompletionFunc(o),
	}
	return cmd
}

// AbortRollout aborts a rollout
func AbortRollout(rolloutIf clientset.RolloutInterface, name string) (*v1alpha1.Rollout, error) {
	ctx := context.TODO()
	// attempt using status subresource, first
	ro, err := rolloutIf.Patch(ctx, name, types.MergePatchType, []byte(abortPatch), metav1.PatchOptions{}, "status")
	if err != nil && k8serrors.IsNotFound(err) {
		ro, err = rolloutIf.Patch(ctx, name, types.MergePatchType, []byte(abortPatch), metav1.PatchOptions{})
	}
	return ro, err
}
