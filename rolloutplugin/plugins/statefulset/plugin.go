package statefulset

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rolloutplugin/plugin/rpc"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

// Plugin implements the ResourcePlugin RPC interface for StatefulSets
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

	// Get current and updated revisions
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

// Abort aborts the rollout by setting partition to replicas (all pods at old version)
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

	// Set partition to replicas to keep all pods at old version
	partition := replicas
	if sts.Spec.UpdateStrategy.RollingUpdate == nil {
		sts.Spec.UpdateStrategy.RollingUpdate = &appsv1.RollingUpdateStatefulSetStrategy{}
	}
	sts.Spec.UpdateStrategy.RollingUpdate.Partition = &partition

	// Update the StatefulSet
	_, err = p.kubeClient.AppsV1().StatefulSets(workloadRef.Namespace).Update(ctx, sts, metav1.UpdateOptions{})
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("failed to abort StatefulSet rollout: %v", err)}
	}

	p.logCtx.WithField("partition", partition).Info("Successfully aborted rollout")
	return types.RpcError{}
}

// Type returns the type of the resource plugin
func (p *Plugin) Type() string {
	return "StatefulSet"
}

// Ensure Plugin implements RPC ResourcePlugin interface
var _ rpc.ResourcePlugin = &Plugin{}
