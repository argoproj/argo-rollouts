package statefulset

import (
	"context"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rolloutplugin"
)

const (
	// FieldManager is the field manager name used for Server-Side Apply
	FieldManager = "argo-rollouts"
)

// Plugin implements the ResourcePlugin interface directly for StatefulSets.
// This is a built-in plugin that runs in-process, avoiding RPC overhead
// and struct conversions that external plugins require.
type Plugin struct {
	logCtx *log.Entry
	client client.Client // Client with cache for reads, direct API for writes
	cache  cache.Cache   // Underlying cache/informer
}

// NewPlugin creates a new StatefulSet plugin instance
// The plugin will create its own k8s client and cache in Init()
func NewPlugin(logCtx *log.Entry) *Plugin {
	return &Plugin{
		logCtx: logCtx,
	}
}

// Init initializes the plugin by creating a k8s client with informer cache
func (p *Plugin) Init() error {
	p.logCtx.Info("Initializing StatefulSet plugin")

	// Get k8s config
	config, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get k8s config: %w", err)
	}

	// Create scheme with required types
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add appsv1 to scheme: %w", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add corev1 to scheme: %w", err)
	}

	// Create cache (informer) for efficient reads
	cacheInstance, err := cache.New(config, cache.Options{
		Scheme: scheme,
	})
	if err != nil {
		return fmt.Errorf("failed to create cache: %w", err)
	}

	// Start the cache in a goroutine with a context that won't be cancelled
	// The cache needs to run for the lifetime of the plugin
	cacheCtx, cacheCancel := context.WithCancel(context.Background())
	_ = cacheCancel // Keep cancel function in case we need it for cleanup later

	go func() {
		if err := cacheInstance.Start(cacheCtx); err != nil {
			p.logCtx.WithError(err).Error("Cache failed to start")
		}
	}()

	// Wait for cache to sync with a timeout context
	syncCtx, syncCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer syncCancel()

	p.logCtx.Info("Waiting for cache to sync...")
	if !cacheInstance.WaitForCacheSync(syncCtx) {
		return fmt.Errorf("failed to sync cache within timeout")
	}
	p.logCtx.Info("Cache synced successfully")

	// Create client that reads from cache
	// Writes will bypass cache and go directly to API server
	k8sClient, err := client.New(config, client.Options{
		Scheme: scheme,
		Cache: &client.CacheOptions{
			Reader: cacheInstance, // Reads go to cache
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create k8s client: %w", err)
	}

	p.client = k8sClient
	p.cache = cacheInstance
	p.logCtx.Info("StatefulSet plugin initialized successfully with cache")

	return nil
}

// GetResourceStatus gets the current status of the StatefulSet
// Reads from the informer cache for efficiency
func (p *Plugin) GetResourceStatus(ctx context.Context, workloadRef v1alpha1.WorkloadRef) (*rolloutplugin.ResourceStatus, error) {
	// Use the namespace from workloadRef, it should already be set by the controller
	namespace := workloadRef.Namespace
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required in workloadRef")
	}

	p.logCtx.WithFields(log.Fields{
		"name":      workloadRef.Name,
		"namespace": namespace,
	}).Info("Getting StatefulSet status from cache")

	// Get the StatefulSet from cache (efficient read)
	sts := &appsv1.StatefulSet{}
	err := p.client.Get(ctx, client.ObjectKey{
		Name:      workloadRef.Name,
		Namespace: namespace,
	}, sts)
	if err != nil {
		return nil, fmt.Errorf("failed to get StatefulSet from cache: %w", err)
	}

	// Calculate replicas
	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	// Get partition value
	partition := int32(0)
	if sts.Spec.UpdateStrategy.RollingUpdate != nil && sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		partition = *sts.Spec.UpdateStrategy.RollingUpdate.Partition
	}

	// Calculate updated replicas based on partition
	// In StatefulSets, pods with ordinal >= partition are at old version
	// So updated replicas = replicas - partition
	updatedReplicas := replicas - partition

	// Get current and updated controller revisions
	currentRevision := sts.Status.CurrentRevision
	updateRevision := sts.Status.UpdateRevision

	// Check if all pods are ready
	ready := sts.Status.ReadyReplicas == replicas

	status := &rolloutplugin.ResourceStatus{
		Replicas:          replicas,
		UpdatedReplicas:   updatedReplicas,
		ReadyReplicas:     sts.Status.ReadyReplicas,
		AvailableReplicas: sts.Status.ReadyReplicas,
		CurrentRevision:   currentRevision,
		UpdatedRevision:   updateRevision,
		Ready:             ready,
	}

	p.logCtx.WithFields(log.Fields{
		"replicas":        replicas,
		"partition":       partition,
		"updatedReplicas": updatedReplicas,
		"readyReplicas":   sts.Status.ReadyReplicas,
		"ready":           ready,
	}).Info("StatefulSet status retrieved from cache")

	return status, nil
}

