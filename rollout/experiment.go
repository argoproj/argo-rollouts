package rollout

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	experimentutil "github.com/argoproj/argo-rollouts/utils/experiment"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

// GetExperimentFromTemplate takes the canary experiment step and converts it to an experiment
func GetExperimentFromTemplate(r *v1alpha1.Rollout, stableRS, newRS *appsv1.ReplicaSet) (*v1alpha1.Experiment, error) {
	step := replicasetutil.GetCurrentExperimentStep(r)
	if step == nil {
		return nil, nil
	}
	podHash := controller.ComputeHash(&r.Spec.Template, r.Status.CollisionCount)
	currentStep := int32(0)
	if r.Status.CurrentStepIndex != nil {
		currentStep = *r.Status.CurrentStepIndex
	}
	revision := ""
	if r.Annotations != nil {
		revision = r.Annotations[annotations.RevisionAnnotation]
	}
	experiment := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            fmt.Sprintf("%s-%s-%s-%d", r.Name, podHash, revision, currentStep),
			Namespace:       r.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(r, controllerKind)},
			Labels: map[string]string{
				v1alpha1.DefaultRolloutUniqueLabelKey: podHash,
			},
			Annotations: map[string]string{
				annotations.RevisionAnnotation: revision,
			},
		},
		Spec: v1alpha1.ExperimentSpec{
			Duration:                step.Duration,
			ProgressDeadlineSeconds: r.Spec.ProgressDeadlineSeconds,
		},
	}

	for i := range step.Templates {
		templateStep := step.Templates[i]
		template := v1alpha1.TemplateSpec{
			Name:     templateStep.Name,
			Replicas: templateStep.Replicas,
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

		if templateStep.Selector != nil {
			template.Selector = templateStep.Selector.DeepCopy()
		} else {
			template.Selector = templateRS.Spec.Selector.DeepCopy()
		}

		if templateStep.Metadata.Labels != nil {
			if template.Template.ObjectMeta.Labels == nil {
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

	for i := range step.Analyses {
		analysis := step.Analyses[i]
		args := analysisutil.BuildArgumentsForRolloutAnalysisRun(&analysis, stableRS, newRS)
		analysisTemplate := v1alpha1.ExperimentAnalysisTemplateRef{
			Name:         analysis.Name,
			TemplateName: analysis.TemplateName,
			Args:         args,
		}
		experiment.Spec.Analyses = append(experiment.Spec.Analyses, analysisTemplate)
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

func (c *RolloutController) reconcileExperiments(roCtx *canaryContext) error {
	rollout := roCtx.Rollout()
	logCtx := roCtx.Log()
	newRS := roCtx.NewRS()
	stableRS := roCtx.StableRS()

	if roCtx.PauseContext().IsAborted() {
		otherExs := roCtx.OtherExperiments()
		allExs := append(otherExs, roCtx.CurrentExperiment())
		return c.cancelExperiments(roCtx, allExs)
	}

	if roCtx.PauseContext().GetPauseCondition(v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		return nil
	}

	step, _ := replicasetutil.GetCurrentCanaryStep(rollout)

	currentEx := roCtx.CurrentExperiment()
	if step != nil && step.Experiment != nil {
		if currentEx == nil {
			// An new experiment can not be created if the stableRS is not created yet
			if stableRS == nil {
				logCtx.Infof("Cannot create experiment until stableRS exists")
				return nil
			}

			newEx, err := GetExperimentFromTemplate(rollout, stableRS, newRS)
			if err != nil {
				return err
			}

			currentEx, err = c.createExperimentWithCollisionHandling(roCtx, newEx)
			if err != nil {
				return err
			}

			msg := fmt.Sprintf("Created Experiment '%s'", currentEx.Name)
			logCtx.Info(msg)
			c.recorder.Event(rollout, corev1.EventTypeNormal, "CreateExperiment", msg)
		}
		switch currentEx.Status.Phase {
		case v1alpha1.AnalysisPhaseInconclusive:
			roCtx.PauseContext().AddPauseCondition(v1alpha1.PauseReasonInconclusiveExperiment)
		case v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseFailed:
			roCtx.PauseContext().AddAbort()
		case v1alpha1.AnalysisPhaseSuccessful:
			// Do not set current Experiment after successful experiment
		default:
			roCtx.SetCurrentExperiment(currentEx)
		}
	}

	otherExs := roCtx.OtherExperiments()
	if currentEx != nil && (step == nil || step.Experiment == nil) {
		otherExs = append(otherExs, currentEx)
	}
	err := c.cancelExperiments(roCtx, otherExs)
	if err != nil {
		return err
	}

	exsToDelete := experimentutil.FilterExperimentsToDelete(otherExs, roCtx.AllRSs())
	err = c.deleteExperiments(roCtx, exsToDelete)
	if err != nil {
		return err
	}

	return nil
}

// createExperimentWithCollisionHandling creates the given experiment, but with a new name
// in the event that an experiment with the same name already exists
func (c *RolloutController) createExperimentWithCollisionHandling(roCtx *canaryContext, newEx *v1alpha1.Experiment) (*v1alpha1.Experiment, error) {
	collisionCount := 1
	baseName := newEx.Name
	for {
		currentEx, err := c.argoprojclientset.ArgoprojV1alpha1().Experiments(newEx.Namespace).Create(newEx)
		if err == nil {
			return currentEx, nil
		}
		if !k8serrors.IsAlreadyExists(err) {
			return nil, err
		}
		existingEx, err := c.argoprojclientset.ArgoprojV1alpha1().Experiments(newEx.Namespace).Get(newEx.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		existingEqual := experimentutil.IsSemanticallyEqual(newEx.Spec, existingEx.Spec)
		controllerRef := metav1.GetControllerOf(existingEx)
		controllerUIDEqual := controllerRef != nil && controllerRef.UID == roCtx.Rollout().UID
		roCtx.log.Infof("Encountered collision of existing experiment %s (phase: %s, equal: %v, controllerUIDEqual: %v)", existingEx.Name, existingEx.Status.Phase, existingEqual, controllerUIDEqual)
		if !existingEx.Status.Phase.Completed() && existingEqual && controllerUIDEqual {
			// If we get here, the existing experiment has been determined to be our experiment and
			// we likely reconciled the rollout with a stale cache (quite common).
			return existingEx, nil
		}
		newEx.Name = fmt.Sprintf("%s.%d", baseName, collisionCount)
		collisionCount++
	}
}

func (c *RolloutController) cancelExperiments(roCtx *canaryContext, exs []*v1alpha1.Experiment) error {
	for i := range exs {
		ex := exs[i]
		if ex == nil {
			continue
		}
		if !ex.Spec.Terminate && !experimentutil.HasFinished(ex) {
			roCtx.Log().Infof("Canceling other running experiment '%s' owned by rollout", ex.Name)
			err := experimentutil.Terminate(c.argoprojclientset.ArgoprojV1alpha1().Experiments(ex.Namespace), ex.Name)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *RolloutController) deleteExperiments(roCtx rolloutContext, exs []*v1alpha1.Experiment) error {
	for i := range exs {
		ex := exs[i]
		if ex.DeletionTimestamp != nil {
			continue
		}
		roCtx.Log().Infof("Trying to cleanup experiment '%s'", ex.Name)
		err := c.argoprojclientset.ArgoprojV1alpha1().Experiments(ex.Namespace).Delete(ex.Name, nil)
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
