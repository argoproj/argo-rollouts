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

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	rolloutslisters "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	experimentutil "github.com/argoproj/argo-rollouts/utils/experiment"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	templateutil "github.com/argoproj/argo-rollouts/utils/template"
)

const (
	requiredAnalysisCompletedMessage = "Required AnalysisRuns completed"
)

type experimentContext struct {
	// parameters supplied to the context
	ex                            *v1alpha1.Experiment
	templateRSs                   map[string]*appsv1.ReplicaSet
	kubeclientset                 kubernetes.Interface
	argoProjClientset             clientset.Interface
	analysisTemplateLister        rolloutslisters.AnalysisTemplateLister
	clusterAnalysisTemplateLister rolloutslisters.ClusterAnalysisTemplateLister
	analysisRunLister             rolloutslisters.AnalysisRunLister
	replicaSetLister              appslisters.ReplicaSetLister
	recorder                      record.EventRecorder
	enqueueExperimentAfter        func(obj interface{}, duration time.Duration)

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
	clusterAnalysisTemplateLister rolloutslisters.ClusterAnalysisTemplateLister,
	analysisRunLister rolloutslisters.AnalysisRunLister,
	recorder record.EventRecorder,
	enqueueExperimentAfter func(obj interface{}, duration time.Duration),
) *experimentContext {

	exCtx := experimentContext{
		ex:                            experiment,
		templateRSs:                   templateRSs,
		kubeclientset:                 kubeclientset,
		argoProjClientset:             argoProjClientset,
		replicaSetLister:              replicaSetLister,
		analysisTemplateLister:        analysisTemplateLister,
		clusterAnalysisTemplateLister: clusterAnalysisTemplateLister,
		analysisRunLister:             analysisRunLister,
		recorder:                      recorder,
		enqueueExperimentAfter:        enqueueExperimentAfter,

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

	newStatus := ec.calculateStatus()
	if duration := calculateEnqueueDuration(ec.ex, newStatus); duration != nil {
		ec.log.Infof("Enqueueing Experiment in %s seconds", duration.String())
		ec.enqueueExperimentAfter(ec.ex, *duration)
	}
	return newStatus
}

// reconcileTemplate reconciles a template to a ReplicaSet. Creates or scales them down as necessary
// will update status.templateStatuses with the current assessed values
func (ec *experimentContext) reconcileTemplate(template v1alpha1.TemplateSpec) {
	logCtx := ec.log.WithField("template", template.Name)
	logCtx.Info("Reconciling template")
	templateStatus := experimentutil.GetTemplateStatus(ec.ex.Status, template.Name)
	if templateStatus == nil {
		templateStatus = &v1alpha1.TemplateStatus{
			Name: template.Name,
		}
	}
	prevStatus := templateStatus.DeepCopy()
	desiredReplicaCount := experimentutil.CalculateTemplateReplicasCount(ec.ex, template)
	now := metav1.Now()

	rs := ec.templateRSs[template.Name]
	if rs == nil {
		// Create the ReplicaSet if necessary
		if desiredReplicaCount > 0 {
			newRS, err := ec.createReplicaSet(template, templateStatus.CollisionCount)
			if err != nil {
				logCtx.Warnf("Failed to create ReplicaSet: %v", err)
				if !k8serrors.IsAlreadyExists(err) {
					templateStatus.Status = v1alpha1.TemplateStatusError
					templateStatus.Message = fmt.Sprintf("Failed to create ReplicaSet for template '%s': %v", template.Name, err)
				}
			}
			if newRS != nil {
				ec.templateRSs[template.Name] = newRS
				templateStatus.LastTransitionTime = &now
				rs = newRS
			}
		}
	} else {
		// Replicaset exists. We ensure it is scaled properly based on termination, or changed replica count
		if *rs.Spec.Replicas != desiredReplicaCount {
			ec.scaleReplicaSetAndRecordEvent(rs, desiredReplicaCount)
			templateStatus.LastTransitionTime = &now
		}
	}

	if rs == nil {
		templateStatus.Replicas = 0
		templateStatus.UpdatedReplicas = 0
		templateStatus.ReadyReplicas = 0
		templateStatus.AvailableReplicas = 0
	} else {
		templateStatus.Replicas = replicasetutil.GetActualReplicaCountForReplicaSets([]*appsv1.ReplicaSet{rs})
		templateStatus.UpdatedReplicas = replicasetutil.GetActualReplicaCountForReplicaSets([]*appsv1.ReplicaSet{rs})
		templateStatus.ReadyReplicas = replicasetutil.GetReadyReplicaCountForReplicaSets([]*appsv1.ReplicaSet{rs})
		templateStatus.AvailableReplicas = replicasetutil.GetAvailableReplicaCountForReplicaSets([]*appsv1.ReplicaSet{rs})
	}

	if prevStatus.Replicas != templateStatus.Replicas ||
		prevStatus.UpdatedReplicas != templateStatus.UpdatedReplicas ||
		prevStatus.ReadyReplicas != templateStatus.ReadyReplicas ||
		prevStatus.AvailableReplicas != templateStatus.AvailableReplicas {

		logCtx.Info("Template progressed")
		logCtx.Infof("Prev status: Current: %d, Updated: %d, Ready: %d, Available: %d",
			prevStatus.Replicas, prevStatus.UpdatedReplicas, prevStatus.ReadyReplicas, prevStatus.AvailableReplicas)
		logCtx.Infof("New status: Current: %d, Updated: %d, Ready: %d, Available: %d",
			templateStatus.Replicas, templateStatus.UpdatedReplicas, templateStatus.ReadyReplicas, templateStatus.AvailableReplicas)
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
		eventType := corev1.EventTypeNormal
		switch templateStatus.Status {
		case v1alpha1.TemplateStatusFailed, v1alpha1.TemplateStatusError:
			eventType = corev1.EventTypeWarning
		}
		ec.recorder.Eventf(ec.ex, record.EventOptions{EventType: eventType, EventReason: "Template" + string(templateStatus.Status)}, msg)
	}
	experimentutil.SetTemplateStatus(ec.newStatus, *templateStatus)
}

// calculateEnqueueDuration returns an appropriate duration to requeue the experiment. This will be
// the shortest of:
// * status.availableAt + spec.duration
// * status.templateStatuses[].lastTransitionTime + spec.progressDeadlineSeconds
// Returns nil if there is no need to requeue
func calculateEnqueueDuration(ex *v1alpha1.Experiment, newStatus *v1alpha1.ExperimentStatus) *time.Duration {
	ex = ex.DeepCopy()
	ex.Status = *(newStatus.DeepCopy())
	if experimentutil.IsTerminating(ex) {
		return nil
	}
	var candidateDuration *time.Duration
	if ex.Status.AvailableAt != nil && ex.Spec.Duration != "" {
		// Set candidate duration to status.availableAt + duration
		passedDuration, timeRemaining := experimentutil.PassedDurations(ex)
		if !passedDuration {
			candidateDuration = &timeRemaining
		}
	}
	deadlineSeconds := defaults.GetExperimentProgressDeadlineSecondsOrDefault(ex)
	now := time.Now()
	for _, template := range ex.Spec.Templates {
		// Set candidate to the earliest of LastTransitionTime + progressDeadlineSeconds
		ts := experimentutil.GetTemplateStatus(ex.Status, template.Name)
		if ts == nil || (ts.Status != v1alpha1.TemplateStatusProgressing && ts.Status != v1alpha1.TemplateStatusRunning) {
			continue
		}
		desiredReplicaCount := experimentutil.CalculateTemplateReplicasCount(ex, template)
		// only requeue if we are not meeting our desired replicas, since if we are at our desired
		// replicas, then theres nothing to check on
		if ts.AvailableReplicas != desiredReplicaCount && ts.LastTransitionTime != nil {
			progressDeadlineDuration := ts.LastTransitionTime.Add(time.Second * time.Duration(deadlineSeconds)).Sub(now)
			if candidateDuration == nil || progressDeadlineDuration < *candidateDuration {
				candidateDuration = &progressDeadlineDuration
			}
		}
	}
	return candidateDuration
}

// reconcileAnalysisRun reconciles a single analysis run, creating or terminating it as necessary.
// Updates the analysis run statuses, which may subsequently fail the experiment.
func (ec *experimentContext) reconcileAnalysisRun(analysis v1alpha1.ExperimentAnalysisTemplateRef) {
	logCtx := ec.log.WithField("analysis", analysis.Name)
	logCtx.Infof("Reconciling analysis")
	prevStatus := experimentutil.GetAnalysisRunStatus(ec.ex.Status, analysis.Name)
	if prevStatus == nil {
		prevStatus = &v1alpha1.ExperimentAnalysisRunStatus{
			Name: analysis.Name,
		}
	}
	newStatus := prevStatus.DeepCopy()

	// setAnalysisRunStatus is a convenience method to:
	// 1. update the runStatus
	// 2. log a message and emit an event on status changes
	defer func() {
		if prevStatus.Phase != newStatus.Phase && newStatus.Phase != "" {
			msg := fmt.Sprintf("AnalysisRun '%s' transitioned from %s -> %s", analysis.Name, prevStatus.Phase, newStatus.Phase)
			if newStatus.Message != "" {
				msg = msg + ": " + newStatus.Message
			}
			eventType := corev1.EventTypeNormal
			switch newStatus.Phase {
			case v1alpha1.AnalysisPhaseFailed, v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseInconclusive:
				eventType = corev1.EventTypeWarning
			}
			ec.recorder.Eventf(ec.ex, record.EventOptions{EventType: eventType, EventReason: "AnalysisRun" + string(newStatus.Phase)}, msg)
		}
		experimentutil.SetAnalysisRunStatus(ec.newStatus, *newStatus)
	}()

	if ec.ex.Status.AvailableAt == nil {
		// If we are not not available yet, don't start any runs
		if analysis.ClusterScope {
			if err := ec.verifyClusterAnalysisTemplate(analysis); err != nil {
				msg := fmt.Sprintf("ClusterAnalysisTemplate verification failed for analysis '%s': %v", analysis.Name, err.Error())
				newStatus.Phase = v1alpha1.AnalysisPhaseError
				newStatus.Message = msg
				logCtx.Warn(msg)
			}
		} else {
			if err := ec.verifyAnalysisTemplate(analysis); err != nil {
				msg := fmt.Sprintf("AnalysisTemplate verification failed for analysis '%s': %v", analysis.Name, err.Error())
				newStatus.Phase = v1alpha1.AnalysisPhaseError
				newStatus.Message = msg
				logCtx.Warn(msg)
			}
		}
		return
	}

	if prevStatus.AnalysisRun == "" {
		// AnalysisRun needs to be created (unless we are terminating)
		if ec.isTerminating {
			logCtx.Warnf("Skipping AnalysisRun creation for analysis %s: experiment is terminating", analysis.Name)
			return
		}
		run, err := ec.createAnalysisRun(analysis)
		if err != nil {
			msg := fmt.Sprintf("Failed to create AnalysisRun for analysis '%s': %v", analysis.Name, err.Error())
			newStatus.Phase = v1alpha1.AnalysisPhaseError
			newStatus.Message = msg
			logCtx.Warn(msg)
		} else {
			newStatus.Phase = v1alpha1.AnalysisPhasePending
			newStatus.AnalysisRun = run.Name
			logCtx.Infof("Created %s", run.Name)
		}
		return
	}

	// If we get here, analysis run has been previously created and we are just checking its status
	if prevStatus.Phase.Completed() {
		// runStatus is already completed. nothing to do
		return
	}

	run, err := ec.analysisRunLister.AnalysisRuns(ec.ex.Namespace).Get(prevStatus.AnalysisRun)
	if err != nil {
		newStatus.Phase = v1alpha1.AnalysisPhaseError
		newStatus.Message = err.Error()
		return
	}

	if ec.isTerminating {
		if !run.Status.Phase.Completed() && !run.Spec.Terminate {
			ec.recorder.Eventf(ec.ex, record.EventOptions{EventReason: "AnalysisRunTerminating"}, "Terminating %s (%s)", analysis.Name, run.Name)
			analysisRunIf := ec.argoProjClientset.ArgoprojV1alpha1().AnalysisRuns(ec.ex.Namespace)
			err := analysisutil.TerminateRun(analysisRunIf, run.Name)
			if err != nil {
				newStatus.Phase = v1alpha1.AnalysisPhaseError
				newStatus.Message = err.Error()
			}
		}
		return
	}

	newStatus.Phase = run.Status.Phase
	newStatus.Message = run.Status.Message
}

// createAnalysisRun creates the analysis run. If an existing runs exists with same name, is
// semantically equal, and is not complete, returns the existing one, otherwise creates a new
// run with a collision counter increase.
func (ec *experimentContext) createAnalysisRun(analysis v1alpha1.ExperimentAnalysisTemplateRef) (*v1alpha1.AnalysisRun, error) {
	analysisRunIf := ec.argoProjClientset.ArgoprojV1alpha1().AnalysisRuns(ec.ex.Namespace)
	args, err := ec.ResolveAnalysisRunArgs(analysis.Args)
	if err != nil {
		return nil, err
	}
	run, err := ec.newAnalysisRun(analysis, args)
	if err != nil {
		return nil, err
	}
	return analysisutil.CreateWithCollisionCounter(ec.log, analysisRunIf, *run)
}

func (ec *experimentContext) ResolveAnalysisRunArgs(args []v1alpha1.Argument) ([]v1alpha1.Argument, error) {
	resolvedArgs := []v1alpha1.Argument{}
	for _, arg := range args {
		resolvedArg := v1alpha1.Argument{Name: arg.Name}
		if arg.Value != nil {
			value, err := templateutil.ResolveExperimentArgsValue(*arg.Value, ec.ex, ec.templateRSs)
			if err != nil {
				return nil, err
			}
			resolvedArg.Value = &value
		}
		resolvedArgs = append(resolvedArgs, resolvedArg)
	}
	return resolvedArgs, nil
}

func (ec *experimentContext) calculateStatus() *v1alpha1.ExperimentStatus {
	prevStatus := ec.newStatus.DeepCopy()
	switch ec.newStatus.Phase {
	case "":
		ec.newStatus.Phase = v1alpha1.AnalysisPhasePending
	case v1alpha1.AnalysisPhasePending, v1alpha1.AnalysisPhaseRunning:
		templateStatus, templateMessage := ec.assessTemplates()
		analysesStatus, analysesMessage := ec.assessAnalysisRuns()
		if templateStatus == v1alpha1.AnalysisPhaseRunning && ec.newStatus.AvailableAt == nil {
			now := metav1.Now()
			ec.newStatus.AvailableAt = &now
			ec.log.Infof("Marked AvailableAt: %v", now)
		}
		if templateStatus.Completed() {
			analysisRun := analysesStatus.Completed() || experimentutil.HasRequiredAnalysisRuns(ec.ex)
			if templateStatus == v1alpha1.AnalysisPhaseSuccessful && analysisRun {
				// If the templates have completed successfully (e.g. it ran without degrading for
				// the entire duration), and there are required analysis runs or they have completed, then
				// the status of the Experiment is deferred to the status of the analyses results.
				ec.newStatus.Phase = analysesStatus
				ec.newStatus.Message = analysesMessage
			} else {
				// Otherwise, use the Failed/Error template status as the Experiment status
				ec.newStatus.Phase = templateStatus
				ec.newStatus.Message = templateMessage
			}
		} else {
			if analysesStatus.Completed() && analysesStatus != v1alpha1.AnalysisPhaseSuccessful {
				// The templates are still Running, but analysis failed, errored or was inconclusive
				// We will now fail the experiment.
				ec.newStatus.Phase = analysesStatus
				ec.newStatus.Message = analysesMessage
			} else if experimentutil.RequiredAnalysisRunsSuccessful(ec.ex, ec.newStatus) {
				// All the required analysis runs have completed successfully so we can conclude the experiment
				// successfully.
				ec.newStatus.Phase = analysesStatus
				ec.newStatus.Message = analysesMessage
			} else {
				// The templates are still Running/Progressing, and the analysis are either still
				// Running/Pending/Successful.
				ec.newStatus.Phase = templateStatus
				ec.newStatus.Message = templateMessage
			}
		}
	}
	ec.newStatus = calculateExperimentConditions(ec.ex, *ec.newStatus)
	if prevStatus.Phase != ec.newStatus.Phase {
		eventType := corev1.EventTypeNormal
		switch ec.newStatus.Phase {
		case v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseFailed, v1alpha1.AnalysisPhaseInconclusive:
			eventType = corev1.EventTypeWarning
		}
		ec.recorder.Eventf(ec.ex, record.EventOptions{EventType: eventType, EventReason: "Experiment" + string(ec.newStatus.Phase)}, "Experiment transitioned from %s -> %s", prevStatus.Phase, ec.newStatus.Phase)
	}
	return ec.newStatus
}

// assessTemplates examines at all the template statuses, and returns the worst of them to be
// considered as the experiment status, along with the message
func (ec *experimentContext) assessTemplates() (v1alpha1.AnalysisPhase, string) {
	worstStatus := v1alpha1.TemplateStatusSuccessful
	message := ""
	for _, template := range ec.ex.Spec.Templates {
		templateStatus := experimentutil.GetTemplateStatus(*ec.newStatus, template.Name)
		if templateStatus == nil {
			worstStatus = experimentutil.Worst(worstStatus, v1alpha1.TemplateStatusProgressing)
		} else {
			if experimentutil.TemplateIsWorse(worstStatus, templateStatus.Status) {
				worstStatus = templateStatus.Status
				message = templateStatus.Message
			}
		}
	}
	switch worstStatus {
	case v1alpha1.TemplateStatusProgressing:
		return v1alpha1.AnalysisPhasePending, message
	case v1alpha1.TemplateStatusFailed:
		return v1alpha1.AnalysisPhaseFailed, message
	case v1alpha1.TemplateStatusError:
		return v1alpha1.AnalysisPhaseError, message
	case v1alpha1.TemplateStatusSuccessful:
		return v1alpha1.AnalysisPhaseSuccessful, message
	}
	return v1alpha1.AnalysisPhaseRunning, message
}

// assessTemplates examines all the analysisrun statuses, and returns the worst of the statuses.
// This status will be under consideration as the experiment status (dependant on other factors).
// Any Failed, Error, Inconclusive runs will cause the Experiment to complete prematurely. If there
// are no analyses, will return AnalysisPhaseSuccessful.
func (ec *experimentContext) assessAnalysisRuns() (v1alpha1.AnalysisPhase, string) {
	worstStatus := v1alpha1.AnalysisPhaseSuccessful
	message := ""

	for _, a := range ec.ex.Spec.Analyses {
		as := experimentutil.GetAnalysisRunStatus(*ec.newStatus, a.Name)
		if analysisutil.IsWorse(worstStatus, as.Phase) {
			worstStatus = as.Phase
			message = as.Message
		}
	}

	if experimentutil.RequiredAnalysisRunsSuccessful(ec.ex, ec.newStatus) && analysisutil.IsWorse(worstStatus, v1alpha1.AnalysisPhaseRunning) {
		return v1alpha1.AnalysisPhaseSuccessful, requiredAnalysisCompletedMessage
	}
	if worstStatus == v1alpha1.AnalysisPhasePending {
		// since this will be used as experiment status, we should return Running instead of Pending
		worstStatus = v1alpha1.AnalysisPhaseRunning
	}
	return worstStatus, message
}

// newAnalysisRun generates an AnalysisRun from the experiment and template
func (ec *experimentContext) newAnalysisRun(analysis v1alpha1.ExperimentAnalysisTemplateRef, args []v1alpha1.Argument) (*v1alpha1.AnalysisRun, error) {

	if analysis.ClusterScope {
		clusterTemplate, err := ec.clusterAnalysisTemplateLister.Get(analysis.TemplateName)
		if err != nil {
			return nil, err
		}
		name := fmt.Sprintf("%s-%s", ec.ex.Name, analysis.Name)

		run, err := analysisutil.NewAnalysisRunFromClusterTemplate(clusterTemplate, args, name, "", ec.ex.Namespace)
		if err != nil {
			return nil, err
		}
		instanceID := analysisutil.GetInstanceID(ec.ex)
		if instanceID != "" {
			run.Labels = map[string]string{v1alpha1.LabelKeyControllerInstanceID: ec.ex.Labels[v1alpha1.LabelKeyControllerInstanceID]}
		}
		run.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(ec.ex, controllerKind)}
		return run, nil
	} else {
		template, err := ec.analysisTemplateLister.AnalysisTemplates(ec.ex.Namespace).Get(analysis.TemplateName)
		if err != nil {
			return nil, err
		}
		name := fmt.Sprintf("%s-%s", ec.ex.Name, analysis.Name)

		run, err := analysisutil.NewAnalysisRunFromTemplate(template, args, name, "", ec.ex.Namespace)
		if err != nil {
			return nil, err
		}
		instanceID := analysisutil.GetInstanceID(ec.ex)
		if instanceID != "" {
			run.Labels = map[string]string{v1alpha1.LabelKeyControllerInstanceID: ec.ex.Labels[v1alpha1.LabelKeyControllerInstanceID]}
		}
		run.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(ec.ex, controllerKind)}
		return run, nil
	}
}

// verifyAnalysisTemplate verifies an AnalysisTemplate. For now, it simply means that it exists
func (ec *experimentContext) verifyAnalysisTemplate(analysis v1alpha1.ExperimentAnalysisTemplateRef) error {
	_, err := ec.analysisTemplateLister.AnalysisTemplates(ec.ex.Namespace).Get(analysis.TemplateName)
	return err
}

// verifyAnalysisTemplate verifies a ClusterAnalysisTemplate. For now, it simply means that it exists
func (ec *experimentContext) verifyClusterAnalysisTemplate(analysis v1alpha1.ExperimentAnalysisTemplateRef) error {
	_, err := ec.clusterAnalysisTemplateLister.Get(analysis.TemplateName)
	return err
}
