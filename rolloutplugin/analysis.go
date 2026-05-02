package rolloutplugin

import (
	"context"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	log "github.com/sirupsen/logrus"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/record"
)

const (
	cancelAnalysisRun = `{
		"spec": {
			"terminate": true
		}
	}`
)

// needsNewAnalysisRunForPlugin determines if a new AnalysisRun should be created.
func needsNewAnalysisRunForPlugin(currentAr *v1alpha1.AnalysisRun, rp *v1alpha1.RolloutPlugin) bool {
	if currentAr == nil {
		return true
	}

	// If the controller has already paused for an inconclusive result, we need to create a new AR to retry.
	if rp.Status.ControllerPause &&
		getRolloutPluginPauseCondition(rp, v1alpha1.PauseReasonCanaryPauseStep) == nil {
		return currentAr.Status.Phase == v1alpha1.AnalysisPhaseInconclusive
	}
	return rp.Status.AbortedAt != nil
}

// getAnalysisRunsForRolloutPlugin gets all AnalysisRuns owned by the RolloutPlugin
func (r *RolloutPluginReconciler) getAnalysisRunsForRolloutPlugin(ctx context.Context, rp *v1alpha1.RolloutPlugin) ([]*v1alpha1.AnalysisRun, error) {
	logCtx := log.WithField("rolloutplugin", rp.Name)

	var analysisRunList v1alpha1.AnalysisRunList
	if err := r.Client.List(ctx, &analysisRunList, client.InNamespace(rp.Namespace)); err != nil {
		logCtx.WithError(err).Error("Failed to list AnalysisRuns for RolloutPlugin")
		return nil, err
	}

	ownedByRolloutPlugin := make([]*v1alpha1.AnalysisRun, 0)
	seen := make(map[string]bool)
	for i := range analysisRunList.Items {
		ar := &analysisRunList.Items[i]
		controllerRef := metav1.GetControllerOf(ar)
		if controllerRef != nil && controllerRef.UID == rp.UID {
			ownedByRolloutPlugin = append(ownedByRolloutPlugin, ar)
			seen[ar.Name] = true
		}
	}

	// Also check for AnalysisRuns referenced in status that might be missing from cache
	arStatuses := []*v1alpha1.RolloutAnalysisRunStatus{
		rp.Status.Canary.CurrentBackgroundAnalysisRunStatus,
		rp.Status.Canary.CurrentStepAnalysisRunStatus,
	}
	for _, arStatus := range arStatuses {
		if arStatus == nil || seen[arStatus.Name] {
			continue
		}
		// Fallback to direct API call only for missing status-referenced ARs
		ar, err := r.ArgoProjClientset.ArgoprojV1alpha1().AnalysisRuns(rp.Namespace).Get(ctx, arStatus.Name, metav1.GetOptions{})
		if err == nil && ar != nil {
			logCtx.Infof("Found analysis run '%s' missing from cache", ar.Name)
			ownedByRolloutPlugin = append(ownedByRolloutPlugin, ar)
		}
	}

	return ownedByRolloutPlugin, nil
}

// filterCurrentAnalysisRuns filters current analysis runs for RolloutPlugin
func (r *RolloutPluginReconciler) filterCurrentAnalysisRuns(allArs []*v1alpha1.AnalysisRun, rp *v1alpha1.RolloutPlugin) (analysisutil.CurrentAnalysisRuns, []*v1alpha1.AnalysisRun) {
	currArs := analysisutil.CurrentAnalysisRuns{}
	otherArs := []*v1alpha1.AnalysisRun{}
	getArName := func(s *v1alpha1.RolloutAnalysisRunStatus) string {
		if s == nil {
			return ""
		}
		return s.Name
	}
	for i := range allArs {
		ar := allArs[i]
		if ar != nil {
			switch ar.Name {
			case getArName(rp.Status.Canary.CurrentStepAnalysisRunStatus):
				currArs.CanaryStep = ar
			case getArName(rp.Status.Canary.CurrentBackgroundAnalysisRunStatus):
				currArs.CanaryBackground = ar
			default:
				otherArs = append(otherArs, ar)
			}
		}
	}
	return currArs, otherArs
}

