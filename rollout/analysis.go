package rollout

import (
	"context"
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
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	rolloututil "github.com/argoproj/argo-rollouts/utils/rollout"
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
	ctx := context.TODO()
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
		ar, err := c.argoprojclientset.ArgoprojV1alpha1().AnalysisRuns(rollout.Namespace).Get(ctx, arStatus.Name, metav1.GetOptions{})
		if err == nil && ar != nil {
			logutil.WithRollout(rollout).Infof("Found analysis run '%s' missing from informer cache", ar.Name)
			ownedByRollout = append(ownedByRollout, ar)
		}
	}
	return ownedByRollout, nil
}

func (c *rolloutContext) reconcileAnalysisRuns() error {
	isAborted := c.pauseContext.IsAborted()
	rollbackToScaleDownDelay := replicasetutil.HasScaleDownDeadline(c.newRS)
	initialDeploy := c.rollout.Status.StableRS == ""
	if isAborted || c.rollout.Status.PromoteFull || rollbackToScaleDownDelay || initialDeploy {
		c.log.Infof("Skipping analysis: isAborted: %v, promoteFull: %v, rollbackToScaleDownDelay: %v, initialDeploy: %v", isAborted, c.rollout.Status.PromoteFull, rollbackToScaleDownDelay, initialDeploy)
		allArs := append(c.currentArs.ToArray(), c.otherArs...)
		c.SetCurrentAnalysisRuns(c.currentArs)
		return c.cancelAnalysisRuns(allArs)
	}

	newCurrentAnalysisRuns := analysisutil.CurrentAnalysisRuns{}
	if c.rollout.Spec.Strategy.Canary != nil {
		stepAnalysisRun, err := c.reconcileStepBasedAnalysisRun()
		if err != nil {
			return err
		}
		newCurrentAnalysisRuns.CanaryStep = stepAnalysisRun

		backgroundAnalysisRun, err := c.reconcileBackgroundAnalysisRun()
		if err != nil {
			return err
		}
		newCurrentAnalysisRuns.CanaryBackground = backgroundAnalysisRun

	}
	if c.rollout.Spec.Strategy.BlueGreen != nil {
		prePromotionAr, err := c.reconcilePrePromotionAnalysisRun()
		if err != nil {
			return err
		}
		c.setPauseOrAbort(prePromotionAr)
		newCurrentAnalysisRuns.BlueGreenPrePromotion = prePromotionAr

		postPromotionAr, err := c.reconcilePostPromotionAnalysisRun()
		if err != nil {
			return err
		}
		c.setPauseOrAbort(postPromotionAr)
		newCurrentAnalysisRuns.BlueGreenPostPromotion = postPromotionAr
	}
	c.SetCurrentAnalysisRuns(newCurrentAnalysisRuns)

	// Due to the possibility that we are operating on stale/inconsistent data in the informer, it's
	// possible that otherArs includes the current analysis runs that we just created or reclaimed
	// in newCurrentAnalysisRuns, despite the fact that our rollout status did not have those set.
	// To prevent us from terminating the runs that we just created moments ago, rebuild otherArs
	// to ensure it does not include the newly created runs.
	otherArs, _ := analysisutil.FilterAnalysisRuns(c.otherArs, func(ar *v1alpha1.AnalysisRun) bool {
		for _, curr := range newCurrentAnalysisRuns.ToArray() {
			if ar.Name == curr.Name {
				c.log.Infof("Rescued %s from inadvertent termination", ar.Name)
				return false
			}
		}
		return true
	})

	err := c.cancelAnalysisRuns(otherArs)
	if err != nil {
		return err
	}

	limitSucceedArs := defaults.GetAnalysisRunSuccessfulHistoryLimitOrDefault(c.rollout)
	limitFailedArs := defaults.GetAnalysisRunUnsuccessfulHistoryLimitOrDefault(c.rollout)
	arsToDelete := analysisutil.FilterAnalysisRunsToDelete(otherArs, c.allRSs, limitSucceedArs, limitFailedArs)
	err = c.deleteAnalysisRuns(arsToDelete)
	if err != nil {
		return err
	}

	c.reconcileAnalysisRunStatusChanges(newCurrentAnalysisRuns)
	return nil
}

