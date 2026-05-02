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

// This is a built-in plugin that runs in-process, avoiding RPC overhead
type Plugin struct {
	logCtx *log.Entry
	client client.Client
}

func NewPlugin(logCtx *log.Entry) *Plugin {
	return &Plugin{logCtx: logCtx}
}

// Init initializes the plugin by creating a k8s client with informer cache.
func (p *Plugin) Init(namespace string) error {
	p.logCtx.Info("Initializing StatefulSet plugin")

	config, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get k8s config: %w", err)
	}

	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add appsv1 to scheme: %w", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add corev1 to scheme: %w", err)
	}

	cacheOpts := cache.Options{
		Scheme: scheme,
	}

	if namespace != "" && namespace != metav1.NamespaceAll {
		p.logCtx.WithField("namespace", namespace).Info("Creating namespaced cache for StatefulSet plugin")
		cacheOpts.DefaultNamespaces = map[string]cache.Config{
			namespace: {},
		}
	} else {
		p.logCtx.Info("Creating cluster-wide cache for StatefulSet plugin")
	}

	cacheInstance, err := cache.New(config, cacheOpts)
	if err != nil {
		return fmt.Errorf("failed to create cache: %w", err)
	}

	// Start the cache in a goroutine with a context that won't be cancelled since the cache needs to run for the lifetime of the plugin
	cacheCtx, cacheCancel := context.WithCancel(context.Background())
	_ = cacheCancel // Keeping cancel function in case we need it for cleanup later

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

	// client that reads from cache and writes to API server
	k8sClient, err := client.New(config, client.Options{
		Scheme: scheme,
		Cache: &client.CacheOptions{
			Reader: cacheInstance,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create k8s client: %w", err)
	}

	p.client = k8sClient
	p.logCtx.Info("StatefulSet plugin initialized successfully with cache")

	return nil
}

// GetResourceStatus gets the current status of the StatefulSet
func (p *Plugin) GetResourceStatus(ctx context.Context, workloadRef v1alpha1.WorkloadRef) (*rolloutplugin.ResourceStatus, error) {
	namespace := workloadRef.Namespace
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required in workloadRef")
	}

	sts := &appsv1.StatefulSet{}
	err := p.client.Get(ctx, client.ObjectKey{
		Name:      workloadRef.Name,
		Namespace: namespace,
	}, sts)
	if err != nil {
		return nil, fmt.Errorf("failed to get StatefulSet from cache: %w", err)
	}

	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	partition := int32(0)
	if sts.Spec.UpdateStrategy.RollingUpdate != nil && sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		partition = *sts.Spec.UpdateStrategy.RollingUpdate.Partition
	}

	// In StatefulSets, pods with ordinal >= partition are at new version
	updatedReplicas := replicas - partition

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

	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	// Calculate partition based on weight
	// Example: 10 replicas, 40% weight -> partition = 10 - (10 * 40 / 100) = 6
	// This means pods 0-3 (4 pods) will be updated, pods 4-9 stay at old version
	partition := replicas - (replicas * weight / 100)

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

	// Create an unstructured patch with ONLY the partition field so as to not interfere with any other changes to the StatefulSet spec
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
		return fmt.Errorf("failed to update StatefulSet partition: %w", err)
	}

	p.logCtx.WithField("partition", partition).Info("Successfully set partition")
	return nil
}

// VerifyWeight verifies that the canary weight has been achieved
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

	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	partition := int32(0)
	if sts.Spec.UpdateStrategy.RollingUpdate != nil && sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		partition = *sts.Spec.UpdateStrategy.RollingUpdate.Partition
	}

	// Calculate expected partition for this weight
	expectedPartition := replicas - (replicas * weight / 100)

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

	p.logCtx.WithFields(log.Fields{
		"expectedPartition": expectedPartition,
		"actualPartition":   partition,
		"expectedUpdated":   expectedUpdated,
		"actualUpdated":     actualUpdated,
		"readyReplicas":     sts.Status.ReadyReplicas,
		"totalReplicas":     replicas,
	}).Info("Weight verification")

	verified := partition == expectedPartition && actualUpdated >= expectedUpdated

	return verified, nil
}

