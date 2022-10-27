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
		// list Rollouts names
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

// ExperimentNameCompletionFunc Returns a completion function that completes as a first argument
// the Experiments names that match the toComplete prefix.
func ExperimentNameCompletionFunc(o *options.ArgoRolloutsOptions) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(c *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		// list Experiments names
		ctx := c.Context()
		opts := metav1.ListOptions{}
		expIf := o.RolloutsClientset().ArgoprojV1alpha1().Experiments(o.Namespace())
		expList, err := expIf.List(ctx, opts)
		if err != nil {
			return []string{}, cobra.ShellCompDirectiveError
		}

		var expNames []string
		for _, exp := range expList.Items {
			if strings.HasPrefix(exp.Name, toComplete) {
				expNames = append(expNames, exp.Name)
			}
		}

		return expNames, cobra.ShellCompDirectiveNoFileComp
	}
}

// AnalysisRunNameCompletionFunc Returns a completion function that completes as a first argument
// the AnalysisRuns names that match the toComplete prefix.
func AnalysisRunNameCompletionFunc(o *options.ArgoRolloutsOptions) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(c *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		// list AnalysisRuns names
		ctx := c.Context()
		opts := metav1.ListOptions{}
		arIf := o.RolloutsClientset().ArgoprojV1alpha1().AnalysisRuns(o.Namespace())
		arList, err := arIf.List(ctx, opts)
		if err != nil {
			return []string{}, cobra.ShellCompDirectiveError
		}

		var arNames []string
		for _, ar := range arList.Items {
			if strings.HasPrefix(ar.Name, toComplete) {
				arNames = append(arNames, ar.Name)
			}
		}

		return arNames, cobra.ShellCompDirectiveNoFileComp
	}
}
