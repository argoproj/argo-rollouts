package rollout

import (
	"fmt"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
func (c *Controller) getAnalysisRunsForRollout(rollout *v1alpha1.Rollout) ([]*v1alpha1.AnalysisRun, error) {
	analysisRuns, err := c.analysisRunLister.AnalysisRuns(rollout.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	ownedByRollout := make([]*v1alpha1.AnalysisRun, 0)
	seen := make(map[string]bool)
	for i := range analysisRuns {
		e := analysisRuns[i]
		controllerRef := metav1.GetControllerOf(e)
		if controllerRef != nil && controllerRef.UID == rollout.UID {
			ownedByRollout = append(ownedByRollout, e)
			seen[e.Name] = true
		}
	}
	arStatuses := []*v1alpha1.RolloutAnalysisRunStatus{
		rollout.Status.Canary.CurrentBackgroundAnalysisRunStatus,
		rollout.Status.Canary.CurrentStepAnalysisRunStatus,
		rollout.Status.BlueGreen.PrePromotionAnalysisRunStatus,
		rollout.Status.BlueGreen.PostPromotionAnalysisRunStatus,
	}
	for _, arStatus := range arStatuses {
		if arStatus == nil || seen[arStatus.Name] {
			continue
		}
		// We recorded a run in the rollout status, but it didn't appear in the lister.
		// Perform a get to see if it truly exists.
		ar, err := c.argoprojclientset.ArgoprojV1alpha1().AnalysisRuns(rollout.Namespace).Get(arStatus.Name, metav1.GetOptions{})
		if err == nil && ar != nil {
			logutil.WithRollout(rollout).Infof("Found analysis run '%s' missing from informer cache", ar.Name)
			ownedByRollout = append(ownedByRollout, ar)
		}
	}
	return ownedByRollout, nil
}

func (c *Controller) reconcileAnalysisRuns(roCtx rolloutContext) error {
	otherArs := roCtx.OtherAnalysisRuns()
	if roCtx.PauseContext().IsAborted() {
		allArs := append(roCtx.CurrentAnalysisRuns().ToArray(), otherArs...)
		roCtx.SetCurrentAnalysisRuns(roCtx.CurrentAnalysisRuns())
		return c.cancelAnalysisRuns(roCtx, allArs)
	}

	newCurrentAnalysisRuns := analysisutil.CurrentAnalysisRuns{}
	rollout := roCtx.Rollout()
	if rollout.Spec.Strategy.Canary != nil {
		stepAnalysisRun, err := c.reconcileStepBasedAnalysisRun(roCtx)
		if err != nil {
			return err
		}
		newCurrentAnalysisRuns.CanaryStep = stepAnalysisRun

		backgroundAnalysisRun, err := c.reconcileBackgroundAnalysisRun(roCtx)
		if err != nil {
			return err
		}
		newCurrentAnalysisRuns.CanaryBackground = backgroundAnalysisRun

	}
	if rollout.Spec.Strategy.BlueGreen != nil {
		prePromotionAr, err := c.reconcilePrePromotionAnalysisRun(roCtx)
		if err != nil {
			return err
		}
		c.setPauseOrAbort(roCtx, prePromotionAr)
		newCurrentAnalysisRuns.BlueGreenPrePromotion = prePromotionAr

		postPromotionAr, err := c.reconcilePostPromotionAnalysisRun(roCtx)
		if err != nil {
			return err
		}
		c.setPauseOrAbort(roCtx, postPromotionAr)
		newCurrentAnalysisRuns.BlueGreenPostPromotion = postPromotionAr
	}
	roCtx.SetCurrentAnalysisRuns(newCurrentAnalysisRuns)

	// Due to the possibility that we are operating on stale/inconsistent data in the informer, it's
	// possible that otherArs includes the current analysis runs that we just created or reclaimed
	// in newCurrentAnalysisRuns, despite the fact that our rollout status did not have those set.
	// To prevent us from terminating the runs that we just created moments ago, rebuild otherArs
	// to ensure it does not include the newly created runs.
	otherArs, _ = analysisutil.FilterAnalysisRuns(otherArs, func(ar *v1alpha1.AnalysisRun) bool {
		for _, curr := range newCurrentAnalysisRuns.ToArray() {
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

	c.reconcileAnalysisRunStatusChanges(roCtx, newCurrentAnalysisRuns)
	return nil
}

func (c *Controller) setPauseOrAbort(roCtx rolloutContext, ar *v1alpha1.AnalysisRun) {
	if ar == nil {
		return
	}
	switch ar.Status.Phase {
	case v1alpha1.AnalysisPhaseInconclusive:
		roCtx.PauseContext().AddPauseCondition(v1alpha1.PauseReasonInconclusiveAnalysis)
	case v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseFailed:
		roCtx.PauseContext().AddAbort(ar.Status.Message)
	}
}

func needsNewAnalysisRun(currentAr *v1alpha1.AnalysisRun, rollout *v1alpha1.Rollout) bool {
	if currentAr == nil {
		return true
	}

	// Here the controller is checking that rollout has already paused for a inconclusive analysis run. If it has paused
	// for the inconclusive analysisrun, the controller needs to create a new AnalysisRun. Otherwise, the controller has
	// not processed the inconclusive run yet (and needs to add a pause). It checks this by seeing if the controllerPause
	// is set and then seeing if the last status was inconclusive.
	// There is an additional check for the BlueGreen Pause because the prepromotion analysis always has the BlueGreen
	// Pause and that causes controllerPause to be set. The extra check for the BlueGreen Pause ensures that a new Analysis
	// Run is created only when the previous AnalysisRun is inconclusive
	if rollout.Status.ControllerPause && getPauseCondition(rollout, v1alpha1.PauseReasonBlueGreenPause) == nil {
		return currentAr.Status.Phase == v1alpha1.AnalysisPhaseInconclusive
	}
	return rollout.Status.AbortedAt != nil
}

// emitAnalysisRunStatusChanges emits a Kubernetes event if the analysis run of that type has changed status
func (c *Controller) emitAnalysisRunStatusChanges(r *v1alpha1.Rollout, prevStatus *v1alpha1.RolloutAnalysisRunStatus, ar *v1alpha1.AnalysisRun, arType string) {
	if ar != nil {
		if prevStatus == nil || prevStatus.Name == ar.Name && prevStatus.Status != ar.Status.Phase {
			prevStatusStr := "NoPreviousStatus"
			if prevStatus != nil {
				prevStatusStr = string(prevStatus.Status)
			}

			eventType := corev1.EventTypeNormal
			if ar.Status.Phase == v1alpha1.AnalysisPhaseFailed || ar.Status.Phase == v1alpha1.AnalysisPhaseError {
				eventType = corev1.EventTypeWarning
			}
			msg := fmt.Sprintf("%s Analysis Run '%s' Status New: '%s' Previous: '%s'", arType, ar.Name, ar.Status.Phase, prevStatusStr)
			c.recorder.Event(r, eventType, "AnalysisRunStatusChange", msg)
		}
	}
}

// reconcileAnalysisRunStatusChanges for each analysisRun type, the controller checks if the analysis run status has changed
// for that type
func (c *Controller) reconcileAnalysisRunStatusChanges(ctx rolloutContext, currARs analysisutil.CurrentAnalysisRuns) {
	rollout := ctx.Rollout()
	c.emitAnalysisRunStatusChanges(
		rollout,
		rollout.Status.BlueGreen.PostPromotionAnalysisRunStatus,
		currARs.BlueGreenPostPromotion,
		v1alpha1.RolloutTypePostPromotionLabel,
	)

	c.emitAnalysisRunStatusChanges(
		rollout,
		rollout.Status.BlueGreen.PrePromotionAnalysisRunStatus,
		currARs.BlueGreenPrePromotion,
		v1alpha1.RolloutTypePrePromotionLabel,
	)

	c.emitAnalysisRunStatusChanges(
		rollout,
		rollout.Status.Canary.CurrentStepAnalysisRunStatus,
		currARs.CanaryStep,
		v1alpha1.RolloutTypeStepLabel,
	)

	c.emitAnalysisRunStatusChanges(
		rollout,
		rollout.Status.Canary.CurrentBackgroundAnalysisRunStatus,
		currARs.CanaryBackground,
		v1alpha1.RolloutTypeBackgroundRunLabel,
	)
}

func (c *Controller) reconcilePrePromotionAnalysisRun(roCtx rolloutContext) (*v1alpha1.AnalysisRun, error) {
	rollout := roCtx.Rollout()
	newRS := roCtx.NewRS()
	currentArs := roCtx.CurrentAnalysisRuns()
	currentAr := currentArs.BlueGreenPrePromotion
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
		return currentAr, err
	}

	if getPauseCondition(rollout, v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		return currentAr, nil
	}

	if needsNewAnalysisRun(currentAr, rollout) {
		podHash := replicasetutil.GetPodTemplateHash(newRS)
		instanceID := analysisutil.GetInstanceID(rollout)
		prePromotionLabels := analysisutil.PrePromotionLabels(podHash, instanceID)
		currentAr, err := c.createAnalysisRun(roCtx, rollout.Spec.Strategy.BlueGreen.PrePromotionAnalysis, "pre", prePromotionLabels)
		if err == nil {
			roCtx.Log().WithField(logutil.AnalysisRunKey, currentAr.Name).Info("Created Pre Promotion AnalysisRun")
		}
		return currentAr, err
	}
	return currentAr, nil
}

// needPostPromotionAnalysisRun indicates if the controller needs to create an analysis run by checking that the desired
// ReplicaSet is the stable ReplicaSet, the active service promotion has not happened, the rollout was just created, or
// the newRS is not saturated
func needPostPromotionAnalysisRun(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet) bool {
	currentPodHash := rollout.Status.CurrentPodHash
	activeSelector := rollout.Status.BlueGreen.ActiveSelector
	return rollout.Status.StableRS == currentPodHash || activeSelector != currentPodHash || currentPodHash == "" || !annotations.IsSaturated(rollout, newRS)
}

func (c *Controller) reconcilePostPromotionAnalysisRun(roCtx rolloutContext) (*v1alpha1.AnalysisRun, error) {
	rollout := roCtx.Rollout()
	newRS := roCtx.NewRS()
	currentArs := roCtx.CurrentAnalysisRuns()
	currentAr := currentArs.BlueGreenPostPromotion
	if rollout.Spec.Strategy.BlueGreen.PostPromotionAnalysis == nil {
		err := c.cancelAnalysisRuns(roCtx, []*v1alpha1.AnalysisRun{currentAr})
		return nil, err
	}
	roCtx.Log().Info("Reconciling Post Promotion Analysis")

	if needPostPromotionAnalysisRun(rollout, newRS) {
		err := c.cancelAnalysisRuns(roCtx, []*v1alpha1.AnalysisRun{currentAr})
		return nil, err
	}

	if getPauseCondition(rollout, v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		return currentAr, nil
	}

	if needsNewAnalysisRun(currentAr, rollout) {
		podHash := replicasetutil.GetPodTemplateHash(newRS)
		instanceID := analysisutil.GetInstanceID(rollout)
		postPromotionLabels := analysisutil.PostPromotionLabels(podHash, instanceID)
		currentAr, err := c.createAnalysisRun(roCtx, rollout.Spec.Strategy.BlueGreen.PostPromotionAnalysis, "post", postPromotionLabels)
		if err == nil {
			roCtx.Log().WithField(logutil.AnalysisRunKey, currentAr.Name).Info("Created Post Promotion AnalysisRun")
		}
		return currentAr, err
	}
	return currentAr, nil
}

func (c *Controller) reconcileBackgroundAnalysisRun(roCtx rolloutContext) (*v1alpha1.AnalysisRun, error) {
	rollout := roCtx.Rollout()
	newRS := roCtx.NewRS()
	currentArs := roCtx.CurrentAnalysisRuns()
	currentAr := currentArs.CanaryBackground
	if rollout.Spec.Strategy.Canary.Analysis == nil {
		err := c.cancelAnalysisRuns(roCtx, []*v1alpha1.AnalysisRun{currentAr})
		return nil, err
	}

	// Do not create a background run if the rollout is completely rolled out, just created, before the starting step
	if rollout.Status.StableRS == rollout.Status.CurrentPodHash || rollout.Status.StableRS == "" || rollout.Status.CurrentPodHash == "" || replicasetutil.BeforeStartingStep(rollout) {
		return nil, nil
	}

	if getPauseCondition(rollout, v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		return currentAr, nil
	}

	if needsNewAnalysisRun(currentAr, rollout) {
		podHash := replicasetutil.GetPodTemplateHash(newRS)
		instanceID := analysisutil.GetInstanceID(rollout)
		backgroundLabels := analysisutil.BackgroundLabels(podHash, instanceID)
		currentAr, err := c.createAnalysisRun(roCtx, &rollout.Spec.Strategy.Canary.Analysis.RolloutAnalysis, "", backgroundLabels)
		if err == nil {
			roCtx.Log().WithField(logutil.AnalysisRunKey, currentAr.Name).Info("Created background AnalysisRun")
		}
		return currentAr, err
	}
	switch currentAr.Status.Phase {
	case v1alpha1.AnalysisPhaseInconclusive:
		roCtx.PauseContext().AddPauseCondition(v1alpha1.PauseReasonInconclusiveAnalysis)
	case v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseFailed:
		roCtx.PauseContext().AddAbort(currentAr.Status.Message)
	}
	return currentAr, nil
}

func (c *Controller) createAnalysisRun(roCtx rolloutContext, rolloutAnalysis *v1alpha1.RolloutAnalysis, infix string, labels map[string]string) (*v1alpha1.AnalysisRun, error) {
	newRS := roCtx.NewRS()
	stableRS := roCtx.StableRS()
	args := analysisutil.BuildArgumentsForRolloutAnalysisRun(rolloutAnalysis.Args, stableRS, newRS)
	podHash := replicasetutil.GetPodTemplateHash(newRS)
	if podHash == "" {
		return nil, fmt.Errorf("Latest ReplicaSet '%s' has no pod hash in the labels", newRS.Name)
	}
	ar, err := c.newAnalysisRunFromRollout(roCtx, rolloutAnalysis, args, podHash, infix, labels)
	if err != nil {
		return nil, err
	}
	analysisRunIf := c.argoprojclientset.ArgoprojV1alpha1().AnalysisRuns(roCtx.Rollout().Namespace)
	return analysisutil.CreateWithCollisionCounter(roCtx.Log(), analysisRunIf, *ar)
}

func (c *Controller) reconcileStepBasedAnalysisRun(roCtx rolloutContext) (*v1alpha1.AnalysisRun, error) {
	rollout := roCtx.Rollout()
	currentArs := roCtx.CurrentAnalysisRuns()
	newRS := roCtx.NewRS()
	step, index := replicasetutil.GetCurrentCanaryStep(rollout)
	currentAr := currentArs.CanaryStep

	if len(rollout.Status.PauseConditions) > 0 || rollout.Status.Abort {
		return currentAr, nil
	}

	if step == nil || step.Analysis == nil || index == nil {
		err := c.cancelAnalysisRuns(roCtx, []*v1alpha1.AnalysisRun{currentAr})
		return nil, err
	}
	if needsNewAnalysisRun(currentAr, rollout) {
		podHash := replicasetutil.GetPodTemplateHash(newRS)
		instanceID := analysisutil.GetInstanceID(rollout)
		stepLabels := analysisutil.StepLabels(*index, podHash, instanceID)
		currentAr, err := c.createAnalysisRun(roCtx, step.Analysis, strconv.Itoa(int(*index)), stepLabels)
		if err == nil {
			roCtx.Log().WithField(logutil.AnalysisRunKey, currentAr.Name).Infof("Created AnalysisRun for step '%d'", *index)
		}
		return currentAr, err
	}

	switch currentAr.Status.Phase {
	case v1alpha1.AnalysisPhaseInconclusive:
		roCtx.PauseContext().AddPauseCondition(v1alpha1.PauseReasonInconclusiveAnalysis)
	case v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseFailed:
		roCtx.PauseContext().AddAbort(currentAr.Status.Message)
	}

	return currentAr, nil
}

func (c *Controller) cancelAnalysisRuns(roCtx rolloutContext, analysisRuns []*v1alpha1.AnalysisRun) error {
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
func (c *Controller) newAnalysisRunFromRollout(roCtx rolloutContext, rolloutAnalysis *v1alpha1.RolloutAnalysis, args []v1alpha1.Argument, podHash string, infix string, labels map[string]string) (*v1alpha1.AnalysisRun, error) {
	r := roCtx.Rollout()
	logctx := roCtx.Log()
	revision := r.Annotations[annotations.RevisionAnnotation]
	nameParts := []string{r.Name, podHash, revision}
	if infix != "" {
		nameParts = append(nameParts, infix)
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
		clusterTemplates := make([]*v1alpha1.ClusterAnalysisTemplate, 0)
		for _, templateRef := range rolloutAnalysis.Templates {

			if templateRef.ClusterScope {
				template, err := c.clusterAnalysisTemplateLister.Get(templateRef.TemplateName)
				if err != nil {
					if k8serrors.IsNotFound(err) {
						logctx.Warnf("ClusterAnalysisTemplate '%s' not found", rolloutAnalysis.TemplateName)
					}
					return nil, err
				}
				clusterTemplates = append(clusterTemplates, template)
			} else {
				template, err := c.analysisTemplateLister.AnalysisTemplates(r.Namespace).Get(templateRef.TemplateName)
				if err != nil {
					if k8serrors.IsNotFound(err) {
						logctx.Warnf("AnalysisTemplate '%s' not found", rolloutAnalysis.TemplateName)
					}
					return nil, err
				}
				templates = append(templates, template)
			}

		}
		run, err = analysisutil.NewAnalysisRunFromTemplates(templates, clusterTemplates, args, name, "", r.Namespace)
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

func (c *Controller) deleteAnalysisRuns(roCtx rolloutContext, ars []*v1alpha1.AnalysisRun) error {
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
