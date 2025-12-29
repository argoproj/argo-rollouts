package statefulset

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rolloutplugin/plugin/rpc"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

// Plugin implements the ResourcePlugin RPC interface for StatefulSets
// TODOH name?
type Plugin struct {
	logCtx     *log.Entry
	kubeClient kubernetes.Interface
}

// NewPlugin creates a new StatefulSet plugin instance
func NewPlugin(kubeClient kubernetes.Interface, logCtx *log.Entry) *Plugin {
	return &Plugin{
		kubeClient: kubeClient,
		logCtx:     logCtx,
	}
}

// InitPlugin initializes the plugin
func (p *Plugin) InitPlugin() types.RpcError {
	p.logCtx.Info("Initializing StatefulSet plugin")
	return types.RpcError{}
}

// GetResourceStatus gets the current status of the StatefulSet
func (p *Plugin) GetResourceStatus(workloadRef v1alpha1.WorkloadRef) (*rpc.ResourceStatus, types.RpcError) {
	ctx := context.Background()

	// Use the namespace from workloadRef, it should already be set by the controller
	namespace := workloadRef.Namespace
	if namespace == "" {
		return nil, types.RpcError{ErrorString: "namespace is required in workloadRef"}
	}

	p.logCtx.WithFields(log.Fields{
		"name":      workloadRef.Name,
		"namespace": namespace,
	}).Info("Getting StatefulSet status")

	// Get the StatefulSet
	sts, err := p.kubeClient.AppsV1().StatefulSets(namespace).Get(ctx, workloadRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, types.RpcError{ErrorString: fmt.Sprintf("failed to get StatefulSet: %v", err)}
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

	status := &rpc.ResourceStatus{
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
	}).Info("StatefulSet status retrieved")

	return status, types.RpcError{}
}

// SetWeight sets the canary weight by adjusting the partition field
func (p *Plugin) SetWeight(workloadRef v1alpha1.WorkloadRef, weight int32) types.RpcError {
	ctx := context.Background()
	p.logCtx.WithFields(log.Fields{
		"name":      workloadRef.Name,
		"namespace": workloadRef.Namespace,
		"weight":    weight,
	}).Info("Setting weight")

	// Get the StatefulSet
	sts, err := p.kubeClient.AppsV1().StatefulSets(workloadRef.Namespace).Get(ctx, workloadRef.Name, metav1.GetOptions{})
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("failed to get StatefulSet: %v", err)}
	}

	// Check if this revision was previously aborted
	shouldClearAbortedAnnotations := false
	shouldClearNewRevisionAnnotations := false

	if abortedRev, exists := sts.Annotations["rolloutplugin.argoproj.io/aborted-revision"]; exists {
		currentUpdateRevision := sts.Status.UpdateRevision

		if currentUpdateRevision == abortedRev {
			// Same revision that was aborted - check if retry is allowed
			if allowRetry, retryExists := sts.Annotations["rolloutplugin.argoproj.io/allow-retry"]; retryExists && allowRetry == "true" {
				// User explicitly allowed retry - we'll clear annotations after successful partition update
				p.logCtx.WithFields(log.Fields{
					"abortedRevision": abortedRev,
				}).Info("Retry allowed for aborted revision, will clear annotations after successful update")
				shouldClearAbortedAnnotations = true
			} else {
				// Retry not allowed
				return types.RpcError{
					ErrorString: fmt.Sprintf(
						"cannot rollout previously aborted revision %s. "+
							"To retry this revision, set annotation 'rolloutplugin.argoproj.io/allow-retry=true' "+
							"or update the StatefulSet spec to create a new revision",
						abortedRev,
					),
				}
			}
		} else {
			// Different revision - we'll clear annotations after successful partition update
			p.logCtx.WithFields(log.Fields{
				"abortedRevision": abortedRev,
				"currentRevision": currentUpdateRevision,
			}).Info("New revision detected, will clear aborted-revision annotation after successful update")
			shouldClearNewRevisionAnnotations = true
		}
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

	// Update the partition field
	if sts.Spec.UpdateStrategy.RollingUpdate == nil {
		sts.Spec.UpdateStrategy.RollingUpdate = &appsv1.RollingUpdateStatefulSetStrategy{}
	}
	sts.Spec.UpdateStrategy.RollingUpdate.Partition = &partition

	// Update the StatefulSet
	_, err = p.kubeClient.AppsV1().StatefulSets(workloadRef.Namespace).Update(ctx, sts, metav1.UpdateOptions{})
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("failed to update StatefulSet partition: %v", err)}
	}

	p.logCtx.WithField("partition", partition).Info("Successfully set partition")

	// Clear annotations AFTER successful partition update
	if shouldClearAbortedAnnotations || shouldClearNewRevisionAnnotations {
		// Re-fetch to get latest state
		sts, err = p.kubeClient.AppsV1().StatefulSets(workloadRef.Namespace).Get(ctx, workloadRef.Name, metav1.GetOptions{})
		if err != nil {
			p.logCtx.WithError(err).Warn("Failed to re-fetch StatefulSet after partition update, annotations not cleared")
			// Don't fail the entire operation, partition was set successfully
			return types.RpcError{}
		}

		annotationsCleared := false
		if shouldClearAbortedAnnotations {
			// Retry was allowed, clear both abort annotations
			delete(sts.Annotations, "rolloutplugin.argoproj.io/aborted-revision")
			delete(sts.Annotations, "rolloutplugin.argoproj.io/allow-retry")
			annotationsCleared = true
			p.logCtx.Info("Clearing aborted-revision and allow-retry annotations after successful partition update")
		} else if shouldClearNewRevisionAnnotations {
			// New revision detected, clear abort annotations
			delete(sts.Annotations, "rolloutplugin.argoproj.io/aborted-revision")
			delete(sts.Annotations, "rolloutplugin.argoproj.io/allow-retry") // Clean up if exists
			annotationsCleared = true
			p.logCtx.Info("Clearing aborted-revision annotation after successful partition update (new revision)")
		}

		if annotationsCleared {
			_, err = p.kubeClient.AppsV1().StatefulSets(workloadRef.Namespace).Update(ctx, sts, metav1.UpdateOptions{})
			if err != nil {
				p.logCtx.WithError(err).Warn("Failed to clear annotations, but partition was set successfully")
				// Don't fail - partition update succeeded, annotation cleanup is best-effort
			} else {
				p.logCtx.Info("Successfully cleared annotations")
			}
		}
	}

	return types.RpcError{}
}

