package rollout

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	patchtypes "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

const (
	cancelAnalysisRun = `{
		"spec": {
			"terminate": true
		}
	}`
)

// getAnalysisRunsForRollout get all analysisRuns owned by the Rollout
func (c *RolloutController) getAnalysisRunsForRollout(rollout *v1alpha1.Rollout) ([]*v1alpha1.AnalysisRun, error) {
	analysisRuns, err := c.analysisRunLister.AnalysisRuns(rollout.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	ownedByRollout := make([]*v1alpha1.AnalysisRun, 0)
	for i := range analysisRuns {
		e := analysisRuns[i]
		controllerRef := metav1.GetControllerOf(e)
		if controllerRef != nil && controllerRef.UID == rollout.UID {
			ownedByRollout = append(ownedByRollout, e)
		}
	}
	return ownedByRollout, nil
}

func (c *RolloutController) reconcileAnalysisRuns(roCtx *canaryContext) error {
	otherArs := roCtx.OtherAnalysisRuns()
	if roCtx.PauseContext().IsAborted() {
		allArs := append(roCtx.CurrentAnalysisRuns(), otherArs...)
		return c.cancelAnalysisRuns(roCtx, allArs)
	}

	newCurrentAnalysisRuns := []*v1alpha1.AnalysisRun{}

	stepAnalysisRun, err := c.reconcileStepBasedAnalysisRun(roCtx)
	if err != nil {
		return err
	}
	if stepAnalysisRun != nil {
		newCurrentAnalysisRuns = append(newCurrentAnalysisRuns, stepAnalysisRun)
	}

	backgroundAnalysisRun, err := c.reconcileBackgroundAnalysisRun(roCtx)
	if err != nil {
		return err
	}
	if backgroundAnalysisRun != nil {
		newCurrentAnalysisRuns = append(newCurrentAnalysisRuns, backgroundAnalysisRun)
	}
	roCtx.SetCurrentAnalysisRuns(newCurrentAnalysisRuns)

	// Due to the possibility that we are operating on stale/inconsistent data in the informer, it's
	// possible that otherArs includes the current analysis runs that we just created or reclaimed
	// in newCurrentAnalysisRuns, despite the fact that our rollout status did not have those set.
	// To prevent us from terminating the runs that we just created moments ago, rebuild otherArs
	// to ensure it does not include the newly created runs.
	otherArs, _ = analysisutil.FilterAnalysisRuns(otherArs, func(ar *v1alpha1.AnalysisRun) bool {
		for _, curr := range newCurrentAnalysisRuns {
			if ar.Name == curr.Name {
				roCtx.log.Infof("Rescued %s from inadvertent termination", ar.Name)
				return false
			}
		}
		return true
	})

	err = c.cancelAnalysisRuns(roCtx, otherArs)
	if err != nil {
		return err
	}

	allRSs := roCtx.AllRSs()
	arsToDelete := analysisutil.FilterAnalysisRunsToDelete(otherArs, allRSs)
	err = c.deleteAnalysisRuns(roCtx, arsToDelete)
	if err != nil {
		return err
	}

	return nil
}

func (c *RolloutController) reconcileBackgroundAnalysisRun(roCtx *canaryContext) (*v1alpha1.AnalysisRun, error) {
	rollout := roCtx.Rollout()
	newRS := roCtx.NewRS()
	currentArs := roCtx.CurrentAnalysisRuns()
	currentAr := analysisutil.FilterAnalysisRunsByName(currentArs, rollout.Status.Canary.CurrentBackgroundAnalysisRun)
	if rollout.Spec.Strategy.Canary.Analysis == nil {
		err := c.cancelAnalysisRuns(roCtx, []*v1alpha1.AnalysisRun{currentAr})
		return nil, err
	}

	// Do not create a background run if the rollout is completely rolled out or just created
	if rollout.Status.Canary.StableRS == rollout.Status.CurrentPodHash || rollout.Status.CurrentPodHash == "" {
		return nil, nil
	}

	if roCtx.PauseContext().GetPauseCondition(v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		return currentAr, nil
	}
	if currentAr == nil {
		podHash := replicasetutil.GetPodTemplateHash(newRS)
		backgroundLabels := analysisutil.BackgroundLabels(podHash)
		currentAr, err := c.createAnalysisRun(roCtx, rollout.Spec.Strategy.Canary.Analysis, backgroundLabels)
		if err == nil {
			roCtx.Log().WithField(logutil.AnalysisRunKey, currentAr.Name).Info("Created background AnalysisRun")
		}
		return currentAr, err
	}
	switch currentAr.Status.Phase {
	case v1alpha1.AnalysisPhaseInconclusive:
		roCtx.PauseContext().AddPauseCondition(v1alpha1.PauseReasonInconclusiveAnalysis)
	case v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseFailed:
		roCtx.PauseContext().AddAbort()
	}
	return currentAr, nil
}

func (c *RolloutController) createAnalysisRun(roCtx *canaryContext, rolloutAnalysisStep *v1alpha1.RolloutAnalysisStep, labels map[string]string) (*v1alpha1.AnalysisRun, error) {
	newRS := roCtx.NewRS()
	stableRS := roCtx.StableRS()
	args := analysisutil.BuildArgumentsForRolloutAnalysisRun(rolloutAnalysisStep, stableRS, newRS)
	podHash := replicasetutil.GetPodTemplateHash(newRS)
	if podHash == "" {
		return nil, fmt.Errorf("Latest ReplicaSet '%s' has no pod hash in the labels", newRS.Name)
	}
	ar, err := c.newAnalysisRunFromRollout(roCtx, rolloutAnalysisStep, args, podHash, labels)
	if err != nil {
		return nil, err
	}
	analysisRunIf := c.argoprojclientset.ArgoprojV1alpha1().AnalysisRuns(roCtx.Rollout().Namespace)
	return analysisutil.CreateWithCollisionCounter(roCtx.Log(), analysisRunIf, *ar)
}

func (c *RolloutController) reconcileStepBasedAnalysisRun(roCtx *canaryContext) (*v1alpha1.AnalysisRun, error) {
	rollout := roCtx.Rollout()
	currentArs := roCtx.CurrentAnalysisRuns()
	newRS := roCtx.NewRS()
	step, index := replicasetutil.GetCurrentCanaryStep(rollout)
	currentAr := analysisutil.FilterAnalysisRunsByName(currentArs, rollout.Status.Canary.CurrentStepAnalysisRun)

	if roCtx.PauseContext().GetPauseCondition(v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		return currentAr, nil
	}

	if step == nil || step.Analysis == nil || index == nil {
		err := c.cancelAnalysisRuns(roCtx, []*v1alpha1.AnalysisRun{currentAr})
		return nil, err
	}
	if currentAr == nil {
		podHash := replicasetutil.GetPodTemplateHash(newRS)
		stepLabels := analysisutil.StepLabels(*index, podHash)
		currentAr, err := c.createAnalysisRun(roCtx, step.Analysis, stepLabels)
		if err == nil {
			roCtx.Log().WithField(logutil.AnalysisRunKey, currentAr.Name).Infof("Created AnalysisRun for step '%d'", *index)
		}
		return currentAr, err
	}

	switch currentAr.Status.Phase {
	case v1alpha1.AnalysisPhaseInconclusive:
		roCtx.PauseContext().AddPauseCondition(v1alpha1.PauseReasonInconclusiveAnalysis)
	case v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseFailed:
		roCtx.PauseContext().AddAbort()
	}

	return currentAr, nil
}

func (c *RolloutController) cancelAnalysisRuns(roCtx *canaryContext, analysisRuns []*v1alpha1.AnalysisRun) error {
	logctx := roCtx.Log()
	for i := range analysisRuns {
		ar := analysisRuns[i]
		isNotCompleted := ar == nil || !ar.Status.Phase.Completed()
		if ar != nil && !ar.Spec.Terminate && isNotCompleted {
			logctx.WithField(logutil.AnalysisRunKey, ar.Name).Infof("Canceling the analysis run '%s'", ar.Name)
			_, err := c.argoprojclientset.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Patch(ar.Name, patchtypes.MergePatchType, []byte(cancelAnalysisRun))
			if err != nil {
				if k8serrors.IsNotFound(err) {
					logctx.Warnf("AnalysisRun '%s' not found", ar.Name)
					continue
				}
				return err
			}
		}
	}
	return nil
}

// newAnalysisRunFromRollout generates an AnalysisRun from the rollouts, the AnalysisRun Step, the new/stable ReplicaSet, and any extra objects.
func (c *RolloutController) newAnalysisRunFromRollout(roCtx *canaryContext, rolloutAnalysisStep *v1alpha1.RolloutAnalysisStep, args []v1alpha1.Argument, podHash string, labels map[string]string) (*v1alpha1.AnalysisRun, error) {
	r := roCtx.Rollout()
	logctx := roCtx.Log()
	template, err := c.analysisTemplateLister.AnalysisTemplates(r.Namespace).Get(rolloutAnalysisStep.TemplateName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logctx.Warnf("AnalysisTemplate '%s' not found", rolloutAnalysisStep.TemplateName)
		}
		return nil, err
	}
	newArgs, err := analysisutil.MergeArgs(args, template.Spec.Args)
	if err != nil {
		return nil, err
	}

	revision := r.Annotations[annotations.RevisionAnnotation]
	ar := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			// TODO(jessesuen): consider incorporating the step index into the name like we do for experiments
			Name:      fmt.Sprintf("%s-%s-%s-%s", r.Name, podHash, revision, rolloutAnalysisStep.Name),
			Namespace: r.Namespace,
			Labels:    labels,
			Annotations: map[string]string{
				annotations.RevisionAnnotation: revision,
			},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(r, controllerKind)},
		},
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: template.Spec.Metrics,
			Args:    newArgs,
		},
	}
	return &ar, nil
}

func (c *RolloutController) deleteAnalysisRuns(roCtx rolloutContext, ars []*v1alpha1.AnalysisRun) error {
	for i := range ars {
		ar := ars[i]
		if ar.DeletionTimestamp != nil {
			continue
		}
		roCtx.Log().Infof("Trying to cleanup analysis run '%s'", ar.Name)
		err := c.argoprojclientset.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Delete(ar.Name, nil)
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
