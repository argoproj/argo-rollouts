package rolloutplugin

import (
	"context"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	plugintypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
)

// RolloutPluginReconciler reconciles a RolloutPlugin object
type RolloutPluginReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	KubeClientset     kubernetes.Interface
	ArgoProjClientset clientset.Interface
	DynamicClientset  dynamic.Interface
	PluginManager     PluginManager
	AnalysisHelper    AnalysisHelper
}

// AnalysisHelper is an interface for managing AnalysisRuns
type AnalysisHelper interface {
	// GetAnalysisRunsForOwner returns all AnalysisRuns owned by the specified resource
	GetAnalysisRunsForOwner(ctx context.Context, ownerName string, namespace string, ownerUID types.UID, statusRefs []v1alpha1.RolloutAnalysisRunStatus) ([]*v1alpha1.AnalysisRun, error)

	// CreateAnalysisRun creates a new AnalysisRun
	CreateAnalysisRun(ctx context.Context, rolloutAnalysis *v1alpha1.RolloutAnalysis, args []v1alpha1.Argument, namespace string, podHash string, infix string, labels map[string]string, annotations map[string]string, ownerRef metav1.OwnerReference) (*v1alpha1.AnalysisRun, error)

	// CancelAnalysisRuns cancels the specified AnalysisRuns
	CancelAnalysisRuns(ctx context.Context, analysisRuns []*v1alpha1.AnalysisRun) error

	// DeleteAnalysisRuns deletes AnalysisRuns based on label selector and history limit
	DeleteAnalysisRuns(ctx context.Context, namespace string, selector labels.Selector, limit int) error
}

// PluginManager is an interface for managing plugins
type PluginManager interface {
	// GetPlugin returns a plugin by name
	GetPlugin(name string) (ResourcePlugin, error)
	// LoadPlugin loads a plugin from the given configuration
	LoadPlugin(config v1alpha1.PluginConfig) error
}

// ResourcePlugin is the interface that all resource plugins must implement.
// Built-in plugins implement this interface directly.
// External RPC plugins implement types.RpcResourcePlugin, which is adapted via RpcPluginWrapper.
type ResourcePlugin interface {
	// Init initializes the plugin
	Init() error

	// GetResourceStatus gets the current status of the referenced workload
	GetResourceStatus(ctx context.Context, workloadRef v1alpha1.WorkloadRef) (*ResourceStatus, error)

	// SetWeight updates the weight (percentage of pods updated)
	SetWeight(ctx context.Context, workloadRef v1alpha1.WorkloadRef, weight int32) error

	// VerifyWeight checks if the desired weight has been achieved
	VerifyWeight(ctx context.Context, workloadRef v1alpha1.WorkloadRef, weight int32) (bool, error)

	// Promote promotes the new version to stable
	Promote(ctx context.Context, workloadRef v1alpha1.WorkloadRef) error

	// Abort aborts the rollout and reverts to the stable version
	Abort(ctx context.Context, workloadRef v1alpha1.WorkloadRef) error

	// Reset returns the workload to baseline state for retry
	Reset(ctx context.Context, workloadRef v1alpha1.WorkloadRef) error
}

// ResourceStatus is an alias for the shared type to avoid import changes in existing code.
// The actual struct is defined in utils/plugin/types to be shared with RPC plugins.
type ResourceStatus = plugintypes.ResourceStatus

//+kubebuilder:rbac:groups=argoproj.io,resources=rolloutplugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=argoproj.io,resources=rolloutplugins/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=argoproj.io,resources=rolloutplugins/finalizers,verbs=update
//+kubebuilder:rbac:groups=argoproj.io,resources=analysisruns,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=argoproj.io,resources=analysistemplates,verbs=get;list;watch
//+kubebuilder:rbac:groups=argoproj.io,resources=clusteranalysistemplates,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *RolloutPluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logCtx := log.WithFields(log.Fields{"namespace": req.Namespace, "rolloutplugin": req.Name})
	logCtx.Info("Reconciling RolloutPlugin")

	// Fetch the RolloutPlugin instance
	rolloutPlugin := &v1alpha1.RolloutPlugin{}
	if err := r.Get(ctx, req.NamespacedName, rolloutPlugin); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle deletion
	if !rolloutPlugin.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, rolloutPlugin, logCtx)
	}

	// Reconcile the RolloutPlugin
	return r.reconcile(ctx, rolloutPlugin, logCtx)
}

// handleDeletion handles the deletion of a RolloutPlugin
func (r *RolloutPluginReconciler) handleDeletion(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, logCtx *log.Entry) (ctrl.Result, error) {
	logCtx.Info("Handling deletion of RolloutPlugin")
	// Perform any cleanup if needed
	return ctrl.Result{}, nil
}