// VerifyWeight verifies that the canary weight has been achieved
func (p *Plugin) VerifyWeight(workloadRef v1alpha1.WorkloadRef, weight int32) (bool, types.RpcError) {
	ctx := context.Background()
	p.logCtx.WithFields(log.Fields{
		"name":      workloadRef.Name,
		"namespace": workloadRef.Namespace,
		"weight":    weight,
	}).Info("Verifying weight")

	// Get the StatefulSet
	sts, err := p.kubeClient.AppsV1().StatefulSets(workloadRef.Namespace).Get(ctx, workloadRef.Name, metav1.GetOptions{})
	if err != nil {
		return false, types.RpcError{ErrorString: fmt.Sprintf("failed to get StatefulSet: %v", err)}
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
		return false, types.RpcError{}
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
	return verified, types.RpcError{}
}

// Promote completes the rollout by setting partition to 0
func (p *Plugin) Promote(workloadRef v1alpha1.WorkloadRef) types.RpcError {
	ctx := context.Background()
	p.logCtx.WithFields(log.Fields{
		"name":      workloadRef.Name,
		"namespace": workloadRef.Namespace,
	}).Info("Promoting rollout")

	// Get the StatefulSet
	sts, err := p.kubeClient.AppsV1().StatefulSets(workloadRef.Namespace).Get(ctx, workloadRef.Name, metav1.GetOptions{})
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("failed to get StatefulSet: %v", err)}
	}

	// Set partition to 0 to update all pods
	partition := int32(0)
	if sts.Spec.UpdateStrategy.RollingUpdate == nil {
		sts.Spec.UpdateStrategy.RollingUpdate = &appsv1.RollingUpdateStatefulSetStrategy{}
	}
	sts.Spec.UpdateStrategy.RollingUpdate.Partition = &partition

	// Update the StatefulSet
	_, err = p.kubeClient.AppsV1().StatefulSets(workloadRef.Namespace).Update(ctx, sts, metav1.UpdateOptions{})
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("failed to promote StatefulSet: %v", err)}
	}

	p.logCtx.WithField("partition", partition).Info("Successfully promoted rollout")
	return types.RpcError{}
}

