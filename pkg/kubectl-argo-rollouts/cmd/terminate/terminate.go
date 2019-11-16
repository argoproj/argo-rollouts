package terminate

import (
	"fmt"

	"github.com/spf13/cobra"
	types "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
)

const (
	terminatePatch = `{"spec":{"terminate":true}}`
)

// NewCmdTerminate returns a new instance of an `argo rollouts terminate` command
func NewCmdTerminate(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "terminate <analysisrun|experiment> RESOURCE",
		Short: "Terminate an AalysisRun or experiment",
		Example: o.Example(`
  # Terminate an analysisRun
  %[1]s terminate analysisrun ANALYSISRUN
  # Terminate a failed experiment
  %[1]s terminate experiment EXPERIMENT
`),
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
		Use:     "analysisrun ANALYSISRUN",
		Aliases: []string{"ar", "analysisruns"},
		Short:   "Terminate an AnalysisRun",
		Example: o.Example(`
  # Terminate an AnalysisRun
  %[1]s terminate ANALYSISRUN
`),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return o.UsageErr(c)
			}
			ns := o.Namespace()
			analysisRunIf := o.RolloutsClientset().ArgoprojV1alpha1().AnalysisRuns(ns)
			for _, name := range args {
				ro, err := analysisRunIf.Patch(name, types.MergePatchType, []byte(terminatePatch))
				if err != nil {
					return err
				}
				fmt.Fprintf(o.Out, "analysisRun '%s' terminated\n", ro.Name)
			}
			return nil
		},
	}
	o.AddKubectlFlags(cmd)
	return cmd
}

// NewCmdTerminateExperiment returns a new instance of an `argo rollouts terminate experiment` command
func NewCmdTerminateExperiment(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:     "experiment EXPERIMENT",
		Aliases: []string{"exp", "experiments"},
		Short:   "Terminate an experiment",
		Example: o.Example(`
  # Terminate an experiment
  %[1]s terminate experiment EXPERIMENT
`),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return o.UsageErr(c)
			}
			ns := o.Namespace()
			experimentIf := o.RolloutsClientset().ArgoprojV1alpha1().Experiments(ns)
			for _, name := range args {
				ro, err := experimentIf.Patch(name, types.MergePatchType, []byte(terminatePatch))
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
