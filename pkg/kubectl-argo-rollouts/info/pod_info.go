package info

import (
	"fmt"
	"sort"
	"strings"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8snode "k8s.io/kubernetes/pkg/util/node"
)

func addPodInfos(rsInfos []*rollout.ReplicaSetInfo, allPods []*corev1.Pod) []*rollout.ReplicaSetInfo {
	var uids []types.UID
	uidToRSInfoIdx := make(map[types.UID]int)
	for i, rsInfo := range rsInfos {
		uids = append(uids, rsInfo.ObjectMeta.UID)
		uidToRSInfoIdx[rsInfo.ObjectMeta.UID] = i
	}

	for _, pod := range allPods {
		owner := ownerRef(pod.OwnerReferences, uids)
		if owner == nil {
			continue
		}

		podInfo := newPodInfo(pod)
		idx := uidToRSInfoIdx[owner.UID]
		rsInfos[idx].Pods = append(rsInfos[idx].Pods, &podInfo)
	}

	for _, rsInfo := range rsInfos {
		sort.Slice(rsInfo.Pods[:], func(i, j int) bool {
			if rsInfo.Pods[i].ObjectMeta.CreationTimestamp != rsInfo.Pods[j].ObjectMeta.CreationTimestamp {
				return rsInfo.Pods[i].ObjectMeta.CreationTimestamp.Before(&rsInfo.Pods[j].ObjectMeta.CreationTimestamp)
			}
			return rsInfo.Pods[i].ObjectMeta.Name < rsInfo.Pods[j].ObjectMeta.Name
		})
	}

	return rsInfos
}

func newPodInfo(pod *corev1.Pod) rollout.PodInfo {
	podInfo := rollout.PodInfo{
		ObjectMeta: &v1.ObjectMeta{
			Name:              pod.Name,
			Namespace:         pod.Namespace,
			CreationTimestamp: pod.CreationTimestamp,
			UID:               pod.UID,
		},
	}
	restarts := 0
	totalContainers := len(pod.Spec.Containers)
	readyContainers := 0

	reason := string(pod.Status.Phase)
	if pod.Status.Reason != "" {
		reason = pod.Status.Reason
	}

	initializing := false
	for i, container := range pod.Status.InitContainerStatuses {
		restarts += int(container.RestartCount)
		switch {
		case container.State.Terminated != nil && container.State.Terminated.ExitCode == 0:
			continue
		case container.State.Terminated != nil:
			// initialization is failed
			if len(container.State.Terminated.Reason) == 0 {
				if container.State.Terminated.Signal != 0 {
					reason = fmt.Sprintf("Init:Signal:%d", container.State.Terminated.Signal)
				} else {
					reason = fmt.Sprintf("Init:ExitCode:%d", container.State.Terminated.ExitCode)
				}
			} else {
				reason = "Init:" + container.State.Terminated.Reason
			}
			initializing = true
		case container.State.Waiting != nil && len(container.State.Waiting.Reason) > 0 && container.State.Waiting.Reason != "PodInitializing":
			reason = "Init:" + container.State.Waiting.Reason
			initializing = true
		default:
			reason = fmt.Sprintf("Init:%d/%d", i, len(pod.Spec.InitContainers))
			initializing = true
		}
		break
	}
	if !initializing {
		restarts = 0
		hasRunning := false
		for i := len(pod.Status.ContainerStatuses) - 1; i >= 0; i-- {
			container := pod.Status.ContainerStatuses[i]
			restarts += int(container.RestartCount)
			if container.State.Waiting != nil && container.State.Waiting.Reason != "" {
				reason = container.State.Waiting.Reason
			} else if container.State.Terminated != nil && container.State.Terminated.Reason != "" {
				reason = container.State.Terminated.Reason
			} else if container.State.Terminated != nil && container.State.Terminated.Reason == "" {
				if container.State.Terminated.Signal != 0 {
					reason = fmt.Sprintf("Signal:%d", container.State.Terminated.Signal)
				} else {
					reason = fmt.Sprintf("ExitCode:%d", container.State.Terminated.ExitCode)
				}
			} else if container.Ready && container.State.Running != nil {
				hasRunning = true
				readyContainers++
			}
		}

		// change pod status back to "Running" if there is at least one container still reporting as "Running" status
		if reason == "Completed" && hasRunning {
			reason = "Running"
		}
	}

	if pod.DeletionTimestamp != nil && pod.Status.Reason == k8snode.NodeUnreachablePodReason {
		reason = "Unknown"
	} else if pod.DeletionTimestamp != nil {
		reason = "Terminating"
	}

	podInfo.Status = reason
	podInfo.Icon = podIcon(podInfo.Status)
	podInfo.Ready = fmt.Sprintf("%d/%d", readyContainers, totalContainers)
	podInfo.Restarts = int32(restarts)
	return podInfo
}

func podIcon(status string) string {
	if strings.HasPrefix(status, "Init:") {
		return IconProgressing
	}
	if strings.HasPrefix(status, "Signal:") || strings.HasPrefix(status, "ExitCode:") {
		return IconBad
	}
	// See:
	// https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/images/types.go
	// https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/kuberuntime/kuberuntime_container.go
	// https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/container/sync_result.go
	if strings.HasSuffix(status, "Error") || strings.HasPrefix(status, "Err") {
		return IconWarning
	}
	switch status {
	case "Pending", "Terminating", "ContainerCreating":
		return IconProgressing
	case "Running", "Completed":
		return IconOK
	case "Failed", "InvalidImageName", "CrashLoopBackOff":
		return IconBad
	case "ImagePullBackOff", "RegistryUnavailable":
		return IconWarning
	case "Unknown":
		return IconUnknown
	}
	return " "
}
