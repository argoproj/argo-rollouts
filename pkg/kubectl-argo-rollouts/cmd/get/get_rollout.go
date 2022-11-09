package get

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/juju/ansiterm"
	"github.com/spf13/cobra"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/signals"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	completionutil "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/util/completion"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/viewcontroller"
)

const (
	getRolloutExample = `
	# Get a rollout
	%[1]s get rollout guestbook

	# Watch progress of a rollout
  	%[1]s get rollout guestbook -w`
)

// NewCmdGetRollout returns a new instance of an `rollouts get rollout` command
func NewCmdGetRollout(o *options.ArgoRolloutsOptions) *cobra.Command {
	getOptions := GetOptions{
		ArgoRolloutsOptions: *o,
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
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			signals.SetupSignalHandler(cancel)
			controller.Start(ctx)

			ri, err := controller.GetRolloutInfo()
			if err != nil {
				return err
			}
			if !getOptions.Watch {
				getOptions.PrintRollout(ri)
			} else {
				rolloutUpdates := make(chan *rollout.RolloutInfo)
				controller.RegisterCallback(func(roInfo *rollout.RolloutInfo) {
					rolloutUpdates <- roInfo
				})
				stopCh := ctx.Done()
				if getOptions.TimeoutSeconds > 0 {
					ts := time.Duration(getOptions.TimeoutSeconds)
					newCtx, cancel := context.WithTimeout(ctx, ts*time.Second)
					ctx = newCtx
					defer cancel()
					stopCh = newCtx.Done()
				}
				go getOptions.WatchRollout(stopCh, rolloutUpdates)
				controller.Run(ctx)
				close(rolloutUpdates)
			}
			return nil
		},
		ValidArgsFunction: completionutil.RolloutNameCompletionFunc(o),
	}
	cmd.Flags().BoolVarP(&getOptions.Watch, "watch", "w", false, "Watch live updates to the rollout")
	cmd.Flags().BoolVar(&getOptions.NoColor, "no-color", false, "Do not colorize output")
	cmd.Flags().IntVar(&getOptions.TimeoutSeconds, "timeout-seconds", 0, "Timeout after specified seconds")
	return cmd
}

func Watch(stopCh <-chan struct{}, rolloutUpdates chan *rollout.RolloutInfo, callback func(*rollout.RolloutInfo)) {
	ticker := time.NewTicker(time.Second)
	var currRolloutInfo *rollout.RolloutInfo
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
			callback(currRolloutInfo)
			preventFlicker = time.Now()
		}
	}
}

