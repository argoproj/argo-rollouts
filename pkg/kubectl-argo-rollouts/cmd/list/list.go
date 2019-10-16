package list

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	argoprojv1alpha1 "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/typed/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
)

const (
	example = `
  # List rollouts
  %[1]s list

  # List rollouts from all namespaces
  %[1]s list --all-namespaces

  # List rollouts and watch for changes
  %[1]s list --watch
`
)

type ListOptions struct {
	name          string
	allNamespaces bool
	watch         bool
	timestamps    bool

	options.ArgoRolloutsOptions
}

// NewCmdList returns a new instance of an `rollouts list` command
func NewCmdList(o *options.ArgoRolloutsOptions) *cobra.Command {
	listOptions := ListOptions{
		ArgoRolloutsOptions: *o,
	}

	var cmd = &cobra.Command{
		Use:          "list",
		Short:        "List rollouts",
		Example:      o.Example(example),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			var namespace string
			if listOptions.allNamespaces {
				namespace = metav1.NamespaceAll
			} else {
				namespace = o.Namespace()
			}
			rolloutIf := o.RolloutsClientset().ArgoprojV1alpha1().Rollouts(namespace)
			opts := listOptions.ListOptions()
			rolloutList, err := rolloutIf.List(opts)
			if err != nil {
				return err
			}
			err = listOptions.PrintRolloutTable(rolloutList)
			if err != nil {
				return err
			}
			if listOptions.watch {
				ctx := context.Background()
				err = listOptions.PrintRolloutUpdates(ctx, rolloutIf, rolloutList)
				if err != nil {
					return err
				}
			}
			return nil
		},
	}
	o.AddKubectlFlags(cmd)
	cmd.Flags().StringVar(&listOptions.name, "name", "", "Only show rollout with specified name")
	cmd.Flags().BoolVar(&listOptions.allNamespaces, "all-namespaces", false, "Include all namespaces")
	cmd.Flags().BoolVarP(&listOptions.watch, "watch", "w", false, "Watch for changes")
	cmd.Flags().BoolVar(&listOptions.timestamps, "timestamps", false, "Print timestamps on updates")
	return cmd
}

// ListOptions returns a metav1.ListOptions based on user supplied flags
func (o *ListOptions) ListOptions() metav1.ListOptions {
	opts := metav1.ListOptions{}
	if o.name != "" {
		nameSelector := fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name=%s", o.name))
		opts.FieldSelector = nameSelector.String()
	}
	return opts
}

// PrintRolloutTable prints rollouts in table format
func (o *ListOptions) PrintRolloutTable(roList *v1alpha1.RolloutList) error {
	if len(roList.Items) == 0 && !o.watch {
		fmt.Fprintln(o.ErrOut, "No resources found.")
		return nil
	}
	w := tabwriter.NewWriter(o.Out, 0, 0, 2, ' ', 0)
	headerStr := headerFmtString
	if o.allNamespaces {
		headerStr = "NAMESPACE\t" + headerStr
	}
	if o.timestamps {
		headerStr = "TIMESTAMP\t" + headerStr
	}
	fmt.Fprintf(w, headerStr)
	for _, ro := range roList.Items {
		roLine := newRolloutInfo(ro)
		fmt.Fprintln(w, roLine.String(o.timestamps, o.allNamespaces))
	}
	_ = w.Flush()
	return nil
}

// PrintRolloutUpdates watches for changes to rollouts and prints the updates
func (o *ListOptions) PrintRolloutUpdates(ctx context.Context, rolloutIf argoprojv1alpha1.RolloutInterface, roList *v1alpha1.RolloutList) error {
	w := tabwriter.NewWriter(o.Out, 0, 0, 2, ' ', 0)

	opts := o.ListOptions()
	opts.ResourceVersion = roList.ListMeta.ResourceVersion
	watchIf, err := rolloutIf.Watch(opts)
	if err != nil {
		return err
	}
	// ticker is used to flush the tabwriter every few moments so that table is aligned when there
	// are a flood of results in the watch channel
	ticker := time.NewTicker(500 * time.Millisecond)

	// prevLines remembers the most recent rollout lines we printed, so that we only print new lines
	// when they have have changed in a meaningful way
	prevLines := make(map[rolloutInfoKey]rolloutInfo)
	for _, ro := range roList.Items {
		roLine := newRolloutInfo(ro)
		prevLines[roLine.key()] = roLine
	}

	var ro *v1alpha1.Rollout
	retries := 0
L:
	for {
		select {
		case next := <-watchIf.ResultChan():
			ro, _ = next.Object.(*v1alpha1.Rollout)
		case <-ticker.C:
			_ = w.Flush()
			continue
		case <-ctx.Done():
			break L
		}
		if ro == nil {
			// if we get here, it means an error on the watch. try to re-establish the watch
			watchIf.Stop()
			newWatchIf, err := rolloutIf.Watch(opts)
			if err != nil {
				if retries > 5 {
					return err
				}
				o.Log.Warn(err)
				// this sleep prevents a hot-loop in the event there is a persistent error
				time.Sleep(time.Second)
				retries++
			} else {
				watchIf = newWatchIf
				retries = 0
			}
			continue
		}
		opts.ResourceVersion = ro.ObjectMeta.ResourceVersion
		roLine := newRolloutInfo(*ro)
		if prevLine, ok := prevLines[roLine.key()]; !ok || prevLine != roLine {
			fmt.Fprintln(w, roLine.String(o.timestamps, o.allNamespaces))
			prevLines[roLine.key()] = roLine
		}
	}
	watchIf.Stop()
	return nil
}
