package terminate

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
	terminatePatch = `{"spec":{"terminate":true}}`
)

const (
	terminateExample = `
	# Terminate an analysisRun
	%[1]s terminate analysisrun guestbook-877894d5b-4-success-rate.1

	# Terminate a failed experiment
	%[1]s terminate experiment my-experiment`

	terminateAnalysisRunExample = `
	# Terminate an AnalysisRun
	%[1]s terminate analysis guestbook-877894d5b-4-success-rate.1`

	terminateExperimentExample = `
	# Terminate an experiment
	%[1]s terminate experiment my-experiment`
)

// NewCmdTerminate returns a new instance of an `argo rollouts terminate` command
func NewCmdTerminate(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:          "terminate <analysisrun|experiment> RESOURCE_NAME",
		Short:        "Terminate an AnalysisRun or Experiment",
		Long:         "This command consists of multiple subcommands which can be used to terminate an AnalysisRun or Experiment that is in progress.",
		Example:      o.Example(terminateExample),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			return o.UsageErr(c)
		},
	}
	cmd.AddCommand(NewCmdTerminateAnalysisRun(o))
	cmd.AddCommand(NewCmdTerminateExperiment(o))
	return cmd
}

// NewCmdTerminateAnalysisRun returns a new instance of an `argo rollouts terminate analysisRun` command
func NewCmdTerminateAnalysisRun(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:          "analysisrun ANALYSISRUN_NAME",
		Aliases:      []string{"ar", "analysisruns"},
		Short:        "Terminate an AnalysisRun",
		Long:         "This command terminates an AnalysisRun.",
		Example:      o.Example(terminateAnalysisRunExample),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return o.UsageErr(c)
			}
			ns := o.Namespace()
			analysisRunIf := o.RolloutsClientset().ArgoprojV1alpha1().AnalysisRuns(ns)
			for _, name := range args {
				ro, err := analysisRunIf.Patch(context.TODO(), name, types.MergePatchType, []byte(terminatePatch), metav1.PatchOptions{})
				if err != nil {
					return err
				}
				fmt.Fprintf(o.Out, "analysisRun '%s' terminated\n", ro.Name)
			}
			return nil
		},
		ValidArgsFunction: completionutil.AnalysisRunNameCompletionFunc(o),
	}
	return cmd
}

// NewCmdTerminateExperiment returns a new instance of an `argo rollouts terminate experiment` command
func NewCmdTerminateExperiment(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:          "experiment EXPERIMENT_NAME",
		Aliases:      []string{"exp", "experiments"},
		Short:        "Terminate an experiment",
		Long:         "This command terminates an Experiment.",
		Example:      o.Example(terminateExperimentExample),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return o.UsageErr(c)
			}
			ns := o.Namespace()
			experimentIf := o.RolloutsClientset().ArgoprojV1alpha1().Experiments(ns)
			for _, name := range args {
				ro, err := experimentIf.Patch(context.TODO(), name, types.MergePatchType, []byte(terminatePatch), metav1.PatchOptions{})
				if err != nil {
					return err
				}
				fmt.Fprintf(o.Out, "experiment '%s' retried\n", ro.Name)
			}
			return nil
		},
		ValidArgsFunction: completionutil.ExperimentNameCompletionFunc(o),
	}
	return cmd
}
