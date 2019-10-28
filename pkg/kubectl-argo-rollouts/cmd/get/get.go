package get

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/juju/ansiterm"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/duration"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/viewcontroller"
)

const (
	example = `
  # Get a rollout
  %[1]s get ROLLOUT
`
)

var (
	colorMapping = map[string]int{
		info.IconProgressing: FgBlue,
		info.IconWarning:     FgYellow,
		info.IconUnknown:     FgYellow,
		info.IconOK:          FgGreen,
		info.IconBad:         FgRed,
		info.IconPaused:      FgWhite,
		//info.IconNeutral:     FgWhite,
	}
)

const (
	IconRollout    = "⟳"
	IconReplicaSet = "⧉"
	IconPod        = "□"
	IconService    = "⑃" // other options: ⋲ ⇶ ⋔ ⤨
	IconExperiment = "Σ" // other options: ꀀ ⋃ ⨄
	IconAnalysis   = "α" // other options: ⚯
)

// ANSI escape codes
const (
	escape    = "\x1b"
	noFormat  = 0
	Bold      = 1
	FgBlack   = 30
	FgRed     = 31
	FgGreen   = 32
	FgYellow  = 33
	FgBlue    = 34
	FgMagenta = 35
	FgCyan    = 36
	FgWhite   = 37
	FgDefault = 39
)

type GetOptions struct {
	watch   bool
	noColor bool

	rolloutUpdates chan *info.RolloutInfo

	options.ArgoRolloutsOptions
}

// NewCmdGet returns a new instance of an `rollouts get` command
func NewCmdGet(o *options.ArgoRolloutsOptions) *cobra.Command {
	getOptions := GetOptions{
		ArgoRolloutsOptions: *o,
	}

	var cmd = &cobra.Command{
		Use:          "get ROLLOUT",
		Short:        "Get details about a rollouts",
		Example:      o.Example(example),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 1 {
				return o.UsageErr(c)
			}
			name := args[0]
			controller := viewcontroller.NewController(o.Namespace(), name, getOptions.KubeClientset(), getOptions.RolloutsClientset())
			ctx := context.Background()
			//ctx, cancel := context.WithCancel(ctx)
			controller.Start(ctx)

			if !getOptions.watch {
				ri, err := controller.GetRolloutInfo()
				if err != nil {
					return err
				}
				getOptions.PrintRollout(ri)
			} else {
				getOptions.rolloutUpdates = make(chan *info.RolloutInfo, 10)
				controller.RegisterCallback(getOptions.RefreshRollout)
				go getOptions.WatchRollout(ctx.Done())
				controller.Run(ctx)
				close(getOptions.rolloutUpdates)
			}
			return nil
		},
	}
	o.AddKubectlFlags(cmd)
	cmd.Flags().BoolVarP(&getOptions.watch, "watch", "w", false, "Watch live updates to the rollout")
	cmd.Flags().BoolVar(&getOptions.noColor, "no-color", false, "Do not colorize output")
	return cmd
}

const (
	tableFormat = "%-17s%v\n"
)

func (o *GetOptions) Clear() {
	fmt.Fprint(o.Out, "\033[H\033[2J")
	fmt.Fprint(o.Out, "\033[0;0H")
}

func (o *GetOptions) RefreshRollout(roInfo *info.RolloutInfo) {
	o.rolloutUpdates <- roInfo
}

func (o *GetOptions) WatchRollout(stopCh <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	var currRolloutInfo *info.RolloutInfo
	for {
		select {
		case roInfo := <-o.rolloutUpdates:
			currRolloutInfo = roInfo
			o.Clear()
			o.PrintRollout(roInfo)
		case <-ticker.C:
			if currRolloutInfo != nil {
				o.Clear()
				o.PrintRollout(currRolloutInfo)
			}
		case <-stopCh:
			return
		}
	}
}

