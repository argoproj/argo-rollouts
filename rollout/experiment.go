package rollout

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	patchtypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	experimentutil "github.com/argoproj/argo-rollouts/utils/experiment"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

const (
	cancelExperimentPatch = `{
		"status": {
			"running": false
		}
	}`
)

// GetExperimentFromTemplate takes the canary experiment step and converts it to an experiment
func GetExperimentFromTemplate(r *v1alpha1.Rollout, stableRS, newRS *appsv1.ReplicaSet) (*v1alpha1.Experiment, error) {
	step := replicasetutil.GetCurrentExperimentStep(r)
	if step == nil {
		return nil, nil
	}
	experiment := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            experimentutil.ExperimentNameFromRollout(r),
			Namespace:       r.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(r, controllerKind)},
		},
		Spec: v1alpha1.ExperimentSpec{
			Duration:                &step.Duration,
			ProgressDeadlineSeconds: pointer.Int32Ptr(defaults.GetProgressDeadlineSecondsOrDefault(r)),
		},
	}
	for i := range step.Templates {
		templateStep := step.Templates[i]
		template := v1alpha1.TemplateSpec{
			Name:     templateStep.Name,
			Replicas: &templateStep.Replicas,
		}
		templateRS := &appsv1.ReplicaSet{}
		switch templateStep.SpecRef {
		case v1alpha1.CanarySpecRef:
			templateRS = newRS
		case v1alpha1.StableSpecRef:
			templateRS = stableRS
		default:
			return nil, fmt.Errorf("Invalid template step SpecRef: must be canary or stable")
		}
		template.Template = templateRS.Spec.Template
		template.MinReadySeconds = templateRS.Spec.MinReadySeconds

		template.Selector = templateRS.Spec.Selector.DeepCopy()
		if templateStep.Metadata.Labels != nil {
			if templateStep.Metadata.Labels == nil {
				template.Template.ObjectMeta.Labels = make(map[string]string)
			}
			for key := range templateStep.Metadata.Labels {
				template.Template.ObjectMeta.Labels[key] = templateStep.Metadata.Labels[key]
			}
		}
		if templateStep.Metadata.Annotations != nil {
			if template.Template.ObjectMeta.Annotations == nil {
				template.Template.ObjectMeta.Annotations = make(map[string]string)
			}

			for key := range templateStep.Metadata.Annotations {
				template.Template.ObjectMeta.Annotations[key] = templateStep.Metadata.Annotations[key]
			}
		}
		experiment.Spec.Templates = append(experiment.Spec.Templates, template)
	}

	return experiment, nil
}

// getExperimentsForRollout get all experiments owned by the Rollout
// changing steps in the Rollout Spec would cause multiple experiments to exist which is why it returns an array
func (c *RolloutController) getExperimentsForRollout(rollout *v1alpha1.Rollout) ([]*v1alpha1.Experiment, error) {
	experiments, err := c.experimentsLister.Experiments(rollout.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	//TODO(dthomson) consider adoption
	ownedByRollout := make([]*v1alpha1.Experiment, 0)
	for i := range experiments {
		e := experiments[i]
		controllerRef := metav1.GetControllerOf(e)
		if controllerRef != nil && controllerRef.UID == rollout.UID {
			ownedByRollout = append(ownedByRollout, e)
		}
	}
	return ownedByRollout, nil
}

func (c *RolloutController) reconcileExperiments(rollout *v1alpha1.Rollout, stableRS, newRS *appsv1.ReplicaSet, currentEx *v1alpha1.Experiment, otherExs []*v1alpha1.Experiment) error {
	logCtx := logutil.WithRollout(rollout)
	for i := range otherExs {
		otherEx := otherExs[i]
		if otherEx.Status.Running != nil && *otherEx.Status.Running {
			logCtx.Infof("Canceling other running experiment '%s' owned by rollout", otherEx.Name)
			_, err := c.argoprojclientset.ArgoprojV1alpha1().Experiments(otherEx.Namespace).Patch(otherEx.Name, patchtypes.MergePatchType, []byte(cancelExperimentPatch))
			if err != nil {
				return err
			}
		}
	}

	step, _ := replicasetutil.GetCurrentCanaryStep(rollout)
	if step == nil || step.Experiment == nil {
		return nil
	}
	if currentEx == nil {
		// An new experiment can not be created if the newRS or stableRS is not created yet
		if newRS == nil || stableRS == nil {
			logCtx.Infof("Cannot create experiment until newRS and stableRS both exist")
			return nil
		}

		newEx, err := GetExperimentFromTemplate(rollout, stableRS, newRS)
		if err != nil {
			return err
		}

		newEx, err = c.argoprojclientset.ArgoprojV1alpha1().Experiments(newEx.Namespace).Create(newEx)
		if err != nil {
			return err
		}
		msg := fmt.Sprintf("Created Experiment '%s'", newEx.Name)
		c.recorder.Event(rollout, corev1.EventTypeNormal, "CreateExperiment", msg)
	}
	return nil
}
