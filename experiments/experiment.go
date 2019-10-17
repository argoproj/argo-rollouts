package experiments

import (
	"time"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	experimentutil "github.com/argoproj/argo-rollouts/utils/experiment"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

type experimentContext struct {
	log                    *log.Entry
	ex                     *v1alpha1.Experiment
	templateRSs            map[string]*appsv1.ReplicaSet
	kubeclientset          kubernetes.Interface
	argoProjClientset      clientset.Interface
	replicaSetLister       appslisters.ReplicaSetLister
	recorder               record.EventRecorder
	enqueueExperimentAfter func(obj interface{}, duration time.Duration)
}

func (ec *experimentContext) reconcile() (*v1alpha1.ExperimentStatus, error) {
	if !experimentutil.HasStarted(ec.ex) {
		ec.log.Info("Experiment has not started yet")
		return ec.calculateStatus(), nil
	}

	passedDuration, _ := experimentutil.PassedDurations(ec.ex)
	if ec.ex.Status.AvailableAt != nil && !passedDuration {
		ec.checkEnqueueExperimentDuringRun()
	}

	statuses := experimentutil.GetTemplateStatusMapping(ec.ex.Status)
	for i := range ec.ex.Spec.Templates {
		template := ec.ex.Spec.Templates[i]
		ec.log.Infof("Reconciling template %s", template.Name)
		templateReady, err := ec.reconcileTemplate(template, statuses[template.Name])
		if err != nil {
			return nil, err
		}
		if templateReady {
			ec.log.Infof("Not finished reconciling template %s", template.Name)
		}
	}

	return ec.calculateStatus(), nil
}

func (ec *experimentContext) reconcileTemplate(template v1alpha1.TemplateSpec, templateStatus v1alpha1.TemplateStatus) (bool, error) {
	name := template.Name
	existingTemplateRS, ok := ec.templateRSs[name]
	if !ok {
		newRS, err := ec.reconcileReplicaSet(template, templateStatus)
		if err != nil {
			return false, err
		}
		ec.templateRSs[name] = newRS
		return false, nil
	}
	templateReplicaCount := experimentutil.CalculateTemplateReplicasCount(ec.ex, template)
	if *existingTemplateRS.Spec.Replicas != templateReplicaCount {
		scaled, _, err := ec.scaleReplicaSetAndRecordEvent(existingTemplateRS, templateReplicaCount)
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

func (ec *experimentContext) checkEnqueueExperimentDuringRun() {
	if passed, timeRemaining := experimentutil.PassedDurations(ec.ex); !passed {
		ec.log.Infof("Enqueueing Experiment in %s seconds", timeRemaining.String())
		ec.enqueueExperimentAfter(ec.ex, timeRemaining)
	}
}

func (ec *experimentContext) calculateStatus() *v1alpha1.ExperimentStatus {
	newStatus := v1alpha1.ExperimentStatus{
		Conditions: ec.ex.Status.Conditions,
	}

	newStatus.Running = ec.ex.Status.Running
	if !experimentutil.HasStarted(ec.ex) {
		newStatus.Running = pointer.BoolPtr(true)
	}

	if passed, _ := experimentutil.PassedDurations(ec.ex); passed {
		newStatus.Running = pointer.BoolPtr(false)
	}

	previousTemplateStatus := experimentutil.GetTemplateStatusMapping(ec.ex.Status)

	allAvailable := true
	for i := range ec.ex.Spec.Templates {
		template := ec.ex.Spec.Templates[i]
		templateStatus := v1alpha1.TemplateStatus{
			Name: template.Name,
		}
		if previousStatus, ok := previousTemplateStatus[template.Name]; ok {
			templateStatus.CollisionCount = previousStatus.CollisionCount
		}

		rs, ok := ec.templateRSs[template.Name]
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

	newStatus.AvailableAt = ec.ex.Status.AvailableAt
	if allAvailable && ec.ex.Status.AvailableAt == nil {
		now := metav1.Now()
		newStatus.AvailableAt = &now
	}
	return calculateExperimentConditions(ec.ex, newStatus)
}