// reconcileAnalysisRuns orchestrates all analysis run reconciliation for the RolloutPlugin
func (r *RolloutPluginReconciler) reconcileAnalysisRuns(ctx context.Context, rp *v1alpha1.RolloutPlugin, allArs []*v1alpha1.AnalysisRun, pCtx *pauseContext, logCtx *log.Entry) error {
	// Split current and other analysis runs
	currentArs, otherArs := r.filterCurrentAnalysisRuns(allArs, rp)

	// Check if we should skip analysis reconciliation
	notProgressing := !conditions.IsRolloutPluginProgressing(&rp.Status)
	if notProgressing {
		logCtx.Info("Skipping analysis - not in progress")
		allArsToCancel := append(currentArs.ToArray(), otherArs...)
		if err := r.cancelAnalysisRuns(ctx, allArsToCancel, logCtx); err != nil {
			return err
		}
		r.setCurrentAnalysisRuns(rp, currentArs)
		return nil
	}

	newCurrentAnalysisRuns := analysisutil.CurrentAnalysisRuns{}

	if rp.Spec.Strategy.Canary != nil {
		// Reconcile step-based analysis
		stepAnalysisRun, err := r.reconcileStepBasedAnalysisRun(ctx, rp, currentArs.CanaryStep, pCtx, logCtx)
		if err != nil {
			return err
		}
		newCurrentAnalysisRuns.CanaryStep = stepAnalysisRun

		// Reconcile background analysis
		backgroundAnalysisRun, err := r.reconcileBackgroundAnalysisRun(ctx, rp, currentArs.CanaryBackground, pCtx, logCtx)
		if err != nil {
			return err
		}
		newCurrentAnalysisRuns.CanaryBackground = backgroundAnalysisRun
	}

	// Emit events for analysis run status changes before updating status
	r.emitAnalysisRunStatusChanges(rp, newCurrentAnalysisRuns)

	// Update status with current analysis runs
	r.setCurrentAnalysisRuns(rp, newCurrentAnalysisRuns)

	// Rebuild otherArs to ensure it doesn't include the newly created runs
	otherArs, _ = analysisutil.FilterAnalysisRuns(otherArs, func(ar *v1alpha1.AnalysisRun) bool {
		for _, curr := range newCurrentAnalysisRuns.ToArray() {
			if ar.Name == curr.Name {
				logCtx.Infof("Rescued %s from inadvertent termination", ar.Name)
				return false
			}
		}
		return true
	})

	// Cancel other analysis runs
	if err := r.cancelAnalysisRuns(ctx, otherArs, logCtx); err != nil {
		return err
	}

	// Garbage collect old analysis runs
	limitSucceedArs := defaults.DefaultAnalysisRunSuccessfulHistoryLimit
	limitFailedArs := defaults.DefaultAnalysisRunUnsuccessfulHistoryLimit
	if rp.Spec.Analysis != nil {
		if rp.Spec.Analysis.SuccessfulRunHistoryLimit != nil {
			limitSucceedArs = *rp.Spec.Analysis.SuccessfulRunHistoryLimit
		}
		if rp.Spec.Analysis.UnsuccessfulRunHistoryLimit != nil {
			limitFailedArs = *rp.Spec.Analysis.UnsuccessfulRunHistoryLimit
		}
	}
	arsToDelete := analysisutil.FilterAnalysisRunsToDelete(otherArs, nil, limitSucceedArs, limitFailedArs)
	if err := r.deleteAnalysisRuns(ctx, arsToDelete, logCtx); err != nil {
		return err
	}

	return nil
}

