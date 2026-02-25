package rolloutplugin

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	patchtypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
)

const (
	cancelAnalysisRun = `{
		"spec": {
			"terminate": true
		}
	}`
)

// getAnalysisRunsForRolloutPlugin gets all AnalysisRuns owned by the RolloutPlugin
func (r *RolloutPluginReconciler) getAnalysisRunsForRolloutPlugin(ctx context.Context, rp *v1alpha1.RolloutPlugin) ([]*v1alpha1.AnalysisRun, error) {
	logger := log.FromContext(ctx)

	// List all AnalysisRuns in the namespace
	analysisRunList, err := r.ArgoProjClientset.ArgoprojV1alpha1().AnalysisRuns(rp.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Everything().String(),
	})
	if err != nil {
		logger.Error(err, "Failed to list AnalysisRuns")
		return nil, err
	}

	// Filter AnalysisRuns owned by this RolloutPlugin
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

	// Also check for AnalysisRuns referenced in status that might be missing from the list
	arStatuses := []*v1alpha1.RolloutAnalysisRunStatus{
		rp.Status.Canary.CurrentBackgroundAnalysisRunStatus,
		rp.Status.Canary.CurrentStepAnalysisRunStatus,
	}
	for _, arStatus := range arStatuses {
		if arStatus == nil || seen[arStatus.Name] {
			continue
		}
		// We recorded a run in the status, but it didn't appear in the list.
		// Perform a get to see if it truly exists.
		ar, err := r.ArgoProjClientset.ArgoprojV1alpha1().AnalysisRuns(rp.Namespace).Get(ctx, arStatus.Name, metav1.GetOptions{})
		if err == nil && ar != nil {
			logger.Info("Found analysis run missing from list", "analysisRun", ar.Name)
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
func (r *RolloutPluginReconciler) reconcileAnalysisRuns(ctx context.Context, rp *v1alpha1.RolloutPlugin, allArs []*v1alpha1.AnalysisRun) error {
	logger := log.FromContext(ctx)

	// Split current and other analysis runs
	currentArs, otherArs := r.filterCurrentAnalysisRuns(allArs, rp)

	// Check if we should skip analysis reconciliation
	initialDeploy := !rp.Status.RolloutInProgress
	if initialDeploy {
		logger.Info("Skipping analysis - no rollout in progress")
		allArsToCancel := append(currentArs.ToArray(), otherArs...)
		if err := r.cancelAnalysisRuns(ctx, rp, allArsToCancel); err != nil {
			return err
		}
		r.setCurrentAnalysisRuns(rp, currentArs)
		return nil
	}

	// Reconcile analysis runs based on strategy
	newCurrentAnalysisRuns := analysisutil.CurrentAnalysisRuns{}

	if rp.Spec.Strategy.Canary != nil {
		// Reconcile step-based analysis
		stepAnalysisRun, err := r.reconcileStepBasedAnalysisRun(ctx, rp, currentArs.CanaryStep)
		if err != nil {
			return err
		}
		newCurrentAnalysisRuns.CanaryStep = stepAnalysisRun

		// Reconcile background analysis
		backgroundAnalysisRun, err := r.reconcileBackgroundAnalysisRun(ctx, rp, currentArs.CanaryBackground)
		if err != nil {
			return err
		}
		newCurrentAnalysisRuns.CanaryBackground = backgroundAnalysisRun
	}

	// Update status with current analysis runs
	r.setCurrentAnalysisRuns(rp, newCurrentAnalysisRuns)

	// Rebuild otherArs to ensure it doesn't include the newly created runs
	otherArs, _ = analysisutil.FilterAnalysisRuns(otherArs, func(ar *v1alpha1.AnalysisRun) bool {
		for _, curr := range newCurrentAnalysisRuns.ToArray() {
			if ar.Name == curr.Name {
				logger.Info("Rescued analysis run from termination", "analysisRun", ar.Name)
				return false
			}
		}
		return true
	})

	// Cancel other analysis runs
	if err := r.cancelAnalysisRuns(ctx, rp, otherArs); err != nil {
		return err
	}

	// Garbage collect old analysis runs
	// Use default history limits (5 successful, 5 unsuccessful)
	limitSucceedArs := int32(5)
	limitFailedArs := int32(5)
	arsToDelete := analysisutil.FilterAnalysisRunsToDelete(otherArs, nil, limitSucceedArs, limitFailedArs)
	if err := r.deleteAnalysisRuns(ctx, arsToDelete); err != nil {
		return err
	}

	return nil
}

// reconcileStepBasedAnalysisRun reconciles step-based analysis for canary strategy
func (r *RolloutPluginReconciler) reconcileStepBasedAnalysisRun(ctx context.Context, rp *v1alpha1.RolloutPlugin, currentAr *v1alpha1.AnalysisRun) (*v1alpha1.AnalysisRun, error) {
	logger := log.FromContext(ctx)

	if rp.Spec.Strategy.Canary == nil {
		return nil, nil
	}

	// Check if current step has analysis
	if rp.Status.CurrentStepIndex == nil {
		return nil, nil
	}

	currentStepIndex := *rp.Status.CurrentStepIndex
	if currentStepIndex >= int32(len(rp.Spec.Strategy.Canary.Steps)) {
		return nil, nil
	}

	currentStep := rp.Spec.Strategy.Canary.Steps[currentStepIndex]
	if currentStep.Analysis == nil {
		// No analysis for this step, cancel any existing analysis
		if currentAr != nil {
			logger.Info("Canceling step analysis - no analysis for current step", "step", currentStepIndex)
			if err := r.cancelAnalysisRuns(ctx, rp, []*v1alpha1.AnalysisRun{currentAr}); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}

	// Check if we need to create a new analysis run
	// Only create if:
	// 1. No current AR exists
	// 2. Rollout is aborted (allows retry after fixes)
	// 3. AR is inconclusive and rollout is paused for it
	if currentAr == nil {
		logger.Info("Creating step-based analysis run", "step", currentStepIndex)
		newAr, err := r.createAnalysisRun(ctx, rp, currentStep.Analysis, fmt.Sprintf("%s-step-%d", rp.Name, currentStepIndex), v1alpha1.RolloutTypeStepLabel)
		if err != nil {
			return nil, err
		}
		return newAr, nil
	}

	// If rollout is aborted, allow creating a new analysis run (for retry scenarios)
	if rp.Status.Aborted {
		logger.Info("Rollout is aborted, creating new step-based analysis run", "step", currentStepIndex)
		newAr, err := r.createAnalysisRun(ctx, rp, currentStep.Analysis, fmt.Sprintf("%s-step-%d", rp.Name, currentStepIndex), v1alpha1.RolloutTypeStepLabel)
		if err != nil {
			return nil, err
		}
		return newAr, nil
	}

	// If AR is inconclusive and rollout is paused, allow creating a new one
	if currentAr.Status.Phase == v1alpha1.AnalysisPhaseInconclusive && rp.Status.Paused {
		logger.Info("Analysis is inconclusive and rollout is paused, creating new step-based analysis run", "step", currentStepIndex)
		newAr, err := r.createAnalysisRun(ctx, rp, currentStep.Analysis, fmt.Sprintf("%s-step-%d", rp.Name, currentStepIndex), v1alpha1.RolloutTypeStepLabel)
		if err != nil {
			return nil, err
		}
		return newAr, nil
	}

	// Otherwise, keep the existing AR (even if failed/error - controller needs to see it)
	return currentAr, nil
}

// reconcileBackgroundAnalysisRun reconciles background analysis for canary strategy
func (r *RolloutPluginReconciler) reconcileBackgroundAnalysisRun(ctx context.Context, rp *v1alpha1.RolloutPlugin, currentAr *v1alpha1.AnalysisRun) (*v1alpha1.AnalysisRun, error) {
	logger := log.FromContext(ctx)

	if rp.Spec.Strategy.Canary == nil || rp.Spec.Strategy.Canary.Analysis == nil {
		return nil, nil
	}

	// Check if we need to create a new analysis run
	// Only create if:
	// 1. No current AR exists
	// 2. Rollout is aborted (allows retry after fixes)
	// 3. AR is inconclusive and rollout is paused for it
	if currentAr == nil {
		logger.Info("Creating background analysis run")
		// RolloutAnalysisBackground embeds RolloutAnalysis, so we can use it directly
		newAr, err := r.createAnalysisRun(ctx, rp, &rp.Spec.Strategy.Canary.Analysis.RolloutAnalysis, fmt.Sprintf("%s-background", rp.Name), v1alpha1.RolloutTypeBackgroundRunLabel)
		if err != nil {
			return nil, err
		}
		return newAr, nil
	}

	// If rollout is aborted, allow creating a new analysis run (for retry scenarios)
	if rp.Status.Aborted {
		logger.Info("Rollout is aborted, creating new background analysis run")
		newAr, err := r.createAnalysisRun(ctx, rp, &rp.Spec.Strategy.Canary.Analysis.RolloutAnalysis, fmt.Sprintf("%s-background", rp.Name), v1alpha1.RolloutTypeBackgroundRunLabel)
		if err != nil {
			return nil, err
		}
		return newAr, nil
	}

	// If AR is inconclusive and rollout is paused, allow creating a new one
	if currentAr.Status.Phase == v1alpha1.AnalysisPhaseInconclusive && rp.Status.Paused {
		logger.Info("Analysis is inconclusive and rollout is paused, creating new background analysis run")
		newAr, err := r.createAnalysisRun(ctx, rp, &rp.Spec.Strategy.Canary.Analysis.RolloutAnalysis, fmt.Sprintf("%s-background", rp.Name), v1alpha1.RolloutTypeBackgroundRunLabel)
		if err != nil {
			return nil, err
		}
		return newAr, nil
	}

	// Otherwise, keep the existing AR (even if failed/error - controller needs to see it)
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

// createAnalysisRun creates a new AnalysisRun from the analysis spec
func (r *RolloutPluginReconciler) createAnalysisRun(ctx context.Context, rp *v1alpha1.RolloutPlugin, analysisSpec *v1alpha1.RolloutAnalysis, namePrefix string, rolloutType string) (*v1alpha1.AnalysisRun, error) {
	logger := log.FromContext(ctx)

	// Get templates
	templates := make([]*v1alpha1.AnalysisTemplate, 0)
	for _, templateRef := range analysisSpec.Templates {
		if templateRef.ClusterScope == nil || !*templateRef.ClusterScope {
			template, err := r.ArgoProjClientset.ArgoprojV1alpha1().AnalysisTemplates(rp.Namespace).Get(ctx, templateRef.TemplateName, metav1.GetOptions{})
			if err != nil {
				logger.Error(err, "Failed to get AnalysisTemplate", "template", templateRef.TemplateName)
				return nil, err
			}
			templates = append(templates, template)
		}
	}

	// Get cluster templates
	clusterTemplates := make([]*v1alpha1.ClusterAnalysisTemplate, 0)
	for _, templateRef := range analysisSpec.Templates {
		if templateRef.ClusterScope != nil && *templateRef.ClusterScope {
			clusterTemplate, err := r.ArgoProjClientset.ArgoprojV1alpha1().ClusterAnalysisTemplates().Get(ctx, templateRef.TemplateName, metav1.GetOptions{})
			if err != nil {
				logger.Error(err, "Failed to get ClusterAnalysisTemplate", "template", templateRef.TemplateName)
				return nil, err
			}
			clusterTemplates = append(clusterTemplates, clusterTemplate)
		}
	}

	// Build labels
	labels := map[string]string{
		v1alpha1.RolloutTypeLabel: rolloutType, // "Step" or "Background"
		"rollout-plugin-name":     rp.Name,
	}

	// Add instance ID label if set (required for controller filtering)
	if r.InstanceID != "" {
		labels[v1alpha1.LabelKeyControllerInstanceID] = r.InstanceID
	}

	// Build annotations
	annotations := map[string]string{}

	// Convert args to the expected type
	convertedArgs := convertAnalysisRunArgsToArguments(analysisSpec.Args)

	// Create AnalysisRun from templates
	ar, err := analysisutil.NewAnalysisRunFromTemplates(
		templates,
		clusterTemplates,
		convertedArgs,
		analysisSpec.DryRun,
		analysisSpec.MeasurementRetention,
		labels,
		annotations,
		"",
		namePrefix+"-",
		rp.Namespace,
	)
	if err != nil {
		logger.Error(err, "Failed to create AnalysisRun from templates")
		return nil, err
	}

	// Set owner reference
	ar.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(rp, v1alpha1.SchemeGroupVersion.WithKind("RolloutPlugin")),
	})

	// Create the AnalysisRun
	createdAr, err := r.ArgoProjClientset.ArgoprojV1alpha1().AnalysisRuns(rp.Namespace).Create(ctx, ar, metav1.CreateOptions{})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			// If it already exists, get it
			logger.Info("AnalysisRun already exists, getting it", "analysisRun", ar.Name)
			return r.ArgoProjClientset.ArgoprojV1alpha1().AnalysisRuns(rp.Namespace).Get(ctx, ar.Name, metav1.GetOptions{})
		}
		logger.Error(err, "Failed to create AnalysisRun")
		return nil, err
	}

	logger.Info("Created AnalysisRun", "analysisRun", createdAr.Name)
	return createdAr, nil
}

