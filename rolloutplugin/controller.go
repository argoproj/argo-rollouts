package rolloutplugin

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
)

// RolloutPluginReconciler reconciles a RolloutPlugin object
type RolloutPluginReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	KubeClientset     kubernetes.Interface
	ArgoProjClientset clientset.Interface
	DynamicClientset  dynamic.Interface
	PluginManager     PluginManager
}

// PluginManager is an interface for managing plugins
type PluginManager interface {
	// GetPlugin returns a plugin by name
	GetPlugin(name string) (ResourcePlugin, error)
	// LoadPlugin loads a plugin from the given configuration
	LoadPlugin(config v1alpha1.PluginConfig) error
}

// ResourcePlugin is the interface that all resource plugins must implement
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
}

// ResourceStatus contains the status of a workload resource
type ResourceStatus struct {
	Replicas          int32
	UpdatedReplicas   int32
	ReadyReplicas     int32
	AvailableReplicas int32
	CurrentRevision   string
	UpdatedRevision   string
	Ready             bool
}

//+kubebuilder:rbac:groups=argoproj.io,resources=rolloutplugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=argoproj.io,resources=rolloutplugins/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=argoproj.io,resources=rolloutplugins/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *RolloutPluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling RolloutPlugin", "namespace", req.Namespace, "name", req.Name)

	// Fetch the RolloutPlugin instance
	rolloutPlugin := &v1alpha1.RolloutPlugin{}
	if err := r.Get(ctx, req.NamespacedName, rolloutPlugin); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle deletion
	if !rolloutPlugin.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, rolloutPlugin)
	}

	// Reconcile the RolloutPlugin
	return r.reconcile(ctx, rolloutPlugin)
}

// handleDeletion handles the deletion of a RolloutPlugin
func (r *RolloutPluginReconciler) handleDeletion(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Handling deletion of RolloutPlugin")
	// Perform any cleanup if needed
	return ctrl.Result{}, nil
}

// reconcile performs the main reconciliation logic
func (r *RolloutPluginReconciler) reconcile(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling RolloutPlugin")

	newStatus := rolloutPlugin.Status.DeepCopy()
	newStatus.ObservedGeneration = rolloutPlugin.Generation

	// Get the plugin
	plugin, err := r.PluginManager.GetPlugin(rolloutPlugin.Spec.Plugin.Name)
	if err != nil {
		logger.Error(err, "Failed to get plugin")
		newStatus.Phase = "Failed"
		newStatus.Message = fmt.Sprintf("Failed to get plugin: %v", err)
		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus)
	}

	// Initialize plugin if not already initialized
	if !newStatus.Initialized {
		if err := plugin.Init(); err != nil {
			logger.Error(err, "Failed to initialize plugin")
			newStatus.Phase = "Failed"
			newStatus.Message = fmt.Sprintf("Failed to initialize plugin: %v", err)
			return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus)
		}
		newStatus.Initialized = true
		logger.Info("Plugin initialized successfully")
	}

	// Prepare workload reference with namespace defaulting
	workloadRef := rolloutPlugin.Spec.WorkloadRef
	if workloadRef.Namespace == "" {
		workloadRef.Namespace = rolloutPlugin.Namespace
	}

	// Get the current status of the referenced workload
	resourceStatus, err := plugin.GetResourceStatus(ctx, workloadRef)
	if err != nil {
		logger.Error(err, "Failed to get resource status")
		newStatus.Phase = "Failed"
		newStatus.Message = fmt.Sprintf("Failed to get resource status: %v", err)
		return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus)
	}

	// Update replica counts
	newStatus.Replicas = resourceStatus.Replicas
	newStatus.UpdatedReplicas = resourceStatus.UpdatedReplicas
	newStatus.ReadyReplicas = resourceStatus.ReadyReplicas
	newStatus.AvailableReplicas = resourceStatus.AvailableReplicas
	newStatus.CurrentRevision = resourceStatus.CurrentRevision
	newStatus.UpdatedRevision = resourceStatus.UpdatedRevision

	// Check if we need to start a rollout
	if resourceStatus.CurrentRevision != resourceStatus.UpdatedRevision {
		if !newStatus.RolloutInProgress {
			logger.Info("Starting new rollout")
			newStatus.RolloutInProgress = true
			newStatus.CurrentStepIndex = nil
			newStatus.Phase = "Progressing"
		}
	}

	// Process rollout steps if in progress
	if newStatus.RolloutInProgress {
		result, err := r.processRollout(ctx, rolloutPlugin, newStatus, plugin, workloadRef)
		if err != nil {
			logger.Error(err, "Failed to process rollout")
			return result, err
		}
		if err := r.updateStatus(ctx, rolloutPlugin, newStatus); err != nil {
			return ctrl.Result{}, err
		}
		return result, nil
	}

	newStatus.Phase = "Healthy"
	newStatus.Message = "RolloutPlugin is healthy"
	return ctrl.Result{}, r.updateStatus(ctx, rolloutPlugin, newStatus)
}

