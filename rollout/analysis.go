package rollout

import (
	"context"
	"fmt"
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

func (c *rolloutContext) reconcileAnalysisRun(run CurrentAnalysisRun) (*v1alpha1.AnalysisRun, error) {

	if run.ShouldCancel() || shouldSkip(run.ARType(), c.rollout, c.newRS) {
		return nil, c.cancelAnalysisRun(run.AnalysisRun())
	}
	if run.ShouldReturnCur() {
		return run.AnalysisRun(), nil
	}
	if run.NeedsNew(c.rollout.Status.ControllerPause, c.rollout.Status.PauseConditions, c.rollout.Status.AbortedAt) {
		podHash := replicasetutil.GetPodTemplateHash(c.newRS)
		instanceID := analysisutil.GetInstanceID(c.rollout)
		return c.createAnalysisRun(run.RolloutAnalysis(), run.Infix(), run.Labels(podHash, instanceID))
	}
	return run.AnalysisRun(), nil
}

func (c *rolloutContext) reconcileAnalysisRuns() error {
	if c.shouldCancelAllAnalysisRuns() {
		return c.cancelAnalysisRuns()
	}
	for _, run := range c.analysisContext.AllCurrentAnalysisRuns() {
		currentRun, err := c.reconcileAnalysisRun(run)
		if err != nil {
			return err
		}
		c.setPauseOrAbort(run.AnalysisRun())
		c.analysisContext.UpdateCurrentAnalysisRuns(currentRun, run.ARType())
	}

	c.SetCurrentAnalysisRuns()

	// Due to the possibility that we are operating on stale/inconsistent data in the informer, it's
	// possible that otherArs includes the current analysis runs that we just created or reclaimed
	// in newCurrentAnalysisRuns, despite the fact that our rollout status did not have those set.
	// To prevent us from terminating the runs that we just created moments ago, rebuild otherArs
	// to ensure it does not include the newly created runs.

	otherArs, _ := analysisutil.FilterAnalysisRuns(c.analysisContext.otherArs, func(ar *v1alpha1.AnalysisRun) bool {
		for _, curr := range c.analysisContext.CurrentAnalysisRunsToArray() {
			if ar.Name == curr.Name {
				c.log.Infof("Rescued %s from inadvertent termination", ar.Name)
				return false
			}
		}
		return true
	})

	for _, ar := range otherArs {
		err := c.cancelAnalysisRun(ar)
		if err != nil {
			return err
		}
	}

	limitSucceedArs := defaults.GetAnalysisRunSuccessfulHistoryLimitOrDefault(c.rollout)
	limitFailedArs := defaults.GetAnalysisRunUnsuccessfulHistoryLimitOrDefault(c.rollout)
	arsToDelete := analysisutil.FilterAnalysisRunsToDelete(otherArs, c.allRSs, limitSucceedArs, limitFailedArs)
	err := c.deleteAnalysisRuns(arsToDelete)
	if err != nil {
		return err
	}

	c.reconcileAnalysisRunStatusChanges(c.analysisContext.CurrentAnalysisRuns)
	return nil
}

func (c *rolloutContext) shouldCancelAllAnalysisRuns() bool {
	isAborted := c.pauseContext.IsAborted()
	rollbackToScaleDownDelay := replicasetutil.HasScaleDownDeadline(c.newRS)
	initialDeploy := c.rollout.Status.StableRS == ""
	isRollbackWithinWindow := c.isRollbackWithinWindow()
	// if certain conditions are met, update the rollout status
	// then cancel all analysis runs
	if isAborted || c.rollout.Status.PromoteFull || rollbackToScaleDownDelay || initialDeploy || isRollbackWithinWindow {
		c.log.Infof("Skipping analysis: isAborted: %v, promoteFull: %v, rollbackToScaleDownDelay: %v, initialDeploy: %v, isRollbackWithinWindow: %v", isAborted, c.rollout.Status.PromoteFull, rollbackToScaleDownDelay, initialDeploy, isRollbackWithinWindow)

		c.SetCurrentAnalysisRuns()
		return true
	}
	return false
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
	// Run is created only when the previous AnalysisRun is inconclusive.
	// Additional check for the Canary Pause prevents Canary promotion when AnalysisRun is inconclusive and reached
	// inconclusiveLimit. Otherwise, another AnalysisRun will be spawned and can cause Success status,
	// because of termination when the AnalysisRun is still in-flight.
	if rollout.Status.ControllerPause &&
		getPauseCondition(rollout, v1alpha1.PauseReasonCanaryPauseStep) == nil &&
		getPauseCondition(rollout, v1alpha1.PauseReasonBlueGreenPause) == nil {

		return currentAr.Status.Phase == v1alpha1.AnalysisPhaseInconclusive
	}
	return rollout.Status.AbortedAt != nil
}

func validPause(controllerPause bool, pauseConditions []v1alpha1.PauseCondition) bool {
	return controllerPause &&
		!pauseConditionsInclude(pauseConditions, v1alpha1.PauseReasonCanaryPauseStep) &&
		!pauseConditionsInclude(pauseConditions, v1alpha1.PauseReasonBlueGreenPause)

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
func (c *rolloutContext) reconcileAnalysisRunStatusChanges(currARs CurrentAnalysisRuns) {
	c.emitAnalysisRunStatusChanges(
		c.rollout.Status.BlueGreen.PostPromotionAnalysisRunStatus,
		currARs.CurrentBlueGreenPostPromotion.Run,
		v1alpha1.RolloutTypePostPromotionLabel,
	)

	c.emitAnalysisRunStatusChanges(
		c.rollout.Status.BlueGreen.PrePromotionAnalysisRunStatus,
		currARs.CurrentBlueGreenPrePromotion.Run,
		v1alpha1.RolloutTypePrePromotionLabel,
	)

	c.emitAnalysisRunStatusChanges(
		c.rollout.Status.Canary.CurrentStepAnalysisRunStatus,
		currARs.CurrentCanaryStep.Run,
		v1alpha1.RolloutTypeStepLabel,
	)

	c.emitAnalysisRunStatusChanges(
		c.rollout.Status.Canary.CurrentBackgroundAnalysisRunStatus,
		currARs.CurrentCanaryBackground.Run,
		v1alpha1.RolloutTypeBackgroundRunLabel,
	)
}

// skipPrePromotionAnalysisRun checks if the controller should skip creating a pre promotion
// analysis run by checking if the rollout active promotion happened, the rollout was just created,
// the newRS is not saturated
func shouldSkip(ARType string, rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet) bool {
	if ARType == v1alpha1.RolloutTypePrePromotionLabel {
		return skipPrePromotionAnalysisRun(rollout, newRS)
	} else if ARType == v1alpha1.RolloutTypePostPromotionLabel {
		return skipPostPromotionAnalysisRun(rollout, newRS)
	} else {
		return false
	}
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

func (c *rolloutContext) cancelAnalysisRun(ar *v1alpha1.AnalysisRun) error {
	ctx := context.TODO()
	isNotCompleted := ar == nil || !ar.Status.Phase.Completed()
	if ar != nil && !ar.Spec.Terminate && isNotCompleted {
		c.log.WithField(logutil.AnalysisRunKey, ar.Name).Infof("Canceling the analysis run '%s'", ar.Name)
		_, err := c.argoprojclientset.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Patch(ctx, ar.Name, patchtypes.MergePatchType, []byte(cancelAnalysisRun), metav1.PatchOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *rolloutContext) cancelAnalysisRuns() error {
	ctx := context.TODO()
	for _, ar := range c.analysisContext.AllAnalysisRuns() {
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
	templates, clusterTemplates, err := c.getAnalysisTemplatesFromRefs(&rolloutAnalysis.Templates)
	if err != nil {
		return nil, err
	}
	runLabels := labels
	for k, v := range rolloutAnalysis.AnalysisRunMetadata.Labels {
		runLabels[k] = v
	}

	for k, v := range c.rollout.Spec.Selector.MatchLabels {
		runLabels[k] = v
	}

	runAnnotations := map[string]string{
		annotations.RevisionAnnotation: revision,
	}
	for k, v := range rolloutAnalysis.AnalysisRunMetadata.Annotations {
		runAnnotations[k] = v
	}
	run, err = analysisutil.NewAnalysisRunFromTemplates(templates, clusterTemplates, args, rolloutAnalysis.DryRun, rolloutAnalysis.MeasurementRetention,
		runLabels, runAnnotations, name, "", c.rollout.Namespace)
	if err != nil {
		return nil, err
	}
	run.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(c.rollout, controllerKind)}
	return run, nil
}

func (c *rolloutContext) getAnalysisTemplatesFromRefs(templateRefs *[]v1alpha1.AnalysisTemplateRef) ([]*v1alpha1.AnalysisTemplate, []*v1alpha1.ClusterAnalysisTemplate, error) {
	templates := make([]*v1alpha1.AnalysisTemplate, 0)
	clusterTemplates := make([]*v1alpha1.ClusterAnalysisTemplate, 0)
	for _, templateRef := range *templateRefs {
		if templateRef.ClusterScope {
			template, err := c.clusterAnalysisTemplateLister.Get(templateRef.TemplateName)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					c.log.Warnf("ClusterAnalysisTemplate '%s' not found", templateRef.TemplateName)
				}
				return nil, nil, err
			}
			clusterTemplates = append(clusterTemplates, template)
			// Look for nested templates
			if template.Spec.Templates != nil {
				innerTemplates, innerClusterTemplates, innerErr := c.getAnalysisTemplatesFromRefs(&template.Spec.Templates)
				if innerErr != nil {
					return nil, nil, innerErr
				}
				clusterTemplates = append(clusterTemplates, innerClusterTemplates...)
				templates = append(templates, innerTemplates...)
			}
		} else {
			template, err := c.analysisTemplateLister.AnalysisTemplates(c.rollout.Namespace).Get(templateRef.TemplateName)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					c.log.Warnf("AnalysisTemplate '%s' not found", templateRef.TemplateName)
				}
				return nil, nil, err
			}
			templates = append(templates, template)
			// Look for nested templates
			if template.Spec.Templates != nil {
				innerTemplates, innerClusterTemplates, innerErr := c.getAnalysisTemplatesFromRefs(&template.Spec.Templates)
				if innerErr != nil {
					return nil, nil, innerErr
				}
				clusterTemplates = append(clusterTemplates, innerClusterTemplates...)
				templates = append(templates, innerTemplates...)
			}
		}

	}
	uniqueTemplates, uniqueClusterTemplates := analysisutil.FilterUniqueTemplates(templates, clusterTemplates)
	return uniqueTemplates, uniqueClusterTemplates, nil
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
