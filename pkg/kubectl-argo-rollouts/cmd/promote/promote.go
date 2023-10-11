package promote

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/typed/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	completionutil "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/util/completion"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

const (
	promoteExample = `
	# Promote a paused rollout
	%[1]s promote guestbook

	# Fully promote a rollout to desired version, skipping analysis, pauses, and steps
	%[1]s promote guestbook --full`

	promoteUsage = `Promote a rollout

Promotes a rollout paused at a canary step, or a paused blue-green pre-promotion.
To skip analysis, pauses and steps entirely, use '--full' to fully promote the rollout`
)

const (
	setCurrentStepIndex                         = `{"status":{"currentStepIndex":%d}}`
	unpausePatch                                = `{"spec":{"paused":false}}`
	clearPauseConditionsPatch                   = `{"status":{"pauseConditions":null}}`
	clearPauseConditionsAndControllerPausePatch = `{"status":{"pauseConditions":null, "controllerPause":false, "currentStepIndex":%d}}`
	unpauseAndClearPauseConditionsPatch         = `{"spec":{"paused":false},"status":{"pauseConditions":null}}`
	promoteFullPatch                            = `{"status":{"promoteFull":true}}`
	clearPauseConditionsPatchWithStep           = `{"status":{"pauseConditions":null, "currentStepIndex":%d}}`
	unpauseAndClearPauseConditionsPatchWithStep = `{"spec":{"paused":false},"status":{"pauseConditions":null, "currentStepIndex":%d}}`

	useBothSkipFlagsError         = "Cannot use skip-current-step and skip-all-steps flags at the same time"
	skipFlagsWithBlueGreenError   = "Cannot skip steps of a bluegreen rollout. Run without a flags"
	skipFlagWithNoStepCanaryError = "Cannot skip steps of a rollout without steps"
)

// NewCmdPromote returns a new instance of an `rollouts promote` command
func NewCmdPromote(o *options.ArgoRolloutsOptions) *cobra.Command {
	var (
		skipCurrentStep = false
		skipAllSteps    = false
		full            = false
	)
	var cmd = &cobra.Command{
		Use:          "promote ROLLOUT_NAME",
		Short:        "Promote a rollout",
		Long:         promoteUsage,
		Example:      o.Example(promoteExample),
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
			ro, err := PromoteRollout(rolloutIf, name, skipCurrentStep, skipAllSteps, full)
			if err != nil {
				return err
			}
			if full {
				fmt.Fprintf(o.Out, "rollout '%s' fully promoted\n", ro.Name)
			} else {
				fmt.Fprintf(o.Out, "rollout '%s' promoted\n", ro.Name)
			}

			return nil
		},
		ValidArgsFunction: completionutil.RolloutNameCompletionFunc(o),
	}
	cmd.Flags().BoolVarP(&skipCurrentStep, "skip-current-step", "c", false, "Skip currently running canary step")
	cmd.Flags().BoolVarP(&skipAllSteps, "skip-all-steps", "a", false, "Skip remaining steps")
	cmd.Flags().MarkDeprecated("skip-current-step", "use without the flag instead ex: promote ROLLOUT_NAME")
	cmd.Flags().MarkShorthandDeprecated("c", "use without the flag instead ex: promote ROLLOUT_NAME")
	cmd.Flags().MarkDeprecated("skip-all-steps", "use --full instead")
	cmd.Flags().MarkShorthandDeprecated("a", "use --full instead")
	cmd.Flags().BoolVar(&full, "full", false, "Perform a full promotion, skipping analysis, pauses, and steps")
	return cmd
}