// processRollout processes the rollout steps based on strategy
func (r *RolloutPluginReconciler) processRollout(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, plugin ResourcePlugin, workloadRef v1alpha1.WorkloadRef) (ctrl.Result, error) {
	strategy := rolloutPlugin.Spec.Strategy
	if strategy.Canary != nil {
		return r.processCanaryRollout(ctx, rolloutPlugin, newStatus, plugin, workloadRef)
	} else if strategy.BlueGreen != nil {
		return r.processBlueGreenRollout(ctx, rolloutPlugin, newStatus, plugin, workloadRef)
	}

	logger := log.FromContext(ctx)
	logger.Info("No strategy defined")
	newStatus.Phase = "Failed"
	newStatus.Message = "No strategy defined"
	return ctrl.Result{}, nil
}

// processCanaryRollout processes a canary rollout
func (r *RolloutPluginReconciler) processCanaryRollout(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, plugin ResourcePlugin, workloadRef v1alpha1.WorkloadRef) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	canary := rolloutPlugin.Spec.Strategy.Canary
	if canary == nil || len(canary.Steps) == 0 {
		logger.Info("No canary steps defined")
		newStatus.Phase = "Successful"
		newStatus.Message = "No canary steps to execute"
		newStatus.RolloutInProgress = false
		return ctrl.Result{}, nil
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
		logger.Info("All canary steps completed, promoting")
		if err := plugin.Promote(ctx, workloadRef); err != nil {
			logger.Error(err, "Failed to promote")
			newStatus.Phase = "Failed"
			newStatus.Message = fmt.Sprintf("Failed to promote: %v", err)
			return ctrl.Result{}, err
		}
		newStatus.Phase = "Successful"
		newStatus.Message = "Rollout completed successfully"
		newStatus.RolloutInProgress = false
		return ctrl.Result{}, nil
	}

	currentStep := canary.Steps[currentStepIndex]
	logger.Info("Processing canary step", "stepIndex", currentStepIndex)

	// Process setWeight step
	if currentStep.SetWeight != nil {
		weight := *currentStep.SetWeight
		logger.Info("Setting weight", "weight", weight)

		if err := plugin.SetWeight(ctx, workloadRef, weight); err != nil {
			logger.Error(err, "Failed to set weight")
			newStatus.Phase = "Failed"
			newStatus.Message = fmt.Sprintf("Failed to set weight: %v", err)
			return ctrl.Result{}, err
		}

		verified, err := plugin.VerifyWeight(ctx, workloadRef, weight)
		if err != nil {
			logger.Error(err, "Failed to verify weight")
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
		logger.Info("Weight verified, moving to next step", "nextStep", nextStep)
		// Requeue immediately to process next step
		return ctrl.Result{Requeue: true}, nil
	}

	// Handle pause step
	if currentStep.Pause != nil {
		logger.Info("Handling pause step", "pauseStartTime", newStatus.PauseStartTime)

		if newStatus.PauseStartTime == nil {
			now := metav1.Now()
			newStatus.PauseStartTime = &now
			newStatus.Paused = true
			newStatus.Message = "Paused"
			logger.Info("Starting pause", "duration", currentStep.Pause.Duration)
			return ctrl.Result{}, nil
		}

		// Check if pause duration has elapsed
		if currentStep.Pause.Duration != nil {
			durationStr := currentStep.Pause.Duration.String()
			duration, err := time.ParseDuration(durationStr)
			if err != nil {
				logger.Error(err, "Failed to parse pause duration", "duration", durationStr)
				newStatus.Phase = "Failed"
				newStatus.Message = fmt.Sprintf("Invalid pause duration: %v", err)
				return ctrl.Result{}, err
			}

			elapsed := time.Since(newStatus.PauseStartTime.Time)
			if elapsed >= duration {
				logger.Info("Pause duration elapsed, moving to next step")
				// Move to next step
				nextStep := currentStepIndex + 1
				newStatus.CurrentStepIndex = &nextStep
				newStatus.CurrentStepComplete = false
				newStatus.PauseStartTime = nil
				newStatus.Paused = false
				// Requeue immediately to process next step
				return ctrl.Result{Requeue: true}, nil
			}

			remaining := duration - elapsed
			newStatus.Message = fmt.Sprintf("Paused (remaining: %s)", remaining.Round(time.Second))
			logger.Info("Still paused", "remaining", remaining)
			// Requeue when pause should be done
			return ctrl.Result{RequeueAfter: remaining}, nil
		}

		// Indefinite pause - wait for manual promotion
		logger.Info("Rollout is paused indefinitely, waiting for manual promotion")
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// processBlueGreenRollout processes a blue/green rollout
func (r *RolloutPluginReconciler) processBlueGreenRollout(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus, plugin ResourcePlugin, workloadRef v1alpha1.WorkloadRef) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Processing blue/green rollout")

	// TODO: Implement blue/green rollout logic
	newStatus.Phase = "Progressing"
	newStatus.Message = "Blue/Green rollout not yet implemented"

	return ctrl.Result{}, nil
}

// updateStatus updates the status of the RolloutPlugin
func (r *RolloutPluginReconciler) updateStatus(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, newStatus *v1alpha1.RolloutPluginStatus) error {
	logger := log.FromContext(ctx)

	patch := client.MergeFrom(rolloutPlugin.DeepCopy())
	rolloutPlugin.Status = *newStatus

	if err := r.Status().Patch(ctx, rolloutPlugin, patch); err != nil {
		logger.Error(err, "Failed to update status")
		return err
	}

	logger.Info("Status updated successfully")
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RolloutPluginReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.RolloutPlugin{}).
		Watches(
			&appsv1.StatefulSet{},
			handler.EnqueueRequestsFromMapFunc(r.findRolloutPluginsForStatefulSet),
		).
		Complete(r)
}

// findRolloutPluginsForStatefulSet maps a StatefulSet to RolloutPlugin CRs that reference it
func (r *RolloutPluginReconciler) findRolloutPluginsForStatefulSet(ctx context.Context, obj client.Object) []reconcile.Request {
	sts := obj.(*appsv1.StatefulSet)

	// List all RolloutPlugin resources in the same namespace
	var rolloutPlugins v1alpha1.RolloutPluginList
	if err := r.Client.List(ctx, &rolloutPlugins, client.InNamespace(sts.GetNamespace())); err != nil {
		log.FromContext(ctx).Error(err, "Failed to list RolloutPlugin resources")
		return []reconcile.Request{}
	}

	// Find RolloutPlugins that reference this StatefulSet
	var requests []reconcile.Request
	for _, rp := range rolloutPlugins.Items {
		if rp.Spec.WorkloadRef.Kind == "StatefulSet" &&
			rp.Spec.WorkloadRef.Name == sts.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKey{
					Namespace: rp.GetNamespace(),
					Name:      rp.GetName(),
				},
			})
			log.FromContext(ctx).Info("StatefulSet change detected, triggering rollout",
				"statefulset", sts.GetName(),
				"rolloutplugin", rp.GetName())
		}
	}

	return requests
}
