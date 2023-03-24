package rollout

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	experimentutil "github.com/argoproj/argo-rollouts/utils/experiment"
	"github.com/argoproj/argo-rollouts/utils/hash"
	"github.com/argoproj/argo-rollouts/utils/record"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

// GetExperimentFromTemplate takes the canary experiment step and converts it to an experiment
func GetExperimentFromTemplate(r *v1alpha1.Rollout, stableRS, newRS *appsv1.ReplicaSet) (*v1alpha1.Experiment, error) {
	step := replicasetutil.GetCurrentExperimentStep(r)
	if step == nil {
		return nil, nil
	}
	podHash := hash.ComputePodTemplateHash(&r.Spec.Template, r.Status.CollisionCount)
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

	instanceID := analysisutil.GetInstanceID(r)
	if instanceID != "" {
		experiment.Labels[v1alpha1.LabelKeyControllerInstanceID] = instanceID
	}

	for i := range step.Templates {
		templateStep := step.Templates[i]
		template := v1alpha1.TemplateSpec{
			Name:     templateStep.Name,
			Replicas: templateStep.Replicas,
		}
		if templateStep.Weight != nil || templateStep.Service != nil {
			template.Service = &v1alpha1.TemplateService{}
			// Need to check if Service is not nil for the case where Weight is not nil and Service is
			if templateStep.Service != nil && templateStep.Service.Name != "" {
				template.Service.Name = templateStep.Service.Name
			}
		}
		templateRS := &appsv1.ReplicaSet{}
		switch templateStep.SpecRef {
		case v1alpha1.CanarySpecRef:
			templateRS = newRS.DeepCopy()
		case v1alpha1.StableSpecRef:
			templateRS = stableRS.DeepCopy()
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
				template.Selector.MatchLabels[key] = templateStep.Metadata.Labels[key]
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
		args, err := analysisutil.BuildArgumentsForRolloutAnalysisRun(analysis.Args, stableRS, newRS, r)
		if err != nil {
			return nil, err
		}

		analysisTemplate := v1alpha1.ExperimentAnalysisTemplateRef{
			Name:                  analysis.Name,
			TemplateName:          analysis.TemplateName,
			ClusterScope:          analysis.ClusterScope,
			Args:                  args,
			RequiredForCompletion: analysis.RequiredForCompletion,
		}
		experiment.Spec.Analyses = append(experiment.Spec.Analyses, analysisTemplate)
	}

	return experiment, nil
}

// getExperimentsForRollout get all experiments owned by the Rollout
// changing steps in the Rollout Spec would cause multiple experiments to exist which is why it returns an array
func (c *Controller) getExperimentsForRollout(rollout *v1alpha1.Rollout) ([]*v1alpha1.Experiment, error) {
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

func (c *rolloutContext) reconcileExperiments() error {
	if c.pauseContext.IsAborted() || c.rollout.Status.PromoteFull {
		allExs := append(c.otherExs, c.currentEx)
		return c.cancelExperiments(allExs)
	}

	if getPauseCondition(c.rollout, v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		return nil
	}

	step, stepIdx := replicasetutil.GetCurrentCanaryStep(c.rollout)

	currentEx := c.currentEx
	if step != nil && step.Experiment != nil {
		c.log.Infof("Reconciling experiment step (stepIndex: %d)", *stepIdx)
		if currentEx == nil {
			// An new experiment can not be created if the stableRS is not created yet
			if c.stableRS == nil {
				c.log.Infof("Cannot create experiment until stableRS exists")
				return nil
			}

			newEx, err := GetExperimentFromTemplate(c.rollout, c.stableRS, c.newRS)
			if err != nil {
				return err
			}

			currentEx, err = c.createExperimentWithCollisionHandling(newEx)
			if err != nil {
				return err
			}
			c.recorder.Eventf(c.rollout, record.EventOptions{EventReason: "ExperimentCreated"}, "Created Experiment '%s'", currentEx.Name)
		}
		switch currentEx.Status.Phase {
		case v1alpha1.AnalysisPhaseInconclusive:
			c.pauseContext.AddPauseCondition(v1alpha1.PauseReasonInconclusiveExperiment)
		case v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseFailed:
			c.pauseContext.AddAbort(currentEx.Status.Message)
		case v1alpha1.AnalysisPhaseSuccessful:
			// Do not set current Experiment after successful experiment
		default:
			c.SetCurrentExperiment(currentEx)
		}
	}

	otherExs := c.otherExs
	if currentEx != nil && (step == nil || step.Experiment == nil) {
		otherExs = append(otherExs, currentEx)
	}
	err := c.cancelExperiments(otherExs)
	if err != nil {
		return err
	}

	limitSuccessful := defaults.GetAnalysisRunSuccessfulHistoryLimitOrDefault(c.rollout)
	limitUnsuccessful := defaults.GetAnalysisRunUnsuccessfulHistoryLimitOrDefault(c.rollout)
	exsToDelete := experimentutil.FilterExperimentsToDelete(otherExs, c.allRSs, limitSuccessful, limitUnsuccessful)
	err = c.deleteExperiments(exsToDelete)
	if err != nil {
		return err
	}

	return nil
}

// createExperimentWithCollisionHandling creates the given experiment, but with a new name
// in the event that an experiment with the same name already exists
func (c *rolloutContext) createExperimentWithCollisionHandling(newEx *v1alpha1.Experiment) (*v1alpha1.Experiment, error) {
	ctx := context.TODO()
	collisionCount := 1
	baseName := newEx.Name
	for {
		currentEx, err := c.argoprojclientset.ArgoprojV1alpha1().Experiments(newEx.Namespace).Create(ctx, newEx, metav1.CreateOptions{})
		if err == nil {
			return currentEx, nil
		}
		if !k8serrors.IsAlreadyExists(err) {
			return nil, err
		}
		existingEx, err := c.argoprojclientset.ArgoprojV1alpha1().Experiments(newEx.Namespace).Get(ctx, newEx.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		existingEqual := experimentutil.IsSemanticallyEqual(newEx.Spec, existingEx.Spec)
		controllerRef := metav1.GetControllerOf(existingEx)
		controllerUIDEqual := controllerRef != nil && controllerRef.UID == c.rollout.UID
		c.log.Infof("Encountered collision of existing experiment %s (phase: %s, equal: %v, controllerUIDEqual: %v)", existingEx.Name, existingEx.Status.Phase, existingEqual, controllerUIDEqual)
		if !existingEx.Status.Phase.Completed() && existingEqual && controllerUIDEqual {
			// If we get here, the existing experiment has been determined to be our experiment and
			// we likely reconciled the rollout with a stale cache (quite common).
			return existingEx, nil
		}
		newEx.Name = fmt.Sprintf("%s-%d", baseName, collisionCount)
		collisionCount++
	}
}

func (c *rolloutContext) cancelExperiments(exs []*v1alpha1.Experiment) error {
	for i := range exs {
		ex := exs[i]
		if ex == nil {
			continue
		}
		if !ex.Spec.Terminate && !experimentutil.HasFinished(ex) {
			c.log.Infof("Canceling other running experiment '%s' owned by rollout", ex.Name)
			err := experimentutil.Terminate(c.argoprojclientset.ArgoprojV1alpha1().Experiments(ex.Namespace), ex.Name)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *rolloutContext) deleteExperiments(exs []*v1alpha1.Experiment) error {
	ctx := context.TODO()
	for i := range exs {
		ex := exs[i]
		if ex.DeletionTimestamp != nil {
			continue
		}
		c.log.Infof("Trying to cleanup experiment '%s'", ex.Name)
		err := c.argoprojclientset.ArgoprojV1alpha1().Experiments(ex.Namespace).Delete(ctx, ex.Name, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
