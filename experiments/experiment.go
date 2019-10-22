package experiments

import (
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/record"

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
	analysisTemplateLister rolloutslisters.AnalysisTemplateLister,
	analysisRunLister rolloutslisters.AnalysisRunLister,
	recorder record.EventRecorder,
	enqueueExperimentAfter func(obj interface{}, duration time.Duration),
) *experimentContext {

	exCtx := experimentContext{
		ex:                     experiment,
		templateRSs:            templateRSs,
		kubeclientset:          kubeclientset,
		argoProjClientset:      argoProjClientset,
		replicaSetLister:       replicaSetLister,
		analysisTemplateLister: analysisTemplateLister,
		analysisRunLister:      analysisRunLister,
		recorder:               recorder,
		enqueueExperimentAfter: enqueueExperimentAfter,

		log:           log.WithField(logutil.ExperimentKey, experiment.Name).WithField(logutil.NamespaceKey, experiment.Namespace),
		newStatus:     experiment.Status.DeepCopy(),
		isTerminating: experimentutil.IsTerminating(experiment),
	}
	return &exCtx
}

func (ec *experimentContext) reconcile() *v1alpha1.ExperimentStatus {
	for _, template := range ec.ex.Spec.Templates {
		ec.reconcileTemplate(template)
	}

	for _, analysis := range ec.ex.Spec.Analyses {
		ec.reconcileAnalysisRun(analysis)
	}
	ec.enqueueAfterDuration()
	return ec.calculateStatus()
}

// reconcileTemplate reconciles a template to a ReplicaSet. Creates or scales them down as necessary
// will update status.templateStatuses with the current assessed values
func (ec *experimentContext) reconcileTemplate(template v1alpha1.TemplateSpec) {
	ec.log.Infof("Reconciling template %s", template.Name)
	templateStatus := experimentutil.GetTemplateStatus(ec.ex.Status, template.Name)
	if templateStatus == nil {
		templateStatus = &v1alpha1.TemplateStatus{
			Name: template.Name,
		}
	}
	prevStatus := templateStatus.DeepCopy()
	var desiredReplicaCount int32 = 0
	if !ec.isTerminating && !templateStatus.Status.Completed() {
		desiredReplicaCount = experimentutil.CalculateTemplateReplicasCount(ec.ex, template)
	}
	now := metav1.Now()

	rs := ec.templateRSs[template.Name]
	if rs == nil {
		// Create the ReplicaSet if necessary
		if templateStatus.Status.Completed() {
			// do nothing (not even pollute the logs)
		} else if ec.isTerminating {
			ec.log.Infof("Skipping ReplicaSet creation for template %s: experiment is terminating", template.Name)
		} else {
			newRS, err := ec.createReplicaSet(template, templateStatus.CollisionCount)
			if err != nil {
				ec.log.Warnf("Failed to create ReplicaSet: %v", err)
				if !k8serrors.IsAlreadyExists(err) {
					templateStatus.Status = v1alpha1.TemplateStatusError
					templateStatus.Message = fmt.Sprintf("Failed to create ReplicaSet for template '%s': %v", template.Name, err)
				}
			}
			if newRS != nil {
				ec.templateRSs[template.Name] = newRS
				templateStatus.LastTransitionTime = &now
			}
		}
		templateStatus.Replicas = 0
		templateStatus.UpdatedReplicas = 0
		templateStatus.ReadyReplicas = 0
		templateStatus.AvailableReplicas = 0
	} else {
		// If we get here, replicaset exists. We need to ensure it's scaled properly based on
		// termination, or changed replica count
		if *rs.Spec.Replicas != desiredReplicaCount {
			ec.scaleReplicaSetAndRecordEvent(rs, desiredReplicaCount)
		}
		templateStatus.Replicas = replicasetutil.GetActualReplicaCountForReplicaSets([]*appsv1.ReplicaSet{rs})
		templateStatus.UpdatedReplicas = replicasetutil.GetActualReplicaCountForReplicaSets([]*appsv1.ReplicaSet{rs})
		templateStatus.ReadyReplicas = replicasetutil.GetReadyReplicaCountForReplicaSets([]*appsv1.ReplicaSet{rs})
		templateStatus.AvailableReplicas = replicasetutil.GetAvailableReplicaCountForReplicaSets([]*appsv1.ReplicaSet{rs})
	}

	if prevStatus.Replicas != templateStatus.Replicas ||
		prevStatus.UpdatedReplicas != templateStatus.UpdatedReplicas ||
		prevStatus.ReadyReplicas != templateStatus.ReadyReplicas ||
		prevStatus.AvailableReplicas != templateStatus.AvailableReplicas {

		ec.log.Infof("Template '%s' progressed from (C:%d, U:%d, R:%d, A:%d) to (C:%d, U:%d, R:%d, A:%d)", template.Name,
			prevStatus.Replicas, prevStatus.UpdatedReplicas, prevStatus.ReadyReplicas, prevStatus.AvailableReplicas,
			templateStatus.Replicas, templateStatus.UpdatedReplicas, templateStatus.ReadyReplicas, templateStatus.AvailableReplicas,
		)
		templateStatus.LastTransitionTime = &now
	}

	// Don't allow template statuses to transition out of completed statuses
	if !templateStatus.Status.Completed() {
		if desiredReplicaCount == templateStatus.AvailableReplicas {
			passedDuration, _ := experimentutil.PassedDurations(ec.ex)
			if passedDuration {
				templateStatus.Status = v1alpha1.TemplateStatusSuccessful
			} else if ec.isTerminating {
				templateStatus.Status = v1alpha1.TemplateStatusSuccessful
			} else {
				templateStatus.Status = v1alpha1.TemplateStatusRunning
			}
		} else {
			from := now
			if templateStatus.LastTransitionTime != nil {
				from = *templateStatus.LastTransitionTime
			}
			progressDeadlineSeconds := defaults.GetExperimentProgressDeadlineSecondsOrDefault(ec.ex)
			deadline := from.Add(time.Duration(progressDeadlineSeconds) * time.Second)
			if now.Time.After(deadline) {
				templateStatus.Status = v1alpha1.TemplateStatusFailed
				templateStatus.Message = fmt.Sprintf("Template '%s' exceeded its progressDeadlineSeconds (%d)", template.Name, progressDeadlineSeconds)
			} else if ec.isTerminating {
				templateStatus.Status = v1alpha1.TemplateStatusSuccessful
			} else {
				templateStatus.Status = v1alpha1.TemplateStatusProgressing
			}
		}
	}

	if prevStatus.Status != templateStatus.Status {
		msg := fmt.Sprintf("Template '%s' transitioned from %s -> %s", template.Name, prevStatus.Status, templateStatus.Status)
		if templateStatus.Message != "" {
			msg = msg + ": " + templateStatus.Message
		}
		ec.log.Info(msg)
		switch templateStatus.Status {
		case v1alpha1.TemplateStatusFailed, v1alpha1.TemplateStatusError:
			ec.recorder.Event(ec.ex, corev1.EventTypeWarning, string(templateStatus.Status), msg)
		default:
			ec.recorder.Event(ec.ex, corev1.EventTypeNormal, string(templateStatus.Status), msg)
		}
	}
	experimentutil.SetTemplateStatus(ec.newStatus, *templateStatus)
}

