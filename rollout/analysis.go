package rollout

import (
	"fmt"
	"strconv"
	"strings"

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

func (c *RolloutController) reconcileAnalysisRuns(roCtx rolloutContext) error {
	otherArs := roCtx.OtherAnalysisRuns()
	if roCtx.PauseContext().IsAborted() {
		allArs := append(roCtx.CurrentAnalysisRuns(), otherArs...)
		return c.cancelAnalysisRuns(roCtx, allArs)
	}

	newCurrentAnalysisRuns := []*v1alpha1.AnalysisRun{}
	rollout := roCtx.Rollout()
	if rollout.Spec.Strategy.Canary != nil {
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
	}
	if rollout.Spec.Strategy.BlueGreen != nil {
		prePromotionAr, err := c.reconcilePrePromotionAnalysisRun(roCtx)
		if err != nil {
			return err
		}
		if prePromotionAr != nil {
			newCurrentAnalysisRuns = append(newCurrentAnalysisRuns, prePromotionAr)
		}
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
				roCtx.Log().Infof("Rescued %s from inadvertent termination", ar.Name)
				return false
			}
		}
		return true
	})

	err := c.cancelAnalysisRuns(roCtx, otherArs)
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

func (c *RolloutController) reconcilePrePromotionAnalysisRun(roCtx rolloutContext) (*v1alpha1.AnalysisRun, error) {
	rollout := roCtx.Rollout()
	newRS := roCtx.NewRS()
	currentArs := roCtx.CurrentAnalysisRuns()
	currentAr := analysisutil.FilterAnalysisRunsByName(currentArs, rollout.Status.BlueGreen.PrePromotionAnalysisRun)
	if rollout.Spec.Strategy.BlueGreen.PrePromotionAnalysis == nil {
		err := c.cancelAnalysisRuns(roCtx, []*v1alpha1.AnalysisRun{currentAr})
		return nil, err
	}
	roCtx.Log().Info("Reconciling Pre Promotion Analysis")

	activeSelector := rollout.Status.BlueGreen.ActiveSelector
	currentPodHash := rollout.Status.CurrentPodHash
	// Do not create an analysis run if the rollout is active promotion happened, the rollout was just created, the newRS is not saturated
	if activeSelector == "" || activeSelector == rollout.Status.CurrentPodHash || currentPodHash == "" || !annotations.IsSaturated(rollout, newRS) {
		err := c.cancelAnalysisRuns(roCtx, []*v1alpha1.AnalysisRun{currentAr})
		return nil, err
	}

	if getPauseCondition(rollout, v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		return currentAr, nil
	}

	if currentAr == nil {
		podHash := replicasetutil.GetPodTemplateHash(newRS)
		instanceID := analysisutil.GetInstanceID(rollout)
		prePromotionLabels := analysisutil.PrePromotionLabels(podHash, instanceID)
		currentAr, err := c.createAnalysisRun(roCtx, rollout.Spec.Strategy.BlueGreen.PrePromotionAnalysis, nil, prePromotionLabels)
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

func (c *RolloutController) reconcileBackgroundAnalysisRun(roCtx rolloutContext) (*v1alpha1.AnalysisRun, error) {
	rollout := roCtx.Rollout()
	newRS := roCtx.NewRS()
	currentArs := roCtx.CurrentAnalysisRuns()
	currentAr := analysisutil.FilterAnalysisRunsByName(currentArs, rollout.Status.Canary.CurrentBackgroundAnalysisRun)
	if rollout.Spec.Strategy.Canary.Analysis == nil {
		err := c.cancelAnalysisRuns(roCtx, []*v1alpha1.AnalysisRun{currentAr})
		return nil, err
	}

	// Do not create a background run if the rollout is completely rolled out, just created, before the starting step
	if rollout.Status.StableRS == rollout.Status.CurrentPodHash || rollout.Status.CurrentPodHash == "" || replicasetutil.BeforeStartingStep(rollout) {
		return nil, nil
	}

	if getPauseCondition(rollout, v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		return currentAr, nil
	}

	if currentAr == nil {
		podHash := replicasetutil.GetPodTemplateHash(newRS)
		instanceID := analysisutil.GetInstanceID(rollout)
		backgroundLabels := analysisutil.BackgroundLabels(podHash, instanceID)
		currentAr, err := c.createAnalysisRun(roCtx, &rollout.Spec.Strategy.Canary.Analysis.RolloutAnalysis, nil, backgroundLabels)
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

func (c *RolloutController) createAnalysisRun(roCtx rolloutContext, rolloutAnalysis *v1alpha1.RolloutAnalysis, stepIdx *int32, labels map[string]string) (*v1alpha1.AnalysisRun, error) {
	newRS := roCtx.NewRS()
	stableRS := roCtx.StableRS()
	args := analysisutil.BuildArgumentsForRolloutAnalysisRun(rolloutAnalysis.Args, stableRS, newRS)
	podHash := replicasetutil.GetPodTemplateHash(newRS)
	if podHash == "" {
		return nil, fmt.Errorf("Latest ReplicaSet '%s' has no pod hash in the labels", newRS.Name)
	}
	ar, err := c.newAnalysisRunFromRollout(roCtx, rolloutAnalysis, args, podHash, stepIdx, labels)
	if err != nil {
		return nil, err
	}
	analysisRunIf := c.argoprojclientset.ArgoprojV1alpha1().AnalysisRuns(roCtx.Rollout().Namespace)
	return analysisutil.CreateWithCollisionCounter(roCtx.Log(), analysisRunIf, *ar)
}

func (c *RolloutController) reconcileStepBasedAnalysisRun(roCtx rolloutContext) (*v1alpha1.AnalysisRun, error) {
	rollout := roCtx.Rollout()
	currentArs := roCtx.CurrentAnalysisRuns()
	newRS := roCtx.NewRS()
	step, index := replicasetutil.GetCurrentCanaryStep(rollout)
	currentAr := analysisutil.FilterAnalysisRunsByName(currentArs, rollout.Status.Canary.CurrentStepAnalysisRun)

	if getPauseCondition(rollout, v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		return currentAr, nil
	}

	if step == nil || step.Analysis == nil || index == nil {
		err := c.cancelAnalysisRuns(roCtx, []*v1alpha1.AnalysisRun{currentAr})
		return nil, err
	}
	if currentAr == nil {
		podHash := replicasetutil.GetPodTemplateHash(newRS)
		instanceID := analysisutil.GetInstanceID(rollout)
		stepLabels := analysisutil.StepLabels(*index, podHash, instanceID)
		currentAr, err := c.createAnalysisRun(roCtx, step.Analysis, index, stepLabels)
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

func (c *RolloutController) cancelAnalysisRuns(roCtx rolloutContext, analysisRuns []*v1alpha1.AnalysisRun) error {
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
func (c *RolloutController) newAnalysisRunFromRollout(roCtx rolloutContext, rolloutAnalysis *v1alpha1.RolloutAnalysis, args []v1alpha1.Argument, podHash string, stepIdx *int32, labels map[string]string) (*v1alpha1.AnalysisRun, error) {
	r := roCtx.Rollout()
	logctx := roCtx.Log()
	revision := r.Annotations[annotations.RevisionAnnotation]
	nameParts := []string{r.Name, podHash, revision}
	if stepIdx != nil {
		nameParts = append(nameParts, strconv.Itoa(int(*stepIdx)))
	}
	if rolloutAnalysis.TemplateName != "" {
		//TODO(dthomson) remove this code block in v0.9.0
		nameParts = append(nameParts, rolloutAnalysis.TemplateName)
	}
	name := strings.Join(nameParts, "-")
	var run *v1alpha1.AnalysisRun
	var err error
	if rolloutAnalysis.TemplateName != "" {
		//TODO(dthomson) remove this code block in v0.9.0
		template, err := c.analysisTemplateLister.AnalysisTemplates(r.Namespace).Get(rolloutAnalysis.TemplateName)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logctx.Warnf("AnalysisTemplate '%s' not found", rolloutAnalysis.TemplateName)
			}
			return nil, err
		}
		run, err = analysisutil.NewAnalysisRunFromTemplate(template, args, name, "", r.Namespace)
		if err != nil {
			return nil, err
		}
	} else {
		templates := make([]*v1alpha1.AnalysisTemplate, 0)
		for _, templateRef := range rolloutAnalysis.Templates {
			template, err := c.analysisTemplateLister.AnalysisTemplates(r.Namespace).Get(templateRef.TemplateName)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					logctx.Warnf("AnalysisTemplate '%s' not found", rolloutAnalysis.TemplateName)
				}
				return nil, err
			}
			templates = append(templates, template)
		}
		run, err = analysisutil.NewAnalysisRunFromTemplates(templates, args, name, "", r.Namespace)
		if err != nil {
			return nil, err
		}
	}
	run.Labels = labels
	run.Annotations = map[string]string{
		annotations.RevisionAnnotation: revision,
	}
	run.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(r, controllerKind)}
	return run, nil
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
