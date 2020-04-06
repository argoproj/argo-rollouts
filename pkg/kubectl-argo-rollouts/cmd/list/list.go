package list

import (
	"fmt"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
)

type ListOptions struct {
	name          string
	allNamespaces bool
	watch         bool
	timestamps    bool

	options.ArgoRolloutsOptions
}

const (
	listExample = `
	# List rollouts
	%[1]s list rollouts
	
	# List experiments
	%[1]s list experiments`

	listUsage = `This command consists of multiple subcommands which can be used to lists all of the 
rollouts or experiments for a specified namespace (uses current namespace context if namespace not specified).`
)

// NewCmdList returns a new instance of an `rollouts list` command
func NewCmdList(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:          "list <rollout|experiment>",
		Short:        "List rollouts or experiments",
		Long:         listUsage,
		Example:      o.Example(listExample),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			return o.UsageErr(c)
		},
	}
	cmd.AddCommand(NewCmdListRollouts(o))
	cmd.AddCommand(NewCmdListExperiments(o))
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