func (o *GetOptions) PrintRollout(roInfo *info.RolloutInfo) {
	fmt.Fprintf(o.Out, tableFormat, "Name:", roInfo.Name)
	fmt.Fprintf(o.Out, tableFormat, "Namespace:", roInfo.Namespace)
	fmt.Fprintf(o.Out, tableFormat, "Status:", o.colorize(roInfo.Icon)+" "+roInfo.Status)
	fmt.Fprintf(o.Out, tableFormat, "Strategy:", roInfo.Strategy)
	if roInfo.Strategy == "Canary" {
		fmt.Fprintf(o.Out, tableFormat, "  Step:", roInfo.Step)
		fmt.Fprintf(o.Out, tableFormat, "  SetWeight:", roInfo.SetWeight)
		fmt.Fprintf(o.Out, tableFormat, "  ActualWeight:", roInfo.ActualWeight)
	}
	images := roInfo.Images()
	if len(images) > 0 {
		fmt.Fprintf(o.Out, tableFormat, "Images:", images[0])
		for i := 1; i < len(images); i++ {
			fmt.Fprintf(o.Out, tableFormat, "", images[i])
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

func (o *GetOptions) PrintRolloutTree(roInfo *info.RolloutInfo) {
	w := ansiterm.NewTabWriter(o.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", "NAME", "KIND", "STATUS", "INFO", "AGE")
	fmt.Fprintf(w, "%s %s\t%s\t%s %s\t%s\t%v\n", IconRollout, roInfo.Name, "Rollout", o.colorize(roInfo.Icon), roInfo.Status, "", duration.HumanDuration(roInfo.Age()))
	for i, rsInfo := range roInfo.ReplicaSets {
		isLast := i == len(roInfo.ReplicaSets)-1
		var prefix, subpfx string
		if !isLast {
			prefix = "├───"
			subpfx = "│   "
		} else {
			prefix = "└───"
			subpfx = "    "
		}
		o.PrintReplicaSetInfo(w, rsInfo, prefix, subpfx)
	}
	_ = w.Flush()
}

func (o *GetOptions) PrintReplicaSetInfo(w io.Writer, rsInfo info.ReplicaSetInfo, prefix string, subpfx string) {
	infoCols := []string{}
	name := rsInfo.Name
	if rsInfo.Stable {
		infoCols = append(infoCols, o.ansiFormat("stable", FgGreen))
		name = o.ansiFormat(name, FgGreen)
	} else if rsInfo.Canary {
		infoCols = append(infoCols, o.ansiFormat("canary", FgYellow))
		name = o.ansiFormat(name, FgYellow)
	} else if rsInfo.Active {
		infoCols = append(infoCols, o.ansiFormat("active", FgGreen))
		name = o.ansiFormat(name, FgGreen)
	} else if rsInfo.Preview {
		infoCols = append(infoCols, o.ansiFormat("preview", FgBlue))
		name = o.ansiFormat(name, FgBlue)
	}
	if rsInfo.Revision != 0 {
		name = fmt.Sprintf("%s (rev:%d)", name, rsInfo.Revision)
	}
	fmt.Fprintf(w, "%s%s %s\t%s\t%s %s\t%s\t%v\n", prefix, IconReplicaSet, name, "ReplicaSet", o.colorize(rsInfo.Icon), rsInfo.Status, strings.Join(infoCols, ","), duration.HumanDuration(rsInfo.Age()))
	for i, podInfo := range rsInfo.Pods {
		fmt.Fprintf(w, subpfx)
		isLast := i == len(rsInfo.Pods)-1
		var podPrefix string
		if !isLast {
			podPrefix = "├"
		} else {
			podPrefix = "└"
		}
		podInfoCol := []string{fmt.Sprintf("ready:%s", podInfo.Ready)}
		if podInfo.Restarts > 0 {
			podInfoCol = append(podInfoCol, fmt.Sprintf("restarts:%d", podInfo.Restarts))
		}
		fmt.Fprintf(w, "%s───%s %s\t%s\t%s %s\t%s\t%v\n", podPrefix, IconPod, podInfo.Name, "Pod", o.colorize(podInfo.Icon), podInfo.Status, strings.Join(podInfoCol, ","), duration.HumanDuration(podInfo.Age()))
	}
}

func (o *GetOptions) colorize(icon string) string {
	if o.noColor {
		return icon
	}
	color := colorMapping[icon]
	return o.ansiFormat(icon, color)
}

// ansiFormat wraps ANSI escape codes to a string to format the string to a desired color.
// NOTE: we still apply formatting even if there is no color formatting desired.
// The purpose of doing this is because when we apply ANSI color escape sequences to our
// output, this confuses the tabwriter library which miscalculates widths of columns and
// misaligns columns. By always applying a ANSI escape sequence (even when we don't want
// color, it provides more consistent string lengths so that tabwriter can calculate
// widths correctly.
func (o *GetOptions) ansiFormat(s string, codes ...int) string {
	// TODO(jessesuen): check for explicit color disabling
	if o.noColor || os.Getenv("TERM") == "dumb" || len(codes) == 0 {
		return s
	}
	codeStrs := make([]string, len(codes))
	for i, code := range codes {
		codeStrs[i] = strconv.Itoa(code)
	}
	sequence := strings.Join(codeStrs, ";")
	return fmt.Sprintf("%s[%sm%s%s[%dm", escape, sequence, s, escape, noFormat)
}