// PromoteFull completes the rollout by setting partition to 0
func (p *Plugin) PromoteFull(ctx context.Context, workloadRef v1alpha1.WorkloadRef) error {
	p.logCtx.WithFields(log.Fields{
		"name":      workloadRef.Name,
		"namespace": workloadRef.Namespace,
	}).Info("Promoting rollout")

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
		return fmt.Errorf("failed to promote StatefulSet: %w", err)
	}

	p.logCtx.WithField("partition", partition).Info("Successfully promoted rollout")
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

	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	// Remember current partition to know which pods are on the new version.
	// Pods with ordinal >= oldPartition are on the NEW version and need to be rolled back.
	oldPartition := int32(0)
	if sts.Spec.UpdateStrategy.RollingUpdate != nil &&
		sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		oldPartition = *sts.Spec.UpdateStrategy.RollingUpdate.Partition
	}

	// Set partition to replicas (block further updates)
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

	err = p.client.Patch(ctx, stsPatch, client.Apply, client.ForceOwnership, client.FieldOwner(FieldManager))
	if err != nil {
		return fmt.Errorf("failed to update StatefulSet during abort: %w", err)
	}

	// Delete pods one-by-one (ordinals >= oldPartition) to avoid outage
	// StatefulSet controller will recreate them using CurrentRevision (old version)
	// because partition=replicas means all pods should be on old version.
	podsToDelete := replicas - oldPartition
	p.logCtx.WithFields(log.Fields{
		"oldPartition": oldPartition,
		"podsToDelete": podsToDelete,
	}).Info("Deleting updated pods one-by-one to force graceful rollback")

	deletedCount := int32(0)
	failedDeletes := []string{}

	// Delete in reverse order (replicas-1 down to oldPartition) for graceful rollback which matches StatefulSet's natural ordering
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
		err = p.waitForPodReady(ctx, workloadRef.Namespace, podName, 600) // TODOH make it configurable 600 second timeout
		if err != nil {
			p.logCtx.WithFields(log.Fields{
				"pod": podName,
				"err": err,
			}).Warn("Pod replacement did not become Ready in time, continuing rollback")
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

// Restart returns the StatefulSet to baseline state (partition = replicas) for restarts
func (p *Plugin) Restart(ctx context.Context, workloadRef v1alpha1.WorkloadRef) error {
	p.logCtx.WithFields(log.Fields{
		"name":      workloadRef.Name,
		"namespace": workloadRef.Namespace,
	}).Info("Restarting StatefulSet for restart")

	sts := &appsv1.StatefulSet{}
	err := p.client.Get(ctx, client.ObjectKey{
		Name:      workloadRef.Name,
		Namespace: workloadRef.Namespace,
	}, sts)
	if err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	// Set partition to replicas using SSA
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

	err = p.client.Patch(ctx, stsPatch, client.Apply, client.ForceOwnership, client.FieldOwner(FieldManager))
	if err != nil {
		return fmt.Errorf("failed to restart StatefulSet: %w", err)
	}

	p.logCtx.WithFields(log.Fields{
		"partition": partition,
		"replicas":  replicas,
	}).Info("Successfully restarted StatefulSet")

	return nil
}

// waitForPodReady waits for a pod to become Ready with a timeout
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
				// Pod not yet created by StatefulSet controller,need to wait
				time.Sleep(2 * time.Second)
				continue
			}
			return fmt.Errorf("failed to get pod %s: %w", podName, err)
		}

		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				p.logCtx.WithFields(log.Fields{
					"pod": podName,
				}).Info("Pod is Ready")
				return nil
			}
		}

		// Pod exists but not Ready yet,need to wait
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