// SetWeight sets the canary weight by adjusting the partition field using Server-Side Apply
func (p *Plugin) SetWeight(ctx context.Context, workloadRef v1alpha1.WorkloadRef, weight int32) error {
	p.logCtx.WithFields(log.Fields{
		"name":      workloadRef.Name,
		"namespace": workloadRef.Namespace,
		"weight":    weight,
	}).Info("Setting weight")

	// Get the StatefulSet from cache
	sts := &appsv1.StatefulSet{}
	err := p.client.Get(ctx, client.ObjectKey{
		Name:      workloadRef.Name,
		Namespace: workloadRef.Namespace,
	}, sts)
	if err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	// Calculate replicas
	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	// Calculate partition based on weight
	// Formula: partition = replicas - (replicas * weight / 100)
	// Example: 10 replicas, 40% weight -> partition = 10 - (10 * 40 / 100) = 6
	// This means pods 0-3 (4 pods) will be updated, pods 4-9 stay at old version
	partition := replicas - (replicas * weight / 100)

	// Ensure partition is within valid range [0, replicas]
	if partition < 0 {
		partition = 0
	}
	if partition > replicas {
		partition = replicas
	}

	p.logCtx.WithFields(log.Fields{
		"replicas":  replicas,
		"weight":    weight,
		"partition": partition,
	}).Info("Calculated partition")

	// Create an unstructured patch with ONLY the partition field
	// This avoids including any other fields that might trigger validation
	stsPatch := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "StatefulSet",
			"metadata": map[string]interface{}{
				"name":      sts.Name,
				"namespace": sts.Namespace,
			},
			"spec": map[string]interface{}{
				"updateStrategy": map[string]interface{}{
					"rollingUpdate": map[string]interface{}{
						"partition": partition,
					},
				},
			},
		},
	}

	// Apply with Server-Side Apply to take ownership of partition field
	// This prevents ArgoCD from showing the field as out-of-sync
	err = p.client.Patch(ctx, stsPatch, client.Apply, client.ForceOwnership, client.FieldOwner(FieldManager))
	if err != nil {
		return fmt.Errorf("failed to update StatefulSet partition using SSA: %w", err)
	}

	p.logCtx.WithField("partition", partition).Info("Successfully set partition using Server-Side Apply")
	return nil
}

