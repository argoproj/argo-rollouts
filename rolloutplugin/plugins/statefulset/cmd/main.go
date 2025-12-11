package main

import (
	"context"
	"fmt"
	"os"

	goPlugin "github.com/hashicorp/go-plugin"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rolloutplugin/plugin/rpc"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

// handshakeConfigs are used to just do a basic handshake between
// a plugin and host. If the handshake fails, a user friendly error is shown.
var handshakeConfig = goPlugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
	MagicCookieValue: "resourceplugin",
}

// StatefulSetPlugin implements the ResourcePlugin interface
type StatefulSetPlugin struct {
	logCtx     *log.Entry
	kubeClient kubernetes.Interface
}

// NewStatefulSetPlugin creates a new StatefulSet plugin instance
func NewStatefulSetPlugin(logCtx *log.Entry) (*StatefulSetPlugin, error) {
	// Create Kubernetes client
	config, err := getKubeConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes config: %w", err)
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &StatefulSetPlugin{
		logCtx:     logCtx,
		kubeClient: kubeClient,
	}, nil
}

// getKubeConfig returns kubernetes config
func getKubeConfig() (*rest.Config, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}

	// Fall back to kubeconfig
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = clientcmd.RecommendedHomeFile
	}

	config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}

	return config, nil
}

// InitPlugin initializes the plugin
func (p *StatefulSetPlugin) InitPlugin() types.RpcError {
	p.logCtx.Info("Initializing StatefulSet plugin")
	return types.RpcError{}
}

// GetResourceStatus gets the current status of the StatefulSet
func (p *StatefulSetPlugin) GetResourceStatus(workloadRef v1alpha1.WorkloadRef) (*rpc.ResourceStatus, types.RpcError) {
	ctx := context.Background()
	p.logCtx.WithFields(log.Fields{
		"name":      workloadRef.Name,
		"namespace": workloadRef.Namespace,
	}).Info("Getting StatefulSet status")

	namespace := workloadRef.Namespace
	if namespace == "" {
		return nil, types.RpcError{ErrorString: "namespace is required in workloadRef"}
	}

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
func (p *StatefulSetPlugin) SetWeight(workloadRef v1alpha1.WorkloadRef, weight int32) types.RpcError {
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
func (p *StatefulSetPlugin) VerifyWeight(workloadRef v1alpha1.WorkloadRef, weight int32) (bool, types.RpcError) {
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

	// Calculate expected partition
	expectedPartition := replicas - (replicas * weight / 100)
	if expectedPartition < 0 {
		expectedPartition = 0
	}
	if expectedPartition > replicas {
		expectedPartition = replicas
	}

	// Calculate expected updated replicas
	expectedUpdatedReplicas := replicas - expectedPartition

	// Verify partition is set correctly
	if partition != expectedPartition {
		p.logCtx.WithFields(log.Fields{
			"expected": expectedPartition,
			"actual":   partition,
		}).Info("Partition mismatch")
		return false, types.RpcError{}
	}

	// Verify the expected number of pods are updated
	// UpdatedReplicas in status represents pods that have been updated
	if sts.Status.UpdatedReplicas < expectedUpdatedReplicas {
		p.logCtx.WithFields(log.Fields{
			"expected": expectedUpdatedReplicas,
			"actual":   sts.Status.UpdatedReplicas,
		}).Info("Not enough pods updated yet")
		return false, types.RpcError{}
	}

	// Verify all pods are ready
	if sts.Status.ReadyReplicas < replicas {
		p.logCtx.WithFields(log.Fields{
			"expected": replicas,
			"actual":   sts.Status.ReadyReplicas,
		}).Info("Not all pods are ready")
		return false, types.RpcError{}
	}

	// Verify StatefulSet has observed the latest generation
	if sts.Status.ObservedGeneration < sts.Generation {
		p.logCtx.Info("StatefulSet has not observed latest generation")
		return false, types.RpcError{}
	}

	p.logCtx.Info("Weight verification successful")
	return true, types.RpcError{}
}

// Promote completes the rollout by setting partition to 0
func (p *StatefulSetPlugin) Promote(workloadRef v1alpha1.WorkloadRef) types.RpcError {
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

	p.logCtx.Info("Successfully promoted rollout")
	return types.RpcError{}
}

// Abort aborts the rollout by setting partition to replicas (all pods at old version)
func (p *StatefulSetPlugin) Abort(workloadRef v1alpha1.WorkloadRef) types.RpcError {
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

	// Set partition to replicas to revert all pods to previous version
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

	p.logCtx.Info("Successfully aborted rollout")
	return types.RpcError{}
}

// Type returns the type of the plugin
func (p *StatefulSetPlugin) Type() string {
	return "StatefulSet"
}

func main() {
	logCtx := log.WithFields(log.Fields{"plugin": "statefulset"})

	// Set log level
	log.SetLevel(log.InfoLevel)
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

	// Create plugin implementation
	pluginImpl, err := NewStatefulSetPlugin(logCtx)
	if err != nil {
		logCtx.Fatalf("Failed to create plugin: %v", err)
	}

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]goPlugin.Plugin{
		"RpcResourcePlugin": &rpc.RpcResourcePlugin{Impl: pluginImpl},
	}

	logCtx.Info("Starting StatefulSet plugin server")

	goPlugin.Serve(&goPlugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
	})
}
