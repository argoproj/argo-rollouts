package get

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/juju/ansiterm"
	"github.com/spf13/cobra"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/viewcontroller"
)

// NewCmdGetExperiment returns a new instance of an `rollouts get experiment` command
func NewCmdGetExperiment(o *options.ArgoRolloutsOptions) *cobra.Command {
	getOptions := GetOptions{
		ArgoRolloutsOptions: *o,
	}

	var cmd = &cobra.Command{
		Use:     "experiment EXPERIMENT",
		Aliases: []string{"exp"},
		Short:   "Get details about an Experiment",
		Example: o.Example(`
  # Get an experiment
  %[1]s get experiment EXPERIMENT
`),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 1 {
				return o.UsageErr(c)
			}
			name := args[0]
			controller := viewcontroller.NewExperimentViewController(o.Namespace(), name, getOptions.KubeClientset(), getOptions.RolloutsClientset())
			ctx := context.Background()
			controller.Start(ctx)

			expInfo, err := controller.GetExperimentInfo()
			if err != nil {
				return err
			}
			if !getOptions.watch {
				getOptions.PrintExperiment(expInfo)
			} else {
				expUpdates := make(chan *info.ExperimentInfo)
				controller.RegisterCallback(func(expInfo *info.ExperimentInfo) {
					expUpdates <- expInfo
				})
				go getOptions.WatchExperiment(ctx.Done(), expUpdates)
				controller.Run(ctx)
				close(expUpdates)
			}
			return nil
		},
	}
	o.AddKubectlFlags(cmd)
	cmd.Flags().BoolVarP(&getOptions.watch, "watch", "w", false, "Watch live updates to the rollout")
	cmd.Flags().BoolVar(&getOptions.noColor, "no-color", false, "Do not colorize output")
	return cmd
}

func (o *GetOptions) WatchExperiment(stopCh <-chan struct{}, expUpdates chan *info.ExperimentInfo) {
	ticker := time.NewTicker(time.Second)
	var currExpInfo *info.ExperimentInfo
	// preventFlicker is used to rate-limit the updates we print to the terminal when updates occur
	// so rapidly that it causes the terminal to flicker
	var preventFlicker time.Time

	for {
		select {
		case expInfo := <-expUpdates:
			currExpInfo = expInfo
		case <-ticker.C:
		case <-stopCh:
			return
		}
		if currExpInfo != nil && time.Now().After(preventFlicker.Add(200*time.Millisecond)) {
			o.Clear()
			o.PrintExperiment(currExpInfo)
			preventFlicker = time.Now()
		}
	}
}

func (o *GetOptions) PrintExperiment(exInfo *info.ExperimentInfo) {
	fmt.Fprintf(o.Out, tableFormat, "Name:", exInfo.Name)
	fmt.Fprintf(o.Out, tableFormat, "Namespace:", exInfo.Namespace)
	fmt.Fprintf(o.Out, tableFormat, "Status:", o.colorize(exInfo.Icon)+" "+exInfo.Status)
	if exInfo.Message != "" {
		fmt.Fprintf(o.Out, tableFormat, "Message:", exInfo.Message)
	}
	images := exInfo.Images()
	if len(images) > 0 {
		fmt.Fprintf(o.Out, tableFormat, "Images:", o.formatImage(images[0]))
		for i := 1; i < len(images); i++ {
			fmt.Fprintf(o.Out, tableFormat, "", o.formatImage(images[i]))
		}
	}

	fmt.Fprintf(o.Out, "\n")
	o.PrintExperimentTree(exInfo)
}

func (o *GetOptions) PrintExperimentTree(exInfo *info.ExperimentInfo) {
	w := ansiterm.NewTabWriter(o.Out, 0, 0, 2, ' ', 0)
	o.PrintHeader(w)
	o.PrintExperimentInfo(w, *exInfo, "", "")
	_ = w.Flush()
}

func (o *GetOptions) PrintExperimentInfo(w io.Writer, expInfo info.ExperimentInfo, prefix string, subpfx string) {
	name := expInfo.Name
	infoCols := []string{}
	total := len(expInfo.ReplicaSets) + len(expInfo.AnalysisRuns)
	curr := 0
	if expInfo.Status == "Running" {
		name = o.ansiFormat(name, FgBlue)
	}
	fmt.Fprintf(w, "%s%s %s\t%s\t%s %s\t%s\t%v\n", prefix, IconExperiment, name, "Experiment", o.colorize(expInfo.Icon), expInfo.Status, expInfo.Age(), strings.Join(infoCols, ","))

	for _, rsInfo := range expInfo.ReplicaSets {
		isLast := curr == total-1
		curr++
		var childPrefix, childSubpfx string
		if !isLast {
			childPrefix = subpfx + "├──"
			childSubpfx = subpfx + "│  "
		} else {
			childPrefix = subpfx + "└──"
			childSubpfx = subpfx + "   "
		}
		o.PrintReplicaSetInfo(w, rsInfo, childPrefix, childSubpfx)
	}
	for _, arInfo := range expInfo.AnalysisRuns {
		fmt.Fprintf(w, subpfx)
		isLast := curr == total-1
		curr++
		var arPrefix string
		if !isLast {
			arPrefix = "├──"
		} else {
			arPrefix = "└──"
		}
		o.PrintAnalysisRunInfo(w, arInfo, arPrefix, "")
	}
}