// enqueueAfterDuration enqueues the experiment at the appropriate duration time after status.availableAt
func (ec *experimentContext) enqueueAfterDuration() {
	// TODO(jessesuen): we need to requeue for ProgressDeadlineSeconds too
	if !experimentutil.HasStarted(ec.ex) {
		return
	}
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

	prevStatus := experimentutil.GetAnalysisRunStatus(ec.ex.Status, analysis.Name)
	if prevStatus == nil {
		prevStatus = &v1alpha1.ExperimentAnalysisRunStatus{
			Name: analysis.Name,
		}
	}
	newStatus := prevStatus.DeepCopy()

	// setAnalysisRunStatus is a convenience method to:
	// 1. update the runStatus
	// 2. log a message and emit an event on status changess
	defer func() {
		if prevStatus.Status != newStatus.Status {
			msg := fmt.Sprintf("Analysis '%s' transitioned from %s -> %s", analysis.Name, prevStatus.Status, newStatus.Status)
			if newStatus.Message != "" {
				msg = msg + ": " + newStatus.Message
			}
			ec.log.Info(msg)
			switch newStatus.Status {
			case v1alpha1.AnalysisStatusFailed, v1alpha1.AnalysisStatusError, v1alpha1.AnalysisStatusInconclusive:
				ec.recorder.Event(ec.ex, corev1.EventTypeWarning, string(newStatus.Status), msg)
			default:
				ec.recorder.Event(ec.ex, corev1.EventTypeNormal, string(newStatus.Status), msg)
			}
		}
		experimentutil.SetAnalysisRunStatus(ec.newStatus, *newStatus)
	}()

	if prevStatus.AnalysisRun == "" {
		// AnalysisRun needs to be created (unless we are terminating)
		if ec.isTerminating {
			ec.log.Warnf("Skipping AnalysisRun creation for analysis %s: experiment is terminating", analysis.Name)
			return
		}
		run, err := ec.createAnalysisRun(analysis)
		if err != nil {
			msg := fmt.Sprintf("Failed to create AnalysisRun for analysis '%s': %v", analysis.Name, err.Error())
			newStatus.Status = v1alpha1.AnalysisStatusError
			newStatus.Message = msg
			ec.log.Warn(msg)
		} else {
			newStatus.Status = v1alpha1.AnalysisStatusPending
			newStatus.AnalysisRun = run.Name
			ec.log.Infof("Created %s", run.Name)
		}
		return
	}

	// If we get here, analysis run has been previously created and we are just checking its status
	if prevStatus.Status.Completed() {
		// runStatus is already completed. nothing to do
		return
	}

	run, err := ec.analysisRunLister.AnalysisRuns(ec.ex.Namespace).Get(prevStatus.AnalysisRun)
	if err != nil {
		newStatus.Status = v1alpha1.AnalysisStatusError
		newStatus.Message = err.Error()
		return
	}

	if ec.isTerminating {
		if !run.Status.Status.Completed() && !run.Spec.Terminate {
			msg := fmt.Sprintf("Terminating %s (%s)", analysis.Name, run.Name)
			ec.log.Warnf(msg)
			ec.recorder.Event(ec.ex, corev1.EventTypeNormal, "Terminate", msg)
			analysisRunIf := ec.argoProjClientset.ArgoprojV1alpha1().AnalysisRuns(ec.ex.Namespace)
			err := analysisutil.TerminateRun(analysisRunIf, run.Name)
			if err != nil {
				newStatus.Status = v1alpha1.AnalysisStatusError
				newStatus.Message = err.Error()
			}
		}
		return
	}

	if run.Status != nil {
		newStatus.Status = run.Status.Status
		newStatus.Message = run.Status.Message
	}
}

