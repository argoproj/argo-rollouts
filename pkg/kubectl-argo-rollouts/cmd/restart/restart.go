package restart

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
)

const (
	example = `
	# Restart the current desired ReplicaSet
	%[1]s restart guestbook --in 10s
`
	restartPatch = `{
	"spec": {
		"restartAt": "%s"
	}
}`
)

func NewCmdRestart(o *options.ArgoRolloutsOptions) *cobra.Command {
	var (
		in string
	)
	var cmd = &cobra.Command{
		Use:          "restart ROLLOUT",
		Short:        "Restart the pods of a rollout",
		Example:      o.Example(example),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 1 {
				return o.UsageErr(c)
			}
			restartIn := o.Now()
			if in != "" {
				duration, err := v1alpha1.DurationString(in).Duration()
				if err != nil {
					panic(err)
				}
				restartIn = metav1.NewTime(restartIn.Add(duration))
			}
			name := args[0]
			rolloutIf := o.RolloutsClientset().ArgoprojV1alpha1().Rollouts(o.Namespace())
			ro, err := rolloutIf.Get(name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			patch := fmt.Sprintf(restartPatch, restartIn.Format(time.RFC3339))
			ro, err = rolloutIf.Patch(name, types.MergePatchType, []byte(patch))
			if err != nil {
				return err
			}
			if in == "" {
				in = "0s"
			}
			fmt.Fprintf(o.Out, "rollout '%s' restarts in %s\n", ro.Name, in)
			return nil
		},
	}
	o.AddKubectlFlags(cmd)
	cmd.Flags().StringVarP(&in, "in", "i", "", "Set amount of time in the future to do restart. (e.g. 30s, 5m, 1h)")

	return cmd
}
