package get

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/juju/ansiterm"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/viewcontroller"
)

const (
	getRolloutExample = `
	# Get a rollout
	%[1]s get rollout guestbook

	# Watch progress of a rollout
	%[1]s get rollout guestbook -w
	
	# Print status
	%[1]s get rollout guestbook -o jsonpath='{.spec.status}'

	# Print Blue Green preview replica set status
	%[1]s get rollout guestbook -o jsonpath='{.spec.replicaSets[?(@.spec.preview==true)].spec.status}'
	`
)

// NewCmdGetRollout returns a new instance of an `rollouts get rollout` command
func NewCmdGetRollout(o *options.ArgoRolloutsOptions) *cobra.Command {
	getOptions := GetOptions{
		ArgoRolloutsOptions: *o,
		PrintFlags:          genericclioptions.NewPrintFlags("rollout"),
	}

	var cmd = &cobra.Command{
		Use:          "rollout ROLLOUT_NAME",
		Short:        "Get details about a rollout",
		Long:         "Get details about and visual representation of a rollout. " + getUsageCommon,
		Aliases:      []string{"ro", "rollouts"},
		Example:      o.Example(getRolloutExample),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 1 {
				return o.UsageErr(c)
			}
			name := args[0]
			controller := viewcontroller.NewRolloutViewController(o.Namespace(), name, getOptions.KubeClientset(), getOptions.RolloutsClientset())
			ctx := context.Background()
			controller.Start(ctx)

			ri, err := controller.GetRolloutInfo()
			if err != nil {
				return err
			}
			if !getOptions.Watch {
				getOptions.PrintRollout(ri)
			} else {
				rolloutUpdates := make(chan *info.RolloutInfo)
				controller.RegisterCallback(func(roInfo *info.RolloutInfo) {
					rolloutUpdates <- roInfo
				})
				go getOptions.WatchRollout(ctx.Done(), rolloutUpdates)
				controller.Run(ctx)
				close(rolloutUpdates)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&getOptions.Watch, "watch", "w", false, "Watch live updates to the rollout")
	cmd.Flags().BoolVar(&getOptions.NoColor, "no-color", false, "Do not colorize output")
	getOptions.PrintFlags.AddFlags(cmd)
	return cmd
}

func (o *GetOptions) WatchRollout(stopCh <-chan struct{}, rolloutUpdates chan *info.RolloutInfo) {
	ticker := time.NewTicker(time.Second)
	var currRolloutInfo *info.RolloutInfo
	// preventFlicker is used to rate-limit the updates we print to the terminal when updates occur
	// so rapidly that it causes the terminal to flicker
	var preventFlicker time.Time

	for {
		select {
		case roInfo := <-rolloutUpdates:
			currRolloutInfo = roInfo
		case <-ticker.C:
		case <-stopCh:
			return
		}
		if currRolloutInfo != nil && time.Now().After(preventFlicker.Add(200*time.Millisecond)) {
			o.Clear()
			o.PrintRollout(currRolloutInfo)
			preventFlicker = time.Now()
		}
	}
}

// formatImage formats an ImageInfo with colorized imageinfo tags (e.g. canary, stable)
func (o *GetOptions) formatImage(image info.ImageInfo) string {
	imageStr := image.Image
	if len(image.Tags) > 0 {
		var colorizedTags []string
		for _, tag := range image.Tags {
			colorizedTags = append(colorizedTags, o.colorize(tag))
		}
		imageStr = fmt.Sprintf("%s (%s)", image.Image, strings.Join(colorizedTags, ", "))
	}
	return imageStr
}

func (o *GetOptions) PrintRollout(roInfo *info.RolloutInfo) {
	if o.PrintFlags.OutputFlagSpecified() {
		printer, err := o.PrintFlags.ToPrinter()
		cmdutil.CheckErr(err)
		printer.PrintObj(roInfo, o.Out)
	} else {
		o.PrintRolloutTable(roInfo)
	}
}

func (o *GetOptions) PrintRolloutTable(roInfo *info.RolloutInfo) {
	fmt.Fprintf(o.Out, tableFormat, "Name:", roInfo.Name)
	fmt.Fprintf(o.Out, tableFormat, "Namespace:", roInfo.Namespace)
	fmt.Fprintf(o.Out, tableFormat, "Status:", o.colorize(roInfo.Spec.Icon)+" "+roInfo.Spec.Status)
	fmt.Fprintf(o.Out, tableFormat, "Strategy:", roInfo.Spec.Strategy)
	if roInfo.Spec.Strategy == "Canary" {
		fmt.Fprintf(o.Out, tableFormat, "  Step:", roInfo.Spec.Step)
		fmt.Fprintf(o.Out, tableFormat, "  SetWeight:", roInfo.Spec.SetWeight)
		fmt.Fprintf(o.Out, tableFormat, "  ActualWeight:", roInfo.Spec.ActualWeight)
	}
	images := roInfo.Images()
	if len(images) > 0 {
		fmt.Fprintf(o.Out, tableFormat, "Images:", o.formatImage(images[0]))
		for i := 1; i < len(images); i++ {
			fmt.Fprintf(o.Out, tableFormat, "", o.formatImage(images[i]))
		}
	}
	fmt.Fprint(o.Out, "Replicas:\n")
	fmt.Fprintf(o.Out, tableFormat, "  Desired:", roInfo.Spec.Desired)
	fmt.Fprintf(o.Out, tableFormat, "  Current:", roInfo.Spec.Current)
	fmt.Fprintf(o.Out, tableFormat, "  Updated:", roInfo.Spec.Updated)
	fmt.Fprintf(o.Out, tableFormat, "  Ready:", roInfo.Spec.Ready)
	fmt.Fprintf(o.Out, tableFormat, "  Available:", roInfo.Spec.Available)

	fmt.Fprintf(o.Out, "\n")
	o.PrintRolloutTree(roInfo)
}

func (o *GetOptions) PrintRolloutTree(roInfo *info.RolloutInfo) {
	w := ansiterm.NewTabWriter(o.Out, 0, 0, 2, ' ', 0)
	o.PrintHeader(w)
	fmt.Fprintf(w, "%s %s\t%s\t%s %s\t%s\t%v\n", IconRollout, roInfo.Name, "Rollout", o.colorize(roInfo.Spec.Icon), roInfo.Spec.Status, roInfo.Age(), "")
	revisions := roInfo.Revisions()
	for i, rev := range revisions {
		isLast := i == len(revisions)-1
		prefix, subpfx := getPrefixes(isLast, "")
		o.PrintRevision(w, roInfo, rev, prefix, subpfx)
	}
	_ = w.Flush()
}

func (o *GetOptions) PrintRevision(w io.Writer, roInfo *info.RolloutInfo, revision int, prefix string, subpfx string) {
	name := fmt.Sprintf("revision:%d", revision)
	fmt.Fprintf(w, "%s%s %s\t%s\t%s %s\t%s\t%v\n", prefix, IconRevision, name, "", "", "", "", "")
	replicaSets := roInfo.ReplicaSetsByRevision(revision)
	experiments := roInfo.ExperimentsByRevision(revision)
	analysisRuns := roInfo.AnalysisRunsByRevision(revision)
	total := len(replicaSets) + len(experiments) + len(analysisRuns)
	curr := 0

	for _, rsInfo := range replicaSets {
		childPrefix, childSubpfx := getPrefixes(curr == total-1, subpfx)
		o.PrintReplicaSetInfo(w, rsInfo, childPrefix, childSubpfx)
		curr++
	}
	for _, expInfo := range experiments {
		childPrefix, childSubpfx := getPrefixes(curr == total-1, subpfx)
		o.PrintExperimentInfo(w, expInfo, childPrefix, childSubpfx)
		curr++
	}
	for _, arInfo := range analysisRuns {
		childPrefix, childSubpfx := getPrefixes(curr == total-1, subpfx)
		o.PrintAnalysisRunInfo(w, arInfo, childPrefix, childSubpfx)
		curr++
	}
}

func (o *GetOptions) PrintReplicaSetInfo(w io.Writer, rsInfo info.ReplicaSetInfo, prefix string, subpfx string) {
	infoCols := []string{}
	name := rsInfo.Name
	if rsInfo.Spec.Stable {
		infoCols = append(infoCols, o.colorize(info.InfoTagStable))
		name = o.colorizeStatus(name, info.InfoTagStable)
	} else if rsInfo.Spec.Canary {
		infoCols = append(infoCols, o.colorize(info.InfoTagCanary))
		name = o.colorizeStatus(name, info.InfoTagCanary)
	} else if rsInfo.Spec.Active {
		infoCols = append(infoCols, o.colorize(info.InfoTagActive))
		name = o.colorizeStatus(name, info.InfoTagActive)
	} else if rsInfo.Spec.Preview {
		infoCols = append(infoCols, o.colorize(info.InfoTagPreview))
		name = o.colorizeStatus(name, info.InfoTagPreview)
	}
	if rsInfo.Spec.ScaleDownDeadline != "" {
		infoCols = append(infoCols, fmt.Sprintf("delay:%s", rsInfo.Spec.ScaleDownDelay()))
	}

	fmt.Fprintf(w, "%s%s %s\t%s\t%s %s\t%s\t%v\n", prefix, IconReplicaSet, name, "ReplicaSet", o.colorize(rsInfo.Spec.Icon), rsInfo.Spec.Status, rsInfo.Age(), strings.Join(infoCols, ","))
	for i, podInfo := range rsInfo.Spec.Pods {
		isLast := i == len(rsInfo.Spec.Pods)-1
		podPrefix, _ := getPrefixes(isLast, subpfx)
		podInfoCol := []string{fmt.Sprintf("ready:%s", podInfo.Ready)}
		if podInfo.Restarts > 0 {
			podInfoCol = append(podInfoCol, fmt.Sprintf("restarts:%d", podInfo.Restarts))
		}
		fmt.Fprintf(w, "%s%s %s\t%s\t%s %s\t%s\t%v\n", podPrefix, IconPod, podInfo.Name, "Pod", o.colorize(podInfo.Icon), podInfo.Status, podInfo.Age(), strings.Join(podInfoCol, ","))
	}
}

func (o *GetOptions) PrintAnalysisRunInfo(w io.Writer, arInfo info.AnalysisRunInfo, prefix string, subpfx string) {
	name := o.colorizeStatus(arInfo.Name, arInfo.Spec.Status)
	infoCols := []string{}
	if arInfo.Spec.Successful > 0 {
		infoCols = append(infoCols, fmt.Sprintf("%s %d", o.colorize(info.IconOK), arInfo.Spec.Successful))
	}
	if arInfo.Spec.Failed > 0 {
		infoCols = append(infoCols, fmt.Sprintf("%s %d", o.colorize(info.IconBad), arInfo.Spec.Failed))
	}
	if arInfo.Spec.Inconclusive > 0 {
		infoCols = append(infoCols, fmt.Sprintf("%s %d", o.colorize(info.IconUnknown), arInfo.Spec.Inconclusive))
	}
	if arInfo.Spec.Error > 0 {
		infoCols = append(infoCols, fmt.Sprintf("%s %d", o.colorize(info.IconWarning), arInfo.Spec.Error))
	}
	fmt.Fprintf(w, "%s%s %s\t%s\t%s %s\t%s\t%v\n", prefix, IconAnalysis, name, "AnalysisRun", o.colorize(arInfo.Spec.Icon), arInfo.Spec.Status, arInfo.Age(), strings.Join(infoCols, ","))
	for i, jobInfo := range arInfo.Spec.Jobs {
		isLast := i == len(arInfo.Spec.Jobs)-1
		jobPrefix, jobChildPrefix := getPrefixes(isLast, subpfx)
		o.PrintJob(w, jobInfo, jobPrefix, jobChildPrefix)
	}
}

func (o *GetOptions) PrintJob(w io.Writer, jobInfo info.JobInfo, prefix string, subpfx string) {
	name := o.colorizeStatus(jobInfo.Name, jobInfo.Status)
	fmt.Fprintf(w, "%s%s %s\t%s\t%s %s\t%s\t%v\n", prefix, IconJob, name, "Job", o.colorize(jobInfo.Icon), jobInfo.Status, jobInfo.Age(), "")
}
