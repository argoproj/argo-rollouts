package retry

import (
	"fmt"

	"github.com/spf13/cobra"
	types "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
)

const (
	retryRolloutPatch    = `{"status":{"abort":false}}`
	retryExperimentPatch = `{"status":null}`
)

// NewCmdRetry returns a new instance of an `argo rollouts retry` command
func NewCmdRetry(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "retry <rolllout|experiment> RESOURCE",
		Short: "Retry a rollout or experiment",
		Example: o.Example(`
  # Retry an aborted rollout
  %[1]s get rollout ROLLOUT
  # Retry a failed experiment
  %[1]s retry experiment EXPERIMENT
`),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			return o.UsageErr(c)
		},
	}
	cmd.AddCommand(NewCmdRetryRollout(o))
	cmd.AddCommand(NewCmdRetryExperiment(o))
	return cmd
}

// NewCmdRetryRollout returns a new instance of an `argo rollouts retry rollout` command
func NewCmdRetryRollout(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:     "rollout ROLLOUT",
		Aliases: []string{"ro"},
		Short:   "Retry an aborted rollout",
		Example: o.Example(`
  # Retry an aborted rollout
  %[1]s retry rollout ROLLOUT
`),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return o.UsageErr(c)
			}
			ns := o.Namespace()
			rolloutIf := o.RolloutsClientset().ArgoprojV1alpha1().Rollouts(ns)
			for _, name := range args {
				ro, err := rolloutIf.Patch(name, types.MergePatchType, []byte(retryRolloutPatch))
				if err != nil {
					return err
				}
				fmt.Fprintf(o.Out, "rollout '%s' retried\n", ro.Name)
			}
			return nil
		},
	}
	o.AddKubectlFlags(cmd)
	return cmd
}

// NewCmdRetryExperiment returns a new instance of an `argo rollouts retry experiment` command
func NewCmdRetryExperiment(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:     "experiment EXPERIMENT",
		Aliases: []string{"exp"},
		Short:   "Retry an experiment",
		Example: o.Example(`
  # Retry an experiment
  %[1]s retry experiment EXPERIMENT
`),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return o.UsageErr(c)
			}
			ns := o.Namespace()
			experimentIf := o.RolloutsClientset().ArgoprojV1alpha1().Experiments(ns)
			for _, name := range args {
				ro, err := experimentIf.Patch(name, types.MergePatchType, []byte(retryExperimentPatch))
				if err != nil {
					return err
				}
				fmt.Fprintf(o.Out, "experiment '%s' retried\n", ro.Name)
			}
			return nil
		},
	}
	o.AddKubectlFlags(cmd)
	return cmd
}
