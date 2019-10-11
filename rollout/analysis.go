package rollout

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
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
			"terminated": true
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

func (c *RolloutController) reconcileAnalysisRuns(rollout *v1alpha1.Rollout, currentArs, otherArs []*v1alpha1.AnalysisRun, stableRS, newRS *appsv1.ReplicaSet, olderRSs []*appsv1.ReplicaSet) ([]*v1alpha1.AnalysisRun, error) {
	if rollout.Spec.Paused {
		return currentArs, nil
	}
	newCurrentAnalysisRuns := []*v1alpha1.AnalysisRun{}

	stepAnalysisRun, err := c.reconcileStepBasedAnalysisRun(rollout, currentArs, stableRS, newRS)
	if err != nil {
		return currentArs, err
	}
	if stepAnalysisRun != nil {
		newCurrentAnalysisRuns = append(newCurrentAnalysisRuns, stepAnalysisRun)
	}

	backgroundAnalysisRun, err := c.reconcileBackgroundAnalysisRun(rollout, currentArs, stableRS, newRS)
	if err != nil {
		return currentArs, err
	}
	if backgroundAnalysisRun != nil {
		newCurrentAnalysisRuns = append(newCurrentAnalysisRuns, backgroundAnalysisRun)
	}

	err = c.cancelAnalysisRuns(rollout, otherArs)
	if err != nil {
		return currentArs, err
	}

	allRSs := append(olderRSs, newRS)
	if stableRS != nil {
		allRSs = append(allRSs, stableRS)
	}
	arsToDelete := analysisutil.FilterAnalysisRunsToDelete(otherArs, allRSs)
	err = c.deleteAnalysisRuns(rollout, arsToDelete)
	if err != nil {
		return currentArs, err
	}

	return newCurrentAnalysisRuns, nil
}

func (c *RolloutController) reconcileBackgroundAnalysisRun(rollout *v1alpha1.Rollout, currentArs []*v1alpha1.AnalysisRun, stableRS, newRS *appsv1.ReplicaSet) (*v1alpha1.AnalysisRun, error) {
	currentAr := analysisutil.FilterAnalysisRunsByName(currentArs, rollout.Status.Canary.CurrentBackgroundAnalysisRun)
	if rollout.Spec.Strategy.CanaryStrategy.Analysis == nil {
		err := c.cancelAnalysisRuns(rollout, []*v1alpha1.AnalysisRun{currentAr})
		if err != nil {
			return nil, err
		}
		return nil, err
	}
	if currentAr == nil {
		podHash := replicasetutil.GetPodTemplateHash(newRS)
		backgroundLabels := analysisutil.BackgroundLabels(podHash)
		currentAr, err := c.createAnalysisRun(rollout, rollout.Spec.Strategy.CanaryStrategy.Analysis, stableRS, newRS, backgroundLabels)
		if err == nil {
			logutil.WithRollout(rollout).WithField(logutil.AnalysisRunKey, currentAr.Name).Info("Created background AnalysisRun")
		}
		return currentAr, err
	}
	return currentAr, nil
}

func (c *RolloutController) createAnalysisRun(rollout *v1alpha1.Rollout, rolloutAnalysisStep *v1alpha1.RolloutAnalysisStep, stableRS, newRS *appsv1.ReplicaSet, labels map[string]string) (*v1alpha1.AnalysisRun, error) {
	args := analysisutil.BuildArgumentsForRolloutAnalysisRun(rolloutAnalysisStep, stableRS, newRS)
	podHash := replicasetutil.GetPodTemplateHash(newRS)
	if podHash == "" {
		return nil, fmt.Errorf("Latest ReplicaSet '%s' has no pod hash in the labels", newRS.Name)
	}
	ar, err := c.getAnalysisRunFromRollout(rollout, rolloutAnalysisStep, args, podHash, labels)
	if err != nil {
		return nil, err
	}
	ar, err = c.argoprojclientset.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Create(ar)
	if err != nil {
		return nil, err
	}
	return ar, nil
}

func (c *RolloutController) reconcileStepBasedAnalysisRun(rollout *v1alpha1.Rollout, currentArs []*v1alpha1.AnalysisRun, stableRS, newRS *appsv1.ReplicaSet) (*v1alpha1.AnalysisRun, error) {
	step, index := replicasetutil.GetCurrentCanaryStep(rollout)
	currentAr := analysisutil.FilterAnalysisRunsByName(currentArs, rollout.Status.Canary.CurrentStepAnalysisRun)
	if step == nil || step.Analysis == nil || index == nil {
		err := c.cancelAnalysisRuns(rollout, []*v1alpha1.AnalysisRun{currentAr})
		if err != nil {
			return nil, err
		}
		return nil, err
	}
	if currentAr == nil {
		podHash := replicasetutil.GetPodTemplateHash(newRS)
		stepLabels := analysisutil.StepLabels(*index, podHash)
		currentAr, err := c.createAnalysisRun(rollout, step.Analysis, stableRS, newRS, stepLabels)
		if err == nil {
			logutil.WithRollout(rollout).WithField(logutil.AnalysisRunKey, currentAr.Name).Infof("Created AnalysisRun for step '%d'", *index)
		}
		return currentAr, err
	}

	return currentAr, nil
}

func (c *RolloutController) cancelAnalysisRuns(r *v1alpha1.Rollout, analysisRuns []*v1alpha1.AnalysisRun) error {
	logctx := logutil.WithRollout(r)
	for i := range analysisRuns {
		ar := analysisRuns[i]
		isNotCompleted := ar == nil || ar.Status == nil || !ar.Status.Status.Completed()
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

// getAnalysisRunFromRollout generates an AnalysisRun from the rollouts, the AnalysisRun Step, the new/stable ReplicaSet, and any extra objects.
func (c *RolloutController) getAnalysisRunFromRollout(r *v1alpha1.Rollout, rolloutAnalysisStep *v1alpha1.RolloutAnalysisStep, args []v1alpha1.Argument, podHash string, labels map[string]string) (*v1alpha1.AnalysisRun, error) {
	logctx := logutil.WithRollout(r)
	template, err := c.analysisTemplateLister.AnalysisTemplates(r.Namespace).Get(rolloutAnalysisStep.TemplateName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logctx.Warnf("AnalysisTemplate '%s' not found", rolloutAnalysisStep.TemplateName)
		}
		return nil, err
	}

	ar := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-%s-%s-", r.Name, rolloutAnalysisStep.TemplateName, podHash),
			Namespace:    r.Namespace,
			Labels:       labels,
			Annotations: map[string]string{
				annotations.RevisionAnnotation: r.Annotations[annotations.RevisionAnnotation],
			},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(r, controllerKind)},
		},
		Spec: v1alpha1.AnalysisRunSpec{
			AnalysisSpec: template.Spec,
			Arguments:    args,
		},
	}
	return &ar, nil
}

func (c *RolloutController) deleteAnalysisRuns(rollout *v1alpha1.Rollout, ars []*v1alpha1.AnalysisRun) error {
	logCtx := logutil.WithRollout(rollout)
	for i := range ars {
		ar := ars[i]
		if ar.DeletionTimestamp != nil {
			continue
		}
		logCtx.Infof("Trying to cleanup analysis run '%s'", ar.Name)
		err := c.argoprojclientset.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Delete(ar.Name, nil)
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