// reconcileStepBasedAnalysisRun reconciles step-based analysis for canary strategy.
func (r *RolloutPluginReconciler) reconcileStepBasedAnalysisRun(ctx context.Context, rp *v1alpha1.RolloutPlugin, currentAr *v1alpha1.AnalysisRun, pCtx *pauseContext, logCtx *log.Entry) (*v1alpha1.AnalysisRun, error) {
	if rp.Spec.Strategy.Canary == nil {
		return nil, nil
	}

	// Early return if paused or aborted — don't create or cancel, just keep current
	if len(rp.Status.PauseConditions) > 0 || rp.Status.Abort {
		return currentAr, nil
	}

	if rp.Status.CurrentStepIndex == nil {
		return nil, nil
	}

	currentStepIndex := *rp.Status.CurrentStepIndex
	if currentStepIndex >= int32(len(rp.Spec.Strategy.Canary.Steps)) {
		return nil, nil
	}

	currentStep := rp.Spec.Strategy.Canary.Steps[currentStepIndex]

	// Detect AR from a previous step (e.g. after promotion that skipped steps)
	analysisRunFromPreviousStep := currentStep.Analysis != nil && currentAr != nil &&
		currentAr.GetLabels()[v1alpha1.RolloutCanaryStepIndexLabel] != strconv.Itoa(int(currentStepIndex))

	if currentStep.Analysis == nil || analysisRunFromPreviousStep {
		// No analysis for this step, or AR is from a previous step — cancel it
		err := r.cancelAnalysisRuns(ctx, []*v1alpha1.AnalysisRun{currentAr}, logCtx)
		return nil, err
	}

	logCtx.Infof("Reconciling analysis step (stepIndex: %d)", currentStepIndex)

	if needsNewAnalysisRunForPlugin(currentAr, rp) {
		newAr, err := r.createAnalysisRun(ctx, rp, currentStep.Analysis,
			strconv.Itoa(int(currentStepIndex)), v1alpha1.RolloutTypeStepLabel, currentStepIndex, logCtx)
		if err == nil {
			logCtx.Infof("Created AnalysisRun '%s' for step %d", newAr.Name, currentStepIndex)
		}
		return newAr, err
	}

	// Phase switch — set pause or abort based on AR result
	switch currentAr.Status.Phase {
	case v1alpha1.AnalysisPhaseInconclusive:
		pCtx.AddPauseCondition(v1alpha1.PauseReasonInconclusiveAnalysis)
	case v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseFailed:
		message := "Step-based analysis phase error/failed"
		if currentAr.Status.Message != "" {
			message += ": " + currentAr.Status.Message
		}
		pCtx.AddAbort(message)
	}

	return currentAr, nil
}

// reconcileBackgroundAnalysisRun reconciles background analysis for canary strategy.
func (r *RolloutPluginReconciler) reconcileBackgroundAnalysisRun(ctx context.Context, rp *v1alpha1.RolloutPlugin, currentAr *v1alpha1.AnalysisRun, pCtx *pauseContext, logCtx *log.Entry) (*v1alpha1.AnalysisRun, error) {
	if rp.Spec.Strategy.Canary == nil || rp.Spec.Strategy.Canary.Analysis == nil {
		return nil, nil
	}

	// Do not create or keep a background run if the rollout is fully promoted
	if rp.Status.CurrentRevision == rp.Status.UpdatedRevision {
		logCtx.Info("Rollout is fully promoted, not running background analysis")
		return nil, nil
	}

	// Do not create background analysis before the configured starting step
	if rp.Spec.Strategy.Canary.Analysis.StartingStep != nil && rp.Status.CurrentStepIndex != nil {
		if *rp.Status.CurrentStepIndex < *rp.Spec.Strategy.Canary.Analysis.StartingStep {
			return nil, nil
		}
	}

	// If already paused for inconclusive analysis, keep the current AR (don't re-create yet)
	if getRolloutPluginPauseCondition(rp, v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		return currentAr, nil
	}

	if needsNewAnalysisRunForPlugin(currentAr, rp) {
		logCtx.Info("Creating background analysis run")
		newAr, err := r.createAnalysisRun(ctx, rp, &rp.Spec.Strategy.Canary.Analysis.RolloutAnalysis,
			"", v1alpha1.RolloutTypeBackgroundRunLabel, -1, logCtx)
		if err == nil {
			logCtx.Infof("Created background AnalysisRun '%s'", newAr.Name)
		}
		return newAr, err
	}

	// Phase switch — set pause or abort based on AR result
	switch currentAr.Status.Phase {
	case v1alpha1.AnalysisPhaseInconclusive:
		pCtx.AddPauseCondition(v1alpha1.PauseReasonInconclusiveAnalysis)
	case v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseFailed:
		message := "Background analysis phase error/failed"
		if currentAr.Status.Message != "" {
			message += ": " + currentAr.Status.Message
		}
		pCtx.AddAbort(message)
	}

	return currentAr, nil
}

