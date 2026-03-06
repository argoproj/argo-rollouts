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

	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	plugintypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/argoproj/argo-rollouts/utils/record"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
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
	MetricsServer     *metrics.MetricsServer
	Recorder          record.EventRecorder
	InstanceID        string
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

	// Restart returns the workload to baseline state for restart
	Restart(ctx context.Context, workloadRef v1alpha1.WorkloadRef) error
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
	startTime := timeutil.Now()
	logCtx := log.WithFields(log.Fields{"namespace": req.Namespace, "rolloutplugin": req.Name})

	// Fetch the RolloutPlugin instance
	rolloutPlugin := &v1alpha1.RolloutPlugin{}
	if err := r.Get(ctx, req.NamespacedName, rolloutPlugin); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Record reconciliation duration and error metrics
	defer func() {
		if r.MetricsServer != nil {
			duration := time.Since(startTime)
			r.MetricsServer.IncRolloutPluginReconcile(rolloutPlugin, duration)
			logCtx.WithField("time_ms", duration.Seconds()*1e3).Info("Reconciliation completed")
		}
	}()

	// Handle deletion
	if !rolloutPlugin.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, rolloutPlugin, logCtx)
	}

	// Reconcile the RolloutPlugin
	result, err := r.reconcile(ctx, rolloutPlugin, logCtx)
	if err != nil {
		// Record error metric
		if r.MetricsServer != nil {
			r.MetricsServer.IncError(rolloutPlugin.Namespace, rolloutPlugin.Name, logutil.RolloutPluginKey)
		}
	}
	return result, err
}

// handleDeletion handles the deletion of a RolloutPlugin
func (r *RolloutPluginReconciler) handleDeletion(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, logCtx *log.Entry) (ctrl.Result, error) {
	logCtx.Info("Handling deletion of RolloutPlugin")

	// Record deletion event
	if r.Recorder != nil {
		r.Recorder.Eventf(rolloutPlugin, record.EventOptions{
			EventReason: "RolloutPluginDeleted",
		}, "RolloutPlugin '%s' deleted", rolloutPlugin.Name)
	}

	// Clean up metrics for this RolloutPlugin
	if r.MetricsServer != nil {
		r.MetricsServer.Remove(rolloutPlugin.Namespace, rolloutPlugin.Name, logutil.RolloutPluginKey)
	}

	return ctrl.Result{}, nil
}

