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
	FieldManager = "argo-rollouts-statefulset-plugin"
)

// Plugin implements the ResourcePlugin interface directly for StatefulSets.
// This is a built-in plugin that runs in-process, avoiding RPC overhead
// and struct conversions that external plugins require.
type Plugin struct {
	logCtx    *log.Entry
	client    client.Client // Client with cache for reads, direct API for writes
	cache     cache.Cache   // Underlying cache/informer
	namespace string        // Namespace to scope the cache to (empty = cluster-wide)
}

// NewPlugin creates a new StatefulSet plugin instance
// The plugin will create its own k8s client and cache in Init()
// namespace: if set, the plugin will only watch resources in this namespace
func NewPlugin(logCtx *log.Entry, namespace string) *Plugin {
	return &Plugin{
		logCtx:    logCtx,
		namespace: namespace,
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
	cacheOpts := cache.Options{
		Scheme: scheme,
	}

	// Configure namespace scoping if running in namespaced mode
	if p.namespace != "" && p.namespace != metav1.NamespaceAll {
		p.logCtx.WithField("namespace", p.namespace).Info("Creating namespaced cache for StatefulSet plugin")
		cacheOpts.DefaultNamespaces = map[string]cache.Config{
			p.namespace: {},
		}
	} else {
		p.logCtx.Info("Creating cluster-wide cache for StatefulSet plugin")
	}

	cacheInstance, err := cache.New(config, cacheOpts)
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
	}).Debug("StatefulSet status retrieved from cache")

	return status, nil
}

// SetWeight sets the canary weight by adjusting the partition field using Server-Side Apply
func (p *Plugin) SetWeight(ctx context.Context, workloadRef v1alpha1.WorkloadRef, weight int32) error {

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

	// Check if partition is already set to the desired value (optimization)
	currentPartition := int32(0)
	if sts.Spec.UpdateStrategy.RollingUpdate != nil && sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		currentPartition = *sts.Spec.UpdateStrategy.RollingUpdate.Partition
	}

	if currentPartition == partition {
		// Partition already set, no need to patch
		return nil
	}

	p.logCtx.WithFields(log.Fields{
		"currentPartition": currentPartition,
		"desiredPartition": partition,
		"weight":           weight,
	}).Debug("Partition needs update")

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

	// STEP 2: Delete pods one-by-one (ordinals >= oldPartition) to avoid outage
	// StatefulSet controller will recreate them using CurrentRevision (old version)
	// because partition=replicas means all pods should be on old version.
	// With partition, pods with ordinal >= partition are on the NEW (updated) version.
	// Pods with ordinal < partition are on the OLD (current) version.
	// So we need to delete pods with ordinal >= oldPartition to roll them back.
	//
	// GRACEFUL ROLLBACK: Delete pods one at a time in reverse order (highest to lowest)
	// Wait for each replacement pod to be Ready before deleting the next one
	// This maintains service availability during rollback
	podsToDelete := replicas - oldPartition
	p.logCtx.WithFields(log.Fields{
		"oldPartition": oldPartition,
		"podsToDelete": podsToDelete,
	}).Info("Deleting updated pods one-by-one to force graceful rollback")

	deletedCount := int32(0)
	failedDeletes := []string{}

	// Delete in reverse order (replicas-1 down to oldPartition) for graceful rollback
	// This matches StatefulSet's natural ordering
	for i := replicas - 1; i >= oldPartition; i-- {
		podName := fmt.Sprintf("%s-%d", sts.Name, i)

		// Delete the pod
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
				continue
			}
		}

		deletedCount++
		p.logCtx.WithFields(log.Fields{
			"pod": podName,
		}).Info("Deleted pod for rollback, waiting for replacement to be Ready")

		// Wait for the replacement pod to be Ready before deleting the next one
		// This ensures service availability during rollback
		err = p.waitForPodReady(ctx, workloadRef.Namespace, podName, 60) // 60 second timeout
		if err != nil {
			p.logCtx.WithFields(log.Fields{
				"pod": podName,
				"err": err,
			}).Warn("Pod replacement did not become Ready in time, continuing rollback")
			// Continue anyway - don't let one stuck pod block the entire rollback
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

// waitForPodReady waits for a pod to become Ready with a timeout
// Returns error if pod doesn't become Ready within timeoutSeconds
func (p *Plugin) waitForPodReady(ctx context.Context, namespace, podName string, timeoutSeconds int) error {
	timeout := time.Duration(timeoutSeconds) * time.Second
	deadline := time.Now().Add(timeout)

	p.logCtx.WithFields(log.Fields{
		"pod":     podName,
		"timeout": timeout,
	}).Debug("Waiting for pod to become Ready")

	for time.Now().Before(deadline) {
		pod := &corev1.Pod{}
		err := p.client.Get(ctx, client.ObjectKey{
			Name:      podName,
			Namespace: namespace,
		}, pod)

		if err != nil {
			if errors.IsNotFound(err) {
				// Pod not yet created by StatefulSet controller, wait
				time.Sleep(2 * time.Second)
				continue
			}
			return fmt.Errorf("failed to get pod %s: %w", podName, err)
		}

		// Check if pod is Ready
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				p.logCtx.WithFields(log.Fields{
					"pod": podName,
				}).Info("Pod is Ready")
				return nil
			}
		}

		// Pod exists but not Ready yet, wait
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("pod %s did not become Ready within %v", podName, timeout)
}

// Type returns the type of the resource plugin
func (p *Plugin) Type() string {
	return "StatefulSet"
}

// Ensure Plugin implements the controller's ResourcePlugin interface
var _ rolloutplugin.ResourcePlugin = &Plugin{}