// VerifyWeight verifies that the canary weight has been achieved
// Reads from cache for efficiency
func (p *Plugin) VerifyWeight(ctx context.Context, workloadRef v1alpha1.WorkloadRef, weight int32) (bool, error) {
	p.logCtx.WithFields(log.Fields{
		"name":      workloadRef.Name,
		"namespace": workloadRef.Namespace,
		"weight":    weight,
	}).Info("Verifying weight")

	// Get the StatefulSet from cache
	sts := &appsv1.StatefulSet{}
	err := p.client.Get(ctx, client.ObjectKey{
		Name:      workloadRef.Name,
		Namespace: workloadRef.Namespace,
	}, sts)
	if err != nil {
		return false, fmt.Errorf("failed to get StatefulSet from cache: %w", err)
	}

	// Calculate replicas
	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	// Get current partition
	partition := int32(0)
	if sts.Spec.UpdateStrategy.RollingUpdate != nil && sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		partition = *sts.Spec.UpdateStrategy.RollingUpdate.Partition
	}

	// Calculate expected partition for this weight
	expectedPartition := replicas - (replicas * weight / 100)
	if expectedPartition < 0 {
		expectedPartition = 0
	}
	if expectedPartition > replicas {
		expectedPartition = replicas
	}

	// Check if partition matches
	if partition != expectedPartition {
		p.logCtx.WithFields(log.Fields{
			"expected": expectedPartition,
			"actual":   partition,
		}).Info("Partition mismatch")
		return false, nil
	}

	// Calculate expected updated replicas
	expectedUpdated := replicas - expectedPartition

	// Get actual updated replicas from StatefulSet status
	actualUpdated := sts.Status.UpdatedReplicas
	ready := sts.Status.ReadyReplicas == replicas

	p.logCtx.WithFields(log.Fields{
		"expectedPartition": expectedPartition,
		"actualPartition":   partition,
		"expectedUpdated":   expectedUpdated,
		"actualUpdated":     actualUpdated,
		"readyReplicas":     sts.Status.ReadyReplicas,
		"totalReplicas":     replicas,
		"ready":             ready,
	}).Info("Weight verification")

	// Weight is achieved when:
	// 1. Partition is set correctly
	// 2. Expected number of pods are actually updated (from status)
	// 3. All pods are ready
	verified := partition == expectedPartition && actualUpdated >= expectedUpdated && ready

	p.logCtx.WithField("verified", verified).Info("Weight verification result")
	return verified, nil
}

// Promote completes the rollout by setting partition to 0 using Server-Side Apply
func (p *Plugin) Promote(ctx context.Context, workloadRef v1alpha1.WorkloadRef) error {
	p.logCtx.WithFields(log.Fields{
		"name":      workloadRef.Name,
		"namespace": workloadRef.Namespace,
	}).Info("Promoting rollout")

	// Create an unstructured patch with partition set to 0
	partition := int32(0)
	stsPatch := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "StatefulSet",
			"metadata": map[string]interface{}{
				"name":      workloadRef.Name,
				"namespace": workloadRef.Namespace,
			},
			"spec": map[string]interface{}{
				"updateStrategy": map[string]interface{}{
					"rollingUpdate": map[string]interface{}{
						"partition": partition,
					},
				},
			},
		},
	}

	// Apply with Server-Side Apply
	err := p.client.Patch(ctx, stsPatch, client.Apply, client.ForceOwnership, client.FieldOwner(FieldManager))
	if err != nil {
		return fmt.Errorf("failed to promote StatefulSet using SSA: %w", err)
	}

	p.logCtx.WithField("partition", partition).Info("Successfully promoted rollout using Server-Side Apply")
	return nil
}