func (o *GetOptions) WatchRollout(stopCh <-chan struct{}, rolloutUpdates chan *rollout.RolloutInfo) {
	Watch(stopCh, rolloutUpdates,
		func(i *rollout.RolloutInfo) {
			o.Clear()
			o.PrintRollout(i)
		})
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

func (o *GetOptions) PrintRollout(roInfo *rollout.RolloutInfo) {
	fmt.Fprintf(o.Out, tableFormat, "Name:", roInfo.ObjectMeta.Name)
	fmt.Fprintf(o.Out, tableFormat, "Namespace:", roInfo.ObjectMeta.Namespace)
	fmt.Fprintf(o.Out, tableFormat, "Status:", o.colorize(roInfo.Icon)+" "+roInfo.Status)
	if roInfo.Message != "" {
		fmt.Fprintf(o.Out, tableFormat, "Message:", roInfo.Message)
	}
	fmt.Fprintf(o.Out, tableFormat, "Strategy:", roInfo.Strategy)
	if roInfo.Strategy == "Canary" {
		fmt.Fprintf(o.Out, tableFormat, "  Step:", roInfo.Step)
		fmt.Fprintf(o.Out, tableFormat, "  SetWeight:", roInfo.SetWeight)
		fmt.Fprintf(o.Out, tableFormat, "  ActualWeight:", roInfo.ActualWeight)
	}
	images := info.Images(roInfo)
	if len(images) > 0 {
		fmt.Fprintf(o.Out, tableFormat, "Images:", o.formatImage(images[0]))
		for i := 1; i < len(images); i++ {
			fmt.Fprintf(o.Out, tableFormat, "", o.formatImage(images[i]))
		}
	}
	fmt.Fprint(o.Out, "Replicas:\n")
	fmt.Fprintf(o.Out, tableFormat, "  Desired:", roInfo.Desired)
	fmt.Fprintf(o.Out, tableFormat, "  Current:", roInfo.Current)
	fmt.Fprintf(o.Out, tableFormat, "  Updated:", roInfo.Updated)
	fmt.Fprintf(o.Out, tableFormat, "  Ready:", roInfo.Ready)
	fmt.Fprintf(o.Out, tableFormat, "  Available:", roInfo.Available)

	fmt.Fprintf(o.Out, "\n")
	o.PrintRolloutTree(roInfo)
}

func (o *GetOptions) PrintRolloutTree(roInfo *rollout.RolloutInfo) {
	w := ansiterm.NewTabWriter(o.Out, 0, 0, 2, ' ', 0)
	o.PrintHeader(w)
	fmt.Fprintf(w, "%s %s\t%s\t%s %s\t%s\t%v\n", IconRollout, roInfo.ObjectMeta.Name, "Rollout", o.colorize(roInfo.Icon), roInfo.Status, info.Age(*roInfo.ObjectMeta), "")
	revisions := info.Revisions(roInfo)
	for i, rev := range revisions {
		isLast := i == len(revisions)-1
		prefix, subpfx := getPrefixes(isLast, "")
		o.PrintRevision(w, roInfo, rev, prefix, subpfx)
	}
	_ = w.Flush()
}

func (o *GetOptions) PrintRevision(w io.Writer, roInfo *rollout.RolloutInfo, revision int, prefix string, subpfx string) {
	name := fmt.Sprintf("revision:%d", revision)
	fmt.Fprintf(w, "%s%s %s\t%s\t%s %s\t%s\t%v\n", prefix, IconRevision, name, "", "", "", "", "")
	replicaSets := info.ReplicaSetsByRevision(roInfo, revision)
	experiments := info.ExperimentsByRevision(roInfo, revision)
	analysisRuns := info.AnalysisRunsByRevision(roInfo, revision)
	total := len(replicaSets) + len(experiments) + len(analysisRuns)
	curr := 0

	for _, rsInfo := range replicaSets {
		childPrefix, childSubpfx := getPrefixes(curr == total-1, subpfx)
		o.PrintReplicaSetInfo(w, *rsInfo, childPrefix, childSubpfx)
		curr++
	}
	for _, expInfo := range experiments {
		childPrefix, childSubpfx := getPrefixes(curr == total-1, subpfx)
		o.PrintExperimentInfo(w, *expInfo, childPrefix, childSubpfx)
		curr++
	}
	for _, arInfo := range analysisRuns {
		childPrefix, childSubpfx := getPrefixes(curr == total-1, subpfx)
		o.PrintAnalysisRunInfo(w, *arInfo, childPrefix, childSubpfx)
		curr++
	}
}

func (o *GetOptions) PrintReplicaSetInfo(w io.Writer, rsInfo rollout.ReplicaSetInfo, prefix string, subpfx string) {
	infoCols := []string{}
	name := rsInfo.ObjectMeta.Name
	if rsInfo.Stable {
		infoCols = append(infoCols, o.colorize(info.InfoTagStable))
		name = o.colorizeStatus(name, info.InfoTagStable)
	}
	if rsInfo.Canary {
		infoCols = append(infoCols, o.colorize(info.InfoTagCanary))
		name = o.colorizeStatus(name, info.InfoTagCanary)
	} else if rsInfo.Active {
		infoCols = append(infoCols, o.colorize(info.InfoTagActive))
		name = o.colorizeStatus(name, info.InfoTagActive)
	} else if rsInfo.Preview {
		infoCols = append(infoCols, o.colorize(info.InfoTagPreview))
		name = o.colorizeStatus(name, info.InfoTagPreview)
	}
	if rsInfo.Ping {
		infoCols = append(infoCols, o.colorize(info.InfoTagPing))
		name = o.colorizeStatus(name, info.InfoTagPing)
	}
	if rsInfo.Pong {
		infoCols = append(infoCols, o.colorize(info.InfoTagPong))
		name = o.colorizeStatus(name, info.InfoTagPong)
	}
	if rsInfo.ScaleDownDeadline != "" {
		infoCols = append(infoCols, fmt.Sprintf("delay:%s", info.ScaleDownDelay(rsInfo)))
	}

	fmt.Fprintf(w, "%s%s %s\t%s\t%s %s\t%s\t%v\n", prefix, IconReplicaSet, name, "ReplicaSet", o.colorize(rsInfo.Icon), rsInfo.Status, info.Age(*rsInfo.ObjectMeta), strings.Join(infoCols, ","))
	for i, podInfo := range rsInfo.Pods {
		isLast := i == len(rsInfo.Pods)-1
		podPrefix, _ := getPrefixes(isLast, subpfx)
		podInfoCol := []string{fmt.Sprintf("ready:%s", podInfo.Ready)}
		if podInfo.Restarts > 0 {
			podInfoCol = append(podInfoCol, fmt.Sprintf("restarts:%d", podInfo.Restarts))
		}
		fmt.Fprintf(w, "%s%s %s\t%s\t%s %s\t%s\t%v\n", podPrefix, IconPod, podInfo.ObjectMeta.Name, "Pod", o.colorize(podInfo.Icon), podInfo.Status, info.Age(*podInfo.ObjectMeta), strings.Join(podInfoCol, ","))
	}
}

func (o *GetOptions) PrintAnalysisRunInfo(w io.Writer, arInfo rollout.AnalysisRunInfo, prefix string, subpfx string) {
	name := o.colorizeStatus(arInfo.ObjectMeta.Name, arInfo.Status)
	infoCols := []string{}
	if arInfo.Successful > 0 {
		infoCols = append(infoCols, fmt.Sprintf("%s %d", o.colorize(info.IconOK), arInfo.Successful))
	}
	if arInfo.Failed > 0 {
		infoCols = append(infoCols, fmt.Sprintf("%s %d", o.colorize(info.IconBad), arInfo.Failed))
	}
	if arInfo.Inconclusive > 0 {
		infoCols = append(infoCols, fmt.Sprintf("%s %d", o.colorize(info.IconUnknown), arInfo.Inconclusive))
	}
	if arInfo.Error > 0 {
		infoCols = append(infoCols, fmt.Sprintf("%s %d", o.colorize(info.IconWarning), arInfo.Error))
	}
	fmt.Fprintf(w, "%s%s %s\t%s\t%s %s\t%s\t%v\n", prefix, IconAnalysis, name, "AnalysisRun", o.colorize(arInfo.Icon), arInfo.Status, info.Age(*arInfo.ObjectMeta), strings.Join(infoCols, ","))
	for i, jobInfo := range arInfo.Jobs {
		isLast := i == len(arInfo.Jobs)-1
		jobPrefix, jobChildPrefix := getPrefixes(isLast, subpfx)
		o.PrintJob(w, *jobInfo, jobPrefix, jobChildPrefix)
	}
}

func (o *GetOptions) PrintJob(w io.Writer, jobInfo rollout.JobInfo, prefix string, subpfx string) {
	name := o.colorizeStatus(jobInfo.ObjectMeta.Name, jobInfo.Status)
	fmt.Fprintf(w, "%s%s %s\t%s\t%s %s\t%s\t%v\n", prefix, IconJob, name, "Job", o.colorize(jobInfo.Icon), jobInfo.Status, info.Age(*jobInfo.ObjectMeta), "")
}
