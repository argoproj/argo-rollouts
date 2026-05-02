package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rolloutplugin/plugin/rpc"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
)

// Verify interface compliance at compile time.
var _ rpc.ResourcePlugin = &RpcPlugin{}

const fieldManager = "argo-rollouts-resource-e2e-plugin"

// RpcPlugin is a minimal resource plugin for E2E testing.
type RpcPlugin struct {
	LogCtx     *log.Entry
	kubeClient kubernetes.Interface
}

func New(logCtx *log.Entry) rpc.ResourcePlugin {
	return &RpcPlugin{LogCtx: logCtx}
}

func (p *RpcPlugin) InitPlugin(namespace string) types.RpcError {
	p.LogCtx.Infof("InitPlugin called: namespace=%s", namespace)

	config, err := ctrl.GetConfig()
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("failed to get k8s config: %v", err)}
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("failed to create kube client: %v", err)}
	}
	p.kubeClient = client
	return types.RpcError{}
}

func (p *RpcPlugin) GetResourceStatus(workloadRef v1alpha1.WorkloadRef) (*types.ResourceStatus, types.RpcError) {
	p.LogCtx.Infof("GetResourceStatus: apiVersion=%s kind=%s name=%s", workloadRef.APIVersion, workloadRef.Kind, workloadRef.Name)

	if p.kubeClient == nil {
		return nil, types.RpcError{ErrorString: "kube client not initialized"}
	}

	ns := workloadRef.Namespace

	sts, err := p.kubeClient.AppsV1().StatefulSets(ns).Get(context.Background(), workloadRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, types.RpcError{ErrorString: fmt.Sprintf("failed to get StatefulSet %s/%s: %v", ns, workloadRef.Name, err)}
	}

	return statefulSetResourceStatus(sts), types.RpcError{}
}

// statefulSetResourceStatus converts a StatefulSet into a ResourceStatus.
func statefulSetResourceStatus(sts *appsv1.StatefulSet) *types.ResourceStatus {
	return &types.ResourceStatus{
		Replicas:          sts.Status.Replicas,
		UpdatedReplicas:   sts.Status.UpdatedReplicas,
		ReadyReplicas:     sts.Status.ReadyReplicas,
		AvailableReplicas: sts.Status.AvailableReplicas,
		CurrentRevision:   sts.Status.CurrentRevision,
		UpdatedRevision:   sts.Status.UpdateRevision,
	}
}

func (p *RpcPlugin) SetWeight(workloadRef v1alpha1.WorkloadRef, weight int32) types.RpcError {
	p.LogCtx.Infof("SetWeight: name=%s weight=%d", workloadRef.Name, weight)

	sts, err := p.kubeClient.AppsV1().StatefulSets(workloadRef.Namespace).Get(context.Background(), workloadRef.Name, metav1.GetOptions{})
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("SetWeight: failed to get StatefulSet: %v", err)}
	}

	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}
	partition := replicas - (replicas * weight / 100)
	return p.patchPartition(workloadRef.Namespace, workloadRef.Name, partition)
}

func (p *RpcPlugin) VerifyWeight(workloadRef v1alpha1.WorkloadRef, weight int32) (bool, types.RpcError) {
	p.LogCtx.Infof("VerifyWeight: name=%s weight=%d", workloadRef.Name, weight)
	return true, types.RpcError{}
}

func (p *RpcPlugin) PromoteFull(workloadRef v1alpha1.WorkloadRef) types.RpcError {
	p.LogCtx.Infof("PromoteFull: name=%s", workloadRef.Name)
	return p.patchPartition(workloadRef.Namespace, workloadRef.Name, 0)
}

// patchPartition updates the StatefulSet's partition
func (p *RpcPlugin) patchPartition(namespace, name string, partition int32) types.RpcError {
	patch := map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "StatefulSet",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
		},
		"spec": map[string]interface{}{
			"updateStrategy": map[string]interface{}{
				"rollingUpdate": map[string]interface{}{
					"partition": partition,
				},
			},
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("patchPartition: marshal failed: %v", err)}
	}
	_, err = p.kubeClient.AppsV1().StatefulSets(namespace).Patch(
		context.Background(), name, k8stypes.ApplyPatchType, patchBytes,
		metav1.PatchOptions{FieldManager: fieldManager, Force: boolPtr(true)},
	)
	if err != nil {
		return types.RpcError{ErrorString: fmt.Sprintf("patchPartition: patch failed: %v", err)}
	}
	p.LogCtx.Infof("patchPartition: set partition=%d on %s/%s", partition, namespace, name)
	return types.RpcError{}
}

func boolPtr(b bool) *bool { return &b }

func (p *RpcPlugin) Abort(workloadRef v1alpha1.WorkloadRef) types.RpcError {
	p.LogCtx.Infof("Abort: name=%s", workloadRef.Name)
	return types.RpcError{}
}

func (p *RpcPlugin) Restart(workloadRef v1alpha1.WorkloadRef) types.RpcError {
	p.LogCtx.Infof("Restart: name=%s", workloadRef.Name)
	return types.RpcError{}
}

func (p *RpcPlugin) Type() string {
	return "E2EResourcePlugin"
}
