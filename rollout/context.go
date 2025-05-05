package rollout

import (
	"context"
	"fmt"
	"slices"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/conditions"
)

type rolloutContext struct {
	reconcilerBase

	log *log.Entry
	// rollout is the rollout being reconciled
	rollout *v1alpha1.Rollout
	// newRollout is the rollout after reconciliation. used to write back to informer
	newRollout *v1alpha1.Rollout
	// newRS is the "new" ReplicaSet. Also referred to as current, or desired.
	// newRS will be nil when the pod template spec changes.
	newRS *appsv1.ReplicaSet
	// stableRS is the "stable" ReplicaSet which will be scaled up upon an abort.
	// stableRS will be nil when a Rollout is first deployed, and will be equal to newRS when fully promoted
	stableRS *appsv1.ReplicaSet
	// allRSs are all the ReplicaSets associated with the Rollout
	allRSs []*appsv1.ReplicaSet
	// olderRSs are "older" ReplicaSets -- anything which is not the newRS
	// this includes the stableRS (when in the middle of an update)
	olderRSs []*appsv1.ReplicaSet
	// otherRSs are ReplicaSets which are neither new or stable (allRSs - newRS - stableRS)
	otherRSs []*appsv1.ReplicaSet

	currentArs analysisutil.CurrentAnalysisRuns
	otherArs   []*v1alpha1.AnalysisRun

	currentEx *v1alpha1.Experiment
	otherExs  []*v1alpha1.Experiment

	newStatus         v1alpha1.RolloutStatus
	pauseContext      *pauseContext
	stepPluginContext *stepPluginContext

	// targetsVerified indicates if the pods targets have been verified with underlying LoadBalancer.
	// This is used in pod-aware flat networks where LoadBalancers target Pods and not Nodes.
	// nil indicates the check was unnecessary or not performed.
	// NOTE: we only perform target verification when we are at specific points in time
	// (e.g. a setWeight step, after a blue-green active switch, after stable service switch),
	// since we do not want to continually verify weight in case it could incur rate-limiting or other expenses.
	targetsVerified *bool
}

func (c *rolloutContext) reconcile() error {
	err := c.checkPausedConditions()
	if err != nil {
		return err
	}

	isScalingEvent, err := c.isScalingEvent()
	if err != nil {
		return err
	}

	if isScalingEvent {
		return c.syncReplicasOnly()
	}

	if c.rollout.Spec.Strategy.BlueGreen != nil {
		return c.rolloutBlueGreen()
	}

	// Due to the rollout validation before this, when we get here strategy is canary
	return c.rolloutCanary()
}

func (c *rolloutContext) SetRestartedAt() {
	c.newStatus.RestartedAt = c.rollout.Spec.RestartAt
}

func (c *rolloutContext) SetCurrentExperiment(ex *v1alpha1.Experiment) {
	c.currentEx = ex
	c.newStatus.Canary.CurrentExperiment = ex.Name
	for i, otherEx := range c.otherExs {
		if otherEx.Name == ex.Name {
			c.log.Infof("Rescued %s from inadvertent termination", ex.Name)
			c.otherExs = append(c.otherExs[:i], c.otherExs[i+1:]...)
			break
		}
	}
}