func (c *rolloutContext) setPauseOrAbort(ar *v1alpha1.AnalysisRun) {
	if ar == nil {
		return
	}
	switch ar.Status.Phase {
	case v1alpha1.AnalysisPhaseInconclusive:
		c.pauseContext.AddPauseCondition(v1alpha1.PauseReasonInconclusiveAnalysis)
	case v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseFailed:
		c.pauseContext.AddAbort(ar.Status.Message)
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
func (c *rolloutContext) emitAnalysisRunStatusChanges(prevStatus *v1alpha1.RolloutAnalysisRunStatus, ar *v1alpha1.AnalysisRun, arType string) {
	if ar != nil && ar.Status.Phase != "" {
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
			c.recorder.Eventf(c.rollout, record.EventOptions{EventType: eventType, EventReason: "AnalysisRun" + string(ar.Status.Phase)}, msg)
		}
	}
}

// reconcileAnalysisRunStatusChanges for each analysisRun type, the controller checks if the analysis run status has changed
// for that type
func (c *rolloutContext) reconcileAnalysisRunStatusChanges(currARs analysisutil.CurrentAnalysisRuns) {
	c.emitAnalysisRunStatusChanges(
		c.rollout.Status.BlueGreen.PostPromotionAnalysisRunStatus,
		currARs.BlueGreenPostPromotion,
		v1alpha1.RolloutTypePostPromotionLabel,
	)

	c.emitAnalysisRunStatusChanges(
		c.rollout.Status.BlueGreen.PrePromotionAnalysisRunStatus,
		currARs.BlueGreenPrePromotion,
		v1alpha1.RolloutTypePrePromotionLabel,
	)

	c.emitAnalysisRunStatusChanges(
		c.rollout.Status.Canary.CurrentStepAnalysisRunStatus,
		currARs.CanaryStep,
		v1alpha1.RolloutTypeStepLabel,
	)

	c.emitAnalysisRunStatusChanges(
		c.rollout.Status.Canary.CurrentBackgroundAnalysisRunStatus,
		currARs.CanaryBackground,
		v1alpha1.RolloutTypeBackgroundRunLabel,
	)
}

func (c *rolloutContext) reconcilePrePromotionAnalysisRun() (*v1alpha1.AnalysisRun, error) {
	currentAr := c.currentArs.BlueGreenPrePromotion
	if c.rollout.Spec.Strategy.BlueGreen.PrePromotionAnalysis == nil {
		err := c.cancelAnalysisRuns([]*v1alpha1.AnalysisRun{currentAr})
		return nil, err
	}
	c.log.Info("Reconciling Pre Promotion Analysis")

	if skipPrePromotionAnalysisRun(c.rollout, c.newRS) {
		err := c.cancelAnalysisRuns([]*v1alpha1.AnalysisRun{currentAr})
		return currentAr, err
	}

	if getPauseCondition(c.rollout, v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		return currentAr, nil
	}

	if needsNewAnalysisRun(currentAr, c.rollout) {
		podHash := replicasetutil.GetPodTemplateHash(c.newRS)
		instanceID := analysisutil.GetInstanceID(c.rollout)
		prePromotionLabels := analysisutil.PrePromotionLabels(podHash, instanceID)
		currentAr, err := c.createAnalysisRun(c.rollout.Spec.Strategy.BlueGreen.PrePromotionAnalysis, "pre", prePromotionLabels)
		if err == nil {
			c.log.WithField(logutil.AnalysisRunKey, currentAr.Name).Info("Created Pre Promotion AnalysisRun")
		}
		return currentAr, err
	}
	return currentAr, nil
}

// skipPrePromotionAnalysisRun checks if the controller should skip creating a pre promotion
// analysis run by checking if the rollout active promotion happened, the rollout was just created,
// the newRS is not saturated
func skipPrePromotionAnalysisRun(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet) bool {
	currentPodHash := replicasetutil.GetPodTemplateHash(newRS)
	activeSelector := rollout.Status.BlueGreen.ActiveSelector
	if rollout.Status.StableRS == currentPodHash || activeSelector == "" || activeSelector == currentPodHash || currentPodHash == "" {
		return true
	}
	// Checking saturation is different if the previewReplicaCount feature is being used because
	// annotations.IsSaturated() also looks at the desired annotation on the ReplicaSet, and the
	// check using previewReplicaCount does not.
	if rollout.Spec.Strategy.BlueGreen.PreviewReplicaCount != nil {
		desiredPreviewCount := *rollout.Spec.Strategy.BlueGreen.PreviewReplicaCount
		return *(newRS.Spec.Replicas) != desiredPreviewCount || newRS.Status.AvailableReplicas != desiredPreviewCount
	}
	return !annotations.IsSaturated(rollout, newRS)
}

// skipPrePromotionAnalysisRun checks if the controller should skip creating a post promotion
// analysis run by checking that the desired ReplicaSet is the stable ReplicaSet, the active
// service promotion has not happened, the rollout was just created, or the newRS is not saturated
func skipPostPromotionAnalysisRun(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet) bool {
	currentPodHash := replicasetutil.GetPodTemplateHash(newRS)
	activeSelector := rollout.Status.BlueGreen.ActiveSelector
	return rollout.Status.StableRS == currentPodHash || activeSelector != currentPodHash || currentPodHash == "" || !annotations.IsSaturated(rollout, newRS)
}

func (c *rolloutContext) reconcilePostPromotionAnalysisRun() (*v1alpha1.AnalysisRun, error) {
	currentAr := c.currentArs.BlueGreenPostPromotion
	if c.rollout.Spec.Strategy.BlueGreen.PostPromotionAnalysis == nil {
		err := c.cancelAnalysisRuns([]*v1alpha1.AnalysisRun{currentAr})
		return nil, err
	}

	c.log.Info("Reconciling Post Promotion Analysis")
	// don't start post-promotion if we are not ready to, or we are still waiting for target verification
	if skipPostPromotionAnalysisRun(c.rollout, c.newRS) || !c.areTargetsVerified() {
		err := c.cancelAnalysisRuns([]*v1alpha1.AnalysisRun{currentAr})
		return currentAr, err
	}

	if getPauseCondition(c.rollout, v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		return currentAr, nil
	}

	if needsNewAnalysisRun(currentAr, c.rollout) {
		podHash := replicasetutil.GetPodTemplateHash(c.newRS)
		instanceID := analysisutil.GetInstanceID(c.rollout)
		postPromotionLabels := analysisutil.PostPromotionLabels(podHash, instanceID)
		currentAr, err := c.createAnalysisRun(c.rollout.Spec.Strategy.BlueGreen.PostPromotionAnalysis, "post", postPromotionLabels)
		if err == nil {
			c.log.WithField(logutil.AnalysisRunKey, currentAr.Name).Info("Created Post Promotion AnalysisRun")
		}
		return currentAr, err
	}
	return currentAr, nil
}

func (c *rolloutContext) reconcileBackgroundAnalysisRun() (*v1alpha1.AnalysisRun, error) {
	currentAr := c.currentArs.CanaryBackground
	if c.rollout.Spec.Strategy.Canary.Analysis == nil {
		err := c.cancelAnalysisRuns([]*v1alpha1.AnalysisRun{currentAr})
		return nil, err
	}

	// Do not create a background run if the rollout is completely rolled out, just created, before the starting step
	if rolloututil.IsFullyPromoted(c.rollout) || c.rollout.Status.StableRS == "" || c.rollout.Status.CurrentPodHash == "" || replicasetutil.BeforeStartingStep(c.rollout) {
		return nil, nil
	}

	if getPauseCondition(c.rollout, v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		return currentAr, nil
	}

	if needsNewAnalysisRun(currentAr, c.rollout) {
		podHash := replicasetutil.GetPodTemplateHash(c.newRS)
		instanceID := analysisutil.GetInstanceID(c.rollout)
		backgroundLabels := analysisutil.BackgroundLabels(podHash, instanceID)
		currentAr, err := c.createAnalysisRun(&c.rollout.Spec.Strategy.Canary.Analysis.RolloutAnalysis, "", backgroundLabels)
		if err == nil {
			c.log.WithField(logutil.AnalysisRunKey, currentAr.Name).Info("Created background AnalysisRun")
		}
		return currentAr, err
	}
	switch currentAr.Status.Phase {
	case v1alpha1.AnalysisPhaseInconclusive:
		c.pauseContext.AddPauseCondition(v1alpha1.PauseReasonInconclusiveAnalysis)
	case v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseFailed:
		c.pauseContext.AddAbort(currentAr.Status.Message)
	}
	return currentAr, nil
}

func (c *rolloutContext) createAnalysisRun(rolloutAnalysis *v1alpha1.RolloutAnalysis, infix string, labels map[string]string) (*v1alpha1.AnalysisRun, error) {
	args, err := analysisutil.BuildArgumentsForRolloutAnalysisRun(rolloutAnalysis.Args, c.stableRS, c.newRS, c.rollout)
	if err != nil {
		return nil, err
	}

	podHash := replicasetutil.GetPodTemplateHash(c.newRS)
	if podHash == "" {
		return nil, fmt.Errorf("Latest ReplicaSet '%s' has no pod hash in the labels", c.newRS.Name)
	}
	ar, err := c.newAnalysisRunFromRollout(rolloutAnalysis, args, podHash, infix, labels)
	if err != nil {
		return nil, err
	}
	analysisRunIf := c.argoprojclientset.ArgoprojV1alpha1().AnalysisRuns(c.rollout.Namespace)
	return analysisutil.CreateWithCollisionCounter(c.log, analysisRunIf, *ar)
}

func (c *rolloutContext) reconcileStepBasedAnalysisRun() (*v1alpha1.AnalysisRun, error) {
	step, index := replicasetutil.GetCurrentCanaryStep(c.rollout)
	currentAr := c.currentArs.CanaryStep

	if len(c.rollout.Status.PauseConditions) > 0 || c.rollout.Status.Abort {
		return currentAr, nil
	}

	if step == nil || step.Analysis == nil || index == nil {
		err := c.cancelAnalysisRuns([]*v1alpha1.AnalysisRun{currentAr})
		return nil, err
	}
	c.log.Infof("Reconciling analysis step (stepIndex: %d)", *index)
	if needsNewAnalysisRun(currentAr, c.rollout) {
		podHash := replicasetutil.GetPodTemplateHash(c.newRS)
		instanceID := analysisutil.GetInstanceID(c.rollout)
		stepLabels := analysisutil.StepLabels(*index, podHash, instanceID)
		currentAr, err := c.createAnalysisRun(step.Analysis, strconv.Itoa(int(*index)), stepLabels)
		if err == nil {
			c.log.WithField(logutil.AnalysisRunKey, currentAr.Name).Infof("Created AnalysisRun for step '%d'", *index)
		}
		return currentAr, err
	}

	switch currentAr.Status.Phase {
	case v1alpha1.AnalysisPhaseInconclusive:
		c.pauseContext.AddPauseCondition(v1alpha1.PauseReasonInconclusiveAnalysis)
	case v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseFailed:
		c.pauseContext.AddAbort(currentAr.Status.Message)
	}

	return currentAr, nil
}

func (c *rolloutContext) cancelAnalysisRuns(analysisRuns []*v1alpha1.AnalysisRun) error {
	ctx := context.TODO()
	for i := range analysisRuns {
		ar := analysisRuns[i]
		isNotCompleted := ar == nil || !ar.Status.Phase.Completed()
		if ar != nil && !ar.Spec.Terminate && isNotCompleted {
			c.log.WithField(logutil.AnalysisRunKey, ar.Name).Infof("Canceling the analysis run '%s'", ar.Name)
			_, err := c.argoprojclientset.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Patch(ctx, ar.Name, patchtypes.MergePatchType, []byte(cancelAnalysisRun), metav1.PatchOptions{})
			if err != nil {
				if k8serrors.IsNotFound(err) {
					c.log.Warnf("AnalysisRun '%s' not found", ar.Name)
					continue
				}
				return err
			}
		}
	}
	return nil
}

// newAnalysisRunFromRollout generates an AnalysisRun from the rollouts, the AnalysisRun Step, the new/stable ReplicaSet, and any extra objects.
func (c *rolloutContext) newAnalysisRunFromRollout(rolloutAnalysis *v1alpha1.RolloutAnalysis, args []v1alpha1.Argument, podHash string, infix string, labels map[string]string) (*v1alpha1.AnalysisRun, error) {
	revision := c.rollout.Annotations[annotations.RevisionAnnotation]
	nameParts := []string{c.rollout.Name, podHash, revision}
	if infix != "" {
		nameParts = append(nameParts, infix)
	}
	name := strings.Join(nameParts, "-")
	var run *v1alpha1.AnalysisRun
	var err error
	templates := make([]*v1alpha1.AnalysisTemplate, 0)
	clusterTemplates := make([]*v1alpha1.ClusterAnalysisTemplate, 0)
	for _, templateRef := range rolloutAnalysis.Templates {
		if templateRef.ClusterScope {
			template, err := c.clusterAnalysisTemplateLister.Get(templateRef.TemplateName)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					c.log.Warnf("ClusterAnalysisTemplate '%s' not found", templateRef.TemplateName)
				}
				return nil, err
			}
			clusterTemplates = append(clusterTemplates, template)
		} else {
			template, err := c.analysisTemplateLister.AnalysisTemplates(c.rollout.Namespace).Get(templateRef.TemplateName)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					c.log.Warnf("AnalysisTemplate '%s' not found", templateRef.TemplateName)
				}
				return nil, err
			}
			templates = append(templates, template)
		}

	}
	run, err = analysisutil.NewAnalysisRunFromTemplates(templates, clusterTemplates, args, rolloutAnalysis.DryRun, rolloutAnalysis.MeasurementRetention, name, "", c.rollout.Namespace)
	if err != nil {
		return nil, err
	}
	run.Labels = labels
	for k, v := range rolloutAnalysis.AnalysisRunMetadata.Labels {
		run.Labels[k] = v
	}

	for k, v := range c.rollout.Spec.Selector.MatchLabels {
		run.Labels[k] = v
	}

	run.Annotations = map[string]string{
		annotations.RevisionAnnotation: revision,
	}
	for k, v := range rolloutAnalysis.AnalysisRunMetadata.Annotations {
		run.Annotations[k] = v
	}
	run.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(c.rollout, controllerKind)}
	return run, nil
}

func (c *rolloutContext) deleteAnalysisRuns(ars []*v1alpha1.AnalysisRun) error {
	ctx := context.TODO()
	for i := range ars {
		ar := ars[i]
		if ar.DeletionTimestamp != nil {
			continue
		}
		c.log.Infof("Trying to cleanup analysis run '%s'", ar.Name)
		err := c.argoprojclientset.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Delete(ctx, ar.Name, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
