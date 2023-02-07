package retry

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
	retryRolloutPatch    = `{"status":{"abort":false}}`
	retryExperimentPatch = `{"status":null}`
)

const (
	retryExample = `
	# Retry an aborted rollout
	%[1]s retry rollout guestbook

	# Retry a failed experiment
	%[1]s retry experiment my-experiment`

	retryRolloutExample = `
	# Retry an aborted rollout
	%[1]s retry rollout guestbook`

	retryExperimentExample = `
	# Retry an experiment
	%[1]s retry experiment my-experiment`
)

// NewCmdRetry returns a new instance of an `argo rollouts retry` command
func NewCmdRetry(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:          "retry <rollout|experiment> RESOURCE_NAME",
		Short:        "Retry a rollout or experiment",
		Long:         "This command consists of multiple subcommands which can be used to restart an aborted rollout or a failed experiment.",
		Example:      o.Example(retryExample),
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
		Use:          "rollout ROLLOUT_NAME",
		Aliases:      []string{"ro", "rollouts"},
		Short:        "Retry an aborted rollout",
		Example:      o.Example(retryRolloutExample),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return o.UsageErr(c)
			}
			ns := o.Namespace()
			rolloutIf := o.RolloutsClientset().ArgoprojV1alpha1().Rollouts(ns)
			for _, name := range args {
				ro, err := RetryRollout(rolloutIf, name)
				if err != nil {
					return err
				}
				fmt.Fprintf(o.Out, "rollout '%s' retried\n", ro.Name)
			}
			return nil
		},
		ValidArgsFunction: completionutil.RolloutNameCompletionFunc(o),
	}
	return cmd
}

// RetryRollout retries a rollout after it's been aborted
func RetryRollout(rolloutIf clientset.RolloutInterface, name string) (*v1alpha1.Rollout, error) {
	ctx := context.TODO()
	// attempt using status subresource, first
	ro, err := rolloutIf.Patch(ctx, name, types.MergePatchType, []byte(retryRolloutPatch), metav1.PatchOptions{}, "status")
	if err != nil && k8serrors.IsNotFound(err) {
		ro, err = rolloutIf.Patch(ctx, name, types.MergePatchType, []byte(retryRolloutPatch), metav1.PatchOptions{})
	}
	return ro, err
}

// NewCmdRetryExperiment returns a new instance of an `argo rollouts retry experiment` command
func NewCmdRetryExperiment(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:          "experiment EXPERIMENT_NAME",
		Aliases:      []string{"exp", "experiments"},
		Short:        "Retry an experiment",
		Long:         "Retry a failed experiment.",
		Example:      o.Example(retryExperimentExample),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return o.UsageErr(c)
			}
			ns := o.Namespace()
			experimentIf := o.RolloutsClientset().ArgoprojV1alpha1().Experiments(ns)
			for _, name := range args {
				ex, err := RetryExperiment(experimentIf, name)
				if err != nil {
					return err
				}
				fmt.Fprintf(o.Out, "experiment '%s' retried\n", ex.Name)
			}
			return nil
		},
		ValidArgsFunction: completionutil.ExperimentNameCompletionFunc(o),
	}
	return cmd
}

// RetryExperiment retries an experiment after it's been aborted
func RetryExperiment(experimentIf clientset.ExperimentInterface, name string) (*v1alpha1.Experiment, error) {
	ctx := context.TODO()
	return experimentIf.Patch(ctx, name, types.MergePatchType, []byte(retryExperimentPatch), metav1.PatchOptions{})
}
