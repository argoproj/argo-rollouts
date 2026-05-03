package rolloutplugin

import (
	"context"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
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
	rolloututil "github.com/argoproj/argo-rollouts/utils/rollout"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"

	validation "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/validation"
)

// RolloutPluginReconciler reconciles a RolloutPlugin object
type RolloutPluginReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	KubeClientset     kubernetes.Interface
	ArgoProjClientset clientset.Interface
	DynamicClientset  dynamic.Interface
	PluginManager     PluginManager
	MetricsServer     *metrics.MetricsServer
	Recorder          record.EventRecorder
	InstanceID        string
}

type PluginManager interface {
	GetPlugin(name string) (ResourcePlugin, error)
}

// ResourcePlugin is the interface that all resource plugins must implement.
// Built-in plugins implement this interface directly.
// External RPC plugins implement types.RpcResourcePlugin, which is adapted via RpcPluginWrapper.
type ResourcePlugin interface {
	// Init initializes the plugin. namespace is the controller's watch namespace
	// (empty/metav1.NamespaceAll for cluster-wide, or a specific namespace in --namespaced mode).
	Init(namespace string) error

	// GetResourceStatus gets the current status of the referenced workload
	GetResourceStatus(ctx context.Context, workloadRef v1alpha1.WorkloadRef) (*ResourceStatus, error)

	// SetWeight updates the weight (percentage of pods updated)
	SetWeight(ctx context.Context, workloadRef v1alpha1.WorkloadRef, weight int32) error

	// VerifyWeight checks if the desired weight has been achieved
	VerifyWeight(ctx context.Context, workloadRef v1alpha1.WorkloadRef, weight int32) (bool, error)

	// PromoteFull promotes the new version to stable
	PromoteFull(ctx context.Context, workloadRef v1alpha1.WorkloadRef) error

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

// Reconcile aims to move the current state of the RolloutPlugin closer to the desired state.
func (r *RolloutPluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := timeutil.Now()
	logCtx := log.WithFields(log.Fields{"namespace": req.Namespace, "rolloutplugin": req.Name})

	// Fetch the RolloutPlugin instance
	rolloutPlugin := &v1alpha1.RolloutPlugin{}
	if err := r.Get(ctx, req.NamespacedName, rolloutPlugin); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Record reconciliation duration
	defer func() {
		if r.MetricsServer != nil {
			duration := time.Since(startTime)
			r.MetricsServer.IncRolloutPluginReconcile(rolloutPlugin, duration)
			logCtx.WithField("time_ms", duration.Seconds()*1e3).Info("Reconciliation completed")
		}
	}()

	if !rolloutPlugin.DeletionTimestamp.IsZero() {
		return r.handleDeletion(rolloutPlugin, logCtx)
	}

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
func (r *RolloutPluginReconciler) handleDeletion(rolloutPlugin *v1alpha1.RolloutPlugin, logCtx *log.Entry) (ctrl.Result, error) {
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

	newStatus := rolloutPlugin.Status.DeepCopy()
	newStatus.ObservedGeneration = rolloutPlugin.Generation

	pCtx := &pauseContext{rolloutPlugin: rolloutPlugin, log: logCtx}

	prevInvalidSpecCond := conditions.GetRolloutPluginCondition(rolloutPlugin.Status, v1alpha1.RolloutPluginConditionInvalidSpec)
	invalidSpecCond := verifyRolloutPluginSpec(rolloutPlugin, prevInvalidSpecCond)
	if invalidSpecCond != nil {
		logCtx.WithField("reason", invalidSpecCond.Message).Warn("RolloutPlugin spec validation failed")

		if r.Recorder != nil {
			r.Recorder.Warnf(rolloutPlugin, record.EventOptions{
				EventReason: conditions.RolloutPluginInvalidSpecReason,
			}, invalidSpecCond.Message)
		}

		newStatus.Message = invalidSpecCond.Message
		conditions.SetRolloutPluginCondition(newStatus, *invalidSpecCond)
		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	refErr := r.validateReferencedResources(ctx, rolloutPlugin, newStatus, logCtx)
	if prevInvalidSpecCond != nil && refErr == nil {
		logCtx.Info("RolloutPlugin spec is now valid, removing InvalidSpec condition")
		conditions.RemoveRolloutPluginCondition(newStatus, v1alpha1.RolloutPluginConditionInvalidSpec)
	}

	if refErr != nil {
		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	// Get the plugin from the singleton PluginManager
	// Plugins are registered at startup and shared across all reconcilers
	plugin, err := r.PluginManager.GetPlugin(rolloutPlugin.Spec.Plugin.Name)
	if err != nil {
		logCtx.WithError(err).Error("Failed to get plugin")
		newStatus.Message = fmt.Sprintf("Plugin '%s' not found. Ensure the plugin is registered in the argo-rollouts-config ConfigMap under 'rolloutPlugins'", rolloutPlugin.Spec.Plugin.Name)
		pluginNotFoundCond := conditions.NewRolloutPluginCondition(
			v1alpha1.RolloutPluginConditionInvalidSpec,
			corev1.ConditionTrue,
			conditions.RolloutPluginInvalidSpecReason,
			newStatus.Message,
		)
		conditions.SetRolloutPluginCondition(newStatus, *pluginNotFoundCond)
		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	workloadRef := rolloutPlugin.Spec.WorkloadRef
	if workloadRef.Namespace == "" {
		workloadRef.Namespace = rolloutPlugin.Namespace
	}

	if newStatus.Restart && newStatus.Aborted {
		return r.processRestart(ctx, rolloutPlugin, newStatus, plugin, workloadRef, logCtx)
	}

	if newStatus.Restart && !newStatus.Aborted {
		logCtx.Warn("Restart rejected: rollout has not been aborted")
		newStatus.Restart = false
	}

	if newStatus.PromoteFull {
		logCtx.Info("PromoteFull detected, clearing pause state")
		newStatus.PauseConditions = nil
		newStatus.ControllerPause = false
		pCtx.ClearPauseConditions()
		newStatus.Message = "Full promotion in progress"
		// Clear manual pause (spec.paused) if set, so the rollout can proceed
		if rolloutPlugin.Spec.Paused {
			logCtx.Info("Clearing spec.paused as part of PromoteFull")
			patch := client.MergeFrom(rolloutPlugin.DeepCopy())
			rolloutPlugin.Spec.Paused = false
			if err := r.Patch(ctx, rolloutPlugin, patch); err != nil {
				logCtx.WithError(err).Error("Failed to clear spec.paused during PromoteFull")
				return ctrl.Result{}, err
			}
		}
	} else if rolloutPlugin.Spec.Paused {
		// Check if spec.paused is set (manual pause)
		if newStatus.Phase != v1alpha1.RolloutPluginPhasePaused {
			logCtx.Info("Rollout manually paused by user")

			// Record pause event
			if r.Recorder != nil {
				r.Recorder.Eventf(rolloutPlugin, record.EventOptions{
					EventReason: conditions.RolloutPluginPausedReason,
				}, conditions.RolloutPluginPausedMessage)
			}

			newStatus.Message = "manually paused"
		}
		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	} else if newStatus.Phase == v1alpha1.RolloutPluginPhasePaused && !rolloutPlugin.Spec.Paused && len(newStatus.PauseConditions) == 0 {
		logCtx.Info("Manual resume detected, clearing pause state")
		newStatus.Message = "Rollout resumed"
	}

	if newStatus.Abort && !newStatus.Aborted {
		logCtx.Info("Manual abort requested via status.Abort field")

		// Call plugin abort
		if abortErr := plugin.Abort(ctx, workloadRef); abortErr != nil {
			logCtx.WithError(abortErr).Error("Failed to abort rollout")
			newStatus.Message = fmt.Sprintf("Failed to abort rollout: %v", abortErr)
			condition := conditions.NewRolloutPluginCondition(
				v1alpha1.RolloutPluginConditionProgressing,
				corev1.ConditionFalse,
				conditions.RolloutPluginReconciliationErrorReason,
				newStatus.Message)
			conditions.SetRolloutPluginCondition(newStatus, *condition)
			return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
		}

		// Record abort event
		if r.Recorder != nil {
			r.Recorder.Warnf(rolloutPlugin, record.EventOptions{
				EventReason: conditions.RolloutPluginAbortedReason,
			}, conditions.RolloutPluginAbortedMessage)
		}

		pCtx.AddAbort("Rollout aborted by user")
		pCtx.CalculatePauseStatus(newStatus)
		newStatus.Message = "Rollout aborted by user"

		condition := conditions.NewRolloutPluginCondition(
			v1alpha1.RolloutPluginConditionProgressing,
			corev1.ConditionFalse,
			conditions.RolloutPluginAbortedReason,
			"Rollout manually aborted by user")
		conditions.SetRolloutPluginCondition(newStatus, *condition)

		logCtx.Info("Rollout aborted successfully")
		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	// Check and update pause/resume conditions before timeout check
	// This ensures that pause time doesn't count toward progressDeadlineSeconds
	if err := checkPausedConditions(rolloutPlugin, newStatus, logCtx); err != nil {
		logCtx.WithError(err).Error("Failed to check paused conditions")
		return ctrl.Result{}, err
	}

	// Check progress deadline timeout
	// Note: Unlike abort/pause checks, we DO NOT return early here timeout is a warning condition
	// that allows the rollout to continue processing and respond to user actions
	if conditions.RolloutPluginTimedOut(rolloutPlugin, newStatus) {
		logCtx.Info("RolloutPlugin has timed out")

		// Set timeout condition (will be no-op if already set with same values)
		timeoutCondition := conditions.NewRolloutPluginCondition(
			v1alpha1.RolloutPluginConditionProgressing,
			corev1.ConditionFalse,
			conditions.RolloutPluginTimedOutReason,
			fmt.Sprintf("RolloutPlugin %s has timed out progressing after %d seconds",
				rolloutPlugin.Name,
				defaults.GetRolloutPluginProgressDeadlineSecondsOrDefault(rolloutPlugin)))
		condChanged := conditions.SetRolloutPluginCondition(newStatus, *timeoutCondition)

		// Record timeout event
		if condChanged && r.Recorder != nil {
			r.Recorder.Warnf(rolloutPlugin, record.EventOptions{
				EventReason: conditions.RolloutPluginTimedOutReason,
			}, fmt.Sprintf(conditions.RolloutPluginTimedOutMessage, rolloutPlugin.Name))
		}

		if condChanged {
			// Condition first transitioned to timed-out this reconciliation.
			if rolloutPlugin.Spec.ProgressDeadlineAbort && !pCtx.IsAborted() {
				logCtx.Info("Aborting RolloutPlugin due to timeout (progressDeadlineAbort=true)")
				if abortErr := plugin.Abort(ctx, workloadRef); abortErr != nil {
					logCtx.WithError(abortErr).Error("Failed to abort rollout due to timeout")
				}
				msg := "Rollout aborted due to timeout"
				pCtx.AddAbort(msg)
				newStatus.Message = msg
				if r.Recorder != nil {
					r.Recorder.Warnf(rolloutPlugin, record.EventOptions{
						EventReason: conditions.RolloutPluginAbortedReason,
					}, "RolloutPlugin aborted due to progress deadline exceeded")
				}
			}
		} else {
			// Condition was already timed-out (unchanged). If ProgressDeadlineAbort was
			// enabled after the timeout occurred and the rollout is not yet aborted, abort now.
			if rolloutPlugin.Spec.ProgressDeadlineAbort && !pCtx.IsAborted() {
				logCtx.Info("Aborting already-timed-out RolloutPlugin (progressDeadlineAbort set retroactively)")
				if abortErr := plugin.Abort(ctx, workloadRef); abortErr != nil {
					logCtx.WithError(abortErr).Error("Failed to abort rollout due to timeout")
				}
				msg := "Rollout aborted due to timeout"
				pCtx.AddAbort(msg)
				newStatus.Message = msg
				if r.Recorder != nil {
					r.Recorder.Warnf(rolloutPlugin, record.EventOptions{
						EventReason: conditions.RolloutPluginAbortedReason,
					}, "RolloutPlugin aborted due to progress deadline exceeded")
				}
			}
		}
	}

	// Get the current status of the referenced workload
	resourceStatus, err := plugin.GetResourceStatus(ctx, workloadRef)
	if err != nil {
		logCtx.WithError(err).Error("Failed to get resource status")
		newStatus.Message = fmt.Sprintf("Failed to get resource status: %v", err)
		condition := conditions.NewRolloutPluginCondition(
			v1alpha1.RolloutPluginConditionProgressing,
			corev1.ConditionFalse,
			conditions.RolloutPluginReconciliationErrorReason,
			newStatus.Message)
		conditions.SetRolloutPluginCondition(newStatus, *condition)
		return ctrl.Result{RequeueAfter: 1 * time.Second}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	// Clear any stale ReconciliationError condition from a previous failed GetResourceStatus.
	progCond := conditions.GetRolloutPluginCondition(*newStatus, v1alpha1.RolloutPluginConditionProgressing)
	if progCond != nil && progCond.Reason == conditions.RolloutPluginReconciliationErrorReason {
		conditions.RemoveRolloutPluginCondition(newStatus, v1alpha1.RolloutPluginConditionProgressing)
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

		if newStatus.AbortedRevision != "" && resourceStatus.UpdatedRevision != newStatus.AbortedRevision {
			logCtx.Info("New revision detected, clearing aborted state")
			newStatus.Aborted = false
			newStatus.AbortedRevision = ""
			pCtx.RemoveAbort()
		}

		timeoutCond := conditions.GetRolloutPluginCondition(*newStatus, v1alpha1.RolloutPluginConditionProgressing)
		if timeoutCond != nil && timeoutCond.Reason == conditions.RolloutPluginTimedOutReason {
			logCtx.Info("New revision detected, clearing timeout condition")
			conditions.RemoveRolloutPluginCondition(newStatus, v1alpha1.RolloutPluginConditionProgressing)
		}

		// Reset restart counter for new rollout
		newStatus.RestartCount = 0
		newStatus.RestartedAt = nil

		// Reset step index so the new rollout starts from step 0
		// Without this, a new revision detected mid-rollout
		// would resume from the old step index, skipping earlier steps including pause steps.
		newStatus.CurrentStepIndex = nil
		newStatus.Message = "New revision detected, restarting rollout from step 0"

		// Clear pause state from previous rollout
		newStatus.PauseConditions = nil
		newStatus.ControllerPause = false
		pCtx.ClearPauseConditions()
	}

	// Check if we need to start a rollout
	if resourceStatus.CurrentRevision != resourceStatus.UpdatedRevision {
		if !conditions.IsRolloutPluginProgressing(newStatus) {
			// If timed out (without abort), don't restart — stay degraded.
			// Without this guard, the TimedOut condition (Progressing=False) causes
			// this block to fire and overwrite it with Progressing=True, creating
			// an infinite timeout→restart loop.
			timedOutCond := conditions.GetRolloutPluginCondition(*newStatus, v1alpha1.RolloutPluginConditionProgressing)
			if timedOutCond != nil && timedOutCond.Reason == conditions.RolloutPluginTimedOutReason {
				logCtx.Info("Rollout has timed out, staying in degraded state")
				pCtx.CalculatePauseStatus(newStatus)
				return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
			}

			// Use newStatus.Aborted (persisted) not pCtx.IsAborted(): we want to detect a
			// stable already-aborted state, not an abort that was just triggered this reconcile.
			if newStatus.Aborted {
				if newStatus.AbortedRevision != "" && resourceStatus.UpdatedRevision == newStatus.AbortedRevision {
					logCtx.WithField("abortedRevision", newStatus.AbortedRevision).Info("Rollout is aborted, not auto-restarting same revision")
					return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
				} else {
					logCtx.WithFields(log.Fields{
						"abortedRevision": newStatus.AbortedRevision,
						"newRevision":     resourceStatus.UpdatedRevision,
					}).Info("New revision detected, clearing aborted state")
					newStatus.Aborted = false
					newStatus.AbortedRevision = ""
					pCtx.RemoveAbort()
				}
			}

			logCtx.Info("Starting new rollout")

			// Record rollout updated/started event
			if r.Recorder != nil {
				r.Recorder.Eventf(rolloutPlugin, record.EventOptions{
					EventReason: conditions.RolloutPluginProgressingReason,
				}, "RolloutPlugin updated to revision %s", resourceStatus.UpdatedRevision)
			}

			newStatus.CurrentStepIndex = nil

			// Remove any old Completed condition from previous rollout
			conditions.RemoveRolloutPluginCondition(newStatus, v1alpha1.RolloutPluginConditionCompleted)

			condition := conditions.NewRolloutPluginCondition(
				v1alpha1.RolloutPluginConditionProgressing,
				corev1.ConditionTrue,
				conditions.RolloutPluginProgressingReason,
				"RolloutPlugin is progressing")
			conditions.SetRolloutPluginCondition(newStatus, *condition)
		}
	}

	if conditions.IsRolloutPluginProgressing(newStatus) {
		result, err := r.processRollout(ctx, rolloutPlugin, newStatus, plugin, workloadRef, pCtx, logCtx)
		if err != nil {
			logCtx.WithError(err).Error("Failed to process rollout")
			return result, err
		}
		pCtx.CalculatePauseStatus(newStatus)
		if err := r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx); err != nil {
			return ctrl.Result{}, err
		}
		return result, nil
	}

	// No rollout in progress — cancel any lingering analysis runs (e.g. background AR
	// that was still running when the rollout completed on the previous reconcile).
	rpWithNewStatus := rolloutPlugin.DeepCopy()
	rpWithNewStatus.Status = *newStatus

	allArs, err := r.getAnalysisRunsForRolloutPlugin(ctx, rpWithNewStatus)
	if err != nil {
		logCtx.WithError(err).Error("Failed to get analysis runs")
		return ctrl.Result{}, err
	}
	if err := r.reconcileAnalysisRuns(ctx, rpWithNewStatus, allArs, pCtx, logCtx); err != nil {
		logCtx.WithError(err).Error("Failed to reconcile analysis runs")
		return ctrl.Result{}, err
	}
	newStatus.Canary.CurrentBackgroundAnalysisRunStatus = rpWithNewStatus.Status.Canary.CurrentBackgroundAnalysisRunStatus
	newStatus.Canary.CurrentStepAnalysisRunStatus = rpWithNewStatus.Status.Canary.CurrentStepAnalysisRunStatus

	// Apply any pending pCtx state (e.g. progressDeadline abort) before status write
	pCtx.CalculatePauseStatus(newStatus)
	return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
}

// checkPausedConditions checks if the given RolloutPlugin is paused or not and adds an appropriate condition.
// These conditions are needed so that we won't accidentally report lack of progress for resumed rollouts
// that were paused for longer than progressDeadlineSeconds.
func checkPausedConditions(rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, logCtx *log.Entry) error {
	progCond := conditions.GetRolloutPluginCondition(*newStatus, v1alpha1.RolloutPluginConditionProgressing)
	progCondPaused := progCond != nil && progCond.Reason == conditions.RolloutPluginPausedReason

	// Paused state is determined by spec.Paused (manual) or PauseConditions (controller-set step pauses)
	isPaused := rolloutPlugin.Spec.Paused || len(newStatus.PauseConditions) > 0
	abortCondExists := progCond != nil && progCond.Reason == conditions.RolloutPluginAbortedReason
	completedCondExists := conditions.GetRolloutPluginCondition(*newStatus, v1alpha1.RolloutPluginConditionCompleted) != nil

	var updatedConditions []*v1alpha1.RolloutPluginCondition

	// Update Progressing condition when pause state changes only when there's an active rolloutplugin (not completed)
	// When paused, the Progressing condition is ConditionUnknown, and we need to be able to transition it back to ConditionTrue on resume.
	if (isPaused != progCondPaused) && !abortCondExists && !completedCondExists {
		if isPaused {
			// Set Progressing condition to Paused
			updatedConditions = append(updatedConditions,
				conditions.NewRolloutPluginCondition(
					v1alpha1.RolloutPluginConditionProgressing,
					corev1.ConditionUnknown,
					conditions.RolloutPluginPausedReason,
					conditions.RolloutPluginPausedMessage))
			logCtx.Debug("Setting Progressing condition to Paused")
		} else {
			// Set Progressing condition to Resumed with NEW timestamp.
			// The new LastUpdateTime resets the progressDeadlineSeconds timer.
			updatedConditions = append(updatedConditions,
				conditions.NewRolloutPluginCondition(
					v1alpha1.RolloutPluginConditionProgressing,
					corev1.ConditionTrue,
					conditions.RolloutPluginProgressingReason,
					"RolloutPlugin resumed"))
			logCtx.Debug("Setting Progressing condition to Resumed (resets timeout timer)")
		}
	}

	// Update Paused RolloutPluginCondition (different from PauseConditions)
	pauseCond := conditions.GetRolloutPluginCondition(*newStatus, v1alpha1.RolloutPluginConditionPaused)
	pausedCondTrue := pauseCond != nil && pauseCond.Status == corev1.ConditionTrue

	// isPaused uses PauseConditions (controller step pauses) and spec.Paused (manual)
	if (isPaused != pausedCondTrue) && !abortCondExists {
		condStatus := corev1.ConditionFalse
		if isPaused {
			condStatus = corev1.ConditionTrue
		}
		updatedConditions = append(updatedConditions,
			conditions.NewRolloutPluginCondition(
				v1alpha1.RolloutPluginConditionPaused,
				condStatus,
				conditions.RolloutPluginPausedReason,
				conditions.RolloutPluginPausedMessage))
	}

	if len(updatedConditions) == 0 {
		return nil
	}

	for _, condition := range updatedConditions {
		conditions.SetRolloutPluginCondition(newStatus, *condition)
	}

	return nil
}

// processRollout processes the rollout steps based on strategy
func (r *RolloutPluginReconciler) processRollout(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, plugin ResourcePlugin, workloadRef v1alpha1.WorkloadRef, pCtx *pauseContext, logCtx *log.Entry) (ctrl.Result, error) {
	strategy := rolloutPlugin.Spec.Strategy
	if strategy.Canary != nil {
		return r.processCanaryRollout(ctx, rolloutPlugin, newStatus, plugin, workloadRef, pCtx, logCtx)
	}

	logCtx.Info("No strategy defined")
	newStatus.Message = "No strategy defined"
	return ctrl.Result{}, nil
}

// processCanaryRollout processes a canary rollout
func (r *RolloutPluginReconciler) processCanaryRollout(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, plugin ResourcePlugin, workloadRef v1alpha1.WorkloadRef, pCtx *pauseContext, logCtx *log.Entry) (ctrl.Result, error) {
	canary := rolloutPlugin.Spec.Strategy.Canary
	if canary == nil || len(canary.Steps) == 0 {
		logCtx.Info("No canary steps defined")
		newStatus.Message = "No canary steps to execute"
		// Remove progressing condition since there are no steps to execute
		conditions.RemoveRolloutPluginCondition(newStatus, v1alpha1.RolloutPluginConditionProgressing)
		return ctrl.Result{}, nil
	}

	if newStatus.PromoteFull {
		logCtx.Info("PromoteFull is set, skipping remaining steps and promoting immediately")

		// Promote the rollout
		if err := plugin.PromoteFull(ctx, workloadRef); err != nil {
			logCtx.WithError(err).Error("Failed to promote during full promotion")
			newStatus.Message = fmt.Sprintf("Failed to promote: %v", err)
			return ctrl.Result{}, err
		}

		// Clear PromoteFull flag and pause state
		newStatus.PromoteFull = false
		pCtx.ClearPauseConditions()
		newStatus.PauseConditions = nil
		newStatus.ControllerPause = false

		// Advance step index to beyond last step so normal completion logic handles the rest.
		// Keep RolloutInProgress = true so the next reconcile doesn't mistake this for a new rollout
		stepCount := int32(len(canary.Steps))
		newStatus.CurrentStepIndex = &stepCount
		newStatus.Message = "Full promotion in progress, waiting for pods to converge"

		logCtx.Info("Full promotion initiated, waiting for convergence")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// Reconcile analysis runs
	rpWithNewStatus := rolloutPlugin.DeepCopy()
	rpWithNewStatus.Status = *newStatus

	allArs, err := r.getAnalysisRunsForRolloutPlugin(ctx, rpWithNewStatus)
	if err != nil {
		logCtx.WithError(err).Error("Failed to get analysis runs")
		return ctrl.Result{}, err
	}
	if err := r.reconcileAnalysisRuns(ctx, rpWithNewStatus, allArs, pCtx, logCtx); err != nil {
		logCtx.WithError(err).Error("Failed to reconcile analysis runs")
		return ctrl.Result{}, err
	}
	// Copy analysis status from rpWithNewStatus back to newStatus
	newStatus.Canary.CurrentBackgroundAnalysisRunStatus = rpWithNewStatus.Status.Canary.CurrentBackgroundAnalysisRunStatus
	newStatus.Canary.CurrentStepAnalysisRunStatus = rpWithNewStatus.Status.Canary.CurrentStepAnalysisRunStatus

	// Initialize step index if not set
	if newStatus.CurrentStepIndex == nil {
		stepIndex := int32(0)
		newStatus.CurrentStepIndex = &stepIndex
	}

	currentStepIndex := *newStatus.CurrentStepIndex
	if currentStepIndex >= int32(len(canary.Steps)) {
		// All steps completed (or PromoteFull advanced past last step).
		// Call Promote to finalize the rollout — what this means is plugin-defined
		// (e.g. set partition=0 for StatefulSet).
		// May be called more than once per rollout (e.g. while waiting for pods to converge).
		if err := plugin.PromoteFull(ctx, workloadRef); err != nil {
			logCtx.WithError(err).Error("Failed to promote")
			newStatus.Message = fmt.Sprintf("Failed to promote: %v", err)
			return ctrl.Result{}, err
		}

		// Without this check, setting RolloutInProgress=false while CurrentRevision != UpdatedRevision
		// causes the next reconcile to mistake the still-converging pods for a new rollout
		if newStatus.CurrentRevision != newStatus.UpdatedRevision {
			logCtx.WithFields(log.Fields{
				"currentRevision": newStatus.CurrentRevision,
				"updatedRevision": newStatus.UpdatedRevision,
			}).Info("Waiting for pods to converge before completing rollout")
			newStatus.Message = "Waiting for all pods to be updated"
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}

		logCtx.Info("All canary steps completed and pods converged, completing rollout")

		// Record completion event
		if r.Recorder != nil {
			r.Recorder.Eventf(rolloutPlugin, record.EventOptions{
				EventReason: conditions.RolloutPluginCompletedReason,
			}, "RolloutPlugin completed update to revision %s", newStatus.UpdatedRevision)
		}

		// Remove progressing condition — calculateRolloutPluginConditions will set Completed
		conditions.RemoveRolloutPluginCondition(newStatus, v1alpha1.RolloutPluginConditionProgressing)

		return ctrl.Result{}, nil
	}

	currentStep := canary.Steps[currentStepIndex]

	// Background analysis runs throughout the rollout, so we check it before processing each step.
	// Phase handling (abort/pause) is done inside reconcileBackgroundAnalysisRun via pCtx.
	// Here we only need to detect if pCtx triggered an abort and call the plugin + set conditions.
	if pCtx.IsAborted() && !rolloutPlugin.Status.Aborted {
		logCtx.Error("Analysis failed, aborting rollout")
		newStatus.Message = pCtx.abortMessage
		condition := conditions.NewRolloutPluginCondition(
			v1alpha1.RolloutPluginConditionProgressing,
			corev1.ConditionFalse,
			conditions.RolloutPluginAnalysisRunFailedReason,
			pCtx.abortMessage)
		conditions.SetRolloutPluginCondition(newStatus, *condition)
		if err := plugin.Abort(ctx, workloadRef); err != nil {
			logCtx.WithError(err).Error("Failed to abort rollout")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	if pCtx.HasAddPause() {
		// Analysis set a pause condition (inconclusive)
		return ctrl.Result{}, nil
	}

	// Process setWeight step
	if currentStep.SetWeight != nil {
		weight := *currentStep.SetWeight

		if err := plugin.SetWeight(ctx, workloadRef, weight); err != nil {
			logCtx.WithError(err).Error("Failed to set weight")
			newStatus.Message = fmt.Sprintf("Failed to set weight: %v", err)
			return ctrl.Result{}, err
		}

		verified, err := plugin.VerifyWeight(ctx, workloadRef, weight)
		if err != nil {
			logCtx.WithError(err).Error("Failed to verify weight")
			newStatus.Message = fmt.Sprintf("Failed to verify weight: %v", err)
			return ctrl.Result{}, err
		}

		if !verified {
			newStatus.Message = fmt.Sprintf("Waiting for weight %d to be verified", weight)
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}

		// Weight verified, move to next step
		newStatus.Message = fmt.Sprintf("Weight set to %d and verified", weight)
		nextStep := currentStepIndex + 1
		newStatus.CurrentStepIndex = &nextStep

		logCtx.WithField("nextStep", nextStep).Info("Weight verified, moving to next step")
		// Requeue immediately
		return ctrl.Result{Requeue: true}, nil
	}

	// Handle analysis step
	if currentStep.Analysis != nil {
		logCtx.Info("Handling analysis step")

		// CurrentStepAnalysisRunStatus is populated by reconcileAnalysisRuns, which runs
		// earlier in the same reconcile.
		if newStatus.Canary.CurrentStepAnalysisRunStatus == nil {
			logCtx.Info("Step analysis run not yet created, requeueing")
			newStatus.Message = fmt.Sprintf("Waiting for analysis run to be created at step %d", currentStepIndex)
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}

		analysisStatus := newStatus.Canary.CurrentStepAnalysisRunStatus.Status
		logCtx.WithFields(log.Fields{
			"status":      analysisStatus,
			"analysisRun": newStatus.Canary.CurrentStepAnalysisRunStatus.Name,
		}).Info("Step analysis status")

		switch analysisStatus {
		case v1alpha1.AnalysisPhaseSuccessful:
			logCtx.Info("Step analysis completed successfully, moving to next step")
			nextStep := currentStepIndex + 1
			newStatus.CurrentStepIndex = &nextStep
			newStatus.Message = "Analysis successful"
			return ctrl.Result{Requeue: true}, nil

		case v1alpha1.AnalysisPhaseRunning, v1alpha1.AnalysisPhasePending, "":
			newStatus.Message = fmt.Sprintf("Running analysis: %s", newStatus.Canary.CurrentStepAnalysisRunStatus.Name)
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil

		default:
			// Failed/Error/Inconclusive are already handled by reconcileStepBasedAnalysisRun via pCtx
			// and caught by the abort/pause check above. If we reach here, it means the phase just
			// changed and pCtx was set — return and let CalculatePauseStatus handle it.
			newStatus.Message = fmt.Sprintf("Analysis phase: %s", analysisStatus)
			return ctrl.Result{}, nil
		}
	}

	//Controller sets PauseConditions + ControllerPause=true when pausing
	//User clears PauseConditions to resume (via ArgoCD Resource Action / kubectl)
	//Controller detects ControllerPause=true && PauseConditions=nil → means pause step is completed
	if currentStep.Pause != nil {
		logCtx.Info("Processing pause step")

		if pCtx.CompletedCanaryPauseStep(*currentStep.Pause) {
			// Pause step completed (user promoted or duration elapsed)

			// Record resume event
			if r.Recorder != nil {
				r.Recorder.Eventf(rolloutPlugin, record.EventOptions{
					EventReason: conditions.RolloutPluginResumedReason,
				}, "RolloutPlugin resumed from pause at step %d", currentStepIndex)
			}

			// Clear pause state and advance to next step
			pCtx.ClearPauseConditions()
			newStatus.ControllerPause = false
			newStatus.PauseConditions = nil

			nextStep := currentStepIndex + 1
			newStatus.CurrentStepIndex = &nextStep
			logCtx.WithFields(log.Fields{
				"fromStep": currentStepIndex,
				"toStep":   nextStep,
			}).Info("Advancing past pause step")
			return ctrl.Result{Requeue: true}, nil
		}

		pauseCondition := getRolloutPluginPauseCondition(rolloutPlugin, v1alpha1.PauseReasonCanaryPauseStep)
		if pauseCondition == nil {
			pCtx.AddPauseCondition(v1alpha1.PauseReasonCanaryPauseStep)
			newStatus.Message = "Paused"

			// Record pause event (step pause)
			if r.Recorder != nil {
				r.Recorder.Eventf(rolloutPlugin, record.EventOptions{
					EventReason: conditions.RolloutPluginPausedReason,
				}, "RolloutPlugin paused at step %d", currentStepIndex)
			}

			logCtx.WithField("duration", currentStep.Pause.Duration).Info("Starting pause")
			return ctrl.Result{}, nil
		}

		// Pause condition exists but not yet completed — check for timed requeue
		if currentStep.Pause.Duration != nil {
			durationStr := currentStep.Pause.Duration.String()
			duration, err := time.ParseDuration(durationStr)
			if err != nil {
				logCtx.WithError(err).WithField("duration", durationStr).Error("Failed to parse pause duration")
				newStatus.Message = fmt.Sprintf("Invalid pause duration: %v", err)
				return ctrl.Result{}, err
			}

			elapsed := time.Since(pauseCondition.StartTime.Time)
			remaining := duration - elapsed
			newStatus.Message = fmt.Sprintf("Paused (remaining: %s)", remaining.Round(time.Second))
			return ctrl.Result{RequeueAfter: remaining}, nil
		}

		// Indefinite pause - wait for manual promotion (user clears pauseConditions).
		// the watch on RolloutPlugin status changes should trigger reconciliation when the user promotes, but this provides a safety net.
		logCtx.Info("Rollout is paused indefinitely, waiting for manual promotion")
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	// Step has no recognized type (setWeight, analysis, pause) — could be an empty step or a
	// future step type added in a newer CRD version. Skip and advance for forward compatibility.
	logCtx.WithField("stepIndex", currentStepIndex).Warn("Step has no recognized action, skipping to next step")
	nextStep := currentStepIndex + 1
	newStatus.CurrentStepIndex = &nextStep
	newStatus.Message = fmt.Sprintf("Completed step %d", currentStepIndex)

	// Requeue immediately
	return ctrl.Result{Requeue: true}, nil
}

func (r *RolloutPluginReconciler) processRestart(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, plugin ResourcePlugin, workloadRef v1alpha1.WorkloadRef, logCtx *log.Entry) (ctrl.Result, error) {
	logCtx.WithFields(log.Fields{"attempt": newStatus.RestartCount + 1}).Info("Processing rollout restart from step 0")

	if !newStatus.Aborted {
		logCtx.WithFields(log.Fields{
			"phase":   newStatus.Phase,
			"aborted": newStatus.Aborted,
		}).Info("Cannot restart: rollout has not been aborted")

		newStatus.Message = "Cannot restart: rollout has not been aborted. Restart is only allowed after aborting the rollout."

		// Clear Restart flag to prevent loop
		newStatus.Restart = false

		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	// Call plugin Restart() to return workload to baseline
	if err := plugin.Restart(ctx, workloadRef); err != nil {
		logCtx.WithError(err).Error("Plugin restart failed")
		newStatus.Message = fmt.Sprintf("Restart failed: plugin restart error: %v", err)

		// Clear Restart flag to prevent loop
		newStatus.Restart = false

		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	newStatus.RestartCount++
	now := metav1.Now()
	newStatus.RestartedAt = &now

	stepZero := int32(0)
	newStatus.CurrentStepIndex = &stepZero
	newStatus.PauseConditions = nil
	newStatus.ControllerPause = false
	newStatus.Aborted = false
	newStatus.Abort = false
	newStatus.Message = fmt.Sprintf("Restart attempt %d: restarting from step 0", newStatus.RestartCount)

	condition := conditions.NewRolloutPluginCondition(
		v1alpha1.RolloutPluginConditionProgressing,
		corev1.ConditionTrue,
		conditions.RolloutPluginRestartedReason,
		fmt.Sprintf("Rollout restarted from step 0 (attempt %d)", newStatus.RestartCount))
	conditions.SetRolloutPluginCondition(newStatus, *condition)

	// Clear Restart flag
	newStatus.Restart = false

	if err := r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx); err != nil {
		return ctrl.Result{}, err
	}

	logCtx.WithFields(log.Fields{
		"attempt":      newStatus.RestartCount,
		"startingStep": 0,
	}).Info("Restart processed successfully, restarting from step 0")

	// Requeue immediately to process the restart
	return ctrl.Result{Requeue: true}, nil
}

// calculateFinalRolloutPluginConditions handles Healthy and Completed condition management at the end of every reconciliation
func calculateFinalRolloutPluginConditions(rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus) bool {
	isPaused := rolloutPlugin.Spec.Paused || len(newStatus.PauseConditions) > 0
	var condChanged bool

	existingHealthyCond := conditions.GetRolloutPluginCondition(rolloutPlugin.Status, v1alpha1.RolloutPluginConditionHealthy)
	if !isPaused && conditions.RolloutPluginIsHealthy(rolloutPlugin, newStatus) {
		healthyCond := conditions.NewRolloutPluginCondition(
			v1alpha1.RolloutPluginConditionHealthy,
			corev1.ConditionTrue,
			conditions.RolloutPluginHealthyReason,
			conditions.RolloutPluginHealthyMessage)
		condChanged = conditions.SetRolloutPluginCondition(newStatus, *healthyCond) || condChanged
	} else if existingHealthyCond != nil {
		unhealthyCond := conditions.NewRolloutPluginCondition(
			v1alpha1.RolloutPluginConditionHealthy,
			corev1.ConditionFalse,
			conditions.RolloutPluginHealthyReason,
			conditions.RolloutPluginNotHealthyMessage)
		condChanged = conditions.SetRolloutPluginCondition(newStatus, *unhealthyCond) || condChanged
	}

	if !conditions.IsRolloutPluginProgressing(newStatus) && newStatus.CurrentRevision != "" && newStatus.CurrentRevision == newStatus.UpdatedRevision {
		completedCond := conditions.NewRolloutPluginCondition(
			v1alpha1.RolloutPluginConditionCompleted,
			corev1.ConditionTrue,
			conditions.RolloutPluginCompletedReason,
			conditions.RolloutPluginCompletedMessage)
		condChanged = conditions.SetRolloutPluginCondition(newStatus, *completedCond) || condChanged
	} else {
		existingCompletedCond := conditions.GetRolloutPluginCondition(rolloutPlugin.Status, v1alpha1.RolloutPluginConditionCompleted)
		if existingCompletedCond != nil && existingCompletedCond.Status == corev1.ConditionTrue {
			notCompletedCond := conditions.NewRolloutPluginCondition(
				v1alpha1.RolloutPluginConditionCompleted,
				corev1.ConditionFalse,
				conditions.RolloutPluginNotCompletedReason,
				fmt.Sprintf(conditions.RolloutPluginNotCompletedMessage, newStatus.UpdatedRevision))
			condChanged = conditions.SetRolloutPluginCondition(newStatus, *notCompletedCond) || condChanged
		}
	}
	return condChanged
}

// validateReferencedResources fetches and validates analysis templates referenced by the RolloutPlugin.
func (r *RolloutPluginReconciler) validateReferencedResources(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, logCtx *log.Entry) error {
	canary := rolloutPlugin.Spec.Strategy.Canary
	if canary == nil {
		return nil
	}

	var allTemplatesWithType []validation.AnalysisTemplatesWithType

	for i, step := range canary.Steps {
		if step.Analysis != nil {
			templatesWithType, err := r.fetchAnalysisTemplates(ctx, rolloutPlugin, step.Analysis, validation.InlineAnalysis, i)
			if err != nil {
				logCtx.WithError(err).Error("Failed to fetch step analysis templates")
				msg := fmt.Sprintf("Failed to fetch analysis templates for step %d: %v", i, err)
				invalidCond := conditions.NewRolloutPluginCondition(
					v1alpha1.RolloutPluginConditionInvalidSpec,
					corev1.ConditionTrue,
					conditions.RolloutPluginInvalidSpecReason,
					msg)
				conditions.SetRolloutPluginCondition(newStatus, *invalidCond)
				newStatus.Message = msg
				return fmt.Errorf("%s", msg)
			}
			allTemplatesWithType = append(allTemplatesWithType, *templatesWithType)
		}
	}

	if canary.Analysis != nil {
		templatesWithType, err := r.fetchAnalysisTemplates(ctx, rolloutPlugin, &canary.Analysis.RolloutAnalysis, validation.BackgroundAnalysis, 0)
		if err != nil {
			logCtx.WithError(err).Error("Failed to fetch background analysis templates")
			msg := fmt.Sprintf("Failed to fetch background analysis templates: %v", err)
			invalidCond := conditions.NewRolloutPluginCondition(
				v1alpha1.RolloutPluginConditionInvalidSpec,
				corev1.ConditionTrue,
				conditions.RolloutPluginInvalidSpecReason,
				msg)
			conditions.SetRolloutPluginCondition(newStatus, *invalidCond)
			newStatus.Message = msg
			return fmt.Errorf("%s", msg)
		}
		allTemplatesWithType = append(allTemplatesWithType, *templatesWithType)
	}

	if len(allTemplatesWithType) == 0 {
		return nil
	}

	refs := validation.ReferencedRolloutPluginResources{
		AnalysisTemplatesWithType: allTemplatesWithType,
	}
	errs := validation.ValidateRolloutPluginReferencedResources(rolloutPlugin, refs)
	if len(errs) > 0 {
		msg := errs[0].Error()
		logCtx.WithField("error", msg).Warn("Reference validation failed")
		invalidCond := conditions.NewRolloutPluginCondition(
			v1alpha1.RolloutPluginConditionInvalidSpec,
			corev1.ConditionTrue,
			conditions.RolloutPluginInvalidSpecReason,
			msg)
		conditions.SetRolloutPluginCondition(newStatus, *invalidCond)
		newStatus.Message = msg
		if r.Recorder != nil {
			r.Recorder.Warnf(rolloutPlugin, record.EventOptions{
				EventReason: conditions.RolloutPluginInvalidSpecReason,
			}, msg)
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// fetchAnalysisTemplates fetches analysis templates referenced by a RolloutAnalysis spec.
func (r *RolloutPluginReconciler) fetchAnalysisTemplates(ctx context.Context, rp *v1alpha1.RolloutPlugin, analysisSpec *v1alpha1.RolloutAnalysis, templateType validation.AnalysisTemplateType, stepIndex int) (*validation.AnalysisTemplatesWithType, error) {
	templates := make([]*v1alpha1.AnalysisTemplate, 0)
	clusterTemplates := make([]*v1alpha1.ClusterAnalysisTemplate, 0)

	for _, templateRef := range analysisSpec.Templates {
		if templateRef.ClusterScope == nil || !*templateRef.ClusterScope {
			template, err := r.ArgoProjClientset.ArgoprojV1alpha1().AnalysisTemplates(rp.Namespace).Get(ctx, templateRef.TemplateName, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("AnalysisTemplate '%s' not found: %w", templateRef.TemplateName, err)
			}
			templates = append(templates, template)
		} else {
			clusterTemplate, err := r.ArgoProjClientset.ArgoprojV1alpha1().ClusterAnalysisTemplates().Get(ctx, templateRef.TemplateName, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("ClusterAnalysisTemplate '%s' not found: %w", templateRef.TemplateName, err)
			}
			clusterTemplates = append(clusterTemplates, clusterTemplate)
		}
	}

	return &validation.AnalysisTemplatesWithType{
		AnalysisTemplates:        templates,
		ClusterAnalysisTemplates: clusterTemplates,
		TemplateType:             templateType,
		CanaryStepIndex:          stepIndex,
		Args:                     analysisSpec.Args,
	}, nil
}

// updateStatus updates the status of the RolloutPlugin.
func (r *RolloutPluginReconciler) updateStatus(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, logCtx *log.Entry) error {
	// Calculate conditions based on final state
	condChanged := calculateFinalRolloutPluginConditions(rolloutPlugin, newStatus)

	// Derive phase and message from  calculated conditions
	newStatus.Phase, newStatus.Message = rolloututil.CalculateRolloutPluginPhase(rolloutPlugin.Spec, *newStatus)

	patch := client.MergeFrom(rolloutPlugin.DeepCopy())
	rolloutPlugin.Status = *newStatus

	if err := r.Status().Patch(ctx, rolloutPlugin, patch); err != nil {
		logCtx.WithError(err).Error("Failed to update status")
		return err
	}

	if condChanged {
		logCtx.Debug("Conditions changed during reconciliation")
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RolloutPluginReconciler) SetupWithManager(mgr ctrl.Manager) error {

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
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
	}

	analysisRunPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldAR, ok1 := e.ObjectOld.(*v1alpha1.AnalysisRun)
			newAR, ok2 := e.ObjectNew.(*v1alpha1.AnalysisRun)
			if !ok1 || !ok2 {
				return false
			}

			// Only trigger if phase changed
			if oldAR.Status.Phase != newAR.Status.Phase {
				return true
			}

			// Skip other updates
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.RolloutPlugin{}, builder.WithPredicates(rolloutPluginPredicate)).
		Owns(&v1alpha1.AnalysisRun{}, builder.WithPredicates(analysisRunPredicate)).
		Watches(
			&appsv1.StatefulSet{},
			handler.EnqueueRequestsFromMapFunc(r.findRolloutPluginsForWorkload),
			builder.WithPredicates(statefulSetPredicate),
		).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 10, // TODOH make it user Configurable
		}).
		Complete(r)
}

// findRolloutPluginsForWorkload maps a workload (StatefulSet, DaemonSet, Deployment, etc.) to RolloutPlugin CRs that reference it
// This is kind-agnostic
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

// verifyRolloutPluginSpec validates the RolloutPlugin spec and returns an InvalidSpec condition if invalid.
func verifyRolloutPluginSpec(rolloutPlugin *v1alpha1.RolloutPlugin, prevCond *v1alpha1.RolloutPluginCondition) *v1alpha1.RolloutPluginCondition {
	msg := validation.ValidateRolloutPlugin(rolloutPlugin)
	if msg == "" {
		return nil
	}
	if prevCond != nil && prevCond.Message == msg {
		prevCond.LastUpdateTime = metav1.Now()
		return prevCond
	}
	return conditions.NewRolloutPluginCondition(v1alpha1.RolloutPluginConditionInvalidSpec, corev1.ConditionTrue, conditions.RolloutPluginInvalidSpecReason, msg)
}