// Abort aborts the rollout by setting partition to replicas and deleting updated pods
func (p *Plugin) Abort(workloadRef v1alpha1.WorkloadRef) types.RpcError {
	ctx := context.Background()
	p.logCtx.WithFields(log.Fields{
		"name":      workloadRef.Name,
		"namespace": workloadRef.Namespace,
	}).Info("Aborting rollout")

	// Get the StatefulSet
	sts, err := p.kubeClient.AppsV1().StatefulSets(workloadRef.Namespace).Get(ctx, workloadRef.Name, metav1.GetOptions{})
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("failed to get StatefulSet: %v", err)}
	}

	// Calculate replicas
	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	// Remember current partition (how many pods are on new version)
	oldPartition := int32(0)
	if sts.Spec.UpdateStrategy.RollingUpdate != nil &&
		sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		oldPartition = *sts.Spec.UpdateStrategy.RollingUpdate.Partition
	}

	// STEP 1: Set partition to replicas (block further updates)
	partition := replicas
	if sts.Spec.UpdateStrategy.RollingUpdate == nil {
		sts.Spec.UpdateStrategy.RollingUpdate = &appsv1.RollingUpdateStatefulSetStrategy{}
	}
	sts.Spec.UpdateStrategy.RollingUpdate.Partition = &partition

	// STEP 2: Add annotation to track aborted revision
	if sts.Annotations == nil {
		sts.Annotations = make(map[string]string)
	}
	sts.Annotations["rolloutplugin.argoproj.io/aborted-revision"] = sts.Status.UpdateRevision

	// Update the StatefulSet
	_, err = p.kubeClient.AppsV1().StatefulSets(workloadRef.Namespace).Update(ctx, sts, metav1.UpdateOptions{})
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("failed to update StatefulSet during abort: %v", err)}
	}

	// STEP 3: Delete pods that were updated (ordinals < oldPartition)
	// StatefulSet controller will recreate them using CurrentRevision (old version)
	// because partition=replicas means all pods should be on old version
	p.logCtx.WithFields(log.Fields{
		"oldPartition":    oldPartition,
		"podsToDelete":    oldPartition,
		"abortedRevision": sts.Status.UpdateRevision,
	}).Info("Deleting updated pods to force rollback")

	deletedCount := int32(0)
	failedDeletes := []string{}

	for i := int32(0); i < oldPartition; i++ {
		podName := fmt.Sprintf("%s-%d", sts.Name, i)
		err := p.kubeClient.CoreV1().Pods(workloadRef.Namespace).Delete(
			ctx,
			podName,
			metav1.DeleteOptions{},
		)
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
		return types.RpcError{
			ErrorString: fmt.Sprintf("aborted rollout but failed to delete %d pods: %v",
				len(failedDeletes), failedDeletes),
		}
	}

	return types.RpcError{}
}

// Reset returns the StatefulSet to baseline state (partition = replicas) for retry
func (p *Plugin) Reset(workloadRef v1alpha1.WorkloadRef) types.RpcError {
	ctx := context.Background()
	p.logCtx.WithFields(log.Fields{
		"name":      workloadRef.Name,
		"namespace": workloadRef.Namespace,
	}).Info("Resetting StatefulSet for retry")

	// Get the StatefulSet
	sts, err := p.kubeClient.AppsV1().StatefulSets(workloadRef.Namespace).Get(ctx, workloadRef.Name, metav1.GetOptions{})
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("failed to get StatefulSet: %v", err)}
	}

	// Calculate replicas
	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	// Set partition to replicas (0% canary = baseline)
	partition := replicas
	if sts.Spec.UpdateStrategy.RollingUpdate == nil {
		sts.Spec.UpdateStrategy.RollingUpdate = &appsv1.RollingUpdateStatefulSetStrategy{}
	}
	sts.Spec.UpdateStrategy.RollingUpdate.Partition = &partition

	// Update the StatefulSet
	_, err = p.kubeClient.AppsV1().StatefulSets(workloadRef.Namespace).Update(ctx, sts, metav1.UpdateOptions{})
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("failed to reset StatefulSet: %v", err)}
	}

	p.logCtx.WithFields(log.Fields{
		"partition": partition,
		"replicas":  replicas,
	}).Info("Successfully reset StatefulSet to baseline (partition = replicas)")

	return types.RpcError{}
}

// Type returns the type of the resource plugin
func (p *Plugin) Type() string {
	return "StatefulSet"
}

// Ensure Plugin implements RPC ResourcePlugin interface
var _ rpc.ResourcePlugin = &Plugin{}
