package completion

import (
	"strings"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
)

// RolloutNameCompletionFunc Returns a completion function that completes as a first argument
// the Rollouts names that match the toComplete prefix.
func RolloutNameCompletionFunc(o *options.ArgoRolloutsOptions) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(c *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		// list rollouts names
		ctx := c.Context()
		opts := metav1.ListOptions{}
		rolloutIf := o.RolloutsClientset().ArgoprojV1alpha1().Rollouts(o.Namespace())
		rolloutList, err := rolloutIf.List(ctx, opts)
		if err != nil {
			return []string{}, cobra.ShellCompDirectiveError
		}

		var rolloutNames []string
		for _, ro := range rolloutList.Items {
			if strings.HasPrefix(ro.Name, toComplete) {
				rolloutNames = append(rolloutNames, ro.Name)
			}
		}

		return rolloutNames, cobra.ShellCompDirectiveNoFileComp
	}
}
