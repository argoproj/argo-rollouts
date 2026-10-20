package rolloutplugin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	plugintypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/argoproj/argo-rollouts/utils/record"
	"github.com/argoproj/argo-rollouts/utils/resourceversion"
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

	// workloadKinds maps each enabled plugin's name to the workload kind it manages.
	workloadKinds map[string]schema.GroupVersionKind

	// versionTrackerOnce lazily creates versionTracker, so reconcilers built as struct
	// literals get it without a constructor.
	versionTrackerOnce sync.Once
	versionTracker     *resourceversion.Tracker
}

type PluginManager interface {
	GetPlugin(name string) (ResourcePlugin, error)
	// IsBuiltinEnabled reports whether the built-in plugin with the given id
	// (the builtin://<id> host, e.g. "statefulset") was enabled in the ConfigMap.
	IsBuiltinEnabled(id string) bool
	// EnabledPlugins returns every plugin enabled in the resourcePlugins ConfigMap (builtin or
	// external), keyed by the name a RolloutPlugin references via spec.plugin.name.
	EnabledPlugins() (map[string]ResourcePlugin, error)
}

// ResourcePlugin is the interface that all resource plugins must implement.
// Built-in plugins implement this interface directly.
// External RPC plugins implement types.RpcResourcePlugin, which is adapted via RpcPluginWrapper.
type ResourcePlugin interface {
	// Init initializes the plugin. It is called once, only after this controller replica becomes
	// the leader, so it is where a plugin should create its clients and start any watches;
	// WatchedGVK may be called before it. ctx is cancelled if leadership is lost. namespace is the
	// controller's watch namespace (empty/metav1.NamespaceAll for cluster-wide, or a specific
	// namespace in --namespaced mode).
	Init(ctx context.Context, namespace string) error

	// WatchedGVK declares the workload resource kind this plugin manages, so
	// SetupWithManager can register a manager-level watch for it deterministically at
	// startup.
	WatchedGVK() (schema.GroupVersionKind, error)

	// The workload methods below receive the whole RolloutPlugin, so a plugin can read the
	// namespace, spec.workloadRef and its per-RolloutPlugin spec.plugin.config. Plugins must
	// treat it as read-only. GetResourceStatus reports the workload resourceVersion it read.

	// Validate checks the plugin-specific parts of the spec
	// An error marks the RolloutPlugin InvalidSpec with its message and
	// no other workload method is called until it passes.
	Validate(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error

	// GetResourceStatus gets the current status of the referenced workload
	GetResourceStatus(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) (*ResourceStatus, error)

	// SetWeight updates the weight (percentage of pods updated)
	SetWeight(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, weight int32) error

	// VerifyWeight checks if the desired weight has been achieved
	VerifyWeight(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, weight int32) (bool, error)

	// PromoteFull promotes the new version to stable
	PromoteFull(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error

	// Abort aborts the rollout and reverts to the stable version.
	Abort(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error

	// Restart returns the workload to baseline state for restart
	Restart(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error

	// DeletionCleanup is called when the RolloutPlugin is deleted, and retried until it succeeds
	// since the deletion waits for it. It must leave the workload's spec as it is and only drop the
	// plugin's claim on it (e.g. its Server-Side Apply field ownership), so another owner such as
	// Argo CD sees the values the plugin left as drift. It is called even if Validate never
	// passed, so a workloadRef the plugin doesn't manage, or a missing workload, isn't an error and doesn't block deletion.
	DeletionCleanup(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error
}

// DeletionCleanupFinalizer keeps a deleted RolloutPlugin until its plugin's DeletionCleanup succeeds.
const DeletionCleanupFinalizer = "rolloutplugin.argoproj.io/deletion-cleanup"

// abortRollback calls plugin.Abort (which may only complete one unit of work, e.g. one pod of a
// multi-replica StatefulSet rollback) and reports whether the rollback has fully converged, along
// with the revision being rolled back.
func abortRollback(ctx context.Context, plugin ResourcePlugin, rolloutPlugin *v1alpha1.RolloutPlugin) (bool, string, error) {
	if err := plugin.Abort(ctx, rolloutPlugin); err != nil {
		return false, "", err
	}
	resourceStatus, err := plugin.GetResourceStatus(ctx, rolloutPlugin)
	if err != nil {
		return false, "", err
	}
	return resourceStatus.UpdatedReplicas == 0 && resourceStatus.Ready, resourceStatus.UpdatedRevision, nil
}

// reconcileAbort drives an abort. userRequested distinguishes status.abort from aborts the
// controller triggers itself (failed analysis, timeoutAbort): a user abort with nothing to roll
// back is ignored, while a controller abort is still recorded so the rollout ends Degraded.
func (r *RolloutPluginReconciler) reconcileAbort(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, plugin ResourcePlugin, reason, abortedMessage, inProgressMessage string, userRequested bool, logCtx *log.Entry) (done bool, result ctrl.Result, err error) {
	// Every pod already runs the updated revision (e.g. after setWeight: 100 or with a single
	// replica): there is no older revision left to roll back to.
	// Readiness is not required: a not-Ready pod on the updated revision (e.g. crash-looping)
	// would otherwise make abortRollback wait forever for UpdatedReplicas to reach 0.
	nothingToRollBack, nothingToRollBackRevision := "", ""
	if resourceStatus, statusErr := plugin.GetResourceStatus(ctx, rolloutPlugin); statusErr == nil && resourceStatus != nil &&
		resourceStatus.CurrentRevision != "" && resourceStatus.CurrentRevision == resourceStatus.UpdatedRevision {
		nothingToRollBack = fmt.Sprintf("nothing to roll back: all pods already run revision %s", resourceStatus.CurrentRevision)
		nothingToRollBackRevision = resourceStatus.UpdatedRevision
	}
	if nothingToRollBack != "" && userRequested {
		logCtx.Warn("Abort ignored, " + nothingToRollBack)
		newStatus.Abort = false
		newStatus.Message = "Abort ignored: " + nothingToRollBack
		if r.Recorder != nil {
			r.Recorder.Warnf(rolloutPlugin, record.EventOptions{
				EventReason: conditions.RolloutPluginAbortIgnoredReason,
			}, newStatus.Message)
		}
		return true, ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	// An aborted rollout runs no analysis: terminate the step and background
	// AnalysisRuns now instead of leaving them running (unwatched) until they complete.
	if err := r.cancelAllAnalysisRuns(ctx, rolloutPlugin, logCtx); err != nil {
		logCtx.WithError(err).Error("Failed to cancel analysis runs for abort")
		return false, ctrl.Result{}, err
	}

	if nothingToRollBack != "" {
		r.markAborted(rolloutPlugin, newStatus, reason, fmt.Sprintf("%s (%s)", abortedMessage, nothingToRollBack), nothingToRollBackRevision, logCtx)
		return true, ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	doneRollback, abortedRevision, abortErr := abortRollback(ctx, plugin, rolloutPlugin)
	if abortErr != nil {
		logCtx.WithError(abortErr).Error("Failed to abort rollout")
		newStatus.Message = fmt.Sprintf("Failed to abort rollout: %v", abortErr)
		condition := conditions.NewRolloutPluginCondition(
			v1alpha1.RolloutPluginConditionProgressing,
			corev1.ConditionFalse,
			conditions.RolloutPluginReconciliationErrorReason,
			newStatus.Message)
		conditions.SetRolloutPluginCondition(newStatus, *condition)
		return false, ctrl.Result{RequeueAfter: 1 * time.Second}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}
	if !doneRollback {
		newStatus.Message = inProgressMessage
		return false, ctrl.Result{RequeueAfter: 1 * time.Second}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	r.markAborted(rolloutPlugin, newStatus, reason, abortedMessage, abortedRevision, logCtx)
	return true, ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
}

// markAborted persists a confirmed-complete abort: sets Aborted/AbortedAt/AbortedRevision,
// clears pause state, sets the
// Degraded-causing Progressing condition, and emits the abort event. abortedRevision comes from
// the workload rather than newStatus, which can still hold the previous revision when the abort
// is processed before a new revision was recorded.
func (r *RolloutPluginReconciler) markAborted(rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, reason, message, abortedRevision string, logCtx *log.Entry) {
	now := timeutil.MetaNow()
	newAbortedAt := rolloutPlugin.Status.AbortedAt
	if newAbortedAt == nil {
		newAbortedAt = &now
	}

	newStatus.Aborted = true
	newStatus.AbortedAt = newAbortedAt
	newStatus.AbortedRevision = abortedRevision
	newStatus.Abort = false
	newStatus.PauseConditions = nil
	newStatus.ControllerPause = false
	newStatus.Message = message

	condition := conditions.NewRolloutPluginCondition(
		v1alpha1.RolloutPluginConditionProgressing,
		corev1.ConditionFalse,
		reason,
		message)
	conditions.SetRolloutPluginCondition(newStatus, *condition)

	if r.Recorder != nil {
		r.Recorder.Warnf(rolloutPlugin, record.EventOptions{
			EventReason: conditions.RolloutPluginAbortedReason,
		}, message)
	}
	logCtx.Info("Rollout aborted successfully")
}

// clearAbortedState lifts a persisted abort.
func clearAbortedState(newStatus *v1alpha1.RolloutPluginStatus) {
	newStatus.Aborted = false
	newStatus.AbortedAt = nil
	newStatus.AbortedRevision = ""
}

// clearCurrentAnalysisRuns drops the current step/background AnalysisRun references so the next
// analysis creates fresh runs. The old runs become "other" runs and are cancelled.
func clearCurrentAnalysisRuns(newStatus *v1alpha1.RolloutPluginStatus) {
	newStatus.Canary.CurrentStepAnalysisRunStatus = nil
	newStatus.Canary.CurrentBackgroundAnalysisRunStatus = nil
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
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

// Reconcile aims to move the current state of the RolloutPlugin closer to the desired state.
func (r *RolloutPluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := timeutil.Now()
	logCtx := log.WithFields(log.Fields{"namespace": req.Namespace, "rolloutplugin": req.Name})
	key := req.String()

	// Fetch the RolloutPlugin instance
	rolloutPlugin := &v1alpha1.RolloutPlugin{}
	if err := r.Get(ctx, req.NamespacedName, rolloutPlugin); err != nil {
		if apierrors.IsNotFound(err) {
			r.getVersionTracker().Forget(key)
			if r.MetricsServer != nil {
				r.MetricsServer.Remove(req.Namespace, req.Name, logutil.RolloutPluginKey)
			}
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// The cache can lag our own writes, and acting on that stale status repeats side effects
	// (e.g. creating a duplicate AnalysisRun), so wait for it to catch up.
	if r.getVersionTracker().IsCacheStale(key, rolloutPlugin.ResourceVersion) {
		logCtx.Infof("requeuing RolloutPlugin due to stale cache after %s", controllerutil.StaleCacheRequeueDelay)
		return ctrl.Result{RequeueAfter: controllerutil.StaleCacheRequeueDelay}, nil
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
		return r.handleDeletion(ctx, rolloutPlugin, logCtx)
	}

	if !slices.Contains(rolloutPlugin.Finalizers, DeletionCleanupFinalizer) {
		if err := r.setFinalizers(ctx, rolloutPlugin, append(slices.Clone(rolloutPlugin.Finalizers), DeletionCleanupFinalizer)); err != nil {
			logCtx.WithError(err).Error("Failed to add finalizer")
			return ctrl.Result{}, err
		}
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

// handleDeletion runs the plugin's DeletionCleanup, then removes the finalizer so the
// RolloutPlugin is deleted. Its metrics are removed once it is gone.
func (r *RolloutPluginReconciler) handleDeletion(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, logCtx *log.Entry) (ctrl.Result, error) {
	if !slices.Contains(rolloutPlugin.Finalizers, DeletionCleanupFinalizer) {
		return ctrl.Result{}, nil
	}
	logCtx.Info("Handling deletion of RolloutPlugin")

	plugin, err := r.PluginManager.GetPlugin(rolloutPlugin.Spec.Plugin.Name)
	if err == nil {
		err = plugin.DeletionCleanup(ctx, rolloutPlugin)
	} else if errors.Is(err, ErrPluginNotEnabled) {
		// A plugin that isn't enabled can't clean up the workload, and won't touch it again.
		logCtx.WithError(err).Warn("Skipping deletion cleanup")
		err = nil
	}
	if err != nil {
		msg := fmt.Sprintf("Deletion cleanup failed, deletion will be retried: %v", err)
		logCtx.Warn(msg)
		if r.Recorder != nil {
			r.Recorder.Warnf(rolloutPlugin, record.EventOptions{EventReason: "DeletionCleanupFailed"}, msg)
		}
		return ctrl.Result{}, err
	}

	finalizers := slices.DeleteFunc(slices.Clone(rolloutPlugin.Finalizers), func(f string) bool { return f == DeletionCleanupFinalizer })
	if err := r.setFinalizers(ctx, rolloutPlugin, finalizers); err != nil {
		logCtx.WithError(err).Error("Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	if r.Recorder != nil {
		r.Recorder.Eventf(rolloutPlugin, record.EventOptions{
			EventReason: "RolloutPluginDeleted",
		}, "RolloutPlugin '%s' deleted", rolloutPlugin.Name)
	}
	return ctrl.Result{}, nil
}

// setFinalizers patches the RolloutPlugin's finalizers. The optimistic lock keeps a patch built
// from a stale read from dropping a finalizer someone else added.
func (r *RolloutPluginReconciler) setFinalizers(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, finalizers []string) error {
	patch := client.MergeFromWithOptions(rolloutPlugin.DeepCopy(), client.MergeFromWithOptimisticLock{})
	rolloutPlugin.Finalizers = finalizers
	if err := r.Patch(ctx, rolloutPlugin, patch); err != nil {
		return err
	}
	r.recordWrite(rolloutPlugin)
	return nil
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

	if refErr := r.validateReferencedResources(ctx, rolloutPlugin, newStatus, logCtx); refErr != nil {
		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	// Get the plugin from the singleton PluginManager
	// Plugins are registered at startup and shared across all reconcilers
	plugin, err := r.PluginManager.GetPlugin(rolloutPlugin.Spec.Plugin.Name)
	if err != nil {
		logCtx.WithError(err).Error("Failed to get plugin")
		if !errors.Is(err, ErrPluginNotEnabled) {
			// An enabled plugin that failed to start (e.g. its process crashed) may recover, so
			// retry instead of marking the spec invalid.
			newStatus.Message = fmt.Sprintf("Failed to get plugin: %v", err)
			condition := conditions.NewRolloutPluginCondition(
				v1alpha1.RolloutPluginConditionProgressing,
				corev1.ConditionFalse,
				conditions.RolloutPluginReconciliationErrorReason,
				newStatus.Message)
			conditions.SetRolloutPluginCondition(newStatus, *condition)
			return ctrl.Result{RequeueAfter: 1 * time.Second}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
		}
		newStatus.Message = fmt.Sprintf("Plugin '%s' not found. Ensure the plugin is registered in the argo-rollouts-config ConfigMap under 'resourcePlugins'", rolloutPlugin.Spec.Plugin.Name)
		pluginNotFoundCond := conditions.NewRolloutPluginCondition(
			v1alpha1.RolloutPluginConditionInvalidSpec,
			corev1.ConditionTrue,
			conditions.RolloutPluginInvalidSpecReason,
			newStatus.Message,
		)
		conditions.SetRolloutPluginCondition(newStatus, *pluginNotFoundCond)
		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	err = plugin.Validate(ctx, rolloutPlugin)
	var workloadKind schema.GroupVersionKind
	if err == nil {
		workloadKind, err = r.workloadKindFor(rolloutPlugin, plugin)
	}
	if err != nil {
		msg := fmt.Sprintf("Plugin '%s' rejected the spec: %s", rolloutPlugin.Spec.Plugin.Name, err.Error())
		logCtx.WithField("reason", msg).Warn("RolloutPlugin spec validation by plugin failed")
		// Reuse the previous condition when unchanged so re-validation doesn't churn its timestamps.
		invalidCond := prevInvalidSpecCond
		if invalidCond == nil || invalidCond.Message != msg {
			invalidCond = conditions.NewRolloutPluginCondition(v1alpha1.RolloutPluginConditionInvalidSpec, corev1.ConditionTrue, conditions.RolloutPluginInvalidSpecReason, msg)
			if r.Recorder != nil {
				r.Recorder.Warnf(rolloutPlugin, record.EventOptions{EventReason: conditions.RolloutPluginInvalidSpecReason}, msg)
			}
		}
		newStatus.Message = msg
		conditions.SetRolloutPluginCondition(newStatus, *invalidCond)
		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	if prevInvalidSpecCond != nil {
		logCtx.Info("RolloutPlugin spec is now valid, removing InvalidSpec condition")
		conditions.RemoveRolloutPluginCondition(newStatus, v1alpha1.RolloutPluginConditionInvalidSpec)
		// Time spent waiting for the spec to be fixed isn't a lack of progress, so restart the
		// timeoutSeconds clock like a resume does; otherwise fixing the spec immediately times out.
		conditions.TouchRolloutPluginProgressingCondition(newStatus)
	}

	workload, err := r.getWorkload(ctx, rolloutPlugin, workloadKind)
	if err != nil {
		logCtx.WithError(err).Error("Failed to get workload")
		newStatus.Message = fmt.Sprintf("Failed to get workload: %v", err)
		condition := conditions.NewRolloutPluginCondition(
			v1alpha1.RolloutPluginConditionProgressing,
			corev1.ConditionFalse,
			conditions.RolloutPluginReconciliationErrorReason,
			newStatus.Message)
		conditions.SetRolloutPluginCondition(newStatus, *condition)
		return ctrl.Result{RequeueAfter: 1 * time.Second}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	// The plugin reads the workload from its own informer, which can lag the informer whose
	// event triggered this reconcile. Deciding from that older view could miss the change and
	// leave the rollout waiting for an event that has already fired, so wait for it to catch up.
	if status, statusErr := plugin.GetResourceStatus(ctx, rolloutPlugin); statusErr == nil && pluginViewIsBehind(status, workload) {
		logCtx.WithFields(log.Fields{
			"observedResourceVersion": status.ObservedResourceVersion,
			"workloadResourceVersion": workload.GetResourceVersion(),
		}).Infof("requeuing RolloutPlugin after %s: plugin has not observed the latest workload yet", controllerutil.StaleCacheRequeueDelay)
		return ctrl.Result{RequeueAfter: controllerutil.StaleCacheRequeueDelay}, nil
	}

	if newStatus.Restart && newStatus.Aborted {
		return r.processRestart(ctx, rolloutPlugin, newStatus, plugin, logCtx)
	}

	if newStatus.Restart && !newStatus.Aborted {
		if newStatus.Abort {
			// Abort requested but Aborted not yet persisted. Keep the restart request;
			// the abort block below sets Aborted=true, and the next reconcile takes the
			// `Restart && Aborted` path into processRestart.
			logCtx.Info("Restart requested while abort is in-flight; deferring until abort completes")
		} else {
			logCtx.Warn("Restart rejected: rollout has not been aborted")
			newStatus.Restart = false
		}
	}

	if newStatus.PromoteFull {
		logCtx.Info("PromoteFull detected, clearing pause state")
		// promoteFull overrides an abort or a timeout.
		progCond := conditions.GetRolloutPluginCondition(*newStatus, v1alpha1.RolloutPluginConditionProgressing)
		timedOut := progCond != nil && progCond.Reason == conditions.RolloutPluginTimedOutReason
		if (newStatus.Abort || newStatus.Aborted || timedOut) && newStatus.CurrentRevision != newStatus.UpdatedRevision {
			logCtx.Info("PromoteFull overrides the abort/timeout")
			clearAbortedState(newStatus)
			newStatus.Abort = false
			conditions.SetRolloutPluginCondition(newStatus, *conditions.NewRolloutPluginCondition(
				v1alpha1.RolloutPluginConditionProgressing,
				corev1.ConditionTrue,
				conditions.RolloutPluginProgressingReason,
				"Full promotion in progress"))
		}
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
			r.recordWrite(rolloutPlugin)
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
	} else if newStatus.Phase == v1alpha1.RolloutPluginPhasePaused && !rolloutPlugin.Spec.Paused && len(newStatus.PauseConditions) == 0 {
		logCtx.Info("Manual resume detected, clearing pause state")
		newStatus.Message = "Rollout resumed"
	}

	if newStatus.Abort && !newStatus.Aborted {
		logCtx.Info("Manual abort requested via status.Abort field")

		_, result, err := r.reconcileAbort(ctx, rolloutPlugin, newStatus, plugin,
			conditions.RolloutPluginAbortedReason, "Rollout aborted by user", "Aborting rollout", true, logCtx)
		return result, err
	}

	// Check and update pause/resume conditions before timeout check
	// This ensures that pause time doesn't count toward spec.timeoutSeconds
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
				defaults.GetRolloutPluginTimeoutSecondsOrDefault(rolloutPlugin)))
		condChanged := conditions.SetRolloutPluginCondition(newStatus, *timeoutCondition)

		// Record timeout event
		if condChanged && r.Recorder != nil {
			r.Recorder.Warnf(rolloutPlugin, record.EventOptions{
				EventReason: conditions.RolloutPluginTimedOutReason,
			}, fmt.Sprintf(conditions.RolloutPluginTimedOutMessage, rolloutPlugin.Name))
		}

		// Abort on timeout when timeoutAbort is enabled and the rollout is not already aborted.
		// This covers both cases: the condition first transitioning to timed-out this reconcile
		// (condChanged), and timeoutAbort being enabled retroactively after the timeout already
		// occurred (condition unchanged). The action is identical, so we do not branch on condChanged.
		if defaults.GetRolloutPluginTimeoutAbort(rolloutPlugin) && !newStatus.Aborted {
			logCtx.Info("Aborting RolloutPlugin due to timeout (timeoutAbort=true)")
			done, result, err := r.reconcileAbort(ctx, rolloutPlugin, newStatus, plugin,
				conditions.RolloutPluginAbortedReason, "Rollout aborted due to timeout", "Aborting rollout due to timeout", false, logCtx)
			if err != nil {
				return ctrl.Result{}, err
			}
			if !done {
				// Requeue to drive the abort forward — without this, nothing else schedules
				// another reconcile once Progressing is False/TimedOut, so a
				// not-yet-done abort would never be retried.
				return result, nil
			}
		}
	}

	// Get the current status of the referenced workload
	resourceStatus, err := plugin.GetResourceStatus(ctx, rolloutPlugin)
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

	// Clear any stale ReconciliationError condition from a previous failed workload read or
	// GetResourceStatus. If a rollout was underway, restore Progressing rather than removing it;
	// otherwise the next block would take it for a new rollout and restart it from step 0.
	progCond := conditions.GetRolloutPluginCondition(*newStatus, v1alpha1.RolloutPluginConditionProgressing)
	if progCond != nil && progCond.Reason == conditions.RolloutPluginReconciliationErrorReason {
		if !newStatus.Aborted && newStatus.CurrentRevision != newStatus.UpdatedRevision {
			conditions.SetRolloutPluginCondition(newStatus, *conditions.NewRolloutPluginCondition(
				v1alpha1.RolloutPluginConditionProgressing,
				corev1.ConditionTrue,
				conditions.RolloutPluginProgressingReason,
				"RolloutPlugin is progressing"))
		} else {
			conditions.RemoveRolloutPluginCondition(newStatus, v1alpha1.RolloutPluginConditionProgressing)
		}
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

	revisionChanged := oldUpdatedRevision != "" && oldUpdatedRevision != resourceStatus.UpdatedRevision
	if revisionChanged {
		logCtx.WithFields(log.Fields{
			"previousUpdatedRevision": oldUpdatedRevision,
			"newUpdatedRevision":      resourceStatus.UpdatedRevision,
		}).Info("Detected new rollout while previous rollout in progress")

		if newStatus.AbortedRevision != "" && resourceStatus.UpdatedRevision != newStatus.AbortedRevision {
			logCtx.Info("New revision detected, clearing aborted state")
			clearAbortedState(newStatus)
		}

		timeoutCond := conditions.GetRolloutPluginCondition(*newStatus, v1alpha1.RolloutPluginConditionProgressing)
		if timeoutCond != nil && timeoutCond.Reason == conditions.RolloutPluginTimedOutReason {
			logCtx.Info("New revision detected, clearing timeout condition")
			conditions.RemoveRolloutPluginCondition(newStatus, v1alpha1.RolloutPluginConditionProgressing)
		}

		// Reset restart counter for new rollout
		newStatus.RestartCount = 0
		newStatus.RestartedAt = nil

		// A promoteFull requested for the previous revision must not skip the new revision's
		// steps and analysis.
		if newStatus.PromoteFull {
			logCtx.Info("New revision detected, clearing promoteFull requested for the previous revision")
			newStatus.PromoteFull = false
		}

		// Reset step index so the new rollout starts from step 0
		// Without this, a new revision detected mid-rollout
		// would resume from the old step index, skipping earlier steps including pause steps.
		newStatus.CurrentStepIndex = nil
		newStatus.Message = "New revision detected, restarting rollout from step 0"

		// Clear pause state from previous rollout
		newStatus.PauseConditions = nil
		newStatus.ControllerPause = false
		pCtx.ClearPauseConditions()

		// The new revision gets fresh analysis runs; without this, a step-0 analysis (or
		// background analysis) would reuse the previous revision's AnalysisRun.
		clearCurrentAnalysisRuns(newStatus)

		// The workload went back to its stable revision (e.g. the template was reverted, like a
		// Rollouts rollback): no canary steps run, every pod is rolled back to it immediately.
		if resourceStatus.CurrentRevision == resourceStatus.UpdatedRevision {
			logCtx.WithField("revision", resourceStatus.UpdatedRevision).Info("Updated revision is the stable revision, rolling back all pods")
			clearAbortedState(newStatus)
			newStatus.Abort = false
			newStatus.Restart = false
			stepCount := int32(0)
			if rolloutPlugin.Spec.Strategy.Canary != nil {
				stepCount = int32(len(rolloutPlugin.Spec.Strategy.Canary.Steps))
			}
			newStatus.CurrentStepIndex = &stepCount
			newStatus.Message = fmt.Sprintf("Rolling back to stable revision %s", resourceStatus.UpdatedRevision)
			conditions.SetRolloutPluginCondition(newStatus, *conditions.NewRolloutPluginCondition(
				v1alpha1.RolloutPluginConditionProgressing,
				corev1.ConditionTrue,
				conditions.RolloutPluginProgressingReason,
				newStatus.Message))
		}
	}

	// Check if we need to start a rollout
	// A paused rollout (Progressing=Unknown/Paused) is the same revision already in progress, not
	// a fresh one; IsRolloutPluginInProgress keeps this block from resetting CurrentStepIndex and
	// re-running completed steps while paused.
	if resourceStatus.CurrentRevision != resourceStatus.UpdatedRevision {
		if !conditions.IsRolloutPluginInProgress(newStatus) {
			// If timed out (without abort), don't restart — stay degraded.
			// Without this guard, the TimedOut condition (Progressing=False) causes
			// this block to fire and overwrite it with Progressing=True, creating
			// an infinite timeout→restart loop.
			progCond := conditions.GetRolloutPluginCondition(*newStatus, v1alpha1.RolloutPluginConditionProgressing)
			if progCond != nil && progCond.Reason == conditions.RolloutPluginTimedOutReason {
				logCtx.Info("Rollout has timed out, staying in degraded state")
				// Timed out is not in progress, so this only cancels the AnalysisRuns and
				// refreshes their status references, as for an aborted rollout below.
				if _, err := r.reconcileAnalysisRunsForStatus(ctx, rolloutPlugin, newStatus, pCtx, logCtx); err != nil {
					return ctrl.Result{}, err
				}
				pCtx.CalculatePauseStatus(newStatus)
				return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
			}

			// newStatus.Aborted here reflects a stable already-aborted state (persisted on a
			// previous reconcile), not an abort that was just triggered this reconcile.
			if newStatus.Aborted {
				if newStatus.AbortedRevision != "" && resourceStatus.UpdatedRevision == newStatus.AbortedRevision {
					logCtx.WithField("abortedRevision", newStatus.AbortedRevision).Info("Rollout is aborted, not auto-restarting same revision")
					// Aborted is not in progress, so this only cancels leftover AnalysisRuns and
					// refreshes their status references (otherwise left reporting Running).
					if _, err := r.reconcileAnalysisRunsForStatus(ctx, rolloutPlugin, newStatus, pCtx, logCtx); err != nil {
						return ctrl.Result{}, err
					}
					return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
				} else {
					logCtx.WithFields(log.Fields{
						"abortedRevision": newStatus.AbortedRevision,
						"newRevision":     resourceStatus.UpdatedRevision,
					}).Info("New revision detected, clearing aborted state")
					clearAbortedState(newStatus)
					clearCurrentAnalysisRuns(newStatus)
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

	// Editing the canary steps mid-rollout restarts it from step 0, since the
	// current step index may now point at a different step, or past the end of fewer steps
	// (which would otherwise read as "all steps done" and promote fully). This also applies
	// while pods converge after the last step or a promoteFull.
	stepHash := conditions.ComputeRolloutPluginStepHash(rolloutPlugin)
	if newStatus.CurrentStepHash != "" && newStatus.CurrentStepHash != stepHash && !revisionChanged &&
		conditions.IsRolloutPluginInProgress(newStatus) && rolloutPlugin.Spec.Strategy.Canary != nil && newStatus.CurrentStepIndex != nil {
		logCtx.Info("Canary steps changed, restarting rollout from step 0")
		newStatus.CurrentStepIndex = nil
		newStatus.PauseConditions = nil
		newStatus.ControllerPause = false
		pCtx.ClearPauseConditions()
		newStatus.Canary.CurrentStepAnalysisRunStatus = nil
		newStatus.Message = "Canary steps changed, restarting from step 0"
		conditions.TouchRolloutPluginProgressingCondition(newStatus)
	}
	newStatus.CurrentStepHash = stepHash

	// PromoteFull only gets cleared inside processCanaryRollout, which only runs while a
	// rollout is in progress. If it's set with nothing to promote, clear it here instead of
	// letting it persist and silently full-promote the next revision that comes along.
	if newStatus.PromoteFull && !conditions.IsRolloutPluginInProgress(newStatus) {
		logCtx.Info("PromoteFull requested but no rollout is in progress; clearing")
		newStatus.PromoteFull = false
		newStatus.Message = "No rollout in progress to promote"
		if r.Recorder != nil {
			r.Recorder.Eventf(rolloutPlugin, record.EventOptions{
				EventReason: "PromoteFullNoOp",
			}, newStatus.Message)
		}
	}

	// Step/analysis reconciliation runs while paused too: pausing
	// only stops workload-mutating steps, while pause-step durations are still evaluated and
	// analysis keeps running.
	if conditions.IsRolloutPluginInProgress(newStatus) {
		result, err := r.processRollout(ctx, rolloutPlugin, newStatus, plugin, pCtx, logCtx)
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
	// that was still running when the rollout completed on the previous reconcile). Not
	// progressing means reconcileAnalysisRuns only cancels, never returns an abort request.
	if _, err := r.reconcileAnalysisRunsForStatus(ctx, rolloutPlugin, newStatus, pCtx, logCtx); err != nil {
		return ctrl.Result{}, err
	}

	// Apply any pending pCtx state (e.g. timeout abort) before status write
	pCtx.CalculatePauseStatus(newStatus)
	if err := r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx); err != nil {
		return ctrl.Result{}, err
	}

	// Safety-net requeue while the workload is still converging (e.g. an initial deploy whose
	// pods are not yet ready). The StatefulSet watch normally drives this transition, but a
	// coalesced or missed readiness event must not strand the RolloutPlugin short of Healthy.
	// The interval is bounded so this is not a hot loop, and the updateStatus no-op guard means
	// these polls do not write. A workload that has converged (Healthy) or is aborted rests
	// until the next real event.
	converged := resourceStatus.Ready && newStatus.CurrentRevision == newStatus.UpdatedRevision
	if !newStatus.Aborted && !converged {
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

// checkPausedConditions checks if the given RolloutPlugin is paused or not and adds an appropriate condition.
// These conditions are needed so that we won't accidentally report lack of progress for resumed rollouts
// that were paused for longer than spec.timeoutSeconds.
func checkPausedConditions(rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, logCtx *log.Entry) error {
	progCond := conditions.GetRolloutPluginCondition(*newStatus, v1alpha1.RolloutPluginConditionProgressing)
	progCondPaused := progCond != nil && progCond.Reason == conditions.RolloutPluginPausedReason

	// Paused state is determined by spec.Paused (manual) or PauseConditions (controller-set step pauses)
	isPaused := rolloutPlugin.Spec.Paused || len(newStatus.PauseConditions) > 0
	// An analysis-triggered abort uses its own reason.
	abortCondExists := newStatus.Aborted || (progCond != nil &&
		(progCond.Reason == conditions.RolloutPluginAbortedReason || progCond.Reason == conditions.RolloutPluginAnalysisRunFailedReason))
	// Completed is kept as False (not removed) once a new revision starts, so check its status.
	completedCond := conditions.GetRolloutPluginCondition(*newStatus, v1alpha1.RolloutPluginConditionCompleted)
	completed := completedCond != nil && completedCond.Status == corev1.ConditionTrue

	var updatedConditions []*v1alpha1.RolloutPluginCondition

	// Update Progressing condition when pause state changes only when there's an active rolloutplugin (not completed)
	// When paused, the Progressing condition is ConditionUnknown, and we need to be able to transition it back to ConditionTrue on resume.
	if progCond != nil && (isPaused != progCondPaused) && !abortCondExists && !completed {
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
			// The new LastUpdateTime resets the timeoutSeconds timer.
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
func (r *RolloutPluginReconciler) processRollout(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, plugin ResourcePlugin, pCtx *pauseContext, logCtx *log.Entry) (ctrl.Result, error) {
	strategy := rolloutPlugin.Spec.Strategy
	if strategy.Canary != nil {
		return r.processCanaryRollout(ctx, rolloutPlugin, newStatus, plugin, pCtx, logCtx)
	}

	logCtx.Info("No strategy defined")
	newStatus.Message = "No strategy defined"
	return ctrl.Result{}, nil
}

// processCanaryRollout processes a canary rollout
func (r *RolloutPluginReconciler) processCanaryRollout(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, plugin ResourcePlugin, pCtx *pauseContext, logCtx *log.Entry) (ctrl.Result, error) {
	canary := rolloutPlugin.Spec.Strategy.Canary
	if canary == nil {
		logCtx.Info("No canary strategy defined")
		newStatus.Message = "No canary strategy to execute"
		conditions.RemoveRolloutPluginCondition(newStatus, v1alpha1.RolloutPluginConditionProgressing)
		return ctrl.Result{}, nil
	}

	// Reconcile analysis runs. A non-empty analysisAbortMessage means an owned AnalysisRun
	// failed and the rollout should be aborted. A failure (e.g. an AnalysisRun the API server
	// rejects) is surfaced in status, like a failed GetResourceStatus, and retried.
	analysisAbortMessage, err := r.reconcileAnalysisRunsForStatus(ctx, rolloutPlugin, newStatus, pCtx, logCtx)
	if err != nil {
		newStatus.Message = fmt.Sprintf("Failed to reconcile AnalysisRuns: %v", err)
		condition := *conditions.NewRolloutPluginCondition(
			v1alpha1.RolloutPluginConditionProgressing,
			corev1.ConditionFalse,
			conditions.RolloutPluginReconciliationErrorReason,
			newStatus.Message)
		// The stale-error clear above resets this condition on every reconcile, so keep the
		// persisted one while the same error repeats rather than rewriting it and re-emitting.
		if prev := conditions.GetRolloutPluginCondition(rolloutPlugin.Status, v1alpha1.RolloutPluginConditionProgressing); prev != nil &&
			prev.Reason == condition.Reason && prev.Message == condition.Message {
			condition = *prev
		} else if r.Recorder != nil {
			r.Recorder.Warnf(rolloutPlugin, record.EventOptions{EventReason: conditions.RolloutPluginReconciliationErrorReason},
				conditions.RolloutPluginReconciliationErrorMessage, err)
		}
		conditions.SetRolloutPluginCondition(newStatus, condition)
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// promoteFull stays set until the pods converge (cleared on completion below).
	// Meanwhile every reconcile clears pauses and reconcileAnalysisRuns cancels the AnalysisRuns,
	// so an Inconclusive one can't pause the rollout again.
	if newStatus.PromoteFull {
		logCtx.Info("PromoteFull is set, skipping remaining steps and promoting")
		stepCount := int32(len(canary.Steps))
		newStatus.CurrentStepIndex = &stepCount
	}

	// Initialize step index if not set
	if newStatus.CurrentStepIndex == nil {
		stepIndex := int32(0)
		newStatus.CurrentStepIndex = &stepIndex
	}

	currentStepIndex := *newStatus.CurrentStepIndex
	if currentStepIndex >= int32(len(canary.Steps)) {
		if msg := pausedHoldMessage(rolloutPlugin, newStatus); msg != "" {
			newStatus.Message = msg
			return ctrl.Result{}, nil
		}

		// All steps completed (or PromoteFull advanced past last step).
		// Call Promote to finalize the rollout — what this means is plugin-defined
		// (e.g. set partition=0 for StatefulSet).
		// May be called more than once per rollout (e.g. while waiting for pods to converge).
		if err := plugin.PromoteFull(ctx, rolloutPlugin); err != nil {
			logCtx.WithError(err).Error("Failed to promote")
			newStatus.Message = fmt.Sprintf("Failed to promote: %v", err)
			return ctrl.Result{}, err
		}

		// Completing before every pod runs the updated revision and is Ready would let the next
		// reconcile mistake the still-converging pods for a new rollout. The status is read again
		// after PromoteFull, which reports not Ready if it just wrote to the workload.
		resourceStatus, err := plugin.GetResourceStatus(ctx, rolloutPlugin)
		if err != nil {
			logCtx.WithError(err).Error("Failed to get resource status")
			newStatus.Message = fmt.Sprintf("Failed to get resource status: %v", err)
			return ctrl.Result{}, err
		}
		converged := resourceStatus.UpdatedRevision == newStatus.UpdatedRevision &&
			resourceStatus.CurrentRevision == resourceStatus.UpdatedRevision &&
			resourceStatus.UpdatedReplicas == resourceStatus.Replicas && resourceStatus.Ready
		if !converged {
			logCtx.WithFields(log.Fields{
				"currentRevision": resourceStatus.CurrentRevision,
				"updatedRevision": resourceStatus.UpdatedRevision,
				"updatedReplicas": resourceStatus.UpdatedReplicas,
				"ready":           resourceStatus.Ready,
			}).Info("Waiting for pods to converge before completing rollout")
			newStatus.Message = "Waiting for all pods to be updated"
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}

		logCtx.Info("All canary steps completed and pods converged, completing rollout")
		newStatus.PromoteFull = false

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
	// reconcileAbort drives the plugin rollback and only persists Aborted=true once confirmed
	// done — reconcileAnalysisRunsForStatus re-derives analysisAbortMessage next reconcile from
	// the still-failed AnalysisRun, so a not-yet-done abort keeps retrying on its own.
	if analysisAbortMessage != "" && !newStatus.Aborted {
		logCtx.Error("Analysis failed, aborting rollout")
		done, result, err := r.reconcileAbort(ctx, rolloutPlugin, newStatus, plugin,
			conditions.RolloutPluginAnalysisRunFailedReason, analysisAbortMessage, analysisAbortMessage, false, logCtx)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !done {
			return result, nil
		}
		return ctrl.Result{}, nil
	}
	if pCtx.HasAddPause() {
		// Analysis set a pause condition (inconclusive)
		return ctrl.Result{}, nil
	}

	// While paused, hold at the current step: no workload mutation and no step advancement.
	// Resuming is a watched change that re-triggersreconciliation, so no requeue is needed here.
	if msg := pausedHoldMessage(rolloutPlugin, newStatus); msg != "" {
		logCtx.WithField("stepIndex", currentStepIndex).Info("Rollout is paused, holding at current step")
		newStatus.Message = msg
		return ctrl.Result{}, nil
	}

	// A pause or analysis step keeps the weight of the last setWeight step,
	// so pods added by a scale-up or left over from a higher weight are
	// brought back to it.
	if currentStep.SetWeight == nil {
		weight := currentSetWeight(canary.Steps, currentStepIndex)
		if err := plugin.SetWeight(ctx, rolloutPlugin, weight); err != nil {
			logCtx.WithError(err).Error("Failed to set weight")
			newStatus.Message = fmt.Sprintf("Failed to set weight: %v", err)
			return ctrl.Result{}, err
		}
	}

	// Process setWeight step
	if currentStep.SetWeight != nil {
		weight := *currentStep.SetWeight

		if err := plugin.SetWeight(ctx, rolloutPlugin, weight); err != nil {
			logCtx.WithError(err).Error("Failed to set weight")
			newStatus.Message = fmt.Sprintf("Failed to set weight: %v", err)
			return ctrl.Result{}, err
		}

		verified, err := plugin.VerifyWeight(ctx, rolloutPlugin, weight)
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
		conditions.TouchRolloutPluginProgressingCondition(newStatus)

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
			conditions.TouchRolloutPluginProgressingCondition(newStatus)
			newStatus.Message = "Analysis successful"
			return ctrl.Result{Requeue: true}, nil

		case v1alpha1.AnalysisPhaseRunning, v1alpha1.AnalysisPhasePending, "":
			newStatus.Message = fmt.Sprintf("Running analysis: %s", newStatus.Canary.CurrentStepAnalysisRunStatus.Name)
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil

		default:
			// Failed/Error/Inconclusive are already handled by the abort/pause check above
			// If we reach here, it means the phase just changed this reconcile — return and let
			// the next reconcile's analysis reconciliation re-derive and act on it.
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
			conditions.TouchRolloutPluginProgressingCondition(newStatus)
			logCtx.WithFields(log.Fields{
				"fromStep": currentStepIndex,
				"toStep":   nextStep,
			}).Info("Advancing past pause step")
			return ctrl.Result{Requeue: true}, nil
		}

		pauseCondition := pCtx.GetPauseCondition(v1alpha1.PauseReasonCanaryPauseStep)
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

		// Pause condition exists but not yet completed — check for timed requeue.
		if currentStep.Pause.Duration != nil {
			durationStr := currentStep.Pause.Duration.String()
			seconds := currentStep.Pause.DurationSeconds()
			if seconds < 0 {
				err := fmt.Errorf("invalid pause duration %q", durationStr)
				logCtx.WithError(err).WithField("duration", durationStr).Error("Failed to parse pause duration")
				newStatus.Message = fmt.Sprintf("Invalid pause duration: %v", err)
				return ctrl.Result{}, err
			}

			duration := time.Duration(seconds) * time.Second
			elapsed := timeutil.Now().Sub(pauseCondition.StartTime.Time)
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

// currentSetWeight returns the weight of the last setWeight step at or before stepIndex, or 0
// when there is none.
func currentSetWeight(steps []v1alpha1.PluginCanaryStep, stepIndex int32) int32 {
	for i := min(stepIndex, int32(len(steps))-1); i >= 0; i-- {
		if steps[i].SetWeight != nil {
			return *steps[i].SetWeight
		}
	}
	return 0
}

// pausedHoldMessage returns a non-empty message when the rollout must hold at its current step:
// it is manually paused (spec.paused), or paused by the controller for a reason other than a
// canary pause step (e.g. inconclusive analysis). Pause steps are driven by the pause step logic
// itself, which keeps evaluating their duration while paused.
func pausedHoldMessage(rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus) string {
	if rolloutPlugin.Spec.Paused {
		return "manually paused"
	}
	for _, cond := range newStatus.PauseConditions {
		if cond.Reason != v1alpha1.PauseReasonCanaryPauseStep {
			return fmt.Sprintf("paused: %s", cond.Reason)
		}
	}
	return ""
}

func (r *RolloutPluginReconciler) processRestart(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, plugin ResourcePlugin, logCtx *log.Entry) (ctrl.Result, error) {
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
	if err := plugin.Restart(ctx, rolloutPlugin); err != nil {
		logCtx.WithError(err).Error("Plugin restart failed")
		newStatus.Message = fmt.Sprintf("Restart failed: plugin restart error: %v", err)

		// Clear Restart flag to prevent loop
		newStatus.Restart = false

		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus, logCtx)
	}

	newStatus.RestartCount++
	now := timeutil.MetaNow()
	newStatus.RestartedAt = &now

	stepZero := int32(0)
	newStatus.CurrentStepIndex = &stepZero
	newStatus.PauseConditions = nil
	newStatus.ControllerPause = false
	clearAbortedState(newStatus)
	newStatus.Abort = false
	// Restarting from step 0 re-runs analysis from scratch; otherwise the failed AnalysisRun
	// that caused the abort would be picked up again and immediately re-abort.
	clearCurrentAnalysisRuns(newStatus)
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

	// Pod progress resets the spec.timeoutSeconds timer.
	if conditions.RolloutPluginMadeProgress(rolloutPlugin, newStatus) {
		conditions.TouchRolloutPluginProgressingCondition(newStatus)
	}

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

	if !conditions.IsRolloutPluginInProgress(newStatus) && newStatus.CurrentRevision != "" && newStatus.CurrentRevision == newStatus.UpdatedRevision {
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

	// Skip the patch when nothing changed at all. Without this, every reconcile writes an
	// identical status, bumping the resourceVersion and re-triggering the self-watch — a
	// self-sustaining reconcile loop. A genuine change (including a condition timestamp
	// transition) makes DeepEqual false, so it is still patched.
	if apiequality.Semantic.DeepEqual(&rolloutPlugin.Status, newStatus) {
		return nil
	}

	// status block doubles as a command channel (pause/abort/promote-full), so a
	// patch computed from a stale read must fail rather than silently resurrect a value the
	// user just cleared server-side. Reconcile returning the conflict error requeues and
	// re-reads.
	patch := client.MergeFromWithOptions(rolloutPlugin.DeepCopy(), client.MergeFromWithOptimisticLock{})
	rolloutPlugin.Status = *newStatus

	if err := r.Status().Patch(ctx, rolloutPlugin, patch); err != nil {
		if apierrors.IsConflict(err) {
			// Returning the error requeues and re-reads on the next reconcile
			logCtx.WithError(err).Warn("Status update conflict, will retry with a fresh read")
		} else {
			logCtx.WithError(err).Error("Failed to update status")
		}
		return err
	}
	r.recordWrite(rolloutPlugin)

	if condChanged {
		logCtx.Debug("Conditions changed during reconciliation")
	}

	return nil
}

func (r *RolloutPluginReconciler) getVersionTracker() *resourceversion.Tracker {
	r.versionTrackerOnce.Do(func() {
		r.versionTracker = resourceversion.NewTracker()
	})
	return r.versionTracker
}

// recordWrite remembers the ResourceVersion returned by a successful write to rolloutPlugin, so
// the next Reconcile waits until the cache has observed it.
func (r *RolloutPluginReconciler) recordWrite(rolloutPlugin *v1alpha1.RolloutPlugin) {
	r.getVersionTracker().Record(client.ObjectKeyFromObject(rolloutPlugin).String(), rolloutPlugin.ResourceVersion)
}

// workloadKindFor returns the workload kind the RolloutPlugin's plugin manages, after checking
// spec.workloadRef references it, so only workloads of watched kinds are read from the cache.
func (r *RolloutPluginReconciler) workloadKindFor(rolloutPlugin *v1alpha1.RolloutPlugin, plugin ResourcePlugin) (schema.GroupVersionKind, error) {
	gvk, ok := r.workloadKinds[rolloutPlugin.Spec.Plugin.Name]
	if !ok {
		var err error
		if gvk, err = plugin.WatchedGVK(); err != nil {
			return schema.GroupVersionKind{}, fmt.Errorf("failed to get the workload kind the plugin manages: %w", err)
		}
	}
	ref := rolloutPlugin.Spec.WorkloadRef
	gv, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil || gv.Group != gvk.Group || ref.Kind != gvk.Kind {
		return schema.GroupVersionKind{}, fmt.Errorf("spec.workloadRef must reference a %s, the kind the plugin manages, got %s %s", gvk.GroupKind(), ref.APIVersion, ref.Kind)
	}
	return gvk, nil
}

// newWorkloadMetadata returns an empty metadata-only object of the given workload kind.
func newWorkloadMetadata(gvk schema.GroupVersionKind) *metav1.PartialObjectMetadata {
	workload := &metav1.PartialObjectMetadata{}
	workload.SetGroupVersionKind(gvk)
	return workload
}

// getWorkload reads the metadata of the RolloutPlugin's workload. The manager's client serves
// metadata-only objects from the informer that drives the workload watch, so it is at least as
// new as the event that triggered this reconcile.
func (r *RolloutPluginReconciler) getWorkload(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, gvk schema.GroupVersionKind) (*metav1.PartialObjectMetadata, error) {
	workload := newWorkloadMetadata(gvk)
	key := client.ObjectKey{Namespace: rolloutPlugin.Namespace, Name: rolloutPlugin.Spec.WorkloadRef.Name}
	if err := r.Client.Get(ctx, key, workload); err != nil {
		return nil, err
	}
	return workload, nil
}

// pluginViewIsBehind reports whether the plugin computed status from an older version of the
// workload than the controller has seen. Resource versions that can't be compared (or a plugin
// that doesn't report one) are never treated as behind.
func pluginViewIsBehind(status *ResourceStatus, workload metav1.Object) bool {
	if status == nil || status.ObservedResourceVersion == "" {
		return false
	}
	cmp, err := resourceversion.CompareResourceVersion(status.ObservedResourceVersion, workload.GetResourceVersion())
	return err == nil && cmp < 0
}

// shouldReconcileRolloutPluginUpdate decides whether a RolloutPlugin update event should
// trigger a reconcile. It fires on spec edits and user-driven status commands, but ignores
// controller-generated status churn (conditions, phase, message, timestamps) so the
// controller does not re-trigger itself on its own status writes.
func shouldReconcileRolloutPluginUpdate(oldRP, newRP *v1alpha1.RolloutPlugin) bool {
	// Skip if ResourceVersion is the same (periodic resync).
	if oldRP.ResourceVersion == newRP.ResourceVersion {
		return false
	}

	// Spec edits always reconcile.
	if oldRP.Generation != newRP.Generation {
		return true
	}

	// Deletion started, so the plugin's DeletionCleanup must run.
	if oldRP.DeletionTimestamp.IsZero() != newRP.DeletionTimestamp.IsZero() {
		return true
	}

	// User-driven status commands (set via kubectl / plugin CLI).
	if oldRP.Status.Abort != newRP.Status.Abort ||
		oldRP.Status.Restart != newRP.Status.Restart ||
		oldRP.Status.PromoteFull != newRP.Status.PromoteFull {
		return true
	}

	// Promotion / pause scheduling: a change in the number of pause conditions
	// (user promote clears them; controller adds one to schedule a duration requeue).
	if len(oldRP.Status.PauseConditions) != len(newRP.Status.PauseConditions) {
		return true
	}

	// Everything else is controller-generated status churn — ignore it.
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *RolloutPluginReconciler) SetupWithManager(mgr ctrl.Manager) error {

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
			return shouldReconcileRolloutPluginUpdate(oldRP, newRP)
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

	b := ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.RolloutPlugin{}, builder.WithPredicates(rolloutPluginPredicate)).
		Owns(&v1alpha1.AnalysisRun{}, builder.WithPredicates(analysisRunPredicate))

	// The set of workload watches to register is derived entirely from the plugins actually
	// enabled in the argo-rollouts-config ConfigMap.
	enabledPlugins, err := r.PluginManager.EnabledPlugins()
	if err != nil {
		return fmt.Errorf("failed to connect enabled plugins for workload watch registration: %w", err)
	}

	// Workload watches cache only object metadata (never full typed or unstructured objects) to
	// keep the controller's memory independent of the workload kinds. Plugins read the full
	// workload themselves; the metadata is enough to map events to RolloutPlugins and to check
	// the plugin's view is not older than the event.
	r.workloadKinds = make(map[string]schema.GroupVersionKind, len(enabledPlugins))
	registered := map[schema.GroupVersionKind]bool{}
	for name, plugin := range enabledPlugins {
		gvk, err := plugin.WatchedGVK()
		if err != nil {
			return fmt.Errorf("plugin %q failed to report its watched GVK: %w", name, err)
		}
		r.workloadKinds[name] = gvk
		// Plugins managing the same kind share one watch.
		if registered[gvk] {
			continue
		}
		registered[gvk] = true
		log.Infof("Plugin %q enabled; registering metadata-only watch for %s", name, gvk)
		b = b.Watches(newWorkloadMetadata(gvk), handler.EnqueueRequestsFromMapFunc(r.findRolloutPluginsForWorkload),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}))
	}

	return b.
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 10, // TODO make it user Configurable
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
		prevCond.LastUpdateTime = timeutil.MetaNow()
		return prevCond
	}
	return conditions.NewRolloutPluginCondition(v1alpha1.RolloutPluginConditionInvalidSpec, corev1.ConditionTrue, conditions.RolloutPluginInvalidSpecReason, msg)
}