func (c *rolloutContext) SetCurrentAnalysisRuns(currARs analysisutil.CurrentAnalysisRuns) {
	c.currentArs = currARs

	if c.rollout.Spec.Strategy.Canary != nil {
		currBackgroundAr := currARs.CanaryBackground
		if currBackgroundAr != nil {
			c.newStatus.Canary.CurrentBackgroundAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
				Name:    currBackgroundAr.Name,
				Status:  currBackgroundAr.Status.Phase,
				Message: currBackgroundAr.Status.Message,
			}
		}
		currStepAr := currARs.CanaryStep
		if currStepAr != nil {
			c.newStatus.Canary.CurrentStepAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
				Name:    currStepAr.Name,
				Status:  currStepAr.Status.Phase,
				Message: currStepAr.Status.Message,
			}
		}
	} else if c.rollout.Spec.Strategy.BlueGreen != nil {
		currPrePromoAr := currARs.BlueGreenPrePromotion
		if currPrePromoAr != nil {
			c.newStatus.BlueGreen.PrePromotionAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
				Name:    currPrePromoAr.Name,
				Status:  currPrePromoAr.Status.Phase,
				Message: currPrePromoAr.Status.Message,
			}
		}
		currPostPromoAr := currARs.BlueGreenPostPromotion
		if currPostPromoAr != nil {
			c.newStatus.BlueGreen.PostPromotionAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
				Name:    currPostPromoAr.Name,
				Status:  currPostPromoAr.Status.Phase,
				Message: currPostPromoAr.Status.Message,
			}
		}
	}
}

// haltProgress returns a reason on whether or not we should halt all progress with an update
// to ReplicaSet counts (e.g. due to canary steps or blue-green promotion). This is either because
// user explicitly paused the rollout by setting `spec.paused`, or the analysis was inconclusive
func (c *rolloutContext) haltProgress() string {
	if c.rollout.Spec.Paused {
		return "user paused"
	}
	if getPauseCondition(c.rollout, v1alpha1.PauseReasonInconclusiveAnalysis) != nil {
		return "inconclusive analysis"
	}
	return ""
}

func (c *rolloutContext) getPromotedRS(newStatus *v1alpha1.RolloutStatus) *appsv1.ReplicaSet {
	i := slices.IndexFunc(c.allRSs, func(rs *appsv1.ReplicaSet) bool {
		return rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] == newStatus.StableRS
	})
	var rs *appsv1.ReplicaSet
	if i != -1 {
		rs = c.allRSs[i]
	}
	return rs
}

// calculateReplicaSetFinalStatus marks the new RS (canary RS or preview RS depending on canary or bluegreen deployment)
// as success or abort if the rollout has failed or is done. Always nil if in the middle of
// an active rollout.
func (c *rolloutContext) calculateReplicaSetFinalStatus(newStatus *v1alpha1.RolloutStatus) error {
	if newStatus.Abort {
		return c.setFinalRSStatusAbort()
	} else if conditions.RolloutCompleted(newStatus) {
		return c.setFinalRSStatusSuccess(newStatus)
	} else {
		// this is if there is a condition besides aborted/success
		// e.g. middle of a rollout that hasn't failed and isn't done yet.
		return nil
	}
}

func (c *rolloutContext) setFinalRSStatusAbort() error {
	// mark RS final status as aborted
	return c.setFinalRSStatus(c.newRS, FinalStatusAbort)
}

func (c *rolloutContext) setFinalRSStatusSuccess(newStatus *v1alpha1.RolloutStatus) error {
	// mark RS final status as success if found
	promotedRS := c.getPromotedRS(newStatus)
	if promotedRS != nil {
		err := c.setFinalRSStatus(promotedRS, FinalStatusSuccess)
		if err != nil {
			return err
		}
	} else {
		c.log.Infof("ReplicaSet not Found: %s", newStatus.StableRS)
	}

	return nil
}

func (c *rolloutContext) setFinalRSStatus(rs *appsv1.ReplicaSet, status string) error {
	ctx := context.Background()
	rs.Annotations[v1alpha1.ReplicaSetFinalStatusKey] = status
	c.log.Infof("Updating replicaset with status: %s", status)
	newRS, err := c.kubeclientset.AppsV1().ReplicaSets(rs.Namespace).Update(ctx, rs, metav1.UpdateOptions{})

	if err != nil {
		return fmt.Errorf("error updating replicaset in setFinalRSStatus %s: %w", rs.Name, err)
	}

	err = c.replicaSetInformer.GetIndexer().Update(newRS)
	if err != nil {
		return fmt.Errorf("error updating replicaset informer in setFinalRSStatus %s: %w", rs.Name, err)
	}

	return nil
}
