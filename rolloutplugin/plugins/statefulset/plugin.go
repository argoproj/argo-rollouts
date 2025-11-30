package statefulset

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rolloutplugin"
)

// Plugin implements the ResourcePlugin interface for StatefulSets
type Plugin struct {
	kubeClient kubernetes.Interface
	client     client.Client
}

// NewPlugin creates a new StatefulSet plugin instance
func NewPlugin(kubeClient kubernetes.Interface, client client.Client) *Plugin {
	return &Plugin{
		kubeClient: kubeClient,
		client:     client,
	}
}

// Init initializes the plugin
func (p *Plugin) Init() error {
	log.Log.Info("Initializing StatefulSet plugin")
	return nil
}

// GetResourceStatus gets the current status of the StatefulSet
func (p *Plugin) GetResourceStatus(ctx context.Context, workloadRef v1alpha1.WorkloadRef) (*rolloutplugin.ResourceStatus, error) {
	logger := log.FromContext(ctx)

	// Use the namespace from workloadRef, it should already be set by the controller
	namespace := workloadRef.Namespace
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required in workloadRef")
	}

	logger.Info("Getting StatefulSet status", "name", workloadRef.Name, "namespace", namespace)

	// Get the StatefulSet
	sts, err := p.kubeClient.AppsV1().StatefulSets(namespace).Get(ctx, workloadRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get StatefulSet: %w", err)
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

	status := &rolloutplugin.ResourceStatus{
		Replicas:          replicas,
		UpdatedReplicas:   updatedReplicas,
		ReadyReplicas:     sts.Status.ReadyReplicas,
		AvailableReplicas: sts.Status.ReadyReplicas,
		CurrentRevision:   currentRevision,
		UpdatedRevision:   updateRevision,
		Ready:             ready,
	}

	logger.Info("StatefulSet status retrieved",
		"replicas", replicas,
		"partition", partition,
		"updatedReplicas", updatedReplicas,
		"readyReplicas", sts.Status.ReadyReplicas,
		"currentRevision", currentRevision,
		"updateRevision", updateRevision,
		"ready", ready)

	return status, nil
}

// SetWeight sets the canary weight by adjusting the partition field
func (p *Plugin) SetWeight(ctx context.Context, workloadRef v1alpha1.WorkloadRef, weight int32) error {
	logger := log.FromContext(ctx)
	logger.Info("Setting weight", "name", workloadRef.Name, "namespace", workloadRef.Namespace, "weight", weight)

	// Get the StatefulSet
	sts, err := p.kubeClient.AppsV1().StatefulSets(workloadRef.Namespace).Get(ctx, workloadRef.Name, metav1.GetOptions{})
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

	logger.Info("Calculated partition", "replicas", replicas, "weight", weight, "partition", partition)

	// Update the partition field
	if sts.Spec.UpdateStrategy.RollingUpdate == nil {
		sts.Spec.UpdateStrategy.RollingUpdate = &appsv1.RollingUpdateStatefulSetStrategy{}
	}
	sts.Spec.UpdateStrategy.RollingUpdate.Partition = &partition

	// Update the StatefulSet
	_, err = p.kubeClient.AppsV1().StatefulSets(workloadRef.Namespace).Update(ctx, sts, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update StatefulSet partition: %w", err)
	}

	logger.Info("Successfully set partition", "partition", partition)
	return nil
}

// VerifyWeight verifies that the canary weight has been achieved
func (p *Plugin) VerifyWeight(ctx context.Context, workloadRef v1alpha1.WorkloadRef, weight int32) (bool, error) {
	logger := log.FromContext(ctx)
	logger.Info("Verifying weight", "name", workloadRef.Name, "namespace", workloadRef.Namespace, "weight", weight)

	// Get the StatefulSet
	sts, err := p.kubeClient.AppsV1().StatefulSets(workloadRef.Namespace).Get(ctx, workloadRef.Name, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to get StatefulSet: %w", err)
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
		logger.Info("Partition mismatch", "expected", expectedPartition, "actual", partition)
		return false, nil
	}

	// Calculate expected updated replicas
	expectedUpdated := replicas - expectedPartition

	// Verify that the correct number of pods are updated and ready
	updatedReplicas := replicas - partition
	ready := sts.Status.ReadyReplicas == replicas

	logger.Info("Weight verification",
		"expectedPartition", expectedPartition,
		"actualPartition", partition,
		"expectedUpdated", expectedUpdated,
		"actualUpdated", updatedReplicas,
		"readyReplicas", sts.Status.ReadyReplicas,
		"totalReplicas", replicas,
		"ready", ready)

	// Weight is achieved when:
	// 1. Partition is set correctly
	// 2. Expected number of pods are updated
	// 3. All pods are ready
	verified := partition == expectedPartition && updatedReplicas >= expectedUpdated && ready

	logger.Info("Weight verification result", "verified", verified)
	return verified, nil
}

// Promote completes the rollout by setting partition to 0
func (p *Plugin) Promote(ctx context.Context, workloadRef v1alpha1.WorkloadRef) error {
	logger := log.FromContext(ctx)
	logger.Info("Promoting rollout", "name", workloadRef.Name, "namespace", workloadRef.Namespace)

	// Get the StatefulSet
	sts, err := p.kubeClient.AppsV1().StatefulSets(workloadRef.Namespace).Get(ctx, workloadRef.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
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
		return fmt.Errorf("failed to promote StatefulSet: %w", err)
	}

	logger.Info("Successfully promoted rollout", "partition", partition)
	return nil
}

// Abort aborts the rollout by setting partition to replicas (all pods at old version)
func (p *Plugin) Abort(ctx context.Context, workloadRef v1alpha1.WorkloadRef) error {
	logger := log.FromContext(ctx)
	logger.Info("Aborting rollout", "name", workloadRef.Name, "namespace", workloadRef.Namespace)

	// Get the StatefulSet
	sts, err := p.kubeClient.AppsV1().StatefulSets(workloadRef.Namespace).Get(ctx, workloadRef.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
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
		return fmt.Errorf("failed to abort StatefulSet rollout: %w", err)
	}

	logger.Info("Successfully aborted rollout", "partition", partition)
	return nil
}

// Ensure Plugin implements ResourcePlugin interface
var _ rolloutplugin.ResourcePlugin = &Plugin{}
