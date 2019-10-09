package rollout

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	patchtypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/kubernetes/pkg/controller"

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

func (c *RolloutController) reconcileAnalysisRuns(rollout *v1alpha1.Rollout, currentArs, otherArs []*v1alpha1.AnalysisRun, stableRS, newRS *appsv1.ReplicaSet) ([]*v1alpha1.AnalysisRun, error) {
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

	//TODO(dthomson) implement reconcileBackgroundBasedAnalysisRun

	err = c.cancelAnalysisRuns(rollout, otherArs)
	if err != nil {
		return currentArs, err
	}

	return newCurrentAnalysisRuns, nil
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
		return c.createStepBasedAnalysisRun(rollout, *index, step.Analysis, stableRS, newRS)
	}

	return currentAr, nil
}

func (c *RolloutController) createStepBasedAnalysisRun(rollout *v1alpha1.Rollout, index int32, rolloutAnalysisStep *v1alpha1.RolloutAnalysisStep, stableRS, newRS *appsv1.ReplicaSet) (*v1alpha1.AnalysisRun, error) {
	podHash := controller.ComputeHash(&rollout.Spec.Template, rollout.Status.CollisionCount)
	// Since the compute hash function is not guaranteed to be stable, we will use the podHash attached the newRS if possible.
	if newRS != nil {
		if newRsPodHash, ok := newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]; ok {
			podHash = newRsPodHash
		}
	}
	analysisRunLabels := analysisutil.StepLabels(rollout, index, podHash)
	args := analysisutil.BuildArgumentsForRolloutAnalysisRun(rolloutAnalysisStep, stableRS, newRS)
	ar, err := c.getAnalysisRunFromRollout(rollout, rolloutAnalysisStep, args, podHash, analysisRunLabels)
	if err != nil {
		return nil, err
	}
	ar, err = c.argoprojclientset.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Create(ar)
	if err != nil {
		return nil, err
	}
	logutil.WithRollout(rollout).WithField(logutil.AnalysisRunKey, ar.Name).Infof("Created AnalysisRun for step '%d'", index)
	return ar, nil
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
			GenerateName: fmt.Sprintf("%s-%s-%s", r.Name, rolloutAnalysisStep.TemplateName, podHash),
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