// Abort aborts the rollout by setting partition to replicas and deleting updated pods using Server-Side Apply
func (p *Plugin) Abort(ctx context.Context, workloadRef v1alpha1.WorkloadRef) error {
	p.logCtx.WithFields(log.Fields{
		"name":      workloadRef.Name,
		"namespace": workloadRef.Namespace,
	}).Info("Aborting rollout")

	// Get the StatefulSet from cache to know replicas and current partition
	sts := &appsv1.StatefulSet{}
	err := p.client.Get(ctx, client.ObjectKey{
		Name:      workloadRef.Name,
		Namespace: workloadRef.Namespace,
	}, sts)
	if err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	// Calculate replicas
	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	// Remember current partition to know which pods are updated
	// Pods with ordinal < partition are on NEW version and need to be rolled back
	oldPartition := int32(0)
	if sts.Spec.UpdateStrategy.RollingUpdate != nil &&
		sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		oldPartition = *sts.Spec.UpdateStrategy.RollingUpdate.Partition
	}

	// STEP 1: Set partition to replicas (block further updates) using SSA
	partition := replicas
	stsPatch := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "StatefulSet",
			"metadata": map[string]interface{}{
				"name":      sts.Name,
				"namespace": sts.Namespace,
			},
			"spec": map[string]interface{}{
				"updateStrategy": map[string]interface{}{
					"rollingUpdate": map[string]interface{}{
						"partition": partition,
					},
				},
			},
		},
	}

	// Apply with Server-Side Apply
	err = p.client.Patch(ctx, stsPatch, client.Apply, client.ForceOwnership, client.FieldOwner(FieldManager))
	if err != nil {
		return fmt.Errorf("failed to update StatefulSet during abort using SSA: %w", err)
	}

	// STEP 2: Delete pods that were updated (ordinals < oldPartition)
	// StatefulSet controller will recreate them using CurrentRevision (old version)
	// because partition=replicas means all pods should be on old version
	// We delete all pods with ordinal < oldPartition to ensure all potentially
	// updated pods are rolled back
	p.logCtx.WithFields(log.Fields{
		"oldPartition": oldPartition,
		"podsToDelete": oldPartition,
	}).Info("Deleting updated pods to force rollback")

	deletedCount := int32(0)
	failedDeletes := []string{}

	for i := int32(0); i < oldPartition; i++ {
		podName := fmt.Sprintf("%s-%d", sts.Name, i)
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: workloadRef.Namespace,
			},
		}
		err := p.client.Delete(ctx, pod)
		if err != nil {
			if !errors.IsNotFound(err) {
				// Log but continue - some pods might already be gone
				p.logCtx.WithFields(log.Fields{
					"pod": podName,
					"err": err,
				}).Warn("Failed to delete pod during abort")
				failedDeletes = append(failedDeletes, podName)
			}
		} else {
			deletedCount++
			p.logCtx.WithFields(log.Fields{
				"pod": podName,
			}).Info("Deleted pod for rollback")
		}
	}

	p.logCtx.WithFields(log.Fields{
		"deletedPods":   deletedCount,
		"failedDeletes": len(failedDeletes),
	}).Info("Successfully aborted rollout")

	if len(failedDeletes) > 0 {
		return fmt.Errorf("aborted rollout but failed to delete %d pods: %v",
			len(failedDeletes), failedDeletes)
	}

	return nil
}

// Restart returns the StatefulSet to baseline state (partition = replicas) for restart using Server-Side Apply
func (p *Plugin) Restart(ctx context.Context, workloadRef v1alpha1.WorkloadRef) error {
	p.logCtx.WithFields(log.Fields{
		"name":      workloadRef.Name,
		"namespace": workloadRef.Namespace,
	}).Info("Restarting StatefulSet for restart")

	// Get the StatefulSet from cache to know replicas
	sts := &appsv1.StatefulSet{}
	err := p.client.Get(ctx, client.ObjectKey{
		Name:      workloadRef.Name,
		Namespace: workloadRef.Namespace,
	}, sts)
	if err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	// Calculate replicas
	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	// Set partition to replicas (0% canary = baseline) using SSA
	partition := replicas
	stsPatch := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "StatefulSet",
			"metadata": map[string]interface{}{
				"name":      sts.Name,
				"namespace": sts.Namespace,
			},
			"spec": map[string]interface{}{
				"updateStrategy": map[string]interface{}{
					"rollingUpdate": map[string]interface{}{
						"partition": partition,
					},
				},
			},
		},
	}

	// Apply with Server-Side Apply
	err = p.client.Patch(ctx, stsPatch, client.Apply, client.ForceOwnership, client.FieldOwner(FieldManager))
	if err != nil {
		return fmt.Errorf("failed to restart StatefulSet using SSA: %w", err)
	}

	p.logCtx.WithFields(log.Fields{
		"partition": partition,
		"replicas":  replicas,
	}).Info("Successfully restarted StatefulSet to baseline (partition = replicas) using Server-Side Apply")

	return nil
}

// Type returns the type of the resource plugin
func (p *Plugin) Type() string {
	return "StatefulSet"
}

// Ensure Plugin implements the controller's ResourcePlugin interface
var _ rolloutplugin.ResourcePlugin = &Plugin{}