// reconcile performs the main reconciliation logic
func (r *RolloutPluginReconciler) reconcile(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, logCtx *log.Entry) (ctrl.Result, error) {
	//logCtx.Info("Reconciling RolloutPlugin")

	newStatus := rolloutPlugin.Status.DeepCopy()
	newStatus.ObservedGeneration = rolloutPlugin.Generation

	// Validate the RolloutPlugin spec
	prevInvalidSpecCond := conditions.GetRolloutPluginCondition(rolloutPlugin.Status, conditions.RolloutPluginInvalidSpec)
	invalidSpecCond := conditions.VerifyRolloutPluginSpec(rolloutPlugin, prevInvalidSpecCond)
	if invalidSpecCond != nil {
		logCtx.WithField("reason", invalidSpecCond.Message).Warn("RolloutPlugin spec validation failed")

		// Record warning event for invalid spec
		if r.Recorder != nil {
			r.Recorder.Warnf(rolloutPlugin, record.EventOptions{
				EventReason: conditions.RolloutPluginInvalidSpecReason,
			}, invalidSpecCond.Message)
		}

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

	// Get the plugin from the singleton PluginManager
	// Plugins are registered at startup and shared across all reconcilers
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

	// Prepare workload reference with namespace defaulting (used by multiple operations below)
	workloadRef := rolloutPlugin.Spec.WorkloadRef
	if workloadRef.Namespace == "" {
		workloadRef.Namespace = rolloutPlugin.Namespace
	}

	// Check if restart is requested via status.Restart (after abort)
	if newStatus.Restart && newStatus.Aborted {
		return r.processRestart(ctx, rolloutPlugin, newStatus, plugin, workloadRef, logCtx)
	}

	// Reject restart if not aborted
	if newStatus.Restart && !newStatus.Aborted {
		logCtx.Warn("Restart rejected: rollout has not been aborted")
		newStatus.Restart = false // TODOH
	}

	// Check if spec.paused is set (manual pause by user)
	if rolloutPlugin.Spec.Paused {
		if !newStatus.Paused {
			logCtx.Info("Rollout manually paused by user")

			// Record pause event
			if r.Recorder != nil {
				r.Recorder.Eventf(rolloutPlugin, record.EventOptions{
					EventReason: conditions.RolloutPluginPausedReason,
				}, conditions.RolloutPluginPausedMessage)
			}

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

		// Record resume event
		if r.Recorder != nil {
			r.Recorder.Eventf(rolloutPlugin, record.EventOptions{
				EventReason: "RolloutPluginResumed",
			}, "RolloutPlugin resumed from manual pause")
		}

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

		// Record abort event
		if r.Recorder != nil {
			r.Recorder.Warnf(rolloutPlugin, record.EventOptions{
				EventReason: conditions.RolloutPluginAbortedReason,
			}, conditions.RolloutPluginAbortedMessage)
		}

		newStatus.Aborted = true
		newStatus.Abort = false                               // Clear the abort flag
		newStatus.AbortedRevision = newStatus.UpdatedRevision // Track which revision was aborted
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

	// Check and update pause/resume conditions before timeout check
	// This ensures that pause time doesn't count toward progressDeadlineSeconds
	if err := r.checkPausedConditions(ctx, rolloutPlugin, newStatus, logCtx); err != nil {
		logCtx.WithError(err).Error("Failed to check paused conditions")
		return ctrl.Result{}, err
	}

	// Check progress deadline timeout
	// Note: Unlike abort/pause checks, we DO NOT return early here
	// This matches Rollout controller behavior - timeout is a warning condition
	// that allows the rollout to continue processing and respond to user actions
	if conditions.RolloutPluginTimedOut(rolloutPlugin, newStatus) {
		logCtx.Info("RolloutPlugin has timed out")

		// Get current timeout condition to check if it changed
		currentTimeoutCond := conditions.GetRolloutPluginCondition(*newStatus, conditions.RolloutPluginProgressing)
		timeoutCondAlreadySet := currentTimeoutCond != nil && currentTimeoutCond.Reason == conditions.RolloutPluginTimedOutReason

		// Set timeout condition (will be no-op if already set with same values)
		timeoutCondition := conditions.NewRolloutPluginCondition(
			conditions.RolloutPluginProgressing,
			corev1.ConditionFalse,
			conditions.RolloutPluginTimedOutReason,
			fmt.Sprintf("RolloutPlugin %s has timed out progressing after %d seconds",
				rolloutPlugin.Name,
				defaults.GetRolloutPluginProgressDeadlineSecondsOrDefault(rolloutPlugin)))
		condChanged := conditions.SetRolloutPluginCondition(newStatus, *timeoutCondition)

		// If progressDeadlineAbort is enabled and not already aborted, abort the rollout
		// Only abort when condition first changes to timeout (not on every reconciliation)
		if condChanged && rolloutPlugin.Spec.ProgressDeadlineAbort && !newStatus.Aborted {
			logCtx.Info("Aborting RolloutPlugin due to timeout (progressDeadlineAbort=true)")
			if abortErr := plugin.Abort(ctx, workloadRef); abortErr != nil {
				logCtx.WithError(abortErr).Error("Failed to abort rollout due to timeout")
			}
			newStatus.Aborted = true
			newStatus.AbortedRevision = newStatus.UpdatedRevision // Track which revision was aborted
			newStatus.RolloutInProgress = false
			newStatus.Phase = "Degraded"
			newStatus.Message = "Rollout aborted due to timeout"

			// Record abort event
			if r.Recorder != nil {
				r.Recorder.Warnf(rolloutPlugin, record.EventOptions{
					EventReason: conditions.RolloutPluginAbortedReason,
				}, "RolloutPlugin aborted due to progress deadline exceeded")
			}
		} else if !timeoutCondAlreadySet && rolloutPlugin.Spec.ProgressDeadlineAbort && !newStatus.Aborted {
			// progressDeadlineAbort may have been set after an existing timeout
			// If update is not aborted yet, we need to abort now
			logCtx.Info("Aborting RolloutPlugin due to timeout (progressDeadlineAbort set after timeout)")
			if abortErr := plugin.Abort(ctx, workloadRef); abortErr != nil {
				logCtx.WithError(abortErr).Error("Failed to abort rollout due to timeout")
			}
			newStatus.Aborted = true
			newStatus.AbortedRevision = newStatus.UpdatedRevision
			newStatus.RolloutInProgress = false
			newStatus.Phase = "Degraded"
			newStatus.Message = "Rollout aborted due to timeout"

			// Record abort event
			if r.Recorder != nil {
				r.Recorder.Warnf(rolloutPlugin, record.EventOptions{
					EventReason: conditions.RolloutPluginAbortedReason,
				}, "RolloutPlugin aborted due to progress deadline exceeded")
			}
		}

		// Record timeout event (if condition just changed)
		if condChanged && r.Recorder != nil {
			r.Recorder.Warnf(rolloutPlugin, record.EventOptions{
				EventReason: conditions.RolloutPluginTimedOutReason,
			}, fmt.Sprintf(conditions.RolloutPluginTimedOutMessage, rolloutPlugin.Name))
		}

		// DO NOT return early - continue processing
		// This allows:
		// 1. User to promote/pause/resume even when timed out
		// 2. Pause steps to complete after their duration
		// 3. Analysis to finish and advance steps
		// 4. Manual actions (abort, promoteFull) to work
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

	if oldUpdatedRevision != "" && oldUpdatedRevision != resourceStatus.UpdatedRevision {
		logCtx.WithFields(log.Fields{
			"previousUpdatedRevision": oldUpdatedRevision,
			"newUpdatedRevision":      resourceStatus.UpdatedRevision,
		}).Info("Detected new rollout while previous rollout in progress")

		// If this is a different revision from the aborted one, clear aborted state
		if newStatus.AbortedRevision != "" && resourceStatus.UpdatedRevision != newStatus.AbortedRevision {
			logCtx.Info("New revision detected, clearing aborted state")
			newStatus.Aborted = false
			newStatus.AbortedRevision = ""
		}

		// Clear timeout condition if present - new revision should get a fresh start
		timeoutCond := conditions.GetRolloutPluginCondition(*newStatus, conditions.RolloutPluginProgressing)
		if timeoutCond != nil && timeoutCond.Reason == conditions.RolloutPluginTimedOutReason {
			logCtx.Info("New revision detected, clearing timeout condition")
			conditions.RemoveRolloutPluginCondition(newStatus, conditions.RolloutPluginProgressing)
		}

		// Reset restart counter for new rollout
		newStatus.RestartCount = 0
		newStatus.RestartedAt = nil
		// newStatus.CurrentStepIndex = nil // TODOH
		// newStatus.Phase = "Progressing"
	}

	// Check if we need to start a rollout
	if resourceStatus.CurrentRevision != resourceStatus.UpdatedRevision {
		if !newStatus.RolloutInProgress {
			// If currently aborted, check if this is the same revision or a new one
			if newStatus.Aborted {
				if newStatus.AbortedRevision != "" && resourceStatus.UpdatedRevision == newStatus.AbortedRevision {
					// Same revision that was aborted - don't auto-restart, stay aborted
					logCtx.WithField("abortedRevision", newStatus.AbortedRevision).Info("Rollout is aborted, not auto-restarting same revision")
					return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
				} else {
					// Different revision - clear aborted state and allow new rollout
					logCtx.WithFields(log.Fields{
						"abortedRevision": newStatus.AbortedRevision,
						"newRevision":     resourceStatus.UpdatedRevision,
					}).Info("New revision detected, clearing aborted state")
					newStatus.Aborted = false
					newStatus.AbortedRevision = ""
				}
			}

			logCtx.Info("Starting new rollout")

			// Record rollout updated/started event
			if r.Recorder != nil {
				r.Recorder.Eventf(rolloutPlugin, record.EventOptions{
					EventReason: conditions.RolloutPluginProgressingReason,
				}, "RolloutPlugin updated to revision %s", resourceStatus.UpdatedRevision)
			}

			// Reset restart counter for new rollout (new UpdatedRevision)
			// newStatus.RestartCount = 0
			// newStatus.RestartedAt = nil // TODOH

			newStatus.RolloutInProgress = true
			newStatus.CurrentStepIndex = nil
			newStatus.Phase = "Progressing"

			// Remove any old Completed condition from previous rollout
			// This is critical - otherwise the old Completed condition can interfere with pause/resume logic
			conditions.RemoveRolloutPluginCondition(newStatus, conditions.RolloutPluginCompleted)

			// Set progressing condition
			condition := conditions.NewRolloutPluginCondition(
				conditions.RolloutPluginProgressing,
				corev1.ConditionTrue,
				conditions.RolloutPluginProgressingReason,
				"RolloutPlugin is progressing")
			conditions.SetRolloutPluginCondition(newStatus, *condition)
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

// checkPausedConditions checks if the given RolloutPlugin is paused or not and adds an appropriate condition.
// These conditions are needed so that we won't accidentally report lack of progress for resumed rollouts
// that were paused for longer than progressDeadlineSeconds.
func (r *RolloutPluginReconciler) checkPausedConditions(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, logCtx *log.Entry) error {
	// Progressing condition
	progCond := conditions.GetRolloutPluginCondition(*newStatus, conditions.RolloutPluginProgressing)
	progCondPaused := progCond != nil && progCond.Reason == conditions.RolloutPluginPausedReason

	isPaused := rolloutPlugin.Spec.Paused || newStatus.Paused
	abortCondExists := progCond != nil && progCond.Reason == conditions.RolloutPluginAbortedReason
	completedCondExists := conditions.GetRolloutPluginCondition(*newStatus, conditions.RolloutPluginCompleted) != nil

	var updatedConditions []*v1alpha1.RolloutPluginCondition

	// Update Progressing condition when pause state changes
	// IMPORTANT: Only update if there's an active rollout (not completed)
	// Completed rollouts should not have their conditions modified by pause/resume checks
	if (isPaused != progCondPaused) && !abortCondExists && !completedCondExists && newStatus.RolloutInProgress {
		if isPaused {
			// Set Progressing condition to Paused
			updatedConditions = append(updatedConditions,
				conditions.NewRolloutPluginCondition(
					conditions.RolloutPluginProgressing,
					corev1.ConditionUnknown,
					conditions.RolloutPluginPausedReason,
					conditions.RolloutPluginPausedMessage))
			logCtx.Info("Setting Progressing condition to Paused")
		} else {
			// Set Progressing condition to Resumed with NEW timestamp
			// This effectively resets the progressDeadlineSeconds timer
			updatedConditions = append(updatedConditions,
				conditions.NewRolloutPluginCondition(
					conditions.RolloutPluginProgressing,
					corev1.ConditionUnknown,
					conditions.RolloutPluginProgressingReason,
					"RolloutPlugin resumed"))
			logCtx.Info("Setting Progressing condition to Resumed (resets timeout timer)")
		}
	}

	// Handle restart after abort (when abort flag is cleared)
	if !newStatus.Abort && abortCondExists {
		updatedConditions = append(updatedConditions,
			conditions.NewRolloutPluginCondition(
				conditions.RolloutPluginProgressing,
				corev1.ConditionUnknown,
				conditions.RolloutPluginProgressingReason,
				"RolloutPlugin restarting after abort"))
		logCtx.Info("Setting Progressing condition to Restart after abort")
	}

	// Update Paused condition
	pauseCond := conditions.GetRolloutPluginCondition(*newStatus, conditions.RolloutPluginPaused)
	pausedCondTrue := pauseCond != nil && pauseCond.Status == corev1.ConditionTrue

	if (isPaused != pausedCondTrue) && !abortCondExists {
		condStatus := corev1.ConditionFalse
		if isPaused {
			condStatus = corev1.ConditionTrue
		}
		updatedConditions = append(updatedConditions,
			conditions.NewRolloutPluginCondition(
				conditions.RolloutPluginPaused,
				condStatus,
				conditions.RolloutPluginPausedReason,
				conditions.RolloutPluginPausedMessage))
		//logCtx.WithField("paused", isPaused).Info("Updating Paused condition")
	}

	if len(updatedConditions) == 0 {
		return nil
	}

	// Apply all condition updates
	for _, condition := range updatedConditions {
		conditions.SetRolloutPluginCondition(newStatus, *condition)
	}

	//logCtx.WithField("conditionCount", len(updatedConditions)).Info("Updated pause/resume conditions")
	return nil
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

		// Record completion event
		if r.Recorder != nil {
			r.Recorder.Eventf(rolloutPlugin, record.EventOptions{
				EventReason: conditions.RolloutPluginCompletedReason,
			}, "RolloutPlugin completed update to revision %s (full promotion)", newStatus.UpdatedRevision)
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

		// Record completion event
		if r.Recorder != nil {
			r.Recorder.Eventf(rolloutPlugin, record.EventOptions{
				EventReason: conditions.RolloutPluginCompletedReason,
			}, "RolloutPlugin completed update to revision %s", newStatus.UpdatedRevision)
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

	// Check background analysis status (if running)
	// Background analysis runs throughout the rollout, so check it before processing each step
	if newStatus.Canary.CurrentBackgroundAnalysisRunStatus != nil {
		bgAnalysisStatus := newStatus.Canary.CurrentBackgroundAnalysisRunStatus.Status
		logCtx.WithFields(log.Fields{
			"status":      bgAnalysisStatus,
			"analysisRun": newStatus.Canary.CurrentBackgroundAnalysisRunStatus.Name,
		}).Info("Background analysis status")

		switch bgAnalysisStatus {
		case v1alpha1.AnalysisPhaseFailed, v1alpha1.AnalysisPhaseError:
			// Background analysis failed, abort the rollout
			logCtx.Error("Background analysis failed, aborting rollout")
			newStatus.Phase = "Failed"
			newStatus.Message = fmt.Sprintf("Background analysis failed: %s", newStatus.Canary.CurrentBackgroundAnalysisRunStatus.Name)
			newStatus.RolloutInProgress = false
			newStatus.Aborted = true
			newStatus.AbortedRevision = newStatus.UpdatedRevision
			if err := plugin.Abort(ctx, workloadRef); err != nil {
				logCtx.WithError(err).Error("Failed to abort rollout")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil

		case v1alpha1.AnalysisPhaseInconclusive:
			// Background analysis is inconclusive, pause the rollout
			logCtx.Info("Background analysis is inconclusive, pausing rollout")
			newStatus.Paused = true
			newStatus.Message = "Paused: Background analysis inconclusive"
			return ctrl.Result{}, nil
		}
		// For Running, Pending, Successful, or unknown status, continue processing the step
		// Successful means the analysis completed successfully
	}

	// Process setWeight step
	if currentStep.SetWeight != nil {
		weight := *currentStep.SetWeight

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
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil // TODOH configurable?
		}

		// Weight verified, move to next step
		newStatus.Message = fmt.Sprintf("Weight set to %d and verified", weight)
		nextStep := currentStepIndex + 1
		newStatus.CurrentStepIndex = &nextStep

		// If next step is a pause, initialize pause state immediately
		// This ensures Paused and PauseStartTime are set atomically with CurrentStepIndex
		if nextStep < int32(len(canary.Steps)) && canary.Steps[nextStep].Pause != nil {
			now := metav1.Now()
			newStatus.PauseStartTime = &now
			newStatus.Paused = true
			newStatus.Message = "Paused"
			logCtx.WithFields(log.Fields{
				"nextStep": nextStep,
				"duration": canary.Steps[nextStep].Pause.Duration,
			}).Info("Weight verified, moving to pause step")
			// Return without requeue - status update will trigger reconcile
			return ctrl.Result{}, nil
		}

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

			// If next step is a pause, initialize pause state immediately
			// This ensures Paused and PauseStartTime are set atomically with CurrentStepIndex
			if nextStep < int32(len(canary.Steps)) && canary.Steps[nextStep].Pause != nil {
				now := metav1.Now()
				newStatus.PauseStartTime = &now
				newStatus.Paused = true
				newStatus.Message = "Paused"
				logCtx.WithFields(log.Fields{
					"nextStep": nextStep,
					"duration": canary.Steps[nextStep].Pause.Duration,
				}).Info("Analysis successful, moving to pause step")
				// Return without requeue - status update will trigger reconcile
				return ctrl.Result{}, nil
			}

			// Requeue immediately to process next step
			// Analysis completion doesn't modify the workload, so no watch trigger
			return ctrl.Result{Requeue: true}, nil

		case v1alpha1.AnalysisPhaseFailed, v1alpha1.AnalysisPhaseError:
			// Analysis failed, abort the rollout
			logCtx.Error("Step analysis failed, aborting rollout")
			newStatus.Phase = "Failed"
			newStatus.Message = fmt.Sprintf("Analysis failed: %s", newStatus.Canary.CurrentStepAnalysisRunStatus.Name)
			newStatus.RolloutInProgress = false
			newStatus.Aborted = true
			newStatus.AbortedRevision = newStatus.UpdatedRevision
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
		logCtx.WithField("pauseStartTime", newStatus.PauseStartTime).Info("Processing pause step")

		if newStatus.PauseStartTime == nil {
			now := metav1.Now()
			newStatus.PauseStartTime = &now
			newStatus.Paused = true
			newStatus.Message = "Paused"

			// Record pause event (step pause)
			if r.Recorder != nil {
				r.Recorder.Eventf(rolloutPlugin, record.EventOptions{
					EventReason: conditions.RolloutPluginPausedReason,
				}, "RolloutPlugin paused at step %d", currentStepIndex)
			}

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

				// Record resume event (step pause completed)
				if r.Recorder != nil {
					r.Recorder.Eventf(rolloutPlugin, record.EventOptions{
						EventReason: "RolloutPluginResumed",
					}, "RolloutPlugin resumed from pause at step %d", currentStepIndex)
				}

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
			//logCtx.WithField("remaining", remaining).Info("Still paused")
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

	// If next step is a pause, initialize pause state immediately
	// This ensures Paused and PauseStartTime are set atomically with CurrentStepIndex
	if nextStep < int32(len(canary.Steps)) && canary.Steps[nextStep].Pause != nil {
		now := metav1.Now()
		newStatus.PauseStartTime = &now
		newStatus.Paused = true
		newStatus.Message = "Paused"
		logCtx.WithFields(log.Fields{
			"nextStep": nextStep,
			"duration": canary.Steps[nextStep].Pause.Duration,
		}).Info("Empty step completed, moving to pause step")
		// Return without requeue - status update will trigger reconcile
		return ctrl.Result{}, nil
	}

	// Requeue immediately to process next step
	return ctrl.Result{Requeue: true}, nil
} //TODO return ctrl.Result{}, nil ?

func (r *RolloutPluginReconciler) processRestart(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, plugin ResourcePlugin, workloadRef v1alpha1.WorkloadRef, logCtx *log.Entry) (ctrl.Result, error) {
	logCtx.WithFields(log.Fields{"attempt": newStatus.RestartCount + 1}).Info("Processing rollout restart from step 0")

	// SAFETY CHECK: Only allow restart if rollout has been aborted
	// This matches the behavior of the main Rollout controller - restart is only valid after abort
	if !newStatus.Aborted {
		logCtx.WithFields(log.Fields{
			"phase":   newStatus.Phase,
			"aborted": newStatus.Aborted,
		}).Info("Cannot restart: rollout has not been aborted")

		newStatus.Phase = "Failed"
		newStatus.Message = "Cannot restart: rollout has not been aborted. Restart is only allowed after aborting the rollout."

		// Clear Restart flag to prevent loop
		newStatus.Restart = false

		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	// Call plugin Restart() to return workload to baseline
	if err := plugin.Restart(ctx, workloadRef); err != nil {
		logCtx.WithError(err).Error("Plugin restart failed")
		newStatus.Phase = "Failed"
		newStatus.Message = fmt.Sprintf("Restart failed: plugin restart error: %v", err)

		// Clear Restart flag to prevent loop
		newStatus.Restart = false

		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	// Increment restart attempt counter
	newStatus.RestartCount++
	now := metav1.Now()
	newStatus.RestartedAt = &now

	// Reset rollout state - always restart from step 0
	stepZero := int32(0)
	newStatus.CurrentStepIndex = &stepZero
	newStatus.CurrentStepComplete = false
	newStatus.RolloutInProgress = true
	newStatus.Paused = false
	newStatus.PauseStartTime = nil
	newStatus.Aborted = false
	newStatus.Abort = false // Clear abort flag to prevent re-abort
	newStatus.Phase = "Progressing"
	newStatus.Message = fmt.Sprintf("Restart attempt %d: restarting from step 0", newStatus.RestartCount)

	// Set progressing condition
	condition := conditions.NewRolloutPluginCondition(
		conditions.RolloutPluginProgressing,
		corev1.ConditionTrue,
		"RolloutRestarted",
		fmt.Sprintf("Rollout restarted from step 0 (attempt %d)", newStatus.RestartCount))
	conditions.SetRolloutPluginCondition(newStatus, *condition)

	// Clear Restart flag (one-shot trigger)
	newStatus.Restart = false

	// Update status to reflect restart processing
	if err := r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx); err != nil {
		return ctrl.Result{}, err
	}

	logCtx.WithFields(log.Fields{
		"attempt":      newStatus.RestartCount,
		"startingStep": 0,
	}).Info("Restart processed successfully, restarting from step 0")

	// Requeue immediately to process the restart
	// Restart() modified the workload, and we need to begin processing from step 0
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
	}

	// Create a predicate for RolloutPlugin that watches ALL updates (like Rollouts controller)
	// This is necessary for status field changes (abort, restart, promoteFull, etc.) to trigger reconciliation
	rolloutPluginPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldRP, okOld := e.ObjectOld.(*v1alpha1.RolloutPlugin)
			newRP, okNew := e.ObjectNew.(*v1alpha1.RolloutPlugin)
			if !okOld || !okNew {
				return false
			}

			// Skip if ResourceVersion is the same (periodic resync)
			if oldRP.ResourceVersion == newRP.ResourceVersion {
				return false
			}

			// Trigger on ALL other updates (spec or status changes)
			// This matches the Rollouts controller behavior where status.abort,
			// status.promoteFull, etc. trigger immediate reconciliation
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
	}

	// Predicate to filter AnalysisRun events - only trigger on phase changes
	analysisRunPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true // Always trigger on creation
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldAR, ok1 := e.ObjectOld.(*v1alpha1.AnalysisRun)
			newAR, ok2 := e.ObjectNew.(*v1alpha1.AnalysisRun)
			if !ok1 || !ok2 {
				return false
			}

			// Only trigger if phase changed (matches Rollouts controller behavior)
			if oldAR.Status.Phase != newAR.Status.Phase {
				return true
			}

			// Skip other updates
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true // Always trigger on deletion
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.RolloutPlugin{}).
		Owns(&v1alpha1.AnalysisRun{}, builder.WithPredicates(analysisRunPredicate)).
		Watches(
			&appsv1.StatefulSet{},
			handler.EnqueueRequestsFromMapFunc(r.findRolloutPluginsForWorkload),
			builder.WithPredicates(statefulSetPredicate),
		).
		Watches(
			&v1alpha1.RolloutPlugin{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(rolloutPluginPredicate),
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
	// TODOH maintain an allow list?

	logCtx := log.WithFields(log.Fields{
		"workloadKind": workloadKind,
		"workloadName": obj.GetName(),
		"namespace":    obj.GetNamespace(),
	})

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
		}
	}

	if len(requests) == 0 {
		logCtx.Debug("No RolloutPlugins found referencing this workload")
	}

	return requests
}
