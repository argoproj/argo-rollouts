package list

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/duration"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	experimentutil "github.com/argoproj/argo-rollouts/utils/experiment"
)

// NewCmdListExperiments returns a new instance of an `rollouts list experiments` command
func NewCmdListExperiments(o *options.ArgoRolloutsOptions) *cobra.Command {
	listOptions := ListOptions{
		ArgoRolloutsOptions: *o,
	}

	var cmd = &cobra.Command{
		Use:     "experiments",
		Aliases: []string{"exp"},
		Short:   "List experiments",
		Example: o.Example(`
  # List rollouts
  %[1]s list experiments

  # List rollouts from all namespaces
  %[1]s list experiments --all-namespaces

  # List rollouts and watch for changes
  %[1]s list experiments --watch
`),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			var namespace string
			if listOptions.allNamespaces {
				namespace = metav1.NamespaceAll
			} else {
				namespace = o.Namespace()
			}
			expIf := o.RolloutsClientset().ArgoprojV1alpha1().Experiments(namespace)
			opts := listOptions.ListOptions()
			expList, err := expIf.List(opts)
			if err != nil {
				return err
			}
			err = listOptions.PrintExperimentTable(expList)
			if err != nil {
				return err
			}
			return nil
		},
	}
	o.AddKubectlFlags(cmd)
	cmd.Flags().BoolVar(&listOptions.allNamespaces, "all-namespaces", false, "Include all namespaces")
	return cmd
}

const (
	expHeaderFmtString = "NAME\tSTATUS\tDURATION\tREMAINING\tAGE\n"
	expColumnFmtString = "%-10s\t%-6s\t%-8s\t%-9s\t%-3s\n"
)

// PrintExperimentTable prints experiments in table format
func (o *ListOptions) PrintExperimentTable(expList *v1alpha1.ExperimentList) error {
	if len(expList.Items) == 0 && !o.watch {
		fmt.Fprintln(o.ErrOut, "No resources found.")
		return nil
	}
	w := tabwriter.NewWriter(o.Out, 0, 0, 2, ' ', 0)
	headerStr := expHeaderFmtString
	fmtStr := expColumnFmtString
	if o.allNamespaces {
		headerStr = "NAMESPACE\t" + headerStr
		fmtStr = "%-9s\t" + fmtStr
	}
	fmt.Fprintf(w, headerStr)
	for _, exp := range expList.Items {
		age := duration.HumanDuration(metav1.Now().Sub(exp.CreationTimestamp.Time))
		dur := "-"
		remaining := "-"
		if exp.Spec.Duration != "" {
			if expDuration, err := exp.Spec.Duration.Duration(); err == nil {
				dur = duration.HumanDuration(expDuration)
				if !exp.Status.Phase.Completed() && exp.Status.AvailableAt != nil {
					if _, timeRemaining := experimentutil.PassedDurations(&exp); timeRemaining > 0 {
						remaining = duration.HumanDuration(timeRemaining)
					}
				}
			}
		}
		var cols []interface{}
		if o.allNamespaces {
			cols = append(cols, exp.Namespace)
		}
		cols = append(cols, exp.Name, exp.Status.Phase, dur, remaining, age)
		fmt.Fprintf(w, fmtStr, cols...)
	}
	_ = w.Flush()
	return nil
}