// convertAnalysisRunArgsToArguments converts AnalysisRunArgument to Argument
func convertAnalysisRunArgsToArguments(args []v1alpha1.AnalysisRunArgument) []v1alpha1.Argument {
	result := make([]v1alpha1.Argument, len(args))
	for i, arg := range args {
		result[i] = v1alpha1.Argument{
			Name:  arg.Name,
			Value: &arg.Value,
		}
		if arg.ValueFrom != nil && arg.ValueFrom.FieldRef != nil {
			result[i].ValueFrom = &v1alpha1.ValueFrom{
				FieldRef: arg.ValueFrom.FieldRef,
			}
		}
	}
	return result
}

// createAnalysisRun creates a new AnalysisRun with deterministic naming and collision counter.
func (r *RolloutPluginReconciler) createAnalysisRun(ctx context.Context, rp *v1alpha1.RolloutPlugin, analysisSpec *v1alpha1.RolloutAnalysis, infix string, rolloutType string, stepIndex int32, logCtx *log.Entry) (*v1alpha1.AnalysisRun, error) {
	// Fetch templates
	templates := make([]*v1alpha1.AnalysisTemplate, 0)
	clusterTemplates := make([]*v1alpha1.ClusterAnalysisTemplate, 0)
	for _, templateRef := range analysisSpec.Templates {
		if templateRef.ClusterScope == nil || !*templateRef.ClusterScope {
			var template v1alpha1.AnalysisTemplate
			if err := r.Client.Get(ctx, client.ObjectKey{Namespace: rp.Namespace, Name: templateRef.TemplateName}, &template); err != nil {
				logCtx.WithError(err).Errorf("Failed to get AnalysisTemplate '%s'", templateRef.TemplateName)
				return nil, err
			}
			templates = append(templates, &template)
		} else {
			var clusterTemplate v1alpha1.ClusterAnalysisTemplate
			if err := r.Client.Get(ctx, client.ObjectKey{Name: templateRef.TemplateName}, &clusterTemplate); err != nil {
				logCtx.WithError(err).Errorf("Failed to get ClusterAnalysisTemplate '%s'", templateRef.TemplateName)
				return nil, err
			}
			clusterTemplates = append(clusterTemplates, &clusterTemplate)
		}
	}

	// Build deterministic name: {rpName}-{updatedRevision}-{infix}
	revision := rp.Status.UpdatedRevision
	name := rp.Name + "-" + revision
	if infix != "" {
		name += "-" + infix
	}

	arLabels := map[string]string{
		v1alpha1.RolloutTypeLabel:       rolloutType,
		v1alpha1.RolloutPluginNameLabel: rp.Name,
	}
	if stepIndex >= 0 {
		arLabels[v1alpha1.RolloutCanaryStepIndexLabel] = strconv.Itoa(int(stepIndex))
	}
	if r.InstanceID != "" {
		arLabels[v1alpha1.LabelKeyControllerInstanceID] = r.InstanceID
	}

	// TODO: anything to add here?
	annotations := map[string]string{}

	convertedArgs := convertAnalysisRunArgsToArguments(analysisSpec.Args)

	ar, err := analysisutil.NewAnalysisRunFromTemplates(
		templates,
		clusterTemplates,
		convertedArgs,
		analysisSpec.DryRun,
		analysisSpec.MeasurementRetention,
		arLabels,
		annotations,
		name,
		"",
		rp.Namespace,
	)
	if err != nil {
		logCtx.WithError(err).Error("Failed to create AnalysisRun from templates")
		return nil, err
	}

	// Set owner reference
	ar.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(rp, v1alpha1.SchemeGroupVersion.WithKind("RolloutPlugin")),
	})

	// Use CreateWithCollisionCounter for idempotent creation with retry
	analysisRunIf := r.ArgoProjClientset.ArgoprojV1alpha1().AnalysisRuns(rp.Namespace)
	createdAr, err := analysisutil.CreateWithCollisionCounter(logCtx, analysisRunIf, *ar)
	if err != nil {
		logCtx.WithError(err).Error("Failed to create AnalysisRun")
		return nil, err
	}

	return createdAr, nil
}