// reconcile performs the main reconciliation logic
func (r *RolloutPluginReconciler) reconcile(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, logCtx *log.Entry) (ctrl.Result, error) {
	logCtx.Info("Reconciling RolloutPlugin")

	newStatus := rolloutPlugin.Status.DeepCopy()
	newStatus.ObservedGeneration = rolloutPlugin.Generation

	// Validate the RolloutPlugin spec
	prevInvalidSpecCond := conditions.GetRolloutPluginCondition(rolloutPlugin.Status, conditions.RolloutPluginInvalidSpec)
	invalidSpecCond := conditions.VerifyRolloutPluginSpec(rolloutPlugin, prevInvalidSpecCond)
	if invalidSpecCond != nil {
		logCtx.WithField("reason", invalidSpecCond.Message).Warn("RolloutPlugin spec validation failed")
		newStatus.Phase = "Failed"
		newStatus.Message = invalidSpecCond.Message
		conditions.SetRolloutPluginCondition(newStatus, *invalidSpecCond)
		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	// Remove InvalidSpec condition if spec is now valid
	if prevInvalidSpecCond != nil {
		logCtx.Info("RolloutPlugin spec is now valid, removing InvalidSpec condition")
		conditions.RemoveRolloutPluginCondition(newStatus, conditions.RolloutPluginInvalidSpec)
	}

	// Get the plugin early - we'll need it for multiple operations
	plugin, err := r.PluginManager.GetPlugin(rolloutPlugin.Spec.Plugin.Name)
	if err != nil {
		logCtx.WithError(err).Error("Failed to get plugin")
		newStatus.Phase = "Failed"
		newStatus.Message = fmt.Sprintf("Plugin '%s' not found. Ensure the plugin is registered in the argo-rollouts-config ConfigMap under 'rolloutPlugins'", rolloutPlugin.Spec.Plugin.Name)
		// Set InvalidSpec condition for plugin not found
		pluginNotFoundCond := conditions.NewRolloutPluginCondition(
			conditions.RolloutPluginInvalidSpec,
			corev1.ConditionTrue,
			conditions.RolloutPluginInvalidSpecReason,
			newStatus.Message,
		)
		conditions.SetRolloutPluginCondition(newStatus, *pluginNotFoundCond)
		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	// Initialize plugin if not already initialized
	if !newStatus.Initialized {
		if err := plugin.Init(); err != nil {
			logCtx.WithError(err).Error("Failed to initialize plugin")
			newStatus.Phase = "Failed"
			newStatus.Message = fmt.Sprintf("Failed to initialize plugin: %v", err)
			return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
		}
		newStatus.Initialized = true
		logCtx.Info("Plugin initialized successfully")
	}

	// Prepare workload reference with namespace defaulting (used by multiple operations below)
	workloadRef := rolloutPlugin.Spec.WorkloadRef
	if workloadRef.Namespace == "" {
		workloadRef.Namespace = rolloutPlugin.Namespace
	}

	// Check if retry is requested via spec.RestartAt
	if rolloutPlugin.Spec.RestartAt != nil {
		return r.processRetry(ctx, rolloutPlugin, newStatus, plugin, workloadRef, logCtx)
	}

	// Check if spec.paused is set (manual pause by user)
	if rolloutPlugin.Spec.Paused {
		if !newStatus.Paused {
			logCtx.Info("Rollout manually paused by user")
			now := metav1.Now()
			newStatus.PauseStartTime = &now
			newStatus.Paused = true
			newStatus.Phase = "Paused"
			newStatus.Message = "manually paused"
		}
		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	// If spec.paused was false but status.paused is true, user is manually resuming
	// Only clear pause state if it was a manual pause (not a step pause)
	// Step pauses are managed within processCanaryRollout logic
	if newStatus.Paused && !rolloutPlugin.Spec.Paused && newStatus.CurrentStepIndex == nil {
		// This was a manual pause that's being resumed
		logCtx.Info("Resuming RolloutPlugin from manual pause")
		newStatus.Paused = false
		newStatus.PauseStartTime = nil
	}

	// Check if manual abort is requested via status.Abort field
	if newStatus.Abort && !newStatus.Aborted {
		logCtx.Info("Manual abort requested via status.Abort field")

		// Call plugin abort
		if abortErr := plugin.Abort(ctx, workloadRef); abortErr != nil {
			logCtx.WithError(abortErr).Error("Failed to abort rollout")
			newStatus.Phase = "Failed"
			newStatus.Message = fmt.Sprintf("Failed to abort rollout: %v", abortErr)
			return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
		}

		newStatus.Aborted = true
		newStatus.Abort = false // Clear the abort flag
		newStatus.RolloutInProgress = false
		newStatus.Phase = "Degraded"
		newStatus.Message = "Rollout aborted by user"

		// Set aborted condition
		condition := conditions.NewRolloutPluginCondition(
			conditions.RolloutPluginProgressing,
			corev1.ConditionFalse,
			conditions.RolloutPluginAbortedReason,
			"Rollout manually aborted by user")
		conditions.SetRolloutPluginCondition(newStatus, *condition)

		logCtx.Info("Rollout aborted successfully")
		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	// Check progress deadline timeout
	if conditions.RolloutPluginTimedOut(rolloutPlugin, newStatus) {
		logCtx.Info("RolloutPlugin has timed out")

		// If progressDeadlineAbort is enabled and not already aborted, abort the rollout
		if rolloutPlugin.Spec.ProgressDeadlineAbort && !newStatus.Aborted {
			logCtx.Info("Aborting RolloutPlugin due to timeout")
			if abortErr := plugin.Abort(ctx, workloadRef); abortErr != nil {
				logCtx.WithError(abortErr).Error("Failed to abort rollout due to timeout")
			}
			newStatus.Aborted = true
			newStatus.Phase = "Degraded"
		}

		// Set timeout condition
		condition := conditions.NewRolloutPluginCondition(
			conditions.RolloutPluginProgressing,
			corev1.ConditionFalse,
			conditions.RolloutPluginTimedOutReason,
			fmt.Sprintf("RolloutPlugin %s has timed out progressing after %d seconds",
				rolloutPlugin.Name,
				defaults.GetRolloutPluginProgressDeadlineSecondsOrDefault(rolloutPlugin)))
		conditions.SetRolloutPluginCondition(newStatus, *condition)

		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	// Get the current status of the referenced workload
	resourceStatus, err := plugin.GetResourceStatus(ctx, workloadRef)
	if err != nil {
		logCtx.WithError(err).Error("Failed to get resource status")
		newStatus.Phase = "Failed"
		newStatus.Message = fmt.Sprintf("Failed to get resource status: %v", err)
		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	// Update replica counts
	// Save the old UpdatedRevision before updating it, to detect if a new rollout started
	oldUpdatedRevision := newStatus.UpdatedRevision

	newStatus.Replicas = resourceStatus.Replicas
	newStatus.UpdatedReplicas = resourceStatus.UpdatedReplicas
	newStatus.ReadyReplicas = resourceStatus.ReadyReplicas
	newStatus.AvailableReplicas = resourceStatus.AvailableReplicas
	newStatus.CurrentRevision = resourceStatus.CurrentRevision
	newStatus.UpdatedRevision = resourceStatus.UpdatedRevision

	// Check minReadySeconds - ensure pods have been ready for the required duration
	if rolloutPlugin.Spec.MinReadySeconds > 0 {
		if !meetsMinReadySeconds(resourceStatus, rolloutPlugin.Spec.MinReadySeconds) {
			logCtx.WithFields(log.Fields{
				"minReadySeconds":   rolloutPlugin.Spec.MinReadySeconds,
				"readyReplicas":     resourceStatus.ReadyReplicas,
				"availableReplicas": resourceStatus.AvailableReplicas,
			}).Info("Waiting for pods to meet minReadySeconds")
			newStatus.Message = fmt.Sprintf("Waiting for pods to meet minReadySeconds (%d seconds)", rolloutPlugin.Spec.MinReadySeconds)
			return ctrl.Result{RequeueAfter: 5 * time.Second}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
		}
	}

	// Check if we need to start a rollout
	if resourceStatus.CurrentRevision != resourceStatus.UpdatedRevision {
		if !newStatus.RolloutInProgress {
			logCtx.Info("Starting new rollout")

			// Reset retry counter for new rollout (new UpdatedRevision)
			newStatus.RetryAttempt = 0
			newStatus.RestartedAt = nil

			newStatus.RolloutInProgress = true
			newStatus.CurrentStepIndex = nil
			newStatus.Phase = "Progressing"

			// Set progressing condition
			condition := conditions.NewRolloutPluginCondition(
				conditions.RolloutPluginProgressing,
				corev1.ConditionTrue,
				conditions.RolloutPluginProgressingReason,
				"RolloutPlugin is progressing")
			conditions.SetRolloutPluginCondition(newStatus, *condition)
		} else {
			// Rollout already in progress - check if this is a NEW rollout (UpdatedRevision changed)
			if oldUpdatedRevision != "" && oldUpdatedRevision != resourceStatus.UpdatedRevision {
				logCtx.WithFields(log.Fields{
					"previousUpdatedRevision": oldUpdatedRevision,
					"newUpdatedRevision":      resourceStatus.UpdatedRevision,
				}).Info("Detected new rollout while previous rollout in progress")

				// Reset retry counter for new rollout
				newStatus.RetryAttempt = 0
				newStatus.RestartedAt = nil
				newStatus.CurrentStepIndex = nil
				newStatus.Phase = "Progressing"

				logCtx.Info("Reset retry counter for new rollout")
			}
		}
	}

	// Process rollout steps if in progress
	if newStatus.RolloutInProgress {
		result, err := r.processRollout(ctx, rolloutPlugin, newStatus, plugin, workloadRef, logCtx)
		if err != nil {
			logCtx.WithError(err).Error("Failed to process rollout")
			return result, err
		}
		if err := r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx); err != nil {
			return ctrl.Result{}, err
		}
		return result, nil
	}

	// Only update phase/message if not already healthy to avoid unnecessary reconciliation
	if newStatus.Phase != "Healthy" {
		newStatus.Phase = "Healthy"
		newStatus.Message = "RolloutPlugin is healthy"
	}

	// Set healthy condition only if not already set
	if conditions.RolloutPluginIsHealthy(rolloutPlugin, newStatus) {
		existingHealthyCond := conditions.GetRolloutPluginCondition(*newStatus, conditions.RolloutPluginHealthy)
		if existingHealthyCond == nil || existingHealthyCond.Status != corev1.ConditionTrue {
			condition := conditions.NewRolloutPluginCondition(
				conditions.RolloutPluginHealthy,
				corev1.ConditionTrue,
				conditions.RolloutPluginHealthyReason,
				"RolloutPlugin is healthy")
			conditions.SetRolloutPluginCondition(newStatus, *condition)
		}
	}

	return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
}

// processRollout processes the rollout steps based on strategy
func (r *RolloutPluginReconciler) processRollout(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, plugin ResourcePlugin, workloadRef v1alpha1.WorkloadRef, logCtx *log.Entry) (ctrl.Result, error) {
	strategy := rolloutPlugin.Spec.Strategy
	if strategy.Canary != nil {
		return r.processCanaryRollout(ctx, rolloutPlugin, newStatus, plugin, workloadRef, logCtx)
	}

	logCtx.Info("No strategy defined")
	newStatus.Phase = "Failed"
	newStatus.Message = "No strategy defined"
	return ctrl.Result{}, nil
}

// processCanaryRollout processes a canary rollout
func (r *RolloutPluginReconciler) processCanaryRollout(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, plugin ResourcePlugin, workloadRef v1alpha1.WorkloadRef, logCtx *log.Entry) (ctrl.Result, error) {
	canary := rolloutPlugin.Spec.Strategy.Canary
	if canary == nil || len(canary.Steps) == 0 {
		logCtx.Info("No canary steps defined")
		newStatus.Phase = "Successful"
		newStatus.Message = "No canary steps to execute"
		newStatus.RolloutInProgress = false
		return ctrl.Result{}, nil
	}

	// Check if PromoteFull is set - if so, skip all steps and promote immediately
	if newStatus.PromoteFull {
		logCtx.Info("PromoteFull is set, skipping remaining steps and promoting immediately")

		// Promote the rollout
		if err := plugin.Promote(ctx, workloadRef); err != nil {
			logCtx.WithError(err).Error("Failed to promote during full promotion")
			newStatus.Phase = "Failed"
			newStatus.Message = fmt.Sprintf("Failed to promote: %v", err)
			return ctrl.Result{}, err
		}

		newStatus.Phase = "Successful"
		newStatus.Message = "Rollout promoted successfully (full promotion)"
		newStatus.RolloutInProgress = false
		newStatus.PromoteFull = false // Clear the flag
		newStatus.Paused = false
		newStatus.PauseStartTime = nil

		// Remove progressing condition and set completed condition
		conditions.RemoveRolloutPluginCondition(newStatus, conditions.RolloutPluginProgressing)
		completedCondition := conditions.NewRolloutPluginCondition(
			conditions.RolloutPluginCompleted,
			corev1.ConditionTrue,
			conditions.RolloutPluginCompletedReason,
			"RolloutPlugin promoted successfully (full promotion)")
		conditions.SetRolloutPluginCondition(newStatus, *completedCondition)

		logCtx.Info("Full promotion completed successfully")
		return ctrl.Result{}, nil
	}

	// Reconcile analysis runs if AnalysisHelper is available
	if r.AnalysisHelper != nil {
		// Create a temporary rolloutPlugin with newStatus for analysis reconciliation
		// This ensures reconcileAnalysisRuns sees the latest status including step index
		rpWithNewStatus := rolloutPlugin.DeepCopy()
		rpWithNewStatus.Status = *newStatus

		allArs, err := r.getAnalysisRunsForRolloutPlugin(ctx, rpWithNewStatus)
		if err != nil {
			logCtx.WithError(err).Error("Failed to get analysis runs")
			return ctrl.Result{}, err
		}
		if err := r.reconcileAnalysisRuns(ctx, rpWithNewStatus, allArs); err != nil {
			logCtx.WithError(err).Error("Failed to reconcile analysis runs")
			return ctrl.Result{}, err
		}
		// TODOH too much copying?
		// Copy analysis status from rpWithNewStatus back to newStatus
		// reconcileAnalysisRuns updates rpWithNewStatus.Status directly
		newStatus.Canary.CurrentBackgroundAnalysisRunStatus = rpWithNewStatus.Status.Canary.CurrentBackgroundAnalysisRunStatus
		newStatus.Canary.CurrentStepAnalysisRunStatus = rpWithNewStatus.Status.Canary.CurrentStepAnalysisRunStatus
	}

	// Initialize step index if not set
	if newStatus.CurrentStepIndex == nil {
		stepIndex := int32(0)
		newStatus.CurrentStepIndex = &stepIndex
		newStatus.CurrentStepComplete = false
	}

	currentStepIndex := *newStatus.CurrentStepIndex
	if currentStepIndex >= int32(len(canary.Steps)) {
		// All steps completed
		logCtx.Info("All canary steps completed, promoting")
		if err := plugin.Promote(ctx, workloadRef); err != nil {
			logCtx.WithError(err).Error("Failed to promote")
			newStatus.Phase = "Failed"
			newStatus.Message = fmt.Sprintf("Failed to promote: %v", err)
			return ctrl.Result{}, err
		}
		newStatus.Phase = "Successful"
		newStatus.Message = "Rollout completed successfully"
		newStatus.RolloutInProgress = false

		// Remove progressing condition and set completed condition
		conditions.RemoveRolloutPluginCondition(newStatus, conditions.RolloutPluginProgressing)
		completedCondition := conditions.NewRolloutPluginCondition(
			conditions.RolloutPluginCompleted,
			corev1.ConditionTrue,
			conditions.RolloutPluginCompletedReason,
			"RolloutPlugin completed successfully")
		conditions.SetRolloutPluginCondition(newStatus, *completedCondition)

		return ctrl.Result{}, nil
	}

	currentStep := canary.Steps[currentStepIndex]
	logCtx.WithField("stepIndex", currentStepIndex).Info("Processing canary step")

	// Process setWeight step
	if currentStep.SetWeight != nil {
		weight := *currentStep.SetWeight
		logCtx.WithField("weight", weight).Info("Setting weight")

		if err := plugin.SetWeight(ctx, workloadRef, weight); err != nil {
			logCtx.WithError(err).Error("Failed to set weight")
			newStatus.Phase = "Failed"
			newStatus.Message = fmt.Sprintf("Failed to set weight: %v", err)
			return ctrl.Result{}, err
		}

		verified, err := plugin.VerifyWeight(ctx, workloadRef, weight)
		if err != nil {
			logCtx.WithError(err).Error("Failed to verify weight")
			newStatus.Phase = "Failed"
			newStatus.Message = fmt.Sprintf("Failed to verify weight: %v", err)
			return ctrl.Result{}, err
		}

		if !verified {
			newStatus.Message = fmt.Sprintf("Waiting for weight %d to be verified", weight)
			// Requeue to check again after 5 seconds
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		// Weight verified, move to next step
		newStatus.Message = fmt.Sprintf("Weight set to %d and verified", weight)
		nextStep := currentStepIndex + 1
		newStatus.CurrentStepIndex = &nextStep
		logCtx.WithField("nextStep", nextStep).Info("Weight verified, moving to next step")
		// Requeue immediately to process next step
		// SetWeight modified the workload, but we need to move to next step now
		return ctrl.Result{Requeue: true}, nil
	}

	// Handle analysis step
	if currentStep.Analysis != nil {
		logCtx.Info("Handling analysis step")

		// Check if step analysis is running
		if newStatus.Canary.CurrentStepAnalysisRunStatus == nil {
			newStatus.Message = "Waiting for analysis to start"
			// Requeue to check again after 2 seconds
			return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
		}

		analysisStatus := newStatus.Canary.CurrentStepAnalysisRunStatus.Status
		logCtx.WithFields(log.Fields{
			"status":      analysisStatus,
			"analysisRun": newStatus.Canary.CurrentStepAnalysisRunStatus.Name,
		}).Info("Step analysis status")

		switch analysisStatus {
		case v1alpha1.AnalysisPhaseSuccessful:
			// Analysis completed successfully, move to next step
			logCtx.Info("Step analysis completed successfully, moving to next step")
			nextStep := currentStepIndex + 1
			newStatus.CurrentStepIndex = &nextStep
			newStatus.CurrentStepComplete = false
			newStatus.Message = "Analysis successful"
			// Requeue immediately to process next step
			// Analysis completion doesn't modify the workload, so no watch trigger
			return ctrl.Result{Requeue: true}, nil

		case v1alpha1.AnalysisPhaseFailed, v1alpha1.AnalysisPhaseError:
			// Analysis failed, abort the rollout
			logCtx.Error("Step analysis failed, aborting rollout")
			newStatus.Phase = "Failed"
			newStatus.Message = fmt.Sprintf("Analysis failed: %s", newStatus.Canary.CurrentStepAnalysisRunStatus.Name)
			newStatus.RolloutInProgress = false
			if err := plugin.Abort(ctx, workloadRef); err != nil {
				logCtx.WithError(err).Error("Failed to abort rollout")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil

		case v1alpha1.AnalysisPhaseInconclusive:
			// Analysis is inconclusive, pause the rollout
			logCtx.Info("Step analysis is inconclusive, pausing rollout")
			newStatus.Paused = true
			newStatus.Message = "Paused: Analysis inconclusive"
			return ctrl.Result{}, nil

		case v1alpha1.AnalysisPhaseRunning, v1alpha1.AnalysisPhasePending, "":
			// Analysis is still running, wait
			newStatus.Message = fmt.Sprintf("Running analysis: %s", newStatus.Canary.CurrentStepAnalysisRunStatus.Name)
			logCtx.WithField("analysisRun", newStatus.Canary.CurrentStepAnalysisRunStatus.Name).Info("Step analysis still running")
			// Requeue to check again after 5 seconds
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil

		default:
			logCtx.WithField("status", analysisStatus).Info("Unknown analysis status")
			newStatus.Message = fmt.Sprintf("Unknown analysis status: %s", analysisStatus)
			// Requeue to check again
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}

	// Handle pause step
	if currentStep.Pause != nil {
		logCtx.WithField("pauseStartTime", newStatus.PauseStartTime).Info("Handling pause step")

		if newStatus.PauseStartTime == nil {
			now := metav1.Now()
			newStatus.PauseStartTime = &now
			newStatus.Paused = true
			newStatus.Message = "Paused"
			logCtx.WithField("duration", currentStep.Pause.Duration).Info("Starting pause")
			// Update status to persist pause start time
			// Status update will trigger another reconcile, no need for immediate requeue
			return ctrl.Result{}, nil
		}

		// Check if pause duration has elapsed
		if currentStep.Pause.Duration != nil {
			durationStr := currentStep.Pause.Duration.String()
			duration, err := time.ParseDuration(durationStr)
			if err != nil {
				logCtx.WithError(err).WithField("duration", durationStr).Error("Failed to parse pause duration")
				newStatus.Phase = "Failed"
				newStatus.Message = fmt.Sprintf("Invalid pause duration: %v", err)
				return ctrl.Result{}, err
			}

			elapsed := time.Since(newStatus.PauseStartTime.Time)
			if elapsed >= duration {
				logCtx.Info("Pause duration elapsed, moving to next step")
				// Move to next step
				nextStep := currentStepIndex + 1
				newStatus.CurrentStepIndex = &nextStep
				newStatus.CurrentStepComplete = false
				newStatus.PauseStartTime = nil
				newStatus.Paused = false
				// Requeue immediately to process next step
				// Pause completion doesn't modify the workload, so no watch trigger
				return ctrl.Result{Requeue: true}, nil
			}

			remaining := duration - elapsed
			newStatus.Message = fmt.Sprintf("Paused (remaining: %s)", remaining.Round(time.Second))
			logCtx.WithField("remaining", remaining).Info("Still paused")
			// Requeue when pause should be done
			return ctrl.Result{RequeueAfter: remaining}, nil
		}

		// Indefinite pause - wait for manual promotion
		logCtx.Info("Rollout is paused indefinitely, waiting for manual promotion")
		return ctrl.Result{}, nil
	}

	// If we reach here, the step has no recognized type (setWeight, analysis, pause)
	// This could be an empty step or a future step type we don't handle yet
	// Move to the next step immediately
	logCtx.WithField("stepIndex", currentStepIndex).Info("Step has no action, moving to next step")
	nextStep := currentStepIndex + 1
	newStatus.CurrentStepIndex = &nextStep
	newStatus.CurrentStepComplete = false
	newStatus.Message = fmt.Sprintf("Completed step %d", currentStepIndex)
	// Requeue immediately to process next step
	return ctrl.Result{Requeue: true}, nil
} //TODO return ctrl.Result{}, nil ?

// processRetry handles the retry logic when spec.RestartAt is set
func (r *RolloutPluginReconciler) processRetry(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, plugin ResourcePlugin, workloadRef v1alpha1.WorkloadRef, logCtx *log.Entry) (ctrl.Result, error) {
	restartAt := *rolloutPlugin.Spec.RestartAt

	logCtx.WithFields(log.Fields{"restartAt": restartAt, "attempt": newStatus.RetryAttempt + 1}).Info("Processing rollout retry")

	// SAFETY CHECK: Only allow retry if rollout has been aborted
	// This matches the behavior of the main Rollout controller - retry is only valid after abort
	if !newStatus.Aborted {
		logCtx.WithFields(log.Fields{
			"phase":   newStatus.Phase,
			"aborted": newStatus.Aborted,
		}).Info("Cannot retry: rollout has not been aborted")

		newStatus.Phase = "Failed"
		newStatus.Message = "Cannot retry: rollout has not been aborted. Retry is only allowed after aborting the rollout."

		// Clear RestartAt to prevent retry loop
		rpCopy := rolloutPlugin.DeepCopy()
		rpCopy.Spec.RestartAt = nil
		if err := r.Update(ctx, rpCopy); err != nil {
			logCtx.WithError(err).Error("Failed to clear RestartAt")
		}

		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	// Call plugin Reset() to return workload to baseline
	if err := plugin.Reset(ctx, workloadRef); err != nil {
		logCtx.WithError(err).Error("Plugin reset failed")
		newStatus.Phase = "Failed"
		newStatus.Message = fmt.Sprintf("Retry failed: plugin reset error: %v", err)

		// Clear RestartAt to prevent retry loop
		rpCopy := rolloutPlugin.DeepCopy()
		rpCopy.Spec.RestartAt = nil
		if err := r.Update(ctx, rpCopy); err != nil {
			logCtx.WithError(err).Error("Failed to clear RestartAt")
		}

		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	// Increment retry attempt counter
	newStatus.RetryAttempt++
	now := metav1.Now()
	newStatus.RestartedAt = &now

	// Reset rollout state
	newStatus.CurrentStepIndex = &restartAt
	newStatus.CurrentStepComplete = false
	newStatus.RolloutInProgress = true
	newStatus.Paused = false
	newStatus.PauseStartTime = nil
	newStatus.Aborted = false
	newStatus.Abort = false // Clear abort flag to prevent re-abort
	newStatus.Phase = "Progressing"
	newStatus.Message = fmt.Sprintf("Retry attempt %d: restarting at step %d", newStatus.RetryAttempt, restartAt)

	// Set progressing condition
	condition := conditions.NewRolloutPluginCondition(
		conditions.RolloutPluginProgressing,
		corev1.ConditionTrue,
		"RolloutRestarted",
		fmt.Sprintf("Rollout restarted at step %d (attempt %d)", restartAt, newStatus.RetryAttempt))
	conditions.SetRolloutPluginCondition(newStatus, *condition)

	// Update status FIRST before clearing spec.restartAt
	// This ensures status changes are visible before the spec field is cleared
	if err := r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx); err != nil {
		return ctrl.Result{}, err
	}

	// Now clear RestartAt from spec (one-shot trigger)
	// IMPORTANT: Fetch the latest version to avoid conflicts with concurrent updates
	// We use Patch() instead of Update() to only modify spec, leaving status untouched
	latestRP := &v1alpha1.RolloutPlugin{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(rolloutPlugin), latestRP); err != nil {
		logCtx.WithError(err).Error("Failed to fetch latest RolloutPlugin for spec update")
		return ctrl.Result{}, err
	}

	// Only update if restartAt is still set (may have been cleared already)
	if latestRP.Spec.RestartAt != nil {
		patch := client.MergeFrom(latestRP.DeepCopy())
		latestRP.Spec.RestartAt = nil
		if err := r.Patch(ctx, latestRP, patch); err != nil {
			logCtx.WithError(err).Error("Failed to clear RestartAt")
			// Don't return error - status is already updated, this is just cleanup
		}
	}

	logCtx.WithFields(log.Fields{
		"attempt":      newStatus.RetryAttempt,
		"startingStep": restartAt,
	}).Info("Retry processed successfully")

	// Requeue immediately to process the restart step
	// Reset() modified the workload, and we need to begin processing from restartAt step
	return ctrl.Result{Requeue: true}, nil
}

// updateStatus updates the status of the RolloutPlugin.
func (r *RolloutPluginReconciler) updateStatus(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, logCtx *log.Entry) error {
	patch := client.MergeFrom(rolloutPlugin.DeepCopy())
	rolloutPlugin.Status = *newStatus

	if err := r.Status().Patch(ctx, rolloutPlugin, patch); err != nil {
		logCtx.WithError(err).Error("Failed to update status")
		return err
	}

	logCtx.Debug("Status updated successfully")
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RolloutPluginReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Create a predicate that filters StatefulSet events to only trigger on meaningful changes
	// This prevents excessive reconciliation from status-only updates
	statefulSetPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldSts, ok1 := e.ObjectOld.(*appsv1.StatefulSet)
			newSts, ok2 := e.ObjectNew.(*appsv1.StatefulSet)
			if !ok1 || !ok2 {
				return true
			}

			// Skip if ResourceVersion is the same (periodic resync)
			if oldSts.ResourceVersion == newSts.ResourceVersion {
				return false
			}

			// Trigger reconcile if spec changed (generation changed)
			if oldSts.Generation != newSts.Generation {
				return true
			}

			// Trigger reconcile if revision changed (rollout in progress)
			if oldSts.Status.CurrentRevision != newSts.Status.CurrentRevision ||
				oldSts.Status.UpdateRevision != newSts.Status.UpdateRevision {
				return true
			}

			// Trigger reconcile if replica counts changed
			if oldSts.Status.ReadyReplicas != newSts.Status.ReadyReplicas ||
				oldSts.Status.UpdatedReplicas != newSts.Status.UpdatedReplicas ||
				oldSts.Status.AvailableReplicas != newSts.Status.AvailableReplicas {
				return true
			}

			// Skip other status-only updates
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return true
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.RolloutPlugin{}).
		Watches(
			&appsv1.StatefulSet{},
			handler.EnqueueRequestsFromMapFunc(r.findRolloutPluginsForWorkload),
			builder.WithPredicates(statefulSetPredicate),
		).
		Complete(r)
}

// findRolloutPluginsForWorkload maps a workload (StatefulSet, DaemonSet, Deployment, etc.) to RolloutPlugin CRs that reference it
// This is kind-agnostic - it matches any workload based on the WorkloadRef.Kind and WorkloadRef.Name
func (r *RolloutPluginReconciler) findRolloutPluginsForWorkload(ctx context.Context, obj client.Object) []reconcile.Request {
	workloadKind := obj.GetObjectKind().GroupVersionKind().Kind
	// For typed objects, GroupVersionKind may not be set, so we need to infer it
	if workloadKind == "" {
		switch obj.(type) {
		case *appsv1.StatefulSet:
			workloadKind = "StatefulSet"
		default:
			workloadKind = "Unknown"
		}
	}
	// TOODH maintain an allow list?

	logCtx := log.WithFields(log.Fields{
		"workloadKind": workloadKind,
		"workloadName": obj.GetName(),
		"namespace":    obj.GetNamespace(),
	})

	logCtx.Debug("Workload event received, checking for related RolloutPlugins")

	// List all RolloutPlugin resources in the same namespace as the workload
	var rolloutPlugins v1alpha1.RolloutPluginList
	if err := r.Client.List(ctx, &rolloutPlugins, client.InNamespace(obj.GetNamespace())); err != nil {
		logCtx.WithError(err).Error("Failed to list RolloutPlugin resources")
		return []reconcile.Request{}
	}

	// Find RolloutPlugins that reference this workload (matching both Kind and Name)
	var requests []reconcile.Request
	for _, rp := range rolloutPlugins.Items {
		if rp.Spec.WorkloadRef.Kind == workloadKind &&
			rp.Spec.WorkloadRef.Name == obj.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKey{
					Namespace: rp.GetNamespace(),
					Name:      rp.GetName(),
				},
			})
			logCtx.WithFields(log.Fields{
				"rolloutplugin": rp.GetName(),
			}).Info("Workload change detected, triggering RolloutPlugin reconciliation")
		}
	}

	if len(requests) == 0 {
		logCtx.Debug("No RolloutPlugins found referencing this workload")
	}

	return requests
}

// meetsMinReadySeconds checks if the workload meets the minReadySeconds requirement.
// For now, this is a simple check that compares availableReplicas with readyReplicas.
// A more sophisticated implementation would track pod ready times, but that requires
// fetching pod details from the cluster, which is kind-specific.
// Plugins can override this behavior with their own kind-specific logic if needed.
func meetsMinReadySeconds(resourceStatus *ResourceStatus, minReadySeconds int32) bool {
	if minReadySeconds == 0 {
		return true
	}
	// Simple heuristic: if availableReplicas equals readyReplicas, assume minReadySeconds is met
	// This works because availableReplicas in most workloads (Deployment, ReplicaSet, StatefulSet, DaemonSet)
	// already factors in minReadySeconds
	return resourceStatus.AvailableReplicas == resourceStatus.ReadyReplicas
}