// PromoteRollout promotes a rollout to the next step, or to end of all steps
func PromoteRollout(rolloutIf clientset.RolloutInterface, name string, skipCurrentStep, skipAllSteps, full bool) (*v1alpha1.Rollout, error) {
	ctx := context.TODO()
	ro, err := rolloutIf.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if skipCurrentStep || skipAllSteps {
		if ro.Spec.Strategy.BlueGreen != nil {
			return nil, fmt.Errorf(skipFlagsWithBlueGreenError)
		}
		if ro.Spec.Strategy.Canary != nil && len(ro.Spec.Strategy.Canary.Steps) == 0 {
			return nil, fmt.Errorf(skipFlagWithNoStepCanaryError)
		}
	}

	// This function is intended to be compatible with Rollouts v0.9 and Rollouts v0.10+, the latter
	// of which uses CRD status subresources. When using status subresource, status must be updated
	// separately from spec. Since we don't know which version is installed in the cluster, we
	// attempt status patching first. If it errors with NotFound, it indicates that status
	// subresource is not used (v0.9), at which point we need to use the unified patch that updates
	// both spec and status. Otherwise, we proceed with a spec only patch.
	specPatch, statusPatch, unifiedPatch := getPatches(ro, skipCurrentStep, skipAllSteps, full)
	if statusPatch != nil {
		ro, err = rolloutIf.Patch(ctx, name, types.MergePatchType, statusPatch, metav1.PatchOptions{}, "status")
		if err != nil {
			// NOTE: in the future, we can simply return error here, if we wish to drop support for v0.9
			if !k8serrors.IsNotFound(err) {
				return nil, err
			}
			// we got a NotFound error. status subresource is not being used, so perform unifiedPatch
			specPatch = unifiedPatch
		}
	}
	if specPatch != nil {
		ro, err = rolloutIf.Patch(ctx, name, types.MergePatchType, specPatch, metav1.PatchOptions{})
		if err != nil {
			return nil, err
		}
	}
	return ro, nil
}

func isInconclusive(rollout *v1alpha1.Rollout) bool {
	return rollout.Spec.Strategy.Canary != nil && rollout.Status.Canary.CurrentStepAnalysisRunStatus != nil && rollout.Status.Canary.CurrentStepAnalysisRunStatus.Status == v1alpha1.AnalysisPhaseInconclusive
}

func getPatches(rollout *v1alpha1.Rollout, skipCurrentStep, skipAllStep, full bool) ([]byte, []byte, []byte) {
	var specPatch, statusPatch, unifiedPatch []byte
	switch {
	case skipCurrentStep:
		_, index := replicasetutil.GetCurrentCanaryStep(rollout)
		// At this point, the controller knows that the rollout is a canary with steps and GetCurrentCanaryStep returns 0 if
		// the index is not set in the rollout
		if index != nil {
			if *index < int32(len(rollout.Spec.Strategy.Canary.Steps)) {
				*index++
			}
			statusPatch = []byte(fmt.Sprintf(setCurrentStepIndex, *index))
			unifiedPatch = statusPatch
		}

	case skipAllStep:
		statusPatch = []byte(fmt.Sprintf(setCurrentStepIndex, len(rollout.Spec.Strategy.Canary.Steps)))
		unifiedPatch = statusPatch
	case full:
		if rollout.Status.CurrentPodHash != rollout.Status.StableRS {
			statusPatch = []byte(promoteFullPatch)
		}
	default:
		unifiedPatch = []byte(unpauseAndClearPauseConditionsPatch)
		if rollout.Spec.Paused {
			specPatch = []byte(unpausePatch)
		}
		// in case if canary rollout in inconclusive state, we want to unset controller pause , clean pause conditions and increment step index
		// so that rollout can proceed to next step
		// without such patch, rollout will be stuck in inconclusive state in case if next step is pause step
		if isInconclusive(rollout) && len(rollout.Status.PauseConditions) > 0 && rollout.Status.ControllerPause {
			_, index := replicasetutil.GetCurrentCanaryStep(rollout)
			if index != nil {
				if *index < int32(len(rollout.Spec.Strategy.Canary.Steps)) {
					*index++
				}
				statusPatch = []byte(fmt.Sprintf(clearPauseConditionsAndControllerPausePatch, *index))
			}
		} else if len(rollout.Status.PauseConditions) > 0 {
			statusPatch = []byte(clearPauseConditionsPatch)
		} else if rollout.Spec.Strategy.Canary != nil {
			// we only want to clear pause conditions, or increment step index (never both)
			// this else block covers the case of promoting a rollout when it is in the middle of
			// running analysis/experiment
			// TODO: we currently do not handle promotion of two analysis steps in a row properly
			_, index := replicasetutil.GetCurrentCanaryStep(rollout)
			// At this point, the controller knows that the rollout is a canary with steps and GetCurrentCanaryStep returns 0 if
			// the index is not set in the rollout
			if index != nil {
				if *index < int32(len(rollout.Spec.Strategy.Canary.Steps)) {
					*index++
				}
				statusPatch = []byte(fmt.Sprintf(clearPauseConditionsPatchWithStep, *index))
				unifiedPatch = []byte(fmt.Sprintf(unpauseAndClearPauseConditionsPatchWithStep, *index))
			}
		}
	}
	return specPatch, statusPatch, unifiedPatch
}