// cancelAnalysisRuns terminates analysis runs that are not yet completed.
func (r *RolloutPluginReconciler) cancelAnalysisRuns(ctx context.Context, ars []*v1alpha1.AnalysisRun, logCtx *log.Entry) error {
	for i := range ars {
		ar := ars[i]
		if ar == nil {
			continue
		}
		if ar.Spec.Terminate || ar.Status.Phase.Completed() {
			continue
		}
		logCtx.Infof("Canceling AnalysisRun '%s'", ar.Name)
		_, err := r.ArgoProjClientset.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Patch(
			ctx,
			ar.Name,
			patchtypes.MergePatchType,
			[]byte(cancelAnalysisRun),
			metav1.PatchOptions{},
		)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logCtx.Warnf("AnalysisRun '%s' not found", ar.Name)
				continue
			}
			return err
		}
	}
	return nil
}

// deleteAnalysisRuns deletes the given analysis runs
func (r *RolloutPluginReconciler) deleteAnalysisRuns(ctx context.Context, ars []*v1alpha1.AnalysisRun, logCtx *log.Entry) error {
	for _, ar := range ars {
		if ar.DeletionTimestamp != nil {
			continue
		}
		logCtx.Infof("Trying to cleanup analysis run '%s'", ar.Name)
		err := r.ArgoProjClientset.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Delete(ctx, ar.Name, metav1.DeleteOptions{})
		if err != nil && !k8serrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// emitAnalysisRunStatusChanges emits a Kubernetes event if the analysis run status has changed
func (r *RolloutPluginReconciler) emitAnalysisRunStatusChanges(rp *v1alpha1.RolloutPlugin, currARs analysisutil.CurrentAnalysisRuns) {
	if r.Recorder == nil {
		return
	}
	r.emitSingleAnalysisRunStatusChange(rp,
		rp.Status.Canary.CurrentStepAnalysisRunStatus,
		currARs.CanaryStep,
		v1alpha1.RolloutTypeStepLabel)
	r.emitSingleAnalysisRunStatusChange(rp,
		rp.Status.Canary.CurrentBackgroundAnalysisRunStatus,
		currARs.CanaryBackground,
		v1alpha1.RolloutTypeBackgroundRunLabel)
}

func (r *RolloutPluginReconciler) emitSingleAnalysisRunStatusChange(rp *v1alpha1.RolloutPlugin, prevStatus *v1alpha1.RolloutAnalysisRunStatus, ar *v1alpha1.AnalysisRun, arType string) {
	if ar == nil || ar.Status.Phase == "" {
		return
	}
	if prevStatus == nil || (prevStatus.Name == ar.Name && prevStatus.Status != ar.Status.Phase) {
		prevStatusStr := "NoPreviousStatus"
		if prevStatus != nil {
			prevStatusStr = string(prevStatus.Status)
		}

		eventType := corev1.EventTypeNormal
		if ar.Status.Phase == v1alpha1.AnalysisPhaseFailed || ar.Status.Phase == v1alpha1.AnalysisPhaseError {
			eventType = corev1.EventTypeWarning
		}
		msg := fmt.Sprintf("%s Analysis Run '%s' Status New: '%s' Previous: '%s'", arType, ar.Name, ar.Status.Phase, prevStatusStr)
		r.Recorder.Eventf(rp, record.EventOptions{EventType: eventType, EventReason: "AnalysisRun" + string(ar.Status.Phase)}, msg)
	}
}

// setCurrentAnalysisRuns updates the status with current analysis run information including Message
func (r *RolloutPluginReconciler) setCurrentAnalysisRuns(rp *v1alpha1.RolloutPlugin, currARs analysisutil.CurrentAnalysisRuns) {
	if currARs.CanaryBackground != nil {
		rp.Status.Canary.CurrentBackgroundAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
			Name:    currARs.CanaryBackground.Name,
			Status:  currARs.CanaryBackground.Status.Phase,
			Message: currARs.CanaryBackground.Status.Message,
		}
	} else {
		rp.Status.Canary.CurrentBackgroundAnalysisRunStatus = nil
	}

	if currARs.CanaryStep != nil {
		rp.Status.Canary.CurrentStepAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
			Name:    currARs.CanaryStep.Name,
			Status:  currARs.CanaryStep.Status.Phase,
			Message: currARs.CanaryStep.Status.Message,
		}
	} else {
		rp.Status.Canary.CurrentStepAnalysisRunStatus = nil
	}
}
