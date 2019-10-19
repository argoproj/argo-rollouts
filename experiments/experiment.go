package experiments

import (
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	rolloutslisters "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	experimentutil "github.com/argoproj/argo-rollouts/utils/experiment"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

type experimentContext struct {
	// parameters supplied to the context
	ex                     *v1alpha1.Experiment
	templateRSs            map[string]*appsv1.ReplicaSet
	kubeclientset          kubernetes.Interface
	argoProjClientset      clientset.Interface
	analysisTemplateLister rolloutslisters.AnalysisTemplateLister
	analysisRunLister      rolloutslisters.AnalysisRunLister
	replicaSetLister       appslisters.ReplicaSetLister
	recorder               record.EventRecorder
	enqueueExperimentAfter func(obj interface{}, duration time.Duration)

	// calculated values during reconciliation
	log       *log.Entry
	newStatus *v1alpha1.ExperimentStatus
	// if isTerminating is true, will not create any analysis runs or replicasets, will scale down
	// all existing replicasets, and terminate all analysis runs
	isTerminating bool
}

func newExperimentContext(
	experiment *v1alpha1.Experiment,
	templateRSs map[string]*appsv1.ReplicaSet,
	kubeclientset kubernetes.Interface,
	argoProjClientset clientset.Interface,
	replicaSetLister appslisters.ReplicaSetLister,
	recorder record.EventRecorder,
	enqueueExperimentAfter func(obj interface{}, duration time.Duration),
) *experimentContext {

	exCtx := experimentContext{
		ex:                     experiment,
		templateRSs:            templateRSs,
		kubeclientset:          kubeclientset,
		argoProjClientset:      argoProjClientset,
		replicaSetLister:       replicaSetLister,
		recorder:               recorder,
		enqueueExperimentAfter: enqueueExperimentAfter,

		log:           log.WithField(logutil.ExperimentKey, experiment.Name).WithField(logutil.NamespaceKey, experiment.Namespace),
		newStatus:     experiment.Status.DeepCopy(),
		isTerminating: experimentutil.IsTerminating(experiment),
	}
	return &exCtx
}

func (ec *experimentContext) reconcile() *v1alpha1.ExperimentStatus {
	if !experimentutil.HasStarted(ec.ex) {
		ec.log.Info("Experiment has not started yet")
		return ec.calculateStatus()
	}

	ec.enqueueAfterDuration()

	for _, template := range ec.ex.Spec.Templates {
		ec.reconcileTemplate(template)
	}

	for _, analysis := range ec.ex.Spec.Analyses {
		ec.reconcileAnalysisRun(analysis)
	}

	return ec.calculateStatus()
}

func (ec *experimentContext) reconcileTemplates() error {
	var origErr error
	statuses := experimentutil.GetTemplateStatusMapping(ec.ex.Status)
	for _, template := range ec.ex.Spec.Templates {
		templateReady, err := ec.reconcileTemplateOld(template, statuses[template.Name])
		if err != nil && origErr == nil {
			origErr = err
		}
		if !templateReady {
			ec.log.Infof("Not finished reconciling template %s", template.Name)
		}
	}
	return origErr
}

// reconcileTemplate reconciles a template to a ReplicaSet. Creates or scales them down as necessary
func (ec *experimentContext) reconcileTemplate(template v1alpha1.TemplateSpec) {
	ec.log.Infof("Reconciling template %s", template.Name)
	templateStatus := experimentutil.GetTemplateStatus(ec.ex.Status, template.Name)
	var collisionCount *int32
	if templateStatus != nil {
		collisionCount = templateStatus.CollisionCount
	}
	existingTemplateRS, replicaSetExists := ec.templateRSs[template.Name]
	if !replicaSetExists {
		if ec.isTerminating {
			ec.log.Warnf("Skipping ReplicaSet creation for template %s: experiment is terminating", template.Name)
			return
		}
		newRS, err := ec.createReplicaSet(template, collisionCount)
		if err != nil {
			ec.log.Warnf("Failed to create ReplicaSet: %v", err)
			return
		}
		if newRS != nil {
			ec.templateRSs[template.Name] = newRS
		}
		return
	}

	// If we get here, replicaset exists. We need to ensure it's scaled properly based on
	// termination, or changed replica count
	var templateReplicaCount int32 = 0
	if !ec.isTerminating {
		templateReplicaCount = experimentutil.CalculateTemplateReplicasCount(ec.ex, template)
	}
	if *existingTemplateRS.Spec.Replicas != templateReplicaCount {
		ec.scaleReplicaSetAndRecordEvent(existingTemplateRS, templateReplicaCount)
	}
}

// reconcileTemplate reconciles a template to a ReplicaSet. Creates or deletes them as necessary
func (ec *experimentContext) reconcileTemplateOld(template v1alpha1.TemplateSpec, templateStatus v1alpha1.TemplateStatus) (bool, error) {
	ec.log.Infof("Reconciling template %s", template.Name)
	existingTemplateRS, ok := ec.templateRSs[template.Name]
	if !ok {
		if ec.isTerminating {
			ec.log.Warnf("Skipping ReplicaSet creation for template %s since experiment is terminating", template.Name)
			return false, nil
		} else {
			newRS, err := ec.createReplicaSet(template, templateStatus.CollisionCount)
			if err != nil {
				return false, err
			}
			ec.templateRSs[template.Name] = newRS
			return false, nil
		}
	}

	// If we get here, replicaset exists. We need to ensure it's scaled properly based on
	// termination, or new replica count
	var templateReplicaCount int32 = 0
	if !ec.isTerminating {
		templateReplicaCount = experimentutil.CalculateTemplateReplicasCount(ec.ex, template)
	}
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

// enqueueAfterDuration enqueues the experiment at the appropriate duration time after status.availableAt
func (ec *experimentContext) enqueueAfterDuration() {
	// TODO(jessesuen): we need to requeue for ProgressDeadlineSeconds too

	if ec.isTerminating {
		return
	}
	if ec.ex.Spec.Duration == nil {
		return
	}
	if ec.ex.Status.AvailableAt == nil {
		return
	}
	passedDuration, timeRemaining := experimentutil.PassedDurations(ec.ex)
	if passedDuration {
		return
	}
	ec.log.Infof("Enqueueing Experiment in %s seconds", timeRemaining.String())
	ec.enqueueExperimentAfter(ec.ex, timeRemaining)
}

// reconcileAnalysisRun reconciles a single analysis run, creating or terminating it as necessary.
// Updates the analysis run statuses, which may subsequently fail the experiment.
func (ec *experimentContext) reconcileAnalysisRun(analysis v1alpha1.ExperimentAnalysisTemplateRef) {
	if ec.ex.Status.AvailableAt == nil {
		return
	}
	ec.log.Infof("Reconciling analysis %s", analysis.Name)

	// setAnalysisRunStatus is a convenience method to:
	// 1. update the runStatus
	// 2. log a message and emit an event on status changess
	setAnalysisRunStatus := func(runStatus v1alpha1.ExperimentAnalysisRunStatus) {
		if ec.newStatus.Status != runStatus.Status {
			msg := fmt.Sprintf("Analysis '%s' transitioned from %s -> %s", analysis.Name, ec.newStatus.Status, runStatus)
			if runStatus.Message != "" {
				msg = ": " + runStatus.Message
			}
			switch runStatus.Status {
			case v1alpha1.AnalysisStatusFailed, v1alpha1.AnalysisStatusError, v1alpha1.AnalysisStatusInconclusive:
				ec.recorder.Event(ec.ex, corev1.EventTypeWarning, string(runStatus.Status), msg)
			default:
				ec.recorder.Event(ec.ex, corev1.EventTypeNormal, string(runStatus.Status), msg)
			}
		}
		experimentutil.SetAnalysisRunStatus(ec.newStatus, runStatus)
	}

	runStatus := experimentutil.GetAnalysisRunStatus(ec.ex.Status, analysis.Name)
	if runStatus == nil {
		// AnalysisRun needs to be created (unless we are terminating)
		if ec.isTerminating {
			ec.log.Warnf("Skipping AnalysisRun creation for analysis %s: experiment is terminating", analysis.Name)
			return
		}
		runStatus = ec.createAnalysisRun(analysis)
		setAnalysisRunStatus(*runStatus)
		return
	}

	// If we get here, analysis run has been previously created and we are just checking its status
	if runStatus.Status.Completed() {
		// runStatus is already completed. nothing to do
		return
	}

	if ec.isTerminating {
		ec.log.Warnf("Terminating %s (%s)", analysis.Name, runStatus.AnalysisRun)
		analysisRunIf := ec.argoProjClientset.ArgoprojV1alpha1().AnalysisRuns(ec.ex.Namespace)
		err := analysisutil.TerminateRun(analysisRunIf, runStatus.AnalysisRun)
		if err != nil {
			runStatus.Status = v1alpha1.AnalysisStatusError
			runStatus.Message = err.Error()
			setAnalysisRunStatus(*runStatus)
		}
		return
	}

	run, err := ec.analysisRunLister.AnalysisRuns(ec.ex.Namespace).Get(runStatus.AnalysisRun)
	if err != nil {
		runStatus.Status = v1alpha1.AnalysisStatusError
		runStatus.Message = err.Error()
		setAnalysisRunStatus(*runStatus)
		return
	}

	runStatus.Status = run.Status.Status
	runStatus.Message = run.Status.Message
	setAnalysisRunStatus(*runStatus)
}

func (ec *experimentContext) createAnalysisRun(analysis v1alpha1.ExperimentAnalysisTemplateRef) *v1alpha1.ExperimentAnalysisRunStatus {
	analysisRunIf := ec.argoProjClientset.ArgoprojV1alpha1().AnalysisRuns(ec.ex.Namespace)
	runStatus := &v1alpha1.ExperimentAnalysisRunStatus{
		Name: analysis.Name,
	}
	run, err := ec.newAnalysisRun(analysis, analysis.Arguments)
	if err != nil {
		runStatus.Status = v1alpha1.AnalysisStatusError
		runStatus.Message = err.Error()
		return runStatus
	}
	run, err = analysisRunIf.Create(run)
	if err != nil {
		runStatus.Status = v1alpha1.AnalysisStatusError
		runStatus.Message = err.Error()
		return runStatus
	}
	runStatus.AnalysisRun = run.Name
	runStatus.Status = v1alpha1.AnalysisStatusPending
	ec.log.Infof("Created %s", analysis.Name)
	return runStatus
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

// newAnalysisRun generates an AnalysisRun from the experiment and template
func (ec *experimentContext) newAnalysisRun(analysis v1alpha1.ExperimentAnalysisTemplateRef, args []v1alpha1.Argument) (*v1alpha1.AnalysisRun, error) {
	template, err := ec.analysisTemplateLister.AnalysisTemplates(ec.ex.Namespace).Get(analysis.TemplateName)
	if err != nil {
		return nil, err
	}

	ar := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName:    fmt.Sprintf("%s-%s-", ec.ex.Name, analysis.Name),
			Namespace:       ec.ex.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ec.ex, controllerKind)},
		},
		Spec: v1alpha1.AnalysisRunSpec{
			AnalysisSpec: template.Spec,
			Arguments:    args,
		},
	}
	return &ar, nil
}