func (ec *experimentContext) createAnalysisRun(analysis v1alpha1.ExperimentAnalysisTemplateRef) (*v1alpha1.AnalysisRun, error) {
	analysisRunIf := ec.argoProjClientset.ArgoprojV1alpha1().AnalysisRuns(ec.ex.Namespace)
	run, err := ec.newAnalysisRun(analysis, analysis.Arguments)
	if err != nil {
		return nil, err
	}
	return analysisRunIf.Create(run)
}

func (ec *experimentContext) calculateStatus() *v1alpha1.ExperimentStatus {
	prevStatus := ec.newStatus.DeepCopy()
	switch ec.newStatus.Status {
	case "":
		ec.newStatus.Status = v1alpha1.AnalysisStatusPending
	case v1alpha1.AnalysisStatusPending:
		ec.newStatus.Status = ec.assessTemplates()
		if ec.newStatus.Status == v1alpha1.AnalysisStatusRunning {
			now := metav1.Now()
			ec.newStatus.AvailableAt = &now
		}
	case v1alpha1.AnalysisStatusRunning:
		ec.newStatus.Status = ec.assessTemplates()
		if passed, _ := experimentutil.PassedDurations(ec.ex); passed {
			ec.newStatus.Status = ec.assessAnalysisRuns()
		}
	}
	ec.newStatus = calculateExperimentConditions(ec.ex, *ec.newStatus)
	if prevStatus.Status != ec.newStatus.Status {
		msg := fmt.Sprintf("Experiment transitioned from %s -> %s", prevStatus.Status, ec.newStatus.Status)
		ec.log.Info(msg)
		switch ec.newStatus.Status {
		case v1alpha1.AnalysisStatusError, v1alpha1.AnalysisStatusFailed, v1alpha1.AnalysisStatusInconclusive:
			ec.recorder.Event(ec.ex, corev1.EventTypeWarning, string(ec.newStatus.Status), msg)
		default:
			ec.recorder.Event(ec.ex, corev1.EventTypeNormal, string(ec.newStatus.Status), msg)
		}
	}
	return ec.newStatus
}

// assessTemplates looks at all the template statuses, and gives an assessment to be used for the
// experiment status.
func (ec *experimentContext) assessTemplates() v1alpha1.AnalysisStatus {
	worstStatus := v1alpha1.TemplateStatusSuccessful
	for _, template := range ec.ex.Spec.Templates {
		templateStatus := experimentutil.GetTemplateStatus(*ec.newStatus, template.Name)
		if templateStatus == nil {
			if experimentutil.TemplateIsWorse(worstStatus, v1alpha1.TemplateStatusProgressing) {
				worstStatus = v1alpha1.TemplateStatusProgressing
			}
		} else {
			if experimentutil.TemplateIsWorse(worstStatus, templateStatus.Status) {
				worstStatus = templateStatus.Status
			}
		}
	}
	switch worstStatus {
	case v1alpha1.TemplateStatusProgressing:
		return v1alpha1.AnalysisStatusPending
	case v1alpha1.TemplateStatusFailed:
		return v1alpha1.AnalysisStatusFailed
	case v1alpha1.TemplateStatusError:
		return v1alpha1.AnalysisStatusError
	}
	// Successful and Running template status codes are both considered to be running
	return v1alpha1.AnalysisStatusRunning
}

// assessTemplates looks at all the template statuses, and gives an assessment to be used for the
// experiment status.
func (ec *experimentContext) assessAnalysisRuns() v1alpha1.AnalysisStatus {
	if len(ec.ex.Spec.Analyses) == 0 {
		return v1alpha1.AnalysisStatusSuccessful
	}
	worstStatus := v1alpha1.AnalysisStatusSuccessful
	allCompleted := true
	for _, a := range ec.ex.Spec.Analyses {
		as := experimentutil.GetAnalysisRunStatus(*ec.newStatus, a.Name)
		if as.Status.Completed() {
			if analysisutil.IsWorse(worstStatus, as.Status) {
				worstStatus = as.Status
			}
		} else {
			allCompleted = false
		}
	}
	if !allCompleted {
		return v1alpha1.AnalysisStatusRunning
	}
	return worstStatus
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
