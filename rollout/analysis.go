package rollout

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	rolloututil "github.com/argoproj/argo-rollouts/utils/rollout"
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
	step, index := replicasetutil.GetCanaryStep(c.rollout)
	specAnalysis := func(run CurrentAnalysisRun) *v1alpha1.RolloutAnalysis {
		switch run.ARType() {
		case v1alpha1.RolloutTypeBackgroundRunLabel:
			if c.rollout.Spec.Strategy.Canary == nil || c.rollout.Spec.Strategy.Canary.Analysis == nil {
				return nil
			}
			return &c.rollout.Spec.Strategy.Canary.Analysis.RolloutAnalysis
		case v1alpha1.RolloutTypeStepLabel:
			if c.rollout.Spec.Strategy.Canary == nil || step == nil {
				return nil
			}
			return step.Analysis
		case v1alpha1.RolloutTypePrePromotionLabel:
			if c.rollout.Spec.Strategy.BlueGreen == nil {
				return nil
			}
			return c.rollout.Spec.Strategy.BlueGreen.PrePromotionAnalysis
		case v1alpha1.RolloutTypePostPromotionLabel:
			if c.rollout.Spec.Strategy.BlueGreen == nil {
				return nil
			}
			return c.rollout.Spec.Strategy.BlueGreen.PostPromotionAnalysis
		}
		return nil
	}
	if run.ShouldCancel(WithAnalysis(specAnalysis(run)), WithBackgroundAnalysis(&c.rollout.Spec.Strategy), WithStep(step), WithStepIndex(index), WithShouldSkip(shouldSkip(run.ARType(), c.rollout, c.newRS))) {
		return nil, c.analysisContext.cancelCurrentAnalysisRun(c.log, c.argoprojclientset, run)
	}

	if run.OutsideAnalysisBoundaries(WithIsFullyPromoted(rolloututil.IsFullyPromoted(c.rollout)), WithIsBeforeStartingStep(replicasetutil.BeforeStartingStep(c.rollout)), WithIsJustCreated(rolloututil.IsJustCreated(c.rollout))) {
		return nil, nil
	}

	if run.ShouldReturnCur(WithAbort(c.rollout.Status.Abort), WithConditions(c.rollout.Status.PauseConditions)) {
		return run.AnalysisRun(), nil
	}

	if run.NeedsNew(c.rollout.Status.ControllerPause, c.rollout.Status.PauseConditions, c.rollout.Status.AbortedAt) {
		podHash := replicasetutil.GetPodTemplateHash(c.newRS)
		instanceID := analysisutil.GetInstanceID(c.rollout)
		newRun, err := c.createAnalysisRun(specAnalysis(run), run.Infix(InfixWithIndex(index)), run.Labels(podHash, instanceID, WithStepIndexLabel(index)))
		run.UpdateRun(newRun)
		return newRun, err
	}

	return run.AnalysisRun(), nil
}

func (c *rolloutContext) reconcileAnalysisRuns() error {
	if c.shouldCancelAllAnalysisRuns() {
		return c.analysisContext.cancelAnalysisRuns(c.log, c.argoprojclientset)
	}
	for _, run := range c.analysisContext.AllCurrentAnalysisRuns() {
		currentRun, err := c.reconcileAnalysisRun(run)
		if err != nil {
			return err
		}
		if run.IsPresent() {
			run.setPauseOrAbort(c.pauseContext)
			c.analysisContext.UpdateCurrentAnalysisRuns(currentRun, run.ARType())
		}
	}

	c.SetCurrentAnalysisRuns()

	// TODO: this filter/cancel logic in the next 15 or so line of code can probably be pulled into the
	// analysisContext

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
		err := c.analysisContext.cancelAnalysisRun(c.log, c.argoprojclientset, ar)
		if err != nil {
			return err
		}
	}

	limitSucceedArs := defaults.GetAnalysisRunSuccessfulHistoryLimitOrDefault(c.rollout)
	limitFailedArs := defaults.GetAnalysisRunUnsuccessfulHistoryLimitOrDefault(c.rollout)
	arsToDelete := analysisutil.FilterAnalysisRunsToDelete(otherArs, c.allRSs, limitSucceedArs, limitFailedArs)
	err := c.analysisContext.deleteAnalysisRuns(c.log, c.argoprojclientset, arsToDelete)
	if err != nil {
		return err
	}

	// emits a Kubernetes event if the analysis run of that type has changed status
	for _, event := range c.analysisContext.reconcileAnalysisRunStatusChanges(
		map[string]*v1alpha1.RolloutAnalysisRunStatus{
			v1alpha1.RolloutTypeBackgroundRunLabel: c.rollout.Status.Canary.CurrentBackgroundAnalysisRunStatus,
			v1alpha1.RolloutTypeStepLabel:          c.rollout.Status.Canary.CurrentStepAnalysisRunStatus,
			v1alpha1.RolloutTypePostPromotionLabel: c.rollout.Status.BlueGreen.PostPromotionAnalysisRunStatus,
			v1alpha1.RolloutTypePrePromotionLabel:  c.rollout.Status.BlueGreen.PrePromotionAnalysisRunStatus,
		},
	) {
		c.recorder.Eventf(c.rollout, record.EventOptions{EventType: event.EventType, EventReason: event.EventReason}, event.msg)
	}

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
