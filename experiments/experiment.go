package experiments

import (
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/diff"
	experimentutil "github.com/argoproj/argo-rollouts/utils/experiment"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

func (ec *ExperimentController) reconcileExperiment(experiment *v1alpha1.Experiment, templateRSs map[string]*appsv1.ReplicaSet) error {
	logCtx := logutil.WithExperiment(experiment)

	if !experimentutil.HasStarted(experiment) {
		logCtx.Info("Experiment has not started yet")
		return ec.syncExperimentStatus(experiment, templateRSs)
	}

	if experiment.Status.AvailableAt != nil && !experimentutil.PassedDurations(experiment) {
		ec.checkEnqueueExperimentDuringRun(experiment)
	}

	statuses := experimentutil.GetTemplateStatusMapping(experiment.Status)
	for i := range experiment.Spec.Templates {
		template := experiment.Spec.Templates[i]
		logCtx.Infof("Reconciling template %s", template.Name)
		templateReady, err := ec.reconcileTemplate(experiment, template, statuses[template.Name], templateRSs)
		if err != nil {
			return err
		}
		if templateReady {
			logCtx.Infof("Not finished reconciling template %s", template.Name)
		}
	}

	return ec.syncExperimentStatus(experiment, templateRSs)
}

func (ec *ExperimentController) reconcileTemplate(experiment *v1alpha1.Experiment, template v1alpha1.TemplateSpec, templateStatus v1alpha1.TemplateStatus, templateRSs map[string]*appsv1.ReplicaSet) (bool, error) {
	name := template.Name
	existingTemplateRS, ok := templateRSs[name]
	if !ok {
		newRS, err := ec.reconcileReplicaSet(experiment, template, templateStatus)
		if err != nil {
			return false, err
		}
		templateRSs[name] = newRS
		return false, nil
	}
	templateReplicaCount := experimentutil.CalculateTemplateReplicasCount(experiment, template)
	if *existingTemplateRS.Spec.Replicas != templateReplicaCount {
		scaled, _, err := ec.scaleReplicaSetAndRecordEvent(existingTemplateRS, templateReplicaCount, experiment)
		if err != nil {
			return false, err
		}
		if scaled {
			return false, nil
		}
	}
	if templateReplicaCount != replicasetutil.GetAvailableReplicaCountForReplicaSets([]*appsv1.ReplicaSet{existingTemplateRS}) {
		return false, nil
	}
	return true, nil
}

func (ec *ExperimentController) checkEnqueueExperimentDuringRun(experiment *v1alpha1.Experiment) {
	if experiment.Status.AvailableAt == nil || experiment.Spec.Duration == nil {
		return
	}
	logCtx := logutil.WithExperiment(experiment)
	now := metav1.Now()
	startTime := experiment.Status.AvailableAt
	expiredTime := startTime.Add(time.Duration(*experiment.Spec.Duration) * time.Second)
	nextResync := now.Add(ec.resyncPeriod)
	if nextResync.After(expiredTime) && expiredTime.After(now.Time) {
		timeRemaining := expiredTime.Sub(now.Time)
		logCtx.Infof("Enqueueing Experiment in %s seconds", timeRemaining.String())
		ec.enqueueExperimentAfter(experiment, timeRemaining)
	}
}

func (ec *ExperimentController) syncExperimentStatus(experiment *v1alpha1.Experiment, templateRSs map[string]*appsv1.ReplicaSet) error {
	newStatus := v1alpha1.ExperimentStatus{
		Conditions: experiment.Status.Conditions,
	}

	newStatus.Running = experiment.Status.Running
	if !experimentutil.HasStarted(experiment) {
		newStatus.Running = pointer.BoolPtr(true)
	}

	if experimentutil.PassedDurations(experiment) {
		newStatus.Running = pointer.BoolPtr(false)
	}

	previousTemplateStatus := experimentutil.GetTemplateStatusMapping(experiment.Status)

	allAvailable := true
	for i := range experiment.Spec.Templates {
		template := experiment.Spec.Templates[i]
		templateStatus := v1alpha1.TemplateStatus{
			Name: template.Name,
		}
		if previousStatus, ok := previousTemplateStatus[template.Name]; ok {
			templateStatus.CollisionCount = previousStatus.CollisionCount
		}

		rs, ok := templateRSs[template.Name]
		if ok {
			replicaCount := defaults.GetExperimentTemplateReplicasOrDefault(template)
			templateStatus.Replicas = replicasetutil.GetActualReplicaCountForReplicaSets([]*appsv1.ReplicaSet{rs})
			templateStatus.UpdatedReplicas = replicasetutil.GetActualReplicaCountForReplicaSets([]*appsv1.ReplicaSet{rs})
			templateStatus.ReadyReplicas = replicasetutil.GetReadyReplicaCountForReplicaSets([]*appsv1.ReplicaSet{rs})
			templateStatus.AvailableReplicas = replicasetutil.GetAvailableReplicaCountForReplicaSets([]*appsv1.ReplicaSet{rs})
			if replicaCount != templateStatus.AvailableReplicas {
				allAvailable = false
			}
		} else {
			allAvailable = false
		}
		newStatus.TemplateStatuses = append(newStatus.TemplateStatuses, templateStatus)
	}

	newStatus.AvailableAt = experiment.Status.AvailableAt
	if allAvailable && experiment.Status.AvailableAt == nil {
		now := metav1.Now()
		newStatus.AvailableAt = &now
	}

	newStatus = ec.calculateExperimentConditions(experiment, newStatus, templateRSs)
	return ec.persistExperimentStatus(experiment, &newStatus)
}

func (ec *ExperimentController) persistExperimentStatus(orig *v1alpha1.Experiment, newStatus *v1alpha1.ExperimentStatus) error {
	logCtx := logutil.WithExperiment(orig)
	patch, modified, err := diff.CreateTwoWayMergePatch(
		&v1alpha1.Experiment{
			Status: orig.Status,
		},
		&v1alpha1.Experiment{
			Status: *newStatus,
		}, v1alpha1.Experiment{})
	if err != nil {
		logCtx.Errorf("Error constructing app status patch: %v", err)
		return err
	}
	if !modified {
		logCtx.Info("No status changes. Skipping patch")
		return nil
	}
	logCtx.Debugf("Experiment Patch: %s", patch)
	_, err = ec.arogProjClientset.ArgoprojV1alpha1().Experiments(orig.Namespace).Patch(orig.Name, patchtypes.MergePatchType, patch)
	if err != nil {
		logCtx.Warningf("Error updating experiment: %v", err)
		return err
	}
	logCtx.Info("Patch status successfully")
	return nil
}