// cancelAnalysisRuns cancels the given analysis runs
func (r *RolloutPluginReconciler) cancelAnalysisRuns(ctx context.Context, rp *v1alpha1.RolloutPlugin, ars []*v1alpha1.AnalysisRun) error {
	logger := log.FromContext(ctx)

	for _, ar := range ars {
		if ar.Spec.Terminate {
			continue
		}
		logger.Info("Canceling AnalysisRun", "analysisRun", ar.Name)
		_, err := r.ArgoProjClientset.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Patch(
			ctx,
			ar.Name,
			patchtypes.MergePatchType,
			[]byte(cancelAnalysisRun),
			metav1.PatchOptions{},
		)
		if err != nil && !k8serrors.IsNotFound(err) {
			logger.Error(err, "Failed to cancel AnalysisRun", "analysisRun", ar.Name)
			return err
		}
	}
	return nil
}

// deleteAnalysisRuns deletes the given analysis runs
func (r *RolloutPluginReconciler) deleteAnalysisRuns(ctx context.Context, ars []*v1alpha1.AnalysisRun) error {
	logger := log.FromContext(ctx)

	for _, ar := range ars {
		logger.Info("Deleting AnalysisRun", "analysisRun", ar.Name)
		err := r.ArgoProjClientset.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Delete(ctx, ar.Name, metav1.DeleteOptions{})
		if err != nil && !k8serrors.IsNotFound(err) {
			logger.Error(err, "Failed to delete AnalysisRun", "analysisRun", ar.Name)
			return err
		}
	}
	return nil
}

// setCurrentAnalysisRuns updates the status with current analysis run information
func (r *RolloutPluginReconciler) setCurrentAnalysisRuns(rp *v1alpha1.RolloutPlugin, currARs analysisutil.CurrentAnalysisRuns) {
	if currARs.CanaryBackground != nil {
		rp.Status.Canary.CurrentBackgroundAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
			Name:   currARs.CanaryBackground.Name,
			Status: currARs.CanaryBackground.Status.Phase,
		}
	} else {
		rp.Status.Canary.CurrentBackgroundAnalysisRunStatus = nil
	}

	if currARs.CanaryStep != nil {
		rp.Status.Canary.CurrentStepAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
			Name:   currARs.CanaryStep.Name,
			Status: currARs.CanaryStep.Status.Phase,
		}
	} else {
		rp.Status.Canary.CurrentStepAnalysisRunStatus = nil
	}

}
