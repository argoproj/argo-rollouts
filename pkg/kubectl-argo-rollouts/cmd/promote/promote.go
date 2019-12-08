package promote

import (
	"fmt"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

const (
	example = `
	# Promote a rollout with the canary strategy
	%[1]s promote guestbook [--skip-steps] [--skip-current-step]
`
	setCurrentStepIndex = `{
	"status": {
		"currentStepIndex": %d
	}
}`

	unpausePatch = `{
	"spec": {
		"paused": false
	},
	"status": {
		"pauseConditions": null
	}
}`
	useBothSkipFlagsError         = "Cannot use skip-current-step and skip-all-steps flags at the same time"
	skipFlagsWithBlueGreenError   = "Cannot skip steps of a bluegreen rollout. Run without a flags"
	skipFlagWithNoStepCanaryError = "Cannot skip steps of a rollout without steps"
)

// NewCmdPromote returns a new instance of an `rollouts promote` command
func NewCmdPromote(o *options.ArgoRolloutsOptions) *cobra.Command {
	var (
		skipCurrentStep = false
		skipAllSteps    = false
	)
	var cmd = &cobra.Command{
		Use:          "promote ROLLOUT",
		Short:        "Promote a rollout",
		Example:      o.Example(example),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 1 {
				return o.UsageErr(c)
			}
			if skipCurrentStep && skipAllSteps {
				return fmt.Errorf(useBothSkipFlagsError)
			}
			name := args[0]
			rolloutIf := o.RolloutsClientset().ArgoprojV1alpha1().Rollouts(o.Namespace())
			ro, err := rolloutIf.Get(name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if skipCurrentStep || skipAllSteps {
				if ro.Spec.Strategy.BlueGreen != nil {
					return fmt.Errorf(skipFlagsWithBlueGreenError)
				}
				if ro.Spec.Strategy.Canary != nil && len(ro.Spec.Strategy.Canary.Steps) == 0 {
					return fmt.Errorf(skipFlagWithNoStepCanaryError)
				}
			}
			patch := getPatch(ro, skipCurrentStep, skipAllSteps)
			ro, err = rolloutIf.Patch(name, types.MergePatchType, patch)
			if err != nil {
				return err
			}
			fmt.Fprintf(o.Out, "rollout '%s' promoted\n", ro.Name)
			return nil
		},
	}
	o.AddKubectlFlags(cmd)
	cmd.Flags().BoolVarP(&skipCurrentStep, "skip-current-step", "c", false, "Skip current step")
	cmd.Flags().BoolVarP(&skipAllSteps, "skip-all-steps", "a", false, "Skip remaining steps")

	return cmd
}

func getPatch(rollout *v1alpha1.Rollout, skipCurrentStep, skipAllStep bool) []byte {
	switch {
	case skipCurrentStep:
		_, index := replicasetutil.GetCurrentCanaryStep(rollout)
		// At this point, the controller knows that the rollout is a canary with steps and GetCurrentCanaryStep returns 0 if
		// the index is not set in the rollout
		if *index < int32(len(rollout.Spec.Strategy.Canary.Steps)) {
			*index++
		}
		return []byte(fmt.Sprintf(setCurrentStepIndex, *index))
	case skipAllStep:
		return []byte(fmt.Sprintf(setCurrentStepIndex, len(rollout.Spec.Strategy.Canary.Steps)))
	default:
		return []byte(unpausePatch)
	}
}
