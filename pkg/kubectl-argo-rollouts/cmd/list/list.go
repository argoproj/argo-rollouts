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

// NewCmdList returns a new instance of an `rollouts list` command
func NewCmdList(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "list <rollout|experiment> RESOURCE",
		Short: "List rollouts, experiments",
		Example: o.Example(`
  # List rollouts
  %[1]s list rollouts
`),
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
